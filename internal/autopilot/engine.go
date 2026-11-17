package autopilot

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
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
//   - Autopilot (beta) in Settings, off on every install until the user turns it on,
//     says whether the engine runs at all (Enabled, which the loop follows on every wake,
//     follow). Off, it takes no lock, sends no routine search, runs no cycle, and refuses
//     every command; turned off, it starts nothing new from then on, kills nothing, and
//     lets go of the lock once nothing it started is still going; turned on, it starts as
//     a start does. Every start goes through dispatch, which asks Enabled once more right
//     before it hands the step over.

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

	// ActionsPerMinute and ActionsPerHour are the most GitHub actions Autopilot sends in a
	// minute and in an hour: a third of the write gate's edit lane, so the user's own
	// clicks always have room.
	ActionsPerMinute = 10
	ActionsPerHour   = 60
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

	// ErrOff is returned for what Autopilot won't do while it is off (Autopilot (beta) in
	// Settings): a command, a test of a routine, and a start that was on its way as it
	// was turned off.
	ErrOff = errors.New("autopilot is off")
)

// elsewhereError says another copy of the app holds the engine.
func elsewhereError() error {
	return uierr.New("Autopilot is running in another copy of "+brand.Name+".", ErrElsewhere)
}

// offError says Autopilot is off, and where to turn it on.
func offError() error {
	return uierr.New("Autopilot is off. Turn it on in Settings → Labs.", ErrOff)
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

	// LocalStates returns the local state of each stored item among ids (its triage on
	// this Mac, as the inbox encodes it), by id: what a triage step decides on.
	LocalStates(ctx context.Context, ids []int64) (map[int64]string, error)

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

	// Outside says where agents may run on outside contributors' items; nil allows them
	// nowhere.
	Outside OutsideRules

	// Problems tells the user of each problem once; nil tells no one, and they stay
	// recorded.
	Problems ProblemSink

	// NeedsYou tells the user, once, of each match that comes to wait for them (Needs
	// your OK); nil tells no one, and the matches still wait.
	NeedsYou NoticeSink

	// Finished tells the user that an agent's background run finished, or failed, as Agent
	// finished; nil tells no one.
	Finished NoticeSink

	// Views reads what the runner keeps of the agents' runs, for the window; RunLogs
	// clears their logs with Clear history. Either may be nil.
	Views   RunViews
	RunLogs RunLogs

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
	outside     OutsideRules
	problems    ProblemSink
	needsYou    NoticeSink
	finished    NoticeSink
	views       RunViews
	runLogs     RunLogs
	events      event.Sink
	clock       clock.Clock
	logger      *slog.Logger
	tick        time.Duration

	// since is when the engine was built: its first cycle waits for a refresh that
	// stored after it.
	since time.Time

	// on is Autopilot (beta) as the loop last read it (Enabled); onSince is when it was
	// last turned on, since when built on, whose first cycle waits for a watch pass after
	// it, as what was stored of the searches while it was off is stale; and draining
	// reports it turned off while this copy held the engine, which lets go of the lock once
	// nothing it started is still going.
	on       atomic.Bool
	onSince  time.Time
	draining bool

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

	// waits are the matches that came to wait for the user since the last cycle ended,
	// whose Needs your OK notices it tells.
	waits map[int64]bool
	// heldBack holds, by match, the start its runner last held back on something working
	// where it would start, which the match waits on for a tick before it is tried again
	// (holdBack): a session in the worktree never has the engine read the item from GitHub
	// on every cycle.
	heldBack map[int64]heldBack

	// writesHeld is when GitHub actions may go again after the write gate refused one,
	// with why: the app's own pace, the budget's reserve, or GitHub asking it to wait.
	writesHeld      time.Time
	writesHeldWords string

	// glanced is Autopilot at a glance as this copy last read it on a tick while another
	// copy held the engine, nil before it did (followElsewhere).
	glanced *Glance

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
		outside: cfg.Outside, problems: cfg.Problems, needsYou: cfg.NeedsYou, waits: map[int64]bool{}, finished: cfg.Finished,
		views: cfg.Views, runLogs: cfg.RunLogs,
		events: event.Or(cfg.Events), clock: svc.clock, logger: logging.Or(svc.logger), tick: tick,
		since: svc.clock.Now().Truncate(time.Second), wake: make(chan struct{}, 1), cmds: make(chan command),
		mail: mailbox{signal: make(chan struct{}, 1)}, closed: make(chan struct{}),
		inflight: map[int64]bool{}, freshReads: map[int64]fresh{}, stopping: map[int64]stopAsk{},
		stateReads: map[int64]time.Time{}, statesAsked: map[int64]bool{},
		checksRead: map[int64]time.Time{}, gone: map[int64]sighting{}, unsaved: map[int64]RunEnd{},
		heldBack: map[int64]heldBack{},
		ending:   map[int64]int{},
	}

	e.register(cfg)

	svc.engine = e
	e.onSince = e.since

	// Autopilot runs only while it is on: the engine starts from the switch as the Service
	// last read it, as core's first build of the searches' snapshot does before the engine
	// is built (off before any read), and its loop reads it again as it starts.
	e.on.Store(svc.seenOn.Load())

	// The lock is tried at once, so the copy that will run the engine sends the routines'
	// searches from its very first refresh, which comes before the engine's loop starts.
	if e.Enabled() {
		if _, err := lock.TryLock(); err != nil {
			e.logger.Warn("could not take Autopilot's lock", "error", err)
		}
	}

	return e
}

