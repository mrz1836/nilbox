package people

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// testNow is the fixed time the tests run at.
var testNow = time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed test value

// octoA is the person the ranking tests rank most.
const octoA = "octo-a"

// exampleRepo is the repository most tests learn about.
var exampleRepo = repo.Name{Owner: "acme-org", Repo: "example-repo"} //nolint:gochecknoglobals // a fixed test value

// newService returns a Service over a fresh database, with a clock at testNow.
func newService(t *testing.T) (*Service, *sql.DB, *testutil.Clock) {
	t.Helper()

	db := testutil.DB(t)
	clk := testutil.NewClock(testNow)

	return NewService(Config{Pool: db, Clock: clk}), db, clk
}

// count returns the rows of table.
func count(t *testing.T, db *sql.DB, table string) int {
	t.Helper()

	var n int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&n))

	return n
}

// logins lists the people's logins in order.
func logins(people []Person) []string {
	out := make([]string, 0, len(people))
	for _, p := range people {
		out = append(out, p.Login)
	}

	return out
}

func TestLearnIsIdempotent(t *testing.T) {
	t.Parallel()

	svc, db, _ := newService(t)
	seen := []Seen{
		{Login: "octo-author", Name: "Octo Author", Number: 7, Role: RoleAuthor, Access: true, At: testNow.Add(-time.Hour)},
		{Login: "octo-reviewer", Number: 7, Role: RoleReviewer, Access: true, At: testNow.Add(-time.Hour)},
		{Login: "Octo-Reviewer", Number: 7, Role: RoleReviewer, At: testNow},
	}

	require.NoError(t, svc.Learn(t.Context(), exampleRepo, seen))
	require.NoError(t, svc.Learn(t.Context(), exampleRepo, seen))

	assert.Equal(t, 2, count(t, db, "people_seen"), "the same pull request in the same role counts once")
	assert.Equal(t, 2, count(t, db, "people"))

	ranked, err := svc.Ranked(t.Context(), exampleRepo)
	require.NoError(t, err)
	require.Len(t, ranked.People, 2)

	byLogin := map[string]Person{}
	for _, p := range ranked.People {
		byLogin[p.Login] = p
	}

	reviewer := byLogin["Octo-Reviewer"]
	assert.Equal(t, 1, reviewer.Reviewed, "one review, however often it was read")
	assert.Equal(t, testNow, reviewer.LastAt, "the later time stands")
	assert.Equal(t, "Octo Author", byLogin["octo-author"].Name)
}

func TestLearnKeepsTheNameWhenANewReadHasNone(t *testing.T) {
	t.Parallel()

	svc, _, _ := newService(t)

	require.NoError(t, svc.Learn(t.Context(), exampleRepo, []Seen{
		{Login: "octo-author", Name: "Octo Author", Number: 1, Role: RoleAuthor, Access: true},
	}))
	require.NoError(t, svc.Learn(t.Context(), exampleRepo, []Seen{
		{Login: "octo-author", Number: 2, Role: RoleAuthor, Access: true},
	}))

	ranked, err := svc.Ranked(t.Context(), exampleRepo)
	require.NoError(t, err)
	require.Len(t, ranked.People, 1)
	assert.Equal(t, "Octo Author", ranked.People[0].Name)
	assert.Equal(t, 2, ranked.People[0].Authored)
}

func TestLearnSkipsWhatItCannotKeep(t *testing.T) {
	t.Parallel()

	svc, db, _ := newService(t)

	require.NoError(t, svc.Learn(t.Context(), exampleRepo, []Seen{
		{Login: "", Number: 1, Role: RoleAuthor},
		{Login: "not a login", Number: 1, Role: RoleAuthor},
		{Login: "octo-user", Number: 0, Role: RoleAuthor},
		{Login: "octo-user", Number: 1, Role: "maintainer"},
	}))

	assert.Zero(t, count(t, db, "people_seen"))

	require.ErrorIs(t, svc.Learn(t.Context(), repo.Name{Owner: "acme-org"}, nil), ErrInvalid)

	_, err := svc.Ranked(t.Context(), repo.Name{})
	require.ErrorIs(t, err, ErrInvalid)
}

