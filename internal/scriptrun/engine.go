package scriptrun

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun/internal/queries"
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

	// Home is the user's home folder, for ~ in the folders the window shows.
	Home string

	// Clock stamps the runs. It defaults to the system clock.
	Clock clock.Clock

	// Logger gets one record per run that ends.
	Logger *slog.Logger

	// Changed is called whenever a run starts, ends, or changes status, or the history
	// changes, never for output. Core emits event.ScriptRunsUpdated from it. It must
	// return promptly and never call back into the engine.
	Changed func()

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
	store    runStore
	locks    *clonelock.Set
	files    logFiles
	home     string
	clock    clock.Clock
	logger   *slog.Logger
	changed  func()
	terminal TerminalOpener
	reveal   Revealer
	quit     func()

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

	// mu guards live, rerunners, and closed.
	mu        sync.Mutex
	live      map[int64]*live
	rerunners map[Kind]Rerunner
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
		store: runStore{q: queries.New(cfg.DB)}, locks: locks, files: logFiles{dir: cfg.LogDir}, home: cfg.Home,
		clock: clock.Or(cfg.Clock), logger: logging.Or(cfg.Logger), changed: cfg.Changed, terminal: cfg.Terminal,
		reveal: cfg.Reveal,
		quit:   cfg.Quit, slots: make(chan struct{}, Slots),
		keep:       retention{runs: KeepRuns, bytes: KeepLogBytes},
		compaction: compaction{over: CompactOver, head: CompactHead, tail: CompactTail},
		tailEvery:  tailEvery, quitKill: QuitKillAfter, quitGrace: quitGrace, pidWait: pidWait,
		ladder: execpath.StopLadder(), alive: procs.Alive, groupAlive: groupAlive, startedAt: procs.StartedAt,
		signal: signalGroup, live: map[int64]*live{}, rerunners: map[Kind]Rerunner{}, closing: make(chan struct{}),
	}
}

// RegisterRerunner makes r work out Run again for runs of kind. A release takes none:
// Run again is never offered for one.
func (e *Engine) RegisterRerunner(kind Kind, r Rerunner) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.rerunners[kind] = r
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

	h, err := e.hold(ctx, spec)
	if err != nil {
		return 0, err
	}

	if spec.RunsIn == RunsInTerminal {
		return e.startTerminal(ctx, spec, h)
	}

	return e.startApp(ctx, spec, h)
}

