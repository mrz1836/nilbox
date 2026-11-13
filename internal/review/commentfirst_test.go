package review

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// mergeText is the composer's text for a merge: the default reply, which a merge with no
// review of its own posts as a comment first.
const mergeText = "Reviewed and approved. Thanks for the work on this, @octo-user!"

// ownHarness is a harness whose pull request is the user's own, read in merge state
// status: GitHub refuses the user's approval of it, so a merge is how they finish it.
func ownHarness(t *testing.T, status string) *harness {
	t.Helper()

	h := newHarness(t)
	h.inbox.pull.AuthorLogin, h.inbox.pull.ReviewRequested = octoUser, false
	h.github.review = pullReview(headA, status)
	h.github.review.ViewerDidAuthor = true
	h.refreshed(t)

	return h
}

// ownClean is ownHarness in a merge state GitHub merges now.
func ownClean(t *testing.T) *harness {
	t.Helper()

	return ownHarness(t, github.MergeStateClean)
}

// ownBlocked is ownHarness in a merge state GitHub holds back for its requirements.
func ownBlocked(t *testing.T) *harness {
	t.Helper()

	return ownHarness(t, github.MergeStateBlocked)
}

// blockedHarness is someone else's pull request, which the rules hold back until it is
// approved.
func blockedHarness(t *testing.T) *harness {
	t.Helper()

	return readHarness(t, github.MergeStateBlocked)
}

// pendingHarness is ownClean with a review of the user's started on GitHub, which holds
// back every review, a comment included.
func pendingHarness(t *testing.T) *harness {
	t.Helper()

	h := newHarness(t)
	h.github.review = pullReview(headA, github.MergeStateClean)
	h.github.review.ViewerDidAuthor, h.github.review.PendingReviews = true, 1
	h.refreshed(t)

	return h
}

// trimmed is err's words as a result's reason gives them.
func trimmed(err error) string {
	return strings.TrimSuffix(uierr.Message(err), ".")
}

// commentFirstCase is a merge with no review of its own: the harness it runs in, and the
// merge state its first read finds.
type commentFirstCase struct {
	name    string
	action  Action
	harness func(t *testing.T) *harness
	states  []*github.MergeState
}

// commentFirstCases are the three merges with no review of their own, each where it merges.
func commentFirstCases() []commentFirstCase {
	return []commentFirstCase{
		{name: "merge now", action: ActionMerge, harness: ownClean, states: []*github.MergeState{open(github.MergeStateClean, headA)}},
		{name: "merge past the rules", action: ActionMergeBypass, harness: bypassHarness, states: []*github.MergeState{held(true)}},
		{name: "merge when ready", action: ActionAutoMerge, harness: ownBlocked},
	}
}

func TestAMergePostsYourTextAsACommentFirst(t *testing.T) {
	t.Parallel()

	want := map[Action]struct {
		message  string
		steps    []string
		requests []string
		progress []string
	}{
		ActionMerge: {
			message:  "Commented and merged acme-org/example-repo#7 (squash)",
			steps:    []string{"Commented", "Merged (squash)"},
			requests: []string{callState, callSubmit, callMerge},
			progress: []string{
				"Checking your account", "Checking that GitHub can merge it", "Posting your comment", "Squash-merging",
			},
		},
		ActionMergeBypass: {
			message:  "Commented and merged acme-org/example-repo#7 past the rules (squash)",
			steps:    []string{"Commented", "Merged past the rules (squash)"},
			requests: []string{callState, callSubmit, callMerge},
			progress: []string{
				"Checking your account", "Checking that GitHub can merge it", "Posting your comment",
				"Squash-merging past the rules",
			},
		},
		ActionAutoMerge: {
			message:  "Commented on acme-org/example-repo#7 · auto-merge is on (squash)",
			steps:    []string{"Commented", "Auto-merge on (squash)"},
			requests: []string{callState, callSubmit, callAuto},
			progress: []string{
				"Checking your account", "Checking nothing was pushed since you opened it", "Posting your comment",
				"Turning on auto-merge",
			},
		},
	}

	for _, tc := range commentFirstCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := tc.harness(t)
			h.github.states = tc.states

			res := h.act(t, ActRequest{
				Action: tc.action, Method: MethodSquash, Accept: true, Body: mergeText,
				Comments: []LineComment{{Path: cacheFile, Line: 3, Side: SideRight, Body: "Nice."}},
			})

			w := want[tc.action]
			assert.Equal(t, w.requests, h.github.requests(), "the merge state, then one comment, then the merge")
			require.Len(t, h.github.sent, 1)
			assert.Equal(t, github.ReviewInput{
				Event: github.ReviewComment, Body: mergeText, CommitID: headA,
				Comments: []github.LineComment{{Path: cacheFile, Line: 3, Side: github.SideRight, Body: "Nice."}},
			}, h.github.sent[0], "a comment, never an approval, on the head the user saw")
			assert.Equal(t, w.message, res.Message)
			assert.Equal(t, w.steps, res.Steps)
			assert.Equal(t, w.progress, h.steps(t))
			assert.True(t, res.Commented)
			assert.True(t, h.inbox.acted[0].Reviewed, "the comment is a review GitHub recorded")
		})
	}
}

