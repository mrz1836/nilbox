package budget

import (
	"encoding/json"
	"slices"
	"sync"
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

// sendChores asks for n chores as a sender in the chore lane would, on the real clock:
// after each refusal it waits as long as RetryIn says, then asks again. It returns when
// each chore was allowed, oldest first.
func sendChores(t *testing.T, tr *Tracker, n int) []time.Time {
	t.Helper()

	sent := make([]time.Time, 0, n)

	for len(sent) < n {
		err := tr.AllowChore(github.ResourceCore)
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

// mostWithin is the most of times, oldest first, that fall within any span of d, as the
// pace counts them: a time counts while it is less than d old.
func mostWithin(times []time.Time, d time.Duration) int {
	most, first := 0, 0

	for i := range times {
		for times[i].Sub(times[first]) >= d {
			first++
		}

		most = max(most, i-first+1)
	}

	return most
}

// allowChores asks for n chores, each ChoreGap after the last, and requires each to be
// allowed.
func allowChores(t *testing.T, tr *Tracker, clk *testutil.Clock, n int) {
	t.Helper()

	for range n {
		require.NoError(t, tr.AllowChore(github.ResourceCore))
		clk.Advance(ChoreGap)
	}
}

func TestChoreLanePaceAndGap(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		tr := NewTracker(nil, nil)
		began := time.Now()

		sent := sendChores(t, tr, 100)

		took := sent[len(sent)-1].Sub(began)
		assert.GreaterOrEqual(t, took, 99*time.Second, "never faster than one a second")
		assert.LessOrEqual(t, took, 2*time.Minute, "about one a second, with a pause for each minute's 50")

		for i := 1; i < len(sent); i++ {
			assert.GreaterOrEqual(t, sent[i].Sub(sent[i-1]), ChoreGap, "chore %d came too soon after the last", i)
		}

		assert.LessOrEqual(t, mostWithin(sent, time.Minute), ChoresPerMinute)
		assert.LessOrEqual(t, mostWithin(sent, time.Hour), ChoresPerHour)
		assert.Equal(t, 100, tr.gauge(12).Chores.LastHour)
		assert.Zero(t, tr.gauge(12).Writes.LastHour, "chores are no edits")
	})
}

func TestChoreLaneKeepsThePacePerHour(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		tr := NewTracker(nil, nil)

		sent := sendChores(t, tr, ChoresPerHour+1)

		assert.GreaterOrEqual(t, sent[ChoresPerHour].Sub(sent[0]), time.Hour,
			"the chore past the hour's waits until the first leaves it")
		assert.LessOrEqual(t, mostWithin(sent, time.Hour), ChoresPerHour)
		assert.LessOrEqual(t, mostWithin(sent, time.Minute), ChoresPerMinute)
	})
}

func TestAChoreTooSoonSaysWhenTheNextMayGo(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	require.NoError(t, tr.AllowChore(github.ResourceCore))
	clk.Advance(400 * time.Millisecond)

	err := tr.AllowChore(github.ResourceCore)
	require.ErrorIs(t, err, ErrWritePace)
	assert.Equal(t, 600*time.Millisecond, RetryIn(err))
	assert.Equal(t, brand.Name+" clears at most one notification on GitHub a second. Try again in 1 second.",
		uierr.Message(err))
	assert.Equal(t, 1, tr.gauge(12).Chores.LastHour, "a refused chore is not counted")
}

func TestAChorePastTheMinuteSaysWhenTheNextMayGo(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	allowChores(t, tr, clk, ChoresPerMinute)

	err := tr.AllowChore(github.ResourceCore)
	require.ErrorIs(t, err, ErrWritePace)
	assert.Equal(t, 10*time.Second, RetryIn(err), "the first chore leaves the minute 60 seconds after it was sent")
	assert.Equal(t, brand.Name+" cleared 50 notifications on GitHub in the last minute, the most it clears in that time. "+
		"Try again in 10 seconds.", uierr.Message(err))
}

func TestChoresLeaveEditLaneUntouched(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()
	tr.ObserveRate(core(5000, 4900), 1)

	allowChores(t, tr, clk, ChoresPerMinute)
	require.ErrorIs(t, tr.AllowChore(github.ResourceCore), ErrWritePace, "the chore lane is full for the minute")

	// Edits still go, as many as ever.
	allowN(t, tr, clk, WritesPerMinute)
	require.ErrorIs(t, tr.AllowWrite(github.ResourceCore), ErrWritePace)

	// And edits never use up the chores: the first chores have left the minute by now.
	require.NoError(t, tr.AllowChore(github.ResourceCore))

	got := tr.gauge(12)
	assert.Equal(t, Writes{
		LastHour: WritesPerMinute, LastMinute: WritesPerMinute, PerHour: WritesPerHour, PerMinute: WritesPerMinute,
		Reserve: 50,
	}, got.Writes)
	assert.Equal(t, ChoresPerMinute+1, got.Chores.LastHour)
	assert.Equal(t, ChoresPerHour, got.Chores.PerHour)
	assert.Equal(t, ChoresPerMinute, got.Chores.PerMinute)
	assert.Equal(t, 50, got.Chores.Reserve)
}