func TestRankedListsOnlyThoseWhoWorkOnTheRepository(t *testing.T) {
	t.Parallel()

	svc, _, _ := newService(t)

	require.NoError(t, svc.Learn(t.Context(), exampleRepo, []Seen{
		// An outside contributor's pull request and commits never qualify them.
		{Login: "octo-outsider", Number: 1, Role: RoleAuthor, At: testNow},
		{Login: "octo-outsider", Number: 1, Role: RoleCommitter, At: testNow},
		// A review without access doesn't either.
		{Login: "octo-drive-by", Number: 1, Role: RoleReviewer, At: testNow},
		// A member's review does.
		{Login: "octo-member", Number: 1, Role: RoleReviewer, Access: true, At: testNow},
		// Someone asked, suggested, or picked does, whatever their association.
		{Login: "octo-asked", Number: 1, Role: RoleRequested, At: testNow},
		{Login: "octo-suggested", Number: 1, Role: RoleSuggested, At: testNow},
		{Login: "octo-picked", Number: 1, Role: RolePicked, At: testNow},
	}))

	ranked, err := svc.Ranked(t.Context(), exampleRepo)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"octo-member", "octo-asked", "octo-suggested", "octo-picked"}, logins(ranked.People))

	other, err := svc.Ranked(t.Context(), repo.Name{Owner: "acme-org", Repo: "other-repo"})
	require.NoError(t, err)
	assert.Empty(t, other.People, "what was seen is the repository's own")
	assert.NotNil(t, other.People)
	assert.NotNil(t, other.Teams)
}

func TestRankOrdersByWhatTheyDidLatestFirst(t *testing.T) {
	t.Parallel()

	day := 24 * time.Hour
	rows := []seenRow{
		// Three recent reviews outweigh one.
		{key: octoA, login: octoA, number: 1, role: RoleReviewer, access: true, at: testNow},
		{key: octoA, login: octoA, number: 2, role: RoleReviewer, access: true, at: testNow},
		{key: octoA, login: octoA, number: 3, role: RoleReviewer, access: true, at: testNow},
		{key: "octo-b", login: "octo-b", number: 1, role: RoleReviewer, access: true, at: testNow},
		// Three reviews 180 days ago count an eighth as much: less than one today.
		{key: "octo-c", login: "octo-c", number: 1, role: RoleReviewer, access: true, at: testNow.Add(-180 * day)},
		{key: "octo-c", login: "octo-c", number: 2, role: RoleReviewer, access: true, at: testNow.Add(-180 * day)},
		{key: "octo-c", login: "octo-c", number: 3, role: RoleReviewer, access: true, at: testNow.Add(-180 * day)},
		// A suggestion alone counts least.
		{key: "octo-d", login: "octo-d", number: 1, role: RoleSuggested, at: testNow},
		// Two alike are told apart by login.
		{key: "octo-e", login: "octo-e", number: 1, role: RoleSuggested, at: testNow},
	}

	got := rank(rows, testNow)
	assert.Equal(t, []string{octoA, "octo-b", "octo-c", "octo-d", "octo-e"}, logins(got))
	assert.InDelta(t, 9, got[0].Score, 0.001)
	assert.InDelta(t, 3*3.0/8, got[2].Score, 0.001)
	assert.Equal(t, 3, got[0].Reviewed)
}

func TestRankCountsEachRole(t *testing.T) {
	t.Parallel()

	rows := []seenRow{
		{key: octoA, login: octoA, number: 1, role: RoleAuthor, access: true, at: testNow},
		{key: octoA, login: octoA, number: 2, role: RoleAuthor, access: true, at: testNow},
		{key: octoA, login: octoA, number: 2, role: RoleCommitter, at: testNow},
		{key: octoA, login: octoA, number: 3, role: RoleReviewer, access: true, at: testNow.Add(-time.Hour)},
		{key: octoA, login: octoA, number: 4, role: RolePicked, at: testNow.Add(time.Hour)},
	}

	got := rank(rows, testNow)
	require.Len(t, got, 1)
	assert.Equal(t, Person{
		Login: octoA, Authored: 2, Committed: 1, Reviewed: 1, LastAt: testNow.Add(time.Hour), Score: got[0].Score,
	}, got[0])
	assert.Greater(t, got[0].Score, 2+2+1+2.5+2.9, "a time ahead of now counts as now")
}

