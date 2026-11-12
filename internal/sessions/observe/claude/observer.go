package claude

import (
	"context"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/sessions"
)

// The observer's pace.
const (
	// SafetyPoll is how often the agents are listed when nothing hints at a change.
	SafetyPoll = 20 * time.Second

	// BlindPoll is how often they are listed when the hint cannot be read, as when
	// Claude Code keeps no sessions folder.
	BlindPoll = 5 * time.Second

	// AbsentGrace is how long a session with a running process may go unlisted by Claude
	// Code before it is taken for ended: its process ID may have gone to another program.
	AbsentGrace = 30 * time.Second

	// Grace is how long after its launch a session may go unlisted before it is taken
	// for one that never started: Claude Code lists a session once past its first
	// questions.
	Grace = 90 * time.Second

	// askTimeout bounds one `claude agents --json`.
	askTimeout = 10 * time.Second

	// sessionsDir is the folder in a config directory where Claude Code keeps a file per
	// running session. It is read only as a hint that something changed, never as data.
	sessionsDir = "sessions"

	// projectsDir is the folder in a config directory that holds the transcripts.
	projectsDir = "projects"
)

// Config holds an Observer's dependencies. Finder and Run are required.
type Config struct {
	// Finder finds the claude command.
	Finder execpath.Finder

	// Run returns a runner whose programs run with the whole environment env.
	Run func(env []string) execpath.Runner

	// Environ is this process's environment; it defaults to os.Environ.
	Environ func() []string

	// Home is the user's home directory, whose .claude is the default config directory.
	Home string

	// Procs lists the running processes, to find a session's process when Claude Code
	// cannot be asked. Optional.
	Procs procs.Lister

	// Alive reports whether a process runs; it defaults to procs.Alive.
	Alive func(pid int) bool

	Logger *slog.Logger
}

// Observer observes Claude Code sessions. It is safe for concurrent use.
type Observer struct {
	cfg    Config
	logger *slog.Logger

	mu          sync.Mutex
	dirs        map[string]*dirState
	transcripts map[string]*transcript
}

// dirState is what the observer knows of one config directory's sessions.
type dirState struct {
	path string

	// asked is when the agents were last listed, ids the sessions followed then, and hint
	// what the sessions folder looked like.
	asked time.Time
	ids   string
	hint  string

	// entries are the sessions listed, by session ID, and failure why the last listing
	// failed, or "", failing since failedAt.
	entries  map[string]Entry
	failure  string
	failedAt time.Time

	// absent holds when Claude Code first left out a session whose process still runs.
	absent map[string]time.Time
}

// transcript is a session's transcript, as last read.
type transcript struct {
	path    string
	mod     time.Time
	size    int64
	message string
}

// New returns an Observer over cfg.
func New(cfg Config) *Observer {
	if cfg.Environ == nil {
		cfg.Environ = os.Environ
	}

	if cfg.Alive == nil {
		cfg.Alive = procs.Alive
	}

	return &Observer{
		cfg: cfg, logger: logging.Or(cfg.Logger), dirs: map[string]*dirState{}, transcripts: map[string]*transcript{},
	}
}

// Observe reports what each of live is doing. It lists the agents of each config
// directory the sessions run with when one is new to it, when the directory's sessions
// folder changed, and, while one of them runs, every SafetyPoll.
func (o *Observer) Observe(ctx context.Context, now time.Time, live []sessions.Record) []sessions.Observation {
	o.mu.Lock()
	defer o.mu.Unlock()

	groups := map[string][]sessions.Record{}
	for _, r := range live {
		groups[r.ConfigDir] = append(groups[r.ConfigDir], r)
	}

	scan := &processScan{lister: o.cfg.Procs}
	out := make([]sessions.Observation, 0, len(live))

	for _, configDir := range slices.Sorted(maps.Keys(groups)) {
		out = append(out, o.observeDir(ctx, configDir, groups[configDir], now, scan)...)
	}

	for key := range o.dirs {
		if _, ok := groups[key]; !ok {
			delete(o.dirs, key)
		}
	}

	o.forgetTranscripts(live)

	return out
}

// Close forgets what was learned; the next Observe lists the agents again.
func (o *Observer) Close() {
	o.mu.Lock()
	defer o.mu.Unlock()

	clear(o.dirs)
	clear(o.transcripts)
}

