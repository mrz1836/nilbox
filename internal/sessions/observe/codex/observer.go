package codex

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/sessions"
)

// The observer's pace.
const (
	// Poll is how often the daemon is asked about the sessions when it has said nothing
	// changed.
	Poll = 3 * time.Second

	// Redial is how long the observer waits to connect again after it could not.
	Redial = 30 * time.Second

	// Recheck is how often a session's known process is checked to still be Codex, in the
	// session's folder: its process ID may have gone to another program.
	Recheck = 30 * time.Second

	// Grace is how long after its launch a session may have no process of its own found
	// before it is taken for one that never started.
	Grace = 60 * time.Second

	// dialTimeout bounds connecting to the daemon and initializing the connection, so a
	// daemon that hangs holds up following only briefly before the observer falls back
	// to the rollout files.
	dialTimeout = 3 * time.Second

	// callTimeout bounds one call to the daemon.
	callTimeout = 10 * time.Second

	// listLimit caps the threads one listing returns.
	listLimit = 50
)

// Config holds an Observer's dependencies. Dialer is required.
type Config struct {
	// Dialer connects to the daemon of a Codex home.
	Dialer Dialer

	// Home is the user's home directory, whose .codex is the default Codex home.
	Home string

	// Procs lists the running processes, to find each session's process. Optional.
	Procs procs.Lister

	// Alive reports whether a process runs; it defaults to procs.Alive.
	Alive func(pid int) bool

	Logger *slog.Logger
}

// Observer observes Codex sessions. It is safe for concurrent use.
type Observer struct {
	cfg    Config
	logger *slog.Logger

	mu    sync.Mutex
	homes map[string]*home

	// checked holds when each session's process was last found to be Codex in its folder.
	checked map[string]time.Time
}

// home is what the observer knows of one Codex home's sessions.
type home struct {
	path string

	// client is the connection to the daemon, or nil; dialed is when the last attempt to
	// connect failed, and failure why.
	client  *Client
	dialed  time.Time
	failure string

	// asked is when the daemon was last asked, and ids the sessions followed then.
	asked time.Time
	ids   string

	// threads are what is known of each linked thread, by ID.
	threads map[string]*thread

	// rollouts reads the home's rollout files, for when the daemon cannot be reached.
	rollouts *rollouts
}

// thread is what is known of one thread.
type thread struct {
	status  ThreadStatus
	updated int64

	// turnAt is the thread's update the latest turn was read at, and turnRead the second
	// it was read in; turnState, turnDetail, and message are what it showed.
	turnAt     int64
	turnRead   int64
	turnState  sessions.State
	turnDetail string
	message    string
}

// settled reports whether the latest turn read still holds: it ended, and the thread
// has not changed since. Codex stamps a thread's updates in whole seconds, so a turn read
// in the second of the thread's last update holds only once read again after it.
func (t *thread) settled() bool {
	return t.turnState == sessions.StateIdle && t.turnAt == t.updated && t.turnRead > t.updated
}

// New returns an Observer over cfg.
func New(cfg Config) *Observer {
	if cfg.Alive == nil {
		cfg.Alive = procs.Alive
	}

	return &Observer{cfg: cfg, logger: logging.Or(cfg.Logger), homes: map[string]*home{}, checked: map[string]time.Time{}}
}

// Observe reports what each of live is doing. It connects to the daemon of each Codex
// home the sessions run with, while any does, and asks it every Poll, or at once when it
// says a thread changed, or a session is new to it.
func (o *Observer) Observe(ctx context.Context, now time.Time, live []sessions.Record) []sessions.Observation {
	o.mu.Lock()
	defer o.mu.Unlock()

	groups := map[string][]sessions.Record{}
	for _, r := range live {
		groups[r.ConfigDir] = append(groups[r.ConfigDir], r)
	}

	for key, h := range o.homes {
		if _, ok := groups[key]; !ok {
			h.close()
			delete(o.homes, key)
		}
	}

	scan := &processScan{lister: o.cfg.Procs, claimed: claims(live)}

	var out []sessions.Observation

	for _, codexHome := range slices.Sorted(maps.Keys(groups)) {
		h := o.home(codexHome)
		recs := groups[codexHome]

		linked := o.refresh(ctx, h, codexHome, recs, now)

		for _, r := range recs {
			out = append(out, o.observe(ctx, h, r, linked[r.ID], now, scan))
		}
	}

	return out
}

