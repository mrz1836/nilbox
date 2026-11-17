package core

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/scripts"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// myScripts reads the user's own Repo scripts for Autopilot's script step
// (autopilot.MyScripts): the builder's choices, the checks of a draft, and the engine's
// Ask before running.
type myScripts struct {
	routines scripts.Routines
}

// MyScripts returns the user's own scripts, in their order.
func (m myScripts) MyScripts(ctx context.Context) ([]autopilot.MyScript, error) {
	got, err := m.routines.Mine(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]autopilot.MyScript, 0, len(got))
	for _, s := range got {
		out = append(out, autopilot.MyScript{
			ID: s.ID, Name: s.Name, Command: s.Command, Asks: s.Confirm, Look: s.Look, Shows: s.Shows,
		})
	}

	return out, nil
}

// scriptClones finds an item's clone, as a launch finds it.
type scriptClones interface {
	CloneTarget(ctx context.Context, itemID int64) (launcher.CloneTarget, error)
}

// scriptStarter starts a routine's run of one of the user's scripts: Repo scripts' own.
type scriptStarter interface {
	Run(ctx context.Context, req scripts.RoutineRun) (int64, error)
}

// scriptHistory stops and reads the run engine's runs.
type scriptHistory interface {
	Run(ctx context.Context, id int64) (scriptrun.Run, error)
	Stop(ctx context.Context, id int64) error
}

// scriptLedger is what the scripts' runner reads of the engine's ledger, and tells it: the
// run engine's run each script step's run started, and how one ended. *autopilot.Engine is
// it.
type scriptLedger interface {
	ScriptRunOf(ctx context.Context, runID int64) (autopilot.ScriptRun, error)
	ScriptRunsGoing(ctx context.Context) (map[int64]int64, error)
	RunEnded(end autopilot.RunEnd)
}

// scriptSteps runs a routine's Run one of my scripts (autopilot.Runner, the scripts'):
// one of the user's own Repo scripts, in the item's clone, in the app, through Repo
// scripts' own run, its end told to the engine once recorded. A script that asks before
// each run runs only with the user's OK of it as it is now, which the job carries; a
// script that is gone, or doesn't show in the clone, fails the step; no clone, a busy
// clone, and Scripts turned off wait. Stop stops it in the run engine; Alive reads the run
// engine's record, and tells the engine of a run that ended while it didn't hear.
type scriptSteps struct {
	starter scriptStarter
	mine    autopilot.MyScripts
	clones  scriptClones
	history scriptHistory

	// ledger is the engine's, nil until it is built.
	ledger func() scriptLedger
}

// newScriptSteps returns the scripts' runner over Repo scripts' service, the launcher's
// clones, the run engine's history, and the engine waker holds once it is built.
func newScriptSteps(svc *scripts.Service, clones scriptClones, history scriptHistory, waker *engineWaker,
) scriptSteps {
	routines := scripts.NewRoutines(svc)

	return scriptSteps{
		starter: routines, mine: myScripts{routines: routines}, clones: clones, history: history,
		ledger: func() scriptLedger {
			if e := waker.Built(); e != nil {
				return e
			}

			return nil
		},
	}
}

// Dispatch starts job's script, and returns with Running, or what it waits on.
func (s scriptSteps) Dispatch(ctx context.Context, job autopilot.Job) (autopilot.Result, error) {
	if job.Step.Script == nil {
		return autopilot.Result{}, fmt.Errorf("%w: not a script step", errUnknownStep)
	}

	sc, err := s.script(ctx, *job.Step.Script)
	if err != nil {
		return autopilot.Result{}, err
	}

	if sc.Asks && !slices.Contains(job.Confirmed, autopilot.ScriptKey(sc.ID, sc.Look)) {
		return autopilot.ScriptAsks(sc), nil
	}

	target, err := s.clones.CloneTarget(ctx, job.Item.ID)
	if err != nil {
		return autopilot.Result{}, err
	}

	if !target.Cloned {
		return autopilot.Result{Wait: autopilot.BlockNoClone, WaitWords: "No local clone of " + job.Item.Repo + "."}, nil
	}

	runID := job.RunID

	id, err := s.starter.Run(ctx, scripts.RoutineRun{
		ID: sc.ID, Dir: target.Dir, Look: sc.Look,
		Ended: func(_ context.Context, _ int64, end scripts.RoutineEnd) { s.tell(scriptEnd(runID, end)) },
	})
	if err != nil {
		return startRefused(err, id)
	}

	return autopilot.Result{
		Running:  true,
		Launched: &autopilot.Launched{ScriptRunID: id, ClonePath: target.Dir, Command: sc.Command},
	}, nil
}

// Stop stops the run engine's run of the script step's run runID.
func (s scriptSteps) Stop(ctx context.Context, runID int64) error {
	l := s.ledger()
	if l == nil {
		return autopilot.ErrNoEngine
	}

	run, err := l.ScriptRunOf(ctx, runID)
	if err != nil {
		return err
	}

	if run.ScriptRunID == 0 {
		return uierr.New("The script hasn't started yet; try again in a moment.", autopilot.ErrNoRun)
	}

	return s.history.Stop(ctx, run.ScriptRunID)
}

