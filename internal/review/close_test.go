package review

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// closeText is the composer's text for a close, which it posts as a comment first.
const closeText = "Closing this: the cache moved to the store package."

// closeOffer is the sheet's offer of a close for #7, as Open shows it.
func closeOffer(t *testing.T, h *harness) (Offer, bool) {
	t.Helper()

	r, err := h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)

	i := slices.IndexFunc(r.Offers, func(o Offer) bool { return o.Action == ActionClose })
	if i < 0 {
		return Offer{}, false
	}

	assert.NotEqual(t, ActionClose, r.Primary, "a close is never the button")

	return r.Offers[i], true
}

func TestACloseWithoutTextOnlyCloses(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateBlocked)

	o, ok := closeOffer(t, h)
	require.True(t, ok, "someone else's pull request, for a user with write access")
	assert.Equal(t, "Close pull request", o.Label)
	assert.True(t, o.Enabled)
	assert.True(t, o.Confirm, "it asks first")
	assert.True(t, o.CommentsFirst)
	assert.False(t, o.Reviews)

	for name, body := range map[string]string{"no text": "", "only spaces": " \n\t ", "only a byte order mark": "\uFEFF"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := readHarness(t, github.MergeStateBlocked)

			res := h.act(t, ActRequest{Action: ActionClose, Body: body})
			assert.Equal(t, OutcomeClosed, res.Outcome)
			assert.Equal(t, "Closed acme-org/example-repo#7", res.Message)
			assert.Equal(t, []string{"Closed"}, res.Steps)
			assert.False(t, res.Commented)
			assert.True(t, res.LeftInbox, "a closed pull request leaves every list")
			assert.False(t, res.LeftReview, "no review was posted")
			assert.Empty(t, res.Method)
			assert.Equal(t, []string{callState, callClose}, h.github.requests(), "the head first, then the close")
			assert.Empty(t, h.github.sent, "no comment")
			assert.Equal(t, []string{"Checking your account", "Checking nothing was pushed since you opened it", "Closing"},
				h.steps(t))
			assert.Equal(t, Acted{Closed: true}, h.inbox.acted[0])
			assert.Empty(t, h.merges.heard(), "a close is no merge: the Release page hears nothing")

			c, err := h.svc.cache.load(t.Context(), pullID, 0)
			require.NoError(t, err)
			assert.Nil(t, c, "a closed pull request's cache is dropped")
		})
	}
}

func TestAClosePostsYourTextAsACommentFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		harness func(t *testing.T) *harness
		left    bool
	}{
		{name: "someone else's", harness: func(t *testing.T) *harness {
			t.Helper()

			return readHarness(t, github.MergeStateBlocked)
		}, left: true},
		{name: "your own, where GitHub refuses an approval but takes a comment", harness: ownBlocked},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := tt.harness(t)

			res := h.act(t, ActRequest{
				Action: ActionClose, Body: closeText,
				Comments: []LineComment{{Path: cacheFile, Line: 3, Side: SideRight, Body: "This moved."}},
			})

			assert.Equal(t, []string{callState, callSubmit, callClose}, h.github.requests(),
				"the head, then one comment, then the close")
			require.Len(t, h.github.sent, 1)
			assert.Equal(t, github.ReviewInput{
				Event: github.ReviewComment, Body: closeText, CommitID: headA,
				Comments: []github.LineComment{{Path: cacheFile, Line: 3, Side: github.SideRight, Body: "This moved."}},
			}, h.github.sent[0], "a comment, never an approval, on the head the user saw")
			assert.Equal(t, OutcomeClosed, res.Outcome)
			assert.Equal(t, "Closed acme-org/example-repo#7 with your comment", res.Message)
			assert.Equal(t, []string{"Commented", "Closed"}, res.Steps)
			assert.True(t, res.Commented)
			assert.True(t, res.LeftInbox)
			assert.Equal(t, tt.left, res.LeftReview, "a comment fulfills a request for a review")
			assert.Equal(t, []string{
				"Checking your account", "Checking nothing was pushed since you opened it", "Posting your comment", "Closing",
			}, h.steps(t))
			assert.Equal(t, Acted{Reviewed: true, ReviewedAt: testNow, ReviewEvent: github.ReviewComment, Closed: true}, h.inbox.acted[0])
		})
	}
}

func TestACloseWhoseCommentFailsClosesNothing(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateBlocked)
	h.github.submitErr = errGateShut

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionClose, Head: headA, Body: closeText})
	require.ErrorIs(t, err, errGateShut, "the comment's own failure")
	assert.Equal(t, []string{callState, callSubmit}, h.github.requests(), "nothing after the comment")
	assert.Equal(t, Acted{}, h.inbox.acted[0])

	o, ok := closeOffer(t, h)
	require.True(t, ok, "the sheet still offers the close")
	assert.True(t, o.Enabled)
}