// Close closes every connection to the daemons; the next Observe connects again.
func (o *Observer) Close() {
	o.mu.Lock()
	defer o.mu.Unlock()

	for key, h := range o.homes {
		h.close()
		delete(o.homes, key)
	}
}

// home returns what is known of a Codex home, "" for the default.
func (o *Observer) home(codexHome string) *home {
	if h, ok := o.homes[codexHome]; ok {
		return h
	}

	path := codexHome
	if path == "" {
		path = filepath.Join(o.cfg.Home, codexInfo().ConfigDir)
	}

	h := &home{path: path, threads: map[string]*thread{}, rollouts: newRollouts(path)}
	o.homes[codexHome] = h

	return h
}

// refresh asks the daemon about a home's sessions when it is due, connecting first if it
// must, and links each session not yet linked. It returns the thread each session it
// linked now is, by the app's ID.
func (o *Observer) refresh(
	ctx context.Context, h *home, codexHome string, recs []sessions.Record, now time.Time,
) map[string]string {
	connected := o.connect(ctx, h, codexHome, now)
	if !h.due(recs, now) {
		return nil
	}

	h.asked, h.ids = now, idsOf(recs)

	if !connected {
		return o.linkRollouts(h, recs, now)
	}

	linked, err := o.ask(ctx, h, recs, now)
	if err != nil {
		o.drop(ctx, h, now, err)

		return nil
	}

	return linked
}

// connect makes sure a home has a connection to its daemon, and reports whether it does.
// After a failure it waits Redial before it tries again.
func (o *Observer) connect(ctx context.Context, h *home, codexHome string, now time.Time) bool {
	if h.client != nil {
		select {
		case <-h.client.Done():
			o.drop(ctx, h, now, h.client.failure())
		default:
			return true
		}
	}

	if !h.dialed.IsZero() && now.Sub(h.dialed) < Redial {
		return false
	}

	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	client, err := o.cfg.Dialer.Dial(dialCtx, codexHome)
	if err != nil {
		h.dialed = now

		if err.Error() != h.failure {
			h.failure = err.Error()
			o.logger.WarnContext(ctx, "could not reach Codex's app server; following its sessions by their processes",
				"codexHome", codexHome, "error", err)
		}

		return false
	}

	h.client, h.dialed, h.failure, h.asked = client, time.Time{}, "", time.Time{}

	return true
}

// drop closes a home's connection after it failed, so the next Observe connects again.
func (o *Observer) drop(ctx context.Context, h *home, now time.Time, err error) {
	if ctx.Err() == nil {
		o.logger.WarnContext(ctx, "lost the connection to Codex's app server", "error", err)
	}

	h.close()
	h.dialed, h.failure = now, err.Error()
}

// due reports whether the daemon, or without it the rollout files, are to be asked about
// a home's sessions now.
func (h *home) due(recs []sessions.Record, now time.Time) bool {
	if h.client != nil {
		select {
		case <-h.client.Changed():
			return true
		default:
		}
	}

	return h.asked.IsZero() || h.ids != idsOf(recs) || now.Sub(h.asked) >= Poll
}

// ask links the sessions not yet linked, then reads every linked thread, and its latest
// turn unless it settled. It returns the threads it linked, by the app's ID.
func (o *Observer) ask(ctx context.Context, h *home, recs []sessions.Record, now time.Time) (map[string]string, error) {
	linked, err := o.link(ctx, h, recs)
	if err != nil {
		return nil, err
	}

	ids := threadIDs(recs, linked)

	for _, id := range ids {
		if err = o.read(ctx, h, id, now); err != nil {
			return linked, err
		}
	}

	for id := range h.threads {
		if !slices.Contains(ids, id) {
			delete(h.threads, id)
		}
	}

	return linked, nil
}

