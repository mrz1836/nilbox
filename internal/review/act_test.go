package review

import (
	"context"
	"slices"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// open is the merge state GitHub reads for #7 at head in status.
func open(status, head string) *github.MergeState {
	return &github.MergeState{Found: true, Status: status, Head: head, State: github.StateOpen}
}

// readHarness is a harness whose pull request the sheet has read, in merge state status.
func readHarness(t *testing.T, status string) *harness {
	t.Helper()

	h := newHarness(t)
	h.github.review = pullReview(headA, status)
	h.refreshed(t)

	return h
}

// act runs req for #7, which must not fail.
func (h *harness) act(t *testing.T, req ActRequest) Result {
	t.Helper()

	req.ItemID = pullID
	if req.Head == "" {
		req.Head = headA
	}

	res, err := h.svc.Act(t.Context(), req)
	require.NoError(t, err)

	return res
}

func TestApproveSendsOnePinnedReview(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateBlocked)

	res := h.act(t, ActRequest{
		Action: ActionApprove, Body: "Looks good.",
		Comments: []LineComment{{Path: cacheFile, Line: 3, Side: SideRight, Body: "Nice."}},
	})

	assert.Equal(t, OutcomeReviewed, res.Outcome)
	assert.Equal(t, "Approved acme-org/example-repo#7", res.Message)
	assert.Equal(t, []string{"Approved"}, res.Steps)
	assert.True(t, res.LeftReview)
	assert.False(t, res.LeftInbox)

	require.Len(t, h.github.sent, 1)
	assert.Equal(t, github.ReviewInput{
		Event: github.ReviewApprove, Body: "Looks good.", CommitID: headA,
		Comments: []github.LineComment{{Path: cacheFile, Line: 3, Side: github.SideRight, Body: "Nice."}},
	}, h.github.sent[0])

	require.Len(t, h.inbox.acted, 1)
	assert.Equal(t, Acted{Reviewed: true, ReviewedAt: testNow, ReviewEvent: github.ReviewApprove}, h.inbox.acted[0])
}

func TestTheOtherReviewsAndTheirWords(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateBlocked)

	res := h.act(t, ActRequest{Action: ActionRequestChanges, Body: "Please add a test."})
	assert.Equal(t, "Requested changes acme-org/example-repo#7", res.Message)
	assert.Equal(t, github.ReviewRequestChanges, h.github.sent[0].Event)

	res = h.act(t, ActRequest{
		Action: ActionComment, Comments: []LineComment{{Path: cacheFile, Line: 2, Side: SideLeft, Body: "Why?"}},
	})
	assert.Equal(t, "Commented acme-org/example-repo#7", res.Message)
	assert.Equal(t, github.ReviewComment, h.github.sent[1].Event)

	require.Len(t, h.inbox.acted, 2)
	assert.Equal(t, github.ReviewRequestChanges, h.inbox.acted[0].ReviewEvent, "the inbox hears which review it was")
	assert.Equal(t, github.ReviewComment, h.inbox.acted[1].ReviewEvent)
}

func TestActRefusesWhatIsNotSent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		req   ActRequest
		want  error
		words string
	}{
		{name: "changes without text", req: ActRequest{Action: ActionRequestChanges}, want: ErrNotOffered, words: "Write something"},
		{name: "a merge GitHub would not make", req: ActRequest{Action: ActionMerge}, want: ErrNotOffered, words: "still needs"},
		{name: "an unknown action", req: ActRequest{Action: "ship"}, want: ErrNotOffered},
		{
			name: "a comment off the diff", want: ErrNotInDiff,
			req: ActRequest{Action: ActionComment, Comments: []LineComment{{Path: cacheFile, Line: 99, Side: SideRight, Body: "x"}}},
		},
		{
			name: "a comment on a file without a diff", want: ErrNotInDiff,
			req: ActRequest{Action: ActionComment, Comments: []LineComment{{Path: "logo.png", Line: 1, Side: SideRight, Body: "x"}}},
		},
		{
			name: "a comment on no file", want: ErrNotInDiff,
			req: ActRequest{Action: ActionComment, Comments: []LineComment{{Path: "nope.go", Line: 1, Side: SideRight, Body: "x"}}},
		},
		{
			name: "comments without a review", want: ErrNotOffered,
			req: ActRequest{Action: ActionUpdateBranch, Comments: []LineComment{{Path: cacheFile, Line: 3, Side: SideRight, Body: "x"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := readHarness(t, github.MergeStateBlocked)
			tt.req.ItemID, tt.req.Head = pullID, headA

			_, err := h.svc.Act(t.Context(), tt.req)
			require.ErrorIs(t, err, tt.want)

			if tt.words != "" {
				assert.Contains(t, uierr.Message(err), tt.words)
			}

			assert.Zero(t, h.inbox.acts, "nothing was sent")
		})
	}
}

func TestActOnAPullRequestPushedSinceSendsNothing(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateClean)

	res := h.act(t, ActRequest{Action: ActionApprove, Head: headB})
	assert.Equal(t, OutcomePushedSince, res.Outcome)
	assert.Contains(t, res.Message, "was pushed to since you opened it")

	h.inbox.pull.HeadOID = headB

	res = h.act(t, ActRequest{Action: ActionApprove})
	assert.Equal(t, OutcomePushedSince, res.Outcome, "the inbox has seen a push the sheet has not")
	assert.Zero(t, h.inbox.acts)
}

