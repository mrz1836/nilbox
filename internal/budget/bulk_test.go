package budget

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// sendBulk asks for n of a bulk mark's writes as the bulk job would, on the real clock:
// after each refusal it waits as long as RetryIn says, then asks again. It returns when
// each was allowed, oldest first.
func sendBulk(t *testing.T, tr *Tracker, n int) []time.Time {
	t.Helper()

	sent := make([]time.Time, 0, n)

	for len(sent) < n {
		err := tr.AllowBulkChore(github.ResourceCore)
		if err != nil {
			require.ErrorIs(t, err, ErrWritePace)

			wait := RetryIn(err)
			require.Positive(t, wait, "a refusal says when the next may go")
			time.Sleep(wait)

			continue
		}

		sent = append(sent, time.Now())
	}

	return sent
}

// allowBulk asks for n of a bulk mark's writes, each BulkGap after the last, and requires
// each to be allowed.
func allowBulk(t *testing.T, tr *Tracker, clk *testutil.Clock, n int) {
	t.Helper()

	for range n {
		require.NoError(t, tr.AllowBulkChore(github.ResourceCore))
		clk.Advance(BulkGap)
	}
}

func TestBulkLanePaceAndGap(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		tr := NewTracker(nil, nil)
		began := time.Now()

		sent := sendBulk(t, tr, 100)

		took := sent[len(sent)-1].Sub(began)
		assert.GreaterOrEqual(t, took, 99*BulkGap, "never faster than one every 1.5 seconds")
		assert.LessOrEqual(t, took, 3*time.Minute, "about 40 a minute")

		for i := 1; i < len(sent); i++ {
			assert.GreaterOrEqual(t, sent[i].Sub(sent[i-1]), BulkGap, "write %d came too soon after the last", i)
		}

		assert.LessOrEqual(t, mostWithin(sent, time.Minute), BulkPerMinute)

		got := tr.gauge(12)
		assert.Equal(t, 100, got.Bulk.LastHour)
		assert.Zero(t, got.Chores.LastHour, "the bulk lane's writes are no chores")
		assert.Zero(t, got.Writes.LastHour, "nor edits")
	})
}

func TestBulkLaneKeepsThePacePerHour(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		tr := NewTracker(nil, nil)

		sent := sendBulk(t, tr, BulkPerHour+1)

		assert.GreaterOrEqual(t, sent[BulkPerHour].Sub(sent[0]), time.Hour,
			"the write past the hour's waits until the first leaves it")
		assert.LessOrEqual(t, mostWithin(sent, time.Hour), BulkPerHour)
		assert.LessOrEqual(t, mostWithin(sent, time.Minute), BulkPerMinute)
	})
}

func TestABulkWriteTooSoonSaysWhenTheNextMayGo(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	require.NoError(t, tr.AllowBulkChore(github.ResourceCore))
	clk.Advance(time.Second)

	err := tr.AllowBulkChore(github.ResourceCore)
	require.ErrorIs(t, err, ErrWritePace)
	assert.Equal(t, 500*time.Millisecond, RetryIn(err))
	assert.Equal(t, brand.Name+" marks notifications done on GitHub one at a time, a little apart. Try again in 1 second.",
		uierr.Message(err))
	assert.Equal(t, 1, tr.gauge(12).Bulk.LastHour, "a refused write is not counted")
}

func TestABulkWritePastTheMinuteSaysWhenTheNextMayGo(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	// Sent BulkGap apart, 40 always span the minute, so the gap holds them first; an
	// earlier run's, kept a second apart, fill the minute in 40 seconds.
	kept := make([]time.Time, 0, BulkPerMinute)
	for i := range BulkPerMinute {
		kept = append(kept, start.Add(time.Duration(i)*time.Second))
	}

	tr.mu.Lock()
	tr.restoreWritesLocked(nil, nil, kept, nil)
	tr.mu.Unlock()
	clk.Set(start.Add(41 * time.Second))

	err := tr.AllowBulkChore(github.ResourceCore)
	require.ErrorIs(t, err, ErrWritePace)
	assert.Equal(t, 19*time.Second, RetryIn(err), "the first leaves the minute 60 s after it was sent")
	assert.Equal(t, brand.Name+" marked 40 notifications done on GitHub in the last minute, the most it marks in that time. "+
		"Try again in 19 seconds.", uierr.Message(err))

	clk.Set(start.Add(time.Minute))
	require.NoError(t, tr.AllowBulkChore(github.ResourceCore))
}