func TestChoresStopAtReserve(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()
	tr.ObserveRate(core(15000, 150), 1) // a larger budget, whose reserve is larger too

	err := tr.AllowChore(github.ResourceCore)
	require.ErrorIs(t, err, ErrBudgetReserved)
	assert.Equal(t, 40*time.Minute, RetryIn(err), "until the budget refills")
	assert.Equal(t, "Your GitHub API budget is down to its last 150 points, which "+brand.Name+" leaves for gh and your agents. "+
		"Try again once it refills in 40 minutes.", uierr.Message(err))
	assert.Zero(t, tr.gauge(12).Chores.LastHour, "a refused chore is not counted")

	clk.Set(reset)
	require.NoError(t, tr.AllowChore(github.ResourceCore))
}

func TestChoresStopDuringHold(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	var (
		edits  github.WriteGate = tr
		chores github.ChoreGate = tr
	)

	// GitHub slowed down an edit: the chores wait too.
	edits.HoldWrites(90 * time.Second)

	err := chores.AllowChore(github.ResourceCore)
	require.ErrorIs(t, err, ErrWritesHeld)
	assert.Equal(t, 90*time.Second, RetryIn(err))
	assert.Equal(t, "GitHub asked "+brand.Name+" to slow down its changes. Try again in 2 minutes.", uierr.Message(err))

	got := tr.gauge(12)
	assert.Equal(t, "2026-01-10T12:21:30Z", got.Chores.HeldUntil)
	assert.Equal(t, got.Writes.HeldUntil, got.Chores.HeldUntil)

	clk.Advance(90 * time.Second)
	require.NoError(t, chores.AllowChore(github.ResourceCore))

	// GitHub slowed down a chore: the edits wait too.
	chores.HoldWrites(time.Minute)

	err = edits.AllowWrite(github.ResourceCore)
	require.ErrorIs(t, err, ErrWritesHeld)
	assert.Equal(t, time.Minute, RetryIn(err))
	require.ErrorIs(t, chores.AllowChore(github.ResourceCore), ErrWritesHeld)

	clk.Advance(time.Minute)
	require.NoError(t, edits.AllowWrite(github.ResourceCore))
	require.NoError(t, chores.AllowChore(github.ResourceCore))
}

func TestSaverKeepsChoresVersion1(t *testing.T) {
	t.Parallel()

	clk := testutil.NewClock(start)
	first := NewTracker(clk, nil)

	allowChores(t, first, clk, 2)
	first.chores.sent = append([]time.Time{start.Add(-2 * time.Hour)}, first.chores.sent...) // long past

	state, err := first.state()
	require.NoError(t, err)
	assert.Contains(t, state, `"version":1`, "chores change no version")
	assert.Contains(t, state, `"chores":["2026-01-10T12:20:00Z","2026-01-10T12:20:01Z"]`,
		"only the last hour's chores are kept")

	second := NewTracker(clk, nil)
	require.NoError(t, second.Restore(state))
	assert.Equal(t, 2, second.gauge(12).Chores.LastHour)
	assert.Zero(t, second.gauge(12).Writes.LastHour)

	// The pace carries on across the restart: the last chore was a moment ago.
	clk.Set(start.Add(1500 * time.Millisecond))
	require.ErrorIs(t, second.AllowChore(github.ResourceCore), ErrWritePace)

	// A state kept before the chore lane still reads, with no chores.
	old := NewTracker(clk, nil)
	require.NoError(t, old.Restore(`{"version":1,"budgets":[],"writes":["2026-01-10T12:19:00Z"]}`))
	assert.Equal(t, noChores(), old.gauge(12).Chores)
	assert.Equal(t, 1, old.gauge(12).Writes.LastHour)

	// A state with no chores leaves them out, as a build before the lane saved it.
	empty, err := NewTracker(clk, nil).state()
	require.NoError(t, err)
	assert.NotContains(t, empty, "chores")

	// A build before the lane reads a state with chores, ignoring them.
	var older struct {
		Version int         `json:"version"`
		Writes  []time.Time `json:"writes"`
	}

	require.NoError(t, json.Unmarshal([]byte(state), &older))
	assert.Equal(t, stateVersion, older.Version)
}

