package core

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestEveryGitHubResponseReachesTheBudget(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, Options{Paths: appdata.NewPaths(filepath.Join(t.TempDir(), "data")), Home: testutil.ClaudeHome(t)})

	require.NotNil(t, c.client)
	assert.Same(t, c.budget, c.client.Rates, "the shared GitHub client tells the budget about every response")
}

func TestTheBudgetComesBackAfterARestart(t *testing.T) {
	t.Parallel()

	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))
	home := testutil.ClaudeHome(t)
	resetAt := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Second)

	first, err := Bootstrap(t.Context(), Options{Paths: paths, Home: home})
	require.NoError(t, err)

	first.budget.ObserveRate(github.RateReading{
		Resource: github.ResourceGraphQL, Limit: 5000, Remaining: 4880, Used: 120, ResetAt: resetAt,
	}, 7)
	first.budget.Refreshed(7, 31)

	// The shell stops the saver before it closes the database; stopping saves.
	stopped, stop := context.WithCancel(t.Context())
	stop()
	first.BudgetSaver.Run(stopped)
	require.NoError(t, first.Close())

	second := bootstrap(t, Options{Paths: paths, Home: home})
	got := second.Budget.Gauge(t.Context())

	assert.Equal(t, 120, got.GraphQL.Used)
	assert.Equal(t, 7, got.GraphQL.Spent, "what the app spent counts on across the restart")
	assert.Equal(t, 31, got.Refresh.Items)
}

func TestTheRateReaderBorrowsTheToken(t *testing.T) {
	t.Parallel()

	runner := noPrograms()
	reader := rateReader{
		tokens: github.NewTokenSource(runner, execpath.Search{Home: testutil.ClaudeHome(t)}), client: github.NewClient("test"),
	}

	// The token is borrowed through a runner that runs nothing, so no request is made.
	err := reader.ReadRates(t.Context())
	require.Error(t, err)
	assert.True(t, errors.Is(err, github.ErrNotLoggedIn) || errors.Is(err, github.ErrGHNotFound), err)
}

func TestStoredRates(t *testing.T) {
	t.Parallel()

	got := storedRates(inbox.RateGauge{
		GraphQL: inbox.Rate{
			Known: true, Limit: 5000, Remaining: 4990, Used: 10,
			ResetAt: "2026-01-10T13:00:00Z", TakenAt: "2026-01-10T12:00:00Z",
		},
		Core:   inbox.Rate{Known: true, Limit: 5000, ResetAt: "soon", TakenAt: "2026-01-10T12:00:00Z"},
		Search: inbox.Rate{},
	})

	require.Len(t, got, 1, "a budget never read, or with a time that can't be read, is left out")
	assert.Equal(t, github.RateReading{
		Resource: github.ResourceGraphQL, Limit: 5000, Remaining: 4990, Used: 10,
		ResetAt: time.Date(2026, 1, 10, 13, 0, 0, 0, time.UTC),
	}, got[0].Reading)
	assert.Equal(t, time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), got[0].TakenAt)
}
