package budget

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// errDiskFull is a save that failed.
var errDiskFull = errors.New("disk full")

// memoryKeeper keeps the state in memory, and counts its saves.
type memoryKeeper struct {
	mu    sync.Mutex
	state string
	saves int
	err   error
}

func (k *memoryKeeper) Load(context.Context) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	return k.state, nil
}

func (k *memoryKeeper) Save(_ context.Context, state string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.saves++

	if k.err != nil {
		return k.err
	}

	k.state = state

	return nil
}

func (k *memoryKeeper) saved() (string, int) {
	k.mu.Lock()
	defer k.mu.Unlock()

	return k.state, k.saves
}

func TestAStateRoundTripsAndKeepsCountingAcrossARestart(t *testing.T) {
	t.Parallel()

	clk := testutil.NewClock(reset.Add(-time.Hour)) // the app was open as the window began
	first := NewTracker(clk, nil)
	clk.Set(start)
	first.ObserveRate(graphql(100, reset), 6)
	first.Refreshed(6, 31)

	state, err := first.state()
	require.NoError(t, err)

	// The next run opens later in the same window.
	clk.Set(start.Add(10 * time.Minute))
	second := NewTracker(clk, nil)
	require.NoError(t, second.Restore(state))
	second.ObserveRate(graphql(110, reset), 4)

	got := second.gauge(12)
	assert.Equal(t, 10, got.GraphQL.Spent, "what the last run spent counts on")
	assert.Equal(t, 2, got.GraphQL.Requests)
	assert.True(t, got.GraphQL.Complete, "the app spends nothing while closed, so the count is still all of it")
	assert.Equal(t, Refresh{Known: true, At: "2026-01-10T12:20:00Z", Points: 6, Items: 31}, got.Refresh)
}

func TestRestoreRefusesAStateItCannotRead(t *testing.T) {
	t.Parallel()

	tr, _, _ := newTestTracker()

	require.ErrorIs(t, tr.Restore("not json"), ErrUnreadableState)
	require.ErrorIs(t, tr.Restore(`{"version":99}`), ErrUnreadableState)
	assert.False(t, tr.gauge(12).GraphQL.Known)
}

func TestRestoreNeverReplacesAReadingOfThisRun(t *testing.T) {
	t.Parallel()

	old, _, _ := newTestTracker()
	old.ObserveRate(graphql(100, reset), 1)
	state, err := old.state()
	require.NoError(t, err)

	tr, _, _ := newTestTracker()
	tr.ObserveRate(graphql(150, reset), 2)
	tr.Refreshed(9, 40)
	require.NoError(t, tr.Restore(state))

	got := tr.gauge(12)
	assert.Equal(t, 150, got.GraphQL.Used)
	assert.Equal(t, 40, got.Refresh.Items)
}

func TestTheSaverSavesSoonAfterAChangeAndAsItStops(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		tr := NewTracker(testutil.NewClock(start), nil)
		keeper := &memoryKeeper{}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			NewSaver(tr, keeper, nil).Run(ctx)
			close(done)
		}()

		tr.ObserveRate(graphql(100, reset), 3)
		tr.ObserveRate(graphql(101, reset), 1)
		synctest.Wait()

		_, saves := keeper.saved()
		assert.Zero(t, saves, "it waits a moment, so a burst of changes is one write")

		time.Sleep(SaveDelay)
		synctest.Wait()

		state, saves := keeper.saved()
		assert.Equal(t, 1, saves)
		assert.Contains(t, state, `"spent":4`)

		tr.Refreshed(4, 12)
		cancel()
		<-done

		state, saves = keeper.saved()
		assert.Equal(t, 2, saves, "stopping saves what changed since")
		assert.Contains(t, state, `"items":12`)
	})
}

func TestTheSaverLogsAFailedSave(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		tr := NewTracker(testutil.NewClock(start), nil)
		keeper := &memoryKeeper{err: errDiskFull}
		logs := &testutil.Logs{}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			NewSaver(tr, keeper, logs.Logger()).Run(ctx)
			close(done)
		}()

		tr.Refreshed(4, 12)
		time.Sleep(SaveDelay)
		synctest.Wait()
		cancel()
		<-done

		assert.Contains(t, logs.String(), "could not keep the API budget")
	})
}
