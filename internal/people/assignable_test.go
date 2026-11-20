package people

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// octoB and octoC are the people the tests know beside octoA.
const (
	octoB = "octo-b"
	octoC = "octo-c"
)

// assignableLogins lists the logins of who can be assigned, and of them those listed, in
// order.
func assignableLogins(a Assignables) (all, listed []string) {
	for _, p := range a.People {
		all = append(all, p.Login)

		if p.Listed {
			listed = append(listed, p.Login)
		}
	}

	return all, listed
}

func TestAWholeListThatNamesEveryoneUnlistsWhomItNoLongerNames(t *testing.T) {
	t.Parallel()

	svc, _, clk := newService(t)

	require.NoError(t, svc.KeepAssignable(t.Context(), exampleRepo, "", []Found{
		{Login: octoA, Name: "Octo A"}, {Login: octoB}, {Login: "dependabot[bot]"}, {Login: "-bad-"},
	}, 4))

	got, err := svc.Assignable(t.Context(), exampleRepo)
	require.NoError(t, err)

	all, listed := assignableLogins(got)
	assert.Equal(t, []string{octoA, octoB}, all, "a bot and a login GitHub can't issue are not kept")
	assert.Equal(t, all, listed)
	assert.True(t, got.Complete, "it named all four GitHub counted")
	assert.Equal(t, 4, got.Total)
	assert.Equal(t, 4, got.Listed)
	assert.Equal(t, testNow, got.ReadAt)
	assert.False(t, got.Stale)
	assert.Equal(t, "Octo A", got.People[0].Name)

	// Someone seen assigned stays known after the next whole list leaves them out, unlisted.
	require.NoError(t, svc.SeenAssigned(t.Context(), exampleRepo, []string{octoB}))
	clk.Advance(time.Hour)
	require.NoError(t, svc.KeepAssignable(t.Context(), exampleRepo, "", []Found{{Login: "Octo-A"}, {Login: octoC}}, 2))

	got, err = svc.Assignable(t.Context(), exampleRepo)
	require.NoError(t, err)

	all, listed = assignableLogins(got)
	assert.Equal(t, []string{"Octo-A", octoB, octoC}, all, "the new spelling")
	assert.Equal(t, []string{"Octo-A", octoC}, listed)
	assert.Equal(t, "Octo A", got.People[0].Name, "a read with no name keeps the name kept")
	assert.Equal(t, testNow, got.People[1].AssignedAt)
}

func TestAListCutShortUnlistsNoOne(t *testing.T) {
	t.Parallel()

	svc, _, _ := newService(t)

	require.NoError(t, svc.KeepAssignable(t.Context(), exampleRepo, "octo", []Found{{Login: "octo-z"}}, 1))
	require.NoError(t, svc.KeepAssignable(t.Context(), exampleRepo, "", []Found{{Login: octoA}}, 150))

	got, err := svc.Assignable(t.Context(), exampleRepo)
	require.NoError(t, err)

	_, listed := assignableLogins(got)
	assert.Equal(t, []string{octoA, "octo-z"}, listed, "the first page of 150 says nothing of the rest")
	assert.False(t, got.Complete)
	assert.Equal(t, 150, got.Total)
	assert.Equal(t, 1, got.Listed)
}

func TestSearchesCoverLongerSearches(t *testing.T) {
	t.Parallel()

	t.Run("a search that named everyone covers longer ones for a week", func(t *testing.T) {
		t.Parallel()

		svc, _, clk := newService(t)

		covered := func(query string) bool {
			t.Helper()

			ok, err := svc.Covers(t.Context(), exampleRepo, query)
			require.NoError(t, err)

			return ok
		}

		assert.True(t, covered("  "), "no words to search")
		assert.False(t, covered("oc"), "nothing read")

		require.NoError(t, svc.KeepAssignable(t.Context(), exampleRepo, " Oc ", []Found{{Login: octoA}}, 1))

		assert.True(t, covered("oc"))
		assert.True(t, covered("OCTO-A"), "a longer search, in any case")
		assert.False(t, covered("o"), "a shorter search may find more")
		assert.False(t, covered("xoc"), "another start")

		clk.Advance(coversFor - time.Minute)
		assert.True(t, covered("octo"))

		clk.Advance(time.Minute)
		assert.False(t, covered("octo"), "a week old")
	})

	t.Run("a search cut off at a page covers only itself, for ten minutes", func(t *testing.T) {
		t.Parallel()

		svc, _, clk := newService(t)

		require.NoError(t, svc.KeepAssignable(t.Context(), exampleRepo, "a", []Found{{Login: octoA}}, 300))

		ok, err := svc.Covers(t.Context(), exampleRepo, "ab")
		require.NoError(t, err)
		assert.False(t, ok, "the others GitHub didn't name may match")

		ok, err = svc.Covers(t.Context(), exampleRepo, "A")
		require.NoError(t, err)
		assert.True(t, ok, "the same words again answer themselves")

		clk.Advance(repeatFor)

		ok, err = svc.Covers(t.Context(), exampleRepo, "a")
		require.NoError(t, err)
		assert.False(t, ok, "ten minutes on")
	})

	t.Run("a whole list that named everyone covers every search", func(t *testing.T) {
		t.Parallel()

		svc, _, clk := newService(t)

		require.NoError(t, svc.KeepAssignable(t.Context(), exampleRepo, "", []Found{{Login: octoA}}, 1))
		clk.Advance(coversFor + time.Hour)

		ok, err := svc.Covers(t.Context(), exampleRepo, "anyone")
		require.NoError(t, err)
		assert.True(t, ok)

		require.NoError(t, svc.KeepAssignable(t.Context(), exampleRepo, "", []Found{{Login: octoA}}, 101))

		ok, err = svc.Covers(t.Context(), exampleRepo, "anyone")
		require.NoError(t, err)
		assert.False(t, ok, "a whole list cut short covers nothing")
	})
}

