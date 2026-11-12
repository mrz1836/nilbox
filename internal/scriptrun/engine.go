package scriptrun

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun/internal/queries"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

const (
	// Slots is how many runs go in the app at once, across every clone.
	Slots = 4

	// KeepRuns is how many runs the history keeps, and KeepLogBytes how much log in all;
	// the oldest finished runs go first, and a run still going is never dropped.
	KeepRuns     = 200
	KeepLogBytes = 512 << 20

	// A finished log over CompactOver bytes keeps its first CompactHead bytes and its last
	// CompactTail.
	CompactOver = 16 << 20
	CompactHead = 4 << 20
	CompactTail = 12 << 20

	// QuitKillAfter is how long a run in the app has, after SIGTERM as the app quits,
	// before it gets SIGKILL. It fits inside the desktop shell's stop timeout.
	QuitKillAfter = 3 * time.Second

	// tailEvery is how often a live run's log is read and its end looked for.
	tailEvery = 100 * time.Millisecond

	// quitGrace is how long, after SIGKILL as the app quits, a run is waited for.
	quitGrace = time.Second

	// pidWait is how long a run in the terminal has to record its shell's PID before it
	// counts as never started: the terminal may first ask the user to allow the app to
	// control it.
	pidWait = 2 * time.Minute

	// startSlack is how far apart two readings of one process's start time may be: Linux
	// moves its answer when the wall clock is stepped.
	startSlack = 2 * time.Second
)

// errNoGroup is returned for a process group that can't be a run's.
var errNoGroup = errors.New("not a run's process group")

// Config holds an Engine's dependencies. DB and LogDir are required.
type Config struct {
	// DB holds script_runs.
	DB *sql.DB

	// Locks is the app's one set of clone locks; nil gives the engine a set of its own.
	Locks *clonelock.Set

	// LogDir is the absolute folder the runs' logs go in: <data>/script-runs.
	LogDir string

	// Home is the user's home folder, for ~ in the folders the window shows and in a run's
	// command, which records its program's absolute path all the same.
	Home string

	// Clock stamps the runs. It defaults to the system clock.
	Clock clock.Clock

	// Logger gets one record per run that ends.
	Logger *slog.Logger

	// Changed is called whenever a run starts, ends, or changes status, or the history
	// changes, never for output. Core emits event.ScriptRunsUpdated from it. It must
	// return promptly and never call back into the engine.
	Changed func()

	// Progressed is called with a run's ID whenever its Progress changes: it reached a
	// step, or Stop turned on or off. Nothing else about the runs changed, so core emits
	// event.ScriptRunProgress from it, never ScriptRunsUpdated. It has Changed's rules, and
	// may be nil.
	Progressed func(id int64)

	// Failed is told of each run whose end is recorded as failed, one stopped at its time
	// limit included, once that end is kept: every kind, in the app or in the terminal,
	// started now or settled after a restart, as its kind's Verifier works it out or as the
	// engine reads how it ended. A run the user stopped, one the quit stopped or a restart
	// interrupted, one that succeeded, and one left Unsettled tell nothing, and no run tells
	// twice: only the record that marks it ended tells, and a record that fails tells nothing
	// until the next start settles the run. Core tells the user (notify's run_failed). It
	// runs on the goroutine that recorded the run, after its Spec's Recorded, must return
	// promptly, and must never call back into the engine. nil tells nothing.
	Failed func(ctx context.Context, f Failure)

	// Terminal opens runs in the user's terminal, and the terminal in a run's folder.
	// Without it, runs there fail to start, and Open in terminal fails.
	Terminal TerminalOpener

	// Reveal shows a run's log file in Finder. Without it, Show the log file fails.
	Reveal Revealer

	// Quit is what the Service's QuitAnyway calls: the user chose to stop the runs and
	// quit.
	Quit func()
}

