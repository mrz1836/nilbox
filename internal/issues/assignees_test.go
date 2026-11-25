package issues

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/assignees"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
)

// assigneeLogins lists the candidates' logins, in order.
func assigneeLogins(c assignees.Candidates) []string {
	out := make([]string, 0, len(c.People))
	for _, p := range c.People {
		out = append(out, p.Login)
	}

	return out
}

func TestAssigneeCandidatesAskGitHubNothing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	got, err := h.svc.AssigneeCandidates(t.Context(), issueID)
	require.NoError(t, err)
	assert.Equal(t, []string{octoUser, octoOther}, assigneeLogins(got), "the user and the author, before a read")
	assert.True(t, got.Stale)

	h.refreshed(t)

	got, err = h.svc.AssigneeCandidates(t.Context(), issueID)
	require.NoError(t, err)
	assert.Equal(t, []string{octoUser, octoOther}, assigneeLogins(got), "its participants are the same people")
	assert.True(t, got.People[0].Mine)
	assert.Equal(t, assignees.SourceAuthor, got.People[1].Source)
	assert.Empty(t, h.github.searched)
}

func TestSearchAssigneesReachesGitHubOnlyWhenNeeded(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	h.github.people, h.github.total = []github.Person{{Login: octoThird, Name: "Octo Third"}}, 150

	got, err := h.svc.LearnAssignees(t.Context(), issueID)
	require.NoError(t, err)
	assert.Equal(t, []string{"issue:"}, h.github.searched, "the whole list, of an issue's repository")
	assert.Equal(t, []string{octoUser, octoOther, octoThird}, assigneeLogins(got))
	assert.False(t, got.Complete, "one of 150")

	got, err = h.svc.SearchAssignees(t.Context(), issueID, "third")
	require.NoError(t, err)
	assert.Equal(t, []string{"issue:", "issue:third"}, h.github.searched, "a list cut short is searched")
	assert.Equal(t, []string{octoThird}, assigneeLogins(got), "those who match")
	assert.Equal(t, assignees.SourceFound, got.People[0].Source)

	_, err = h.svc.SearchAssignees(t.Context(), issueID, "THIRD")
	require.NoError(t, err)
	assert.Len(t, h.github.searched, 2, "the same words again ask nothing")

	_, err = h.svc.SearchAssignees(t.Context(), issueID, "  ")
	require.NoError(t, err)
	assert.Len(t, h.github.searched, 2, "no words, no search")
}

func TestAReadLearnsWhoIsAssigned(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.refreshed(t)

	assert.Equal(t, []string{octoUser}, h.assignable.SeenAssigned())
}

func TestTheAssigneeReadsOfWhatIsNoIssue(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.inbox.issue.Kind = item.KindPR

	for _, call := range []func() (assignees.Candidates, error){
		func() (assignees.Candidates, error) { return h.svc.AssigneeCandidates(t.Context(), issueID) },
		func() (assignees.Candidates, error) { return h.svc.LearnAssignees(t.Context(), issueID) },
		func() (assignees.Candidates, error) { return h.svc.SearchAssignees(t.Context(), issueID, "octo") },
	} {
		got, err := call()
		require.ErrorIs(t, err, ErrNotIssue)
		assert.NotNil(t, got.People, "never nil")
	}

	assert.Empty(t, h.github.searched)
}

func TestTheInboxsUnassignMeHasTheSheetReadTheIssueAgain(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	h.github.views = append(h.github.views, issueView())

	h.refreshed(t)
	require.Equal(t, 1, h.github.called(callView), "a read under 20 seconds old is served again")

	edits := NewInboxEdits(h.svc)
	require.NoError(t, edits.Unassigned(t.Context(), issueID))

	h.refreshed(t)
	assert.Equal(t, 2, h.github.called(callView), "the next refresh reads the issue, however recently it was read")
	require.NoError(t, edits.Unassigned(t.Context(), issueID+1), "an issue with no cache is no error")
}