func TestBulkLaneAndChoreLaneStayApart(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	// A click's chore holds the bulk lane for ChoreGap.
	require.NoError(t, tr.AllowChore(github.ResourceCore))
	clk.Advance(300 * time.Millisecond)

	err := tr.AllowBulkChore(github.ResourceCore)
	require.ErrorIs(t, err, ErrWritePace)
	assert.Equal(t, 700*time.Millisecond, RetryIn(err), "the bulk lane waits out the chore's second")

	clk.Advance(700 * time.Millisecond)
	require.NoError(t, tr.AllowBulkChore(github.ResourceCore))

	// A bulk write holds the chore lane for ChoreGap too, never its own BulkGap.
	clk.Advance(400 * time.Millisecond)

	err = tr.AllowChore(github.ResourceCore)
	require.ErrorIs(t, err, ErrWritePace)
	assert.Equal(t, 600*time.Millisecond, RetryIn(err))

	clk.Advance(600 * time.Millisecond)
	require.NoError(t, tr.AllowChore(github.ResourceCore))

	got := tr.gauge(12)
	assert.Equal(t, 2, got.Chores.LastHour)
	assert.Equal(t, 1, got.Bulk.LastHour)

	// Edits are never held by either lane.
	require.NoError(t, tr.AllowWrite(github.ResourceCore))

	clk.Advance(ChoreGap)
	require.NoError(t, tr.AllowBulkChore(github.ResourceCore), "a second after the chore, the bulk lane may go")
}

func TestBulkLaneSharesTheHoldAndTheReserve(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	var chores github.ChoreGate = tr

	// GitHub slowed down an edit: the bulk lane waits too.
	tr.HoldWrites(90 * time.Second)

	err := chores.AllowBulkChore(github.ResourceCore)
	require.ErrorIs(t, err, ErrWritesHeld)
	assert.Equal(t, 90*time.Second, RetryIn(err))
	assert.Equal(t, "2026-01-10T12:21:30Z", tr.gauge(12).Bulk.HeldUntil)

	clk.Advance(90 * time.Second)
	require.NoError(t, chores.AllowBulkChore(github.ResourceCore))

	// A budget down to its reserve holds the bulk lane until it refills.
	clk.Advance(BulkGap)
	tr.ObserveRate(core(5000, 50), 1)

	err = chores.AllowBulkChore(github.ResourceCore)
	require.ErrorIs(t, err, ErrBudgetReserved)
	assert.Equal(t, reset.Sub(clk.Now()), RetryIn(err))
	assert.Equal(t, 50, tr.gauge(12).Bulk.Reserve)
}

func TestSaverKeepsTheBulkLane(t *testing.T) {
	t.Parallel()

	clk := testutil.NewClock(start)
	first := NewTracker(clk, nil)

	allowBulk(t, first, clk, 2)
	first.bulk.sent = append([]time.Time{start.Add(-2 * time.Hour)}, first.bulk.sent...) // long past

	state, err := first.state()
	require.NoError(t, err)
	assert.Contains(t, state, `"version":1`, "the bulk lane changes no version")
	assert.Contains(t, state, `"bulk":["2026-01-10T12:20:00Z","2026-01-10T12:20:01.5Z"]`,
		"only the last hour's writes are kept")

	second := NewTracker(clk, nil)
	require.NoError(t, second.Restore(state))
	assert.Equal(t, 2, second.gauge(12).Bulk.LastHour)
	assert.Zero(t, second.gauge(12).Chores.LastHour)

	// The pace carries on across the restart: the last write was a moment ago.
	clk.Set(start.Add(2 * time.Second))
	require.ErrorIs(t, second.AllowBulkChore(github.ResourceCore), ErrWritePace)

	// A state kept before the bulk lane still reads, with none.
	old := NewTracker(clk, nil)
	require.NoError(t, old.Restore(`{"version":1,"budgets":[],"chores":["2026-01-10T12:19:00Z"]}`))
	assert.Equal(t, noBulk(), old.gauge(12).Bulk)
	assert.Equal(t, 1, old.gauge(12).Chores.LastHour)

	// A state with none leaves the lane out, as a build before it saved it.
	empty, err := NewTracker(clk, nil).state()
	require.NoError(t, err)
	assert.NotContains(t, empty, "bulk")
}