// threadIDs returns the thread of each session that has one: the one it was linked to
// before, or now.
func threadIDs(recs []sessions.Record, linked map[string]string) []string {
	var ids []string

	for _, r := range recs {
		id := cmp.Or(r.AgentSessionID, linked[r.ID])

		if id != "" {
			ids = append(ids, id)
		}
	}

	return ids
}

// link lists the threads in the folders of the sessions not yet linked, and links each
// to the thread it started. Codex lists a thread only by the exact folder it recorded,
// which has its symbolic links resolved, so each folder is asked for both as the launch
// named it and resolved.
func (o *Observer) link(ctx context.Context, h *home, recs []sessions.Record) (map[string]string, error) {
	var dirs []string

	taken := map[string]bool{}

	for _, r := range recs {
		if r.AgentSessionID == "" {
			dirs = append(dirs, r.Dir, samePath(r.Dir))
		} else {
			taken[r.AgentSessionID] = true
		}
	}

	if len(dirs) == 0 {
		return map[string]string{}, nil
	}

	slices.Sort(dirs)

	var listed struct {
		Data []Thread `json:"data"`
	}

	params := map[string]any{"cwd": slices.Compact(dirs), "limit": listLimit}
	if err := o.call(ctx, h, "thread/list", params, &listed); err != nil {
		return nil, err
	}

	return linkAll(listed.Data, recs, taken), nil
}

// linkRollouts links the sessions not yet linked through the rollout files Codex wrote
// since each of them launched, for when the daemon cannot be reached.
func (o *Observer) linkRollouts(h *home, recs []sessions.Record, now time.Time) map[string]string {
	var starts []time.Time

	taken := map[string]bool{}

	for _, r := range recs {
		if r.AgentSessionID != "" {
			taken[r.AgentSessionID] = true
		} else {
			starts = append(starts, r.LaunchedAt.Add(-linkSlack))
		}
	}

	if len(starts) == 0 {
		return nil
	}

	return linkAll(h.rollouts.threads(starts, now), recs, taken)
}

// read reads one thread, and its latest turn unless the turn read last settled.
func (o *Observer) read(ctx context.Context, h *home, id string, now time.Time) error {
	var got struct {
		Thread Thread `json:"thread"`
	}

	if err := o.call(ctx, h, "thread/read", map[string]any{"threadId": id}, &got); err != nil {
		if isRPCError(err) {
			return nil // a thread the daemon cannot read yet, such as one not saved
		}

		return err
	}

	t := h.threads[id]
	if t == nil {
		t = &thread{}
		h.threads[id] = t
	}

	t.status, t.updated = got.Thread.Status, got.Thread.UpdatedAt

	if t.settled() {
		return nil
	}

	var turns struct {
		Data []Turn `json:"data"`
	}

	err := o.call(ctx, h, "thread/turns/list", map[string]any{"threadId": id, "limit": 1, "itemsView": "full"}, &turns)

	switch {
	case isRPCError(err):
		return nil // no turn yet: Codex keeps a thread once its first message is sent
	case err != nil:
		return err
	case len(turns.Data) == 0:
		return nil
	}

	t.turnAt, t.turnRead = t.updated, now.Unix()
	t.turnState, t.turnDetail = TurnState(turns.Data[0])

	if msg := LastAgentMessage(turns.Data[0]); msg != "" {
		t.message = msg
	}

	return nil
}

// isRPCError reports whether err holds an error the daemon answered a call with.
func isRPCError(err error) bool {
	_, ok := errors.AsType[*RPCError](err)

	return ok
}

// call makes one call on a home's connection, bounded by callTimeout.
func (o *Observer) call(ctx context.Context, h *home, method string, params, result any) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	return h.client.Call(ctx, method, params, result)
}

