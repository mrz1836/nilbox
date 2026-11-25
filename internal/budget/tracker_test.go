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
		Spent: 7, Requests: 3, Complete: false, Purposes: Purposes{Other: 7},
	}, got, "the app opened 20 minutes into the window, so it may have missed what it spent before")
	assert.Equal(t, 4, events.Count(), "the window hears each change")
}

func TestTheSpendIsSplitByPurposeAndTheSharesAddUp(t *testing.T) {
	t.Parallel()

	tr, _, _ := newTestTracker()

	tagged := func(used int, p github.Purpose) github.RateReading {
		r := graphql(used, reset)
		r.Purpose = p

		return r
	}

	tr.ObserveRate(tagged(100, github.PurposeInbox), 3)
	tr.ObserveRate(tagged(102, github.PurposeWatches), 2)
	tr.ObserveRate(tagged(103, github.PurposeWatches), 1)
	tr.ObserveRate(tagged(104, github.PurposeFollower), 1)
	tr.ObserveRate(tagged(106, github.PurposeAutopilot), 2)
	tr.ObserveRate(tagged(107, github.PurposeOther), 1)
	tr.ObserveRate(tagged(107, github.PurposeWatches), 0) // a reading that spent nothing

	got := tr.gauge(12).GraphQL
	assert.Equal(t, Purposes{Inbox: 3, Watches: 3, Follower: 1, Autopilot: 2, Other: 1}, got.Purposes)
	assert.Equal(t, 10, got.Spent, "the reading sums every request")
	assert.Equal(t, 6, got.Requests)

	p := got.Purposes
	assert.Equal(t, got.Spent, p.Inbox+p.Watches+p.Follower+p.Autopilot+p.Other, "the shares add up")
	assert.Equal(t, 107, got.Used, "what a request was for is no part of the reading")

	state, err := tr.state()
	require.NoError(t, err)

	restored, _, _ := newTestTracker()
	require.NoError(t, restored.Restore(state))
	assert.Equal(t, p, restored.gauge(12).GraphQL.Purposes, "the split outlives the app")
}

func TestAKeptPurposeThisBuildDoesntKnowCountsAsOther(t *testing.T) {
	t.Parallel()

	for range 20 { // map order varies, and must never matter
		got := purposesOf(restoredPurposes(map[string]int{"inbox": 2, "later-purpose": 3, "watches": 1}, 6))

		assert.Equal(t, Purposes{Inbox: 2, Watches: 1, Other: 3}, got, "a purpose a later build added is shown as other")
		assert.Equal(t, 6, got.Inbox+got.Watches+got.Follower+got.Autopilot+got.Other, "so the shares still add up")
	}

	over := purposesOf(restoredPurposes(map[string]int{"inbox": 4, "watches": 4}, 6))
	assert.Equal(t, Purposes{Inbox: 4, Other: 2}, over, "a split past what was spent keeps the known purposes in order")
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

func TestTwoWindowsGitHubCountsAtOnceEachKeepWhatTheAppSpent(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	// GitHub counts the one GraphQL budget in two windows, and its responses report one or
	// the other by turns: an older one, which refills at reset, and one that began as the
	// app opened and refills an hour later.
	older, newer := reset, start.Add(time.Hour)
	inbox := func(used int, at time.Time) github.RateReading {
		r := graphql(used, at)
		r.Purpose = github.PurposeInbox

		return r
	}

	tr.ObserveRate(graphql(126, older), 0) // the rate limit endpoint
	tr.ObserveRate(inbox(3, newer), 2)
	clk.Advance(time.Second)
	tr.ObserveRate(inbox(128, older), 1)
	tr.ObserveRate(inbox(6, newer), 3)
	tr.ObserveRate(graphql(128, older), 0)

	got := tr.gauge(12).GraphQL
	assert.Equal(t, 128, got.Used, "the gauge shows the window with the least left")
	assert.Equal(t, "2026-01-10T13:00:00Z", got.ResetAt)
	assert.Equal(t, 1, got.Spent, "what the app spent in that window, never cleared by a report of the other")
	assert.Equal(t, 1, got.Requests)
	assert.Equal(t, Purposes{Inbox: 1}, got.Purposes)
	assert.False(t, got.Complete, "the app opened after the older window began")

	clk.Advance(time.Minute)
	tr.ObserveRate(inbox(300, newer), 4)

	got = tr.gauge(12).GraphQL
	assert.Equal(t, 300, got.Used, "once the other has less left, the gauge shows it")
	assert.Equal(t, "2026-01-10T13:20:00Z", got.ResetAt)
	assert.Equal(t, 9, got.Spent, "with all the app spent in it")
	assert.Equal(t, 3, got.Requests)
	assert.True(t, got.Complete, "the app was open as the newer window began")

	clk.Set(reset.Add(-5 * time.Minute))
	tr.ObserveRate(inbox(310, newer), 1)
	clk.Set(reset.Add(time.Second))
	tr.ObserveRate(graphql(1, reset.Add(time.Hour)), 1)
	assert.Len(t, tr.budgets[github.ResourceGraphQL], 2, "a window that refilled is let go once another begins")
	assert.Equal(t, 310, tr.gauge(12).GraphQL.Used, "the window still running with the least left")
}

func TestAnotherAccountsBudgetTakesOverOnceTheOldOneGoesUnreported(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	tr.ObserveRate(graphql(100, reset), 5)
	tr.ObserveRate(graphql(40, reset.Add(-10*time.Minute)), 1) // gh auth switch: another hour, still running

	got := tr.gauge(12).GraphQL
	assert.Equal(t, 100, got.Used, "both were reported lately, so the one with the least left shows")
	assert.Equal(t, 5, got.Spent)

	clk.Advance(unseenFor)
	tr.ObserveRate(graphql(45, reset.Add(-10*time.Minute)), 1)
	assert.Equal(t, 100, tr.gauge(12).GraphQL.Used, "a window unreported for no longer than unseenFor still counts")

	clk.Advance(time.Second)
	tr.ObserveRate(graphql(46, reset.Add(-10*time.Minute)), 1)

	got = tr.gauge(12).GraphQL
	assert.Equal(t, 46, got.Used, "the old account's window, unreported for longer, no longer shows")
	assert.Equal(t, 3, got.Spent)
}

func TestATrackerHoldsAFewWindowsOfABudgetAtMost(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	for i := range maxWindows + 1 {
		tr.ObserveRate(graphql(10*(i+1), reset.Add(time.Duration(i)*time.Minute)), 1)
		clk.Advance(time.Second)
	}

	windows := tr.budgets[github.ResourceGraphQL]
	require.Len(t, windows, maxWindows)
	assert.Nil(t, windowAt(windows, reset), "the window reported longest ago makes room")
	assert.Equal(t, 40, tr.gauge(12).GraphQL.Used)
}

func TestALateAnswerOfAWindowNotHeldIsDropped(t *testing.T) {
	t.Parallel()

	tr, clk, events := newTestTracker()

	tr.ObserveRate(graphql(100, reset), 1)
	clk.Set(reset.Add(10 * time.Minute))
	tr.ObserveRate(graphql(5, reset.Add(time.Hour)), 1)
	tr.ObserveRate(graphql(900, reset.Add(5*time.Minute)), 1) // a window that began and ended unseen

	assert.Equal(t, 5, tr.gauge(12).GraphQL.Used)
	assert.Equal(t, 2, events.Count())
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

	assert.Equal(t, Gauge{RefreshesPerHour: 12, Writes: noWrites(), Chores: noChores(), Bulk: noBulk()}, tr.gauge(12))
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
