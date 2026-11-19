package measure

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/core"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
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

func TestAReportOfOneWatchPassCountsItAsTheSteadyState(t *testing.T) {
	t.Parallel()

	report := RefreshReport{
		Runs: []time.Duration{time.Second}, Cost: 2, Searches: 1,
		Passes: []PassTime{{After: 250 * time.Millisecond, Took: time.Second, Requests: 1, Points: 2, Items: 3, Read: 3}},
	}

	lines := report.Lines()
	assert.Contains(t, lines, "watch.steady_after_median_ms=250", "with one pass, it is the steady state")
	assert.Contains(t, lines, "watch.steady_points=2")
	assert.Contains(t, lines, "refresh.total_cost=4 (inbox 2 + watches 2)")
}

func TestPassTimeKeepsWhatTheWatchPassDid(t *testing.T) {
	t.Parallel()

	got := passTime(inbox.WatchPass{
		At: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Took: 3 * time.Second, After: 2 * time.Second, Searches: 5,
		Failed: 1, Requests: 4, Points: 6, Items: 120, Read: 7, Changed: 2,
	})

	assert.Equal(t, PassTime{After: 2 * time.Second, Took: 3 * time.Second, Requests: 4, Points: 6, Items: 120, Read: 7}, got)
}

// scratchCore starts the app over a scratch data directory with a runner that runs
// nothing, so no token can be borrowed and no request reaches GitHub, and closes it when
// the test ends.
func scratchCore(t *testing.T) *core.Core {
	t.Helper()

	c, err := core.Bootstrap(t.Context(), core.Options{
		Paths: appdata.NewPaths(filepath.Join(t.TempDir(), "data")), Home: t.TempDir(), Version: "test",
		Runner: &testutil.Runner{Stderr: "not run", Err: errNotRun},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	return c
}

func TestAddRoutinesSwitchesOnARoutineForEachTypicalSearch(t *testing.T) {
	t.Parallel()

	for _, n := range []int{2, MaxSearches} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			t.Parallel()

			c := scratchCore(t)

			require.NoError(t, addRoutines(t.Context(), c.Autopilot, n))

			routines, err := c.Autopilot.List(t.Context())
			require.NoError(t, err)
			require.Len(t, routines, n, "one for each of the first searches")

			for i, r := range routines {
				search := typicalSearchesList[i]

				assert.Equal(t, "Measure "+strconv.Itoa(i+1), r.Name)
				assert.True(t, r.Enabled, "switched on")
				require.Len(t, r.Spec.Watch.Sources, 1)
				require.NotNil(t, r.Spec.Watch.Sources[0].Search)
				assert.Equal(t, search, *r.Spec.Watch.Sources[0].Search, "routine %d watches search %d", i+1, i+1)

				want := catalog.EventChecksFailed // so its search follows the checks
				if search.Kind == item.KindIssue {
					want = catalog.EventAppears // Checks fail never fires for an issue
				}

				assert.Equal(t, want, r.Spec.Trigger.Event, "routine %d", i+1)
			}
		})
	}
}

func TestAddRoutinesSaysWhichSearchFailed(t *testing.T) {
	t.Parallel()

	t.Run("saving", func(t *testing.T) {
		t.Parallel()

		c := scratchCore(t)
		testutil.Exec(t, c.DB, `CREATE TRIGGER one_routine BEFORE INSERT ON autopilot_routines
			WHEN (SELECT COUNT(*) FROM autopilot_routines) >= 1 BEGIN SELECT RAISE(ABORT, 'no room'); END`)

		err := addRoutines(t.Context(), c.Autopilot, 3)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "add the routine for search 2")
	})

	t.Run("switching on", func(t *testing.T) {
		t.Parallel()

		c := scratchCore(t)
		testutil.Exec(t, c.DB, `CREATE TRIGGER stay_off BEFORE UPDATE ON autopilot_routines
			WHEN NEW.enabled BEGIN SELECT RAISE(ABORT, 'stays off'); END`)

		err := addRoutines(t.Context(), c.Autopilot, 2)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "switch on the routine for search 1")
	})
}

func TestATimedRefreshWithSearchesStopsAtAFailedRefresh(t *testing.T) {
	t.Parallel()

	c := scratchCore(t)

	// No token can be borrowed, so the refresh fails before any request, and no watch
	// pass is waited for.
	run, pass, err := timeOneWithSearches(t.Context(), c)
	require.ErrorIs(t, err, github.ErrNotLoggedIn)
	assert.Zero(t, run)
	assert.Zero(t, pass)
}

func TestRunRefreshWithSearchesStopsAtTheWarmUp(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	report, err := RunRefresh(t.Context(), RefreshOptions{
		Home: t.TempDir(), Version: "test", TempDir: tmp, Searches: MaxSearches + 2,
		Runner: &testutil.Runner{Stderr: "not run", Err: errNotRun},
	})
	require.ErrorIs(t, err, github.ErrNotLoggedIn)
	assert.Contains(t, err.Error(), "the warm-up refresh")
	assert.Empty(t, report.Runs, "no refresh is timed before the account is known")

	entries, readErr := os.ReadDir(tmp)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "the scratch data directory is deleted")
}
