package autopilot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The local steps that change an item's triage (Pin, Snooze, Dismiss, and Restore) run the
// inbox's own triage, marked with the routine that ran them, through Triage, which core
// adapts. Each change is recorded in autopilot_effects in the triage's own transaction,
// before the triage's write, with the item's local state before and after it, so the two
// are stored together or not at all, and a step a crash cut off after its change runs no
// second time. Undo, a command the loop applies, puts an item back as it was before an
// effect, only where the item is still as the effect left it, and leaves the routine hands
// off the item, as the user's own reversal of what it did does (Outcomes). A routine leaves
// an item alone from then on, until Let it act again.

// ErrNoTriage is returned for an Undo with no triage to put an item back through.
var ErrNoTriage = errors.New("the inbox's triage isn't wired")

// TriageRequest is a local step's triage of its item, as the inbox's triage runs it for a
// routine.
type TriageRequest struct {
	RoutineID int64
	ItemID    int64

	// Action is the step (catalog.StepPin, StepSnooze, StepDismiss, or StepRestore), and
	// Days how long a snooze lasts.
	Action catalog.StepKind
	Days   int

	// Expect is the item's local state the routine decided on (Job.Local): the step is a
	// compare-and-set on it, and changes nothing (KeptChanged) when the part it changes
	// differs now.
	Expect string
}

// TriageKept says why a routine's triage changed nothing, in the inbox's words.
type TriageKept string

// Why a routine's triage changed nothing.
const (
	// KeptPinned is an item that was pinned already.
	KeptPinned TriageKept = "pinned"

	// KeptDismissed is an item a routine had dismissed already.
	KeptDismissed TriageKept = "dismissed"

	// KeptYours is an item the user had dismissed or snoozed themselves, which a routine
	// leaves as the user left it.
	KeptYours TriageKept = "yours"

	// KeptNotSetAside is an item neither dismissed nor snoozed, which a Restore had nothing
	// to bring back from.
	KeptNotSetAside TriageKept = "not_set_aside"

	// KeptChanged is an item whose triage changed since the routine decided on it, as by
	// the user's own triage in between, which wins.
	KeptChanged TriageKept = "changed"
)

// TriageChange is what the inbox's triage did to an item for a routine: its local state
// before and after, as the inbox writes them (JSON), or, for a change of nothing, why.
type TriageChange struct {
	Before string
	After  string
	Kept   TriageKept
}

// Changed reports a step that changed the item's triage.
func (c TriageChange) Changed() bool {
	return c.Kept == ""
}

// TriageUndo is one effect Undo takes back: the routine, the item, the step, and the item's
// local state before and after it, as the effect recorded them.
type TriageUndo struct {
	RoutineID int64
	ItemID    int64
	Action    catalog.StepKind
	Before    string
	After     string
}

// Triage is the inbox's own triage, as a routine's local steps and Undo reach it. Core
// adapts the inbox.
type Triage interface {
	// Apply runs req, and returns what it did. record runs in the change's own
	// transaction, before its write, with what it does; an error rolls it back. A step
	// that changes nothing runs no record.
	Apply(ctx context.Context, req TriageRequest,
		record func(ctx context.Context, tx *sql.Tx, change TriageChange) error) (TriageChange, error)

	// Undo puts back, in one transaction and in the order given, the state each of undos
	// found before it, where its item is still as it left it, and reports which it put
	// back; record runs in the same transaction once they are written.
	Undo(ctx context.Context, undos []TriageUndo,
		record func(ctx context.Context, tx *sql.Tx, put []bool) error) ([]bool, error)

	// AsLeft reports, for each of undos, whether its item is still as it left it.
	AsLeft(ctx context.Context, undos []TriageUndo) ([]bool, error)
}

// triageDispatcher runs the triage steps through the engine's Triage, recording each
// change as an effect.
type triageDispatcher struct {
	e *Engine
}

// errLeftAlone is a triage step whose routine was left hands off its item while the step
// was on its way: it changes nothing.
var errLeftAlone = errors.New("the routine leaves the item alone")

