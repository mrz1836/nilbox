package review

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/assignees"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/timeline"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// readOnly is an account's permission that can't assign, and octoReviewer the person who
// reviewed the tests' pull request.
const (
	readOnly     = "READ"
	octoReviewer = "octo-reviewer"
)

// assignedTo returns a harness whose sheet read #7 assigned to logins, as GitHub has it.
func assignedTo(t *testing.T, logins ...string) *harness {
	t.Helper()

	h := newHarness(t)
	h.github.review.Assignees = github.People{Logins: logins, Total: len(logins)}
	h.github.assigned = logins
	h.refreshed(t)

	return h
}

// contextAt returns when the sheet's cache of #7 says it was read, nil once expired.
func contextAt(t *testing.T, db *sql.DB) *time.Time {
	t.Helper()

	var at *time.Time
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT context_at FROM review_diffs WHERE item_id = ?", pullID).Scan(&at))

	return at
}

// assigneeNames lists the logins r shows assigned, the user's with "(you)".
func assigneeNames(r Review) []string {
	out := []string{}

	for _, a := range r.Assignees {
		if a.Mine {
			out = append(out, a.Login+" (you)")
		} else {
			out = append(out, a.Login)
		}
	}

	return out
}

func TestAReadKeepsWhoThePullRequestIsAssignedTo(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	r, err := h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, []Assignee{}, r.Assignees, "none before the sheet read it, never nil")
	assert.False(t, r.CanEditAssignees, "not before the sheet read who it is assigned to")

	h.github.review.Assignees = github.People{Logins: []string{octoOther, "Octo-User"}, Total: 12}

	r = h.refreshed(t)
	assert.Equal(t, []string{octoOther, "Octo-User (you)"}, assigneeNames(r))
	assert.Equal(t, 10, r.MoreAssignees, "the ones the read didn't name")
	assert.True(t, r.CanEditAssignees)
	assert.Equal(t, []string{octoOther, "Octo-User"}, h.assignable.SeenAssigned(), "the directory learns who is assigned")

	h.inbox.pull.ViewerPermission = "TRIAGE"

	r, err = h.svc.Rebuild(t.Context(), pullID)
	require.NoError(t, err)
	assert.True(t, r.CanEditAssignees, "triage assigns")

	h.inbox.pull.ViewerPermission = readOnly

	r, err = h.svc.Rebuild(t.Context(), pullID)
	require.NoError(t, err)
	assert.False(t, r.CanEditAssignees)
}

func TestACacheAnOlderBuildWroteCantEditTheAssignees(t *testing.T) {
	t.Parallel()

	h := assignedTo(t, octoOther)
	testutil.Exec(t, h.db, "UPDATE review_diffs SET context = json_remove(context, '$.assignees')")

	r, err := h.svc.Rebuild(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, []Assignee{}, r.Assignees, "it says nothing of them")
	assert.False(t, r.CanEditAssignees)
	assert.True(t, r.CanEditReviewers, "the rest stands")

	_, err = h.svc.EditAssignees(t.Context(), AssigneesRequest{ItemID: pullID, Add: []string{octoA}})
	require.ErrorIs(t, err, ErrCannotEditAssignees)
	assert.Equal(t, "Reload the pull request to read who it is assigned to, then try again.", uierr.Message(err))

	assert.True(t, h.refreshedAgain(t).CanEditAssignees, "a read brings them back")
}

// refreshedAgain reads the pull request again however recently the sheet did.
func (h *harness) refreshedAgain(t *testing.T) Review {
	t.Helper()

	r, err := h.svc.ReadAgain(t.Context(), pullID)
	require.NoError(t, err)

	return r
}

func TestEditAssigneesUnassignsThenAssignsAndRebuildsWithNoRead(t *testing.T) {
	t.Parallel()

	h := assignedTo(t, octoOther, "octo-b")
	readAt := contextAt(t, h.db)

	res, err := h.svc.EditAssignees(t.Context(), AssigneesRequest{
		ItemID: pullID, Add: []string{octoA, " " + octoUser + " ", "OCTO-A", ""}, Remove: []string{octoOther, "octo-x", "octo-x"},
	})
	require.NoError(t, err)

	assert.Equal(t, AssigneesResult{Sent: 2}, res)
	assert.Equal(t, []string{callUnassign, callAssign}, h.github.requests(), "one request to take off, then one to assign")
	assert.Equal(t, []string{"octo-b", octoA, octoUser}, h.github.assigned)
	assert.Equal(t, 1, h.inbox.acts, "one action, through the inbox's Actor")

	r, err := h.svc.Rebuild(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, []string{"octo-b", octoA, "octo-user (you)"}, assigneeNames(r), "as GitHub answered the last step")
	assert.Equal(t, 1, h.github.called(callReview), "no read")
	assert.Equal(t, readAt, contextAt(t, h.db), "when the sheet read it stands")
	assert.Equal(t, []string{octoOther, "octo-b", octoA, octoUser}, h.assignable.SeenAssigned(),
		"the read's, then those the user assigned")
}