func TestActBeforeTheFirstRead(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionApprove, Head: headA})
	require.ErrorIs(t, err, ErrNotRead)
}

func TestApproveAndMerge(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateClean)
	h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}

	res := h.act(t, ActRequest{Action: ActionApproveMerge, Accept: true})

	assert.Equal(t, OutcomeMerged, res.Outcome)
	assert.Equal(t, "Approved and merged acme-org/example-repo#7 (squash)", res.Message)
	assert.Equal(t, []string{"Approved", "Merged (squash)"}, res.Steps)
	assert.True(t, res.LeftInbox)
	assert.Equal(t, MethodSquash, res.Method)
	assert.Equal(t, []github.MergeInput{{SHA: headA, Method: "SQUASH"}}, h.github.merges)
	assert.Equal(t, Acted{Reviewed: true, ReviewedAt: testNow, ReviewEvent: github.ReviewApprove, Merged: true}, h.inbox.acted[0])

	c, err := h.svc.cache.load(t.Context(), pullID, 0)
	require.NoError(t, err)
	assert.Nil(t, c, "a merged pull request's cache is dropped")
}

func TestMergeAsksForTheWarningsToBeAccepted(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateClean)

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionMerge, Head: headA})
	require.ErrorIs(t, err, ErrWarningsNotAccepted, "the workflow file")

	_, err = h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionMerge, Head: headA, Accept: true, Method: MethodRebase})
	require.ErrorIs(t, err, ErrNotOffered, "a method the repository doesn't allow")
	assert.Zero(t, h.inbox.acts)

	h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}
	res := h.act(t, ActRequest{Action: ActionMerge, Accept: true, Method: MethodMerge})
	assert.Equal(t, "Merged acme-org/example-repo#7 (merge)", res.Message)
	assert.Empty(t, h.github.sent, "a merge alone leaves no review")
}

func TestApproveAndMergeFallsBackToAutoMerge(t *testing.T) {
	t.Parallel()

	for name, setup := range map[string]func(h *harness){
		"blocked once approved": func(h *harness) {
			h.github.states = []*github.MergeState{open(github.MergeStateBlocked, headA)}
		},
		"refused by the merge": func(h *harness) {
			h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}
			h.github.mergeErr = &github.NotMergeableError{Reason: "Required status check is expected."}
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := readHarness(t, github.MergeStateClean)
			setup(h)

			res := h.act(t, ActRequest{Action: ActionApproveMerge, Accept: true})
			assert.Equal(t, OutcomeAutoMerge, res.Outcome)
			assert.Equal(t, "Approved acme-org/example-repo#7 · auto-merge is on", res.Message)
			assert.Equal(t, []string{"SQUASH@" + headA}, h.github.autos)
			assert.False(t, res.LeftInbox)
		})
	}
}

func TestAMergeTellsTheReleasePageWhichRepositoryChanged(t *testing.T) {
	t.Parallel()

	merged := readHarness(t, github.MergeStateClean)
	merged.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}

	res := merged.act(t, ActRequest{Action: ActionApproveMerge, Accept: true})
	require.Equal(t, OutcomeMerged, res.Outcome)
	assert.Equal(t, []repo.Name{{Owner: "acme-org", Repo: "example-repo"}}, merged.merges.heard(),
		"the merged pull request's repository is heard once")

	auto := readHarness(t, github.MergeStateClean)
	auto.github.states = []*github.MergeState{open(github.MergeStateBlocked, headA)}

	res = auto.act(t, ActRequest{Action: ActionApproveMerge, Accept: true})
	require.Equal(t, OutcomeAutoMerge, res.Outcome)
	assert.Empty(t, auto.merges.heard(), "auto-merge merges nothing yet")

	refused := readHarness(t, github.MergeStateClean)
	refused.github.states = []*github.MergeState{open(github.MergeStateDirty, headA)}

	res = refused.act(t, ActRequest{Action: ActionApproveMerge, Accept: true})
	require.Equal(t, OutcomeApprovedNotMerged, res.Outcome)
	assert.Empty(t, refused.merges.heard(), "nor does a merge GitHub won't make")
}

