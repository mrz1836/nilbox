package autopilot

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The engine runs the routines. It is a desktop worker with one goroutine of its own, the
// loop, which alone writes the ledger and starts anything:
//
//   - The loop wakes on the inbox storing items (ItemsChanged), a change of Autopilot's
//     settings or of a routine (Wake), what the inbox saw happen to an item (Outcomes),
//     its 1-minute tick (settle and wait timers, free slots, active hours, a new day, and
//     the lock), a command from the window, and a worker's result; what comes during a
//     cycle folds into one more.
//   - A cycle reads SQLite and evaluates, which takes milliseconds. Everything slow (an
//     item read fresh, check names, states, a start, a GitHub action) runs on a worker,
//     which hands its result back to the loop, so a command is applied within a fraction
//     of a second, mid-cycle or not.
//   - The window's actions are commands, applied on the loop, then answered; Stop and
//     Stop all go straight to the runner, never queued behind the loop, so they work
//     before the first refresh, offline, or signed out.
//   - Only the copy that holds the lock (Lock, a flock on <data>/autopilot.lock) runs
//     cycles and applies commands; another tries again on every tick, and takes over once
//     the holder lets go or its process ends.
//   - The first cycle after a start waits for a refresh that carried the routines'
//     searches, so the engine never acts on what the database held when the app was last
//     open; commands don't wait for it.

// Defaults of the engine's timing.
const (
	// DefaultTick is the loop's tick.
	DefaultTick = time.Minute

	// panicWait is how long the loop holds off cycles after one that panicked, so a panic
	// that recurs never spins.
	panicWait = time.Minute

	// notifyEvery is the least time between two autopilot:updated events.
	notifyEvery = 250 * time.Millisecond

	// pruneEvery is how often the retention runs.
	pruneEvery = time.Hour

	// stateReadFor is how long a state read of an item that left a watch stands, before
	// the item's match closes as having left it.
	stateReadFor = 30 * time.Minute

	// askAgainAfter is how long the engine waits before it asks again for a read that
	// brought nothing: check names, a search read at once.
	askAgainAfter = 5 * time.Minute

	// FirstCap is the most matches a routine with a GitHub action queues on its first
	// pass after it is turned on or changed; over it, it queues none, and they wait for the
	// user as a backlog.
	FirstCap = 25
)

var (
	// ErrElsewhere is returned for a command while another copy of the app holds the
	// engine.
	ErrElsewhere = errors.New("autopilot runs in another copy")

	// ErrStopped is returned for a command once the engine stopped.
	ErrStopped = errors.New("the engine stopped")

	// ErrNoRunner is returned for a Stop with no runner to stop.
	ErrNoRunner = errors.New("no runner")

	// ErrNotRunning is returned for a command before the engine's loop started.
	ErrNotRunning = errors.New("the engine isn't running yet")
)

// elsewhereError says another copy of the app holds the engine.
func elsewhereError() error {
	return uierr.New("Autopilot is running in another copy of "+brand.Name+".", ErrElsewhere)
}

// EngineItems is what the engine reads of the inbox, and asks it to read. Core adapts the
// inbox's store and service.
type EngineItems interface {
	// WatchedItems returns the stored items of the account signed in that the lists name,
	// or a search with one of keys returns, a stale row included.
	WatchedItems(ctx context.Context, keys []string, lists []catalog.ListSource) ([]int64, error)

	// Facts returns the facts of the stored items among ids, as of now.
	Facts(ctx context.Context, ids []int64, now time.Time) ([]facts.Facts, error)

	// ItemStates returns what is stored of each item among ids' state; an id with no
	// stored item is left out.
	ItemStates(ctx context.Context, ids []int64) (map[int64]ItemState, error)

	// InboxStatus returns the account signed in, the last successful refresh, and what the
	// last watch pass read of each search.
	InboxStatus(ctx context.Context) (InboxStatus, error)

	// ReadChecks reads and stores the check names of the pull requests among ids, and
	// returns what it spent.
	ReadChecks(ctx context.Context, ids []int64) (int, error)

	// ReadStates reads and stores the states of the items among ids, one request.
	ReadStates(ctx context.Context, ids []int64) error

	// ReadItem reads one item again from GitHub and stores it: the read right before a
	// start.
	ReadItem(ctx context.Context, id int64) error

	// WatchNow sends the routines' searches now, outside a whole refresh, and waits for
	// them to be stored.
	WatchNow(ctx context.Context) error
}

