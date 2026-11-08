package budget

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// start is the fake clock's time when a tracker is made: 20 minutes into the hour whose
// budgets refill at reset.
var (
	start = time.Date(2026, 1, 10, 12, 20, 0, 0, time.UTC) //nolint:gochecknoglobals // fixed test times
	reset = time.Date(2026, 1, 10, 13, 0, 0, 0, time.UTC)  //nolint:gochecknoglobals // fixed test times
)

// graphql is a GraphQL reading with used points of 5,000 used, refilling at at.
func graphql(used int, at time.Time) github.RateReading {
	return github.RateReading{Resource: github.ResourceGraphQL, Limit: 5000, Remaining: 5000 - used, Used: used, ResetAt: at}
}

// newTestTracker returns a tracker on a fake clock at start, with its events.
func newTestTracker() (*Tracker, *testutil.Clock, *testutil.Events) {
	clk := testutil.NewClock(start)
	events := &testutil.Events{}

	return NewTracker(clk, events), clk, events
}

func TestATrackerHoldsTheNewestReadingAndCountsWhatTheAppSpent(t *testing.T) {
	t.Parallel()

	tr, clk, events := newTestTracker()

	tr.ObserveRate(graphql(100, reset), 0)
	tr.ObserveRate(graphql(100, reset), 4)
	clk.Advance(time.Second)
	tr.ObserveRate(graphql(98, reset), 2) // a slower request that answered later
	tr.ObserveRate(graphql(130, reset), 1)

	got := tr.gauge(12).GraphQL
	assert.Equal(t, Reading{
		Known: true, Limit: 5000, Remaining: 4870, Used: 130,
		ResetAt: "2026-01-10T13:00:00Z", TakenAt: "2026-01-10T12:20:01Z",
		Spent: 7, Requests: 3, Complete: false,
	}, got, "the app opened 20 minutes into the window, so it may have missed what it spent before")
	assert.Equal(t, 4, events.Count(), "the window hears each change")
}

func TestANewWindowStartsCountingAgain(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	tr.ObserveRate(graphql(100, reset), 5)
	clk.Set(reset.Add(time.Minute))
	tr.ObserveRate(graphql(3, reset.Add(time.Hour)), 3)

	got := tr.gauge(12).GraphQL
	assert.Equal(t, 3, got.Used)
	assert.Equal(t, 3, got.Spent)
	assert.True(t, got.Complete, "the app was open when this window began, so it counted all it spent")

	tr.ObserveRate(graphql(4900, reset), 9)
	assert.Equal(t, 3, tr.gauge(12).GraphQL.Used, "a late answer from a window that is over is dropped")
	assert.Equal(t, 3, tr.gauge(12).GraphQL.Spent)
}

func TestAnotherAccountsBudgetReplacesTheOneHeld(t *testing.T) {
	t.Parallel()

	tr, _, _ := newTestTracker()

	tr.ObserveRate(graphql(100, reset), 5)
	tr.ObserveRate(graphql(40, reset.Add(-20*time.Minute)), 1) // gh auth switch: another hour, still running

	got := tr.gauge(12).GraphQL
	assert.Equal(t, 40, got.Used)
	assert.Equal(t, 1, got.Spent)
}

func TestAnUnchangedReadingIsFreshWithoutTellingTheWindow(t *testing.T) {
	t.Parallel()

	tr, clk, events := newTestTracker()

	tr.ObserveRate(graphql(100, reset), 0)
	clk.Advance(30 * time.Second)
	tr.ObserveRate(graphql(100, reset), 0)

	assert.Equal(t, "2026-01-10T12:20:30Z", tr.gauge(12).GraphQL.TakenAt)
	assert.Equal(t, 1, events.Count())
}

func TestATrackerIgnoresAResponseWithoutABudget(t *testing.T) {
	t.Parallel()

	tr, _, events := newTestTracker()

	tr.ObserveRate(github.RateReading{}, 3)
	tr.ObserveRate(github.RateReading{Resource: github.ResourceCore}, 1)

	assert.Equal(t, Gauge{RefreshesPerHour: 12}, tr.gauge(12))
	assert.Zero(t, events.Count())
}

func TestATrackerKeepsTheLastRefresh(t *testing.T) {
	t.Parallel()

	tr, _, events := newTestTracker()

	tr.Refreshed(5, 31)

	assert.Equal(t, Refresh{Known: true, At: "2026-01-10T12:20:00Z", Points: 5, Items: 31}, tr.gauge(12).Refresh)
	assert.Equal(t, []event.Event{event.BudgetUpdated{}}, events.All())
}

func TestSeedingShowsStoredReadingsUntilTheFirstResponse(t *testing.T) {
	t.Parallel()

	tr, _, _ := newTestTracker()
	stored := start.Add(-time.Hour)

	tr.Seed([]Stored{
		{Reading: graphql(200, reset), TakenAt: stored},
		{Reading: github.RateReading{Resource: github.ResourceCore}, TakenAt: stored}, // unread
	})

	got := tr.gauge(12)
	assert.Equal(t, 200, got.GraphQL.Used)
	assert.Equal(t, "2026-01-10T11:20:00Z", got.GraphQL.TakenAt)
	assert.Zero(t, got.GraphQL.Spent, "what another run spent is not known")
	assert.False(t, got.Core.Known)

	tr.ObserveRate(graphql(210, reset), 2)
	tr.Seed([]Stored{{Reading: graphql(500, reset), TakenAt: stored}})
	assert.Equal(t, 210, tr.gauge(12).GraphQL.Used, "a reading of this run is never replaced by a stored one")
}

func TestWindowLength(t *testing.T) {
	t.Parallel()

	assert.Equal(t, time.Hour, windowLength(github.ResourceGraphQL))
	assert.Equal(t, time.Hour, windowLength(github.ResourceCore))
	assert.Equal(t, time.Minute, windowLength(github.ResourceSearch))
}

func TestATrackerIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	tr, _, _ := newTestTracker()
	done := make(chan struct{})

	for i := range 8 {
		go func() {
			defer func() { done <- struct{}{} }()

			for j := range 50 {
				tr.ObserveRate(graphql(i*50+j, reset), 1)
				_ = tr.gauge(12)
			}
		}()
	}

	for range 8 {
		<-done
	}

	require.Equal(t, 400, tr.gauge(12).GraphQL.Spent)
}