func TestEditAssigneesThatAssignsTheUserTellsTheInbox(t *testing.T) {
	t.Parallel()

	h := assignedTo(t, octoOther)
	h.github.assignBadToken = true

	_, err := h.svc.EditAssignees(t.Context(), AssigneesRequest{ItemID: pullID, Add: []string{"Octo-User"}})
	require.NoError(t, err)
	require.Len(t, h.inbox.acted, 1)
	assert.Equal(t, Acted{SelfAssigned: true, SelfAssignedAt: testNow}, h.inbox.acted[0],
		"the inbox lists it as assigned at once, at GitHub's time of the edit, made on the second try")

	// The user is assigned now, and assigns someone else in another's place.
	_, err = h.svc.EditAssignees(t.Context(), AssigneesRequest{ItemID: pullID, Add: []string{octoA}, Remove: []string{octoOther}})
	require.NoError(t, err)
	require.Len(t, h.inbox.acted, 2)
	assert.Equal(t, Acted{}, h.inbox.acted[1], "assigning only others records nothing")
}

func TestEditAssigneesSendsOnlyWhatChanges(t *testing.T) {
	t.Parallel()

	h := assignedTo(t, octoOther)

	res, err := h.svc.EditAssignees(t.Context(), AssigneesRequest{ItemID: pullID, Remove: []string{octoOther}})
	require.NoError(t, err)
	assert.Equal(t, AssigneesResult{Sent: 1}, res)
	assert.Equal(t, []string{callUnassign}, h.github.requests())
	assert.Equal(t, []string{octoOther}, h.assignable.SeenAssigned(), "no one assigned")

	res, err = h.svc.EditAssignees(t.Context(), AssigneesRequest{ItemID: pullID, Add: []string{octoA}, Remove: []string{"Octo-A"}})
	require.NoError(t, err)
	assert.Equal(t, AssigneesResult{}, res, "one both added and taken off is sent not at all")
	assert.Equal(t, []string{callUnassign}, h.github.requests())
}

func TestEditAssigneesNeverSendsAStepTwice(t *testing.T) {
	t.Parallel()

	h := assignedTo(t, octoOther)
	h.github.assignBadToken = true

	res, err := h.svc.EditAssignees(t.Context(), AssigneesRequest{
		ItemID: pullID, Add: []string{octoA}, Remove: []string{octoOther},
	})
	require.NoError(t, err)

	assert.Equal(t, AssigneesResult{Sent: 2}, res)
	assert.Equal(t, 1, h.github.called(callUnassign), "the request GitHub made isn't sent again with the new token")
	assert.Equal(t, 2, h.github.called(callAssign))
	assert.Equal(t, 2, h.inbox.tries)
	assert.Equal(t, []string{octoA}, h.github.assigned)
}

func TestEditAssigneesKeepsWhatAFirstStepMade(t *testing.T) {
	t.Parallel()

	h := assignedTo(t, octoOther)
	h.github.assignErr = uierr.New("GitHub didn't assign octo-a.", github.ErrEditIgnored)

	res, err := h.svc.EditAssignees(t.Context(), AssigneesRequest{
		ItemID: pullID, Add: []string{octoA}, Remove: []string{octoOther},
	})
	require.NoError(t, err)
	assert.Equal(t, AssigneesResult{Sent: 1, Failed: "GitHub didn't assign octo-a."}, res)

	r, err := h.svc.Rebuild(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, []string{}, assigneeNames(r), "the unassignment GitHub made stands")
	assert.Equal(t, []string{octoOther}, h.assignable.SeenAssigned(), "no one was assigned")

	h.github.unassignErr = uierr.New("GitHub refused the change.", errFakeRead)

	res, err = h.svc.EditAssignees(t.Context(), AssigneesRequest{ItemID: pullID, Add: []string{octoA}, Remove: []string{"octo-b"}})
	require.ErrorIs(t, err, errFakeRead, "a failure before any step is the error")
	assert.Equal(t, AssigneesResult{}, res)
	assert.Equal(t, 1, h.github.called(callAssign), "it stops at the first step that fails")
}

