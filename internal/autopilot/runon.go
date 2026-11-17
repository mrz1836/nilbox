package autopilot

import (
	"context"
	"errors"
	"fmt"

	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Run on the matches now: from the builder's Test, once the user confirmed the list it
// showed, the routine runs at once on each of the items it matches now that it would run
// on (TestItem.RunsNow): one it has no record of, one whose record closed before it ever
// ran, and one recorded as already matching when the user chose only what changes from
// now on. Each is the routine's own match for the occurrence it matches for now, with the
// user's OK, so it starts as soon as the limits let it, never waiting for Ask first or a
// catch-up's question, and the routine never runs on that occurrence again. Every limit
// still holds each (Pause, the active hours, the tries and the day's counts, a free slot,
// the daily cap, the outside contributor rule, this Mac's readiness, the item read fresh,
// the holds), and a script that asks before each run still asks. The routine must be on:
// one that is off runs on nothing.

// maxRunOn caps the items Run on the matches now takes at once: what a Test lists.
const maxRunOn = maxTestItems

// What Run on the matches now refuses: a routine that is off, or another account's, runs on
// nothing now, and a list longer than a Test's is no list the user confirmed.
var (
	// ErrRoutineOff is returned for Run on the matches now on a routine that is off.
	ErrRoutineOff = errors.New("the routine is off")

	// ErrOtherAccount is returned for Run on the matches now on another account's routine.
	ErrOtherAccount = errors.New("the routine is another account's")

	// ErrTooMany is returned for Run on the matches now on more items than a Test lists.
	ErrTooMany = errors.New("too many items at once")
)

// RanOn is what Run on the matches now did: the matches it set on their way, and how many of
// the items it was given it left out, since they no longer match, the routine has a record
// of their occurrence that it ran, or it leaves them alone, with Words saying so for a
// toast.
type RanOn struct {
	Matches []Match `json:"matches"`
	Skipped int     `json:"skipped"`
	Words   string  `json:"words"`
}

// RunOnMatches runs the routine id now on each of items that it would run on now (see the
// notes at the top of this file), and returns what it did. The routine must be saved,
// loading, on, and of the account signed in; in Dry run, each records what it would have
// done.
func (s *Service) RunOnMatches(ctx context.Context, id int64, items []int64) (RanOn, error) {
	out := RanOn{Matches: []Match{}}

	e, err := s.engineOrErr()
	if err != nil {
		return out, err
	}

	if len(items) > maxRunOn {
		return out, uierr.New(fmt.Sprintf("Run it on at most %d items at once.", maxRunOn), ErrTooMany)
	}

	var made []int64

	err = e.do(ctx, func(ctx context.Context) error {
		made, out.Skipped, err = e.runOnMatches(ctx, id, items)

		return err
	})
	if err != nil {
		return out, err
	}

	if out.Matches, err = s.matchesByID(ctx, made); err != nil {
		return out, err
	}

	out.Words = ranOnWords(len(made), out.Skipped)

	return out, nil
}

// runOnMatches records, on the loop, the routine id's match on each of items it would run on
// now, for the occurrence it matches for, with the user's OK; it returns the matches it set
// on their way, and how many items it left out.
func (e *Engine) runOnMatches(ctx context.Context, id int64, items []int64) ([]int64, int, error) {
	p, r, err := e.runOnRoutine(ctx, id)
	if err != nil {
		return nil, 0, err
	}

	all, err := e.items.Facts(ctx, items, p.now.UTC())
	if err != nil {
		return nil, 0, uierr.New("The items' facts couldn't be read.", err)
	}

	if p.runs, err = e.ledger.itemRuns(ctx, items); err != nil {
		return nil, 0, uierr.New("The items' runs couldn't be read.", err)
	}

	made := make([]int64, 0, len(all))

	for _, f := range all {
		m, runErr := p.runOn(ctx, r, f)
		if runErr != nil {
			return nil, 0, uierr.New("The run couldn't be recorded.", runErr)
		}

		if m != 0 {
			made = append(made, m)
		}
	}

	e.Wake()

	return made, len(items) - len(made), nil
}

// runOnRoutine reads the routine id for Run on the matches now, with what its ledger keeps,
// refusing one that is broken, another account's, or off.
func (e *Engine) runOnRoutine(ctx context.Context, id int64) (*pass, *routineRun, error) {
	row, err := e.svc.store.get(ctx, e.svc.store.q, id)
	if err != nil {
		return nil, nil, uierr.New("That routine is gone.", err)
	}

	l := load(row.Spec)
	viewer := e.svc.viewer(ctx)

	switch {
	case l.broken != nil:
		return nil, nil, uierr.New(brokenWords(l.broken)+" It can't run.", fmt.Errorf("%w: %w", ErrBroken, l.broken))
	case otherAccount(row.AccountLogin, viewer):
		return nil, nil, uierr.New("It belongs to @"+row.AccountLogin+", so it doesn't run here.", ErrOtherAccount)
	case !row.Enabled:
		return nil, nil, uierr.New("Switch the routine on to run it.", ErrRoutineOff)
	}

	r := &routineRun{
		row: row, spec: l.spec, mode: storedMode(row.Mode), hash: specHash(row.Spec), inWatch: map[int64]bool{},
		decided: map[int64]eval.Decision{},
	}
	p := &pass{e: e, now: e.clock.Now(), cycle: &Cycle{}}
	p.inbox.Viewer = viewer

	if err = p.readRoutine(ctx, r); err != nil {
		return nil, nil, uierr.New("The routine's history couldn't be read.", err)
	}

	return p, r, nil
}

// runOn sets r's match on f's item on its way, with the user's OK, when r would run on it
// now, and returns the match, 0 for none: a new one, or its record that closed before it
// ran, or that already matched when the user chose only what changes from now on, opened
// again.
func (p *pass) runOn(ctx context.Context, r *routineRun, f facts.Facts) (int64, error) {
	d := eval.Evaluate(r.spec, f, p.historyOf(r, f.Item.ID), p.now)
	if r.handsOff[f.Item.ID] || d.Match != eval.Yes {
		return 0, nil
	}

	dry := r.mode == autopilotprefs.ModeDryRun

	prior, recorded := r.matches[matchKey{item: f.Item.ID, fire: d.FireKey, dryRun: dry}]

	switch {
	case recorded && !runsNow(prior):
		return 0, nil
	case recorded:
		m, err := p.reopen(ctx, r, prior.ID, d)
		if err != nil {
			return 0, err
		}

		m.Approved = true
		p.e.save(ctx, m)

		return m.ID, nil
	}

	m := p.newMatch(r, f, d, dry)
	m.Approved = true

	inserted, err := p.e.ledger.insert(ctx, m)
	if err != nil || !inserted {
		return 0, err
	}

	return m.ID, nil
}

// runsNow reports a record of an occurrence Run on the matches now runs on: one that closed
// before it ever ran, and one recorded as already matching when the user chose only what
// changes from now on.
func runsNow(row queries.ListRoutineMatchKeysRow) bool {
	return reopensRow(row) || State(row.State) == StateBaseline
}

// ranOnWords says what Run on the matches now did, for a toast: "Running on 3. 1 no
// longer matched, or has a record already."
func ranOnWords(ran, skipped int) string {
	words := "It runs on none of them."
	if ran > 0 {
		words = "Running on " + textutil.Counted(ran, "item", "items") + "."
	}

	if skipped > 0 {
		words += " " + textutil.Counted(skipped, "no longer matches, or has", "no longer match, or have") +
			" a record already."
	}

	return words
}
