package issues

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// sawLatest is what the window says the user saw of #12: its comments as the sheet read
// them.
func sawLatest() Seen {
	return Seen{Comments: 2, Latest: "IC_2", LatestAt: stamp(latestAt)}
}

// comment is a comment GitHub reads before a close: by author, at, the user's when mine.
func comment(id, author string, at time.Time, mine bool) github.CommentMark {
	return github.CommentMark{ID: id, CreatedAt: at, Author: author, Mine: mine}
}

func TestACommentIsPostedOnce(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	res := h.act(t, ActRequest{Action: ActionComment, Body: "Fixed in #14."})

	assert.Equal(t, Result{
		Outcome: OutcomeCommented, Message: "Commented on " + issueRef, Steps: []string{"Commented"}, Commented: true,
	}, res)
	assert.Equal(t, []string{"Fixed in #14."}, h.github.comments)
	assert.Equal(t, []string{callComment}, h.github.requests(), "a comment reads nothing first")
	assert.Equal(t, []string{"Checking your account", "Commenting"}, h.steps(t))
	assert.Equal(t, []Acted{{}}, h.inbox.acted)
	assert.True(t, h.opened(t).Stale, "the sheet reads the issue again, with the comment")
}

func TestACommentNeedsTextButNoRead(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: issueID, Action: ActionComment, Body: " \n\uFEFF"})
	require.ErrorIs(t, err, ErrNotOffered)
	assert.Equal(t, "Write something first.", uierr.Message(err))
	assert.Zero(t, h.inbox.acts, "nothing is sent")

	res := h.act(t, ActRequest{Action: ActionComment, Body: "On it."})
	assert.Equal(t, OutcomeCommented, res.Outcome, "a comment goes from the card alone")
	assert.Zero(t, h.github.called(callView))
}

func TestACloseWaitsForARead(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: issueID, Action: ActionCloseCompleted})
	require.ErrorIs(t, err, ErrNotOffered)
	assert.Equal(t, waitForRead, uierr.Message(err))
	assert.Zero(t, h.inbox.acts)
}

func TestACloseWithTextPostsItFirst(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	res := h.act(t, ActRequest{Action: ActionCloseNotPlanned, Body: "Works as intended."})

	assert.Equal(t, Result{
		Outcome: OutcomeClosed, Message: "Closed " + issueRef + " as not planned with your comment",
		Steps: []string{"Commented", "Closed as not planned"}, Commented: true, LeftInbox: true,
	}, res)
	assert.Equal(t, []string{callState, callComment, callClose}, h.github.requests())
	assert.Equal(t, []string{"Works as intended."}, h.github.comments)
	assert.Equal(t, []string{github.IssueReasonNotPlanned}, h.github.reasons)
	assert.Equal(t, []string{
		"Checking your account", "Checking for new comments since you opened it", "Posting your comment",
		"Closing as not planned",
	}, h.steps(t))
	assert.Equal(t, []Acted{{Closed: true, Own: true}}, h.inbox.acted, "the inbox takes it out of the lists")
	assert.Nil(t, h.cached(t), "the sheet forgets a closed issue")
}

func TestACloseWithoutTextPostsNothing(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	res := h.act(t, ActRequest{Action: ActionCloseCompleted, Body: "  "})

	assert.Equal(t, Result{
		Outcome: OutcomeClosed, Message: "Closed " + issueRef + " as completed", Steps: []string{"Closed as completed"},
		LeftInbox: true,
	}, res)
	assert.Equal(t, []string{callState, callClose}, h.github.requests())
	assert.Equal(t, []string{github.IssueReasonCompleted}, h.github.reasons)
}

func TestAFailedCommentClosesNothing(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	h.github.commentErr = uierr.New("GitHub refused the comment.", errFakeRead)

	_, err := h.svc.Act(t.Context(), ActRequest{
		ItemID: issueID, Action: ActionCloseCompleted, Body: closingText, Seen: sawLatest(),
	})
	require.ErrorIs(t, err, errFakeRead)
	assert.Zero(t, h.github.called(callClose))
	assert.Equal(t, []Acted{{}}, h.inbox.acted)
	assert.True(t, h.opened(t).Stale, "the sheet reads it again")
}

