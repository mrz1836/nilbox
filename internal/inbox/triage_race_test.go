package inbox

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The user's own triage against a routine's step on its way: what takes the routine's step
// back is told, and timed, so the engine never acts on the item as if it hadn't been.

func TestTheUsersTriageTellsWhatItTookBackBeforeAnythingWakes(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)
	sink := h.telling(t)
	r := h.routine(t, "Tidy")
	ctx := t.Context()

	// Each user path that takes back a routine's step tells the sink while the window, and
	// the engine its event wakes, have yet to hear the inbox changed.
	var early []bool

	before := h.events.Count()
	sink.then = func(context.Context) { early = append(early, h.events.Count() == before) }

	for _, c := range []struct {
		step TriageAction
		user func() error
	}{
		{TriageSnooze, func() error { _, err := h.svc.Restore(ctx, id); return err }},
		{TriageDismiss, func() error { _, err := h.svc.Snooze(ctx, id, 1); return err }},
		{TriagePin, func() error { _, err := h.svc.Unpin(ctx, id); return err }},
	} {
		before = h.events.Count()
		_, err := h.svc.Restore(ctx, id)
		require.NoError(t, err)

		_, err = NewRoutines(h.svc).Triage(ctx, RoutineTriage{Routine: r, Item: id, Action: c.step, Days: 3})
		require.NoError(t, err)

		before = h.events.Count()

		require.NoError(t, c.user())
		require.Greater(t, h.events.Count(), before, "the window hears of it after")
	}

	assert.Len(t, early, 4, "three reversals, and the restore of the user's own snooze")

	for i, ok := range early {
		assert.True(t, ok, "outcome %d was told after the window heard", i)
	}
}

func TestALaterStepYieldsOnceTheUserTookItsRoutinesMarkOff(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)
	r := h.routine(t, "Pin then dismiss")
	routines := NewRoutines(h.svc)
	ctx := t.Context()

	h.triage(t, r, id, TriagePin, 0)

	decided, err := routines.LocalStates(ctx, []int64{id})
	require.NoError(t, err)

	// The routine decided its Dismiss on its own pin; the user unpins before the Dismiss
	// lands, before the hands off is recorded, and the part the Dismiss changes is as it was.
	_, err = h.svc.Unpin(ctx, id)
	require.NoError(t, err)

	got, err := routines.Triage(ctx, RoutineTriage{Routine: r, Item: id, Action: TriageDismiss, Expect: decided[id]})
	require.NoError(t, err)
	assert.Equal(t, KeptChanged, got.Kept, "the routine's mark it decided on is gone: the user took it off")
	assert.Nil(t, h.local(t, id).DismissedAt)

	// Another routine's step decided on that state isn't the one whose mark went.
	got, err = routines.Triage(ctx, RoutineTriage{
		Routine: h.routine(t, "Another"), Item: id, Action: TriageDismiss, Expect: decided[id],
	})
	require.NoError(t, err)
	assert.True(t, got.Changed())
}

func TestAReversalIsTimedOnceItsTransactionHoldsTheWriteLock(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		step TriageAction
		user func(ctx context.Context, h *harness, id int64) error
	}{
		{"restore", TriageSnooze, func(ctx context.Context, h *harness, id int64) error {
			_, err := h.svc.Restore(ctx, id)

			return err
		}},
		{"snooze", TriageDismiss, func(ctx context.Context, h *harness, id int64) error {
			_, err := h.svc.Snooze(ctx, id, 1)

			return err
		}},
		{"unpin", TriagePin, func(ctx context.Context, h *harness, id int64) error {
			_, err := h.svc.Unpin(ctx, id)

			return err
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				h, id, _ := triageHarness(t)
				sink := h.telling(t)
				r := h.routine(t, "Tidy")
				h.triage(t, r, id, c.step, 3)

				// Another write holds the one connection, as a routine's step committing
				// while the user's click waits for the lock.
				h.pool.SetMaxOpenConns(1)

				conn, err := h.pool.Conn(t.Context())
				require.NoError(t, err)

				done := make(chan error, 1)

				go func() { done <- c.user(t.Context(), h, id) }()

				synctest.Wait()
				h.clock.Advance(time.Minute)
				require.NoError(t, conn.Close())
				require.NoError(t, <-done)

				told := sink.all()
				require.Len(t, told, 1)
				assert.Equal(t, r, told[0].Reversed.Routine)
				assert.True(t, told[0].At.Equal(h.clock.Now()),
					"the reversal's time comes after every step committed before it took the lock")
			})
		})
	}
}

// markKeeper names the items whose Restore marks the prune keeps, or fails.
type markKeeper struct {
	ids []int64
	err error
}

func (k markKeeper) KeptMarks(context.Context) ([]int64, error) { return k.ids, k.err }

func TestTheRefreshKeepsARestoreMarkAnUndoStillNeeds(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name   string
		keeper func(id int64) MarkKeeper
		left   int64
	}{
		{"Autopilot still offers its Undo", func(id int64) MarkKeeper { return markKeeper{ids: []int64{id}} }, 1},
		{"no one says", func(int64) MarkKeeper { return markKeeper{} }, 0},
		{"the keeper can't be read", func(int64) MarkKeeper { return markKeeper{err: errLedgerFull} }, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			h, id, _ := triageHarness(t)
			h.svc = NewService(Config{
				Pool: h.pool, Tokens: h.tokens, Client: h.client, Clock: h.clock, Events: h.events, Marks: c.keeper(id),
				Logger: h.logs.Logger(),
			})

			r := h.routine(t, "Bring back")
			_, err := h.svc.Snooze(t.Context(), id, 1)
			require.NoError(t, err)
			h.triage(t, r, id, TriageRestore, 0)

			h.clock.Advance(91 * 24 * time.Hour)
			h.refresh(t)
			assert.Equal(t, c.left, h.count(t, "item_local_state"), "past Autopilot's history")
		})
	}
}