// InUse reports whether a run going now uses tool, an integration's ID such as "mage-x".
// The installer waits for such a run to end before it replaces the tool.
func (e *Engine) InUse(tool string) bool {
	if tool == "" {
		return false
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, l := range e.live {
		if l.tool == tool {
			return true
		}
	}

	return false
}

// Active reports whether a run is going in the app, one from before the app restarted
// included: quitting would stop it. A run in the terminal doesn't count. It never waits on
// anything but the engine's own lock, so the quit guard can ask it on the main thread.
func (e *Engine) Active() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, l := range e.live {
		if l.runsIn == RunsInApp {
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

// StopAll stops every run in the app, as the app quits: SIGTERM to each at once, SIGKILL
// to those left after QuitKillAfter, and each run's record then says it was stopped. Runs
// in the terminal are left alone, still recorded as running, for the next start to follow
// again. Nothing starts after it. It returns once the runs are recorded, or given up on.
func (e *Engine) StopAll() {
	e.regMu.Lock()
	e.mu.Lock()

	first := !e.closed
	if first {
		e.closed = true
		close(e.closing)
	}

	var runs []*live

	for _, l := range e.live {
		if l.runsIn == RunsInApp {
			runs = append(runs, l)
		}
	}

	e.mu.Unlock()
	e.regMu.Unlock()

	if first {
		e.stopForQuit(runs)
	}

	e.wg.Wait()
}

// stopForQuit sends SIGTERM to runs, SIGKILL to those still going QuitKillAfter later,
// and gives up on any still not recorded a moment after that.
func (e *Engine) stopForQuit(runs []*live) {
	for _, l := range runs {
		l.quit.Store(true)
		e.signalRun(l, syscall.SIGTERM)
	}

	if waitDone(runs, e.quitKill) {
		return
	}

	for _, l := range runs {
		e.signalRun(l, syscall.SIGKILL)
	}

	if waitDone(runs, e.quitGrace) {
		return
	}

	for _, l := range runs {
		l.giveUp()
	}
}

// waitDone waits up to d for every run to be done, and reports whether they all were.
func waitDone(runs []*live, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	for _, l := range runs {
		select {
		case <-l.done:
		case <-timer.C:
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
	case len(spec.Argv) == 0 || !filepath.IsAbs(spec.Argv[0]):
		return fmt.Errorf("%w: the command doesn't start with a program's absolute path", ErrInvalidSpec)
	case !filepath.IsAbs(e.files.dir):
		return fmt.Errorf("%w: no folder for the logs", ErrInvalidSpec)
	case e.isClosed():
		return closedError()
	default:
		return nil
	}
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

// hold takes, for a run in the app, a slot, then the clone's lock, then runs spec.Before.
// The slot comes first, so a run that waits for one never holds its clone while it waits:
// the clone's lock is only ever tried, and a busy clone fails the start at once.
func (e *Engine) hold(ctx context.Context, spec Spec) (holds, error) {
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

	if spec.Before != nil {
		if err := spec.Before(ctx); err != nil {
			h.free()

			return holds{}, err
		}
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

	var once sync.Once

	return func() { once.Do(func() { <-e.slots }) }, nil
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

	id, err := e.insert(ctx, spec)
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

// launch creates a run's log and starts its program writing into it, in its own process
// group, and records the process.
func (e *Engine) launch(ctx context.Context, l *live, spec Spec) error {
	if err := e.files.ensureDir(); err != nil {
		return err
	}

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

	id, err := e.insert(ctx, spec)
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
// holds left at its names.
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

	err := e.terminal.OpenRun(ctx, TerminalRun{
		Name: fmt.Sprintf("run-%d", l.id), Dir: spec.Dir, Argv: spec.Argv, Env: spec.Env,
		Log: e.files.log(l.id), PIDFile: e.files.pid(l.id), ExitFile: e.files.exit(l.id),
	})
	if err != nil {
		return fmt.Errorf("open the run in the terminal: %w", err)
	}

	return nil
}

// insert records a run as it starts.
func (e *Engine) insert(ctx context.Context, spec Spec) (int64, error) {
	command := spec.Command
	if command == "" {
		command = displayCommand(spec.Argv)
	}

	return e.store.insert(ctx, started{
		spec: spec, command: command, runsIn: spec.RunsIn, at: e.clock.Now(),
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
		kind: spec.Kind, runsIn: spec.RunsIn, tool: spec.Tool, dir: spec.Dir, repo: e.where(spec.Repo, spec.Dir),
		title: spec.Title, timeout: timeout, output: spec.Output, finish: spec.Finish, intent: spec.Intent, holds: h,
		started: e.clock.Now(),
		done:    make(chan struct{}), abandon: make(chan struct{}),
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
// restarted, climbs the stop ladder. It returns at once.
func (e *Engine) stop(ctx context.Context, id int64) error {
	l := e.liveRun(id)
	if l == nil {
		if _, err := e.store.get(ctx, id); err != nil {
			return err
		}

		return uierr.New("It has already ended.", ErrNotRunning)
	}

	if l.runsIn == RunsInTerminal {
		return uierr.New("A run in your terminal stops there: press Control-C in its window.", ErrNoStop)
	}

	l.stopped.Store(true)

	if l.proc != nil {
		l.proc.Stop(e.ladder)

		return nil
	}

	l.stopOnce.Do(func() {
		e.mu.Lock()
		defer e.mu.Unlock()

		if !e.closed {
			e.wg.Go(func() { e.climb(l, e.ladder) })
		}
	})

	return nil
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

// liveRun returns the run the engine follows with id, or nil.
func (e *Engine) liveRun(id int64) *live {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.live[id]
}

// rerunner returns the Rerunner of kind, or nil.
func (e *Engine) rerunner(kind Kind) Rerunner {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.rerunners[kind]
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

// displayCommand writes argv out as a shell would read it, quoting what needs quoting.
func displayCommand(argv []string) string {
	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		parts = append(parts, shellQuote(arg))
	}

	return strings.Join(parts, " ")
}

// shellQuote quotes s for a POSIX shell, unless it needs none.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsFunc(s, needsQuote) {
		return s
	}

	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// needsQuote reports a character a shell would read as more than itself.
func needsQuote(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("-_./=:,+@%", r)
}
