package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestLaunchDefaultTargetsListTheInboxRepositories(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t, ".claude"))
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	pr := github.Item{IsPR: true, Number: 1, CreatedAt: at, UpdatedAt: at, RepoOwner: "acme-org", RepoName: "web"}
	archived := github.Item{
		Number: 2, CreatedAt: at, UpdatedAt: at, RepoOwner: "acme-org", RepoName: "old",
		Repo: github.Repository{IsArchived: true},
	}

	_, err := inbox.StoreFixture(t.Context(), c.DB, inbox.Fixture{Viewer: "octo-user", At: at, Items: []inbox.FixtureItem{
		{Item: pr, Authored: true},
		{Item: archived, Assigned: true},
	}})
	require.NoError(t, err)

	got, err := c.LaunchDefaults.Targets(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []launchdefaults.Target{
		{Owner: "acme-org", Items: 1},
		{Owner: "acme-org", Name: "web", Items: 1},
	}, got)
}

func TestInboxReposReportsAnUnreadableDatabase(t *testing.T) {
	t.Parallel()

	_, err := inboxRepos{store: inbox.NewStore(testutil.ClosedDB(t), nil)}.Repos(t.Context())
	require.ErrorContains(t, err, "count the items of each repository")
}