func TestACloseOfAPullRequestPushedSinceSendsNothing(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateBlocked)
	h.github.states = []*github.MergeState{open(github.MergeStateBlocked, headB)}

	res := h.act(t, ActRequest{Action: ActionClose, Body: closeText})
	assert.Equal(t, OutcomePushedSince, res.Outcome)
	assert.Equal(t, "acme-org/example-repo#7 was pushed to since you opened it. Reload it to see the new commits.",
		res.Message)
	assert.False(t, res.Commented, "the draft keeps its text for the commits the user hasn't seen")
	assert.False(t, res.LeftInbox)
	assert.Empty(t, res.Steps)
	assert.Equal(t, []string{callState}, h.github.requests(), "the head alone: no comment, no close")
	assert.Equal(t, Acted{}, h.inbox.acted[0])

	// A push the sheet already knows of sends nothing at all.
	h = readHarness(t, github.MergeStateBlocked)

	res = h.act(t, ActRequest{Action: ActionClose, Head: headB, Body: closeText})
	assert.Equal(t, OutcomePushedSince, res.Outcome)
	assert.Zero(t, h.inbox.acts)
	assert.Empty(t, h.github.requests())
}

func TestACloseOfAPullRequestNoLongerOpenSendsNothing(t *testing.T) {
	t.Parallel()

	for name, state := range map[string]*github.MergeState{
		"closed": {Found: true, State: github.StateClosed, Head: headA},
		"merged": {Found: true, State: github.StateMerged, Head: headA},
		"gone":   {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := readHarness(t, github.MergeStateBlocked)
			h.github.states = []*github.MergeState{state}

			_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionClose, Head: headA, Body: closeText})
			require.ErrorIs(t, err, ErrNotOffered, "nothing happened, so it is an error")
			assert.Equal(t, "It's no longer open.", uierr.Message(err))
			assert.Equal(t, []string{callState}, h.github.requests())
			assert.Empty(t, h.github.sent)
			assert.Equal(t, Acted{}, h.inbox.acted[0])
		})
	}
}

func TestACommentPostedBeforeACloseThatFailedSaysBoth(t *testing.T) {
	t.Parallel()

	refused := uierr.New("GitHub wouldn't close acme-org/example-repo#7: Resource not accessible.", errGateShut)
	signedOut := uierr.New("You're signed in to another account now.", errFakeRead)

	tests := []struct {
		name     string
		setup    func(h *harness)
		requests []string
		reason   string
	}{
		{
			name:     "refused by GitHub",
			setup:    func(h *harness) { h.github.closeErr = refused },
			requests: []string{callState, callSubmit, callClose},
			reason:   "GitHub wouldn't close acme-org/example-repo#7: Resource not accessible",
		},
		{
			name:     "the fresh token refused too",
			setup:    func(h *harness) { h.github.closeErr = badCredentials() },
			requests: []string{callState, callSubmit, callClose, callState, callClose},
			reason:   trimmed(badCredentials()),
		},
		{
			name: "pushed to before the fresh token's try",
			setup: func(h *harness) {
				h.github.closeOnce = badCredentials()
				h.github.states = []*github.MergeState{open(github.MergeStateBlocked, headA), open(github.MergeStateBlocked, headB)}
			},
			requests: []string{callState, callSubmit, callClose, callState},
			reason:   "It was pushed to since you opened it",
		},
		{
			name: "the fresh token's account check failing",
			setup: func(h *harness) {
				h.github.closeOnce, h.inbox.retryErr = badCredentials(), signedOut
			},
			requests: []string{callState, callSubmit, callClose},
			reason:   "You're signed in to another account now",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := readHarness(t, github.MergeStateBlocked)
			tt.setup(h)

			res, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionClose, Head: headA, Body: closeText})
			require.NoError(t, err, "the comment is on GitHub, so the sheet hears a result")
			assert.Equal(t, OutcomeCommentedNotClosed, res.Outcome)
			assert.Equal(t, tt.reason, res.Reason)
			assert.Equal(t, "Commented on acme-org/example-repo#7, but it wasn't closed: "+tt.reason, res.Message)
			assert.True(t, res.Commented, "the sheet clears the draft")
			assert.False(t, res.LeftInbox, "it is still open")
			assert.Equal(t, []string{"Commented"}, res.Steps)
			assert.Equal(t, tt.requests, h.github.requests())
			assert.Len(t, h.github.sent, 1, "commented once")
			assert.Equal(t, Acted{Reviewed: true, ReviewedAt: testNow, ReviewEvent: github.ReviewComment}, h.inbox.acted[0])
		})
	}
}

func TestACloseWithoutTextThatFailsIsAnError(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateBlocked)
	h.github.closeErr = errGateShut

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionClose, Head: headA})
	require.ErrorIs(t, err, errGateShut, "nothing was posted, so it is an error")
	assert.Equal(t, Acted{}, h.inbox.acted[0])

	_, ok := closeOffer(t, h)
	assert.True(t, ok, "the cache is kept, marked out of date, and the sheet offers the close again")
}

