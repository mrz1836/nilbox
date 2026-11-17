package sim

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/mrz1836/nilbox/internal/autopilot"
)

// Runner is the fake agent runner: a run it starts goes on until the scenario ends it
// (Finish), stops it (Stop), or the runner closes. It outlives an engine, as a run may
// outlive the app's quit, and tells the engine it is attached to how each run ended. It
// keeps the Runner's contract: a run stays alive until its end has been delivered, and a
// run it never started is one it has no record of (but FinishLate, which breaks it).
type Runner struct {
	mu      sync.Mutex
	engine  *autopilot.Engine
	going   map[int64]autopilot.Job
	started []autopilot.Job
	stopped []int64
	asked   []int64
	closed  bool

	// gate holds a start until Release, or with startFirst its answer once it started;
	// aliveErr has Alive fail; stopFails is how many Stops fail before one goes through;
	// stopGate holds every Stop until ReleaseStops.
	gate       chan struct{}
	startFirst bool
	aliveErr   error
	stopFails  int
	stopGate   chan struct{}

	// answers are what the next starts answer with instead of starting, as a launch the
	// launcher held back does, and held the jobs each answered.
	answers []autopilot.Result
	held    []autopilot.Job
}

// NewRunner returns a runner with nothing going.
func NewRunner() *Runner {
	return &Runner{going: map[int64]autopilot.Job{}}
}

// AnswerWith has the next starts answer with each of results in turn instead of starting,
// as launches the launcher held back.
func (r *Runner) AnswerWith(results ...autopilot.Result) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.answers = append(r.answers, results...)
}

// Held returns the jobs answered with AnswerWith's results, in order.
func (r *Runner) Held() []autopilot.Job {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.held)
}

// Close closes the runner, as the run engine closes at quit: a start fails with
// autopilot.ErrClosed.
func (r *Runner) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.closed = true
}

// Reopen opens a closed runner again, as the next start's run engine is.
func (r *Runner) Reopen() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.closed = false
}

// Hold holds every start until Release, as a start that waits on the run engine.
func (r *Runner) Hold() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.gate, r.startFirst = make(chan struct{}), false
}

// HoldAnswers starts each run at once, but holds its answer until Release, as a start the
// run engine took whose answer hasn't come back.
func (r *Runner) HoldAnswers() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.gate, r.startFirst = make(chan struct{}), true
}

// Release lets the starts through.
func (r *Runner) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.gate != nil {
		close(r.gate)
		r.gate = nil
	}
}

// FailAlive has Alive fail with err, or answer again for nil.
func (r *Runner) FailAlive(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.aliveErr = err
}

// FailStops has the next n Stops fail.
func (r *Runner) FailStops(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopFails = n
}

// HoldStops holds every Stop until ReleaseStops, as a runner that takes its time over a
// stop: the engine has asked it, and its answer, done or failed, isn't back yet.
func (r *Runner) HoldStops() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopGate = make(chan struct{})
}

// ReleaseStops lets the Stops through.
func (r *Runner) ReleaseStops() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.stopGate != nil {
		close(r.stopGate)
		r.stopGate = nil
	}
}

// Forget forgets a run, as a run that ended with no one told does.
func (r *Runner) Forget(runID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.going, runID)
}

// Lose forgets a run altogether, as a runner whose history drops old runs does, breaking
// the contract: it then says it never started it.
func (r *Runner) Lose(runID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.going, runID)
	r.started = slices.DeleteFunc(r.started, func(j autopilot.Job) bool { return j.RunID == runID })
}

// Dispatch starts a run.
func (r *Runner) Dispatch(ctx context.Context, job autopilot.Job) (autopilot.Result, error) {
	r.mu.Lock()
	if len(r.answers) > 0 {
		res := r.answers[0]
		r.answers = r.answers[1:]
		r.held = append(r.held, job)
		r.mu.Unlock()

		return res, nil
	}

	gate, first := r.gate, r.startFirst

	if first {
		r.going[job.RunID] = job
		r.started = append(r.started, job)
	}
	r.mu.Unlock()

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return autopilot.Result{}, ctx.Err()
		}
	}

	if first {
		return autopilot.Result{Running: true}, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return autopilot.Result{}, autopilot.ErrClosed
	}

	r.going[job.RunID] = job
	r.started = append(r.started, job)

	return autopilot.Result{Running: true}, nil
}

// Stop stops a run, and tells the engine; while HoldStops holds them, it waits for
// ReleaseStops first.
func (r *Runner) Stop(ctx context.Context, runID int64) error {
	r.mu.Lock()
	r.asked = append(r.asked, runID)
	gate := r.stopGate
	r.mu.Unlock()

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	r.mu.Lock()

	if r.stopFails > 0 {
		r.stopFails--
		r.mu.Unlock()

		return errStop
	}

	_, ok := r.going[runID]
	r.stopped = append(r.stopped, runID)
	r.mu.Unlock()

	if ok {
		r.end(runID, autopilot.RunEnd{RunID: runID, Outcome: autopilot.RunStopped})
	}

	return nil
}