func TestEditAssigneesExpiresACacheItCantRewrite(t *testing.T) {
	t.Parallel()

	h := assignedTo(t, octoOther)
	testutil.Exec(t, h.db, `CREATE TRIGGER no_patch BEFORE UPDATE OF context ON review_diffs
BEGIN SELECT RAISE(ABORT, 'the cache is read-only'); END`)

	res, err := h.svc.EditAssignees(t.Context(), AssigneesRequest{ItemID: pullID, Add: []string{octoA}})
	require.NoError(t, err)
	assert.Equal(t, AssigneesResult{Sent: 1}, res, "GitHub made it")
	assert.Nil(t, contextAt(t, h.db), "the next refresh reads it again")
	assert.Contains(t, h.logs.String(), "could not keep the pull request's assignees")
}

func TestEditAssigneesRefusesWhatItCannotSend(t *testing.T) {
	t.Parallel()

	nine := []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9"}
	ten := []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "a10"}

	tests := []struct {
		name     string
		assigned []string
		setup    func(h *harness)
		req      AssigneesRequest
		want     error
		words    string
	}{
		{
			name: "not read", setup: func(h *harness) { testutil.Exec(t, h.db, "DELETE FROM review_diffs") },
			req:  AssigneesRequest{Add: []string{octoA}},
			want: ErrCannotEditAssignees, words: "Wait for the pull request to load from GitHub, then try again.",
		},
		{
			name: "without triage", setup: func(h *harness) { h.inbox.pull.ViewerPermission = readOnly },
			req:  AssigneesRequest{Add: []string{octoA}},
			want: ErrCannotEditAssignees, words: "Your account can't change this pull request's assignees: that takes triage access.",
		},
		{
			name: "closed",
			setup: func(h *harness) {
				testutil.Exec(t, h.db, `UPDATE review_diffs SET context = json_set(context, '$.state', 'CLOSED')`)
			},
			req:  AssigneesRequest{Add: []string{octoA}},
			want: ErrCannotEditAssignees, words: "This pull request is no longer open, so its assignees can't change.",
		},
		{
			name: "the user taken off", assigned: []string{octoUser}, setup: func(*harness) {},
			req:  AssigneesRequest{Remove: []string{"Octo-User"}},
			want: ErrOwnAssignment, words: "Take yourself off with Unassign me, in the card's menu.",
		},
		{
			name: "more than ten", assigned: nine, setup: func(*harness) {},
			req:  AssigneesRequest{Add: []string{octoA, "octo-b"}},
			want: ErrTooManyAssignees, words: "GitHub assigns a pull request to 10 people at most. Unassign someone first.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := assignedTo(t, tt.assigned...)
			tt.setup(h)

			tt.req.ItemID = pullID
			_, err := h.svc.EditAssignees(t.Context(), tt.req)
			require.ErrorIs(t, err, tt.want)
			assert.Equal(t, tt.words, uierr.Message(err))
			assert.Zero(t, h.github.called(callAssign)+h.github.called(callUnassign), "nothing is sent")
		})
	}

	// A swap at the cap fits: the unassignment goes first.
	h := assignedTo(t, ten...)

	res, err := h.svc.EditAssignees(t.Context(), AssigneesRequest{ItemID: pullID, Add: []string{octoA}, Remove: []string{"a10"}})
	require.NoError(t, err)
	assert.Equal(t, AssigneesResult{Sent: 2}, res)

	h = newHarness(t)
	h.inbox.pulled = errDirectory

	_, err = h.svc.EditAssignees(t.Context(), AssigneesRequest{ItemID: pullID, Add: []string{octoA}})
	require.ErrorIs(t, err, errDirectory)

	got, err := h.svc.AssigneeCandidates(t.Context(), pullID)
	require.ErrorIs(t, err, errDirectory)
	assert.NotNil(t, got.People)
}

func TestAssigneeCandidatesAskGitHubNothing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.review.Reviews = []github.Review{
		{Author: octoReviewer, AuthorType: "User", State: "APPROVED"},
		{Author: "helper", AuthorType: "Bot", State: "COMMENTED"},
	}
	h.github.review.Timeline = github.TimelinePage{Items: []github.TimelineItem{
		{Type: github.TimelineComment, ID: "IC_1", Actor: "octo-commenter", ActorType: "User"},
		{Type: github.TimelineComment, ID: "IC_2", Actor: "renovate", ActorType: "Bot"},
	}}
	h.refreshed(t)

	got, err := h.svc.AssigneeCandidates(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, []string{octoUser, octoOther, octoReviewer, "octo-commenter"}, assigneeLogins(got),
		"the user, the author, then the people who took part")
	assert.Equal(t, assignees.SourceYou, got.People[0].Source)
	assert.True(t, got.Stale)
	assert.Empty(t, h.github.searched)
}