// Engine starts runs and follows them to their end. It is safe for concurrent use. Run
// is its desktop worker, and the Service its bound face; the Engine itself is not bound,
// since a Spec holds functions.
type Engine struct {
	store      runStore
	locks      *clonelock.Set
	files      logFiles
	home       string
	clock      clock.Clock
	logger     *slog.Logger
	changed    func()
	progressed func(id int64)
	failed     func(ctx context.Context, f Failure)
	terminal   TerminalOpener
	reveal     Revealer
	quit       func()

	// slots holds a token per run going in the app.
	slots chan struct{}

	// The retention, the compaction, and the times, which tests shorten.
	keep       retention
	compaction compaction
	tailEvery  time.Duration
	quitKill   time.Duration
	quitGrace  time.Duration
	pidWait    time.Duration
	ladder     execpath.Ladder

	// What tells a process or a group is alive, when a process started, and how a group
	// is signaled; tests replace them.
	alive      func(pid int) bool
	groupAlive func(pgid int) bool
	startedAt  func(pid int) time.Time
	signal     func(pgid int, sig syscall.Signal) error

	// regMu orders a start's recording and registration of its run against reconcile's
	// reading of the runs left behind and StopAll's closing: a start holds it to read,
	// they hold it to write.
	regMu sync.RWMutex

	// mu guards live, rerunners, verifiers, and closed.
	mu        sync.Mutex
	live      map[int64]*live
	rerunners map[Kind]Rerunner
	verifiers map[Kind]Verifier
	closed    bool

	// closing is closed once StopAll begins.
	closing chan struct{}

	// pruneMu runs one retention or history clearing at a time.
	pruneMu sync.Mutex

	// wg tracks the goroutines that follow the runs.
	wg sync.WaitGroup
}

// NewEngine returns an Engine over cfg.
func NewEngine(cfg Config) *Engine {
	locks := cfg.Locks
	if locks == nil {
		locks = clonelock.New()
	}

	return &Engine{
		store: runStore{db: cfg.DB, q: queries.New(cfg.DB)}, locks: locks, files: logFiles{dir: cfg.LogDir}, home: cfg.Home,
		clock: clock.Or(cfg.Clock), logger: logging.Or(cfg.Logger), changed: cfg.Changed, progressed: cfg.Progressed,
		failed: cfg.Failed, terminal: cfg.Terminal, reveal: cfg.Reveal, quit: cfg.Quit, slots: make(chan struct{}, Slots),
		keep:       retention{runs: KeepRuns, bytes: KeepLogBytes},
		compaction: compaction{over: CompactOver, head: CompactHead, tail: CompactTail},
		tailEvery:  tailEvery, quitKill: QuitKillAfter, quitGrace: quitGrace, pidWait: pidWait,
		ladder: execpath.StopLadder(), alive: procs.Alive, groupAlive: groupAlive, startedAt: procs.StartedAt,
		signal: signalGroup, live: map[int64]*live{}, rerunners: map[Kind]Rerunner{}, verifiers: map[Kind]Verifier{},
		closing: make(chan struct{}),
	}
}

// RegisterRerunner makes r work out Run again for runs of kind. A release takes none:
// Run again is never offered for one.
func (e *Engine) RegisterRerunner(kind Kind, r Rerunner) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.rerunners[kind] = r
}

// RegisterVerifier makes v verify the runs of kind from before the app restarted: it
// gives each again the Callbacks its Spec gave it. Register it before Run, which picks
// those runs up.
func (e *Engine) RegisterVerifier(kind Kind, v Verifier) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.verifiers[kind] = v
}

// Start starts the run spec describes and returns its ID. In order, it takes the clone's
// lock, failing at once with a *clonelock.BusyError when another job holds it; takes a
// slot, for a run in the app, waiting for one until ctx ends unless it is a release, which
// fails at once with ErrNoSlot; runs spec.Before; records the run; and starts it, in the
// app or in the terminal. A run that was recorded but couldn't start is recorded as failed,
// and its ID comes back with the error.
func (e *Engine) Start(ctx context.Context, spec Spec) (int64, error) {
	if spec.RunsIn == "" {
		spec.RunsIn = RunsInApp
	}

	if err := e.check(spec); err != nil {
		return 0, err
	}

	h, err := e.hold(ctx, &spec)
	if err != nil {
		return 0, err
	}

	if spec.RunsIn == RunsInTerminal {
		return e.startTerminal(ctx, spec, h)
	}

	return e.startApp(ctx, spec, h)
}

