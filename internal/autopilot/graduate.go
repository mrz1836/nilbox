package autopilot

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Graduating: a routine in Ask first whose last GraduateAfter runs the user approved all
// finished cleanly offers to go on Autopilot ("Put it on Autopilot?"), on its row. It is a
// suggestion alone: nothing here, or anywhere, changes a routine's mode by itself. Yes is
// the user's own SetMode; Not now (DeclineGraduate) waits for that many clean approvals
// more.

// GraduateAfter is how many approved runs in a row must finish cleanly before a routine
// offers to go on Autopilot.
const GraduateAfter = 5

// graduates reports whether r offers to go on Autopilot, from its ledger: a routine of the
// account signed in, on, loading, in Ask first, with a step that asks first, whose last
// GraduateAfter answered matches since the user last said Not now were approved and
// finished cleanly.
func (s *Service) graduates(ctx context.Context, l *ledger, r Routine, declined *time.Time) (bool, error) {
	if !r.Enabled || r.OtherAccount || r.BrokenReason != "" || r.Mode != autopilotprefs.ModeAsk ||
		len(r.DryRunOnly) > 0 || !asksFirst(r) {
		return false, nil
	}

	since := time.Time{}
	if declined != nil {
		since = *declined
	}

	rows, err := l.q.ListRoutineAsked(ctx, queries.ListRoutineAskedParams{
		RoutineID: r.ID, Since: &since, RowLimit: GraduateAfter,
	})
	if err != nil {
		return false, uierr.New("Autopilot's history couldn't be read.", err)
	}

	if len(rows) < GraduateAfter {
		return false, nil
	}

	return !slices.ContainsFunc(rows, func(m queries.ListRoutineAskedRow) bool { return !cleanApproval(m) }), nil
}

// asksFirst reports a routine with a step that follows its mode, which Ask first asks the
// user about: without one, Ask first and Autopilot run it alike.
func asksFirst(r Routine) bool {
	return slices.ContainsFunc(r.Spec.Steps, func(st spec.Step) bool {
		info, ok := catalog.StepByKind(st.Kind())

		return ok && info.AsksFirst
	})
}

// cleanApproval reports a match the user approved that ran to its end with no step
// failing, no agent that pushed nothing, and no checks still failing after.
func cleanApproval(m queries.ListRoutineAskedRow) bool {
	if !m.Approved || State(m.State) != StateDone || Reason(m.Reason) != ReasonRan {
		return false
	}

	for _, bad := range []string{
		`"status":"` + stepFailed + `"`,
		`"outcome":"` + string(catalog.OutcomeNotPushed) + `"`,
		`"outcome":"` + string(catalog.OutcomeFailingChecks) + `"`,
	} {
		if strings.Contains(m.Steps, bad) {
			return false
		}
	}

	return true
}

// DeclineGraduate answers Not now to putting the routine id on Autopilot: it offers again
// only once that many more approved runs in a row finish cleanly. Its mode stays as it is.
func (s *Service) DeclineGraduate(ctx context.Context, id int64) (Routine, error) {
	if s.store.q == nil {
		return Routine{}, ErrNotSetUp
	}

	now := s.clock.Now()

	n, err := s.store.q.SetRoutineGraduateDeclined(ctx, queries.SetRoutineGraduateDeclinedParams{
		ID: id, DeclinedAt: &now,
	})
	if err != nil {
		return Routine{}, uierr.New("Not now couldn't be saved.", err)
	}

	if n == 0 {
		return Routine{}, goneError(id)
	}

	if s.engine != nil {
		s.engine.notify()
	}

	return s.Get(ctx, id)
}
