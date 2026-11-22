package autopilot

import (
	"context"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// What runs next: each cycle, as it decides every routine on every item it watches, the
// engine notes the routines that will run on an item by themselves (noteUpcoming), so the
// item's ⚡ chip can say so before they do. It keeps them in memory, by item, replaced
// whole by each cycle that decided every routine (keepUpcoming), so the window's read
// (Upcoming) is a map lookup: no database, no GitHub.

// Upcoming is a routine that will run on an item by itself: its trigger is on its way
// (eval.Pending: checks still running or settling, a schedule due later today, a quiet
// spell due within a day), its conditions count the item for sure, it acts (not a Dry
// run), it doesn't leave the item alone, and it has no record of the occurrence the trigger
// fires for then, or only one that reopens. A trigger that waits on an event no one can
// foresee (a comment, a label, an approval) is never on its way, so a routine never marks
// every item it watches.
type Upcoming struct {
	ItemID    int64  `json:"itemId"`
	RoutineID int64  `json:"routineId"`
	Routine   string `json:"routine"`

	// Mode is the routine's mode, and AsksFirst whether it waits for the user's OK before
	// it does anything: in Ask first, before an agent or GitHub step, while a routine of
	// local steps alone (a snooze, a pin) runs them by itself in either mode.
	Mode      autopilotprefs.Mode `json:"mode"`
	AsksFirst bool                `json:"asksFirst"`

	// Trigger is what it waits on; When says when it holds, as a sentence ends ("when its
	// checks pass", "once its checks settle", "at 09:00"), and At when, when the clock
	// alone decides it, nil while it waits on GitHub.
	Trigger catalog.Event `json:"trigger"`
	When    string        `json:"when"`
	At      *time.Time    `json:"at"`

	// Steps are what it will do, in order, as a match's steps show before they start.
	Steps []MatchStep `json:"steps"`
}

// whyLeftAlone says why a routine leaves an item alone that the user told it to, before it
// ran there, as Activity's Left alone line says it.
const whyLeftAlone = "You told it to leave this one alone."

// noteUpcoming notes r as a routine that will run on f's item, decided d with history h,
// when it is one (Upcoming): d's trigger is on its way, r acts, has no record of the
// occurrence d's trigger fires for then that keeps it from running (reopensRow), and its
// conditions count the item for sure. The item is one r doesn't leave alone, which
// evaluateItem checked.
func (p *pass) noteUpcoming(r *routineRun, f facts.Facts, h eval.History, d eval.Decision) {
	if d.Pending == nil || r.mode == autopilotprefs.ModeDryRun {
		return
	}

	if row, ok := r.matches[matchKey{item: f.Item.ID, fire: d.Pending.FireKey}]; ok && !reopensRow(row) {
		return
	}

	if r.plan.Conditions(f, h, p.now) != eval.Yes {
		return
	}

	p.upcoming = append(p.upcoming, Upcoming{
		ItemID: f.Item.ID, RoutineID: r.id(), Routine: r.row.Name, Mode: r.mode, AsksFirst: waitsForOK(r),
		Trigger: r.spec.Trigger.Event, When: d.Pending.When, At: d.Pending.At, Steps: r.plannedSteps(),
	})
}

// waitsForOK reports a routine that waits for the user's OK before it acts: one in Ask
// first with a step that follows its mode (asksFirst); a routine of local steps alone runs
// them by itself in either mode.
func waitsForOK(r *routineRun) bool {
	return r.mode == autopilotprefs.ModeAsk && asksFirst(Routine{Spec: r.spec})
}

// plannedSteps are r's steps as a match's steps show before they start, worked out once a
// cycle.
func (r *routineRun) plannedSteps() []MatchStep {
	if r.planned == nil {
		r.planned = make([]MatchStep, 0, len(r.spec.Steps))

		for _, st := range r.spec.Steps {
			r.planned = append(r.planned, stepView(&stepRun{Step: st}))
		}
	}

	return r.planned
}

// keepUpcoming keeps list, a cycle's routines that will run, by item, in place of the last
// cycle's: nil forgets them all, as when Autopilot is turned off.
func (e *Engine) keepUpcoming(list []Upcoming) {
	byItem := make(map[int64][]Upcoming, len(list))

	for _, u := range list {
		byItem[u.ItemID] = append(byItem[u.ItemID], u)
	}

	e.mu.Lock()
	e.upcoming = byItem
	e.mu.Unlock()
}

// dropUpcoming forgets that the routine id will run on the item, at once, before the next
// cycle decides it again.
func (e *Engine) dropUpcoming(id, itemID int64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	left := slices.DeleteFunc(slices.Clone(e.upcoming[itemID]), func(u Upcoming) bool { return u.RoutineID == id })
	if len(left) == 0 {
		delete(e.upcoming, itemID)
	} else {
		e.upcoming[itemID] = left
	}
}

// Upcoming returns the routines that will run by themselves on the items among ids (at most
// as many as LatestMatches reads), by item in ids' order, each item's in the routines'
// order, as the engine's last cycle found them. It reads memory alone. It returns none while
// Autopilot is off or paused, no one is signed in, or another copy of the app runs the
// routines, since then nothing here runs by itself. Its list is never nil.
func (s *Service) Upcoming(ctx context.Context, ids []int64) ([]Upcoming, error) {
	out := []Upcoming{}

	e := s.engine
	if e == nil || len(ids) == 0 || !e.holding() {
		return out, nil
	}

	if cfg := s.settings(ctx); !cfg.Enabled || cfg.Paused {
		return out, nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.status.SignedOut {
		return out, nil
	}

	for _, id := range ids[:min(len(ids), maxLatest)] {
		for _, u := range e.upcoming[id] {
			u.Steps = textutil.NonNil(slices.Clone(u.Steps))
			out = append(out, u)
		}
	}

	return out, nil
}

// LeaveAlone has the routine id leave the item alone from now on, as it does an item whose
// change by it the user took back: it no longer decides on the item, so never runs there,
// until Let it act again, which Activity's Left alone line offers. The item's ⚡ chip offers
// it for a routine about to run there.
func (s *Service) LeaveAlone(ctx context.Context, id, itemID int64) error {
	e, err := s.engineOrErr()
	if err != nil {
		return err
	}

	return e.do(ctx, func(ctx context.Context) error {
		err := e.ledger.q.InsertHandsOff(ctx, queries.InsertHandsOffParams{
			RoutineID: id, ItemID: itemID, Since: e.clock.Now(), Why: whyLeftAlone,
		})
		if err != nil {
			return uierr.New("The routine couldn't be told to leave it alone.", err)
		}

		e.dropUpcoming(id, itemID)
		e.Wake()

		return nil
	})
}