// InUse reports whether a run going now uses tool, an integration's ID such as "mage-x".
// The installer waits for such a run to end before it replaces the tool. The tool's own
// update doesn't count: it is the replacement, not a run the replacement would cut across.
// Nor does a run whose end is recorded, which the engine is only letting go of.
func (e *Engine) InUse(tool string) bool {
	if tool == "" {
		return false
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, l := range e.live {
		if l.tool == tool && !l.settling && !l.ended && l.kind != KindUpdate {
			return true
		}
	}

	return false
}

// Updating returns the ID of tool's own update going now (a KindUpdate run), however it
// started, one from before the app restarted included, or 0: nothing else may replace the
// tool meanwhile. An update whose end is recorded is no longer going.
func (e *Engine) Updating(tool string) int64 {
	if tool == "" {
		return 0
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for id, l := range e.live {
		if l.kind == KindUpdate && l.tool == tool && !l.settling && !l.ended {
			return id
		}
	}

	return 0
}

// Active reports whether a run is going in the app, one from before the app restarted
// included: quitting would stop it. A run in the terminal doesn't count, nor does one whose
// end is recorded. It never waits on anything but the engine's own lock, so the quit guard
// can ask it on the main thread.
func (e *Engine) Active() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, l := range e.live {
		if l.runsIn == RunsInApp && !l.settling && !l.ended {
			return true
		}
	}

	return false
}

// Run is the engine's desktop worker. It reconciles the runs the app left behind when it
// last quit or crashed, then waits for ctx to end and stops every run in the app with
// StopAll. Each live run's log is tailed by the goroutine that follows it.
func (e *Engine) Run(ctx context.Context) {
	e.reconcile(ctx)

	<-ctx.Done()

	e.StopAll()
}

// StopAll stops every run in the app, as the app quits: SIGTERM to each as soon as its kind
// lets it be stopped, which is at once unless it refuses (a release while magex switches
// branches or pulls), SIGKILL to those left QuitKillAfter after the quit began, and each
// run's record then says it was stopped. A run whose kind still refuses QuitKillAfter in
// is never signaled: it is let go, still recorded as running, and the next start follows
// it again or settles how it ended, as it does a run in the terminal, which is always left
// alone. Nothing starts after it. It returns once the runs are recorded, or given up on.
func (e *Engine) StopAll() {
	e.regMu.Lock()
	e.mu.Lock()

	first := !e.closed
	if first {
		e.closed = true
		close(e.closing)
	}

	var runs, settling []*live

	for _, l := range e.live {
		switch {
		case l.settling:
			settling = append(settling, l)
		case l.runsIn == RunsInApp && !l.ended:
			runs = append(runs, l)
		}
	}

	e.mu.Unlock()
	e.regMu.Unlock()

	// A run only being settled has nothing left to stop: its Finish ends at once, and the
	// next start settles it again.
	for _, l := range settling {
		l.giveUp()
	}

	if first {
		e.stopForQuit(runs)
	}

	e.wg.Wait()
}

// stopForQuit sends each of runs SIGTERM as soon as its kind lets it be stopped, within
// QuitKillAfter, then SIGKILL to those still going, and gives up on any still not recorded
// a moment after that. A run its kind still holds back once QuitKillAfter has passed is
// let go unsignaled, so a pull is never cut short: its record stays as going for the next
// start. The whole fits inside the desktop shell's stop timeout.
func (e *Engine) stopForQuit(runs []*live) {
	deadline, stop := closedAfter(e.quitKill)
	defer stop()

	signaled, held := e.termWhenAllowed(runs, deadline)

	for _, l := range held {
		e.logger.Warn("left a run going as the app quit: stopping it now could leave its clone half changed",
			"run", l.id, "kind", string(l.kind), "why", l.progressNow().StopRefused)
		l.giveUp()
	}

	if waitDone(signaled, deadline) {
		return
	}

	for _, l := range signaled {
		e.signalRun(l, syscall.SIGKILL)
	}

	grace, stopGrace := closedAfter(e.quitGrace)
	defer stopGrace()

	if waitDone(signaled, grace) {
		return
	}

	for _, l := range signaled {
		l.giveUp()
	}
}

// termWhenAllowed sends each of runs SIGTERM, marked as stopped by the quit, as soon as its
// kind lets it be stopped, looking again at each tail of the logs until deadline fires. It
// returns the runs it signaled, and those its kind still held back then. A run that ends
// on its own meanwhile, its end recorded, is neither.
func (e *Engine) termWhenAllowed(runs []*live, deadline <-chan struct{}) (signaled, held []*live) {
	held = slices.Clone(runs)

	ticker := time.NewTicker(e.tailEvery)
	defer ticker.Stop()

	for {
		held = slices.DeleteFunc(held, func(l *live) bool {
			switch {
			case isDone(l) || e.isEnded(l):
				return true
			case l.progressNow().StopRefused != "":
				return false
			default:
				l.quit.Store(true)
				e.signalRun(l, syscall.SIGTERM)
				signaled = append(signaled, l)

				return true
			}
		})

		if len(held) == 0 {
			return signaled, nil
		}

		select {
		case <-deadline:
			return signaled, held
		case <-ticker.C:
		}
	}
}

// isDone reports whether a run's end is recorded, or the engine let it go.
func isDone(l *live) bool {
	select {
	case <-l.done:
		return true
	default:
		return false
	}
}

// closedAfter returns a channel closed once d has passed, which any number of waits can
// read, and what stops it early.
func closedAfter(d time.Duration) (<-chan struct{}, func() bool) {
	ch := make(chan struct{})
	timer := time.AfterFunc(d, func() { close(ch) })

	return ch, timer.Stop
}

// waitDone waits, until timeout is closed, for every run to be done, and reports whether
// they all were.
func waitDone(runs []*live, timeout <-chan struct{}) bool {
	for _, l := range runs {
		select {
		case <-l.done:
		case <-timeout:
			return false
		}
	}

	return true
}

// check returns why spec can't start, if it can't.
func (e *Engine) check(spec Spec) error {
	switch {
	case !spec.Kind.Valid():
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidSpec, spec.Kind)
	case !spec.RunsIn.Valid():
		return fmt.Errorf("%w: unknown place %q", ErrInvalidSpec, spec.RunsIn)
	case !filepath.IsAbs(spec.Dir):
		return fmt.Errorf("%w: the folder %q is not absolute", ErrInvalidSpec, spec.Dir)
	case spec.Before == nil && !validArgv(spec.Argv):
		return fmt.Errorf("%w: the command doesn't start with a program's absolute path", ErrInvalidSpec)
	case !filepath.IsAbs(e.files.dir):
		return fmt.Errorf("%w: no folder for the logs", ErrInvalidSpec)
	case e.isClosed():
		return closedError()
	default:
		return nil
	}
}