func TestAssignableStaleness(t *testing.T) {
	t.Parallel()

	svc, _, clk := newService(t)

	got, err := svc.Assignable(t.Context(), exampleRepo)
	require.NoError(t, err)
	assert.True(t, got.Stale, "never read")
	assert.Empty(t, got.People)
	assert.NotNil(t, got.People)

	// A search's failure says nothing of the whole list.
	require.NoError(t, svc.MarkAssignableFailed(t.Context(), exampleRepo, "octo"))

	got, err = svc.Assignable(t.Context(), exampleRepo)
	require.NoError(t, err)
	assert.True(t, got.Stale)

	require.NoError(t, svc.MarkAssignableFailed(t.Context(), exampleRepo, ""))

	got, err = svc.Assignable(t.Context(), exampleRepo)
	require.NoError(t, err)
	assert.False(t, got.Stale, "held off after the whole list's read failed")
	assert.False(t, got.Complete, "a read that never went through names no one")

	clk.Advance(retryAfter)
	require.NoError(t, svc.KeepAssignable(t.Context(), exampleRepo, "", nil, 0))

	got, err = svc.Assignable(t.Context(), exampleRepo)
	require.NoError(t, err)
	assert.False(t, got.Stale)
	assert.True(t, got.Complete, "GitHub said no one, and named no one")

	clk.Advance(readEvery)

	got, err = svc.Assignable(t.Context(), exampleRepo)
	require.NoError(t, err)
	assert.True(t, got.Stale, "a week old")
}

func TestAssignablePrunesToItsBounds(t *testing.T) {
	t.Parallel()

	t.Run("a person who can be assigned outlives what was seen of them", func(t *testing.T) {
		t.Parallel()

		svc, db, _ := newService(t)

		require.NoError(t, svc.KeepAssignable(t.Context(), exampleRepo, "", []Found{{Login: octoA}}, 1))
		require.NoError(t, svc.Learn(t.Context(), exampleRepo, nil))

		assert.Equal(t, 1, count(t, db, "people"), "a learn's pruning keeps who can be assigned")
		assert.Equal(t, 1, count(t, db, "people_assignable"))
	})

	t.Run("a year of rows, a week of searches, and the people they name", func(t *testing.T) {
		t.Parallel()

		svc, db, clk := newService(t)

		require.NoError(t, svc.KeepAssignable(t.Context(), exampleRepo, "", []Found{{Login: octoA}}, 1))
		require.NoError(t, svc.KeepAssignable(t.Context(), exampleRepo, "octo", []Found{{Login: octoA}}, 1))
		require.NoError(t, svc.SeenAssigned(t.Context(), exampleRepo, []string{octoB, "renovate[bot]"}))
		assert.Equal(t, 2, count(t, db, "people_assignable"), "no bot")

		clk.Advance(coversFor + time.Minute)
		require.NoError(t, svc.SeenAssigned(t.Context(), exampleRepo, nil))
		assert.Equal(t, 1, count(t, db, "people_assignable_reads"), "the search went a week on; the whole list stays")

		clk.Advance(keptFor)
		require.NoError(t, svc.SeenAssigned(t.Context(), exampleRepo, []string{octoC}))
		assert.Equal(t, 1, count(t, db, "people_assignable"))
		assert.Zero(t, count(t, db, "people_assignable_reads"))
		assert.Equal(t, 1, count(t, db, "people"), "a person nothing names goes")
	})

	t.Run("the latest rows in all", func(t *testing.T) {
		t.Parallel()

		svc, db, clk := newService(t)
		assert.Equal(t, int64(maxAssignable), svc.maxAssignable)

		svc.maxAssignable = 2

		for _, login := range []string{octoA, octoB, octoC} {
			require.NoError(t, svc.SeenAssigned(t.Context(), exampleRepo, []string{login}))
			clk.Advance(time.Minute)
		}

		got, err := svc.Assignable(t.Context(), exampleRepo)
		require.NoError(t, err)

		all, _ := assignableLogins(got)
		assert.Equal(t, []string{octoB, octoC}, all, "the earliest seen goes")
		assert.Equal(t, 2, count(t, db, "people"))
	})
}

