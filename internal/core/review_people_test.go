package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/assignees"
	"github.com/mrz1836/nilbox/internal/people"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/review"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestTheReviewSheetsRolesAreTheDirectorysOwn(t *testing.T) {
	t.Parallel()

	assert.Equal(t, map[string]people.Role{
		review.RoleAuthor: people.RoleAuthor, review.RoleReviewer: people.RoleReviewer,
		review.RoleCommitter: people.RoleCommitter, review.RoleRequested: people.RoleRequested,
		review.RoleSuggested: people.RoleSuggested, review.RolePicked: people.RolePicked,
	}, map[string]people.Role{
		review.RoleAuthor: people.Role(review.RoleAuthor), review.RoleReviewer: people.Role(review.RoleReviewer),
		review.RoleCommitter: people.Role(review.RoleCommitter), review.RoleRequested: people.Role(review.RoleRequested),
		review.RoleSuggested: people.Role(review.RoleSuggested), review.RolePicked: people.Role(review.RolePicked),
	}, "core converts a role by its name")
}

func TestReviewPeopleAdaptsTheDirectory(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC)
	adapter := reviewPeople{directory: people.NewService(people.Config{Pool: testutil.DB(t), Clock: testutil.NewClock(at)})}
	name := repo.Name{Owner: boundOwner, Repo: ghRepo}

	require.NoError(t, adapter.Learn(t.Context(), name, []review.PersonSeen{
		{Login: "octo-member", Name: "Octo Member", Number: 7, Role: review.RoleReviewer, Access: true, At: at},
	}))
	require.NoError(t, adapter.SetTeams(t.Context(), name, []review.TeamCandidate{{Slug: "web", Name: "Web"}}, true))
	require.NoError(t, adapter.MarkFailed(t.Context(), name))
	require.NoError(t, adapter.MarkRead(t.Context(), name, 50, true))

	got, err := adapter.Ranked(t.Context(), name)
	require.NoError(t, err)
	assert.Equal(t, review.Directory{
		People: []review.KnownPerson{{Login: "octo-member", Name: "Octo Member", Reviewed: 1, LastAt: at}},
		Teams:  []review.TeamCandidate{{Slug: "web", Name: "Web"}},
		ReadAt: at, Pulls: 50, Org: true,
	}, got)

	_, err = adapter.Ranked(t.Context(), repo.Name{})
	require.ErrorIs(t, err, people.ErrInvalid)
}

func TestReviewPeopleCountsAndForgetsTheDirectory(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	adapter := reviewPeople{directory: people.NewService(people.Config{Pool: db, Clock: testutil.NewClock(time.Now())})}
	name := repo.Name{Owner: boundOwner, Repo: ghRepo}

	require.NoError(t, adapter.Learn(t.Context(), name, []review.PersonSeen{
		{Login: "octo-member", Number: 7, Role: review.RoleReviewer, Access: true},
	}))

	got, err := adapter.Summary(t.Context())
	require.NoError(t, err)
	assert.Equal(t, review.PeopleSummary{People: 1, Repos: 1}, got)

	require.NoError(t, adapter.Forget(t.Context()))

	got, err = adapter.Summary(t.Context())
	require.NoError(t, err)
	assert.Equal(t, review.PeopleSummary{}, got)

	testutil.Exec(t, db, "DROP TABLE people_teams")

	_, err = adapter.Summary(t.Context())
	require.Error(t, err)
}

func TestAssigneesDirectoryAdaptsTheDirectory(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC)
	directory := people.NewService(people.Config{Pool: testutil.DB(t), Clock: testutil.NewClock(at)})
	adapter := assigneesDirectory{directory: directory}
	name := repo.Name{Owner: boundOwner, Repo: ghRepo}
	other := repo.Name{Owner: boundOwner, Repo: "other-repo"}

	require.NoError(t, directory.Learn(t.Context(), name, []people.Seen{
		{Login: "octo-member", Name: "Octo Member", Number: 7, Role: people.RoleReviewer, Access: true, At: at},
	}))
	require.NoError(t, directory.Learn(t.Context(), other, []people.Seen{
		{Login: "octo-elsewhere", Number: 3, Role: people.RoleAuthor, Access: true, At: at},
	}))
	require.NoError(t, adapter.Assigned(t.Context(), name, []string{"octo-assigned"}))

	covered, err := adapter.Covers(t.Context(), name, "octo")
	require.NoError(t, err)
	assert.False(t, covered)

	require.NoError(t, adapter.Keep(t.Context(), name, "octo", assignees.Found{
		People: []assignees.Person{{Login: "octo-listed", Name: "Octo Listed"}}, Total: 1,
	}))
	require.NoError(t, adapter.MarkFailed(t.Context(), name, ""))

	covered, err = adapter.Covers(t.Context(), name, "octo-l")
	require.NoError(t, err)
	assert.True(t, covered, "a search that named everyone covers a longer one")

	got, err := adapter.Known(t.Context(), name)
	require.NoError(t, err)
	assert.Equal(t, assignees.Known{
		Assignable: []assignees.Assignable{
			{Login: "octo-assigned", AssignedAt: at}, {Login: "octo-listed", Name: "Octo Listed", Listed: true},
		},
		WorksHere: []assignees.Worker{{Login: "octo-member", Name: "Octo Member", Reviewed: 1, LastAt: at}},
		Org: []assignees.Worker{
			{Login: "octo-elsewhere", Authored: 1, LastAt: at, Where: boundOwner + "/other-repo"},
		},
	}, got, "held off after the failed read of the whole list, so not stale")

	require.NoError(t, adapter.Keep(t.Context(), name, "", assignees.Found{
		People: []assignees.Person{{Login: "octo-listed"}}, Total: 1,
	}))

	got, err = adapter.Known(t.Context(), name)
	require.NoError(t, err)
	assert.True(t, got.Complete)
	assert.Equal(t, at, got.ReadAt)
	assert.Equal(t, []assignees.Worker{}, got.Org, "a whole list that named everyone reads no org")

	_, err = adapter.Known(t.Context(), repo.Name{})
	require.ErrorIs(t, err, people.ErrInvalid)
}