func TestRetryInSaysWhenTheNextEditMayGo(t *testing.T) {
	t.Parallel()

	tr, _, _ := newTestTracker()

	for range WritesPerMinute {
		require.NoError(t, tr.AllowWrite(github.ResourceCore))
	}

	err := tr.AllowWrite(github.ResourceCore)
	require.ErrorIs(t, err, ErrWritePace)
	assert.Equal(t, time.Minute, RetryIn(err))
	assert.Equal(t, ErrWritePace.Error()+": 30 in the last minute", err.Error(), "the wait adds nothing to the text")

	assert.Zero(t, RetryIn(nil))
	assert.Zero(t, RetryIn(errDiskFull), "an error that is no refusal")
}

func TestAFutureTimeNeverShutsALane(t *testing.T) {
	t.Parallel()

	clk := testutil.NewClock(start)
	tr := NewTracker(clk, nil)

	// A run whose clock was an hour ahead kept an edit and a chore an hour from now.
	require.NoError(t, tr.Restore(
		`{"version":1,"budgets":[],"writes":["2026-01-10T13:20:00Z"],"chores":["2026-01-10T13:20:00Z"]}`,
	))

	require.NoError(t, tr.AllowChore(github.ResourceCore), "a time after now never holds the lane shut")
	require.NoError(t, tr.AllowWrite(github.ResourceCore))

	err := tr.AllowChore(github.ResourceCore)
	require.ErrorIs(t, err, ErrWritePace)
	assert.Equal(t, ChoreGap, RetryIn(err), "the gap counts from the chore sent now")

	clk.Advance(ChoreGap)
	require.NoError(t, tr.AllowChore(github.ResourceCore))

	got := tr.gauge(12)
	assert.Equal(t, 2, got.Chores.LastHour, "the time from the future is forgotten")
	assert.Equal(t, 1, got.Writes.LastHour)
}

func TestAClockSetBackNeverShutsALane(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	require.NoError(t, tr.AllowChore(github.ResourceCore))
	require.NoError(t, tr.AllowWrite(github.ResourceCore))

	clk.Set(start.Add(-10 * time.Minute))

	require.NoError(t, tr.AllowChore(github.ResourceCore))
	require.NoError(t, tr.AllowWrite(github.ResourceCore))
	assert.Equal(t, 1, tr.gauge(12).Chores.LastHour)
	assert.Equal(t, 1, tr.gauge(12).Writes.LastHour)
}

func TestRetryInIsTheLongestWait(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	// 50 chores in the last minute, the last of them 0.3 s ago: both the gap and the
	// minute's pace refuse, and the minute's wait is the longer.
	allowChores(t, tr, clk, ChoresPerMinute)
	clk.Set(start.Add(49*time.Second + 300*time.Millisecond))

	err := tr.AllowChore(github.ResourceCore)
	require.ErrorIs(t, err, ErrWritePace)
	assert.Equal(t, 10*time.Second+700*time.Millisecond, RetryIn(err), "the minute's wait, not the gap's 0.7 s")
	assert.Equal(t, brand.Name+" cleared 50 notifications on GitHub in the last minute, the most it clears in that time. "+
		"Try again in 11 seconds.", uierr.Message(err))

	// A shorter hold leaves the minute's wait the longest; a longer one is the wait.
	tr.HoldWrites(5 * time.Second)
	assert.Equal(t, 10*time.Second+700*time.Millisecond, RetryIn(tr.AllowChore(github.ResourceCore)))

	tr.HoldWrites(2 * time.Minute)

	err = tr.AllowChore(github.ResourceCore)
	require.ErrorIs(t, err, ErrWritesHeld)
	assert.Equal(t, 2*time.Minute, RetryIn(err))

	// A budget down to its reserve waits until it refills, longer still.
	tr.ObserveRate(core(15000, 150), 1)

	err = tr.AllowChore(github.ResourceCore)
	require.ErrorIs(t, err, ErrBudgetReserved)
	assert.Equal(t, reset.Sub(start.Add(49*time.Second+300*time.Millisecond)), RetryIn(err))
}

func TestEditsSentSideBySideAreCountedInOrder(t *testing.T) {
	t.Parallel()

	tr := NewTracker(nil, nil) // the real clock, read as each edit is decided

	var wg sync.WaitGroup

	for range WritesPerMinute {
		wg.Go(func() { assert.NoError(t, tr.AllowWrite(github.ResourceCore)) })
	}

	wg.Wait()

	tr.mu.Lock()
	sent := append([]time.Time(nil), tr.writes.sent...)
	tr.mu.Unlock()

	assert.Len(t, sent, WritesPerMinute)
	assert.True(t, slices.IsSortedFunc(sent, time.Time.Compare), "each edit is counted after the one before")
}
