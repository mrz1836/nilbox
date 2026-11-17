package core

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agentrun"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/review"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// TestMergeAndReviewReachNoForbiddenAction is the guard over Merge it's and Post the
// agent's review's path: the adapter names no sheet action but a merge now, auto-merge,
// and the three reviews (and reads approve_merge's offer, to tell a merge GitHub holds for
// an approving review, never sending it), so no merge past the rules, combined approval
// and merge, close, or approval of workflow runs is reachable from a routine.
func TestMergeAndReviewReachNoForbiddenAction(t *testing.T) {
	t.Parallel()

	allowed := []string{
		"review.ActionMerge", "review.ActionAutoMerge", "review.ActionComment", "review.ActionRequestChanges",
		"review.ActionApprove", "review.ActionNone", "review.ActionApproveMerge",
	}

	named := sheetActionsNamed(t, "adapters_followups.go")
	require.NotEmpty(t, named, "the guard reads the adapter")

	for _, name := range named {
		assert.Contains(t, allowed, name, "Merge it and Post the agent's review must never name %s", name)
	}

	events := []autopilot.ReviewEvent{autopilot.EventComment, autopilot.EventRequestChanges, autopilot.EventApprove}

	sent := make([]review.Action, 0, 2+len(events))
	sent = append(sent, mergeActionOf(true), mergeActionOf(false))

	for _, e := range events {
		a, ok := reviewActionOfEvent(e)
		require.True(t, ok, e)

		sent = append(sent, a)
	}

	for _, a := range sent {
		assert.NotContains(t, []review.Action{
			review.ActionMergeBypass, review.ActionApproveMerge, review.ActionApproveAutoMerge, review.ActionClose,
			review.ActionApproveRuns,
		}, a, "never sent")
	}

	_, ok := reviewActionOfEvent("dismiss")
	assert.False(t, ok)
}

// pinnedHead is the commit a merge or a review is pinned to.
const pinnedHead = "abc"

// offered is an offer of action, enabled or with why not, and its warnings.
func offered(action review.Action, enabled bool, warnings ...string) review.Offer {
	o := review.Offer{Action: action, Enabled: enabled, Warnings: warnings}
	if !enabled {
		o.Reason = "GitHub won't merge it yet."
	}

	return o
}

func TestAMergeGoesOnlyAsGitHubSaysItWould(t *testing.T) {
	t.Parallel()

	now := autopilot.MergeSend{Head: pinnedHead}
	auto := autopilot.MergeSend{Head: pinnedHead, AutoMerge: true}

	for _, c := range []struct {
		name    string
		rev     review.Review
		req     autopilot.MergeSend
		decided bool
		outcome autopilot.GitHubOutcome
		action  review.Action
	}{
		{
			name: "mergeable now: it merges", req: now, action: review.ActionMerge,
			rev: review.Review{Head: pinnedHead, Offers: []review.Offer{offered(review.ActionMerge, true)}},
		},
		{
			name: "not mergeable now: not offered", req: now, decided: true, outcome: autopilot.GitHubNotOffered,
			rev: review.Review{Head: pinnedHead, Offers: []review.Offer{offered(review.ActionMerge, false)}},
		},
		{
			name: "held for an approving review: it waits for a person", req: now, decided: true,
			outcome: autopilot.GitHubNeedsReview, rev: review.Review{Head: pinnedHead, Offers: []review.Offer{
				offered(review.ActionMerge, false), offered(review.ActionApproveMerge, true),
			}},
		},
		{
			name: "pushed to since its checks passed: never merged", req: now, decided: true,
			outcome: autopilot.GitHubNotOffered,
			rev:     review.Review{Head: "def", Offers: []review.Offer{offered(review.ActionMerge, true)}},
		},
		{
			name: "with warnings: it waits for the user", req: now, decided: true, outcome: autopilot.GitHubWarning,
			rev: review.Review{Head: pinnedHead, Offers: []review.Offer{offered(review.ActionMerge, true, "Lint failed.")}},
		},
		{
			name: "auto-merge back on", req: auto, action: review.ActionAutoMerge,
			rev: review.Review{Head: pinnedHead, Offers: []review.Offer{offered(review.ActionAutoMerge, true)}},
		},
		{
			name: "auto-merge on already: nothing to do", req: auto, decided: true, outcome: autopilot.GitHubNothing,
			rev: review.Review{Head: pinnedHead, AutoMerge: review.MethodSquash},
		},
		{
			name: "only a merge past the rules: not offered", req: now, decided: true, outcome: autopilot.GitHubNotOffered,
			rev: review.Review{Head: pinnedHead, Offers: []review.Offer{
				offered(review.ActionMerge, false), offered(review.ActionMergeBypass, true),
			}},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			action, sent, decided := mergeDecision(c.rev, c.req)
			assert.Equal(t, c.decided, decided)

			if c.decided {
				assert.Equal(t, c.outcome, sent.Outcome)
			} else {
				assert.Equal(t, c.action, action)
			}

			assert.NotEqual(t, review.ActionMergeBypass, action, "never past the rules")
		})
	}
}