// observeDir reports the sessions recs that run with one config directory, listing its
// agents first when they are due.
func (o *Observer) observeDir(
	ctx context.Context, configDir string, recs []sessions.Record, now time.Time, scan *processScan,
) []sessions.Observation {
	dir := o.dir(configDir)

	if o.due(dir, recs, now) {
		o.ask(ctx, dir, configDir, now)
		dir.ids = idsOf(recs)
	}

	out := make([]sessions.Observation, 0, len(recs))

	for _, r := range recs {
		if obs, ok := o.observe(ctx, dir, r, recs, now, scan); ok {
			out = append(out, obs)
		}
	}

	return out
}

// dir returns what is known of a config directory, "" for the default.
func (o *Observer) dir(configDir string) *dirState {
	if d, ok := o.dirs[configDir]; ok {
		return d
	}

	path := configDir
	if path == "" {
		path = filepath.Join(o.cfg.Home, agentInfo().ConfigDir)
	}

	d := &dirState{path: path}
	o.dirs[configDir] = d

	return d
}

// due reports whether the agents of a config directory are to be listed now: at once for
// sessions new to it, then on a change to its sessions folder, and, while one of them
// runs, every SafetyPoll, or every BlindPoll when the folder cannot be read. Sessions that
// ended, watched only for being resumed outside the app, are never polled for: a resumed
// session adds its file to the folder, which is the change that lists them again.
func (o *Observer) due(d *dirState, recs []sessions.Record, now time.Time) bool {
	if d.asked.IsZero() || d.ids != idsOf(recs) {
		return true
	}

	running := slices.ContainsFunc(recs, func(r sessions.Record) bool { return r.State.Live() })

	hint, ok := d.readHint()

	switch {
	case !ok:
		return running && now.Sub(d.asked) >= BlindPoll
	case hint != d.hint:
		return true
	default:
		return running && now.Sub(d.asked) >= SafetyPoll
	}
}

// ask lists the agents of a config directory with `claude agents --json`, run with that
// directory as Claude Code's config, or with none for the default.
func (o *Observer) ask(ctx context.Context, d *dirState, configDir string, now time.Time) {
	entries, failure := o.list(ctx, configDir)
	if failure != "" && failure != d.failure {
		o.logger.WarnContext(ctx, "could not ask Claude Code how its sessions are", "configDir", configDir,
			"error", failure)
	}

	d.asked, d.failure = now, failure

	switch {
	case failure == "":
		d.entries, d.failedAt = entries, time.Time{}
	case d.failedAt.IsZero():
		d.failedAt = now
	}

	// The hint is read once the listing names the sessions' processes, whose files it
	// stats too.
	d.hint, _ = d.readHint()
}

// list runs `claude agents --json`, and returns what it listed, or why it could not.
func (o *Observer) list(ctx context.Context, configDir string) (map[string]Entry, string) {
	a := agentInfo()

	bin, err := o.cfg.Finder.Find(a.Command)
	if err != nil {
		return nil, err.Error()
	}

	ctx, cancel := context.WithTimeout(ctx, askTimeout)
	defer cancel()

	out, stderr, err := o.cfg.Run(o.env(configDir)).Run(ctx, bin, "agents", "--json")
	if err != nil {
		if detail := execpath.FirstLine(stderr); detail != "" {
			return nil, err.Error() + ": " + detail
		}

		return nil, err.Error()
	}

	entries, err := ParseAgents(out)
	if err != nil {
		return nil, err.Error()
	}

	return entries, ""
}

// env is this process's environment with Claude Code's config variable set to configDir,
// or unset for the default.
func (o *Observer) env(configDir string) []string {
	name := agentInfo().ConfigEnv

	env := slices.DeleteFunc(slices.Clone(o.cfg.Environ()), func(kv string) bool {
		return strings.HasPrefix(kv, name+"=")
	})

	if configDir != "" {
		env = append(env, name+"="+configDir)
	}

	return env
}

// observe reports one session, and whether there is anything to report: as listed when it
// is, as the conversation its own process moved on to when that is listed instead, and
// otherwise running, starting, or ended by its process. A session that ended is reported
// only once Claude Code lists it again, resumed outside the app.
func (o *Observer) observe(
	ctx context.Context, d *dirState, r sessions.Record, recs []sessions.Record, now time.Time, scan *processScan,
) (sessions.Observation, bool) {
	if e, ok := d.entries[r.AgentSessionID]; ok && d.failure == "" && r.AgentSessionID != "" {
		delete(d.absent, r.ID)

		return o.listed(d, r, e, false), true
	}

	if !r.State.Live() {
		return sessions.Observation{}, false
	}

	if e, ok := d.movedOn(r, recs); ok {
		delete(d.absent, r.ID)

		return o.listed(d, r, e, true), true
	}

	return o.notListed(ctx, d, r, now, scan), true
}

