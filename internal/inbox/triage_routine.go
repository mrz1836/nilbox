package inbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/store"
)

// A routine's triage: Autopilot's local steps (Pin, Snooze, Dismiss, and Restore) run the
// inbox's own triage on an item, marked with the routine that did it
// (item_local_state.set_aside_by for a snooze, a dismissal, or a restore, and pinned_by for
// a pin), and an Undo puts an item back as it was before one, only where the item is still
// as the step left it. A routine's Snooze snoozes as the user's does, under the user's wake
// rules, for the routine's length. A routine never replaces what the user set aside
// themselves: its Snooze and Dismiss leave an item the user dismissed or snoozed as the
// user left it, and its Pin leaves a pinned item as it is. Autopilot reaches these through
// Routines, the inbox's face the window never reaches; core adapts it.
//
// Every triage path of the user's own clears the attribution it changes (triage.go), so the
// window never says a routine did what the user did last, and one that takes back what a
// routine did (a restore of what it set aside, a snooze over it, a dismissal of what it
// brought back, an unpin of what it pinned) tells the outcome sink so (Outcome.Reversed).

// TriageAction is one of a routine's triage steps.
type TriageAction string

// The triage steps a routine runs.
const (
	TriagePin     TriageAction = "pin"
	TriageSnooze  TriageAction = "snooze"
	TriageDismiss TriageAction = "dismiss"
	TriageRestore TriageAction = "restore"
)

// Valid reports one of the triage steps.
func (a TriageAction) Valid() bool {
	switch a {
	case TriagePin, TriageSnooze, TriageDismiss, TriageRestore:
		return true
	default:
		return false
	}
}

// ErrTriageAction is a routine's triage step the inbox doesn't know.
var ErrTriageAction = errors.New("not a triage step")

// TriageKept says why a routine's triage changed nothing.
type TriageKept string

// Why a routine's triage changed nothing.
const (
	// KeptPinned is an item that was pinned already.
	KeptPinned TriageKept = "pinned"

	// KeptDismissed is an item a routine had dismissed already.
	KeptDismissed TriageKept = "dismissed"

	// KeptYours is an item the user had dismissed or snoozed themselves, which a routine's
	// Snooze or Dismiss leaves as the user left it.
	KeptYours TriageKept = "yours"

	// KeptNotSetAside is an item neither dismissed nor snoozed, which a Restore has nothing
	// to bring back from.
	KeptNotSetAside TriageKept = "not_set_aside"
)

// LocalState is an item's triage on this Mac, as item_local_state holds it, and as a
// routine's triage records it before and after its change (as JSON, TriageChange): the zero
// value is an item with none. SnoozeBaseline and WakeReasons are the columns' own JSON.
type LocalState struct {
	DismissedAt    *time.Time `json:"dismissedAt,omitempty"`
	SnoozedUntil   *time.Time `json:"snoozedUntil,omitempty"`
	SnoozeBaseline *string    `json:"snoozeBaseline,omitempty"`
	WakeReasons    *string    `json:"wakeReasons,omitempty"`
	PinnedAt       *time.Time `json:"pinnedAt,omitempty"`
	SetAsideBy     *int64     `json:"setAsideBy,omitempty"`
	PinnedBy       *int64     `json:"pinnedBy,omitempty"`
}

// setAside reports an item dismissed, or snoozed past now.
func (l LocalState) setAside(now time.Time) bool {
	return l.DismissedAt != nil || l.SnoozedUntil != nil && l.SnoozedUntil.After(now)
}

// yours reports an item the user set aside themselves, still set aside at now.
func (l LocalState) yours(now time.Time) bool {
	return l.setAside(now) && l.SetAsideBy == nil
}

// restoredBy returns the routine that brought the item back from a dismissal or a snooze,
// while nothing set it aside since, or 0.
func (l LocalState) restoredBy() int64 {
	if l.DismissedAt != nil || l.SnoozedUntil != nil {
		return 0
	}

	return store.Deref(l.SetAsideBy)
}