func TestAMergeWithoutTextPostsNoComment(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{"no text": "", "only spaces": " \n\t ", "only a byte order mark": "\uFEFF"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := ownClean(t)
			h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}

			res := h.act(t, ActRequest{Action: ActionMerge, Accept: true, Body: body})
			assert.Equal(t, []string{callState, callMerge}, h.github.requests(), "no review request")
			assert.Equal(t, "Merged acme-org/example-repo#7 (squash)", res.Message)
			assert.Equal(t, []string{"Merged (squash)"}, res.Steps)
			assert.False(t, res.Commented)
			assert.Equal(t, []string{"Checking your account", "Checking that GitHub can merge it", "Squash-merging"}, h.steps(t))
		})
	}
}

func TestAMergeWhoseCommentFailsMergesNothing(t *testing.T) {
	t.Parallel()

	for _, tc := range commentFirstCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := tc.harness(t)
			h.github.states = tc.states
			h.github.submitErr = errGateShut

			_, err := h.svc.Act(t.Context(), ActRequest{
				ItemID: pullID, Action: tc.action, Head: headA, Accept: true, Body: mergeText,
			})
			require.ErrorIs(t, err, errGateShut, "the comment's own failure")
			assert.Equal(t, []string{callState, callSubmit}, h.github.requests(), "nothing after the comment")
			assert.Empty(t, h.github.merges)
			assert.Empty(t, h.github.autos)
			assert.Equal(t, Acted{}, h.inbox.acted[0])
		})
	}
}

func TestAMergeGitHubWouldRefusePostsNoComment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		action  Action
		harness func(t *testing.T) *harness
		state   *github.MergeState
		readErr error
		want    error
		words   string
	}{
		{
			name: "conflicts", action: ActionMerge, harness: ownClean, state: open(github.MergeStateDirty, headA),
			want: github.ErrNotMergeable, words: "It has conflicts.",
		},
		{
			name: "blocked", action: ActionMerge, harness: ownClean, state: open(github.MergeStateBlocked, headA),
			want: github.ErrNotMergeable, words: "Branch protection still blocks it.",
		},
		{name: "a read that fails", action: ActionMerge, harness: ownClean, readErr: errFakeRead, want: errFakeRead},
		{
			name: "no longer let past the rules", action: ActionMergeBypass, harness: bypassHarness, state: held(false),
			want: github.ErrNotMergeable, words: "Branch protection still blocks it.",
		},
		{
			name: "auto-merge of one no longer open", action: ActionAutoMerge, harness: ownBlocked,
			state: &github.MergeState{Found: true, State: github.StateClosed, Head: headA},
			want:  github.ErrNotMergeable, words: "It's no longer open.",
		},
		{
			name: "auto-merge of a draft", action: ActionAutoMerge, harness: ownBlocked,
			state: &github.MergeState{
				Found: true, State: github.StateOpen, Status: github.MergeStateBlocked, Head: headA, IsDraft: true,
			},
			want: github.ErrNotMergeable, words: "It's a draft",
		},
		{name: "auto-merge with a read that fails", action: ActionAutoMerge, harness: ownBlocked, readErr: errFakeRead, want: errFakeRead},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := tt.harness(t)
			h.github.stateErr = tt.readErr

			if tt.state != nil {
				h.github.states = []*github.MergeState{tt.state}
			}

			_, err := h.svc.Act(t.Context(), ActRequest{
				ItemID: pullID, Action: tt.action, Head: headA, Method: MethodSquash, Accept: true, Body: mergeText,
			})
			require.ErrorIs(t, err, tt.want, "nothing happened, so it is an error")

			if tt.words != "" {
				assert.Contains(t, uierr.Message(err), tt.words)
			}

			assert.Equal(t, []string{callState}, h.github.requests(), "the merge state alone: no comment")
			assert.Empty(t, h.github.sent)
			assert.Equal(t, Acted{}, h.inbox.acted[0])
		})
	}
}

