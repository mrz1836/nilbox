package inbox

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/store"
)

// routine stores an Autopilot routine named name, as its service would, and returns its ID:
// a routine's triage is marked with one.
func (h *harness) routine(t *testing.T, name string) int64 {
	t.Helper()

	now := h.clock.Now().UTC()

	res, err := h.pool.ExecContext(t.Context(), `INSERT INTO autopilot_routines (name, name_key, mode, spec, created_at,
		updated_at) VALUES (?, ?, 'auto', '{}', ?, ?)`, name, name, now, now)
	require.NoError(t, err)

	id, err := res.LastInsertId()
	require.NoError(t, err)

	return id
}

var (
	// errNoTx is a record handed no transaction.
	errNoTx = errors.New("no transaction")

	// errLedgerFull is a record that fails.
	errLedgerFull = errors.New("the ledger is full")
)

// recorded is what a routine's triage handed its Record, and whether it was asked.
type recorded struct {
	changes []TriageChange
}

// record keeps each change it is handed, in the change's transaction.
func (r *recorded) record(_ context.Context, tx *sql.Tx, c TriageChange) error {
	if tx == nil {
		return errNoTx
	}

	r.changes = append(r.changes, c)

	return nil
}

// triage runs a routine's step on the item, recording the change, and returns it.
func (h *harness) triage(t *testing.T, routine, id int64, action TriageAction, days int) (TriageChange, *recorded) {
	t.Helper()

	rec := &recorded{}

	got, err := NewRoutines(h.svc).Triage(t.Context(), RoutineTriage{
		Routine: routine, Item: id, Action: action, Days: days, Record: rec.record,
	})
	require.NoError(t, err)

	return got, rec
}

// local reads the item's local state.
func (h *harness) local(t *testing.T, id int64) LocalState {
	t.Helper()

	got, err := localStateOf(t.Context(), h.svc.q, id)
	require.NoError(t, err)

	return got
}

// decoded reads a change's state as JSON.
func decoded(t *testing.T, raw string) LocalState {
	t.Helper()

	got, err := store.DecodeJSON[LocalState](raw)
	require.NoError(t, err)

	return got
}

func TestARoutinesTriageIsMarkedWithTheRoutine(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)
	r := h.routine(t, "Tidy")
	notified := h.events.Count()

	pinned, rec := h.triage(t, r, id, TriagePin, 0)
	require.True(t, pinned.Changed())
	require.Len(t, rec.changes, 1, "the change is recorded in its own transaction")
	assert.Equal(t, pinned, rec.changes[0])
	assert.Equal(t, LocalState{}, decoded(t, pinned.Before), "nothing before")
	assert.Equal(t, r, *decoded(t, pinned.After).PinnedBy)
	assert.Equal(t, notified+1, h.events.Count(), "the window hears the inbox changed")

	snoozed, _ := h.triage(t, r, id, TriageSnooze, 3)
	require.True(t, snoozed.Changed())

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)

	it := itemByNumber(t, snap.ReviewRequested, 1)
	assert.Equal(t, "2026-01-13T12:00:00Z", it.SnoozedUntil, "the routine's length, at the same time of day")
	assert.Equal(t, r, it.SetAsideBy, "marked with the routine that snoozed it")
	assert.Equal(t, r, it.PinnedBy, "and the one that pinned it")
	assert.NotEmpty(t, it.PinnedAt)

	dismissed, _ := h.triage(t, r, id, TriageDismiss, 0)
	require.True(t, dismissed.Changed(), "a routine's dismissal replaces its own snooze")
	assert.NotNil(t, h.local(t, id).DismissedAt)
	assert.Nil(t, h.local(t, id).SnoozedUntil)

	restored, _ := h.triage(t, r, id, TriageRestore, 0)
	require.True(t, restored.Changed())

	after := h.local(t, id)
	assert.Nil(t, after.DismissedAt)
	assert.Nil(t, after.SnoozedUntil)
	assert.Equal(t, r, *after.SetAsideBy, "the routine that brought it back is kept, for an Undo")
	assert.Equal(t, decoded(t, restored.After), after)
}

func TestARoutinesSnoozeFollowsTheWakeRules(t *testing.T) {
	t.Parallel()

	h, id, details := wakeHarness(t)
	r := h.routine(t, "Snooze bots")

	snoozed, _ := h.triage(t, r, id, TriageSnooze, 3)
	require.NotNil(t, decoded(t, snoozed.After).SnoozeBaseline, "it records the activity it compares with")
	assert.NotNil(t, h.baseline(t, id))

	details.Commits += 2
	details.HeadOID = "2222222222222222222222222222222222222222"
	it := h.later(t, details)

	require.NotNil(t, it.WokeBy, "new commits end a routine's snooze, as the user's")
	assert.Equal(t, snooze.Reasons{Commits: 2}, *it.WokeBy)
	assert.Equal(t, r, it.SetAsideBy)
}

