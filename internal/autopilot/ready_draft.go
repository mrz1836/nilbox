package autopilot

import (
	"context"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// DraftReadiness checks what the draft d needs on this Mac before it is saved, as quick
// setup shows it beside its test: each need answered from what the checks last found
// within readyFor, and checked again cheaply where it wasn't, never asking GitHub. It keeps
// nothing, and its lists are never nil.
func (s *Service) DraftReadiness(ctx context.Context, d Draft) (RoutineReady, error) {
	out := RoutineReady{Name: d.Name, Ready: true, Problems: []ReadyCheck{}, Warnings: []ReadyCheck{}}

	r := s.ready
	if r == nil || r.checker == nil {
		return out, nil
	}

	defaults, err := r.checker.Defaults(ctx)
	if err != nil {
		return out, uierr.New("What this Mac has couldn't be checked.", err)
	}

	sp := spec.Normalize(d.Spec)

	results, err := r.draftChecks(ctx, Needs(sp, defaults), s.clock.Now())
	if err != nil {
		return out, uierr.New("What this Mac has couldn't be checked.", err)
	}

	snap := readySnapshot{on: true, checked: true, results: results, defaults: defaults}
	if rr, ok := snap.routineReady(0, d.Name, sp, false); ok {
		return *rr, nil
	}

	return out, nil
}

// draftChecks checks needs for a draft, not fresh, the places by this build's offers, and
// keeps nothing.
func (r *readiness) draftChecks(ctx context.Context, needs []Need, now time.Time) (map[string]ReadyCheck, error) {
	results := map[string]ReadyCheck{}
	asked := make([]Need, 0, len(needs))

	for _, n := range needs {
		if n.Kind == NeedPlace {
			results[n.ID()] = r.placeCheck(n, now)

			continue
		}

		asked = append(asked, n)
	}

	got, err := r.checker.Check(ctx, asked, false)
	if err != nil {
		return nil, err
	}

	for _, c := range got {
		c.ID = c.Need.ID()
		results[c.ID] = c
	}

	return results, nil
}