// Dispatch runs job's triage step, a compare-and-set on the item's local state the routine
// decided on (job.Local). A step whose effect is recorded already, as one a crash cut off
// after its change was stored, is done. One whose routine the user left hands off the item
// meanwhile, as their own reversal recorded after the step was decided, changes nothing:
// the check is in the triage's own transaction, so the user's change always wins.
func (d triageDispatcher) Dispatch(ctx context.Context, job Job) (Result, error) {
	e := d.e
	req := TriageRequest{RoutineID: job.RoutineID, ItemID: job.Item.ID, Action: job.Step.Kind(), Expect: job.Local}

	if job.Step.Snooze != nil {
		req.Days = job.Step.Snooze.SnoozeDays()
	}

	_, err := e.ledger.q.GetStepEffect(ctx, queries.GetStepEffectParams{MatchID: job.MatchID, Step: int64(job.Index)})

	switch {
	case err == nil:
		return Result{Summary: effectWords(req.Action, req.Days)}, nil
	case !errors.Is(err, sql.ErrNoRows):
		return Result{}, fmt.Errorf("read the step's effect: %w", err)
	}

	change, err := e.triage.Apply(ctx, req, func(ctx context.Context, tx *sql.Tx, change TriageChange) error {
		return recordEffect(ctx, e.ledger.q.WithTx(tx), job, req, change, e.clock.Now())
	})

	switch {
	case errors.Is(err, errLeftAlone):
		return Result{Summary: leftAloneWords}, nil
	case err != nil:
		return Result{}, err
	case !change.Changed():
		return Result{Summary: keptWords(change.Kept)}, nil
	}

	return Result{Summary: effectWords(req.Action, req.Days)}, nil
}

// recordEffect records, through q, in the triage's own transaction, what job's step does to
// its item, at now; it refuses (errLeftAlone) while the routine leaves the item alone, which
// rolls the change back.
func recordEffect(ctx context.Context, q *queries.Queries, job Job, req TriageRequest, change TriageChange,
	now time.Time,
) error {
	n, err := q.CountHandsOff(ctx, queries.CountHandsOffParams{RoutineID: job.RoutineID, ItemID: job.Item.ID})
	if err != nil {
		return fmt.Errorf("read whether the routine leaves the item alone: %w", err)
	}

	if n > 0 {
		return errLeftAlone
	}

	_, err = q.InsertEffect(ctx, queries.InsertEffectParams{
		MatchID: job.MatchID, Step: int64(job.Index), RoutineID: job.RoutineID, ItemID: new(job.Item.ID),
		Action: string(req.Action), StateBefore: change.Before, StateAfter: change.After, AppliedAt: now,
	})

	return err
}

// leftAloneWords says a step changed nothing since its routine leaves the item alone.
const leftAloneWords = "Left alone: you took back what it did here."

// effectWords says what a triage step did.
func effectWords(action catalog.StepKind, days int) string {
	if action == catalog.StepSnooze {
		return "Snoozed it for " + textutil.Counted(days, "day", "days") + "."
	}

	return map[catalog.StepKind]string{
		catalog.StepPin: "Pinned it.", catalog.StepDismiss: "Dismissed it.", catalog.StepRestore: "Brought it back.",
	}[action]
}

// keptWords says why a triage step changed nothing.
func keptWords(k TriageKept) string {
	switch k {
	case KeptPinned:
		return "It was pinned already."
	case KeptDismissed:
		return "It was dismissed already."
	case KeptYours:
		return "You set it aside yourself, so it's left as you left it."
	case KeptNotSetAside:
		return "It wasn't snoozed or dismissed."
	case KeptChanged:
		return "Not done: the item changed since this matched."
	default:
		return "Nothing changed."
	}
}

// Undone is what an Undo did with the effects it was given.
type Undone struct {
	// PutBack are the effects whose items it put back as they were before; Left those
	// whose items changed since (the user's own triage, a snooze the wake rules ended, a
	// later step), which it left as they are; and Skipped those undone or taken back
	// already, whose item is gone, or that aren't recorded.
	PutBack []int64 `json:"putBack"`
	Left    []int64 `json:"left"`
	Skipped []int64 `json:"skipped"`

	// Words says it, for a toast: "Put back 5 items. 1 changed since, so it's left as it
	// is."
	Words string `json:"words"`
}