// ItemState is what is stored of an item's state.
type ItemState struct {
	// State is OPEN, CLOSED, or MERGED, "" before a read said.
	State string

	// FetchedAt is when a read last brought the item; Listed and Watched report it in an
	// inbox list, and returned by a routine's search.
	FetchedAt time.Time
	Listed    bool
	Watched   bool
}

// Closed reports an item merged or closed.
func (s ItemState) Closed() bool {
	return s.State == "CLOSED" || s.State == "MERGED"
}

// InboxStatus is what the engine reads of the inbox's last reads.
type InboxStatus struct {
	// Viewer is the account signed in, "" before one did.
	Viewer string

	// LastRefresh is when the last whole refresh stored, zero before one did.
	LastRefresh time.Time

	// WatchedAt is when the last watch pass stored, zero before one did; WatchPaused says
	// why the last refresh sent no searches.
	WatchedAt   time.Time
	WatchPaused string

	// Searches is each search's last answer, by key.
	Searches map[string]SearchState

	// SignedOut reports that the last refresh found no one signed in to GitHub.
	SignedOut bool
}

// SearchState is one search's last answer.
type SearchState struct {
	Answered  bool
	At        time.Time
	Error     string
	Truncated bool
	Partial   bool
}

// complete reports an answer that holds every item the search matches: answered, with no
// error, and neither truncated nor read in part. Only such an answer proves an item that
// isn't among its results left it.
func (s SearchState) complete() bool {
	return s.Answered && s.Error == "" && !s.Truncated && !s.Partial
}

// EngineConfig holds an Engine's dependencies beside its Service. Items is required; the
// rest may be nil.
type EngineConfig struct {
	// Items reads the inbox.
	Items EngineItems

	// Lock is the one-engine lock; nil always holds.
	Lock *Lock

	// Runner runs the agent steps, and Dispatchers the other parts' steps, by the
	// catalog's names; a step with none can't run.
	Runner      Runner
	Dispatchers map[catalog.Dispatcher]Dispatcher

	// Triage is the inbox's own triage, which runs the triage steps (Pin, Snooze,
	// Dismiss, Restore), each recorded as an effect, unless Dispatchers names a dispatcher
	// of its own for them, and through which Undo puts an item back; nil runs none.
	Triage Triage

	// Holds says what else holds a start back; nil holds nothing.
	Holds Holds

	// Problems tells the user of each problem once; nil tells no one, and they stay
	// recorded.
	Problems ProblemSink

	// Events hears event.AutopilotUpdated.
	Events event.Sink

	// Tick is the loop's tick; 0 is DefaultTick.
	Tick time.Duration
}

// Engine runs the routines (see the notes at the top of this file).
type Engine struct {
	svc         *Service
	items       EngineItems
	ledger      *ledger
	prefs       *prefs.Store
	lock        *Lock
	runner      Runner
	dispatchers map[catalog.Dispatcher]Dispatcher
	triage      Triage
	holds       Holds
	problems    ProblemSink
	events      event.Sink
	clock       clock.Clock
	logger      *slog.Logger
	tick        time.Duration

	// since is when the engine was built: its first cycle waits for a refresh that
	// stored after it.
	since time.Time

	wake chan struct{}
	cmds chan command
	mail mailbox

	// closed closes once Run has returned, which refuses later commands.
	closed chan struct{}
	runs   sync.Once

	// The loop's own state, which nothing else touches.
	state      engineState
	primed     bool
	asked      time.Time
	inflight   map[int64]bool
	freshReads map[int64]fresh
	stopping   map[int64]stopAsk
	stateReads map[int64]time.Time
	checksRead map[int64]time.Time
	holdOff    time.Time

	// statesAsked are the items whose states a worker reads now; runnerClosed reports a
	// runner that refused a start as it closed, so no agent starts until the next start;
	// and checking reports the runs going being checked now.
	statesAsked  map[int64]bool
	runnerClosed bool
	checking     bool

	// checks counts the checks of the runs going, and gone holds, by run, the check that
	// first found it gone or never started, which a later one must confirm; unsaved holds
	// the runs' ends that couldn't be written, tried again on the next tick.
	checks  int
	gone    map[int64]sighting
	unsaved map[int64]RunEnd

	// endsMu guards ending, the runs whose RunEnded is posted but not yet applied.
	endsMu sync.Mutex
	ending map[int64]int

	// mu guards what the window reads of the engine, and the notifications.
	mu         sync.Mutex
	status     EngineStatus
	lastNotify time.Time
	notifying  bool

	// work tracks the workers, which Run waits for before it returns.
	work sync.WaitGroup
}