func TestApproveAndMergeReportsWhatGitHubWouldNotDo(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateClean)
	h.github.states = []*github.MergeState{open(github.MergeStateDirty, headA)}

	res := h.act(t, ActRequest{Action: ActionApproveMerge, Accept: true})
	assert.Equal(t, OutcomeApprovedNotMerged, res.Outcome)
	assert.Equal(t, "Approved, but GitHub won't merge yet: It has conflicts", res.Message)
	assert.True(t, res.LeftReview, "the approval stands")

	h = readHarness(t, github.MergeStateClean)
	h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}
	h.github.mergeErr = errGateShut

	res = h.act(t, ActRequest{Action: ActionApproveMerge, Accept: true})
	assert.Equal(t, OutcomeApprovedNotMerged, res.Outcome, "the gate refusing the second write")
	assert.Equal(t, 1, h.github.called(callSubmit))

	h = readHarness(t, github.MergeStateClean)
	h.github.stateErr, h.github.stateOK = errFakeRead, 1

	res = h.act(t, ActRequest{Action: ActionApproveMerge, Accept: true})
	assert.Equal(t, OutcomeApprovedNotMerged, res.Outcome, "the guard read failed")
}

// steps returns the steps the harness's actions reported, in order.
func (h *harness) steps(t *testing.T) []string {
	t.Helper()

	all := h.events.All()
	out := make([]string, 0, len(all))

	for _, e := range all {
		p, ok := e.(event.ReviewProgress)
		require.True(t, ok, "only progress: %T", e)
		assert.Equal(t, pullID, p.ItemID)
		out = append(out, p.Step)
	}

	return out
}

func TestAnActionReportsEachStep(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := readHarness(t, github.MergeStateBlocked)
		h.github.states = []*github.MergeState{
			open(github.MergeStateBlocked, headA), open(github.MergeStateUnknown, headA), open(github.MergeStateClean, headA),
		}

		res := h.act(t, ActRequest{Action: ActionApproveMerge, Method: MethodSquash, Accept: true})
		require.Equal(t, OutcomeMerged, res.Outcome)

		assert.Equal(t, []string{
			"Checking your account",
			"Checking nothing was pushed since you opened it",
			"Approving",
			"Checking that GitHub can merge it",
			"Waiting for GitHub to work out whether it can merge",
			"Squash-merging",
		}, h.steps(t))
	})

	for _, c := range []struct {
		req  ActRequest
		last string
	}{
		{ActRequest{Action: ActionComment, Body: "A note."}, "Commenting"},
		{ActRequest{Action: ActionRequestChanges, Body: "Fix it."}, "Requesting changes"},
	} {
		t.Run(c.last, func(t *testing.T) {
			t.Parallel()

			h := readHarness(t, github.MergeStateBlocked)
			h.act(t, c.req)
			assert.Equal(t, []string{"Checking your account", c.last}, h.steps(t))
		})
	}
}

// bypassHarness is a harness whose pull request branch protection holds back, and which
// GitHub lets the user merge past the rules.
func bypassHarness(t *testing.T) *harness {
	t.Helper()

	h := newHarness(t)
	h.github.review = pullReview(headA, github.MergeStateBlocked)
	h.github.review.ViewerCanBypass = true
	h.refreshed(t)

	return h
}

// held is the merge state of #7 at headA, held back by the rules, with whether GitHub lets
// the user past them.
func held(canBypass bool) *github.MergeState {
	s := open(github.MergeStateBlocked, headA)
	s.CanBypass = canBypass

	return s
}

func TestAMergePastTheRules(t *testing.T) {
	t.Parallel()

	t.Run("merges what the rules hold back, once accepted", func(t *testing.T) {
		t.Parallel()

		h := bypassHarness(t)
		h.github.states = []*github.MergeState{held(true)}

		res := h.act(t, ActRequest{Action: ActionMergeBypass, Method: MethodSquash, Accept: true})
		assert.Equal(t, OutcomeMerged, res.Outcome)
		assert.Equal(t, "Merged acme-org/example-repo#7 past the rules (squash)", res.Message)
		assert.Equal(t, []string{"Merged past the rules (squash)"}, res.Steps)
		require.Len(t, h.github.merges, 1)
		assert.Equal(t, github.MergeInput{SHA: headA, Method: string(MethodSquash)}, h.github.merges[0])
		assert.Zero(t, h.github.called(callSubmit), "no review")
		assert.Contains(t, h.steps(t), "Squash-merging past the rules")
	})

	t.Run("is never sent unaccepted", func(t *testing.T) {
		t.Parallel()

		h := bypassHarness(t)

		_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionMergeBypass, Head: headA})
		require.ErrorIs(t, err, ErrWarningsNotAccepted)
		assert.Zero(t, h.github.called(callMerge))
	})

	t.Run("is refused once GitHub no longer lets the user", func(t *testing.T) {
		t.Parallel()

		h := bypassHarness(t)
		h.github.states = []*github.MergeState{held(false)}

		_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionMergeBypass, Head: headA, Accept: true})
		require.ErrorIs(t, err, github.ErrNotMergeable)
		assert.Zero(t, h.github.called(callMerge))
	})

	t.Run("is a plain merge once GitHub would merge anyway", func(t *testing.T) {
		t.Parallel()

		h := bypassHarness(t)
		h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}

		res := h.act(t, ActRequest{Action: ActionMergeBypass, Method: MethodSquash, Accept: true})
		assert.Equal(t, "Merged acme-org/example-repo#7 (squash)", res.Message)
		assert.Equal(t, []string{"Merged (squash)"}, res.Steps)
	})

	t.Run("never merges a head the user didn't see", func(t *testing.T) {
		t.Parallel()

		h := bypassHarness(t)
		moved := held(true)
		moved.Head = headB
		h.github.states = []*github.MergeState{moved}

		res := h.act(t, ActRequest{Action: ActionMergeBypass, Method: MethodSquash, Accept: true})
		assert.Equal(t, OutcomePushedSince, res.Outcome)
		assert.Zero(t, h.github.called(callMerge))
	})
}

