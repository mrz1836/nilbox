package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