// command is a window's action, applied on the loop.
type command struct {
	fn    func(ctx context.Context) error
	reply chan error
}

// mailbox holds the workers' results until the loop applies them, never blocking a
// worker, and its signal says one is waiting.
type mailbox struct {
	mu     sync.Mutex
	posts  []func(ctx context.Context)
	signal chan struct{}
}

// post hands fn to the loop.
func (m *mailbox) post(fn func(ctx context.Context)) {
	m.mu.Lock()
	m.posts = append(m.posts, fn)
	m.mu.Unlock()

	select {
	case m.signal <- struct{}{}:
	default:
	}
}

// take returns what is waiting, and forgets it.
func (m *mailbox) take() []func(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := m.posts
	m.posts = nil

	return out
}

// NewEngine returns the engine that runs svc's routines, and makes svc hand it the
// window's commands.
func NewEngine(svc *Service, cfg EngineConfig) *Engine {
	tick := cfg.Tick
	if tick <= 0 {
		tick = DefaultTick
	}

	lock := cfg.Lock
	if lock == nil {
		lock = NewLock("")
	}

	e := &Engine{
		svc: svc, items: cfg.Items, ledger: newLedger(svc.store.db, svc.clock), prefs: prefs.New(svc.store.db, svc.clock),
		lock: lock, runner: cfg.Runner, dispatchers: maps.Clone(cfg.Dispatchers), triage: cfg.Triage, holds: cfg.Holds,
		problems: cfg.Problems,
		events:   event.Or(cfg.Events), clock: svc.clock, logger: logging.Or(svc.logger), tick: tick,
		since: svc.clock.Now().Truncate(time.Second), wake: make(chan struct{}, 1), cmds: make(chan command),
		mail: mailbox{signal: make(chan struct{}, 1)}, closed: make(chan struct{}),
		inflight: map[int64]bool{}, freshReads: map[int64]fresh{}, stopping: map[int64]stopAsk{},
		stateReads: map[int64]time.Time{}, statesAsked: map[int64]bool{},
		checksRead: map[int64]time.Time{}, gone: map[int64]sighting{}, unsaved: map[int64]RunEnd{},
		ending: map[int64]int{},
	}

	if e.dispatchers == nil {
		e.dispatchers = map[catalog.Dispatcher]Dispatcher{}
	}

	if cfg.Runner != nil {
		e.dispatchers[catalog.DispatchAgent] = cfg.Runner
	}

	if _, own := e.dispatchers[catalog.DispatchTriage]; cfg.Triage != nil && !own {
		e.dispatchers[catalog.DispatchTriage] = triageDispatcher{e: e}
	}

	svc.engine = e

	// The lock is tried at once, so the copy that will run the engine sends the routines'
	// searches from its very first refresh, which comes before the engine's loop starts.
	if _, err := lock.TryLock(); err != nil {
		e.logger.Warn("could not take Autopilot's lock", "error", err)
	}

	return e
}

// Wake has the engine run a cycle soon: a setting or a routine changed.
func (e *Engine) Wake() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// ItemsChanged hears that the inbox stored items outside a whole refresh, and runs a cycle
// soon.
func (e *Engine) ItemsChanged([]int64) {
	e.Wake()
}

// Outcomes hears what the inbox saw happen to items, and runs a cycle soon, which reads
// their states again. What the user's own triage took back of a routine's (an outcome's
// Reversed) leaves that routine hands off the item at once, in whichever copy of the app
// the user did it: the routine leaves the item alone from then on, until Let it act again.
// A record that fails is logged.
func (e *Engine) Outcomes(ctx context.Context, outcomes []Outcome) {
	for _, o := range outcomes {
		if o.Reversed.RoutineID == 0 || o.Reversed.ItemID == 0 {
			continue
		}

		if err := e.ledger.handOff(ctx, o.Reversed, o.Kind, e.clock.Now()); err != nil {
			e.logger.WarnContext(ctx, "Autopilot could not record what the user took back of a routine's",
				"routine", o.Reversed.RoutineID, "item", o.Reversed.ItemID, "error", err)
		}
	}

	e.Wake()
}