// validArgv reports a command that starts with a program's absolute path.
func validArgv(argv []string) bool {
	return len(argv) > 0 && filepath.IsAbs(argv[0])
}

// holds is what a starting run holds: its clone's lock and, in the app, a slot.
type holds struct {
	lock, slot func()
}

// free lets go of what h holds.
func (h holds) free() {
	if h.slot != nil {
		h.slot()
	}

	if h.lock != nil {
		h.lock()
	}
}

// hold takes, for a run in the app, a slot, then the clone's lock, then runs spec.Before,
// which may set spec's command. The slot comes first, so a run that waits for one never
// holds its clone while it waits: the clone's lock is only ever tried, and a busy clone
// fails the start at once.
func (e *Engine) hold(ctx context.Context, spec *Spec) (holds, error) {
	var h holds

	if spec.RunsIn == RunsInApp {
		slot, err := e.takeSlot(ctx, spec.Kind)
		if err != nil {
			return holds{}, err
		}

		h.slot = slot
	}

	release, held, ok := e.locks.TryLock(spec.Dir, clonelock.Holder{
		Kind: spec.Kind.lockKind(), Repo: e.where(spec.Repo, spec.Dir), Title: spec.Title,
	})
	if !ok {
		h.free()

		busy := &clonelock.BusyError{Holder: held}

		return holds{}, uierr.New(busy.Error(), busy)
	}

	h.lock = release

	if spec.Before == nil {
		return h, nil
	}

	argv, err := spec.Before(ctx)
	if err != nil {
		h.free()

		return holds{}, err
	}

	if argv != nil {
		spec.Argv = argv
	}

	if spec.BeforeEnv != nil {
		spec.Env = spec.BeforeEnv()
	}

	if !validArgv(spec.Argv) {
		h.free()

		return holds{}, fmt.Errorf("%w: Before gave no command that starts with a program's absolute path",
			ErrInvalidSpec)
	}

	return h, nil
}

