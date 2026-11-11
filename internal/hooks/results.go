package hooks

import (
	"context"
	"encoding/json"
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
	var res ActionResult

	if err := s.result(ctx, runID, scriptrun.KindHooks, &res); err != nil {
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
	var res ChecksResult

	if err := s.result(ctx, runID, scriptrun.KindChecks, &res); err != nil {
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

// result reads the outcome of the run runID, of kind, into into.
func (s *Service) result(ctx context.Context, runID int64, kind scriptrun.Kind, into any) error {
	if s.history == nil {
		return ErrNotSetUp
	}

	run, err := s.history.Run(ctx, runID)
	if err != nil {
		return err
	}

	switch {
	case run.Kind != kind:
		return uierr.New("This run has no "+string(kind)+" result.", fmt.Errorf("%w: run %d is a %s", ErrNoResult, runID,
			run.Kind))
	case run.Status.Live():
		return uierr.New("This run is still going.", fmt.Errorf("%w: run %d", ErrNoResult, runID))
	}

	data, err := json.Marshal(run.Result.Outcome)
	if err == nil {
		err = json.Unmarshal(data, into)
	}

	if err != nil {
		return noResult(runID, err.Error())
	}

	return nil
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

	where := run.RunsIn
	if where == "" {
		where = scriptrun.RunsInApp
	}

	switch run.Kind {
	case scriptrun.KindHooks:
		var in ActionIntent
		if err = decodeIntent(run.Result.Intent, &in); err != nil {
			return scriptrun.Spec{}, err
		}

		return r.s.actionSpec(ctx, in.Action, c, in.Confirm, where, nil)
	case scriptrun.KindChecks:
		var in ChecksIntent
		if err = decodeIntent(run.Result.Intent, &in); err != nil {
			return scriptrun.Spec{}, err
		}

		return r.s.checksSpec(ctx, c, in, where)
	case scriptrun.KindRelease, scriptrun.KindScript, scriptrun.KindUpdate:
	}

	return scriptrun.Spec{}, fmt.Errorf("%w: a %s run", ErrInvalidRequest, run.Kind)
}

// decodeIntent reads a run's intent back from its record, where it was decoded as plain
// JSON values.
func decodeIntent(recorded, into any) error {
	data, err := json.Marshal(recorded)
	if err == nil {
		err = json.Unmarshal(data, into)
	}

	if err != nil {
		return fmt.Errorf("%w: the run's intent: %w", ErrInvalidRequest, err)
	}

	return nil
}
