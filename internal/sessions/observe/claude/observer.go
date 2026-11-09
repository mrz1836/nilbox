package claude

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/logging"
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
	// failed, or "".
	entries map[string]Entry
	failure string

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
// folder changed, and otherwise every SafetyPoll.
func (o *Observer) Observe(ctx context.Context, now time.Time, live []sessions.Record) []sessions.Observation {
	o.mu.Lock()
	defer o.mu.Unlock()

	groups := map[string][]sessions.Record{}
	for _, r := range live {
		groups[r.ConfigDir] = append(groups[r.ConfigDir], r)
	}

	scan := &processScan{lister: o.cfg.Procs}

	var out []sessions.Observation

	for _, configDir := range sortedKeys(groups) {
		dir := o.dir(configDir)
		recs := groups[configDir]

		if o.due(dir, recs, now) {
			o.ask(ctx, dir, configDir, now)
			dir.ids = idsOf(recs)
		}

		for _, r := range recs {
			out = append(out, o.observe(ctx, dir, r, now, scan))
		}
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

// due reports whether the agents of a config directory are to be listed now.
func (o *Observer) due(d *dirState, recs []sessions.Record, now time.Time) bool {
	if d.asked.IsZero() || d.ids != idsOf(recs) {
		return true
	}

	hint, ok := d.readHint()
	if !ok {
		return now.Sub(d.asked) >= BlindPoll
	}

	return hint != d.hint || now.Sub(d.asked) >= SafetyPoll
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
	if failure == "" {
		d.entries = entries
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

// observe reports one session: as listed when it is, and otherwise running, starting, or
// ended by its process.
func (o *Observer) observe(
	ctx context.Context, d *dirState, r sessions.Record, now time.Time, scan *processScan,
) sessions.Observation {
	if e, ok := d.entries[r.AgentSessionID]; ok && d.failure == "" && r.AgentSessionID != "" {
		delete(d.absent, r.ID)

		obs := sessions.Observation{ID: r.ID, PID: e.PID, LastMessage: o.lastMessage(d, r.AgentSessionID), Seen: true}
		obs.State, obs.Detail = MapState(e)

		return obs
	}

	return o.notListed(ctx, d, r, now, scan)
}

// notListed reports a session Claude Code did not list: unknown while its process runs
// and Claude Code cannot be asked, as it was while it runs and was only briefly left out,
// ended once its process is gone, starting within its grace, and otherwise as its folder
// shows.
func (o *Observer) notListed(
	ctx context.Context, d *dirState, r sessions.Record, now time.Time, scan *processScan,
) sessions.Observation {
	obs := sessions.Observation{ID: r.ID}
	pid := r.PID

	switch {
	case pid > 0 && o.cfg.Alive(pid) && d.failure != "":
		obs.State, obs.Detail = sessions.StateUnknown, "Claude Code didn't say how this session is"
	case pid > 0 && o.cfg.Alive(pid) && d.absentFor(r.ID, now) < AbsentGrace:
		// Listed before and running still: Claude Code lists it again on its next change.
	case pid > 0:
		obs.State = sessions.StateEnded
		obs.LastMessage = o.lastMessage(d, r.AgentSessionID)
	case now.Sub(r.LaunchedAt) < Grace:
		obs.State = sessions.StateStarting
	default:
		obs.State, obs.PID = o.unlisted(ctx, r, scan)
	}

	return obs
}

// unlisted reports a session past its grace that Claude Code never listed: running, when
// some process works in its folder, ended when none does, and unknown when the processes
// cannot be listed.
func (o *Observer) unlisted(ctx context.Context, r sessions.Record, scan *processScan) (sessions.State, int) {
	found, ok := scan.in(ctx, r.Dir, agentInfo().Command)

	switch {
	case !ok:
		return sessions.StateUnknown, 0
	case found > 0:
		return sessions.StateUnknown, found
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
// folder, or "".
func findTranscript(configDir, sessionID string) string {
	if !agent.ValidSessionID(sessionID) {
		return ""
	}

	matches, err := filepath.Glob(filepath.Join(configDir, projectsDir, "*", sessionID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return ""
	}

	return matches[0]
}

// processScan lists the running processes at most once per Observe.
type processScan struct {
	lister procs.Lister
	done   bool
	list   []procs.Process
	err    error
}

// in returns a process working in dir itself, preferring one running command, or 0 for
// none, and false when the processes cannot be listed.
func (s *processScan) in(ctx context.Context, dir, command string) (int, bool) {
	if s.lister == nil {
		return 0, false
	}

	if !s.done {
		s.done = true
		s.list, s.err = s.lister.Processes(ctx)
	}

	if s.err != nil {
		return 0, false
	}

	found := procs.In(s.list, dir)
	if len(found) == 0 {
		return 0, true
	}

	for _, p := range found {
		if filepath.Base(p.Command) == command {
			return p.PID, true
		}
	}

	return found[0].PID, true
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

// sortedKeys returns a map's keys in order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return keys
}