func TestARoutineNeverReplacesWhatTheUserSetAside(t *testing.T) {
	t.Parallel()

	h, id, other := triageHarness(t)
	r := h.routine(t, "Tidy")
	ctx := t.Context()

	_, err := h.svc.Dismiss(ctx, id)
	require.NoError(t, err)

	for _, action := range []TriageAction{TriageSnooze, TriageDismiss} {
		got, rec := h.triage(t, r, id, action, 3)
		assert.Equal(t, KeptYours, got.Kept, action)
		assert.Empty(t, rec.changes, "%s: a change of nothing records nothing", action)
	}

	_, err = h.svc.Snooze(ctx, other, 7)
	require.NoError(t, err)

	got, _ := h.triage(t, r, other, TriageSnooze, 1)
	assert.Equal(t, KeptYours, got.Kept, "the user's snooze keeps its own length")
	assert.Equal(t, "2026-01-17T12:00:00Z", h.local(t, other).SnoozedUntil.UTC().Format(time.RFC3339))

	_, err = h.svc.Pin(ctx, other)
	require.NoError(t, err)

	got, _ = h.triage(t, r, other, TriagePin, 0)
	assert.Equal(t, KeptPinned, got.Kept)
	assert.Nil(t, h.local(t, other).PinnedBy, "the user's pin stays the user's")

	// A routine's Restore brings back what the user set aside: that is what it is for.
	got, _ = h.triage(t, r, id, TriageRestore, 0)
	assert.True(t, got.Changed())

	got, _ = h.triage(t, r, id, TriageRestore, 0)
	assert.Equal(t, KeptNotSetAside, got.Kept, "nothing left to bring back")

	// What a routine set aside, a routine may set aside again; a dismissal stays one.
	got, _ = h.triage(t, r, id, TriageDismiss, 0)
	require.True(t, got.Changed())

	got, _ = h.triage(t, h.routine(t, "Another"), id, TriageDismiss, 0)
	assert.Equal(t, KeptDismissed, got.Kept)
}

func TestARoutinesTriageRefusesWhatItCantRun(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)
	r := h.routine(t, "Tidy")
	routines := NewRoutines(h.svc)

	for _, c := range []struct {
		name string
		t    RoutineTriage
		want error
	}{
		{"an unknown step", RoutineTriage{Routine: r, Item: id, Action: "archive"}, ErrTriageAction},
		{"no routine", RoutineTriage{Item: id, Action: TriagePin}, ErrTriageAction},
		{"a snooze too long", RoutineTriage{Routine: r, Item: id, Action: TriageSnooze, Days: 400}, snooze.ErrInvalidDays},
		{"an unknown item", RoutineTriage{Routine: r, Item: 999, Action: TriagePin}, item.ErrNotFound},
	} {
		_, err := routines.Triage(t.Context(), c.t)
		require.ErrorIs(t, err, c.want, c.name)
	}

	failing := RoutineTriage{
		Routine: r, Item: id, Action: TriagePin,
		Record: func(context.Context, *sql.Tx, TriageChange) error { return errLedgerFull },
	}

	_, err := routines.Triage(t.Context(), failing)
	require.ErrorIs(t, err, errLedgerFull)
	assert.Zero(t, h.count(t, "item_local_state"), "a record that fails stores no change")
}

func TestTheUsersOwnTriageClearsTheRoutinesMark(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)
	r := h.routine(t, "Tidy")
	ctx := t.Context()

	h.triage(t, r, id, TriagePin, 0)
	pinnedAt := h.local(t, id).PinnedAt

	_, err := h.svc.Pin(ctx, id)
	require.NoError(t, err)
	assert.Nil(t, h.local(t, id).PinnedBy, "the user's pin makes it theirs")
	assert.True(t, pinnedAt.Equal(*h.local(t, id).PinnedAt), "in the same place")

	_, err = h.svc.Unpin(ctx, id)
	require.NoError(t, err)

	h.triage(t, r, id, TriagePin, 0)
	require.Equal(t, r, *h.local(t, id).PinnedBy)

	_, err = h.svc.Unpin(ctx, id)
	require.NoError(t, err)
	assert.Nil(t, h.local(t, id).PinnedBy, "the user's unpin clears the routine's mark")

	for _, user := range []func() error{
		func() error { _, userErr := h.svc.Dismiss(ctx, id); return userErr },
		func() error { _, userErr := h.svc.Snooze(ctx, id, 2); return userErr },
		func() error { _, userErr := h.svc.Restore(ctx, id); return userErr },
	} {
		_, err = h.svc.Restore(ctx, id)
		require.NoError(t, err)

		h.triage(t, r, id, TriageSnooze, 3)
		require.Equal(t, r, *h.local(t, id).SetAsideBy)

		require.NoError(t, user())
		assert.Nil(t, h.local(t, id).SetAsideBy, "the user's own triage clears the routine's mark")
	}

	assert.Zero(t, h.count(t, "item_local_state"), "nothing is left in the row")
}

