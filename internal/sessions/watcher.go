package sessions

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/logging"
)

// The Watcher's pace.
const (
	// DefaultTick is how often the Watcher asks the observers while a session runs. Each
	// observer decides itself how often it asks its agent; most ticks cost a few file
	// stats.
	DefaultTick = time.Second

	// DefaultDebounce is how long a session must wait for the user before the
	// AttentionSink hears of it, so a prompt answered at once never shows a banner.
	DefaultDebounce = 5 * time.Second

	// offRecheck is how often the Watcher reads the master switch again while it is off
	// and some session has not ended.
	offRecheck = 10 * time.Second

	// pruneEvery is how often the Watcher forgets old sessions.
	pruneEvery = 6 * time.Hour

	// ReviveWindow is how long after it ended a session is watched for being resumed
	// outside the app, as `claude --resume <id>` in a terminal resumes it.
	ReviveWindow = 24 * time.Hour

	// ReviveTick is how often the Watcher looks for a resumed session while none runs.
	ReviveTick = 20 * time.Second
)

// Observer reports what one agent's sessions are doing, through the agent's own
// supported interface.
type Observer interface {
	// Observe reports what each of live, the agent's sessions that have not ended, is
	// doing as of now, with an observation for every one it has news of. The Watcher
	// calls it about once a Tick while any runs; the observer decides how often it asks
	// the agent, and answers from what it learned last in between. It never fails: an
	// agent it cannot ask makes the sessions StateUnknown, or StateEnded once their
	// process is gone.
	Observe(ctx context.Context, now time.Time, live []Record) []Observation

	// Close releases what the observer keeps between calls, such as a connection. The
	// next Observe starts again.
	Close()
}

// Switch reads whether the user follows sessions. The settings service implements it.
type Switch interface {
	FollowSessions(ctx context.Context) (bool, error)
}

// AttentionSink hears the moments a session needs the user: it has waited for them a
// while, or it finished a turn. The notify package's banners implement it.
type AttentionSink interface {
	// NeedsYou says a session has waited for the user for the debounce.
	NeedsYou(ctx context.Context, s Session)

	// Finished says a session finished a turn it was working on.
	Finished(ctx context.Context, s Session)
}

// WatcherConfig holds a Watcher's dependencies. Store is required.
type WatcherConfig struct {
	Store *Store

	// Observers holds each agent's Observer. An agent without one has its sessions left
	// as they are.
	Observers map[agent.ID]Observer

	// Switch is the user's master switch; without it, sessions are always followed.
	Switch Switch

	// Optional: the moments a session needs the user are handed to Attention.
	Attention AttentionSink

	// Events hears an event.SessionsUpdated whenever a session changes.
	Events event.Sink

	// Clock stamps every change; it defaults to the system clock.
	Clock clock.Clock

	// Home shows home as ~ in the sessions the AttentionSink hears of.
	Home string

	Logger *slog.Logger

	// Tick and Debounce default to DefaultTick and DefaultDebounce.
	Tick     time.Duration
	Debounce time.Duration

	// NewID mints the ID of a session resumed outside the app; it defaults to a random
	// UUID.
	NewID func() string
}

// Watcher reconciles what the agents report into the ledger: a desktop.Worker that runs
// while the app is open. It observes only while some session has not ended, reconciles
// every one when it starts, and wakes at once when a launch records a new one.
type Watcher struct {
	store     *Store
	observers map[agent.ID]Observer
	toggle    Switch
	attention AttentionSink
	events    event.Sink
	clock     clock.Clock
	home      string
	logger    *slog.Logger
	tick      time.Duration
	debounce  time.Duration
	newID     func() string

	wake chan struct{}

	// mu guards the fields below, which Run alone writes.
	mu sync.Mutex

	// waiting holds when each session came to wait for the user, until the
	// AttentionSink hears of it.
	waiting map[string]time.Time

	// known holds the last state each running session reported that was not
	// StateUnknown, so a moment its agent could not be asked is not taken for a change.
	known map[string]State

	// observing reports whether the observers were asked since they were last closed.
	observing bool

	// pruned is when old sessions were last forgotten.
	pruned time.Time
}

// NewWatcher returns a Watcher over cfg that has not started.
func NewWatcher(cfg WatcherConfig) *Watcher {
	w := &Watcher{
		store: cfg.Store, observers: cfg.Observers, toggle: cfg.Switch, attention: cfg.Attention,
		events: event.Or(cfg.Events), clock: clock.Or(cfg.Clock), home: cfg.Home, logger: logging.Or(cfg.Logger),
		tick: cfg.Tick, debounce: cfg.Debounce, newID: cfg.NewID, wake: make(chan struct{}, 1),
		waiting: map[string]time.Time{}, known: map[string]State{},
	}

	if w.newID == nil {
		w.newID = uuid.NewString
	}

	if w.tick <= 0 {
		w.tick = DefaultTick
	}

	if w.debounce <= 0 {
		w.debounce = DefaultDebounce
	}

	return w
}