func TestARetriedTokenNeverCommentsTwiceBeforeAClose(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateBlocked)
	h.github.closeOnce = badCredentials()

	res := h.act(t, ActRequest{Action: ActionClose, Body: closeText})
	assert.Equal(t, OutcomeClosed, res.Outcome)
	assert.Equal(t, 2, h.inbox.tries, "tried again with a fresh token")
	assert.Equal(t, []string{callState, callSubmit, callClose, callState, callClose}, h.github.requests(),
		"commented once, and read the head again right before the second close")
	assert.Equal(t, "Closed acme-org/example-repo#7 with your comment", res.Message)
	assert.Equal(t, Acted{Reviewed: true, ReviewedAt: testNow, ReviewEvent: github.ReviewComment, Closed: true}, h.inbox.acted[0])
}

func TestARetriedCloseWithoutTextReadsTheHeadAgain(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateBlocked)
	h.github.closeOnce = badCredentials()
	h.github.states = []*github.MergeState{open(github.MergeStateBlocked, headA), open(github.MergeStateBlocked, headB)}

	res := h.act(t, ActRequest{Action: ActionClose})
	assert.Equal(t, OutcomePushedSince, res.Outcome, "nothing was posted, and the second try finds the push")
	assert.False(t, res.Commented)
	assert.Equal(t, []string{callState, callClose, callState}, h.github.requests())
	assert.Equal(t, Acted{}, h.inbox.acted[0])
}

func TestACloseTheInboxFailedToRecordIsStillAClose(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateBlocked)
	h.inbox.recordErr = errFakeRead

	res := h.act(t, ActRequest{Action: ActionClose, Body: closeText})
	assert.Equal(t, OutcomeClosed, res.Outcome, "GitHub closed it; the next refresh catches the inbox up")
	assert.True(t, res.Commented)
	assert.True(t, res.LeftInbox)
	assert.Contains(t, h.logs.String(), "an action's step failed after GitHub answered it")
}

func TestAReviewStartedOnGitHubHoldsBackOnlyTheClosesComment(t *testing.T) {
	t.Parallel()

	h := pendingHarness(t)

	o, ok := closeOffer(t, h)
	require.True(t, ok)
	assert.True(t, o.Enabled, "the close itself is offered")
	assert.Equal(t, "You have a review of this pull request started on GitHub. Submit or discard it there first. "+
		"Or clear your text and line comments to close it without a comment.", o.CommentBlocked)

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionClose, Head: headA, Body: closeText})
	require.ErrorIs(t, err, ErrNotOffered)
	assert.Equal(t, o.CommentBlocked, uierr.Message(err), "the words the window holds the button back with")
	assert.Zero(t, h.inbox.acts)

	res := h.act(t, ActRequest{Action: ActionClose})
	assert.Equal(t, OutcomeClosed, res.Outcome, "without text it closes")
	assert.Equal(t, []string{callState, callClose}, h.github.requests())
}

func TestACloseSendsNothingItWouldNotOffer(t *testing.T) {
	t.Parallel()

	t.Run("line comments off the diff", func(t *testing.T) {
		t.Parallel()

		h := readHarness(t, github.MergeStateBlocked)

		_, err := h.svc.Act(t.Context(), ActRequest{
			ItemID: pullID, Action: ActionClose, Head: headA, Body: closeText,
			Comments: []LineComment{{Path: cacheFile, Line: 99, Side: SideRight, Body: "x"}},
		})
		require.ErrorIs(t, err, ErrNotInDiff)
		assert.Zero(t, h.inbox.acts)
		assert.Empty(t, h.github.requests())
	})

	t.Run("a user GitHub doesn't let close it", func(t *testing.T) {
		t.Parallel()

		// Write access and the author's own login count for nothing: GitHub's say does.
		h := newHarness(t)
		h.inbox.pull.AuthorLogin = octoUser
		h.github.review = pullReview(headA, github.MergeStateBlocked)
		h.github.review.ViewerCanClose = false
		h.refreshed(t)

		_, ok := closeOffer(t, h)
		assert.False(t, ok, "never offered")

		_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionClose, Head: headA})
		require.ErrorIs(t, err, ErrNotOffered)
		assert.Zero(t, h.inbox.acts)
		assert.Empty(t, h.github.requests())
	})

	t.Run("a user GitHub lets close it, whatever their access", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		h.inbox.pull.ViewerPermission = "READ"
		h.github.review = pullReview(headA, github.MergeStateBlocked)
		h.refreshed(t)

		res := h.act(t, ActRequest{Action: ActionClose})
		assert.Equal(t, OutcomeClosed, res.Outcome)
	})

	t.Run("a pull request the sheet hasn't read", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)

		r, err := h.svc.Open(t.Context(), pullID)
		require.NoError(t, err)

		i := slices.IndexFunc(r.Offers, func(o Offer) bool { return o.Action == ActionClose })
		require.GreaterOrEqual(t, i, 0)
		assert.False(t, r.Offers[i].Enabled)
		assert.Equal(t, "Wait for it to load from GitHub.", r.Offers[i].Reason)

		_, err = h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionClose, Head: headA})
		require.ErrorIs(t, err, ErrNotRead)
		assert.Zero(t, h.inbox.acts)
	})
}