func TestTakingBackARoutinesTriageTellsWhichRoutine(t *testing.T) {
	t.Parallel()

	h, id, other := triageHarness(t)
	sink := h.telling(t)
	r := h.routine(t, "Tidy")
	ctx := t.Context()
	at := h.clock.Now().UTC()

	step := func(target int64, action TriageAction) {
		t.Helper()

		_, err := NewRoutines(h.svc).Triage(ctx, RoutineTriage{Routine: r, Item: target, Action: action, Days: 3})
		require.NoError(t, err)
	}

	step(id, TriageSnooze)
	_, err := h.svc.Restore(ctx, id)
	require.NoError(t, err)

	step(id, TriageDismiss)
	_, err = h.svc.Snooze(ctx, id, 1)
	require.NoError(t, err)

	step(other, TriagePin)
	_, err = h.svc.Unpin(ctx, other)
	require.NoError(t, err)

	// The user dismisses; a routine brings it back, which tells a restore; the user
	// dismisses it again, which takes the routine's Restore back.
	_, err = h.svc.Restore(ctx, id)
	require.NoError(t, err)
	_, err = h.svc.Dismiss(ctx, other)
	require.NoError(t, err)
	step(other, TriageRestore)
	_, err = h.svc.Dismiss(ctx, other)
	require.NoError(t, err)

	// Going further than a routine is no reversal: a dismissal over its snooze, and the
	// user's pin over its pin, take nothing back.
	step(id, TriageSnooze)
	_, err = h.svc.Dismiss(ctx, id)
	require.NoError(t, err)
	step(id, TriagePin)
	_, err = h.svc.Pin(ctx, id)
	require.NoError(t, err)

	back := func(kind OutcomeKind, ref int, target int64, did TriageAction) Outcome {
		o := told(kind, prRef(ref), at)
		o.Reversed = Reversal{Routine: r, Item: target, Did: did}

		return o
	}

	assert.Equal(t, [][]Outcome{
		{back(OutcomeRestored, 1, id, TriageSnooze)},
		{back(OutcomeSnoozed, 1, id, TriageDismiss)},
		{back(OutcomeUnpinned, 2, other, TriagePin)},
		{told(OutcomeRestored, prRef(1), at)},
		{told(OutcomeDismissed, prRef(2), at)},
		{told(OutcomeRestored, prRef(2), at)},
		{back(OutcomeDismissed, 2, other, TriageRestore)},
		{told(OutcomeDismissed, prRef(1), at)},
	}, sink.told())
}

func TestAnExpiredSnoozeOfARoutinesIsNoReversal(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)
	sink := h.telling(t)
	r := h.routine(t, "Tidy")

	_, err := NewRoutines(h.svc).Triage(t.Context(), RoutineTriage{Routine: r, Item: id, Action: TriageSnooze, Days: 1})
	require.NoError(t, err)

	h.clock.Advance(48 * time.Hour)

	_, err = h.svc.Snooze(t.Context(), id, 1)
	require.NoError(t, err)
	_, err = h.svc.Restore(t.Context(), id)
	require.NoError(t, err)

	assert.Equal(t, [][]Outcome{{told(OutcomeRestored, prRef(1), h.clock.Now())}}, sink.told(),
		"its snooze had ended: snoozing again agrees with it, and only the user's own snooze is brought back")
}

// undoOf is the undo of change, a routine's step on the item.
func undoOf(routine, id int64, action TriageAction, change TriageChange) TriageUndo {
	return TriageUndo{Routine: routine, Item: id, Action: action, Before: change.Before, After: change.After}
}

// undo puts back undos, and returns which it put back and what record was handed.
func (h *harness) undo(t *testing.T, undos ...TriageUndo) ([]bool, []bool) {
	t.Helper()

	var seen []bool

	put, err := NewRoutines(h.svc).UndoTriage(t.Context(), undos, func(_ context.Context, tx *sql.Tx, put []bool) error {
		require.NotNil(t, tx)

		seen = put

		return nil
	})
	require.NoError(t, err)

	return put, seen
}