// Wake asks the Watcher to reconcile now, as a launch does once it records a session. It
// never waits.
func (w *Watcher) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run reconciles until ctx ends: at once, then once a Tick while a session runs, or once
// woken while none does. It closes the observers before it returns.
func (w *Watcher) Run(ctx context.Context) {
	defer w.closeObservers()

	for {
		if !w.wait(ctx, w.Reconcile(ctx)) {
			return
		}
	}
}

// Reconcile asks the observers about every session that has not ended, stores what
// changed, tells the window, and hands the AttentionSink the sessions that need the user.
// It returns how long to wait before the next, or 0 to wait to be woken.
func (w *Watcher) Reconcile(ctx context.Context) time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := w.clock.Now()
	w.prune(ctx, now)

	live, err := w.store.Live(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.WarnContext(ctx, "could not read the sessions to follow", "error", err)
		}

		return w.tick
	}

	ended := w.resumable(ctx, now, live)
	if wait, idle := w.idle(ctx, live, ended); idle {
		return wait
	}

	observed, revived := w.revive(ctx, now, ended, w.observe(ctx, now, append(slices.Clone(live), ended...)))

	changes, err := w.store.Apply(ctx, now, observed)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.WarnContext(ctx, "could not store what the sessions reported", "error", err)
		}

		return w.tick
	}

	return w.settle(ctx, now, changes, revived, len(live) > 0)
}

// settle tells the window what changed, hands the AttentionSink what needs the user, and
// returns how long to wait: a tick while a session runs, else ReviveTick.
func (w *Watcher) settle(ctx context.Context, now time.Time, changes []Change, revived, running bool) time.Duration {
	if len(changes) > 0 || revived {
		w.events.Emit(event.SessionsUpdated{})
	}

	w.heed(ctx, now, changes)

	if !running && !revived {
		return ReviveTick
	}

	return w.tick
}

// idle reports, with how long to wait, when there is nothing to ask the agents: no
// session runs nor could be resumed, or the user follows none. The observers then let go
// of what they keep.
func (w *Watcher) idle(ctx context.Context, live, ended []Record) (time.Duration, bool) {
	wait := time.Duration(0)

	switch {
	case len(live) == 0 && len(ended) == 0:
	case !w.following(ctx):
		wait = offRecheck
	default:
		return 0, false
	}

	w.closeObserversLocked()
	clear(w.waiting)
	clear(w.known)

	return wait, true
}

// resumable returns the sessions that ended in the last ReviveWindow, however long ago
// they were launched, and could be resumed outside the app under the same ID, as Claude
// Code's can: the last of each ID to end, leaving out an ID a running session already has.
func (w *Watcher) resumable(ctx context.Context, now time.Time, live []Record) []Record {
	ended, err := w.store.EndedSince(ctx, now.Add(-ReviveWindow))
	if err != nil {
		return nil
	}

	seen := map[string]bool{}
	for _, r := range live {
		seen[r.AgentSessionID] = true
	}

	var out []Record

	for _, r := range ended {
		a, err := agent.Lookup(r.Agent)
		if r.AgentSessionID == "" || seen[r.AgentSessionID] || err != nil || !a.SessionIDs {
			continue
		}

		seen[r.AgentSessionID] = true
		out = append(out, r)
	}

	return out
}

// revive records, as a new session resumed from it, each ended session its agent listed
// as running again, and returns the observations to store: the running sessions', and
// the revived ones' under their new IDs.
func (w *Watcher) revive(ctx context.Context, now time.Time, ended []Record, observed []Observation) ([]Observation, bool) {
	byID := make(map[string]Record, len(ended))
	for _, r := range ended {
		byID[r.ID] = r
	}

	out := make([]Observation, 0, len(observed))
	revived := false

	for _, o := range observed {
		r, wasEnded := byID[o.ID]
		if !wasEnded {
			out = append(out, o)

			continue
		}

		if id, ok := w.reviveOne(ctx, now, r, o); ok {
			o.ID, revived = id, true
			out = append(out, o)
		}
	}

	return out, revived
}

// reviveOne records ended, which its agent listed as o, as a new session resumed from it,
// when o shows it running again, and returns the new session's ID.
func (w *Watcher) reviveOne(ctx context.Context, now time.Time, ended Record, o Observation) (string, bool) {
	if !o.Seen || !o.State.Live() {
		return "", false
	}

	r := ended
	r.ID, r.ResumedFrom, r.Prompt, r.PromptHead, r.PID = w.newID(), ended.ID, "", "", 0
	r.State, r.Detail, r.LastMessage, r.LaunchedAt = StateStarting, "", "", now

	// A launch that resumed the session from the app may have recorded it since the
	// running sessions were read; then that record follows it, not a second one.
	inserted, err := w.store.InsertUnlessLive(ctx, r)
	if err != nil {
		w.logger.WarnContext(ctx, "could not follow a session resumed outside the app", "error", err)
	}

	return r.ID, err == nil && inserted
}

