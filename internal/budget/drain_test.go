package budget

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// drainFrom has the tracker read, a minute apart from now, a GraphQL budget whose window
// refills at reset, used points at each reading, and the app spending mine of each step.
func drainFrom(tr *Tracker, clk *testutil.Clock, mine int, used ...int) {
	for i, u := range used {
		if i > 0 {
			clk.Advance(time.Minute)
		}

		tr.ObserveRate(graphql(u, reset), mine)
	}
}

// steps returns n readings from first, each step higher than the last.
func steps(first, step, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = first + i*step
	}

	return out
}

func TestABudgetDrainsWhenSomethingElseSpendsItFast(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	// Ten minutes of 150 points a minute, 1 of them the app's, 40 minutes before it refills.
	drainFrom(tr, clk, 1, steps(300, 150, 11)...)

	got := tr.gauge(12).GraphQL.Drain
	assert.Equal(t, Drain{
		Draining: true, Others: 1490, Mine: 10, Minutes: 10, RunsOutAt: "2026-01-10T12:51:20Z",
	}, got, "3,200 left at 150 a minute runs out in 21m20s, before the refill at 13:00")
	assert.False(t, tr.gauge(12).Core.Drain.Draining, "a budget never read never drains")

	alerts := tr.alerts()
	require.Len(t, alerts, 1)
	assert.Equal(t, Alert{
		Resource: github.ResourceGraphQL,
		Title:    "Something signed in as you, not " + brand.Name + ", used 1,490 GraphQL points in 10 minutes.",
		Body:     "At this pace it runs out around 12:51 PM, before it refills at 1:00 PM.",
		Key:      "budget-draining-graphql-1768050000",
	}, alerts[0])

	clk.Advance(time.Minute)
	tr.ObserveRate(graphql(2000, reset), 1)
	assert.True(t, tr.gauge(12).GraphQL.Drain.Draining, "it still drains")
	assert.Empty(t, tr.alerts(), "the user hears of a budget draining once an hour at most")
}

func TestABudgetDoesNotDrainOnAPaceItOutlasts(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	// 600 points in 15 minutes leaves 4,000 to last 100 minutes, past the refill in 40.
	drainFrom(tr, clk, 0, steps(400, 40, 16)...)

	assert.False(t, tr.gauge(12).GraphQL.Drain.Draining)
	assert.Empty(t, tr.alerts())
}

func TestTheAppsOwnSpendNeverDrainsABudget(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	// The app spends every point the budget loses: nothing else is spending it.
	drainFrom(tr, clk, 150, steps(300, 150, 11)...)

	assert.False(t, tr.gauge(12).GraphQL.Drain.Draining)
}

func TestABudgetNeedsAFewMinutesOfReadingsToDrain(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	drainFrom(tr, clk, 0, steps(300, 400, 5)...)
	assert.False(t, tr.gauge(12).GraphQL.Drain.Draining, "four minutes is too short to judge a pace")

	clk.Advance(time.Minute)
	tr.ObserveRate(graphql(2300, reset), 0)
	assert.True(t, tr.gauge(12).GraphQL.Drain.Draining, "five minutes is enough")
}

func TestADrainFadesOnceItsReadingsAreOld(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	drainFrom(tr, clk, 0, steps(300, 150, 11)...)
	require.True(t, tr.gauge(12).GraphQL.Drain.Draining)

	clk.Advance(paceSpan)
	assert.False(t, tr.gauge(12).GraphQL.Drain.Draining, "no reading in the last 15 minutes shows a pace")
}

func TestTheSearchBudgetNeverDrains(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	for i := range 11 {
		tr.ObserveRate(github.RateReading{
			Resource: github.ResourceSearch, Limit: 30, Remaining: 30 - i, Used: i, ResetAt: reset,
		}, 0)
		clk.Advance(time.Minute)
	}

	assert.False(t, tr.gauge(12).Search.Drain.Draining, "its window lasts a minute")
}

func TestAWindowKeepsAboutASampleAMinute(t *testing.T) {
	t.Parallel()

	tr, clk, _ := newTestTracker()

	// A reading every 10 seconds for 30 minutes.
	for i := range 180 {
		tr.ObserveRate(graphql(100+i, reset), 0)
		clk.Advance(10 * time.Second)
	}

	samples := tr.budgets[github.ResourceGraphQL][0].samples
	assert.LessOrEqual(t, len(samples), 2*int(paceSpan/sampleGap)+1, "a burst of readings keeps few samples")
	assert.GreaterOrEqual(t, len(samples), int(paceSpan/sampleGap)/2)
	assert.LessOrEqual(t, samples[len(samples)-1].at.Sub(samples[0].at), paceSpan, "none older than the span")
}

func TestCountText(t *testing.T) {
	t.Parallel()

	for n, want := range map[int]string{
		0: "0", 7: "7", 999: "999", 1000: "1,000", 4449: "4,449", 1234567: "1,234,567", -1800: "-1,800",
	} {
		assert.Equal(t, want, countText(n), n)
	}
}

// heardAlerts is a Notifier that keeps what it hears.
type heardAlerts struct {
	mu   sync.Mutex
	seen []Alert
}

func (h *heardAlerts) Drained(_ context.Context, alert Alert) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.seen = append(h.seen, alert)
}

func (h *heardAlerts) heard() []Alert {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]Alert(nil), h.seen...)
}

func TestTheWarnerTellsOfEachBudgetDrainingOnceAnHour(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		clk := testutil.NewClock(start)
		tr := NewTracker(clk, nil)
		heard := &heardAlerts{}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			NewWarner(tr, heard).Run(ctx)
			close(done)
		}()

		drainFrom(tr, clk, 1, steps(300, 150, 11)...)
		synctest.Wait()
		require.Len(t, heard.heard(), 1, "told once, as the budget began to drain")
		assert.Equal(t, github.ResourceGraphQL, heard.heard()[0].Resource)

		clk.Advance(time.Minute)
		tr.ObserveRate(graphql(2000, reset), 1)
		synctest.Wait()
		assert.Len(t, heard.heard(), 1, "not again within the hour")

		// The next window drains as well: told again only once an hour has passed.
		clk.Set(reset.Add(time.Minute))
		next := reset.Add(time.Hour)

		for i := range 11 {
			tr.ObserveRate(graphql(200+i*200, next), 1)
			clk.Advance(time.Minute)
		}

		synctest.Wait()
		assert.Len(t, heard.heard(), 1, "41 minutes after it was told, a new window draining waits")

		clk.Set(reset.Add(30 * time.Minute))

		for i := range 7 {
			tr.ObserveRate(graphql(2400+i*150, next), 1)
			clk.Advance(time.Minute)
		}

		synctest.Wait()
		require.Len(t, heard.heard(), 2, "an hour on, a budget draining is told again")
		assert.Equal(t, "budget-draining-graphql-1768053600", heard.heard()[1].Key)

		cancel()
		<-done
	})
}