func TestTheCloseGuardStopsAtSomeoneElsesComment(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	h.github.states = []*github.IssueState{openState(seenComment(), comment("IC_3", octoThird, testNow, false))}

	res := h.act(t, ActRequest{Action: ActionCloseCompleted, Body: closingText})

	assert.Equal(t, Result{
		Outcome: OutcomeNewActivity, Reason: "octo-third commented since you opened it",
		Message: "octo-third commented since you opened it, so " + issueRef + " wasn't closed. Read what's new, then " +
			"close it again.",
		Steps: []string{},
	}, res)
	assert.Equal(t, []string{callState}, h.github.requests(), "nothing is sent")
	assert.Equal(t, []Acted{{}}, h.inbox.acted)
	assert.True(t, h.opened(t).Stale, "the sheet reads the new comment")
}

func TestTheCloseGuardIgnoresTheUsersOwnComments(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	h.github.states = []*github.IssueState{openState(
		seenComment(), comment("IC_3", octoUser, testNow, true), comment("IC_4", "OCTO-USER", testNow, false),
	)}

	res := h.act(t, ActRequest{Action: ActionCloseCompleted})
	assert.Equal(t, OutcomeClosed, res.Outcome, "the user's own comments, by GitHub's word or their login")
}

func TestTheCloseGuardCountsCommentsItCantSee(t *testing.T) {
	t.Parallel()

	h := readHarness(t)

	recent := make([]github.CommentMark, 0, 10)
	for i := range 10 {
		recent = append(recent, comment("IC_mine", octoUser, testNow.Add(time.Duration(i)*time.Second), true))
	}

	h.github.states = []*github.IssueState{{
		Found: true, State: github.StateOpen, CanClose: true, Comments: 14, Recent: recent,
	}}

	res := h.act(t, ActRequest{Action: ActionCloseCompleted})
	assert.Equal(t, OutcomeNewActivity, res.Outcome, "ten of the user's since, and more before them")
	assert.Equal(t, "New comments came in since you opened it", res.Reason)

	h.github.states = []*github.IssueState{{
		Found: true, State: github.StateOpen, CanClose: true, Comments: 1,
		Recent: []github.CommentMark{comment("IC_9", "", testNow, false)},
	}}
	res = h.act(t, ActRequest{Action: ActionCloseCompleted, Seen: Seen{Comments: 0}})
	assert.Equal(t, "Someone commented since you opened it", res.Reason, "a deleted account's comment")
}

func TestTheCloseGuardGoesByTheCommentTheUserSaw(t *testing.T) {
	t.Parallel()

	h := readHarness(t)

	// The seen comment's twin, written the same second, is new; one before it is not.
	h.github.states = []*github.IssueState{openState(
		comment("IC_0", octoThird, latestAt.Add(-time.Second), false), seenComment(),
	)}
	assert.Equal(t, OutcomeClosed, h.act(t, ActRequest{Action: ActionCloseCompleted}).Outcome)

	h = readHarness(t)
	h.github.states = []*github.IssueState{openState(seenComment(), comment("IC_5", octoThird, latestAt, false))}
	assert.Equal(t, OutcomeNewActivity, h.act(t, ActRequest{Action: ActionCloseCompleted}).Outcome)

	// A seen comment deleted since: the guard stops at the first one older than it.
	h = readHarness(t)
	h.github.states = []*github.IssueState{openState(comment("IC_0", octoThird, latestAt.Add(-time.Second), false))}
	assert.Equal(t, OutcomeClosed, h.act(t, ActRequest{Action: ActionCloseCompleted}).Outcome)
}