func TestAMergeOfAPullRequestPushedSinceItWasReadPostsNoComment(t *testing.T) {
	t.Parallel()

	for _, tc := range commentFirstCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := tc.harness(t)
			moved := open(github.MergeStateClean, headB)
			moved.CanBypass = true
			h.github.states = []*github.MergeState{moved}

			res := h.act(t, ActRequest{Action: tc.action, Method: MethodSquash, Accept: true, Body: mergeText})
			assert.Equal(t, OutcomePushedSince, res.Outcome)
			assert.Equal(t, "acme-org/example-repo#7 was pushed to since you opened it. Reload it to see the new commits.",
				res.Message)
			assert.False(t, res.Commented, "the draft keeps its text for the commits the user hasn't seen")
			assert.Empty(t, res.Steps)
			assert.Equal(t, []string{callState}, h.github.requests())
		})
	}
}

func TestACommentPostedBeforeAMergeThatDidNotHappenSaysBoth(t *testing.T) {
	t.Parallel()

	refused := &github.NotMergeableError{Reason: "Required status check is expected."}

	tests := []struct {
		name     string
		action   Action
		harness  func(t *testing.T) *harness
		setup    func(g *fakeGitHub)
		requests []string
		reason   string
		message  string
	}{
		{
			name: "refused by the merge, never turning on auto-merge", action: ActionMerge, harness: ownClean,
			setup: func(g *fakeGitHub) {
				g.states, g.mergeErr = []*github.MergeState{open(github.MergeStateClean, headA)}, refused
			},
			requests: []string{callState, callSubmit, callMerge}, reason: trimmed(refused),
			message: "Commented on acme-org/example-repo#7, but it wasn't merged: " + trimmed(refused),
		},
		{
			name: "the gate refusing the merge", action: ActionMerge, harness: ownClean,
			setup: func(g *fakeGitHub) {
				g.states, g.mergeErr = []*github.MergeState{open(github.MergeStateClean, headA)}, errGateShut
			},
			requests: []string{callState, callSubmit, callMerge}, reason: trimmed(errGateShut),
			message: "Commented on acme-org/example-repo#7, but it wasn't merged: " + trimmed(errGateShut),
		},
		{
			name: "the gate refusing a merge past the rules", action: ActionMergeBypass, harness: bypassHarness,
			setup: func(g *fakeGitHub) {
				g.states, g.mergeErr = []*github.MergeState{held(true)}, errGateShut
			},
			requests: []string{callState, callSubmit, callMerge}, reason: trimmed(errGateShut),
			message: "Commented on acme-org/example-repo#7, but it wasn't merged: " + trimmed(errGateShut),
		},
		{
			name: "auto-merge refused", action: ActionAutoMerge, harness: ownBlocked,
			setup: func(g *fakeGitHub) {
				g.autoErr = uierr.New("Auto-merge is off here.", github.ErrGraphQL)
			},
			requests: []string{callState, callSubmit, callAuto}, reason: "Auto-merge is off here",
			message: "Commented on acme-org/example-repo#7, but auto-merge isn't on: Auto-merge is off here",
		},
		{
			name: "the merge auto-merge falls back to, refused", action: ActionAutoMerge, harness: ownBlocked,
			setup: func(g *fakeGitHub) {
				g.autoErr = github.ErrMergeableNow
				g.states = []*github.MergeState{open(github.MergeStateBlocked, headA), open(github.MergeStateDirty, headA)}
			},
			requests: []string{callState, callSubmit, callAuto, callState}, reason: "It has conflicts",
			message: "Commented on acme-org/example-repo#7, but it wasn't merged: It has conflicts",
		},
		{
			name: "the merge auto-merge falls back to, failing", action: ActionAutoMerge, harness: ownBlocked,
			setup: func(g *fakeGitHub) {
				g.autoErr, g.mergeErr = github.ErrMergeableNow, errGateShut
				g.states = []*github.MergeState{open(github.MergeStateBlocked, headA), open(github.MergeStateClean, headA)}
			},
			requests: []string{callState, callSubmit, callAuto, callState, callMerge}, reason: trimmed(errGateShut),
			message: "Commented on acme-org/example-repo#7, but it wasn't merged: " + trimmed(errGateShut),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := tt.harness(t)
			tt.setup(h.github)

			res := h.act(t, ActRequest{Action: tt.action, Method: MethodSquash, Accept: true, Body: mergeText})
			assert.Equal(t, OutcomeCommentedNotMerged, res.Outcome, "a result, never an error: the comment stands")
			assert.Equal(t, tt.message, res.Message)
			assert.Equal(t, tt.reason, res.Reason)
			assert.Equal(t, []string{"Commented"}, res.Steps)
			assert.True(t, res.Commented)
			assert.False(t, res.LeftInbox)
			assert.Equal(t, tt.requests, h.github.requests())
			assert.Equal(t, Acted{Reviewed: true, ReviewedAt: testNow, ReviewEvent: github.ReviewComment}, h.inbox.acted[0])
		})
	}
}