func TestAnApprovalChecksTheHeadFirst(t *testing.T) {
	t.Parallel()

	t.Run("one pushed to since is not approved", func(t *testing.T) {
		t.Parallel()

		h := readHarness(t, github.MergeStateBlocked)
		h.github.states = []*github.MergeState{open(github.MergeStateBlocked, headB)}

		res := h.act(t, ActRequest{Action: ActionApprove})
		assert.Equal(t, OutcomePushedSince, res.Outcome)
		assert.Empty(t, res.Steps)
		assert.Empty(t, h.github.sent, "GitHub would take an approval of the older commit")
		assert.Empty(t, h.inbox.acted[0], "nothing to record")
	})

	t.Run("one whose head can't be read is not approved", func(t *testing.T) {
		t.Parallel()

		h := readHarness(t, github.MergeStateBlocked)
		h.github.stateErr = errFakeRead

		_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionApproveMerge, Head: headA, Accept: true})
		require.ErrorIs(t, err, errFakeRead)
		assert.Empty(t, h.github.sent)
	})

	t.Run("one no longer open is not approved", func(t *testing.T) {
		t.Parallel()

		h := readHarness(t, github.MergeStateBlocked)
		h.github.states = []*github.MergeState{{Found: true, State: github.StateClosed, Head: headA}}

		_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionApprove, Head: headA})
		require.ErrorIs(t, err, ErrNotOffered)
		assert.Equal(t, "It's no longer open.", uierr.Message(err))
		assert.Empty(t, h.github.sent)
	})

	t.Run("a comment stays on the commit the user saw", func(t *testing.T) {
		t.Parallel()

		h := readHarness(t, github.MergeStateBlocked)
		h.github.states = []*github.MergeState{open(github.MergeStateBlocked, headB)}

		res := h.act(t, ActRequest{Action: ActionComment, Body: "About what I saw."})
		assert.Equal(t, OutcomeReviewed, res.Outcome)
		assert.Zero(t, h.github.called(callState), "no read for a comment")
		require.Len(t, h.github.sent, 1)
		assert.Equal(t, headA, h.github.sent[0].CommitID)
	})
}

func TestApproveAndMergeOfAPullRequestPushedMidway(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateClean)
	h.github.states = []*github.MergeState{open(github.MergeStateBlocked, headA), open(github.MergeStateClean, headB)}

	res := h.act(t, ActRequest{Action: ActionApproveMerge, Accept: true})
	assert.Equal(t, OutcomePushedSince, res.Outcome)
	assert.Equal(t, []string{"Approved"}, res.Steps)
	assert.Contains(t, res.Message, "Approved, but")
	assert.Zero(t, h.github.called(callMerge), "never merged past the head the user saw")

	h = readHarness(t, github.MergeStateClean)
	h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}
	h.github.mergeErr = github.ErrPushedSince

	res = h.act(t, ActRequest{Action: ActionApproveMerge, Accept: true})
	assert.Equal(t, OutcomePushedSince, res.Outcome, "GitHub's 409")
}

func TestAPlainMergeNeverTurnsOnAutoMerge(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateClean)
	h.github.states = []*github.MergeState{open(github.MergeStateBlocked, headA)}

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionMerge, Head: headA, Accept: true})
	require.ErrorIs(t, err, github.ErrNotMergeable)
	assert.Zero(t, h.github.called(callAuto))
	assert.Zero(t, h.github.called(callMerge))
}

func TestARetriedTokenNeverApprovesTwice(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateClean)
	h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}
	h.github.mergeOnce = badCredentials()

	res := h.act(t, ActRequest{Action: ActionApproveMerge, Accept: true})
	assert.Equal(t, OutcomeMerged, res.Outcome)
	assert.Equal(t, 2, h.inbox.tries, "tried again with a fresh token")
	assert.Equal(t, 1, h.github.called(callSubmit), "approved once")
	assert.Equal(t, 2, h.github.called(callMerge))
	assert.Equal(t, []string{"Approved", "Merged (squash)"}, res.Steps)
}

func TestTheGuardWaitsWhileGitHubWorksItOut(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := readHarness(t, github.MergeStateClean)
		h.github.states = []*github.MergeState{
			open(github.MergeStateBlocked, headA), // the head, read before the approval
			open(github.MergeStateUnknown, headA), open(github.MergeStateUnknown, headA), open(github.MergeStateClean, headA),
		}

		start := time.Now()
		res := h.act(t, ActRequest{Action: ActionApproveMerge, Accept: true})

		assert.Equal(t, OutcomeMerged, res.Outcome)
		assert.Equal(t, 1+guardReads, h.github.called(callState))
		assert.Equal(t, 2*guardWait, time.Since(start), "two waits between three reads")
	})
}

