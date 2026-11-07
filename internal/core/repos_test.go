package core

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/inbox/model"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestLaunchDefaultTargetsListTheInboxRepositories(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t, ".claude"),
		Runner: noPrograms(),
	})
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	account := model.Account{Login: "octo-user"}
	require.NoError(t, c.DB.Create(&account).Error)

	web := model.Repo{Owner: "acme-org", Name: "web"}
	require.NoError(t, c.DB.Create(&web).Error)
	require.NoError(t, c.DB.Create(&model.Repo{Owner: "acme-org", Name: "old", IsArchived: true}).Error)
	require.NoError(t, c.DB.Create(&model.Item{
		AccountID: account.ID, RepoID: web.ID, Kind: item.KindPR, Number: 1,
		ItemCreatedAt: at, ItemUpdatedAt: at, FetchedAt: at, ReasonAuthored: true,
	}).Error)

	got, err := c.LaunchDefaults.Targets(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []launchdefaults.Target{
		{Owner: "acme-org", Items: 1},
		{Owner: "acme-org", Name: "web", Items: 1},
	}, got)
}

func TestInboxReposReportsAnUnreadableDatabase(t *testing.T) {
	t.Parallel()

	_, err := inboxRepos{db: testutil.ClosedDB(t)}.Repos(t.Context())
	require.ErrorContains(t, err, "count the items of each repository")
}