// assigneeLogins lists the candidates' logins, in order.
func assigneeLogins(c assignees.Candidates) []string {
	out := make([]string, 0, len(c.People))
	for _, p := range c.People {
		out = append(out, p.Login)
	}

	return out
}

func TestLearnAndSearchAssigneesReadThePullRequestsRepository(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.assignable = &github.AssignableUsers{People: []github.Person{{Login: "octo-listed", Name: "Octo Listed"}}, Total: 3}

	got, err := h.svc.LearnAssignees(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, []string{"pr:"}, h.github.searched, "the whole list, of a pull request's repository")
	assert.Contains(t, assigneeLogins(got), "octo-listed")
	assert.False(t, got.Complete, "one of three")
	assert.Equal(t, 1, h.inbox.peeks, "a read that stores nothing")

	_, err = h.svc.LearnAssignees(t.Context(), pullID)
	require.NoError(t, err)
	assert.Len(t, h.github.searched, 1, "not due again")

	got, err = h.svc.SearchAssignees(t.Context(), pullID, " Octo ")
	require.NoError(t, err)
	assert.Equal(t, []string{"pr:", "pr:Octo"}, h.github.searched)
	assert.Equal(t, assignees.SourceFound, got.People[len(got.People)-1].Source)

	_, err = h.svc.SearchAssignees(t.Context(), pullID, "Octo")
	require.NoError(t, err)
	assert.Len(t, h.github.searched, 2, "answered by what the search kept")

	h.github.assignableErr = uierr.New("GitHub is down.", errFakeRead)

	_, err = h.svc.SearchAssignees(t.Context(), pullID, "zed")
	require.ErrorIs(t, err, errFakeRead)
	assert.Equal(t, `Couldn't search GitHub for "zed" in acme-org/example-repo: GitHub is down.`, uierr.Message(err))
	assert.Equal(t, []string{"zed"}, h.assignable.Failed())
}

func TestASheetWithoutAFinderOffersNoOne(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.svc = NewService(Config{
		Pool: h.db, Inbox: h.inbox, GitHub: h.github, Settings: fakeSettings{ignored: h.ignored}, Replies: fakeReplies{},
		Clock: h.clock,
	})
	h.github.review.Assignees = github.People{Logins: []string{octoOther}, Total: 1}
	h.refreshed(t)

	got, err := h.svc.LearnAssignees(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, assignees.Candidates{People: []assignees.Candidate{}}, got)
	assert.Empty(t, h.github.searched)

	res, err := h.svc.EditAssignees(t.Context(), AssigneesRequest{ItemID: pullID, Add: []string{octoA}})
	require.NoError(t, err, "the edit itself needs no directory")
	assert.Equal(t, AssigneesResult{Sent: 1}, res)

	summary, err := h.svc.PeopleSummary(t.Context())
	require.NoError(t, err)
	assert.Equal(t, PeopleSummary{}, summary)

	summary, err = h.svc.ForgetPeople(t.Context())
	require.NoError(t, err)
	assert.Equal(t, PeopleSummary{}, summary)
}

func TestForgetPeopleEmptiesTheDirectory(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.review = peopleReview()
	h.refreshed(t)

	summary, err := h.svc.PeopleSummary(t.Context())
	require.NoError(t, err)
	assert.Positive(t, summary.People)

	summary, err = h.svc.ForgetPeople(t.Context())
	require.NoError(t, err)
	assert.Equal(t, PeopleSummary{}, summary, "what is left")
	assert.Empty(t, h.people.seenAs(RoleReviewer))

	h.people.err = errDirectory

	_, err = h.svc.ForgetPeople(t.Context())
	require.ErrorIs(t, err, errDirectory)

	_, err = h.svc.PeopleSummary(t.Context())
	require.ErrorIs(t, err, errDirectory)
}

func TestParticipantsAreThePeopleWhoTookPart(t *testing.T) {
	t.Parallel()

	got := participants(pullContext{
		Reviews: []contextReview{{Author: octoReviewer}, {Author: "helper", Bot: true}, {}},
		Timeline: []timeline.Entry{
			{Kind: timeline.KindComment, Actor: "octo-commenter"},
			{Kind: timeline.KindReviewed, Actor: octoReviewer},
			{Kind: timeline.KindComment, Actor: "renovate", ActorIsBot: true},
			{Kind: timeline.KindMerged, Actor: "octo-merger"},
		},
	})
	assert.Equal(t, []string{octoReviewer, "octo-commenter", octoReviewer}, got,
		"rank lists each once; events say nothing of who took part")
}