// setAsideBy returns the routine whose dismissal or snooze of the item is in effect at
// now, and what it did, or 0.
func (l LocalState) setAsideBy(now time.Time) (int64, TriageAction) {
	switch {
	case !l.setAside(now) || l.SetAsideBy == nil:
		return 0, ""
	case l.DismissedAt != nil:
		return *l.SetAsideBy, TriageDismiss
	default:
		return *l.SetAsideBy, TriageSnooze
	}
}

// RoutineTriage is a routine's triage step on one item.
type RoutineTriage struct {
	// Routine is the routine's ID, which the item's triage is marked with, and Item the
	// item's.
	Routine int64
	Item    int64

	// Action is the step, and Days how long a snooze lasts (snooze.MinDays to MaxDays).
	Action TriageAction
	Days   int

	// Record, when set, runs in the change's own transaction, before the change is
	// written, with what it does: Autopilot records its effect there, so the change and its
	// record are stored together or not at all. An error rolls the change back. It isn't
	// run for a step that changes nothing.
	Record func(ctx context.Context, tx *sql.Tx, change TriageChange) error
}

// TriageChange is what a routine's triage step did to an item's local state: its state
// before and after, as JSON (LocalState), and, for a step that changed nothing, why.
type TriageChange struct {
	Before string
	After  string
	Kept   TriageKept
}

// Changed reports a step that changed the item's triage.
func (c TriageChange) Changed() bool {
	return c.Kept == ""
}

// TriageUndo is one of a routine's triage changes to take back: the routine, the item, the
// step, and its TriageChange's Before and After.
type TriageUndo struct {
	Routine int64
	Item    int64
	Action  TriageAction
	Before  string
	After   string
}

// Triage runs a routine's triage step on one item, through the inbox's own triage, and
// returns what it did; the window hears the inbox changed when it changed something.
func (r *Routines) Triage(ctx context.Context, t RoutineTriage) (TriageChange, error) {
	return r.svc.routineTriage(ctx, t)
}

// UndoTriage puts back, in one transaction, the local state each of undos found before its
// change, only where the item is still as the change left it, in the order given (newest
// first, so an item two changes moved goes back to the earliest's state before), and
// reports which it put back. record, when set, runs in the same transaction once they are
// written, with which were put back: Autopilot records the Undo there. An item gone, or
// changed since, is left as it is.
func (r *Routines) UndoTriage(
	ctx context.Context, undos []TriageUndo, record func(ctx context.Context, tx *sql.Tx, put []bool) error,
) ([]bool, error) {
	return r.svc.undoTriage(ctx, undos, record)
}

// AsLeft reports, for each of undos, whether its item is still as its change left it, so
// an Undo would put it back. It reads the database alone.
func (r *Routines) AsLeft(ctx context.Context, undos []TriageUndo) ([]bool, error) {
	return r.svc.asLeft(ctx, undos)
}

// routineTriage runs t: it reads the item's local state, works out the change, records it
// through t.Record, writes it, and tells the window. A Restore that brought the item back
// tells the outcome sink, as the user's does.
func (s *Service) routineTriage(ctx context.Context, t RoutineTriage) (TriageChange, error) {
	if err := checkRoutineTriage(t); err != nil {
		return TriageChange{}, err
	}

	now := s.now()
	until := s.clock.Now().AddDate(0, 0, t.Days).UTC().Truncate(time.Second)

	var (
		change TriageChange
		told   []Outcome
	)

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		var applyErr error

		change, told, applyErr = s.applyTriage(ctx, tx, t, now, until)

		return applyErr
	})
	if err != nil {
		return TriageChange{}, fmt.Errorf("%s the item for a routine: %w", t.Action, err)
	}

	if change.Changed() {
		s.snapshots.empty()
		s.notify()
	}

	s.settle(ctx, told)

	return change, nil
}