// takeSlot takes a slot for a run in the app, and returns its release. A release never
// waits for one; any other run waits until ctx ends or the engine stops.
func (e *Engine) takeSlot(ctx context.Context, kind Kind) (func(), error) {
	if kind == KindRelease {
		select {
		case e.slots <- struct{}{}:
		default:
			return nil, uierr.New(fmt.Sprintf("%d runs are going already, and a release never waits for one to end. "+
				"Try again when one ends.", cap(e.slots)), ErrNoSlot)
		}
	} else {
		select {
		case e.slots <- struct{}{}:
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for a free slot: %w", ctx.Err())
		case <-e.closing:
			return nil, closedError()
		}
	}

	return sync.OnceFunc(func() { <-e.slots }), nil
}

// startApp records a run and starts it in the app.
func (e *Engine) startApp(ctx context.Context, spec Spec, h holds) (int64, error) {
	e.regMu.RLock()
	defer e.regMu.RUnlock()

	if e.isClosed() {
		h.free()

		return 0, closedError()
	}

	l := e.newLive(spec, h)

	id, err := e.insert(ctx, spec, l.started)
	if err != nil {
		h.free()

		return 0, err
	}

	l.id = id

	if err = e.launch(ctx, l, spec); err != nil {
		e.failStart(ctx, l, "It couldn't start: "+uierr.Message(err))

		return id, uierr.New("The run couldn't start.", err)
	}

	e.follow(context.WithoutCancel(ctx), l, e.followApp)
	e.notify()

	return id, nil
}

// launch keeps the run's state, creates its log and starts its program writing into it,
// in its own process group, and records the process.
func (e *Engine) launch(ctx context.Context, l *live, spec Spec) error {
	if err := e.files.ensureDir(); err != nil {
		return err
	}

	e.keepState(ctx, l.id, spec.Keep)

	log, err := e.files.create(l.id)
	if err != nil {
		return err
	}

	defer func() { _ = log.Close() }() // the program holds its own descriptor

	proc, err := execpath.Start(context.WithoutCancel(ctx), execpath.Cmd{
		Path: spec.Argv[0], Args: spec.Argv[1:], Dir: spec.Dir, Env: spec.Env, Output: log,
	})
	if err != nil {
		return fmt.Errorf("start the run: %w", err)
	}

	l.proc, l.pid, l.pgid = proc, proc.Pid(), proc.Pgid()
	l.procStart = e.startedAt(l.pid)

	if err = e.store.setProcess(ctx, l.id, l.pid, l.pgid, l.procStart); err != nil {
		e.logger.WarnContext(ctx, "couldn't record a run's process", "run", l.id, "error", err)
	}

	return nil
}

// startTerminal records a run and opens it in the terminal.
func (e *Engine) startTerminal(ctx context.Context, spec Spec, h holds) (int64, error) {
	l, err := e.register(ctx, spec, h)
	if err != nil {
		return 0, err
	}

	if err = e.open(ctx, l, spec); err != nil {
		e.failStart(ctx, l, uierr.Message(err))

		return l.id, err
	}

	e.follow(context.WithoutCancel(ctx), l, func(ctx context.Context, l *live) {
		e.followTerminal(ctx, l, e.pidWait)
	})
	e.notify()

	return l.id, nil
}

// register records a run and registers it as live.
func (e *Engine) register(ctx context.Context, spec Spec, h holds) (*live, error) {
	e.regMu.RLock()
	defer e.regMu.RUnlock()

	if e.isClosed() {
		h.free()

		return nil, closedError()
	}

	l := e.newLive(spec, h)

	id, err := e.insert(ctx, spec, l.started)
	if err != nil {
		h.free()

		return nil, err
	}

	l.id = id

	e.mu.Lock()
	e.live[id] = l
	e.mu.Unlock()

	return l, nil
}