// listed reports a session as Claude Code listed it, as e; relinked reports e is the
// conversation the session's process moved on to.
func (o *Observer) listed(d *dirState, r sessions.Record, e Entry, relinked bool) sessions.Observation {
	obs := sessions.Observation{
		ID: r.ID, PID: e.PID, LastMessage: o.lastMessage(d, e.SessionID), Seen: true, Relinked: relinked,
	}

	if relinked {
		obs.AgentSessionID = e.SessionID
	}

	obs.State, obs.Detail = MapState(e)

	return obs
}

// movedOn returns the conversation a running session's own process went on to, as
// Claude Code's /clear and /resume move a session to another: the one Claude Code lists
// under the session's process, in its folder, that no other session follows.
func (d *dirState) movedOn(r sessions.Record, recs []sessions.Record) (Entry, bool) {
	if d.failure != "" || r.PID <= 0 {
		return Entry{}, false
	}

	for _, e := range d.entries {
		if e.PID != r.PID || !agent.ValidSessionID(e.SessionID) || e.Cwd == "" || !paths.Same(e.Cwd, r.Dir) {
			continue
		}

		followed := slices.ContainsFunc(recs, func(other sessions.Record) bool {
			return other.ID != r.ID && other.AgentSessionID == e.SessionID
		})
		if !followed {
			return e, true
		}
	}

	return Entry{}, false
}

// notListed reports a session Claude Code did not list. While its process runs, it is
// left as it was when Claude Code could not be asked for a moment or only briefly left it
// out, and unknown when Claude Code cannot be asked for longer. It is ended once its
// process is gone, starting within its grace, and otherwise as its folder shows.
func (o *Observer) notListed(
	ctx context.Context, d *dirState, r sessions.Record, now time.Time, scan *processScan,
) sessions.Observation {
	obs := sessions.Observation{ID: r.ID}
	pid := r.PID

	switch {
	case pid > 0 && o.cfg.Alive(pid):
		if obs.State, obs.Detail = d.stillRunning(r.ID, now); obs.State == sessions.StateEnded {
			obs.LastMessage = o.lastMessage(d, r.AgentSessionID)
		}
	case pid > 0:
		obs.State = sessions.StateEnded
		obs.LastMessage = o.lastMessage(d, r.AgentSessionID)
	case now.Sub(r.LaunchedAt) < Grace:
		obs.State = sessions.StateStarting
	default:
		obs.State, obs.PID = o.unlisted(ctx, d, r, scan)
	}

	return obs
}

// stillRunning reports a session Claude Code did not list whose process still runs: left
// as it was, a zero State, when Claude Code could not be asked just now, or only briefly
// left it out, as it does until its next change; unknown when Claude Code cannot be asked
// for longer; and ended when it has left the session out that long, since its process ID
// may belong to another program by then.
func (d *dirState) stillRunning(id string, now time.Time) (sessions.State, string) {
	switch {
	case d.failure != "" && now.Sub(d.failedAt) < AbsentGrace:
		return "", ""
	case d.failure != "":
		return sessions.StateUnknown, "Claude Code didn't say how this session is"
	case d.absentFor(id, now) < AbsentGrace:
		return "", ""
	default:
		return sessions.StateEnded, ""
	}
}

// unlisted reports a session past its grace that Claude Code never listed. It is running,
// with that process, when a Claude Code process works in its folder; unknown when the
// processes cannot be listed, or when other programs work there and Claude Code cannot be
// asked; and ended otherwise. Another program's process is never taken for the session's.
func (o *Observer) unlisted(
	ctx context.Context, d *dirState, r sessions.Record, scan *processScan,
) (sessions.State, int) {
	found, others, ok := scan.in(ctx, r.Dir)

	switch {
	case !ok:
		return sessions.StateUnknown, 0
	case found > 0:
		return sessions.StateUnknown, found
	case others && d.failure != "":
		return sessions.StateUnknown, 0
	default:
		return sessions.StateEnded, 0
	}
}

