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
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/notify"
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

func TestABudgetDrainingIsANotificationAboutNoItemToldOncePerWindow(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	opts := testOptions(t)
	opts.Events = events
	c := bootstrap(t, opts)

	require.NotNil(t, c.BudgetWarner, "the shell runs the warner with the other work")

	alerts := budgetAlerts{dispatcher: notify.NewDispatcher(notify.Config{Pool: c.DB, Events: events})}
	alert := budget.Alert{
		Resource: github.ResourceGraphQL,
		Title:    "Something signed in as you, not " + brand.Name + ", used 1,490 GraphQL points in 10 minutes.",
		Body:     "At this pace it runs out around 12:51 PM, before it refills at 1:00 PM.",
		Key:      "budget-draining-graphql-1768050000",
	}

	alerts.Drained(t.Context(), alert)
	alerts.Drained(t.Context(), alert) // as after a restart, for the same window

	log, err := c.Notifications.List(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, 1, "one window's drain is told once")

	got := log.Items[0]
	assert.Equal(t, notify.KindBudgetDraining, got.Kind)
	assert.Equal(t, alert.Title, got.Title)
	assert.Equal(t, alert.Body, got.Detail)
	assert.Empty(t, got.URL, "a click opens the API budget's panel, not a page")
	assert.Empty(t, got.ItemKind, "about no item")
	assert.Contains(t, events.All(), event.Event(event.NotificationsUpdated{}))
}