// open opens a run in the terminal, after clearing any file a run the history no longer
// holds left at its names and keeping the run's state.
func (e *Engine) open(ctx context.Context, l *live, spec Spec) error {
	if e.terminal == nil {
		return uierr.New("Runs in your terminal aren't available here.", ErrNoTerminal)
	}

	if err := e.files.ensureDir(); err != nil {
		return err
	}

	if err := e.files.clear(l.id); err != nil {
		return fmt.Errorf("clear the run's files: %w", err)
	}

	e.keepState(ctx, l.id, spec.Keep)

	err := e.terminal.OpenRun(ctx, TerminalRun{
		Name: ScriptName(l.id), Dir: spec.Dir, Argv: spec.Argv, Env: spec.Env,
		Log: e.files.log(l.id), PIDFile: e.files.pid(l.id), ExitFile: e.files.exit(l.id),
	})
	if err != nil {
		return fmt.Errorf("open the run in the terminal: %w", err)
	}

	return nil
}

// insert records a run as it starts, at at: the start its state holds too, which a
// failure's notification keeps to tell the run from a later one given its ID.
func (e *Engine) insert(ctx context.Context, spec Spec, at time.Time) (int64, error) {
	command := spec.Command
	if command == "" {
		command = DisplayCommand(spec.Argv, e.home)
	}

	return e.store.insert(ctx, started{
		spec: spec, command: command, runsIn: spec.RunsIn, at: at,
		result: Result{Tool: spec.Tool, Intent: spec.Intent, Warnings: []string{}, Unrecognized: []string{}},
	})
}

// newLive returns a starting run's state.
func (e *Engine) newLive(spec Spec, h holds) *live {
	timeout := spec.Timeout
	switch {
	case spec.RunsIn == RunsInTerminal || timeout < 0:
		timeout = 0
	case timeout == 0:
		timeout = spec.Kind.Timeout()
	}

	return &live{
		Callbacks: spec.Callbacks, kind: spec.Kind, runsIn: spec.RunsIn, tool: spec.Tool, dir: spec.Dir, name: spec.Repo,
		repo: e.where(spec.Repo, spec.Dir), title: spec.Title, timeout: timeout, intent: spec.Intent, holds: h,
		started: e.clock.Now(), done: make(chan struct{}), abandon: make(chan struct{}),
	}
}

// follow registers l as live and starts following it with fn. When the engine has
// stopped, l is let go of instead, and its record stays as it is for the next start.
func (e *Engine) follow(ctx context.Context, l *live, fn func(context.Context, *live)) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		if e.live[l.id] == l {
			delete(e.live, l.id)
		}

		l.holds.free()
		l.markDone()

		return
	}

	e.live[l.id] = l

	e.wg.Go(func() { fn(ctx, l) })
}

// stop starts stopping a run in the app: its program, or a run from before the app
// restarted, climbs the stop ladder. It returns at once. A run whose kind says it can't be
// stopped now is left going, and the error says why in the kind's words. A run whose end
// is recorded has ended, though the engine may not have let go of it yet: it is never
// marked stopped, nor signaled.
func (e *Engine) stop(ctx context.Context, id int64) error {
	l := e.goingRun(id)
	if l == nil {
		if _, err := e.store.get(ctx, id); err != nil {
			return err
		}

		return alreadyEnded()
	}

	if l.runsIn == RunsInTerminal {
		return uierr.New("A run in your terminal stops there: press Control-C in its window.", ErrNoStop)
	}

	if l.settling {
		return alreadyEnded()
	}

	if why := l.progressNow().StopRefused; why != "" {
		return uierr.New(why, ErrStopRefused)
	}

	// Under the lock, so the run's end can't be recorded between the look and the stop.
	e.mu.Lock()
	defer e.mu.Unlock()

	if l.ended {
		return alreadyEnded()
	}

	l.stopped.Store(true)

	if l.proc != nil {
		l.proc.Stop(e.ladder)

		return nil
	}

	l.stopOnce.Do(func() {
		if !e.closed {
			e.wg.Go(func() { e.climb(l, e.ladder) })
		}
	})

	return nil
}

