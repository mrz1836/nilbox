package issues

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// labelBug is the label the edit tests add and take off.
const labelBug = "bug"

func TestLabelsReadsTheRepositorysLabelsOnceForTenMinutes(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.repoLabels = &github.RepoLabels{
		Labels: []github.RepoLabel{{Name: labelBug, Color: "#d73a4a", Description: "Something isn't working"}, {Name: "p1"}},
		Total:  2,
	}

	want := RepoLabels{
		Labels: []RepoLabel{{Name: labelBug, Color: "#d73a4a", Description: "Something isn't working"}, {Name: "p1"}},
		Total:  2,
	}

	got, err := h.svc.Labels(t.Context(), issueID)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	got, err = h.svc.Labels(t.Context(), issueID)
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, 1, h.github.called(callLabels), "kept, so read once")

	h.clock.Advance(10 * time.Minute)

	_, err = h.svc.Labels(t.Context(), issueID)
	require.NoError(t, err)
	assert.Equal(t, 2, h.github.called(callLabels), "read again once ten minutes have passed")
}

func TestLabelsSaysWhyItCouldntRead(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	got, err := h.svc.Labels(t.Context(), issueID)
	require.ErrorIs(t, err, errNotCalled)
	assert.NotNil(t, got.Labels)

	h.inbox.issue.Kind = item.KindPR

	_, err = h.svc.Labels(t.Context(), issueID)
	require.ErrorIs(t, err, ErrNotIssue)
}

func TestTheMemoKeepsTheNewestReads(t *testing.T) {
	t.Parallel()

	l := memo[RepoLabels]{size: labelRepos}

	for i := range labelRepos + 1 {
		l.put(string(rune('a'+i)), RepoLabels{Total: i}, testNow.Add(time.Duration(i)*time.Second))
	}

	_, ok := l.get("a", testNow.Add(time.Minute))
	assert.False(t, ok, "the oldest gave way")

	kept, ok := l.get("b", testNow.Add(time.Minute))
	require.True(t, ok)
	assert.Equal(t, 1, kept.Total)

	_, ok = l.get("b", testNow.Add(-time.Minute))
	assert.False(t, ok, "a read from the future is no read")
}

func TestAssignableSearchesOnceForTheSameWordsAndMarksTheUser(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.people = []github.Assignable{{Login: "Octo-User", Name: "Octo"}, {Login: octoOther}}

	got, err := h.svc.Assignable(t.Context(), issueID, " octo")
	require.NoError(t, err)
	assert.Equal(t, []Candidate{{Login: "Octo-User", Name: "Octo", Mine: true}, {Login: octoOther}}, got)

	_, err = h.svc.Assignable(t.Context(), issueID, "OCTO ")
	require.NoError(t, err)
	_, err = h.svc.Assignable(t.Context(), issueID, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"octo", ""}, h.github.searched, "the same words are kept ten minutes")

	h.clock.Advance(10 * time.Minute)

	_, err = h.svc.Assignable(t.Context(), issueID, "octo")
	require.NoError(t, err)
	assert.Len(t, h.github.searched, 3)

	h.inbox.issue.Kind = item.KindPR

	got, err = h.svc.Assignable(t.Context(), issueID, "octo")
	require.ErrorIs(t, err, ErrNotIssue)
	assert.NotNil(t, got)
}

func TestEditSendsEachChangeOnce(t *testing.T) {
	t.Parallel()

	h := readHarness(t)

	got, err := h.svc.Edit(t.Context(), EditRequest{
		ItemID:          issueID,
		AddLabels:       []string{"triage", " Triage ", "", labelBug},
		RemoveLabels:    []string{"p1", labelBug},
		AddAssignees:    []string{octoOther, "octo-fourth"},
		RemoveAssignees: []string{"octo-third", "Octo-Fourth"},
	})
	require.NoError(t, err)
	assert.Equal(t, EditResult{Sent: 4}, got)

	assert.Equal(t, []string{"+label:triage", "-label:p1", "+assignee:" + octoOther, "-assignee:octo-third"},
		h.github.edits, "a name given twice goes once, and one both added and taken off not at all")
	assert.True(t, h.cached(t).fetchedAt.IsZero(), "the sheet reads it again next")
}