func TestTheOrgsPeopleComeFromTheOwnersOtherRepositories(t *testing.T) {
	t.Parallel()

	svc, _, _ := newService(t)
	web := repo.Name{Owner: "Acme-Org", Repo: "web"}
	api := repo.Name{Owner: "acme-org", Repo: "api"}
	elsewhere := repo.Name{Owner: "other-org", Repo: "web"}

	require.NoError(t, svc.Learn(t.Context(), web, []Seen{
		{Login: "octo-web", Number: 1, Role: RoleReviewer, Access: true, At: testNow.Add(-time.Hour)},
		{Login: "octo-outsider", Number: 1, Role: RoleAuthor, At: testNow},
	}))
	require.NoError(t, svc.Learn(t.Context(), api, []Seen{
		{Login: "octo-web", Number: 3, Role: RoleRequested, At: testNow},
		{Login: "octo-api", Number: 4, Role: RoleAuthor, Access: true, At: testNow.Add(-2 * time.Hour)},
	}))
	require.NoError(t, svc.Learn(t.Context(), exampleRepo, []Seen{
		{Login: "octo-here", Number: 9, Role: RoleReviewer, Access: true, At: testNow},
	}))
	require.NoError(t, svc.Learn(t.Context(), elsewhere, []Seen{
		{Login: "octo-elsewhere", Number: 2, Role: RoleReviewer, Access: true, At: testNow},
	}))

	org, err := svc.OrgRanked(t.Context(), exampleRepo)
	require.NoError(t, err)

	assert.Equal(t, []string{"octo-web", "octo-api"}, logins(org),
		"not the repository's own, another owner's, or someone without access")
	assert.Equal(t, "acme-org/api", org[0].Where, "where they were seen last")
	assert.Equal(t, testNow, org[0].LastAt)
	assert.Equal(t, 1, org[0].Reviewed)

	_, err = svc.OrgRanked(t.Context(), repo.Name{})
	require.ErrorIs(t, err, ErrInvalid)
}

func TestForgetEmptiesTheDirectory(t *testing.T) {
	t.Parallel()

	svc, db, _ := newService(t)
	other := repo.Name{Owner: "acme-org", Repo: "other-repo"}

	summary, err := svc.Summary(t.Context())
	require.NoError(t, err)
	assert.Equal(t, Summary{}, summary)

	require.NoError(t, svc.Learn(t.Context(), exampleRepo, []Seen{
		{Login: octoA, Number: 1, Role: RoleAuthor, Access: true},
	}))
	require.NoError(t, svc.SetTeams(t.Context(), other, []Team{{Slug: "web"}}, true))
	require.NoError(t, svc.MarkRead(t.Context(), exampleRepo, 1, true))
	require.NoError(t, svc.KeepAssignable(t.Context(), repo.Name{Owner: "Acme-Org", Repo: "Example-Repo"}, "",
		[]Found{{Login: octoA}, {Login: octoB}}, 2))

	summary, err = svc.Summary(t.Context())
	require.NoError(t, err)
	assert.Equal(t, Summary{People: 2, Repos: 2}, summary, "a repository named in any case counts once")

	require.NoError(t, svc.Forget(t.Context()))

	for _, table := range []string{
		"people", "people_seen", "people_teams", "people_reads", "people_assignable", "people_assignable_reads",
	} {
		assert.Zero(t, count(t, db, table), table)
	}

	summary, err = svc.Summary(t.Context())
	require.NoError(t, err)
	assert.Equal(t, Summary{}, summary)
}

func TestAssignableErrorsReachTheCaller(t *testing.T) {
	t.Parallel()

	svc, db, _ := newService(t)

	for _, call := range []error{
		svc.KeepAssignable(t.Context(), repo.Name{}, "", nil, 0),
		svc.MarkAssignableFailed(t.Context(), repo.Name{}, ""),
		svc.SeenAssigned(t.Context(), repo.Name{}, nil),
	} {
		require.ErrorIs(t, call, ErrInvalid)
	}

	_, err := svc.Assignable(t.Context(), repo.Name{})
	require.ErrorIs(t, err, ErrInvalid)

	_, err = svc.Covers(t.Context(), repo.Name{}, "octo")
	require.ErrorIs(t, err, ErrInvalid)

	for _, table := range []string{"people_assignable_reads", "people_assignable", "people_teams"} {
		testutil.Exec(t, db, "DROP TABLE "+table)
	}

	require.Error(t, svc.KeepAssignable(t.Context(), exampleRepo, "", []Found{{Login: octoA}}, 1))
	require.Error(t, svc.MarkAssignableFailed(t.Context(), exampleRepo, ""))
	require.Error(t, svc.SeenAssigned(t.Context(), exampleRepo, []string{octoA}))
	require.Error(t, svc.Forget(t.Context()))

	_, err = svc.Assignable(t.Context(), exampleRepo)
	require.Error(t, err)

	_, err = svc.Covers(t.Context(), exampleRepo, "octo")
	require.Error(t, err)

	_, err = svc.Summary(t.Context())
	require.Error(t, err)

	testutil.Exec(t, db, "DROP TABLE people_seen")

	_, err = svc.OrgRanked(t.Context(), exampleRepo)
	require.Error(t, err)
}