func TestACommentPostedBeforeGitHubFoundAPush(t *testing.T) {
	t.Parallel()

	h := ownClean(t)
	h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}
	h.github.mergeErr = github.ErrPushedSince

	res := h.act(t, ActRequest{Action: ActionMerge, Accept: true, Body: mergeText})
	assert.Equal(t, OutcomePushedSince, res.Outcome)
	assert.Equal(t,
		"Commented, but acme-org/example-repo#7 was pushed to since you opened it, so it wasn't merged.", res.Message)
	assert.Equal(t, []string{"Commented"}, res.Steps)
	assert.True(t, res.Commented, "the sheet drops the text it posted")
	assert.Equal(t, []string{callState, callSubmit, callMerge}, h.github.requests())
}

func TestAFailureAfterTheCommentIsAResult(t *testing.T) {
	t.Parallel()

	signedOut := uierr.New("You're signed in to another account now.", errFakeRead)

	tests := []struct {
		name     string
		action   Action
		harness  func(t *testing.T) *harness
		setup    func(t *testing.T, h *harness) context.Context
		requests []string
		reason   string
		message  string
	}{
		{
			name: "the fresh token's read refused too", action: ActionMerge, harness: ownClean,
			setup: func(t *testing.T, h *harness) context.Context {
				t.Helper()

				h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}
				h.github.mergeOnce, h.github.stateErr, h.github.stateOK = badCredentials(), badCredentials(), 1

				return t.Context()
			},
			requests: []string{callState, callSubmit, callMerge, callState}, reason: trimmed(badCredentials()),
			message: "Commented on acme-org/example-repo#7, but it wasn't merged: " + trimmed(badCredentials()),
		},
		{
			name: "the fresh token's account check failing", action: ActionMerge, harness: ownClean,
			setup: func(t *testing.T, h *harness) context.Context {
				t.Helper()

				h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}
				h.github.mergeOnce, h.inbox.retryErr = badCredentials(), signedOut

				return t.Context()
			},
			requests: []string{callState, callSubmit, callMerge}, reason: "You're signed in to another account now",
			message: "Commented on acme-org/example-repo#7, but it wasn't merged: You're signed in to another account now",
		},
		{
			name: "the context ending", action: ActionAutoMerge, harness: ownBlocked,
			setup: func(t *testing.T, h *harness) context.Context {
				t.Helper()

				ctx, cancel := context.WithCancel(t.Context())
				t.Cleanup(cancel)

				h.github.submitted, h.github.autoErr = cancel, github.ErrMergeableNow

				return ctx
			},
			requests: []string{callState, callSubmit, callAuto, callState}, reason: trimmed(context.Canceled),
			message: "Commented on acme-org/example-repo#7, but it wasn't merged: " + trimmed(context.Canceled),
		},
		{
			name: "the inbox failing to record it, after a merge GitHub refused", action: ActionMerge, harness: ownClean,
			setup: func(t *testing.T, h *harness) context.Context {
				t.Helper()

				h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}
				h.github.mergeErr, h.inbox.recordErr = errGateShut, errFakeRead

				return t.Context()
			},
			requests: []string{callState, callSubmit, callMerge}, reason: trimmed(errGateShut),
			message: "Commented on acme-org/example-repo#7, but it wasn't merged: " + trimmed(errGateShut),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := tt.harness(t)
			ctx := tt.setup(t, h)

			res, err := h.svc.Act(ctx, ActRequest{
				ItemID: pullID, Action: tt.action, Head: headA, Method: MethodSquash, Accept: true, Body: mergeText,
			})
			require.NoError(t, err, "the comment is on GitHub, so the sheet hears a result")
			assert.Equal(t, OutcomeCommentedNotMerged, res.Outcome)
			assert.Equal(t, tt.reason, res.Reason)
			assert.Equal(t, tt.message, res.Message)
			assert.True(t, res.Commented, "the sheet clears the draft")
			assert.Equal(t, []string{"Commented"}, res.Steps)
			assert.Equal(t, tt.requests, h.github.requests())
			assert.Len(t, h.github.sent, 1, "commented once")
		})
	}
}