// wait waits until next has passed, or until woken when next is 0, and reports false once
// ctx has ended instead.
func (w *Watcher) wait(ctx context.Context, next time.Duration) bool {
	var due <-chan time.Time

	if next > 0 {
		timer := time.NewTimer(next)
		defer timer.Stop()

		due = timer.C
	}

	select {
	case <-ctx.Done():
		return false
	case <-w.wake:
		return true
	case <-due:
		return true
	}
}

// following reads the master switch; a switch that cannot be read counts as on.
func (w *Watcher) following(ctx context.Context) bool {
	if w.toggle == nil {
		return true
	}

	on, err := w.toggle.FollowSessions(ctx)
	if err != nil {
		w.logger.WarnContext(ctx, "could not read whether to follow sessions; following them", "error", err)

		return true
	}

	return on
}

// observe asks each agent's observer about its sessions, and every other observer with
// none, so it lets go of what it keeps.
func (w *Watcher) observe(ctx context.Context, now time.Time, live []Record) []Observation {
	byAgent := map[agent.ID][]Record{}
	for _, r := range live {
		byAgent[r.Agent] = append(byAgent[r.Agent], r)
	}

	var out []Observation

	for _, id := range sortedAgents(w.observers) {
		obs := w.observers[id]
		if obs == nil {
			continue
		}

		if len(byAgent[id]) == 0 {
			obs.Close()

			continue
		}

		out = append(out, obs.Observe(ctx, now, byAgent[id])...)
	}

	w.observing = true

	return out
}

// heed hands the AttentionSink what changed: a session waiting for the user once it has
// waited the debounce, and a turn finished at once.
func (w *Watcher) heed(ctx context.Context, now time.Time, changes []Change) {
	for _, c := range changes {
		w.heedOne(ctx, now, c)
	}

	w.remind(ctx, now)
}

// heedOne takes in one change. A session whose agent could not be asked for a moment,
// StateUnknown, changes nothing: what it reports next is compared with what it reported
// before, so a prompt it still waits on is not announced twice, and a turn that finished
// meanwhile is still announced.
func (w *Watcher) heedOne(ctx context.Context, now time.Time, c Change) {
	id, after := c.After.ID, c.After.State
	if !c.StateChanged() || after == StateUnknown {
		return
	}

	before, ok := w.known[id]
	if !ok {
		before = c.Before.State
	}

	if after.Live() {
		w.known[id] = after
	} else {
		delete(w.known, id)
	}

	switch {
	case before == after:
		return
	case after.NeedsYou():
		w.waiting[id] = now
	default:
		delete(w.waiting, id)
	}

	if w.attention != nil && before == StateWorking && after == StateIdle {
		w.attention.Finished(ctx, c.After.Session(w.home))
	}
}

// remind hands the AttentionSink each session that has waited for the user the debounce,
// once. One whose agent cannot be asked just now stays waiting until it can.
func (w *Watcher) remind(ctx context.Context, now time.Time) {
	for id, since := range w.waiting {
		if now.Sub(since) < w.debounce {
			continue
		}

		r, err := w.store.Get(ctx, id)
		if err == nil && r.State == StateUnknown {
			continue
		}

		delete(w.waiting, id)

		if w.attention == nil || err != nil || !r.State.NeedsYou() {
			continue
		}

		w.attention.NeedsYou(ctx, r.Session(w.home))
	}
}

// prune forgets old sessions, at most once every pruneEvery.
func (w *Watcher) prune(ctx context.Context, now time.Time) {
	if !w.pruned.IsZero() && now.Sub(w.pruned) < pruneEvery {
		return
	}

	w.pruned = now

	forgot, err := w.store.Prune(ctx, now)

	switch {
	case err != nil && ctx.Err() == nil:
		w.logger.WarnContext(ctx, "could not forget old sessions", "error", err)
	case forgot > 0:
		w.logger.DebugContext(ctx, "forgot old sessions", "count", forgot)
	}
}

// closeObservers closes every observer, as Run returns.
func (w *Watcher) closeObservers() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.closeObserversLocked()
}

// closeObserversLocked closes every observer, once since they were last asked.
func (w *Watcher) closeObserversLocked() {
	if !w.observing {
		return
	}

	for _, obs := range w.observers {
		if obs != nil {
			obs.Close()
		}
	}

	w.observing = false
}

// sortedAgents returns the agents of a per-agent map in a stable order.
func sortedAgents[V any](m map[agent.ID]V) []agent.ID {
	out := make([]agent.ID, 0, len(m))
	for id := range m {
		out = append(out, id)
	}

	slices.Sort(out)

	return out
}