// StopAll stops every script step's run going.
func (s scriptSteps) StopAll(ctx context.Context) error {
	l := s.ledger()
	if l == nil {
		return nil
	}

	going, err := l.ScriptRunsGoing(ctx)
	if err != nil {
		return err
	}

	var errs []error

	for _, id := range going {
		if id == 0 {
			continue
		}

		if stopErr := s.history.Stop(ctx, id); stopErr != nil && !errors.Is(stopErr, scriptrun.ErrNotRunning) {
			errs = append(errs, stopErr)
		}
	}

	return errors.Join(errs...)
}

// Alive says what the run engine knows of the script step's run runID: one going is alive;
// one that ended is told to the engine from its record, as the engine may not have heard
// (a quit came first), and is alive until that end is applied; a run whose start the
// ledger never recorded, or that the history no longer holds, is gone, never "never", as
// it may have run.
func (s scriptSteps) Alive(ctx context.Context, runID int64) (autopilot.Liveness, error) {
	l := s.ledger()
	if l == nil {
		return autopilot.LiveUnknown, nil
	}

	run, err := l.ScriptRunOf(ctx, runID)
	if err != nil {
		return autopilot.LiveUnknown, err
	}

	if run.ScriptRunID == 0 {
		return autopilot.LiveGone, nil
	}

	rec, err := s.history.Run(ctx, run.ScriptRunID)

	switch {
	case errors.Is(err, scriptrun.ErrNotFound):
		return autopilot.LiveGone, nil
	case err != nil:
		return autopilot.LiveUnknown, err
	case rec.Status.Live():
		return autopilot.LiveAlive, nil
	}

	// A record stopped without the engine hearing was stopped by a quit, or by the user
	// as one came: either way, the user looks at it again.
	quit := rec.Status == scriptrun.StatusStopped
	l.RunEnded(runEndOf(runID, rec.Status, rec.Summary, quit))

	return autopilot.LiveAlive, nil
}

// errUnknownStep is a job for a step this runner doesn't run.
var errUnknownStep = errors.New("a step the scripts don't run")

// startRefused says what a start Repo scripts refused comes to: a busy clone waits for it,
// Scripts turned off, or a script that can't run, waits as not ready, the app quitting
// keeps the match queued, and anything else fails the step. A run recorded that couldn't
// start (id) is failed in the run engine's history, and fails the step too.
func startRefused(err error, id int64) (autopilot.Result, error) {
	switch {
	case errors.Is(err, scriptrun.ErrClosed):
		return autopilot.Result{}, fmt.Errorf("%w: %w", autopilot.ErrClosed, err)
	case id == 0 && errors.Is(err, clonelock.ErrBusy):
		return autopilot.Result{Wait: autopilot.BlockBusyWorktree, WaitWords: "Queued: " + uierr.Message(err)}, nil
	case id == 0 && errors.Is(err, scripts.ErrCantRun):
		return autopilot.Result{Wait: autopilot.BlockNotReady, WaitWords: "Not ready on this Mac: " + uierr.Message(err)}, nil
	case id == 0 && errors.Is(err, scripts.ErrNoClone):
		return autopilot.Result{Wait: autopilot.BlockNoClone, WaitWords: uierr.Message(err)}, nil
	}

	return autopilot.Result{}, err
}

// scriptEnd is how a routine's script run ended, as the engine records it: the app's quit
// interrupts it, the user's Stop stops it, and otherwise its outcome says.
func scriptEnd(runID int64, end scripts.RoutineEnd) autopilot.RunEnd {
	return runEndOf(runID, end.Outcome.Status, end.Outcome.Summary, end.Quit)
}

// runEndOf is the engine's end of the run runID, whose script run ended with status and
// summary; quit reports the app's quit stopped it, which interrupts it.
func runEndOf(runID int64, status scriptrun.Status, summary string, quit bool) autopilot.RunEnd {
	out := autopilot.RunEnd{RunID: runID, Summary: summary}

	switch {
	case quit, status == scriptrun.StatusInterrupted:
		out.Outcome, out.Error = autopilot.RunInterrupted, summary
	case status == scriptrun.StatusSucceeded:
		out.Outcome = autopilot.RunDone
	case status == scriptrun.StatusStopped:
		out.Outcome = autopilot.RunStopped
	default:
		out.Outcome, out.Error = autopilot.RunFailed, summary
	}

	return out
}

// script returns the user's script ref names, or the error that fails a step naming one
// that is gone.
func (s scriptSteps) script(ctx context.Context, ref spec.ScriptStep) (autopilot.MyScript, error) {
	mine, err := s.mine.MyScripts(ctx)
	if err != nil {
		return autopilot.MyScript{}, err
	}

	for _, m := range mine {
		if m.ID == ref.ID {
			return m, nil
		}
	}

	return autopilot.MyScript{}, uierr.New(fmt.Sprintf("%q is no longer one of your scripts: pick another in the routine.",
		ref.Name), fmt.Errorf("%w: your script %d", scripts.ErrNotFound, ref.ID))
}

// tell hands end to the engine, once it is built.
func (s scriptSteps) tell(end autopilot.RunEnd) {
	if l := s.ledger(); l != nil {
		l.RunEnded(end)
	}
}
