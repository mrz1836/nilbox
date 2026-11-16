package sessions_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// runRecord is a background run's session on pull request 12, run 7.
func runRecord(id string) sessions.Record {
	r := record(id, 12)
	r.RunID, r.Terminal, r.State = 7, "", sessions.StateWorking

	return r
}

func TestARunsSessionTakesItsStateFromTheRun(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	events := &testutil.Events{}
	reread := &fakeRereader{}
	ledger := sessions.NewLedger(sessions.Config{
		Store: store, Events: events, Rereader: reread, Clock: testutil.NewClock(launched().Add(time.Minute)),
	})

	require.NoError(t, ledger.Start(t.Context(), runRecord("r1")))

	got, err := ledger.ByRun(t.Context(), 7)
	require.NoError(t, err)
	assert.Equal(t, "r1", got.ID)
	assert.Equal(t, int64(7), got.RunID)
	assert.Equal(t, sessions.StateWorking, got.State)

	view := got.Session("/srv")
	assert.Equal(t, int64(7), view.RunID, "the row's pill shows it as a background run's")
	assert.False(t, view.CanFocus, "it has no terminal to show")

	require.NoError(t, ledger.RunState(t.Context(), "r1", sessions.RunUpdate{
		State: sessions.StateEnded, LastMessage: "Every check passes now.", PID: 4200,
	}))

	got, err = store.Get(t.Context(), "r1")
	require.NoError(t, err)
	assert.Equal(t, sessions.StateEnded, got.State)
	assert.Equal(t, "Every check passes now.", got.LastMessage)
	assert.Equal(t, launched().Add(time.Minute), got.EndedAt)
	assert.True(t, got.Session("/srv").CanResume, "an ended run's session can be taken over")
	assert.Equal(t, []int64{12}, reread.heard(), "its item is read again once it stops working")
	assert.Contains(t, events.All(), event.Event(event.SessionsUpdated{}))

	require.NoError(t, ledger.RunState(t.Context(), "r1", sessions.RunUpdate{State: sessions.StateWorking}))

	got, err = store.Get(t.Context(), "r1")
	require.NoError(t, err)
	assert.Equal(t, sessions.StateEnded, got.State, "an ended session stays as it ended")
}

func TestRunStateLeavesATerminalSessionAlone(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	ledger := sessions.NewLedger(sessions.Config{Store: store})

	require.NoError(t, store.Insert(t.Context(), record("s1", 12)))
	require.NoError(t, ledger.RunState(t.Context(), "s1", sessions.RunUpdate{State: sessions.StateEnded}))

	got, err := store.Get(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, sessions.StateStarting, got.State, "only a run's session takes a run's state")

	_, err = ledger.ByRun(t.Context(), 7)
	require.ErrorIs(t, err, sessions.ErrNotFound)
}

func TestTheWatcherLeavesARunsSessionToTheRun(t *testing.T) {
	t.Parallel()

	w := newWatched(t)
	require.NoError(t, w.ledger.Start(t.Context(), runRecord("r1")))

	w.claude.set("r1", sessions.StateEnded)
	w.watcher.Reconcile(t.Context())

	assert.Equal(t, sessions.StateWorking, w.state(t, "r1"), "no observer's word changes a run's session")

	calls, _ := w.claude.counts()
	assert.Zero(t, calls, "the observer isn't even asked")
}