// emptyUndone is an Undone with no list nil.
func emptyUndone() Undone {
	return Undone{PutBack: []int64{}, Left: []int64{}, Skipped: []int64{}}
}

// Undo takes back the local steps' effects ids: one, a match's (Match.Effects), or a
// grouped line's (ActivityGroup.Undo). Each item is put back as it was before its effect,
// but only where it is still as the effect left it; an item the user changed since stays
// as the user left it. Each routine it put an item back for leaves that item alone from
// then on, until Let it act again. A command the engine applies; no list in its result is
// nil.
func (s *Service) Undo(ctx context.Context, ids []int64) (Undone, error) {
	out := emptyUndone()

	e, err := s.engineOrErr()
	if err != nil {
		return out, err
	}

	if e.triage == nil {
		return out, uierr.New("This version can't put an item back.", ErrNoTriage)
	}

	err = e.do(ctx, func(ctx context.Context) error {
		got, undoErr := e.undo(ctx, ids)
		out = got

		return undoErr
	})

	return out, err
}

// undo takes back the effects ids, newest first, so an item two of them changed goes back
// to the earliest's state before: in one transaction, the items put back, the effects
// marked undone, and their routines left hands off the items.
func (e *Engine) undo(ctx context.Context, ids []int64) (Undone, error) {
	out := emptyUndone()

	ids = union(ids, nil)
	if len(ids) == 0 {
		out.Words = undoneWords(out)

		return out, nil
	}

	rows, err := e.ledger.q.ListEffects(ctx, ids)
	if err != nil {
		return out, uierr.New("What the routines did couldn't be read.", err)
	}

	picked := pickUndoable(rows, ids, &out)

	if len(picked) > 0 {
		if err = e.putBack(ctx, picked, &out); err != nil {
			return out, err
		}
	}

	slices.Sort(out.Skipped)
	out.Words = undoneWords(out)

	return out, nil
}

// pickUndoable returns the effects among rows Undo may still put back, and adds to out's
// Skipped those undone or taken back already, whose item is gone, and the ids not recorded.
func pickUndoable(rows []queries.AutopilotEffect, ids []int64, out *Undone) []queries.AutopilotEffect {
	picked := []queries.AutopilotEffect{}
	recorded := map[int64]bool{}

	for _, r := range rows {
		recorded[r.ID] = true

		if r.UndoneAt != nil || r.ReversedAt != nil || r.ItemID == nil {
			out.Skipped = append(out.Skipped, r.ID)
		} else {
			picked = append(picked, r)
		}
	}

	for _, id := range ids {
		if !recorded[id] {
			out.Skipped = append(out.Skipped, id)
		}
	}

	return picked
}

// putBack puts back what the effects picked changed, recording each one put back as undone
// and its routine hands off the item in the same transaction, and sorts them into out.
func (e *Engine) putBack(ctx context.Context, picked []queries.AutopilotEffect, out *Undone) error {
	now := e.clock.Now()

	undos := make([]TriageUndo, 0, len(picked))
	for _, r := range picked {
		undos = append(undos, undoOf(r))
	}

	put, err := e.triage.Undo(ctx, undos, func(ctx context.Context, tx *sql.Tx, put []bool) error {
		return recordUndone(ctx, e.ledger.q.WithTx(tx), picked, put, now)
	})
	if err != nil {
		return uierr.New("It couldn't be put back.", err)
	}

	for i, r := range picked {
		if i < len(put) && put[i] {
			out.PutBack = append(out.PutBack, r.ID)
		} else {
			out.Left = append(out.Left, r.ID)
		}
	}

	slices.Sort(out.PutBack)
	slices.Sort(out.Left)
	e.Wake()

	return nil
}