func TestAMergeTheInboxFailedToRecordIsStillAMerge(t *testing.T) {
	t.Parallel()

	h := ownClean(t)
	h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}
	h.inbox.recordErr = errFakeRead

	res := h.act(t, ActRequest{Action: ActionMerge, Accept: true, Body: mergeText})
	assert.Equal(t, OutcomeMerged, res.Outcome, "GitHub merged it; the next refresh catches the inbox up")
	assert.Equal(t, "Commented and merged acme-org/example-repo#7 (squash)", res.Message)
	assert.True(t, res.Commented)
	assert.True(t, res.LeftInbox)
	assert.Contains(t, h.logs.String(), "an action's step failed after GitHub answered it")
}

func TestAFailureBeforeAnyCommentIsStillAnError(t *testing.T) {
	t.Parallel()

	h := ownClean(t)
	h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}
	h.github.mergeErr = badCredentials()

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionMerge, Head: headA, Accept: true})
	require.True(t, github.IsBadCredentials(err), "a merge with no text posted nothing, so it is an error")

	h = readHarness(t, github.MergeStateClean)
	h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}
	h.github.mergeErr = badCredentials()

	_, err = h.svc.Act(t.Context(), ActRequest{
		ItemID: pullID, Action: ActionApproveMerge, Head: headA, Accept: true, Body: mergeText,
	})
	require.True(t, github.IsBadCredentials(err), "an approval's text went as the approval, as before")
}

func TestARetriedTokenNeverCommentsTwice(t *testing.T) {
	t.Parallel()

	h := ownClean(t)
	h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}
	h.github.mergeOnce = badCredentials()

	res := h.act(t, ActRequest{Action: ActionMerge, Accept: true, Body: mergeText})
	assert.Equal(t, OutcomeMerged, res.Outcome)
	assert.Equal(t, 2, h.inbox.tries, "tried again with a fresh token")
	assert.Equal(t, []string{callState, callSubmit, callMerge, callState, callMerge}, h.github.requests(), "commented once")
	assert.Equal(t, "Commented and merged acme-org/example-repo#7 (squash)", res.Message)
}