// StopAll stops every run.
func (r *Runner) StopAll(ctx context.Context) error {
	for _, id := range r.Going() {
		if err := r.Stop(ctx, id); err != nil {
			return err
		}
	}

	return nil
}

// Alive says whether a run goes, ended, or never started here.
func (r *Runner) Alive(_ context.Context, runID int64) (autopilot.Liveness, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.aliveErr != nil {
		return autopilot.LiveUnknown, r.aliveErr
	}

	if _, ok := r.going[runID]; ok {
		return autopilot.LiveAlive, nil
	}

	if slices.ContainsFunc(r.started, func(j autopilot.Job) bool { return j.RunID == runID }) {
		return autopilot.LiveGone, nil
	}

	return autopilot.LiveNever, nil
}

// errStop is a Stop that failed.
var errStop = errors.New("the run couldn't be stopped")

// Going returns the runs going, in the order they started.
func (r *Runner) Going() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]int64, 0, len(r.going))
	for id := range r.going {
		out = append(out, id)
	}

	slices.Sort(out)

	return out
}

// Started returns every run started, in order.
func (r *Runner) Started() []autopilot.Job {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.started)
}

// Stopped returns the runs a Stop reached.
func (r *Runner) Stopped() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.stopped)
}

// StopsAsked returns the run of every Stop asked, in order: one held, one that failed,
// and one that reached its run alike.
func (r *Runner) StopsAsked() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.asked)
}

// Finish ends a run as end says (its RunID is filled in, and its outcome done unless it
// says), and tells the engine.
func (r *Runner) Finish(runID int64, end autopilot.RunEnd) {
	r.end(runID, ended(runID, end))
}

// FinishLate ends a run as Finish does, but as a runner that breaks the contract: it lets
// go of the run at once, and tells the engine only when deliver is called.
func (r *Runner) FinishLate(runID int64, end autopilot.RunEnd) (deliver func()) {
	r.mu.Lock()
	delete(r.going, runID)
	e := r.engine
	r.mu.Unlock()

	end = ended(runID, end)

	return func() {
		if e != nil {
			e.RunEnded(end)
		}
	}
}

// end tells the engine of a run's end, then lets go of the run: until its end is
// delivered, it stays alive.
func (r *Runner) end(runID int64, end autopilot.RunEnd) {
	r.mu.Lock()
	e := r.engine
	r.mu.Unlock()

	if e != nil {
		e.RunEnded(end)
	}

	r.mu.Lock()
	delete(r.going, runID)
	r.mu.Unlock()
}

// ended fills in a run's ID, and its outcome, done unless end says.
func ended(runID int64, end autopilot.RunEnd) autopilot.RunEnd {
	end.RunID = runID
	if end.Outcome == "" {
		end.Outcome = autopilot.RunDone
	}

	return end
}

// Notices is a fake of the notifications the notices tell (autopilot.NoticeSink): it
// records each notice handed over, keeps one of each key as the notifications do (a key
// told already is told by no one, and no error), fails while Fail says so, as a database
// that refuses a write, and, while Hold says so, records a notice and then waits for the
// app to quit, as a quit that cuts short the step that told it.
type Notices struct {
	mu      sync.Mutex
	calls   []autopilot.Notice
	told    []autopilot.Notice
	failing error
	holding bool
}

// Notice records n, or fails.
func (n *Notices) Notice(ctx context.Context, notice autopilot.Notice) error {
	n.mu.Lock()

	if n.failing != nil {
		n.mu.Unlock()

		return n.failing
	}

	n.calls = append(n.calls, notice)

	if notice.Key == "" || !slices.ContainsFunc(n.told, func(t autopilot.Notice) bool { return t.Key == notice.Key }) {
		n.told = append(n.told, notice)
	}

	holding := n.holding
	n.mu.Unlock()

	if holding {
		<-ctx.Done()

		return ctx.Err()
	}

	return nil
}

// Told returns the notices told, in order, each key once, as the user sees them.
func (n *Notices) Told() []autopilot.Notice {
	n.mu.Lock()
	defer n.mu.Unlock()

	return slices.Clone(n.told)
}

// Calls returns every notice handed over, in order, a key told again included.
func (n *Notices) Calls() []autopilot.Notice {
	n.mu.Lock()
	defer n.mu.Unlock()

	return slices.Clone(n.calls)
}

// Hold has every notice wait, once recorded, for the app to quit, or tells them at once
// again for false.
func (n *Notices) Hold(hold bool) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.holding = hold
}

// Fail has every notice fail with err from now on, or be told again for nil.
func (n *Notices) Fail(err error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.failing = err
}

// attach tells e of the runs' ends from now on.
func (r *Runner) attach(e *autopilot.Engine) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.engine = e
}