// lastMessage returns what the session last said, reading its transcript again only
// when it changed.
func (o *Observer) lastMessage(d *dirState, sessionID string) string {
	if sessionID == "" {
		return ""
	}

	t := o.transcripts[sessionID]
	if t == nil || t.path == "" {
		path := findTranscript(d.path, sessionID)
		if path == "" {
			return ""
		}

		t = &transcript{path: path}
		o.transcripts[sessionID] = t
	}

	info, err := os.Stat(t.path)
	if err != nil {
		t.path = ""

		return t.message
	}

	if info.ModTime().Equal(t.mod) && info.Size() == t.size {
		return t.message
	}

	if msg, err := LastMessage(t.path); err == nil {
		t.mod, t.size, t.message = info.ModTime(), info.Size(), msg
	}

	return t.message
}

// forgetTranscripts drops the transcripts of sessions no longer followed.
func (o *Observer) forgetTranscripts(live []sessions.Record) {
	keep := map[string]bool{}
	for _, r := range live {
		keep[r.AgentSessionID] = true
	}

	for id := range o.transcripts {
		if !keep[id] {
			delete(o.transcripts, id)
		}
	}
}

// absentFor returns how long Claude Code has left a session out, counting from now when
// this is the first time.
func (d *dirState) absentFor(id string, now time.Time) time.Duration {
	if d.absent == nil {
		d.absent = map[string]time.Time{}
	}

	since, ok := d.absent[id]
	if !ok {
		d.absent[id] = now

		return 0
	}

	return now.Sub(since)
}

// readHint stats the config directory's sessions folder and the files in it, and
// returns what it saw, or false when the folder cannot be read.
func (d *dirState) readHint() (string, bool) {
	dir := filepath.Join(d.path, sessionsDir)

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", false
	}

	var b strings.Builder

	b.WriteString(strconv.FormatInt(info.ModTime().UnixNano(), 10))

	for _, e := range d.entries {
		if e.PID <= 0 {
			continue
		}

		if fi, err := os.Stat(filepath.Join(dir, strconv.Itoa(e.PID)+".json")); err == nil {
			b.WriteString(" " + strconv.Itoa(e.PID) + ":" + strconv.FormatInt(fi.ModTime().UnixNano(), 10))
		}
	}

	return b.String(), true
}

// findTranscript returns the transcript of a session under a config directory's projects
// folder, or "": the regular file <projects>/<project>/<sessionID>.jsonl. The folders are
// listed rather than matched as a pattern, so a config directory whose name holds a
// pattern's characters is read as named.
func findTranscript(configDir, sessionID string) string {
	if !agent.ValidSessionID(sessionID) {
		return ""
	}

	projects := filepath.Join(configDir, projectsDir)

	entries, err := os.ReadDir(projects)
	if err != nil {
		return ""
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		path := filepath.Join(projects, e.Name(), sessionID+".jsonl")
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}

	return ""
}

// processScan lists the running processes at most once per Observe, and resolves each
// one's directory at most once, however many sessions' folders it is asked of.
type processScan struct {
	lister  procs.Lister
	done    bool
	located *procs.Located
	err     error
}

// in returns a Claude Code process working in dir itself, or 0 for none, whether other
// programs work there, and false when the processes cannot be listed.
func (s *processScan) in(ctx context.Context, dir string) (pid int, others, ok bool) {
	if s.lister == nil {
		return 0, false, false
	}

	if !s.done {
		s.done = true

		list, err := s.lister.Processes(ctx)
		s.located, s.err = procs.Locate(list), err
	}

	if s.err != nil {
		return 0, false, false
	}

	for _, p := range s.located.In(dir) {
		if pid == 0 && isClaudeCommand(p.Command) {
			pid = p.PID
		} else {
			others = true
		}
	}

	return pid, others, true
}

// versionName is the name of a Claude Code build as its native installer lays it out, a
// file named for its version, such as 2.1.319, which the OS then reports as the command.
var versionName = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// isClaudeCommand reports a process running Claude Code: the claude command, or a build
// named for its version.
func isClaudeCommand(command string) bool {
	name := filepath.Base(command)

	return name == agentInfo().Command || versionName.MatchString(name)
}

// agentInfo is Claude Code's spec.
func agentInfo() agent.Agent {
	a, _ := agent.Lookup(agent.Claude)

	return a
}

// idsOf names the sessions followed, in order.
func idsOf(recs []sessions.Record) string {
	ids := make([]string, 0, len(recs))
	for _, r := range recs {
		ids = append(ids, r.ID)
	}

	slices.Sort(ids)

	return strings.Join(ids, ",")
}