// applyTriage is routineTriage's transaction, tx: it reads the item's state, works out the
// change, records it through t.Record, and writes it, at now, a snooze lasting until until.
// It returns the change, and what it tells the outcome sink.
func (s *Service) applyTriage(ctx context.Context, tx *sql.Tx, t RoutineTriage, now, until time.Time,
) (TriageChange, []Outcome, error) {
	q := s.q.WithTx(tx)

	before, err := localStateOf(ctx, q, t.Item)
	if err != nil {
		return TriageChange{}, nil, err
	}

	baseline, err := snoozeBaseline(ctx, q, t)
	if err != nil {
		return TriageChange{}, nil, err
	}

	after, kept := decideTriage(t, before, now, until, baseline)

	change, err := triageChange(before, after, kept)
	if err != nil || !change.Changed() {
		return change, nil, err
	}

	told, err := s.routineTold(ctx, q, t, now)
	if err != nil {
		return TriageChange{}, nil, err
	}

	if t.Record != nil {
		if err = t.Record(ctx, tx, change); err != nil {
			return TriageChange{}, nil, err
		}
	}

	if err = writeTriage(ctx, q, t, after, now); err != nil {
		return TriageChange{}, nil, err
	}

	return change, told, q.PruneState(ctx, t.Item)
}

// snoozeBaseline is the activity a routine's snooze of t's item compares with (see
// activityBaseline), nil for any other step.
func snoozeBaseline(ctx context.Context, q *queries.Queries, t RoutineTriage) (*string, error) {
	if t.Action != TriageSnooze {
		return nil, nil //nolint:nilnil // only a snooze compares with the item's activity
	}

	return activityBaseline(ctx, q, t.Item)
}

// routineTold is what a routine's step t, which changes its item, tells the outcome sink: a
// Restore, which brings the item back, tells a restore, as the user's does, so the mirror of
// the GitHub notifications takes a dismissal's clears back; the other steps tell nothing. It
// reads nothing while no sink listens.
func (s *Service) routineTold(ctx context.Context, q *queries.Queries, t RoutineTriage, now time.Time,
) ([]Outcome, error) {
	if t.Action != TriageRestore {
		return nil, nil
	}

	was, found, err := s.triageBefore(ctx, q, t.Item)
	if err != nil || !found {
		return nil, err
	}

	return triageOutcome(OutcomeRestored, was, now), nil
}

// checkRoutineTriage refuses a step the inbox can't run: one it doesn't know, of no
// routine, or a snooze of a length the user couldn't choose.
func checkRoutineTriage(t RoutineTriage) error {
	switch {
	case !t.Action.Valid():
		return fmt.Errorf("%w: %q", ErrTriageAction, t.Action)
	case t.Routine <= 0:
		return fmt.Errorf("%w: a routine's triage names no routine", ErrTriageAction)
	case t.Action == TriageSnooze && !snooze.ValidDays(t.Days):
		return fmt.Errorf("%w: %d days; choose %d to %d", snooze.ErrInvalidDays, t.Days, snooze.MinDays, snooze.MaxDays)
	}

	return nil
}

// decideTriage works out what t does to an item whose local state is before, at now: the
// state after, or why it changes nothing. A snooze lasts until until, compared with
// baseline; the routine marks what it set.
func decideTriage(t RoutineTriage, before LocalState, now, until time.Time, baseline *string) (LocalState, TriageKept) {
	after := before
	routine := new(t.Routine)

	switch t.Action {
	case TriagePin:
		if before.PinnedAt != nil {
			return before, KeptPinned
		}

		after.PinnedAt, after.PinnedBy = new(now), routine
	case TriageSnooze:
		if before.yours(now) {
			return before, KeptYours
		}

		after.DismissedAt, after.SnoozedUntil, after.SnoozeBaseline = nil, new(until), baseline
		after.WakeReasons, after.SetAsideBy = nil, routine
	case TriageDismiss:
		switch {
		case before.yours(now):
			return before, KeptYours
		case before.DismissedAt != nil:
			return before, KeptDismissed
		}

		after.DismissedAt, after.SnoozedUntil, after.SnoozeBaseline = new(now), nil, nil
		after.WakeReasons, after.SetAsideBy = nil, routine
	case TriageRestore:
		if !before.setAside(now) {
			return before, KeptNotSetAside
		}

		after.DismissedAt, after.SnoozedUntil, after.SnoozeBaseline = nil, nil, nil
		after.WakeReasons, after.SetAsideBy = nil, routine
	}

	return after, ""
}