func TestTheGuardGivesUpAfterItsReads(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := readHarness(t, github.MergeStateClean)
		h.github.states = []*github.MergeState{open(github.MergeStateUnknown, headA)}

		res := h.act(t, ActRequest{Action: ActionApproveMerge, Accept: true})
		assert.Equal(t, OutcomeApprovedNotMerged, res.Outcome)
		assert.Equal(t, 1+guardReads, h.github.called(callState), "the head before the approval, then the guard's")
	})
}

func TestTheGuardStopsWithItsContext(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := readHarness(t, github.MergeStateClean)
		h.github.states = []*github.MergeState{open(github.MergeStateUnknown, headA)}

		ctx, cancel := context.WithTimeout(t.Context(), guardWait/2)
		defer cancel()

		_, err := h.svc.Act(ctx, ActRequest{ItemID: pullID, Action: ActionApproveMerge, Head: headA, Accept: true})
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

func TestTheGuardRefusesWhatIsNoLongerMergeable(t *testing.T) {
	t.Parallel()

	for name, state := range map[string]*github.MergeState{
		"merged": {Found: true, Status: github.MergeStateClean, Head: headA, State: github.StateMerged},
		"gone":   {},
		"draft":  {Found: true, Status: github.MergeStateClean, Head: headA, State: github.StateOpen, IsDraft: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := readHarness(t, github.MergeStateClean)
			h.github.states = []*github.MergeState{state}

			_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionMerge, Head: headA, Accept: true})
			require.ErrorIs(t, err, github.ErrNotMergeable)
		})
	}
}

func TestApproveAndAutoMerge(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateBlocked)

	res := h.act(t, ActRequest{Action: ActionApproveAutoMerge, Accept: true})
	assert.Equal(t, OutcomeAutoMerge, res.Outcome)
	assert.Equal(t, []string{"Approved", "Auto-merge on (squash)"}, res.Steps)

	// GitHub can merge it now, so it merges instead.
	h = readHarness(t, github.MergeStateBlocked)
	h.github.autoErr = github.ErrMergeableNow
	h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}

	res = h.act(t, ActRequest{Action: ActionApproveAutoMerge, Accept: true})
	assert.Equal(t, OutcomeMerged, res.Outcome)

	// GitHub refuses auto-merge.
	h = readHarness(t, github.MergeStateBlocked)
	h.github.autoErr = uierr.New("Auto-merge is off here.", github.ErrGraphQL)

	res = h.act(t, ActRequest{Action: ActionApproveAutoMerge, Accept: true})
	assert.Equal(t, OutcomeApprovedNotMerged, res.Outcome)
	assert.Equal(t, "Auto-merge is off here", res.Reason)
}

func TestAutoMergeOfAnApprovedPullRequest(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	review := pullReview(headA, github.MergeStateBlocked)
	review.ViewerReview = github.ReviewMark{State: "APPROVED", Commit: headA}
	h.github.review = review
	h.refreshed(t)

	res := h.act(t, ActRequest{Action: ActionAutoMerge, Accept: true})
	assert.Equal(t, OutcomeAutoMerge, res.Outcome)
	assert.Equal(t, "Auto-merge is on for acme-org/example-repo#7 (squash)", res.Message)
	assert.Empty(t, h.github.sent)

	h.github.autoErr = github.ErrPushedSince
	h.inbox.pull.AutoMerge = ""

	res = h.act(t, ActRequest{Action: ActionAutoMerge, Accept: true})
	assert.Equal(t, OutcomePushedSince, res.Outcome)
}

func TestUpdateTheBranch(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateBehind)

	res := h.act(t, ActRequest{Action: ActionUpdateBranch})
	assert.Equal(t, OutcomeBranchUpdating, res.Outcome)
	assert.Equal(t, "Updating the branch of acme-org/example-repo#7", res.Message)

	h.github.updateErr = github.ErrPushedSince
	res = h.act(t, ActRequest{Action: ActionUpdateBranch})
	assert.Equal(t, OutcomePushedSince, res.Outcome)

	h.github.updateErr = errGateShut
	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionUpdateBranch, Head: headA})
	require.ErrorIs(t, err, errGateShut, "nothing happened, so it is an error")
}

func TestAskDependabotToRebase(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.inbox.pull.AuthorLogin, h.inbox.pull.AuthorType = "dependabot", "Bot"
	h.github.review = pullReview(headA, github.MergeStateDirty)
	h.refreshed(t)

	res := h.act(t, ActRequest{Action: ActionRebaseBot})
	assert.Equal(t, OutcomeAskedBot, res.Outcome)
	assert.Equal(t, []string{"@dependabot rebase"}, h.github.comments)

	h.github.commentErr = errGateShut
	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionRebaseBot, Head: headA})
	require.ErrorIs(t, err, errGateShut)
}

