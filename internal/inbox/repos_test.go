package inbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestRepoCounts(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)

	in := func(owner, name string, number int, listed bool) FixtureItem {
		it := githubItem(name, number, "OWNER", true, false)
		it.RepoOwner = owner

		return FixtureItem{Item: it, Assigned: listed}
	}

	archived := in("acme-org", "old", 4, true)
	archived.Repo = github.Repository{IsArchived: true}

	// The items of Docs and tools are in no list, so they are not counted.
	_, err := StoreFixture(t.Context(), db, Fixture{Viewer: octoUser, Items: []FixtureItem{
		in("acme-org", "web", 3, true),
		in("acme-org", "api", 1, true),
		in("acme-org", "api", 2, true),
		in("beta-org", "tools", 5, false),
		archived,
		in("Acme-Org", "Docs", 6, false),
	}})
	require.NoError(t, err)

	got, err := NewStore(db, nil).RepoCounts(t.Context())
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

	got, err := NewStore(testutil.DB(t), nil).RepoCounts(t.Context())
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestRepoCountsReportsAnUnreadableDatabase(t *testing.T) {
	t.Parallel()

	_, err := NewStore(testutil.ClosedDB(t), nil).RepoCounts(t.Context())
	require.ErrorContains(t, err, "count the items of each repository")
}

func TestDefaultBranch(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)

	it := githubItem("sdk", 1, "OWNER", true, false)
	it.RepoOwner = "acme-org"
	it.Repo = github.Repository{DefaultBranch: "trunk"}

	_, err := StoreFixture(t.Context(), db, Fixture{Viewer: octoUser, Items: []FixtureItem{{Item: it, Assigned: true}}})
	require.NoError(t, err)

	s := NewStore(db, nil)

	got, err := s.DefaultBranch(t.Context(), repo.Name{Owner: "Acme-Org", Repo: "SDK"})
	require.NoError(t, err)
	assert.Equal(t, "trunk", got, "the stored default branch, the name matched ignoring case")

	got, err = s.DefaultBranch(t.Context(), repo.Name{Owner: "acme-org", Repo: "unseen"})
	require.NoError(t, err)
	assert.Empty(t, got, "a repository the inbox has never seen has none")

	require.NoError(t, db.Close())

	_, err = s.DefaultBranch(t.Context(), repo.Name{Owner: "acme-org", Repo: "sdk"})
	require.ErrorContains(t, err, "read the default branch of acme-org/sdk")
}