// triageChange encodes a change from before to after, or why it changes nothing.
func triageChange(before, after LocalState, kept TriageKept) (TriageChange, error) {
	b, err := store.EncodeJSON(before)
	if err != nil {
		return TriageChange{}, err
	}

	a, err := store.EncodeJSON(after)
	if err != nil {
		return TriageChange{}, err
	}

	return TriageChange{Before: b, After: a, Kept: kept}, nil
}

// writeTriage writes the state after t's change.
func writeTriage(ctx context.Context, q *queries.Queries, t RoutineTriage, after LocalState, now time.Time) error {
	var (
		n   int64
		err error
	)

	switch t.Action {
	case TriagePin:
		n, err = q.SetPin(ctx, queries.SetPinParams{PinnedAt: after.PinnedAt, PinnedBy: after.PinnedBy, UpdatedAt: now, ID: t.Item})
	case TriageRestore:
		return q.Restore(ctx, queries.RestoreParams{SetAsideBy: after.SetAsideBy, UpdatedAt: now, ItemID: t.Item})
	case TriageSnooze, TriageDismiss:
		n, err = q.SetTriage(ctx, queries.SetTriageParams{
			DismissedAt: after.DismissedAt, SnoozedUntil: after.SnoozedUntil, SnoozeBaseline: after.SnoozeBaseline,
			SetAsideBy: after.SetAsideBy, UpdatedAt: now, ID: t.Item,
		})
	}

	switch {
	case err != nil:
		return err
	case n == 0:
		return fmt.Errorf("%w (id %d)", item.ErrNotFound, t.Item)
	}

	return nil
}

// undoTriage puts back what undos changed, where their items are still as they left
// them, and records it through record, in one transaction; then it tells the window.
func (s *Service) undoTriage(
	ctx context.Context, undos []TriageUndo, record func(ctx context.Context, tx *sql.Tx, put []bool) error,
) ([]bool, error) {
	put := make([]bool, len(undos))
	now := s.now()

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		for i, u := range undos {
			done, err := undoOne(ctx, q, u, now)
			if err != nil {
				return err
			}

			put[i] = done
		}

		if record != nil {
			return record(ctx, tx, put)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("undo a routine's triage: %w", err)
	}

	if slices.Contains(put, true) {
		s.snapshots.empty()
		s.notify()
	}

	return put, nil
}

// undoOne puts back what u changed, when its item is still as u left it, and reports
// whether it did. An item gone, or changed since, is left as it is.
func undoOne(ctx context.Context, q *queries.Queries, u TriageUndo, now time.Time) (bool, error) {
	if !u.Action.Valid() {
		return false, fmt.Errorf("%w: %q", ErrTriageAction, u.Action)
	}

	cur, err := localStateOf(ctx, q, u.Item)
	if errors.Is(err, item.ErrNotFound) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	before, after, err := decodeChange(u)
	if err != nil {
		return false, err
	}

	if !asLeftBy(u.Action, cur, after) {
		return false, nil
	}

	if err = putBack(ctx, q, u, before, now); err != nil {
		return false, err
	}

	return true, q.PruneState(ctx, u.Item)
}