// keepUpHarness is a harness whose pull request the sheet has read in merge state status,
// opened by Dependabot or someone else, which the user may update or not.
func keepUpHarness(t *testing.T, status string, bot, canUpdate bool) *harness {
	t.Helper()

	h := newHarness(t)
	if bot {
		h.inbox.pull.AuthorLogin, h.inbox.pull.AuthorType = "dependabot", "Bot"
	}

	h.github.review = pullReview(headA, status)
	h.github.review.ViewerCanUpdateBranch = canUpdate
	h.refreshed(t)

	return h
}

func TestActDecidesAgainOnARebaseOrAnUpdateWheneverTheyApply(t *testing.T) {
	t.Parallel()

	t.Run("Dependabot asked to rebase a branch behind its base in any merge state", func(t *testing.T) {
		t.Parallel()

		for _, status := range []string{
			github.MergeStateBlocked, github.MergeStateClean, github.MergeStateUnstable, github.MergeStateBehind,
			github.MergeStateDirty,
		} {
			t.Run(status, func(t *testing.T) {
				t.Parallel()

				// GitHub can't update a branch with conflicts, which the rebase resolves.
				h := keepUpHarness(t, status, true, status != github.MergeStateDirty)

				res := h.act(t, ActRequest{Action: ActionRebaseBot})
				assert.Equal(t, OutcomeAskedBot, res.Outcome)
				assert.Equal(t, []string{callComment}, h.github.requests())
				assert.Equal(t, []string{"@dependabot rebase"}, h.github.comments)
			})
		}
	})

	for _, c := range []struct {
		name, words string
		edit        func(h *harness)
	}{
		{"never someone else's pull request", "isn't something the sheet can do", func(h *harness) {
			h.inbox.pull.AuthorLogin, h.inbox.pull.AuthorType = octoOther, "User"
		}},
		{"nor a branch up to date", "nothing to rebase", func(h *harness) {
			h.github.review.ViewerCanUpdateBranch = false
		}},
		{"nor for a user without push access", "push access", func(h *harness) {
			h.inbox.pull.ViewerPermission = "TRIAGE"
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			h.inbox.pull.AuthorLogin, h.inbox.pull.AuthorType = "dependabot", "Bot"
			h.github.review = pullReview(headA, github.MergeStateBlocked)
			h.github.review.ViewerCanUpdateBranch = true
			c.edit(h)
			h.refreshed(t)

			_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionRebaseBot, Head: headA})
			require.ErrorIs(t, err, ErrNotOffered)
			assert.Contains(t, uierr.Message(err), c.words)
			assert.Zero(t, h.inbox.acts, "nothing was sent")
		})
	}

	t.Run("a branch behind a base the rules don't mind is updated", func(t *testing.T) {
		t.Parallel()

		h := keepUpHarness(t, github.MergeStateBlocked, false, true)

		res := h.act(t, ActRequest{Action: ActionUpdateBranch})
		assert.Equal(t, OutcomeBranchUpdating, res.Outcome)
		assert.Equal(t, []string{callUpdate}, h.github.requests())
	})

	t.Run("an update GitHub doesn't let the user make", func(t *testing.T) {
		t.Parallel()

		h := keepUpHarness(t, github.MergeStateBlocked, false, false)

		_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionUpdateBranch, Head: headA})
		require.ErrorIs(t, err, ErrNotOffered)
		assert.Zero(t, h.inbox.acts, "nothing was sent")
	})

	t.Run("Dependabot's branch is updated once the warning is accepted", func(t *testing.T) {
		t.Parallel()

		h := keepUpHarness(t, github.MergeStateBlocked, true, true)

		_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionUpdateBranch, Head: headA})
		require.ErrorIs(t, err, ErrWarningsNotAccepted)
		assert.Equal(t, "Accept the warnings first.", uierr.Message(err))
		assert.Zero(t, h.inbox.acts, "nothing was sent")

		res := h.act(t, ActRequest{Action: ActionUpdateBranch, Accept: true})
		assert.Equal(t, OutcomeBranchUpdating, res.Outcome)
		assert.Equal(t, []string{callUpdate}, h.github.requests())
		assert.Empty(t, h.github.comments, "Dependabot isn't asked as well")
	})
}

func TestTheSheetReadsAgainAfterAnAction(t *testing.T) {
	t.Parallel()

	t.Run("one GitHub took", func(t *testing.T) {
		t.Parallel()

		h := readHarness(t, github.MergeStateBlocked)
		reads := h.github.called(callReview)

		h.act(t, ActRequest{Action: ActionApprove})

		opened, err := h.svc.Open(t.Context(), pullID)
		require.NoError(t, err)
		assert.Empty(t, opened.FetchedAt, "the read before the approval no longer counts as current")

		h.refreshed(t)
		assert.Equal(t, reads+1, h.github.called(callReview), "a refresh right after reads again")
	})

	t.Run("one refused for a head the sheet no longer has", func(t *testing.T) {
		t.Parallel()

		h := readHarness(t, github.MergeStateClean)
		reads := h.github.called(callReview)

		res := h.act(t, ActRequest{Action: ActionMerge, Head: headB, Accept: true})
		assert.Equal(t, OutcomePushedSince, res.Outcome)

		h.refreshed(t)
		assert.Equal(t, reads+1, h.github.called(callReview), "the reload the sheet offers reads")
	})

	t.Run("one GitHub refused", func(t *testing.T) {
		t.Parallel()

		h := readHarness(t, github.MergeStateBlocked)
		reads := h.github.called(callReview)
		h.github.submitErr = github.ErrOwnPull

		_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionApprove, Head: headA})
		require.ErrorIs(t, err, github.ErrOwnPull)

		h.refreshed(t)
		assert.Equal(t, reads+1, h.github.called(callReview))
	})
}