// RunEnded hears how a run ended, from its runner, on any goroutine, without waiting.
// From the moment it is called, the run counts as ending, so no check of the runs going
// takes it for one that ended unseen.
func (e *Engine) RunEnded(end RunEnd) {
	e.endsMu.Lock()
	e.ending[end.RunID]++
	e.endsMu.Unlock()

	e.mail.post(func(ctx context.Context) {
		e.applyRunEnd(ctx, end)

		e.endsMu.Lock()
		defer e.endsMu.Unlock()

		if e.ending[end.RunID]--; e.ending[end.RunID] <= 0 {
			delete(e.ending, end.RunID)
		}
	})
}

// KeptItems returns every item a match or a run refers to, which the inbox's prune keeps.
func (e *Engine) KeptItems(ctx context.Context) ([]int64, error) {
	refs, err := e.ledger.q.ListLedgerItems(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]int64, 0, len(refs))
	for _, r := range refs {
		if r != nil {
			out = append(out, *r)
		}
	}

	return out, nil
}

// Run runs the loop until ctx ends, then lets go of the lock and returns once the workers
// have. It runs once; a later call returns at once.
func (e *Engine) Run(ctx context.Context) {
	first := false

	e.runs.Do(func() { first = true })

	if !first {
		return
	}

	defer close(e.closed)
	defer e.lock.Unlock()
	defer e.work.Wait()

	e.setStatus(func(s *EngineStatus) { s.Running = true })
	defer e.setStatus(func(s *EngineStatus) { s.Running, s.Holds = false, false })

	tick := time.NewTicker(e.tick)
	defer tick.Stop()

	e.takeLock(ctx)

	for {
		if e.holding() && !time.Now().Before(e.holdOff) {
			e.safeCycle(ctx)
		}

		if !e.await(ctx, tick.C) {
			return
		}
	}
}

// endPending reports a run whose RunEnded hasn't been applied yet.
func (e *Engine) endPending(runID int64) bool {
	e.endsMu.Lock()
	defer e.endsMu.Unlock()

	return e.ending[runID] > 0
}

// await waits for what wakes the loop, applies a command or the workers' results, and
// reports false once ctx ends. Whatever woke it, a cycle follows, and what comes while
// the cycle runs folds into the next.
func (e *Engine) await(ctx context.Context, tick <-chan time.Time) bool {
	select {
	case <-ctx.Done():
		return false
	case <-e.wake:
	case <-tick:
		e.takeLock(ctx)
		e.onTick(ctx)
	case c := <-e.cmds:
		c.reply <- e.safeCommand(ctx, c.fn)
	case <-e.mail.signal:
		for _, fn := range e.mail.take() {
			e.safeApply(ctx, fn)
		}
	}

	return true
}

// onTick does what each tick does while this copy holds the engine: it builds the
// routines' searches again from the database, which catches a routine changed in another
// copy of the app, and checks the runs the ledger records as going.
func (e *Engine) onTick(ctx context.Context) {
	if !e.holding() {
		return
	}

	e.svc.rebuildWatches(ctx)
	e.retryEnds(ctx)
	e.unstrand(ctx)
	e.reconcile(ctx)
}

// HoldsWatches reports whether this copy of the app reads the routines' searches: only
// the copy that holds the engine's lock does.
func (w *Searches) HoldsWatches() bool {
	e := w.svc.engine

	return e == nil || e.lock.Held()
}

// holding reports whether this copy holds the engine.
func (e *Engine) holding() bool {
	return e.lock.Held()
}

// takeLock tries the lock, if not held, and on taking it reads the engine's state and
// checks the runs it records as going.
func (e *Engine) takeLock(ctx context.Context) {
	if e.lock.Held() && e.statusHolds() {
		return
	}

	held, err := e.lock.TryLock()
	if err != nil {
		e.logger.WarnContext(ctx, "could not take Autopilot's lock", "error", err)
	}

	if !held {
		if e.setHolds(false, true) {
			e.notify()
		}

		return
	}

	st, err := readEngineState(ctx, e.prefs)
	if err != nil {
		e.logger.WarnContext(ctx, "could not read Autopilot's engine state", "error", err)
	}

	e.state = st
	e.primed = false
	e.setHolds(true, false)
	e.svc.rebuildWatches(ctx)
	e.logger.InfoContext(ctx, "this copy runs Autopilot")
	e.reconcile(ctx)
	e.notify()
}