// recordUndone records, through q, each of the effects picked whose item was put back
// (put) as undone at now, and its routine hands off the item.
func recordUndone(ctx context.Context, q *queries.Queries, picked []queries.AutopilotEffect, put []bool, now time.Time,
) error {
	for i, r := range picked {
		if i >= len(put) || !put[i] {
			continue
		}

		if _, err := q.SetEffectUndone(ctx, queries.SetEffectUndoneParams{UndoneAt: new(now), ID: r.ID}); err != nil {
			return err
		}

		err := q.InsertHandsOff(ctx, queries.InsertHandsOffParams{
			RoutineID: r.RoutineID, ItemID: store.Deref(r.ItemID), Since: now, Why: whyUndone,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// KeptMarks returns every item whose routine's Restore Undo can still take back (neither
// undone nor reversed): the items whose Restore mark the inbox keeps however old, so an
// Undo Activity still offers can tell the item is as the Restore left it.
func (e *Engine) KeptMarks(ctx context.Context) ([]int64, error) {
	refs, err := e.ledger.q.ListUndoableItems(ctx, string(catalog.StepRestore))
	if err != nil {
		return nil, fmt.Errorf("read the routines' restores Undo can take back: %w", err)
	}

	out := make([]int64, 0, len(refs))
	for _, r := range refs {
		if r != nil {
			out = append(out, *r)
		}
	}

	return out, nil
}

// whyUndone is why a routine leaves an item alone once Undo put it back.
const whyUndone = "You undid what it did."

// undoOf is what Undo hands the triage for the effect r.
func undoOf(r queries.AutopilotEffect) TriageUndo {
	return TriageUndo{
		RoutineID: r.RoutineID, ItemID: store.Deref(r.ItemID), Action: catalog.StepKind(r.Action),
		Before: r.StateBefore, After: r.StateAfter,
	}
}

// undoneWords says what an Undo did.
func undoneWords(u Undone) string {
	var parts []string

	if len(u.PutBack) > 0 {
		parts = append(parts, "Put back "+textutil.Counted(len(u.PutBack), "item", "items")+".")
	}

	switch n := len(u.Left); {
	case n == 1:
		parts = append(parts, "1 changed since, so it's left as it is.")
	case n > 1:
		parts = append(parts, strconv.Itoa(n)+" changed since, so they're left as they are.")
	}

	if len(parts) == 0 {
		return "There was nothing left to undo."
	}

	return strings.Join(parts, " ")
}

// Reversal is what the user's own triage took back of a routine's: the routine, the item,
// and what the routine had done to it.
type Reversal struct {
	RoutineID int64
	ItemID    int64
	Action    catalog.StepKind
}

// reversalWords says why a routine leaves an item alone, once the user's triage, an outcome
// of kind, took back what it did (action).
func reversalWords(kind string, action catalog.StepKind) string {
	did := map[catalog.StepKind]string{
		catalog.StepPin: "pinned", catalog.StepSnooze: "snoozed", catalog.StepDismiss: "dismissed",
		catalog.StepRestore: "brought back",
	}[action]

	switch {
	case did == "":
		return "You took back what it did."
	case kind == "snoozed" && action == catalog.StepSnooze:
		return "You snoozed it again yourself."
	case kind == "restored":
		return "You brought back what it " + did + "."
	}

	return "You " + kind + " what it " + did + "."
}

// handOff records that the user's triage, an outcome of kind at at, took back what a
// routine did to an item: its effects of that part of the triage (the pin, or the
// dismissal and the snooze) applied by then are reversed, and it leaves the item alone,
// in one transaction, at now. A step of the routine's applied after the user's triage is
// no part of what the user took back, and keeps its Undo.
func (l *ledger) handOff(ctx context.Context, rev Reversal, kind string, at, now time.Time) error {
	actions := []string{string(catalog.StepSnooze), string(catalog.StepDismiss)}
	if rev.Action == catalog.StepPin {
		actions = []string{string(catalog.StepPin)}
	}

	if at.IsZero() {
		at = now
	}

	return l.inTx(ctx, func(q *queries.Queries) error {
		_, err := q.SetEffectsReversed(ctx, queries.SetEffectsReversedParams{
			ReversedAt: new(now), RoutineID: rev.RoutineID, ItemID: new(rev.ItemID), AppliedBefore: at,
			Actions: actions,
		})
		if err != nil {
			return fmt.Errorf("record what the user took back: %w", err)
		}

		return q.InsertHandsOff(ctx, queries.InsertHandsOffParams{
			RoutineID: rev.RoutineID, ItemID: rev.ItemID, Since: now, Why: reversalWords(kind, rev.Action),
		})
	})
}

// Effect is what one of a match's local steps did to its item's triage.
type Effect struct {
	ID      int64            `json:"id"`
	MatchID int64            `json:"matchId"`
	Step    int              `json:"step"`
	Action  catalog.StepKind `json:"action"`

	// AppliedAt is when it ran; UndoneAt when Undo put the item back, and ReversedAt when
	// the user's own triage took it back, each nil until then.
	AppliedAt  time.Time  `json:"appliedAt"`
	UndoneAt   *time.Time `json:"undoneAt"`
	ReversedAt *time.Time `json:"reversedAt"`

	// CanUndo reports that Undo would put the item back now: it is still as the step left
	// it, and this copy runs the engine.
	CanUndo bool `json:"canUndo"`
}

// effectsOf reads the effects of the matches ids, by match, with whether Undo can take each
// back now.
func (s *Service) effectsOf(ctx context.Context, l *ledger, ids []int64) (map[int64][]Effect, error) {
	out := map[int64][]Effect{}

	rows, err := l.q.ListMatchEffects(ctx, ids)
	if err != nil {
		return nil, uierr.New("What the routines did couldn't be read.", err)
	}

	undoable := s.undoable(ctx, rows)

	for i, r := range rows {
		out[r.MatchID] = append(out[r.MatchID], Effect{
			ID: r.ID, MatchID: r.MatchID, Step: int(r.Step), Action: catalog.StepKind(r.Action), AppliedAt: r.AppliedAt,
			UndoneAt: r.UndoneAt, ReversedAt: r.ReversedAt, CanUndo: undoable[i],
		})
	}

	return out, nil
}

// undoable reports, for each of rows, whether Undo would put its item back now: neither
// undone nor taken back, its item still as it left it, and an engine here to undo it. A
// read that fails is logged, and says none.
func (s *Service) undoable(ctx context.Context, rows []queries.AutopilotEffect) []bool {
	out := make([]bool, len(rows))
	if s.engine == nil || s.engine.triage == nil {
		return out
	}

	undos, at := pending(rows)
	if len(undos) == 0 {
		return out
	}

	left, err := s.engine.triage.AsLeft(ctx, undos)
	if err != nil {
		s.logger.InfoContext(ctx, "could not read whether the routines' changes can be undone", "error", err)

		return out
	}

	for j, i := range at {
		out[i] = j < len(left) && left[j]
	}

	return out
}

// pending returns the undos of the effects among rows neither undone nor taken back, of an
// item still there, and where each is among rows.
func pending(rows []queries.AutopilotEffect) ([]TriageUndo, []int) {
	var (
		undos []TriageUndo
		at    []int
	)

	for i, r := range rows {
		if r.UndoneAt == nil && r.ReversedAt == nil && r.ItemID != nil {
			undos, at = append(undos, undoOf(r)), append(at, i)
		}
	}

	return undos, at
}

// ActivityGroup is one line of Activity for a routine's local steps of one kind on one
// local day ("Snoozed 6 · Undo"), which stands for the matches it covers.
type ActivityGroup struct {
	// Key names the line across pages, whose matches may fall on two: the day, the
	// routine, and the step.
	Key string `json:"key"`

	RoutineID int64            `json:"routineId"`
	Routine   string           `json:"routine"`
	Action    catalog.StepKind `json:"action"`

	// Words says it: "Snoozed 6".
	Words string `json:"words"`

	// Count is how many changes it holds; Undone how many Undo put back, and Reversed how
	// many the user took back themselves.
	Count    int `json:"count"`
	Undone   int `json:"undone"`
	Reversed int `json:"reversed"`

	// Undo holds the effects Undo would still put back now: what its Undo sends.
	Undo []int64 `json:"undo"`

	// Matches are the matches it covers, and Items their items, newest first.
	Matches []int64   `json:"matches"`
	Items   []ItemRef `json:"items"`

	// Latest is when the newest of its steps ran.
	Latest time.Time `json:"latest"`
}

// LeftAlone is an item a routine leaves alone, since the user took back what it did there:
// Activity's "Left alone" line, with Let it act again.
type LeftAlone struct {
	RoutineID int64   `json:"routineId"`
	Routine   string  `json:"routine"`
	Item      ItemRef `json:"item"`

	// Why says what the user did: "You brought back what it snoozed."
	Why string `json:"why"`

	Since time.Time `json:"since"`
}

// maxLeftAlone caps the items Activity says the routines leave alone.
const maxLeftAlone = 200

// leftAlone reads the items the routines leave alone, newest first.
func (s *Service) leftAlone(ctx context.Context, l *ledger) ([]LeftAlone, error) {
	rows, err := l.q.ListLeftAlone(ctx, maxLeftAlone)
	if err != nil {
		return nil, uierr.New("The items the routines leave alone couldn't be read.", err)
	}

	named, err := routineNames(ctx, l)
	if err != nil {
		return nil, err
	}

	out := make([]LeftAlone, 0, len(rows))

	for _, r := range rows {
		out = append(out, LeftAlone{
			RoutineID: r.RoutineID, Routine: named[r.RoutineID], Why: r.Why, Since: r.Since,
			Item: ItemRef{
				ID: r.ItemID, Kind: item.Kind(store.Deref(r.Kind)), Repo: store.Deref(r.RepoOwner) + "/" + store.Deref(r.RepoName),
				Number: int(store.Deref(r.Number)), Title: store.Deref(r.Title), URL: store.Deref(r.URL),
			},
		})
	}

	return out, nil
}

// groupLocal adds to days the grouped lines of the local steps among views, each on the
// local day its step ran, in loc, newest first; a day with no match gets its own entry.
func groupLocal(days []ActivityDay, views []Match, loc *time.Location) []ActivityDay {
	type line struct {
		date string
		g    *ActivityGroup
	}

	var lines []line

	byKey := map[string]*ActivityGroup{}

	for _, v := range views {
		for _, eff := range v.Effects {
			date := eff.AppliedAt.In(loc).Format(time.DateOnly)
			key := date + "|" + strconv.FormatInt(v.RoutineID, 10) + "|" + string(eff.Action)

			g, ok := byKey[key]
			if !ok {
				g = &ActivityGroup{
					Key: key, RoutineID: v.RoutineID, Routine: v.Routine, Action: eff.Action, Undo: []int64{},
					Matches: []int64{}, Items: []ItemRef{},
				}
				byKey[key] = g
				lines = append(lines, line{date: date, g: g})
			}

			g.add(v, eff)
		}
	}

	for _, ln := range lines {
		ln.g.Words = groupVerb(ln.g.Action) + " " + strconv.Itoa(ln.g.Count)
		days = withGroup(days, ln.date, *ln.g)
	}

	return days
}

// add counts eff, a step of the match v, into the line.
func (g *ActivityGroup) add(v Match, eff Effect) {
	g.Count++

	switch {
	case eff.UndoneAt != nil:
		g.Undone++
	case eff.ReversedAt != nil:
		g.Reversed++
	case eff.CanUndo:
		g.Undo = append(g.Undo, eff.ID)
	}

	if !slices.Contains(g.Matches, v.ID) {
		g.Matches = append(g.Matches, v.ID)
		g.Items = append(g.Items, v.Item)
	}

	if eff.AppliedAt.After(g.Latest) {
		g.Latest = eff.AppliedAt
	}
}

// groupVerb names what a line's steps did.
func groupVerb(action catalog.StepKind) string {
	if verb, ok := map[catalog.StepKind]string{
		catalog.StepPin: "Pinned", catalog.StepSnooze: "Snoozed", catalog.StepDismiss: "Dismissed",
		catalog.StepRestore: "Restored",
	}[action]; ok {
		return verb
	}

	return textutil.Capitalize(string(action))
}

// withGroup adds g to the day date among days, newest first, adding the day when it has no
// entry yet.
func withGroup(days []ActivityDay, date string, g ActivityGroup) []ActivityDay {
	for i := range days {
		switch {
		case days[i].Date == date:
			days[i].Groups = append(days[i].Groups, g)

			return days
		case days[i].Date < date:
			return slices.Insert(days, i, ActivityDay{Date: date, Matches: []Match{}, Groups: []ActivityGroup{g}})
		}
	}

	return append(days, ActivityDay{Date: date, Matches: []Match{}, Groups: []ActivityGroup{g}})
}
