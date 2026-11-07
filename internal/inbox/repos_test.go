package inbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/inbox/model"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestRepoCounts(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	account := model.Account{Login: "octo-user"}
	require.NoError(t, db.Create(&account).Error)

	repos := []model.Repo{
		{Owner: "acme-org", Name: "web"},
		{Owner: "acme-org", Name: "api"},
		{Owner: "beta-org", Name: "tools"},
		{Owner: "acme-org", Name: "old", IsArchived: true},
		{Owner: "Acme-Org", Name: "Docs"},
	}
	for i := range repos {
		require.NoError(t, db.Create(&repos[i]).Error)
	}

	// The last item has no reason left, so it is not listed.
	for n, repoID := range []int64{repos[1].ID, repos[1].ID, repos[0].ID, repos[3].ID, repos[2].ID} {
		require.NoError(t, db.Create(&model.Item{
			AccountID: account.ID, RepoID: repoID, Kind: item.KindPR, Number: n + 1,
			ItemCreatedAt: at, ItemUpdatedAt: at, FetchedAt: at, ReasonAssigned: n != 4,
		}).Error)
	}

	got, err := RepoCounts(t.Context(), db)
	require.NoError(t, err)
	assert.Equal(t, []RepoCount{
		{Repo: repo.Name{Owner: "acme-org", Repo: "api"}, Items: 2},
		{Repo: repo.Name{Owner: "Acme-Org", Repo: "Docs"}, Items: 0},
		{Repo: repo.Name{Owner: "acme-org", Repo: "web"}, Items: 1},
		{Repo: repo.Name{Owner: "beta-org", Repo: "tools"}, Items: 0},
	}, got, "by owner and then name ignoring case, without archived repositories, counting listed items")
}

func TestRepoCountsWithNoRepositories(t *testing.T) {
	t.Parallel()

	got, err := RepoCounts(t.Context(), testutil.DB(t))
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestRepoCountsReportsAnUnreadableDatabase(t *testing.T) {
	t.Parallel()

	_, err := RepoCounts(t.Context(), testutil.ClosedDB(t))
	require.ErrorContains(t, err, "count the items of each repository")
}