func TestAReviewGitHubRefusesIsAnError(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateClean)
	h.github.submitErr = github.ErrOwnPull

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionApproveMerge, Head: headA, Accept: true})
	require.ErrorIs(t, err, github.ErrOwnPull)
	assert.Equal(t, 1, h.github.called(callState), "only the head, read before the approval")
	assert.Zero(t, h.github.called(callMerge), "no merge without the approval")
}

func TestBadCredentialsMidwayAreRetriedNotReported(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateClean)
	h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}
	h.github.mergeErr = badCredentials()

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionApproveMerge, Head: headA, Accept: true})
	require.True(t, github.IsBadCredentials(err), "the actor retries a refused token; a second refusal is its error")
}

// heldRunsHarness is a harness whose pull request the sheet has read at headA, unstable
// with the workflow runs with IDs runs waiting for approval, and another app's suite that
// failed to start. Its diff changes a workflow, so approving them asks first.
func heldRunsHarness(t *testing.T, runs ...int64) *harness {
	t.Helper()

	h := newHarness(t)
	h.github.review = pullReview(headA, github.MergeStateUnstable)
	h.github.review.Unrun = []github.UnrunSuite{{Name: "Example CI", Conclusion: "STARTUP_FAILURE"}}

	for _, id := range runs {
		h.github.review.Unrun = append(h.github.review.Unrun, github.UnrunSuite{
			Name: "Workflow " + strconv.FormatInt(id, 10), Conclusion: github.SuiteAwaitsApproval, RunID: id,
			URL: "https://github.com/acme-org/example-repo/actions/runs/" + strconv.FormatInt(id, 10),
		})
	}

	h.refreshed(t)

	return h
}

func TestApproveHeldRuns(t *testing.T) {
	t.Parallel()

	h := heldRunsHarness(t, 101, 102)

	before, err := h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)

	i := slices.IndexFunc(before.Offers, func(o Offer) bool { return o.Action == ActionApproveRuns })
	require.GreaterOrEqual(t, i, 0, "the sheet offers to approve them")
	assert.True(t, before.Offers[i].Enabled)
	assert.Equal(t, []string{workflowsWarning}, before.Offers[i].Warnings, "#7's diff changes a workflow")

	res := h.act(t, ActRequest{Action: ActionApproveRuns, Accept: true, Body: "Not sent."})
	assert.Equal(t, OutcomeRunsApproved, res.Outcome)
	assert.Equal(t, "Approved 2 workflow runs on acme-org/example-repo#7", res.Message)
	assert.Empty(t, res.Reason)
	assert.Equal(t, []string{"Approved Workflow 101", "Approved Workflow 102"}, res.Steps)
	assert.False(t, res.LeftReview || res.LeftInbox || res.Commented)
	assert.Equal(t, []string{"acme-org/example-repo:101", "acme-org/example-repo:102"}, h.github.approved,
		"each run is approved once, in its repository, by its ID")
	assert.Equal(t, []string{callApprove, callApprove}, h.github.requests(),
		"no text is posted, and nothing else is read or sent")
	assert.Equal(t, []string{"Checking your account", "Approving Workflow 101", "Approving Workflow 102"}, h.steps(t))

	opened, err := h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)
	assert.Empty(t, opened.FetchedAt, "the read before the approval no longer counts as current")

	// The sheet's next read finds the runs started: none held, and their checks queued.
	reads := h.github.called(callReview)
	h.github.review = pullReview(headA, github.MergeStateBlocked)
	h.github.review.Checks = append(h.github.review.Checks, github.Check{
		Kind: github.CheckKindRun, Name: "lint", Workflow: "Workflow 101", State: "QUEUED", Outcome: github.CheckPending,
	})

	r := h.refreshed(t)
	assert.Equal(t, reads+1, h.github.called(callReview), "a refresh right after reads again")
	assert.Empty(t, r.Unrun)

	assert.False(t, slices.ContainsFunc(r.Offers, func(o Offer) bool { return o.Action == ActionApproveRuns }),
		"nothing is left to approve")
}

func TestApprovingOneHeldRunSaysSo(t *testing.T) {
	t.Parallel()

	h := heldRunsHarness(t, 101)

	res := h.act(t, ActRequest{Action: ActionApproveRuns, Accept: true})
	assert.Equal(t, "Approved 1 workflow run on acme-org/example-repo#7", res.Message)
}