// statusHolds reports whether the status says this copy holds the engine.
func (e *Engine) statusHolds() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.status.Holds
}

// setHolds records whether this copy holds the engine, and reports a change.
func (e *Engine) setHolds(holds, elsewhere bool) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	was := e.status.Holds != holds || e.status.Elsewhere != elsewhere
	e.status.Holds, e.status.Elsewhere = holds, elsewhere

	return was
}

// isRunning reports whether the loop runs.
func (e *Engine) isRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.status.Running
}

// setStatus changes the status under its lock.
func (e *Engine) setStatus(fn func(s *EngineStatus)) {
	e.mu.Lock()
	defer e.mu.Unlock()

	fn(&e.status)
}

// do hands fn to the loop and waits for it to be applied, or for ctx to end. A command
// is refused while another copy holds the engine, and once the engine stopped.
func (e *Engine) do(ctx context.Context, fn func(ctx context.Context) error) error {
	c := command{fn: fn, reply: make(chan error, 1)}

	if !e.isRunning() {
		return uierr.New("Autopilot is starting; try again in a moment.", ErrNotRunning)
	}

	select {
	case e.cmds <- c:
	case <-ctx.Done():
		return ctx.Err()
	case <-e.closed:
		return uierr.New("Autopilot has stopped.", ErrStopped)
	}

	select {
	case err := <-c.reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// safeCommand applies a command, refusing it while another copy holds the engine, and
// recovers a panic as an error.
func (e *Engine) safeCommand(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.ErrorContext(ctx, "an Autopilot command panicked", "panic", r)

			err = uierr.New("Autopilot couldn't do that.", errPanic)
		}
	}()

	if !e.holding() {
		return elsewhereError()
	}

	err = fn(ctx)

	e.notify()

	return err
}

// errPanic is a command that panicked.
var errPanic = errors.New("the command panicked")

// safeApply applies a worker's result, recovering a panic.
func (e *Engine) safeApply(ctx context.Context, fn func(ctx context.Context)) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.ErrorContext(ctx, "applying an Autopilot result panicked", "panic", r)
		}
	}()

	if e.holding() {
		fn(ctx)
	}
}

// safeCycle runs a cycle, recovering a panic, after which the loop holds off cycles for
// panicWait.
func (e *Engine) safeCycle(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.ErrorContext(ctx, "an Autopilot cycle panicked", "panic", r)

			e.holdOff = time.Now().Add(panicWait)
		}
	}()

	e.cycle(ctx)
}

// goWork runs fn on a worker, tracked so Run waits for it, recovering a panic.
func (e *Engine) goWork(ctx context.Context, fn func(ctx context.Context)) {
	e.work.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				e.logger.ErrorContext(ctx, "an Autopilot worker panicked", "panic", r)
			}
		}()

		fn(ctx)
	})
}

// notify tells the window Autopilot changed, at most every notifyEvery: a call that comes
// sooner is folded into one sent once that time has passed.
func (e *Engine) notify() {
	e.mu.Lock()

	if e.notifying {
		e.mu.Unlock()

		return
	}

	wait := time.Until(e.lastNotify.Add(notifyEvery))
	if wait <= 0 {
		e.lastNotify = time.Now()
		e.mu.Unlock()
		e.events.Emit(event.AutopilotUpdated{})

		return
	}

	e.notifying = true
	e.mu.Unlock()

	time.AfterFunc(wait, func() {
		e.mu.Lock()
		e.notifying, e.lastNotify = false, time.Now()
		e.mu.Unlock()
		e.events.Emit(event.AutopilotUpdated{})
	})
}

// saveState keeps the engine's state, logging a failure: the next cycle saves it again.
func (e *Engine) saveState(ctx context.Context) {
	if err := writeEngineState(ctx, e.prefs, e.state); err != nil {
		e.logger.WarnContext(ctx, "could not save Autopilot's engine state", "error", err)
	}
}