func TestAMergeWithTextSendsNothingItWouldNotWithout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		harness func(t *testing.T) *harness
		req     ActRequest
		want    error
		words   string
	}{
		{
			name: "line comments off the diff", want: ErrNotInDiff, harness: ownClean,
			req: ActRequest{
				Action: ActionMerge, Accept: true, Body: mergeText,
				Comments: []LineComment{{Path: cacheFile, Line: 99, Side: SideRight, Body: "x"}},
			},
		},
		{
			name: "a merge past the rules not accepted", want: ErrWarningsNotAccepted, harness: bypassHarness,
			req: ActRequest{Action: ActionMergeBypass, Body: mergeText},
		},
		{
			name: "a merge GitHub would not make", want: ErrNotOffered, words: "still needs", harness: blockedHarness,
			req: ActRequest{Action: ActionMerge, Accept: true, Body: mergeText},
		},
		{
			name: "a comment GitHub would not take", want: ErrNotOffered, words: "started on GitHub", harness: pendingHarness,
			req: ActRequest{Action: ActionMerge, Accept: true, Body: mergeText},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := tt.harness(t)
			tt.req.ItemID, tt.req.Head = pullID, headA

			_, err := h.svc.Act(t.Context(), tt.req)
			require.ErrorIs(t, err, tt.want)

			if tt.words != "" {
				assert.Contains(t, uierr.Message(err), tt.words)
			}

			assert.Zero(t, h.inbox.acts, "nothing was sent")
			assert.Empty(t, h.github.requests())
		})
	}
}

func TestAReviewStartedOnGitHubHoldsBackOnlyTheComment(t *testing.T) {
	t.Parallel()

	h := pendingHarness(t)

	r, err := h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)

	i := slices.IndexFunc(r.Offers, func(o Offer) bool { return o.Action == ActionMerge })
	require.GreaterOrEqual(t, i, 0)
	assert.True(t, r.Offers[i].Enabled, "the merge itself is offered")
	assert.True(t, r.Offers[i].CommentsFirst)

	_, err = h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionMerge, Head: headA, Accept: true, Body: mergeText})
	require.ErrorIs(t, err, ErrNotOffered)
	assert.Equal(t, r.Offers[i].CommentBlocked, uierr.Message(err), "the words the window holds the button back with")
	assert.Equal(t, "You have a review of this pull request started on GitHub. Submit or discard it there first. "+
		"Or clear your text and line comments to merge without a comment.", uierr.Message(err))
	assert.Zero(t, h.inbox.acts)

	h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}

	res := h.act(t, ActRequest{Action: ActionMerge, Accept: true})
	assert.Equal(t, OutcomeMerged, res.Outcome, "without text it merges as before")
	assert.Equal(t, []string{callState, callMerge}, h.github.requests())
}

func TestTheOtherActionsPostNoCommentFirst(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateBehind)

	res := h.act(t, ActRequest{Action: ActionUpdateBranch, Body: mergeText})
	assert.Equal(t, OutcomeBranchUpdating, res.Outcome)
	assert.Equal(t, []string{callUpdate}, h.github.requests(), "an update of the branch posts no text")
	assert.False(t, res.Commented)

	h = readHarness(t, github.MergeStateClean)
	h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}

	res = h.act(t, ActRequest{Action: ActionApproveMerge, Accept: true, Body: mergeText})
	assert.Equal(t, "Approved and merged acme-org/example-repo#7 (squash)", res.Message)
	require.Len(t, h.github.sent, 1)
	assert.Equal(t, github.ReviewApprove, h.github.sent[0].Event, "the approval carries the text, as before")
	assert.False(t, res.Commented)
}

func TestTextIsWhatTheWindowTrimsToSomething(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateBlocked)

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionComment, Head: headA, Body: "\uFEFF"})
	require.ErrorIs(t, err, ErrNotOffered, "a comment of only a byte order mark says nothing, as the window says")
	assert.Zero(t, h.inbox.acts)
}
