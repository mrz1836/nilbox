package review

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// pushedSinceHarness is a harness whose pull request the sheet has read after its author
// pushed two commits on top of the one the user requested changes on, which GitHub no
// longer asks the user to review: the inbox lists it as pushed to since.
func pushedSinceHarness(t *testing.T) *harness {
	t.Helper()

	h := newHarness(t)
	h.inbox.pull.ReviewRequested, h.inbox.pull.ReviewDecision = false, "CHANGES_REQUESTED"
	h.github.review = pullReview(headA, github.MergeStateBlocked)
	h.github.review.CommitTotal = 3
	h.github.review.Commits = []github.PullCommit{
		{OID: headB, Headline: "Add the cache", Author: octoOther},
		{OID: sha("c"), Headline: "Address the review", Author: octoOther},
		{OID: headA, Headline: "Fix the tests", Author: octoOther},
	}
	h.github.review.Verdict = &github.Verdict{State: github.ReviewStateChangesRequested, Commit: headB}
	h.github.asks = []string{"octo-reviewer"}

	return h
}

func TestReRequestYourReview(t *testing.T) {
	t.Parallel()

	h := pushedSinceHarness(t)
	r := h.refreshed(t)

	o := offerOf(t, r, ActionRerequestReview)
	assert.True(t, o.Enabled, o.Reason)
	assert.Equal(t, "Re-request my review", o.Label)
	assert.NotEqual(t, ActionRerequestReview, r.Primary, "never the button")

	res := h.act(t, ActRequest{Action: ActionRerequestReview, Body: "Not sent."})
	assert.Equal(t, OutcomeReviewRerequested, res.Outcome)
	assert.Equal(t, "Re-requested your review of acme-org/example-repo#7", res.Message)
	assert.Equal(t, []string{"Asked for your review again"}, res.Steps)
	assert.False(t, res.LeftReview || res.LeftInbox || res.Commented)

	assert.Equal(t, []string{octoUser}, h.github.asked, "one request, for the account the inbox checked")
	assert.Equal(t, []string{callRequest}, h.github.requests(), "nothing else: no review, no comment, nothing dismissed")
	assert.Equal(t, []string{"Asking for your review again"}, h.steps(t)[1:], "after the account check")

	require.Len(t, h.inbox.acted, 1)
	assert.Equal(t, Acted{
		Rerequested: true, RerequestedAt: testNow,
		ReviewRequests: github.ReviewRequests{Users: []string{"octo-reviewer", octoUser}, Teams: []string{}},
	}, h.inbox.acted[0], "the inbox hears who GitHub asks now, to record at once")
}

func TestAReRequestNeverGoesTwice(t *testing.T) {
	t.Parallel()

	h := pushedSinceHarness(t)
	h.refreshed(t)
	h.github.requestErr = badCredentials()

	res := h.act(t, ActRequest{Action: ActionRerequestReview})
	assert.Equal(t, OutcomeReviewRerequested, res.Outcome)
	assert.Equal(t, 2, h.inbox.tries, "tried again with a fresh token")
	assert.Equal(t, []string{octoUser}, h.github.asked, "the request GitHub refused never went: it goes once")

	// A try after GitHub took it, as the Actor's retry with a fresh token is, sends nothing.
	a := &action{kind: ActionRerequestReview, login: octoUser}
	a.acted.Rerequested = true

	require.NoError(t, a.follow(t.Context(), h.svc, github.NewToken("fake-token-3")))
	assert.Len(t, h.github.asked, 1)
}

func TestAReRequestOfAPullRequestPushedSinceSendsNothing(t *testing.T) {
	t.Parallel()

	h := pushedSinceHarness(t)
	h.refreshed(t)

	res := h.act(t, ActRequest{Action: ActionRerequestReview, Head: headB})
	assert.Equal(t, OutcomePushedSince, res.Outcome)
	assert.Zero(t, h.inbox.acts)
	assert.Zero(t, h.github.called(callRequest))
}

func TestAReRequestIsRefusedWhereItIsNotOffered(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		setUp  func(*harness)
		req    ActRequest
		reason string
	}{
		{
			name:   "GitHub asks the user already",
			setUp:  func(h *harness) { h.inbox.pull.ReviewUsers = []string{"Octo-User"} },
			reason: "GitHub already asks you to review it again.",
		},
		{
			name:   "without write access",
			setUp:  func(h *harness) { h.inbox.pull.ViewerPermission = "READ" },
			reason: "Only people with write access to the repository can be asked to review it.",
		},
		{
			name:   "an approval since",
			setUp:  func(h *harness) { h.github.review.Verdict.State = github.ReviewStateApproved },
			reason: "That isn't something the sheet can do with this pull request.",
		},
		{
			name:   "line comments, which go with a review",
			setUp:  func(*harness) {},
			req:    ActRequest{Comments: []LineComment{{Path: cacheFile, Line: 3, Side: SideRight, Body: "x"}}},
			reason: "Line comments go with a review.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := pushedSinceHarness(t)
			tt.setUp(h)
			h.refreshed(t)

			req := tt.req
			req.ItemID, req.Action, req.Head = pullID, ActionRerequestReview, headA

			_, err := h.svc.Act(t.Context(), req)
			require.ErrorIs(t, err, ErrNotOffered)
			assert.Equal(t, tt.reason, uierr.Message(err))
			assert.Zero(t, h.github.called(callRequest), "nothing was sent")
		})
	}
}

func TestAReRequestGitHubRefusesIsAnError(t *testing.T) {
	t.Parallel()

	h := pushedSinceHarness(t)
	h.refreshed(t)
	h.github.requestErr = uierr.New("GitHub didn't ask you to review acme-org/example-repo#7 again.", github.ErrEditIgnored)

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionRerequestReview, Head: headA})
	require.ErrorIs(t, err, github.ErrEditIgnored, "nothing happened, so it is an error")
	require.Len(t, h.inbox.acted, 1)
	assert.False(t, h.inbox.acted[0].Rerequested, "the inbox records nothing")
}

// offerOf returns r's offer of action, which must be there.
func offerOf(t *testing.T, r Review, action Action) Offer {
	t.Helper()

	for _, o := range r.Offers {
		if o.Action == action {
			return o
		}
	}

	require.Failf(t, "not offered", "%s", action)

	return Offer{}
}

func TestNoReRequestAfterOnlyYourOwnPush(t *testing.T) {
	t.Parallel()

	// The user merged the base in to fix the checks: the author still owes the changes,
	// so the sheet says nothing came since, and offers no re-request.
	h := pushedSinceHarness(t)
	h.github.review.Commits = []github.PullCommit{
		{OID: headB, Headline: "Add the cache", Author: octoOther},
		{OID: headA, Headline: "Merge main into widget-cache", Author: octoUser},
	}
	h.github.review.CommitTotal = 2

	r := h.refreshed(t)
	require.NotNil(t, r.YourReview)
	assert.Equal(t, SinceNone, r.YourReview.Since)

	for _, o := range r.Offers {
		assert.NotEqual(t, ActionRerequestReview, o.Action, "no re-request after the user's own push")
	}
}