// alreadyEnded is the error of stopping a run that has ended.
func alreadyEnded() error {
	return uierr.New("It has already ended.", ErrNotRunning)
}

// climb sends a run from before the app restarted each of ladder's signals in turn, until
// its group is gone or is no longer the run's.
func (e *Engine) climb(l *live, ladder execpath.Ladder) {
	ladder.Climb(l.done, func() bool { return e.groupLive(l) }, func(sig syscall.Signal) error {
		return e.signal(l.pgid, sig)
	})
}

// signalRun sends sig to a run's process group: through its handle, while its program
// runs, or, for a run from before the app restarted, by its group once its start time
// says it is still that run.
func (e *Engine) signalRun(l *live, sig syscall.Signal) {
	if l.proc != nil {
		select {
		case <-l.proc.Done():
		default:
			_ = l.proc.Signal(sig)
		}

		return
	}

	if e.groupLive(l) {
		_ = e.signal(l.pgid, sig)
	}
}

// groupLive reports whether a run's process group is still the run's: some process is in
// it, and its leader started when the run's did.
func (e *Engine) groupLive(l *live) bool {
	return e.groupAlive(l.pgid) && sameStart(e.startedAt(l.pid), l.procStart)
}

// processAlive reports whether pid still runs and is the process that started at start.
func (e *Engine) processAlive(pid int, start time.Time) bool {
	return e.alive(pid) && sameStart(e.startedAt(pid), start)
}

// goingRun returns the run the engine follows with id while it goes, or nil: nil too for a
// run whose end is recorded, which the engine is only letting go of.
func (e *Engine) goingRun(id int64) *live {
	e.mu.Lock()
	defer e.mu.Unlock()

	if l := e.live[id]; l != nil && !l.ended {
		return l
	}

	return nil
}

// isEnded reports whether a run's end is recorded.
func (e *Engine) isEnded(l *live) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	return l.ended
}

// rerunner returns the Rerunner of kind, or nil.
func (e *Engine) rerunner(kind Kind) Rerunner {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.rerunners[kind]
}

// verifier returns the Verifier of kind, or nil.
func (e *Engine) verifier(kind Kind) Verifier {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.verifiers[kind]
}

// isClosed reports whether StopAll has begun.
func (e *Engine) isClosed() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.closed
}

// notify tells Changed that the runs changed.
func (e *Engine) notify() {
	if e.changed != nil {
		e.changed()
	}
}

// noteProgress tells Progressed, with the run's ID, when its progress changed since it
// last looked. Only the goroutine that follows the run calls it.
func (e *Engine) noteProgress(l *live) {
	if l.Progress == nil {
		return
	}

	now := l.Progress()
	if now == l.shown {
		return
	}

	l.shown = now

	if e.progressed != nil {
		e.progressed(l.id)
	}
}

// where names a clone for the user: its repository, or else its folder.
func (e *Engine) where(name repo.Name, dir string) string {
	if name.Valid() {
		return name.String()
	}

	return paths.Tilde(dir, e.home)
}

// closedError is the error of a start once the app is quitting.
func closedError() error {
	return uierr.New(brand.Name+" is quitting, so nothing new starts.", ErrClosed)
}

// sameStart reports whether two readings of a process's start time are of one process.
// A time that couldn't be read matches nothing.
func sameStart(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return false
	}

	d := a.Sub(b)

	return d <= startSlack && d >= -startSlack
}

// DisplayCommand writes argv out as a shell would read it, each word as
// textutil.ShellWord quotes it, with the program's folder under home written from ~, as
// the window shows every folder. The ~/ stays outside the quotes, where a shell still
// reads it as home. A run's Command defaults to it, and the integrations show a tool's own
// update command with it.
func DisplayCommand(argv []string, home string) string {
	parts := make([]string, 0, len(argv))

	for i, arg := range argv {
		if rest, ok := strings.CutPrefix(paths.Tilde(arg, home), "~/"); i == 0 && ok {
			parts = append(parts, "~/"+textutil.ShellWord(rest))

			continue
		}

		parts = append(parts, textutil.ShellWord(arg))
	}

	return strings.Join(parts, " ")
}
