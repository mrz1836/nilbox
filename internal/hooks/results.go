package hooks

import (
	"cmp"
	"context"
	"fmt"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// ActionResult returns a hooks action's result, as its run recorded it once the hooks were
// read: the run's Result.Outcome, typed, since the runner keeps it as plain JSON, which the
// window can't name. It fails for a run the history no longer holds
// (scriptrun.ErrNotFound), a run that isn't a hooks action, one still going, and one that
// ended without a result, as one that couldn't start (ErrNoResult).
func (s *Service) ActionResult(ctx context.Context, runID int64) (ActionResult, error) {
	res, err := resultOf[ActionResult](ctx, s, runID, scriptrun.KindHooks)
	if err != nil {
		return ActionResult{}, err
	}

	if !res.Action.Valid() {
		return ActionResult{}, noResult(runID, "names no action")
	}

	res.Removed = textutil.NonNil(res.Removed)

	return res, nil
}

// ChecksResult returns a checks run's result, as its run recorded it, typed, as
// ActionResult does a hooks action's.
func (s *Service) ChecksResult(ctx context.Context, runID int64) (ChecksResult, error) {
	res, err := resultOf[ChecksResult](ctx, s, runID, scriptrun.KindChecks)
	if err != nil {
		return ChecksResult{}, err
	}

	if !res.Scope.Valid() {
		return ChecksResult{}, noResult(runID, "names no scope")
	}

	res.Checks, res.Changed = textutil.NonNil(res.Checks), textutil.NonNil(res.Changed)
	res.Installs, res.Errors = textutil.NonNil(res.Installs), textutil.NonNil(res.Errors)

	for i := range res.Checks {
		res.Checks[i].Detail = textutil.NonNil(res.Checks[i].Detail)
		res.Checks[i].ChangedFiles = textutil.NonNil(res.Checks[i].ChangedFiles)
	}

	for i := range res.Changed {
		res.Changed[i].By = textutil.NonNil(res.Changed[i].By)
	}

	return res, nil
}

// resultOf reads the outcome of s's run runID, of kind, back as a T.
func resultOf[T any](ctx context.Context, s *Service, runID int64, kind scriptrun.Kind) (T, error) {
	var none T

	if s.history == nil {
		return none, ErrNotSetUp
	}

	run, err := s.history.Run(ctx, runID)
	if err != nil {
		return none, err
	}

	switch {
	case run.Kind != kind:
		return none, uierr.New("This run has no "+string(kind)+" result.", fmt.Errorf("%w: run %d is a %s", ErrNoResult,
			runID, run.Kind))
	case run.Status.Live():
		return none, uierr.New("This run is still going.", fmt.Errorf("%w: run %d", ErrNoResult, runID))
	}

	res, err := scriptrun.Decode[T](run.Result.Outcome)
	if err != nil {
		return none, noResult(runID, err.Error())
	}

	return res, nil
}

// noResult is the error of a run that ended without a result.
func noResult(runID int64, why string) error {
	return uierr.New("This run ended without a result to show.", fmt.Errorf("%w: run %d: %s", ErrNoResult, runID, why))
}

// Rerunner works Run again out for hooks actions and checks runs (scriptrun.Rerunner):
// the same action or checks, from its recorded intent, in the same clone, checked afresh
// as a click is. Core registers it with the runner for scriptrun.KindHooks and
// scriptrun.KindChecks. It is its own type, not a method of the Service, which the window
// binds.
type Rerunner struct {
	s *Service
}

// NewRerunner returns the Rerunner of s's runs.
func NewRerunner(s *Service) Rerunner {
	return Rerunner{s: s}
}

// Rerun returns the spec of run again: a hooks action or a checks run, where it ran.
func (r Rerunner) Rerun(ctx context.Context, run scriptrun.Record) (scriptrun.Spec, error) {
	c, err := r.s.cloneAt(ctx, paths.Tilde(run.Dir, r.s.home))
	if err != nil {
		return scriptrun.Spec{}, err
	}

	if !c.name.Valid() {
		c.name = run.Repo
	}

	where := cmp.Or(run.RunsIn, scriptrun.RunsInApp)

	switch run.Kind {
	case scriptrun.KindHooks:
		var in ActionIntent
		if in, err = scriptrun.Decode[ActionIntent](run.Result.Intent); err != nil {
			return scriptrun.Spec{}, fmt.Errorf("%w: the run's intent: %w", ErrInvalidRequest, err)
		}

		return r.s.actionSpec(ctx, in.Action, c, in.Confirm, where, nil)
	case scriptrun.KindChecks:
		var in ChecksIntent
		if in, err = scriptrun.Decode[ChecksIntent](run.Result.Intent); err != nil {
			return scriptrun.Spec{}, fmt.Errorf("%w: the run's intent: %w", ErrInvalidRequest, err)
		}

		return r.s.checksSpec(ctx, c, in, where)
	case scriptrun.KindRelease, scriptrun.KindScript, scriptrun.KindUpdate, scriptrun.KindPull:
	}

	return scriptrun.Spec{}, fmt.Errorf("%w: a %s run", ErrInvalidRequest, run.Kind)
}
