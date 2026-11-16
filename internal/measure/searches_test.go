package measure

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/watch"
)

func TestSearchesArg(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 5, SearchesArg([]string{RefreshFlag, SearchesFlag + "5"}))
	assert.Zero(t, SearchesArg([]string{RefreshFlag}))
	assert.Zero(t, SearchesArg([]string{SearchesFlag + "many"}))
	assert.Zero(t, SearchesArg([]string{SearchesFlag + "-2"}))
}

func TestTheTypicalSearchesBuildForAnyAccount(t *testing.T) {
	t.Parallel()

	require.Len(t, typicalSearchesList, MaxSearches)

	keys := map[string]bool{}

	for _, s := range typicalSearchesList {
		q, err := watch.Build(s, "octo-user")
		require.NoError(t, err)
		assert.Contains(t, q.Text, "user:octo-user")
		assert.Empty(t, watch.Check(s), "fits any account")

		keys[q.Key] = true
	}

	assert.Len(t, keys, MaxSearches, "each a search of its own")
}

func TestTheReportSaysWhatTheWatchPassesDid(t *testing.T) {
	t.Parallel()

	report := RefreshReport{
		Runs: []time.Duration{2 * time.Second, 2 * time.Second, 2 * time.Second}, Cost: 3, Limit: 5000, Searches: 5,
		Passes: []PassTime{
			{After: 3 * time.Second, Took: 6 * time.Second, Requests: 4, Points: 4, Items: 120, Read: 110},
			{After: 400 * time.Millisecond, Took: 3 * time.Second, Requests: 3, Points: 3, Items: 120, Read: 2},
			{After: 600 * time.Millisecond, Took: 3 * time.Second, Requests: 2, Points: 2, Items: 120},
		},
	}

	lines := report.Lines()
	assert.Contains(t, lines, "watch.searches=5")
	assert.Contains(t, lines, "watch.pass=1 after_ms=3000 took_ms=6000 requests=4 points=4 items=120 read=110")
	assert.Contains(t, lines, "watch.steady_after_median_ms=500", "the first pass reads every new item; the rest are steady")
	assert.Contains(t, lines, "watch.steady_points=3")
	assert.Contains(t, lines, "refresh.total_cost=6 (inbox 3 + watches 3)")
	assert.Contains(t, lines, "refresh.total_budget_pct=1.44")

	assert.NotContains(t, RefreshReport{Runs: report.Runs, Cost: 3}.Lines(), "watch.searches=0",
		"no searches, no watch lines")
}