// observe reports one session: by its process whether it runs, and by its thread what it
// does.
func (o *Observer) observe(
	ctx context.Context, h *home, r sessions.Record, linkedNow string, now time.Time, scan *processScan,
) sessions.Observation {
	obs := sessions.Observation{ID: r.ID, AgentSessionID: linkedNow}

	threadID := cmp.Or(r.AgentSessionID, linkedNow)

	t := h.threads[threadID]
	if t != nil {
		obs.LastMessage = t.message
	}

	running, pid := o.running(ctx, r, now, scan)
	obs.PID = pid

	switch {
	case running == processGone:
		obs.State = sessions.StateEnded
	case t == nil:
		obs.State, obs.Detail = o.unthreaded(h, running)
	default:
		obs.State, obs.Detail = t.state()
	}

	return obs
}

// unthreaded says what a running session whose thread is not known is doing: starting,
// while it may still start one, and unknown when nothing can tell.
func (o *Observer) unthreaded(h *home, running processState) (sessions.State, string) {
	switch {
	case running == processStarting:
		return sessions.StateStarting, ""
	case running == processUnknown || h.client == nil:
		return sessions.StateUnknown, o.unavailable(h)
	default:
		return sessions.StateStarting, ""
	}
}

// unavailable says why a session's state is unknown.
func (o *Observer) unavailable(h *home) string {
	if h.client == nil {
		return "Codex's app server isn't reachable"
	}

	return "Codex didn't say how this session is"
}

// OutsideServer is the detail of a session whose thread Codex runs outside its app
// server, as it does one started with config overrides, so its card never shows that it
// waits for the user.
const OutsideServer = "Runs outside Codex's app server, so it can't show when it waits for you"

// state is what a thread shows of its session: its live status when the daemon hosts it,
// and otherwise its latest turn, and that it shows no more. Without a turn read, nothing
// says what the session does.
func (t *thread) state() (sessions.State, string) {
	if state, detail, ok := MapStatus(t.status); ok {
		return state, detail
	}

	detail := cmp.Or(t.turnDetail, OutsideServer)

	if t.turnState == "" {
		return sessions.StateUnknown, detail
	}

	return t.turnState, detail
}

// processState is what is known of a session's process.
type processState int

const (
	processRunning processState = iota
	processStarting
	processGone
	processUnknown
)

// running says whether a session's process runs, finding it in the session's folder
// when it is not known yet, and returns it.
func (o *Observer) running(ctx context.Context, r sessions.Record, now time.Time, scan *processScan) (processState, int) {
	if r.PID > 0 {
		if !o.cfg.Alive(r.PID) {
			return processGone, r.PID
		}

		return o.recheck(ctx, r, now, scan)
	}

	pid, ok := scan.claim(ctx, r)

	switch {
	case !ok:
		return processUnknown, 0
	case pid > 0:
		return processRunning, pid
	case now.Sub(r.LaunchedAt) < Grace:
		return processStarting, 0
	default:
		return processGone, 0
	}
}

// recheck checks, every Recheck, that a session's running process is still Codex in the
// session's folder, as its process ID may have gone to another program, and returns it.
// Another Codex process in the folder is another session's, never this one's.
func (o *Observer) recheck(ctx context.Context, r sessions.Record, now time.Time, scan *processScan) (processState, int) {
	if at, ok := o.checked[r.ID]; ok && now.Sub(at) < Recheck {
		return processRunning, r.PID
	}

	pids, ok := scan.codexIn(ctx, r.Dir)
	if !ok {
		return processRunning, r.PID
	}

	if !slices.Contains(pids, r.PID) {
		delete(o.checked, r.ID)

		return processGone, r.PID
	}

	o.checked[r.ID] = now

	return processRunning, r.PID
}

// close closes a home's connection, if it has one.
func (h *home) close() {
	if h.client != nil {
		_ = h.client.Close()
		h.client = nil
	}

	clear(h.threads)
}

