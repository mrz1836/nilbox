package autopilot

import (
	"context"
	"fmt"
	"reflect"

	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Turning a routine on, taking it out of Dry run, or saving a change to its Watch, When,
// or If asks the user about the items it matches now and has no record of (Matching):
// run on them too, or only on what changes from now on. The change and the answer reach
// the engine together, as one command, so no cycle runs between them: with "from now on",
// the routine's current matches are recorded as its baseline once its searches were read
// after the change, and never run. Without an engine (or in a copy of the app that doesn't
// hold it), the change is made at once, and the answer is the default.

// ErrInvalidCurrent is returned for an answer that isn't one of the choices.
var ErrInvalidCurrent = fmt.Errorf("unknown choice for the current matches")

// checkCurrent refuses an answer that isn't one of the choices.
func checkCurrent(c Current) error {
	if c.Valid() {
		return nil
	}

	return uierr.New("Choose to run on them too, or only on what changes from now on.",
		fmt.Errorf("%w %q", ErrInvalidCurrent, c))
}

// Save stores d, as save says, and with d.Current CurrentFromNow, records what the routine
// matches now as its baseline, when it is on and its Watch, When, or If changed.
func (s *Service) Save(ctx context.Context, d Draft) (Routine, error) {
	if err := checkCurrent(d.Current); err != nil {
		return Routine{}, err
	}

	var before *Routine

	if d.ID != 0 {
		if r, err := s.Get(ctx, d.ID); err == nil {
			before = &r
		}
	}

	return s.applying(ctx, d.Current, func(ctx context.Context) (Routine, error) {
		return s.save(ctx, d)
	}, func(after Routine) bool {
		return before != nil && after.Enabled && watchChanged(*before, after)
	})
}

// watchChanged reports a routine whose Watch, When, or If changed.
func watchChanged(before, after Routine) bool {
	return !reflect.DeepEqual(before.Spec.Watch, after.Spec.Watch) ||
		!reflect.DeepEqual(before.Spec.Trigger, after.Spec.Trigger) ||
		!reflect.DeepEqual(before.Spec.Conditions, after.Spec.Conditions)
}

// SetEnabled switches the routine id on or off, and returns it. A broken routine can't be
// switched on; switching on one that is on keeps when it was. Switching it on with
// current CurrentFromNow records what it matches now as its baseline.
func (s *Service) SetEnabled(ctx context.Context, id int64, on bool, current Current) (Routine, error) {
	if err := checkCurrent(current); err != nil {
		return Routine{}, err
	}

	return s.applying(ctx, current, func(ctx context.Context) (Routine, error) {
		return s.setEnabled(ctx, id, on)
	}, func(after Routine) bool { return on && after.Enabled })
}

// SetMode sets how the routine id acts on a match, and returns it. A broken routine's mode
// can't be changed, and a routine with a step this build can't run yet stays in Dry run.
// Leaving Dry run with current CurrentFromNow records what it matches now as its
// baseline.
func (s *Service) SetMode(ctx context.Context, id int64, mode autopilotprefs.Mode, current Current) (Routine, error) {
	if err := checkCurrent(current); err != nil {
		return Routine{}, err
	}

	return s.applying(ctx, current, func(ctx context.Context) (Routine, error) {
		return s.setMode(ctx, id, mode)
	}, func(after Routine) bool { return mode != autopilotprefs.ModeDryRun && after.Enabled })
}

// applying makes change, through the engine when this copy runs it, and records what the
// routine matches now as its baseline when current says so and asks says the change is one
// the question is asked about.
func (s *Service) applying(ctx context.Context, current Current, change func(ctx context.Context) (Routine, error),
	asks func(after Routine) bool,
) (Routine, error) {
	e := s.engine
	if e == nil || !e.lock.Held() {
		return change(ctx)
	}

	var out Routine

	err := e.do(ctx, func(ctx context.Context) error {
		r, err := change(ctx)
		if err != nil {
			return err
		}

		out = r

		if current == CurrentFromNow && asks(r) {
			e.fromNow(ctx, r.ID, r.Mode == autopilotprefs.ModeDryRun)
		}

		return nil
	})

	return out, err
}