func TestUndoPutsBackWhatTheRoutineChanged(t *testing.T) {
	t.Parallel()

	h, id, other := triageHarness(t)
	r := h.routine(t, "Tidy")

	snoozed, _ := h.triage(t, r, id, TriageSnooze, 3)
	pinned, _ := h.triage(t, r, other, TriagePin, 0)
	undos := []TriageUndo{undoOf(r, id, TriageSnooze, snoozed), undoOf(r, other, TriagePin, pinned)}

	left, err := NewRoutines(h.svc).AsLeft(t.Context(), undos)
	require.NoError(t, err)
	assert.Equal(t, []bool{true, true}, left)

	put, seen := h.undo(t, undos...)
	assert.Equal(t, []bool{true, true}, put)
	assert.Equal(t, put, seen, "the record hears which were put back")
	assert.Zero(t, h.count(t, "item_local_state"), "both are as they were: nothing")

	put, _ = h.undo(t, undos...)
	assert.Equal(t, []bool{false, false}, put, "a second Undo finds them changed since, and changes nothing")
}

func TestUndoLeavesWhatChangedSinceAsItIs(t *testing.T) {
	t.Parallel()

	h, id, other := triageHarness(t)
	r := h.routine(t, "Tidy")
	ctx := t.Context()

	snoozed, _ := h.triage(t, r, id, TriageSnooze, 3)
	_, err := h.svc.Snooze(ctx, id, 7)
	require.NoError(t, err)

	user := h.local(t, id)

	put, _ := h.undo(t, undoOf(r, id, TriageSnooze, snoozed))
	assert.Equal(t, []bool{false}, put, "the user snoozed it since")
	assert.Equal(t, user, h.local(t, id), "as the user left it")

	// Another routine's step since changed it too, and so does a newer step of the same
	// routine's: Undo of the older leaves it.
	first, _ := h.triage(t, r, other, TriageSnooze, 1)
	h.clock.Advance(time.Hour)
	second, _ := h.triage(t, r, other, TriageSnooze, 2)

	put, _ = h.undo(t, undoOf(r, other, TriageSnooze, first))
	assert.Equal(t, []bool{false}, put)

	// Newest first, both go back, to the state before the first.
	put, _ = h.undo(t, undoOf(r, other, TriageSnooze, second), undoOf(r, other, TriageSnooze, first))
	assert.Equal(t, []bool{true, true}, put)
	assert.Equal(t, LocalState{}, h.local(t, other))
}

func TestUndoPutsBackAnotherRoutinesStepItReplaced(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)
	first, second := h.routine(t, "Snooze"), h.routine(t, "Dismiss")

	h.triage(t, first, id, TriageSnooze, 3)
	before := h.local(t, id)

	dismissed, _ := h.triage(t, second, id, TriageDismiss, 0)

	put, _ := h.undo(t, undoOf(second, id, TriageDismiss, dismissed))
	assert.Equal(t, []bool{true}, put)
	assert.Equal(t, before, h.local(t, id), "the first routine's snooze is back, marked with it")

	// A routine deleted for good leaves no mark to put back.
	dismissed, _ = h.triage(t, second, id, TriageDismiss, 0)
	h.exec(t, `DELETE FROM autopilot_routines WHERE id = ?`, first)

	put, _ = h.undo(t, undoOf(second, id, TriageDismiss, dismissed))
	assert.Equal(t, []bool{true}, put)
	assert.Nil(t, h.local(t, id).SetAsideBy)
	assert.Equal(t, before.SnoozedUntil, h.local(t, id).SnoozedUntil)
}

func TestUndoOfARoutinesRestoreSetsTheItemAsideAgain(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)
	r := h.routine(t, "Bring back")

	_, err := h.svc.Dismiss(t.Context(), id)
	require.NoError(t, err)

	dismissed := h.local(t, id)

	restored, _ := h.triage(t, r, id, TriageRestore, 0)

	put, _ := h.undo(t, undoOf(r, id, TriageRestore, restored))
	assert.Equal(t, []bool{true}, put)
	assert.Equal(t, dismissed, h.local(t, id), "the user's dismissal is back")
}

func TestUndoOfAGoneItemOrAWokenSnoozeChangesNothing(t *testing.T) {
	t.Parallel()

	h, id, details := wakeHarness(t)
	r := h.routine(t, "Snooze bots")

	snoozed, _ := h.triage(t, r, id, TriageSnooze, 3)

	details.Commits += 2
	details.HeadOID = "2222222222222222222222222222222222222222"
	woken := h.later(t, details)
	require.NotNil(t, woken.WokeBy)

	put, _ := h.undo(t, undoOf(r, id, TriageSnooze, snoozed), TriageUndo{Routine: r, Item: 999, Action: TriagePin})
	assert.Equal(t, []bool{false, false}, put, "the wake rules ended the snooze, and no item has 999")

	_, err := NewRoutines(h.svc).UndoTriage(t.Context(), []TriageUndo{{Routine: r, Item: id, Action: "archive"}}, nil)
	require.ErrorIs(t, err, ErrTriageAction)
}