func TestTheEarlierPinWins(t *testing.T) {
	t.Parallel()

	// The window saw only the user's own comment: octo-other's, which the read holds, is
	// new to them.
	h := readHarness(t)
	res := h.act(t, ActRequest{
		Action: ActionCloseCompleted, Seen: Seen{Comments: 1, Latest: "IC_1", LatestAt: stamp(mineAt)},
	})
	assert.Equal(t, OutcomeNewActivity, res.Outcome)
	assert.Equal(t, "octo-other commented since you opened it", res.Reason)

	// The window says it saw a comment newer than the read holds: the read's pin stands.
	h = readHarness(t)
	h.github.states = []*github.IssueState{openState(seenComment(), comment("IC_3", octoThird, testNow, false))}
	res = h.act(t, ActRequest{
		Action: ActionCloseCompleted, Seen: Seen{Comments: 3, Latest: "IC_3", LatestAt: stamp(testNow)},
	})
	assert.Equal(t, OutcomeNewActivity, res.Outcome)

	// A request that says it saw no comment at all pins before every one.
	h = readHarness(t)
	res = h.act(t, ActRequest{Action: ActionCloseCompleted, Seen: Seen{Comments: 0, LatestAt: "never"}})
	assert.Equal(t, OutcomeNewActivity, res.Outcome)

	// A read that holds no comment pins before every one too.
	h = newHarness(t)
	noComments := issueView()
	noComments.LatestComment = nil
	h.github.views = []*github.IssueView{noComments}
	h.refreshed(t)
	res = h.act(t, ActRequest{Action: ActionCloseCompleted})
	assert.Equal(t, OutcomeNewActivity, res.Outcome)
}

func TestACloseOfAnIssueSomeoneClosedFirst(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	h.github.states = []*github.IssueState{{Found: true, State: github.StateClosed, StateReason: github.StateReasonCompleted}}

	res := h.act(t, ActRequest{Action: ActionCloseNotPlanned, Body: "Not planned."})
	assert.Equal(t, Result{
		Outcome: OutcomeAlreadyClosed, Message: issueRef + " was already closed as completed", Steps: []string{},
		LeftInbox: true,
	}, res)
	assert.Equal(t, []string{callState}, h.github.requests(), "nothing is sent")
	assert.Equal(t, []Acted{{Closed: true}}, h.inbox.acted, "the card leaves the inbox")
	assert.Nil(t, h.cached(t))

	// GitHub keeps the reason of an issue closed between the read and the close.
	h = readHarness(t)
	h.github.closedAs = "duplicate"
	res = h.act(t, ActRequest{Action: ActionCloseNotPlanned, Body: "Not planned."})
	assert.Equal(t, Result{
		Outcome: OutcomeAlreadyClosed, Message: "Commented on " + issueRef + ", but someone closed it first as a duplicate",
		Steps: []string{"Commented"}, Commented: true, LeftInbox: true,
	}, res)

	h = readHarness(t)
	h.github.states = []*github.IssueState{{Found: true, State: github.StateClosed}}
	res = h.act(t, ActRequest{Action: ActionCloseCompleted})
	assert.Equal(t, issueRef+" was already closed", res.Message, "GitHub didn't say as what")

	// Someone closed it as the same reason first: GitHub names them.
	h = readHarness(t)
	h.github.closedBy = octoOther
	res = h.act(t, ActRequest{Action: ActionCloseCompleted})
	assert.Equal(t, OutcomeAlreadyClosed, res.Outcome)
	assert.Equal(t, issueRef+" was already closed as completed", res.Message)
	assert.Equal(t, []Acted{{Closed: true}}, h.inbox.acted, "someone else's close is not the user's own")

	h = readHarness(t)
	h.github.closedBy = "Octo-User"
	assert.Equal(t, OutcomeClosed, h.act(t, ActRequest{Action: ActionCloseCompleted}).Outcome, "the user's own close")
	assert.Equal(t, []Acted{{Closed: true, Own: true}}, h.inbox.acted)
}