func TestLearnPrunesToItsBounds(t *testing.T) {
	t.Parallel()

	t.Run("a repository's newest pull requests", func(t *testing.T) {
		t.Parallel()

		svc, db, _ := newService(t)

		seen := make([]Seen, 0, maxPullsPerRepo+5)
		for n := 1; n <= maxPullsPerRepo+5; n++ {
			seen = append(seen, Seen{Login: "octo-user", Number: n, Role: RoleAuthor, Access: true, At: testNow})
		}

		require.NoError(t, svc.Learn(t.Context(), exampleRepo, seen))
		assert.Equal(t, maxPullsPerRepo, count(t, db, "people_seen"))

		var oldest int
		require.NoError(t, db.QueryRowContext(t.Context(), "SELECT MIN(number) FROM people_seen").Scan(&oldest))
		assert.Equal(t, 6, oldest, "the oldest numbers go")
	})

	t.Run("a year of rows, and the people they name", func(t *testing.T) {
		t.Parallel()

		svc, db, clk := newService(t)

		require.NoError(t, svc.Learn(t.Context(), exampleRepo, []Seen{
			{Login: "octo-old", Number: 1, Role: RoleAuthor, Access: true, At: testNow.Add(-keptFor - time.Hour)},
			{Login: "octo-new", Number: 2, Role: RoleAuthor, Access: true, At: testNow},
		}))

		assert.Equal(t, 1, count(t, db, "people_seen"))
		assert.Equal(t, 1, count(t, db, "people"), "a person nothing names goes")

		clk.Advance(keptFor + time.Hour)
		require.NoError(t, svc.Learn(t.Context(), exampleRepo, nil))
		assert.Zero(t, count(t, db, "people_seen"))
		assert.Zero(t, count(t, db, "people"))
	})

	t.Run("the latest rows in all", func(t *testing.T) {
		t.Parallel()

		svc, db, _ := newService(t)
		assert.Equal(t, int64(maxSeen), svc.maxSeen)

		// A small bound stands in for the real one, which takes long to fill; the rows share
		// one time, so the cut is by row.
		svc.maxSeen = 50

		testutil.Exec(t, db, `INSERT INTO people (login_key, login, updated_at) VALUES ('octo-user', 'octo-user', ?)`, testNow)
		testutil.Exec(t, db, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 60)
INSERT INTO people_seen (repo, login_key, number, role, access, at)
SELECT 'acme-org/repo-' || (i / 10), 'octo-user', i, 'author', 1, ? FROM n`, testNow.Add(-time.Hour))

		require.NoError(t, svc.Learn(t.Context(), exampleRepo, []Seen{
			{Login: "octo-user", Number: 1, Role: RoleAuthor, Access: true, At: testNow},
		}))

		assert.Equal(t, 50, count(t, db, "people_seen"))

		var latest int
		require.NoError(t, db.QueryRowContext(t.Context(),
			"SELECT COUNT(*) FROM people_seen WHERE repo = 'acme-org/example-repo'").Scan(&latest))
		assert.Equal(t, 1, latest, "the row just seen is the latest, so it stays")
	})
}

func TestTeams(t *testing.T) {
	t.Parallel()

	svc, db, clk := newService(t)

	require.NoError(t, svc.SetTeams(t.Context(), exampleRepo, []Team{{Slug: "web", Name: "Web"}, {Slug: "api"}, {Slug: " "}}, true))
	clk.Advance(time.Minute)
	require.NoError(t, svc.SetTeams(t.Context(), exampleRepo, []Team{{Slug: "ops"}}, false))

	ranked, err := svc.Ranked(t.Context(), exampleRepo)
	require.NoError(t, err)
	assert.Equal(t, []Team{{Slug: "ops"}, {Slug: "api"}, {Slug: "web", Name: "Web"}}, ranked.Teams,
		"the teams asked to review first, then by slug")

	// A new list replaces the old; a team asked for stays.
	require.NoError(t, svc.SetTeams(t.Context(), exampleRepo, []Team{{Slug: "Web"}}, true))

	ranked, err = svc.Ranked(t.Context(), exampleRepo)
	require.NoError(t, err)
	assert.Equal(t, []Team{{Slug: "ops"}, {Slug: "Web", Name: "Web"}}, ranked.Teams, "the name kept, the new spelling")

	// A team asked for a year ago, and listed no more, goes.
	clk.Advance(keptFor + time.Hour)
	require.NoError(t, svc.SetTeams(t.Context(), exampleRepo, nil, true))
	assert.Zero(t, count(t, db, "people_teams"))

	require.ErrorIs(t, svc.SetTeams(t.Context(), repo.Name{}, nil, true), ErrInvalid)
}

func TestReadsAndStaleness(t *testing.T) {
	t.Parallel()

	svc, db, clk := newService(t)

	ranked, err := svc.Ranked(t.Context(), exampleRepo)
	require.NoError(t, err)
	assert.True(t, ranked.Stale, "never read")
	assert.True(t, ranked.ReadAt.IsZero())

	require.NoError(t, svc.MarkFailed(t.Context(), exampleRepo))

	ranked, err = svc.Ranked(t.Context(), exampleRepo)
	require.NoError(t, err)
	assert.False(t, ranked.Stale, "held off after a failure")

	clk.Advance(retryAfter)

	ranked, err = svc.Ranked(t.Context(), exampleRepo)
	require.NoError(t, err)
	assert.True(t, ranked.Stale, "due again once the back-off passed")

	require.NoError(t, svc.MarkRead(t.Context(), exampleRepo, 50, true))

	ranked, err = svc.Ranked(t.Context(), exampleRepo)
	require.NoError(t, err)
	assert.False(t, ranked.Stale)
	assert.Equal(t, clk.Now().UTC(), ranked.ReadAt)
	assert.Equal(t, 50, ranked.Pulls)
	assert.True(t, ranked.Org)

	clk.Advance(readEvery - time.Minute)

	ranked, err = svc.Ranked(t.Context(), exampleRepo)
	require.NoError(t, err)
	assert.False(t, ranked.Stale, "read within the week")

	clk.Advance(time.Minute)

	ranked, err = svc.Ranked(t.Context(), exampleRepo)
	require.NoError(t, err)
	assert.True(t, ranked.Stale, "a week old")

	// A failure after a read holds it off too, and keeps what the read found.
	require.NoError(t, svc.MarkFailed(t.Context(), exampleRepo))

	ranked, err = svc.Ranked(t.Context(), exampleRepo)
	require.NoError(t, err)
	assert.False(t, ranked.Stale)
	assert.Equal(t, 50, ranked.Pulls)

	// A repository not read or failed in a year is forgotten.
	clk.Advance(keptFor + time.Hour)
	require.NoError(t, svc.MarkFailed(t.Context(), repo.Name{Owner: "acme-org", Repo: "other-repo"}))
	assert.Equal(t, 1, count(t, db, "people_reads"))

	require.ErrorIs(t, svc.MarkRead(t.Context(), repo.Name{}, 1, false), ErrInvalid)
	require.ErrorIs(t, svc.MarkFailed(t.Context(), repo.Name{}), ErrInvalid)
}

func TestRepositoryNamesIgnoreCase(t *testing.T) {
	t.Parallel()

	svc, _, _ := newService(t)

	require.NoError(t, svc.Learn(t.Context(), repo.Name{Owner: "Acme-Org", Repo: "Example-Repo"}, []Seen{
		{Login: "octo-user", Number: 1, Role: RoleAuthor, Access: true},
	}))

	ranked, err := svc.Ranked(t.Context(), exampleRepo)
	require.NoError(t, err)
	assert.Equal(t, []string{"octo-user"}, logins(ranked.People))
	assert.Equal(t, testNow, ranked.People[0].LastAt, "a zero time is now")
}

func TestErrorsReachTheCaller(t *testing.T) {
	t.Parallel()

	svc, db, _ := newService(t)

	for _, table := range []string{"people_seen", "people_teams", "people_reads"} {
		testutil.Exec(t, db, "DROP TABLE "+table)
	}

	require.Error(t, svc.Learn(t.Context(), exampleRepo, []Seen{{Login: "octo-user", Number: 1, Role: RoleAuthor}}))
	require.Error(t, svc.SetTeams(t.Context(), exampleRepo, []Team{{Slug: "web"}}, true))
	require.Error(t, svc.SetTeams(t.Context(), exampleRepo, []Team{{Slug: "web"}}, false))
	require.Error(t, svc.MarkRead(t.Context(), exampleRepo, 1, false))
	require.Error(t, svc.MarkFailed(t.Context(), exampleRepo))

	_, err := svc.Ranked(t.Context(), exampleRepo)
	require.Error(t, err)
}