// linkAll links each session not yet linked to the thread it started, taking each
// thread once. A thread belongs to the latest launch in its folder that came before it,
// since any later launch came after it: so a launch whose own thread never came never
// takes a later launch's, nor one a linked session started. Each session then picks among
// its own threads as Link does.
func linkAll(threads []Thread, recs []sessions.Record, taken map[string]bool) map[string]string {
	dirs := make([]string, len(recs))
	for i, r := range recs {
		dirs[i] = samePath(r.Dir)
	}

	own := map[string][]Thread{}

	for _, t := range threads {
		if i, ok := launchBefore(t, recs, dirs); ok && recs[i].AgentSessionID == "" {
			own[recs[i].ID] = append(own[recs[i].ID], t)
		}
	}

	out := map[string]string{}

	for _, r := range recs {
		if r.AgentSessionID != "" {
			continue
		}

		if t, ok := Link(own[r.ID], r, taken); ok {
			taken[t.ID] = true
			out[r.ID] = t.ID
		}
	}

	return out
}

// launchBefore returns the index of the latest launch in recs whose folder, in dirs, is
// t's, and which came before t was created, within linkSlack.
func launchBefore(t Thread, recs []sessions.Record, dirs []string) (int, bool) {
	dir := samePath(t.Cwd)
	latest := -1

	for i, r := range recs {
		if dirs[i] != dir || t.CreatedAt < r.LaunchedAt.Add(-linkSlack).Unix() {
			continue
		}

		if latest < 0 || r.LaunchedAt.After(recs[latest].LaunchedAt) {
			latest = i
		}
	}

	return latest, latest >= 0
}

// processScan lists the running processes at most once per Observe, and knows which
// session each process found is.
type processScan struct {
	lister procs.Lister
	done   bool
	list   []procs.Process
	err    error

	// claimed holds the session each process is, by process ID.
	claimed map[int]string
}

// claims returns the session each known process is, by process ID.
func claims(live []sessions.Record) map[int]string {
	out := map[int]string{}

	for _, r := range live {
		if r.PID > 0 {
			out[r.PID] = r.ID
		}
	}

	return out
}

// codexIn returns the Codex processes working in dir itself, leaving out the daemon,
// which keeps the folder of the session that started it, and false when the processes
// cannot be listed.
func (s *processScan) codexIn(ctx context.Context, dir string) ([]int, bool) {
	if s.lister == nil {
		return nil, false
	}

	if !s.done {
		s.done = true

		list, err := s.lister.Processes(ctx)
		s.list, s.err = procs.WithoutServices(list, []string{codexInfo().Command}), err
	}

	if s.err != nil {
		return nil, false
	}

	var pids []int

	for _, p := range procs.In(s.list, dir) {
		if filepath.Base(p.Command) == codexInfo().Command {
			pids = append(pids, p.PID)
		}
	}

	return pids, true
}

// claim finds the process of a session whose process is not known yet: a Codex process
// in its folder that no other session is, which it then is. It returns 0 for none, and
// false when the processes cannot be listed.
func (s *processScan) claim(ctx context.Context, r sessions.Record) (int, bool) {
	pids, ok := s.codexIn(ctx, r.Dir)
	if !ok {
		return 0, false
	}

	for _, pid := range pids {
		if _, taken := s.claimed[pid]; !taken {
			s.claimed[pid] = r.ID

			return pid, true
		}
	}

	return 0, true
}

// codexInfo is Codex's spec.
func codexInfo() agent.Agent {
	a, _ := agent.Lookup(agent.Codex)

	return a
}

// idsOf names the sessions followed, in order.
func idsOf(recs []sessions.Record) string {
	ids := make([]string, 0, len(recs))
	for _, r := range recs {
		ids = append(ids, r.ID)
	}

	slices.Sort(ids)

	var out strings.Builder
	for _, id := range ids {
		out.WriteString(id)
		out.WriteByte(',')
	}

	return out.String()
}