func TestACloseGitHubWontLetThroughSendsNothing(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	h.github.states = []*github.IssueState{{Found: true, State: github.StateOpen, CanClose: false}}

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: issueID, Action: ActionCloseCompleted, Body: closingText})
	require.ErrorIs(t, err, ErrNotOffered)
	assert.Equal(t, "GitHub doesn't let your account close this issue.", uierr.Message(err))

	h.github.states = []*github.IssueState{{}}
	_, err = h.svc.Act(t.Context(), ActRequest{ItemID: issueID, Action: ActionCloseCompleted, Body: closingText})
	require.ErrorIs(t, err, github.ErrIssueGone)

	h.github.stateErr = errFakeRead
	_, err = h.svc.Act(t.Context(), ActRequest{ItemID: issueID, Action: ActionCloseCompleted, Body: closingText})
	require.ErrorIs(t, err, errFakeRead)

	assert.Zero(t, h.github.called(callComment))
	assert.Zero(t, h.github.called(callClose))
}

func TestAFailureAfterTheCommentIsAResult(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	h.github.closeErr = &github.StatusError{StatusCode: http.StatusUnprocessableEntity, Message: "Validation Failed"}
	h.github.closeErr = uierr.New("GitHub refused to close it.", h.github.closeErr)

	res := h.act(t, ActRequest{Action: ActionCloseCompleted, Body: closingText})
	assert.Equal(t, Result{
		Outcome: OutcomeCommentedNotClosed, Reason: "GitHub refused to close it",
		Message: "Commented on " + issueRef + ", but it wasn't closed: GitHub refused to close it",
		Steps:   []string{"Commented"}, Commented: true,
	}, res)
	assert.True(t, h.opened(t).Stale, "the cache is out of date, not forgotten")
}

func TestATokenRetryNeverPostsTwice(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	h.github.closeOnce = badCredentials()

	res := h.act(t, ActRequest{Action: ActionCloseCompleted, Body: closingText})
	assert.Equal(t, OutcomeClosed, res.Outcome)
	assert.Equal(t, []string{closingText}, h.github.comments, "the comment went once")
	assert.Equal(t, []string{callState, callComment, callClose, callState, callClose}, h.github.requests(),
		"the retry reads the state again, and closes")
	assert.Equal(t, 2, h.inbox.tries)
	assert.Equal(t, []string{"Commented", "Closed as completed"}, res.Steps)
}

func TestARetryAfterTheCommentStopsAtSomeoneElsesComment(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	h.github.closeOnce = badCredentials()
	h.github.states = []*github.IssueState{
		openState(seenComment()),
		openState(seenComment(), comment("IC_3", octoUser, testNow, true), comment("IC_4", octoThird, testNow, false)),
	}

	res := h.act(t, ActRequest{Action: ActionCloseCompleted, Body: closingText})
	assert.Equal(t, OutcomeCommentedNotClosed, res.Outcome)
	assert.Equal(t, "octo-third commented since you opened it", res.Reason)
	assert.True(t, res.Commented)
	assert.Equal(t, 1, h.github.called(callClose), "only the refused try")
}

func TestARefusedRetryAfterTheCommentIsAResult(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	h.github.closeOnce = badCredentials()
	h.inbox.retryErr = uierr.New("Sign in again.", errFakeRead)

	res := h.act(t, ActRequest{Action: ActionCloseCompleted, Body: closingText})
	assert.Equal(t, OutcomeCommentedNotClosed, res.Outcome)
	assert.Equal(t, "Sign in again", res.Reason)

	h = readHarness(t)
	h.github.commentOnce = badCredentials()
	h.inbox.retryErr = uierr.New("Sign in again.", errFakeRead)

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: issueID, Action: ActionComment, Body: "Hi."})
	require.ErrorIs(t, err, errFakeRead, "nothing was posted, so the error stands")
}