func TestEditNeverSendsAStepTwice(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	h.github.editErr = map[string]error{"-assignee:octo-third": badCredentials()}

	got, err := h.svc.Edit(t.Context(), EditRequest{
		ItemID: issueID, AddLabels: []string{"triage"}, RemoveAssignees: []string{"octo-third"},
	})
	require.NoError(t, err)
	assert.Equal(t, EditResult{Sent: 2}, got)

	assert.Equal(t, []string{"+label:triage", "-assignee:octo-third"}, h.github.edits, "the retry sent the rest alone")
	assert.Equal(t, 2, h.inbox.tries)
}

func TestEditStopsAtTheFirstStepThatFails(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	h.github.editErr = map[string]error{"-label:p1": uierr.New("GitHub kept \"p1\".", github.ErrEditIgnored)}

	got, err := h.svc.Edit(t.Context(), EditRequest{
		ItemID: issueID, AddLabels: []string{"triage"}, RemoveLabels: []string{"p1"}, AddAssignees: []string{octoOther},
	})
	require.NoError(t, err, "GitHub made a change, which the sheet reads again")
	assert.Equal(t, EditResult{Sent: 1, Failed: "GitHub kept \"p1\"."}, got)
	assert.Equal(t, []string{"+label:triage"}, h.github.edits, "what GitHub made before it stands")

	h.github.editErr = map[string]error{"+label:triage": github.ErrEditIgnored}

	_, err = h.svc.Edit(t.Context(), EditRequest{ItemID: issueID, AddLabels: []string{"triage"}})
	require.ErrorIs(t, err, github.ErrEditIgnored, "nothing was made, so nothing to read again")
}

func TestEditWordsSomeoneElseGitHubKeptAssigned(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	h.github.editErr = map[string]error{"-assignee:" + octoOther: github.ErrEditIgnored}

	_, err := h.svc.Edit(t.Context(), EditRequest{ItemID: issueID, RemoveAssignees: []string{octoOther}})
	require.ErrorIs(t, err, github.ErrEditIgnored)
	assert.Equal(t, "GitHub kept "+octoOther+" assigned to "+exampleRepo+"#12. "+
		"Your account may not be allowed to change its assignees.", uierr.Message(err))
}

func TestEditRefusesWhatTheUserCantDoThere(t *testing.T) {
	t.Parallel()

	h := readHarness(t)

	_, err := h.svc.Edit(t.Context(), EditRequest{ItemID: issueID, RemoveAssignees: []string{"Octo-User"}})
	require.ErrorIs(t, err, ErrCannotEdit, "Unassign me takes the user off")

	got, err := h.svc.Edit(t.Context(), EditRequest{ItemID: issueID, AddLabels: []string{" "}})
	require.NoError(t, err)
	assert.Zero(t, got.Sent)
	assert.Zero(t, h.inbox.acts, "nothing to send")

	h.inbox.issue.ViewerPermission = "READ"

	_, err = h.svc.Edit(t.Context(), EditRequest{ItemID: issueID, AddLabels: []string{labelBug}})
	require.ErrorIs(t, err, ErrCannotEdit)

	h.inbox.issue.ViewerPermission, h.inbox.issue.Kind = "TRIAGE", item.KindPR

	_, err = h.svc.Edit(t.Context(), EditRequest{ItemID: issueID, AddLabels: []string{labelBug}})
	require.ErrorIs(t, err, ErrNotIssue)

	unread := newHarness(t)

	_, err = unread.svc.Edit(t.Context(), EditRequest{ItemID: issueID, AddLabels: []string{labelBug}})
	require.ErrorIs(t, err, ErrCannotEdit, "the sheet hasn't read who it is assigned to")
	assert.Empty(t, h.github.edits)
}

func TestTheSheetSaysWhetherTheUserMayEditIt(t *testing.T) {
	t.Parallel()

	assert.False(t, newHarness(t).opened(t).CanEdit, "not before the sheet read it")

	h := readHarness(t)
	assert.True(t, h.opened(t).CanEdit, "write access, on an open issue")

	h.inbox.issue.ViewerPermission = "TRIAGE"
	assert.True(t, h.opened(t).CanEdit)

	h.inbox.issue.ViewerPermission = "READ"
	assert.False(t, h.opened(t).CanEdit)
}
