package measure

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/inbox"
)

var (
	errRefreshFailed = errors.New("refresh failed")
	errNotRun        = errors.New("tests run no external programs")
)

// fakeRefresher answers refreshes with snapshots carrying the given GraphQL costs.
type fakeRefresher struct {
	costs   []int
	limit   int
	unknown bool
	failAt  int
	calls   int
}

func (f *fakeRefresher) Refresh(context.Context) (inbox.Snapshot, error) {
	f.calls++

	if f.failAt == f.calls {
		return inbox.Snapshot{}, errRefreshFailed
	}

	var snap inbox.Snapshot

	snap.Rate.GraphQL = inbox.Rate{Known: !f.unknown, Limit: f.limit, Cost: f.costs[(f.calls-1)%len(f.costs)]}

	return snap, nil
}

// steppingClock advances by the next step each time it is read twice (once per refresh).
type steppingClock struct {
	now   time.Time
	steps []time.Duration
	reads int
}

func (c *steppingClock) Now() time.Time {
	if c.reads%2 == 1 {
		c.now = c.now.Add(c.steps[c.reads/2])
	}

	c.reads++

	return c.now
}

func TestTimeRefreshes(t *testing.T) {
	clock := &steppingClock{
		now:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		steps: []time.Duration{900 * time.Millisecond, 700 * time.Millisecond, 800 * time.Millisecond, 650 * time.Millisecond, 1200 * time.Millisecond},
	}
	refresher := &fakeRefresher{costs: []int{1, 1, 2, 1, 1}, limit: 5000}

	report, err := TimeRefreshes(t.Context(), refresher, 5, clock.Now)
	require.NoError(t, err)

	assert.Equal(t, 5, refresher.calls)
	assert.Equal(t, clock.steps, report.Runs)
	assert.Equal(t, 800*time.Millisecond, report.Median())
	assert.Equal(t, 2, report.Cost, "the highest cost seen")
	assert.Equal(t, 24, report.HourlyPoints())
	assert.InDelta(t, 0.48, report.BudgetPercent(), 1e-9)
	assert.Equal(t, []string{
		"refresh.run=1 ms=900",
		"refresh.run=2 ms=700",
		"refresh.run=3 ms=800",
		"refresh.run=4 ms=650",
		"refresh.run=5 ms=1200",
		"refresh.median_ms=800 median_s=0.80",
		"refresh.graphql_cost=2",
		"refresh.graphql_limit=5000",
		"refresh.hourly_points=24 (cost x 12 refreshes per hour)",
		"refresh.budget_pct=0.48",
	}, report.Lines())
}

func TestRefreshReportBudget(t *testing.T) {
	report := RefreshReport{Cost: 1}

	assert.Equal(t, 5000, report.HourlyBudget(), "GitHub's standard budget when none was recorded")
	assert.InDelta(t, 0.24, report.BudgetPercent(), 1e-9)

	report.Limit = 10000
	assert.InDelta(t, 0.12, report.BudgetPercent(), 1e-9)
}

func TestTimeRefreshesErrors(t *testing.T) {
	_, err := TimeRefreshes(t.Context(), &fakeRefresher{costs: []int{1}}, 0, time.Now)
	require.ErrorIs(t, err, ErrTooFewRuns)

	report, err := TimeRefreshes(t.Context(), &fakeRefresher{costs: []int{1}, failAt: 2}, 5, time.Now)
	require.ErrorIs(t, err, errRefreshFailed)
	assert.Contains(t, err.Error(), "refresh 2 of 5")
	assert.Len(t, report.Runs, 1)

	_, err = TimeRefreshes(t.Context(), &fakeRefresher{costs: []int{1}, unknown: true}, 5, time.Now)
	require.ErrorIs(t, err, ErrNoGraphQLReading)
}

// refusingRunner runs no program.
type refusingRunner struct{}

func (refusingRunner) Run(context.Context, string, ...string) (stdout, stderr []byte, err error) {
	return nil, []byte("not run"), errNotRun
}

func TestRunRefreshUsesAScratchDatabase(t *testing.T) {
	tmp := t.TempDir()

	// No token can be borrowed, so the first refresh fails before any request is made.
	_, err := RunRefresh(t.Context(), RefreshOptions{Home: t.TempDir(), Version: "test", TempDir: tmp, Runner: refusingRunner{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refresh 1 of 5")

	entries, readErr := os.ReadDir(tmp)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "the scratch data directory is deleted")
}

func TestRunRefreshBootstrapFails(t *testing.T) {
	_, err := RunRefresh(t.Context(), RefreshOptions{TempDir: t.TempDir(), Runner: refusingRunner{}})
	require.Error(t, err, "a home directory is required")

	_, err = RunRefresh(t.Context(), RefreshOptions{Home: t.TempDir(), TempDir: "/nonexistent-scratch-parent"})
	require.Error(t, err)
}