// Enabled reports whether Autopilot is on (Autopilot (beta) in Settings), as the engine
// last read it: off, it runs no cycle, applies no command, sends no routine search, and
// starts nothing. It is safe on any goroutine, and is the one check every start goes
// through (dispatch asks it right before a step is handed to its dispatcher, a run's
// included), so a part that starts anything of its own asks it too.
func (e *Engine) Enabled() bool {
	return e.on.Load()
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

		if err := e.ledger.handOff(ctx, o.Reversed, o.Kind, o.At, e.clock.Now()); err != nil {
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

	if e.Enabled() {
		e.takeLock(ctx)
	}

	for {
		e.follow(ctx)

		if e.Enabled() && e.holding() && !time.Now().Before(e.holdOff) {
			e.safeCycle(ctx)
		}

		if !e.await(ctx, tick.C) {
			return
		}
	}
}

// register sets up the engine's dispatchers: cfg's own, the runner for the agent steps,
// the engine's triage dispatcher over cfg.Triage, and its GitHub dispatcher over the
// Service's GitHubActions, each unless cfg names one of its own.
func (e *Engine) register(cfg EngineConfig) {
	if e.dispatchers == nil {
		e.dispatchers = map[catalog.Dispatcher]Dispatcher{}
	}

	if cfg.Runner != nil {
		e.dispatchers[catalog.DispatchAgent] = cfg.Runner
	}

	if _, own := e.dispatchers[catalog.DispatchTriage]; cfg.Triage != nil && !own {
		e.dispatchers[catalog.DispatchTriage] = triageDispatcher{e: e}
	}

	if _, own := e.dispatchers[catalog.DispatchGitHub]; e.svc.github != nil && !own {
		e.dispatchers[catalog.DispatchGitHub] = NewGitHub(e.svc.github)
	}
}

// follow reads whether Autopilot is on and turns the engine on or off to match, on the
// loop, as each wake brings it: Autopilot (beta) turned off, it starts nothing new from
// then on and sends no routine search, and lets go of the lock once nothing it started is
// still going (drain); turned on, it starts as a start does. Settings that can't be read
// leave it as it was, logged.
func (e *Engine) follow(ctx context.Context) {
	on, err := e.svc.enabled(ctx)
	if err != nil {
		if ctx.Err() == nil {
			e.logger.WarnContext(ctx, "could not read whether Autopilot is on", "error", err)
		}

		return
	}

	switch was := e.on.Swap(on); {
	case on && !was:
		e.turnOn(ctx)
	case !on && was:
		e.turnOff(ctx)
	}

	if !on {
		e.drain(ctx)
	}
}

// turnOn starts the engine as a start does, once Autopilot is turned on: it takes the
// lock, reading the engine's state and checking the runs going (takeLock), unless it
// still holds it from before it was turned off, and its first cycle waits for a watch pass
// from now on, since what was stored of the routines' searches while it was off is stale.
// The state's last refresh is the one the engine saw before it was off, so a gap longer
// than the catch-up setting begins a catch-up, as after a quit.
func (e *Engine) turnOn(ctx context.Context) {
	e.logger.InfoContext(ctx, "Autopilot was turned on")

	e.onSince, e.asked, e.primed = e.clock.Now().Truncate(time.Second), time.Time{}, false

	if e.draining {
		e.draining = false
		e.svc.rebuildWatches(ctx)
		e.notify()

		return
	}

	e.takeLock(ctx)
}

// turnOff stops the engine's starts once Autopilot is turned off: from then on it runs no
// cycle and applies no command, and the routines' searches are none, so the inbox sends
// them no more. It kills nothing; while this copy holds the engine, it drains.
func (e *Engine) turnOff(ctx context.Context) {
	e.logger.InfoContext(ctx, "Autopilot was turned off; it starts nothing new")

	e.draining = e.statusHolds() || e.lock.Held()
	if !e.draining {
		e.setHolds(false, false)
	}

	e.glanced = nil
	e.svc.rebuildWatches(ctx)
	e.notify()
}

// drain lets go of the lock while Autopilot is off, once nothing this copy started is
// still under way: no worker's result to come, no check of the runs going, no run's end
// left to write, and no run the ledger records as going (one whose end is on its way still
// is, until that end is applied), so what was under way as it was turned off is recorded as
// it ends, its runs too; the ticks settle those that ended unseen meanwhile.
func (e *Engine) drain(ctx context.Context) {
	if !e.draining || len(e.inflight) > 0 || e.checking || len(e.unsaved) > 0 {
		return
	}

	runs, err := e.ledger.q.ListActiveRuns(ctx)
	if err != nil {
		if ctx.Err() == nil {
			e.logger.WarnContext(ctx, "Autopilot could not read the runs going", "error", err)
		}

		return
	}

	if len(runs) > 0 {
		return
	}

	e.draining = false
	e.lock.Unlock()
	e.setHolds(false, false)
	e.logger.InfoContext(ctx, "Autopilot is off; this copy let go of its lock")
	e.notify()
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
		// Off, the engine takes no lock, and its ticks only settle what it started before,
		// while it drains.
		if e.Enabled() {
			e.takeLock(ctx)
		}

		if e.Enabled() || e.draining {
			e.onTick(ctx)
		}
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
// copy of the app, and checks the runs the ledger records as going. While another copy
// holds it, the tick follows what that copy changes (followElsewhere).
func (e *Engine) onTick(ctx context.Context) {
	if !e.holding() {
		e.followElsewhere(ctx)

		return
	}

	e.glanced = nil
	e.svc.rebuildWatches(ctx)
	e.retryEnds(ctx)
	e.unstrand(ctx)
	e.reconcile(ctx)
}

// followElsewhere reads Autopilot at a glance, on a tick while another copy of the app
// holds the engine, and tells the window and the shell (autopilot:updated) when it reads
// otherwise than on the tick before: this copy runs no cycle that would, so a Pause, a
// routine switched on or off, or an agent's run that the other copy changed reaches this
// copy's menu bar and its window's close within a tick. The first read only sets what the
// next is compared with.
func (e *Engine) followElsewhere(ctx context.Context) {
	g, err := e.svc.Glance(ctx)
	if err != nil {
		if ctx.Err() == nil {
			e.logger.DebugContext(ctx, "could not read Autopilot while another copy runs it", "error", err)
		}

		return
	}

	changed := e.glanced != nil && !sameGlance(*e.glanced, g)
	e.glanced = &g

	if changed {
		e.notify()
	}
}

// sameGlance reports whether two glances read alike.
func sameGlance(a, b Glance) bool {
	return a.On == b.On && a.Routines == b.Routines && a.Enabled == b.Enabled && a.Paused == b.Paused &&
		a.NeedsYou == b.NeedsYou &&
		slices.EqualFunc(a.Working, b.Working, func(x, y WorkingRun) bool {
			return x.RunID == y.RunID && x.CreatedAt.Equal(y.CreatedAt) && x.StartedAt.Equal(y.StartedAt)
		})
}

// HoldsWatches reports whether this copy of the app reads the routines' searches: only
// the copy that holds the engine's lock does, and none while Autopilot is off.
func (w *Searches) HoldsWatches() bool {
	e := w.svc.engine

	return e == nil || (e.Enabled() && e.lock.Held())
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

// safeCommand applies a command, refusing it while Autopilot is off, as it reads now, and
// while another copy holds the engine, and recovers a panic as an error.
func (e *Engine) safeCommand(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.ErrorContext(ctx, "an Autopilot command panicked", "panic", r)

			err = uierr.New("Autopilot couldn't do that.", errPanic)
		}
	}()

	e.follow(ctx)

	if !e.Enabled() {
		return offError()
	}

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