// putBack writes the part of before that u's step changed: the pin, or the dismissal and
// the snooze. A routine before names that is gone for good is dropped from it.
func putBack(ctx context.Context, q *queries.Queries, u TriageUndo, before LocalState, now time.Time) error {
	write := func(by *int64) error {
		if u.Action == TriagePin {
			_, err := q.SetPin(ctx, queries.SetPinParams{PinnedAt: before.PinnedAt, PinnedBy: by, UpdatedAt: now, ID: u.Item})

			return err
		}

		_, err := q.PutSetAside(ctx, queries.PutSetAsideParams{
			DismissedAt: before.DismissedAt, SnoozedUntil: before.SnoozedUntil, SnoozeBaseline: before.SnoozeBaseline,
			WakeReasons: before.WakeReasons, SetAsideBy: by, UpdatedAt: now, ID: u.Item,
		})

		return err
	}

	by := before.SetAsideBy
	if u.Action == TriagePin {
		by = before.PinnedBy
	}

	err := write(by)
	if by != nil && store.IsForeignKeyViolation(err) {
		err = write(nil)
	}

	return err
}

// asLeft reports, for each of undos, whether its item is still as its change left it.
func (s *Service) asLeft(ctx context.Context, undos []TriageUndo) ([]bool, error) {
	out := make([]bool, len(undos))
	if len(undos) == 0 {
		return out, nil
	}

	ids := make([]int64, 0, len(undos))
	for _, u := range undos {
		ids = append(ids, u.Item)
	}

	rows, err := s.q.ListLocalStates(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read the items' triage: %w", err)
	}

	byID := make(map[int64]LocalState, len(rows))
	for _, r := range rows {
		byID[r.ID] = LocalState{
			DismissedAt: r.DismissedAt, SnoozedUntil: r.SnoozedUntil, SnoozeBaseline: r.SnoozeBaseline,
			WakeReasons: r.WakeReasons, PinnedAt: r.PinnedAt, SetAsideBy: r.SetAsideBy, PinnedBy: r.PinnedBy,
		}
	}

	for i, u := range undos {
		cur, found := byID[u.Item]
		if !found || !u.Action.Valid() {
			continue
		}

		_, after, decodeErr := decodeChange(u)
		out[i] = decodeErr == nil && asLeftBy(u.Action, cur, after)
	}

	return out, nil
}

// asLeftBy reports whether cur is still as a step of action left it (after): the pin and
// who pinned it, or the dismissal, the snooze's end, and who set them. A snooze the wake
// rules ended, a later step, or any triage of the user's own changed it.
func asLeftBy(action TriageAction, cur, after LocalState) bool {
	if action == TriagePin {
		return sameTime(cur.PinnedAt, after.PinnedAt) && sameID(cur.PinnedBy, after.PinnedBy)
	}

	return sameTime(cur.DismissedAt, after.DismissedAt) && sameTime(cur.SnoozedUntil, after.SnoozedUntil) &&
		sameID(cur.SetAsideBy, after.SetAsideBy)
}

// sameTime reports two times the same, nil alike.
func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}

	return a.Equal(*b)
}

// sameID reports two IDs the same, nil alike.
func sameID(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}

// decodeChange reads an undo's states before and after.
func decodeChange(u TriageUndo) (LocalState, LocalState, error) {
	before, err := store.DecodeJSON[LocalState](u.Before)
	if err != nil {
		return LocalState{}, LocalState{}, fmt.Errorf("read the triage before a routine's step: %w", err)
	}

	after, err := store.DecodeJSON[LocalState](u.After)
	if err != nil {
		return LocalState{}, LocalState{}, fmt.Errorf("read the triage a routine's step left: %w", err)
	}

	return before, after, nil
}

// localStateOf reads the local state of the item with id, the zero LocalState when it has
// none, or item.ErrNotFound when no item has id.
func localStateOf(ctx context.Context, q *queries.Queries, id int64) (LocalState, error) {
	r, err := q.GetLocalState(ctx, id)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return LocalState{}, fmt.Errorf("%w (id %d)", item.ErrNotFound, id)
	case err != nil:
		return LocalState{}, fmt.Errorf("read the item's triage: %w", err)
	}

	return LocalState{
		DismissedAt: r.DismissedAt, SnoozedUntil: r.SnoozedUntil, SnoozeBaseline: r.SnoozeBaseline,
		WakeReasons: r.WakeReasons, PinnedAt: r.PinnedAt, SetAsideBy: r.SetAsideBy, PinnedBy: r.PinnedBy,
	}, nil
}