func TestApprovingHeldRunsAsksFirstWhenItChangesWorkflows(t *testing.T) {
	t.Parallel()

	h := heldRunsHarness(t, 101)

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionApproveRuns, Head: headA})
	require.ErrorIs(t, err, ErrWarningsNotAccepted)
	assert.Zero(t, h.github.called(callApprove))

	// Without the workflow in its diff, it runs on the click.
	h = newHarness(t)
	h.github.diff = &github.PullDiff{Head: headA, Total: 1, Complete: true, Files: []github.PullFile{
		{Path: cacheFile, Status: "modified", Patch: "@@ -1 +1 @@\n-a\n+b", HasPatch: true, Additions: 1, Deletions: 1},
	}}
	h.github.review = pullReview(headA, github.MergeStateUnstable)
	h.github.review.Unrun = []github.UnrunSuite{{Name: "CI", Conclusion: github.SuiteAwaitsApproval, RunID: 7}}
	h.refreshed(t)

	res := h.act(t, ActRequest{Action: ActionApproveRuns})
	assert.Equal(t, OutcomeRunsApproved, res.Outcome)
	assert.Equal(t, []string{"acme-org/example-repo:7"}, h.github.approved)
}

func TestARetriedTokenNeverApprovesARunTwice(t *testing.T) {
	t.Parallel()

	h := heldRunsHarness(t, 101, 102)
	h.github.approveOnce = map[int64]error{102: badCredentials()}

	res := h.act(t, ActRequest{Action: ActionApproveRuns, Accept: true})
	assert.Equal(t, OutcomeRunsApproved, res.Outcome)
	assert.Equal(t, []string{"acme-org/example-repo:101", "acme-org/example-repo:102"}, h.github.approved)
	assert.Equal(t, 3, h.github.called(callApprove), "101 once, then 102 with each token")
	assert.Equal(t, 2, h.inbox.tries)
}

func TestApprovingHeldRunsPartwayIsAResult(t *testing.T) {
	t.Parallel()

	h := heldRunsHarness(t, 101, 102, 103)
	h.github.approveErrs = map[int64]error{102: uierr.New(
		"GitHub wouldn't approve workflow run 102 of acme-org/example-repo: Resource not accessible by integration.",
		errGateShut,
	)}

	res := h.act(t, ActRequest{Action: ActionApproveRuns, Accept: true})
	assert.Equal(t, OutcomeRunsApproved, res.Outcome)
	assert.Equal(t, "GitHub wouldn't approve workflow run 102 of acme-org/example-repo: Resource not accessible by "+
		"integration", res.Reason)
	assert.Equal(t, "Approved 1 of 3 workflow runs on acme-org/example-repo#7; GitHub refused the rest: GitHub "+
		"wouldn't approve workflow run 102 of acme-org/example-repo: Resource not accessible by integration", res.Message)
	assert.Equal(t, []string{"Approved Workflow 101"}, res.Steps)
	assert.Equal(t, []string{"acme-org/example-repo:101"}, h.github.approved, "the first refusal stops the rest")
}

func TestApprovingHeldRunsThatGitHubRefusesFirstIsAnError(t *testing.T) {
	t.Parallel()

	h := heldRunsHarness(t, 101, 102)
	h.github.approveErrs = map[int64]error{101: errGateShut}

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionApproveRuns, Head: headA, Accept: true})
	require.ErrorIs(t, err, errGateShut, "nothing happened, so it is an error")
	assert.Empty(t, h.github.approved)
	assert.Equal(t, 1, h.github.called(callApprove))
}

func TestApprovingHeldRunsOfAPullRequestPushedSinceSendsNothing(t *testing.T) {
	t.Parallel()

	h := heldRunsHarness(t, 101)

	res := h.act(t, ActRequest{Action: ActionApproveRuns, Head: headB, Accept: true})
	assert.Equal(t, OutcomePushedSince, res.Outcome, "the runs read belong to another head than the one the user saw")
	assert.Zero(t, h.github.called(callApprove))
	assert.Zero(t, h.inbox.acts)
}

func TestApprovingHeldRunsWithoutTheirIDsWaits(t *testing.T) {
	t.Parallel()

	// A cache an older build kept names the held runs without their IDs.
	h := newHarness(t)
	h.github.review = pullReview(headA, github.MergeStateUnstable)
	h.github.review.Unrun = []github.UnrunSuite{{Name: "CI", Conclusion: github.SuiteAwaitsApproval}}
	h.refreshed(t)

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionApproveRuns, Head: headA, Accept: true})
	require.ErrorIs(t, err, ErrNotOffered)
	assert.Equal(t, "Wait for it to load from GitHub.", uierr.Message(err))
	assert.Zero(t, h.github.called(callApprove))
}

func TestApprovingHeldRunsTakesWriteAccess(t *testing.T) {
	t.Parallel()

	h := heldRunsHarness(t, 101)
	h.inbox.pull.ViewerPermission = "READ"

	_, err := h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionApproveRuns, Head: headA, Accept: true})
	require.ErrorIs(t, err, ErrNotOffered)
	assert.Zero(t, h.github.called(callApprove))
}
