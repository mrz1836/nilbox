package inbox

import (
	"context"
	"database/sql"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github/githubtest"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// ledgerKey is the setting a test's Record writes in a routine's triage's transaction, as
// Autopilot records its effect there.
const ledgerKey = "test.ledger"

// ledgered returns what a test's Record wrote, and whether it is stored.
func (h *harness) ledgered(t *testing.T) (string, bool) {
	t.Helper()

	v, ok, err := prefs.New(h.pool, h.clock).Get(t.Context(), ledgerKey)
	require.NoError(t, err)

	return v, ok
}

// writeLedger is a Record that writes the change's After in the change's own transaction.
func writeLedger(ctx context.Context, tx *sql.Tx, change TriageChange) error {
	return prefs.New(nil, nil).WithTx(tx).Set(ctx, ledgerKey, change.After)
}

func TestARoutinesTriageAndItsRecordAreStoredTogetherOrNotAtAll(t *testing.T) {
	t.Parallel()

	h, id, other := triageHarness(t)
	r := h.routine(t, "Tidy")
	routines := NewRoutines(h.svc)

	refuse(t, h, "BEFORE INSERT ON item_local_state", "")

	notified := h.events.Count()

	for _, action := range []TriageAction{TriagePin, TriageSnooze, TriageDismiss} {
		ran := false

		_, err := routines.Triage(t.Context(), RoutineTriage{
			Routine: r, Item: id, Action: action, Days: 3,
			Record: func(ctx context.Context, tx *sql.Tx, change TriageChange) error {
				ran = true

				return writeLedger(ctx, tx, change)
			},
		})
		require.ErrorContains(t, err, string(action)+" the item for a routine", action)
		require.True(t, ran, "%s: the record runs before the change is written", action)

		_, stored := h.ledgered(t)
		assert.False(t, stored, "%s: the record the change can't be stored with is rolled back", action)
	}

	assert.Equal(t, LocalState{}, h.local(t, id), "nothing changed")
	assert.Equal(t, notified, h.events.Count(), "and the window is told nothing")

	h.exec(t, "DROP TRIGGER refused_write")

	got, err := routines.Triage(t.Context(), RoutineTriage{Routine: r, Item: other, Action: TriagePin, Record: writeLedger})
	require.NoError(t, err)

	ledger, stored := h.ledgered(t)
	require.True(t, stored)
	assert.Equal(t, got.After, ledger, "the change and its record are stored together")
}

func TestAnUndoThatCantBeStoredPutsNothingBack(t *testing.T) {
	t.Parallel()

	h, id, other := triageHarness(t)
	r := h.routine(t, "Tidy")

	pinned, _ := h.triage(t, r, id, TriagePin, 0)
	snoozed, _ := h.triage(t, r, other, TriageSnooze, 3)
	before := []LocalState{h.local(t, id), h.local(t, other)}

	refuse(t, h, "BEFORE INSERT ON item_local_state", "NEW.item_id = "+strconv.FormatInt(other, 10))

	recorded := false

	_, err := NewRoutines(h.svc).UndoTriage(t.Context(),
		[]TriageUndo{undoOf(r, id, TriagePin, pinned), undoOf(r, other, TriageSnooze, snoozed)},
		func(context.Context, *sql.Tx, []bool) error {
			recorded = true

			return nil
		})
	require.ErrorContains(t, err, "undo a routine's triage")
	assert.False(t, recorded, "the Undo is recorded only once it is written")
	after := []LocalState{h.local(t, id), h.local(t, other)}
	assert.Equal(t, before, after, "the pin it could put back is rolled back too")

	h.exec(t, "DROP TRIGGER refused_write")

	put, err := NewRoutines(h.svc).UndoTriage(t.Context(), []TriageUndo{undoOf(r, id, TriagePin, pinned)}, nil)
	require.NoError(t, err, "an Undo needs no record")
	assert.Equal(t, []bool{true}, put)
	assert.Equal(t, LocalState{}, h.local(t, id))
}

func TestAnUndoRefusesAChangeItCantRead(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)
	r := h.routine(t, "Tidy")
	routines := NewRoutines(h.svc)

	pinned, _ := h.triage(t, r, id, TriagePin, 0)

	for _, c := range []struct {
		undo TriageUndo
		want string
	}{
		{TriageUndo{Routine: r, Item: id, Action: TriagePin, Before: "{", After: pinned.After}, "read the triage before a routine's step"},
		{TriageUndo{Routine: r, Item: id, Action: TriagePin, Before: pinned.Before, After: "{"}, "read the triage a routine's step left"},
	} {
		_, err := routines.UndoTriage(t.Context(), []TriageUndo{c.undo}, nil)
		require.ErrorContains(t, err, c.want)
	}

	assert.Equal(t, r, *h.local(t, id).PinnedBy, "the item is left as the step left it")

	left, err := routines.AsLeft(t.Context(), []TriageUndo{
		undoOf(r, id, TriagePin, pinned),
		{Routine: r, Item: id, Action: TriagePin, Before: pinned.Before, After: "{"},
		{Routine: r, Item: id, Action: "archive", Before: pinned.Before, After: pinned.After},
		{Routine: r, Item: 999, Action: TriagePin, Before: pinned.Before, After: pinned.After},
	})
	require.NoError(t, err)
	assert.Equal(t, []bool{true, false, false, false}, left,
		"only an undo it can read, of a step it knows, on a stored item, would put it back")

	none, err := routines.AsLeft(t.Context(), nil)
	require.NoError(t, err)
	assert.NotNil(t, none)
	assert.Empty(t, none)
}

func TestARoutinesTriageReportsATriageItCantRead(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)
	r := h.routine(t, "Tidy")
	routines := NewRoutines(h.svc)

	pinned, _ := h.triage(t, r, id, TriagePin, 0)

	h.exec(t, "DROP TABLE item_local_state")

	_, err := routines.Triage(t.Context(), RoutineTriage{Routine: r, Item: id, Action: TriageDismiss})
	require.ErrorContains(t, err, "read the item's triage")

	_, err = routines.UndoTriage(t.Context(), []TriageUndo{undoOf(r, id, TriagePin, pinned)}, nil)
	require.ErrorContains(t, err, "read the item's triage")

	closed := NewRoutines(NewService(Config{Pool: testutil.ClosedDB(t), Tokens: githubtest.NewTokens(), Client: &fakeClient{}}))

	_, err = closed.LocalStates(t.Context(), []int64{id})
	require.ErrorContains(t, err, "read the items' triage")

	_, err = closed.AsLeft(t.Context(), []TriageUndo{undoOf(r, id, TriagePin, pinned)})
	require.ErrorContains(t, err, "read the items' triage")

	states, err := closed.LocalStates(t.Context(), nil)
	require.NoError(t, err, "no item, no read")
	assert.Empty(t, states)
}