func TestTheInboxsBookkeepingFailingKeepsWhatGitHubDid(t *testing.T) {
	t.Parallel()

	for _, body := range []string{closingText, ""} {
		h := readHarness(t)
		h.inbox.recordErr = errFakeRead

		res := h.act(t, ActRequest{Action: ActionCloseCompleted, Body: body})
		assert.Equal(t, OutcomeClosed, res.Outcome, "with the text %q", body)
		assert.True(t, res.LeftInbox)
		assert.Contains(t, h.logs.String(), "an action's step failed after GitHub answered it")
	}

	h := readHarness(t)
	h.inbox.recordErr = errFakeRead
	h.github.states = []*github.IssueState{{Found: true, State: github.StateClosed, StateReason: github.StateReasonNotPlanned}}

	res := h.act(t, ActRequest{Action: ActionCloseCompleted})
	assert.Equal(t, OutcomeAlreadyClosed, res.Outcome, "a close found made")
	assert.True(t, res.LeftInbox)
}

func TestALockedConversation(t *testing.T) {
	t.Parallel()

	locked := issueView()
	locked.Locked, locked.LockReason = true, "TOO_HEATED"

	h := newHarness(t)
	h.inbox.issue.ViewerPermission = "TRIAGE"
	h.github.views = []*github.IssueView{locked}
	is := h.refreshed(t)

	assert.Equal(t, "Locked as too heated · only people with write access can comment", is.Standing.Sentence)
	assert.False(t, offerOf(t, is, ActionComment).Enabled)
	assert.Equal(t, ActionNone, is.Primary)

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: issueID, Action: ActionComment, Body: "Hi."})
	require.ErrorIs(t, err, ErrNotOffered)
	assert.Equal(t, lockedComment, uierr.Message(err))

	_, err = h.svc.Act(t.Context(), ActRequest{ItemID: issueID, Action: ActionCloseCompleted, Body: closingText})
	require.ErrorIs(t, err, ErrNotOffered)
	assert.Equal(t, lockedClose, uierr.Message(err))
	assert.Zero(t, h.inbox.acts)

	res := h.act(t, ActRequest{Action: ActionCloseCompleted})
	assert.Equal(t, OutcomeClosed, res.Outcome, "a close with no text goes")

	h = newHarness(t)
	h.github.views = []*github.IssueView{locked}
	h.refreshed(t)
	assert.Equal(t, OutcomeCommented, h.act(t, ActRequest{Action: ActionComment, Body: "Hi."}).Outcome,
		"write access comments on a locked conversation")
}

func TestOnlyWhatTheSheetOffersRuns(t *testing.T) {
	t.Parallel()

	h := readHarness(t)

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: issueID, Action: "reopen"})
	require.ErrorIs(t, err, ErrNotOffered)
	assert.Equal(t, "That isn't something the sheet can do with this issue.", uierr.Message(err))

	cantClose := issueView()
	cantClose.ViewerCanClose = false
	h = newHarness(t)
	h.github.views = []*github.IssueView{cantClose}
	h.refreshed(t)

	_, err = h.svc.Act(t.Context(), ActRequest{ItemID: issueID, Action: ActionCloseNotPlanned})
	require.ErrorIs(t, err, ErrNotOffered, "GitHub doesn't let the user close it")

	h.inbox.issued = errFakeRead
	_, err = h.svc.Act(t.Context(), ActRequest{ItemID: issueID, Action: ActionComment, Body: "Hi."})
	require.ErrorIs(t, err, errFakeRead)
	assert.Zero(t, h.inbox.acts)
}

func TestSettlingTheCacheMayFail(t *testing.T) {
	t.Parallel()

	h := readHarness(t)

	_, err := h.db.ExecContext(t.Context(), `CREATE TRIGGER no_expire BEFORE UPDATE OF fetched_at ON issue_views
		BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)

	res := h.act(t, ActRequest{Action: ActionComment, Body: "Hi."})
	assert.Equal(t, OutcomeCommented, res.Outcome, "the comment stands")
	assert.Contains(t, h.logs.String(), "could not update the cached issue after an action")
}