func TestAReviewIsPostedOnlyAtTheCommitTheAgentRead(t *testing.T) {
	t.Parallel()

	rev := review.Review{Head: pinnedHead, Offers: []review.Offer{offered(review.ActionApprove, true)}}

	_, decided := reviewDecision(rev, autopilot.ReviewSend{Head: pinnedHead}, review.ActionApprove)
	assert.False(t, decided, "it goes")

	sent, decided := reviewDecision(rev, autopilot.ReviewSend{Head: "old"}, review.ActionApprove)
	assert.True(t, decided)
	assert.Equal(t, autopilot.GitHubNotOffered, sent.Outcome, "a push since posts nothing")

	sent, decided = reviewDecision(rev, autopilot.ReviewSend{Head: pinnedHead}, review.ActionRequestChanges)
	assert.True(t, decided)
	assert.Equal(t, autopilot.GitHubNotOffered, sent.Outcome, "a review the sheet doesn't offer isn't sent")
}

// changesAt is what agent runs changed on an item at each head, for agentChangedAt.
type changesAt map[string]sensitive.Check

func (c changesAt) AgentChanged(_ context.Context, _ int64, head string) (sensitive.Check, bool, error) {
	check, ok := c[head]

	return check, ok, nil
}

func TestNoMergeFollowsAnAgentsChangeToSensitiveFiles(t *testing.T) {
	t.Parallel()

	g := githubSteps{changes: changesAt{
		"workflows": {Complete: true, Groups: []sensitive.Group{{Label: "Workflow files"}, {Label: "Agent files"}}},
		"part":      {Complete: false},
		"clean":     {Complete: true},
	}}

	words, held, err := g.agentChangedAt(t.Context(), 1, "workflows")
	require.NoError(t, err)
	assert.True(t, held)
	assert.Equal(t, "Its agent's commits changed workflow files and agent files, so it isn't merged: a person reviews those.", words)

	_, held, err = g.agentChangedAt(t.Context(), 1, "part")
	require.NoError(t, err)
	assert.True(t, held, "what it changed, not all known, merges nothing")

	for _, head := range []string{"clean", "never-noted"} {
		_, held, err = g.agentChangedAt(t.Context(), 1, head)
		require.NoError(t, err)
		assert.False(t, held, head)
	}

	_, held, err = githubSteps{}.agentChangedAt(t.Context(), 1, "workflows")
	require.NoError(t, err)
	assert.False(t, held, "nothing to read, nothing held")
}

func TestAMergesAnswerIsItsOutcome(t *testing.T) {
	t.Parallel()

	got, err := mergeSent(review.Result{Outcome: review.OutcomeMerged, Message: "Merged #12."}, nil)
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubSentIt, got.Outcome)
	assert.Equal(t, "Merged #12.", got.Words)

	got, err = mergeSent(review.Result{Outcome: review.OutcomePushedSince}, nil)
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome, "a push since merges nothing, and isn't tried again")

	gone := uierr.New("That isn't something the sheet can do now.", fmt.Errorf("%w: merge", review.ErrNotOffered))
	got, err = mergeSent(review.Result{}, gone)
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome)

	_, err = mergeSent(review.Result{}, errDropped)
	require.ErrorIs(t, err, errDropped, "anything else may have reached GitHub, and fails the step")
}

func TestAnAgentsLineCommentsGoAsTheSheetTakesThem(t *testing.T) {
	t.Parallel()

	got := lineComments([]autopilot.ReviewLine{
		{Path: "a.go", Line: 4, Body: "x"},
		{Path: "a.go", Line: 9, StartLine: 6, Side: "left", Body: "y"},
		{Path: "a.go", Line: 3, StartLine: 5, Body: "a range backward is one line"},
	})

	assert.Equal(t, []review.LineComment{
		{Path: "a.go", Line: 4, Side: review.SideRight, Body: "x"},
		{Path: "a.go", Line: 9, Side: review.SideLeft, StartLine: 6, StartSide: review.SideLeft, Body: "y"},
		{Path: "a.go", Line: 3, Side: review.SideRight, Body: "a range backward is one line"},
	}, got)

	assert.Equal(t, review.MethodSquash, mergeMethodOf(catalog.MergeSquash))
	assert.Equal(t, review.MethodMerge, mergeMethodOf(catalog.MergeCommit))
	assert.Equal(t, review.MethodRebase, mergeMethodOf(catalog.MergeRebase))
	assert.Empty(t, mergeMethodOf(catalog.MergeDefault), "the repository's own, as Settings picks it")
}

func TestAnAgentsReviewIsCarriedFromItsReport(t *testing.T) {
	t.Parallel()

	assert.Nil(t, agentReview(nil))

	got := agentReview(&agentrun.Review{
		Verdict: "request_changes", Body: "A typo.",
		Comments: []agentrun.ReviewComment{{Path: "a.go", Line: 2, Side: "RIGHT", Body: "typo"}},
	})
	require.NotNil(t, got)
	assert.Equal(t, autopilot.ReviewRequestChanges, got.Verdict)
	assert.Equal(t, []autopilot.ReviewLine{{Path: "a.go", Line: 2, Side: "RIGHT", Body: "typo"}}, got.Comments)
}
