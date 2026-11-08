package budget

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// noWrites is the edits of a tracker that sent none, with nothing known of the REST
// budget.
func noWrites() Writes {
	return Writes{PerHour: WritesPerHour, PerMinute: WritesPerMinute, Reserve: minReserve}
}

// core is a REST reading with remaining points of limit left, refilling at reset.
func core(limit, remaining int) github.RateReading {
	return github.RateReading{
		Resource: github.ResourceCore, Limit: limit, Remaining: remaining, Used: limit - remaining, ResetAt: reset,
	}
}

// allowN asks for n edits, each a second after the last, and requires each to be allowed.
func allowN(t *testing.T, tr *Tracker, clk *testutil.Clock, n int) {
	t.Helper()

	for range n {
		require.NoError(t, tr.AllowWrite(github.ResourceCore))
		clk.Advance(time.Second)
	}
}

func TestReserveIsAHundredthOfTheBudget(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 50, Reserve(5000))
	assert.Equal(t, 150, Reserve(15000))
	assert.Equal(t, minReserve, Reserve(60), "never below the floor")
	assert.Equal(t, minReserve, Reserve(0))
}

func TestAllowWriteCountsEachEdit(t *testing.T) {
	t.Parallel()

	tr, clk, events := newTestTracker()
	tr.ObserveRate(core(5000, 4900), 1)

	before := events.Count()

	allowN(t, tr, clk, 3)

	assert.Equal(t, before+3, events.Count(), "the window hears of each edit")
	assert.Equal(t, Writes{
		LastHour: 3, LastMinute: 3, PerHour: WritesPerHour, PerMinute: WritesPerMinute, Reserve: 50,
	}, tr.gauge(12).Writes)

	clk.Advance(time.Minute)
	assert.Equal(t, 0, tr.gauge(12).Writes.LastMinute)
	assert.Equal(t, 3, tr.gauge(12).Writes.LastHour)

	clk.Advance(time.Hour)
	assert.Equal(t, 0, tr.gauge(12).Writes.LastHour, "an edit counts for an hour")
}

func TestAllowWriteLeavesTheReserve(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()
	tr.ObserveRate(core(5000, 50), 1)

	err := tr.AllowWrite(github.ResourceCore)
	require.ErrorIs(t, err, ErrBudgetReserved)
	assert.Equal(t, "Your GitHub API budget is down to its last 50 points, which "+brand.Name+" leaves for gh and your agents. "+
		"Try again once it refills in 40 minutes.", uierr.Message(err))
	assert.Equal(t, 0, tr.gauge(12).Writes.LastHour, "a refused edit is not counted")

	// Another budget is not this one's.
	require.NoError(t, tr.AllowWrite(github.ResourceGraphQL))

	// Once the budget refills, the reading no longer holds.
	clk.Set(reset)
	require.NoError(t, tr.AllowWrite(github.ResourceCore))
}

func TestAllowWriteKeepsThePacePerMinute(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	allowN(t, tr, clk, WritesPerMinute)

	err := tr.AllowWrite(github.ResourceCore)
	require.ErrorIs(t, err, ErrWritePace)
	assert.Equal(t, brand.Name+" made 30 changes on GitHub in the last minute, the most it makes to stay clear of "+
		"GitHub's limits. Try again in 30 seconds.", uierr.Message(err))

	// The first edit leaves the minute 60 seconds after it was sent.
	clk.Set(start.Add(time.Minute + time.Second))
	require.NoError(t, tr.AllowWrite(github.ResourceCore))
}

func TestAllowWriteKeepsThePacePerHour(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	for range WritesPerHour {
		require.NoError(t, tr.AllowWrite(github.ResourceCore))
		clk.Advance(15 * time.Second)
	}

	err := tr.AllowWrite(github.ResourceCore)
	require.ErrorIs(t, err, ErrWritePace)
	assert.Contains(t, uierr.Message(err), "200 changes on GitHub in the last hour")
	assert.Contains(t, uierr.Message(err), "Try again in 10 minutes.")

	clk.Set(start.Add(time.Hour + time.Second))
	require.NoError(t, tr.AllowWrite(github.ResourceCore))
}

func TestHoldWritesWaitsAsGitHubAsked(t *testing.T) {
	t.Parallel()

	tr, clk, events := newTestTracker()

	tr.HoldWrites(0)
	assert.Zero(t, events.Count(), "no wait holds nothing")

	tr.HoldWrites(90 * time.Second)
	tr.HoldWrites(30 * time.Second) // a shorter wait leaves the longer one
	assert.Equal(t, 1, events.Count())
	assert.Equal(t, "2026-01-10T12:21:30Z", tr.gauge(12).Writes.HeldUntil)

	err := tr.AllowWrite(github.ResourceCore)
	require.ErrorIs(t, err, ErrWritesHeld)
	assert.Equal(t, "GitHub asked "+brand.Name+" to slow down its changes. Try again in 2 minutes.", uierr.Message(err))

	clk.Advance(90 * time.Second)
	require.NoError(t, tr.AllowWrite(github.ResourceCore))
	assert.Empty(t, tr.gauge(12).Writes.HeldUntil)
}

func TestTheEditsOutliveARestart(t *testing.T) {
	t.Parallel()

	clk := testutil.NewClock(start)
	first := NewTracker(clk, nil)

	allowN(t, first, clk, 2)
	first.writes = append([]time.Time{start.Add(-2 * time.Hour)}, first.writes...) // long past
	first.HoldWrites(time.Minute)

	state, err := first.state()
	require.NoError(t, err)

	clk.Set(start.Add(10 * time.Second))
	second := NewTracker(clk, nil)
	require.NoError(t, second.Restore(state))

	got := second.gauge(12).Writes
	assert.Equal(t, 2, got.LastHour, "only the last hour's edits are kept")
	assert.Equal(t, "2026-01-10T12:21:02Z", got.HeldUntil)
	require.ErrorIs(t, second.AllowWrite(github.ResourceCore), ErrWritesHeld)
}

func TestAStateWithoutEditsStillReads(t *testing.T) {
	t.Parallel()

	tr, _, _ := newTestTracker()

	require.NoError(t, tr.Restore(`{"version":1,"budgets":[]}`))
	assert.Equal(t, noWrites(), tr.gauge(12).Writes)
}
