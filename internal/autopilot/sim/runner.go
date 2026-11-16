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
	closed  bool

	// gate holds a start until Release, or with startFirst its answer once it started;
	// aliveErr has Alive fail; stopFails is how many Stops fail before one goes through.
	gate       chan struct{}
	startFirst bool
	aliveErr   error
	stopFails  int
}

// NewRunner returns a runner with nothing going.
func NewRunner() *Runner {
	return &Runner{going: map[int64]autopilot.Job{}}
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

// Forget forgets a run, as a run that ended with no one told does.
func (r *Runner) Forget(runID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.going, runID)
}

// Dispatch starts a run.
func (r *Runner) Dispatch(ctx context.Context, job autopilot.Job) (autopilot.Result, error) {
	r.mu.Lock()
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

// Stop stops a run, and tells the engine.
func (r *Runner) Stop(_ context.Context, runID int64) error {
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

// Dispatcher is a fake part's dispatcher: it records each job, which for a GitHub
// action is the write sent, and answers it at once, as Answer says (done, with nothing
// said, by default), or once Release lets the answers through.
type Dispatcher struct {
	mu   sync.Mutex
	jobs []autopilot.Job
	gate chan struct{}

	// Answer, when set, says how a job went.
	Answer func(job autopilot.Job) (autopilot.Result, error)
}

// Hold holds every answer until Release: the job is done, its answer not yet back.
func (d *Dispatcher) Hold() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.gate = make(chan struct{})
}

// Release lets the answers through.
func (d *Dispatcher) Release() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.gate != nil {
		close(d.gate)
		d.gate = nil
	}
}

// Dispatch records job and answers it.
func (d *Dispatcher) Dispatch(ctx context.Context, job autopilot.Job) (autopilot.Result, error) {
	d.mu.Lock()
	d.jobs = append(d.jobs, job)
	answer, gate := d.Answer, d.gate
	d.mu.Unlock()

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return autopilot.Result{}, ctx.Err()
		}
	}

	if answer != nil {
		return answer(job)
	}

	return autopilot.Result{Sent: true}, nil
}

// Jobs returns the jobs dispatched, in order.
func (d *Dispatcher) Jobs() []autopilot.Job {
	d.mu.Lock()
	defer d.mu.Unlock()

	return slices.Clone(d.jobs)
}

// attach tells e of the runs' ends from now on.
func (r *Runner) attach(e *autopilot.Engine) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.engine = e
}
