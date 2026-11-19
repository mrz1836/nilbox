package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/sensitive"
)

// errChangesUnread is what agent runs changed on an item failing to read.
var errChangesUnread = errors.New("the agent runs' changes could not be read")

// unreadChanges is what agent runs changed on an item, which can't be read.
type unreadChanges struct{}

func (unreadChanges) AgentChanged(context.Context, int64, string) (sensitive.Check, bool, error) {
	return sensitive.Check{}, false, errChangesUnread
}

// mergeOf is a Merge it of the rig's pull request, pinned to ghHeadA.
func (r *ghRig) mergeOf(auto bool, method catalog.MergeMethod) autopilot.MergeSend {
	return autopilot.MergeSend{Item: r.ref(r.pull, ghPullNumber), Head: ghHeadA, AutoMerge: auto, Method: method}
}

// reviewOf is a Post the agent's review of the rig's pull request, pinned to ghHeadA.
func (r *ghRig) reviewOf(event autopilot.ReviewEvent, body string, lines ...autopilot.ReviewLine) autopilot.ReviewSend {
	return autopilot.ReviewSend{
		Item: r.ref(r.pull, ghPullNumber), Head: ghHeadA, Event: event, Body: body, Comments: lines,
	}
}

func TestAMergeItGoesThroughTheReviewSheetAtTheHeadItsChecksPassedOn(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)

	got, err := r.steps.Merge(t.Context(), r.mergeOf(false, catalog.MergeDefault))
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubSentIt, got.Outcome)
	assert.NotEmpty(t, got.Words, "it says what the sheet did")

	writes := r.pulls.writes()
	require.Len(t, writes, 1, "one merge, and nothing else")
	assert.True(t, strings.HasPrefix(writes[0], "merge #7 "), writes[0])
	assert.Contains(t, r.pulls.asked(), "state "+ghNode(ghPullNumber), "the merge state is read fresh right before it")
	assert.True(t, r.pulls.allAutopilots(), "every request says it is Autopilot's")
}

func TestAMergeItMergesNothingPushedToSinceItsChecksPassed(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)
	r.pulls.setRead(ghPullNumber, ghPullRead(ghPullNumber, ghAuthor, "User", ghHeadB, github.MergeStateClean))

	got, err := r.steps.Merge(t.Context(), r.mergeOf(false, catalog.MergeDefault))
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome)
	assert.Equal(t, "It was pushed to after its checks passed, so it isn't merged.", got.Words)
	assert.Empty(t, r.pulls.writes(), "no merge went")
}

func TestAMergeItMergesOnlyHowTheRepositoryAllows(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)

	got, err := r.steps.Merge(t.Context(), r.mergeOf(false, catalog.MergeRebase))
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome)
	assert.Equal(t, "The repository doesn't allow that merge method.", got.Words)
	assert.Empty(t, r.pulls.writes())
}

func TestAMergeItTurnsAutoMergeBackOnWhileGitHubHoldsIt(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)
	r.pulls.setRead(ghPullNumber, ghPullRead(ghPullNumber, ghAuthor, "User", ghHeadA, github.MergeStateBlocked))

	got, err := r.steps.Merge(t.Context(), r.mergeOf(true, catalog.MergeSquash))
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubSentIt, got.Outcome)
	assert.Equal(t, []string{"auto-merge #7 SQUASH"}, r.pulls.writes())
}

func TestAMergeItFollowsNoAgentChangeAPersonMustReview(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)

	r.steps.changes = changesAt{ghHeadA: {Complete: true, Groups: []sensitive.Group{{Label: "Workflow files"}}}}

	got, err := r.steps.Merge(t.Context(), r.mergeOf(false, catalog.MergeDefault))
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome)
	assert.Equal(t, "Its agent's commits changed workflow files, so it isn't merged: a person reviews those.", got.Words)

	r.steps.changes = changesAt{ghHeadA: {Complete: true, Groups: []sensitive.Group{
		{Label: "Workflow files"}, {Label: "Agent files"}, {Label: "Tooling files"},
	}}}

	got, err = r.steps.Merge(t.Context(), r.mergeOf(false, catalog.MergeDefault))
	require.NoError(t, err)
	assert.Equal(t, "Its agent's commits changed workflow files, agent files, and tooling files, so it isn't merged: "+
		"a person reviews those.", got.Words)

	r.steps.changes = unreadChanges{}

	_, err = r.steps.Merge(t.Context(), r.mergeOf(false, catalog.MergeDefault))
	require.ErrorIs(t, err, errChangesUnread, "what it changed, unread, merges nothing")

	assert.Empty(t, r.pulls.writes(), "no merge went")
}

func TestAMergeItThatCantReadThePullRequestSendsNothing(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)
	r.pulls.failReads(errDropped)

	got, err := r.steps.Merge(t.Context(), r.mergeOf(false, catalog.MergeDefault))
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubUnread, got.Outcome)

	posted, err := r.steps.PostReview(t.Context(), r.reviewOf(autopilot.EventComment, "Looks fine."))
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubUnread, posted.Outcome)

	assert.Empty(t, r.pulls.writes())
}

func TestTheAgentsReviewIsPostedWithTheLineCommentsItsDiffShows(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)

	got, err := r.steps.PostReview(t.Context(), r.reviewOf(autopilot.EventApprove, "Looks right.",
		autopilot.ReviewLine{Path: "cache.go", Line: 3, Body: "Nice."},
		autopilot.ReviewLine{Path: "cache.go", Line: 40, Body: "Off the diff."},
		autopilot.ReviewLine{Path: "missing.go", Line: 1, Body: "Not in it."},
	))
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubSentIt, got.Outcome)
	assert.True(t, strings.HasSuffix(got.Words, " 2 line comments left out: the diff doesn't show those lines."),
		"it counts the comments it left out: %q", got.Words)

	require.Len(t, r.pulls.reviews, 1)
	sent := r.pulls.reviews[0]
	assert.Equal(t, github.ReviewApprove, sent.Event)
	assert.Equal(t, ghHeadA, sent.CommitID, "at the commit the agent read")
	assert.Equal(t, "Looks right.", sent.Body)
	assert.Equal(t, []github.LineComment{{Path: "cache.go", Line: 3, Side: "RIGHT", Body: "Nice."}}, sent.Comments)
}

func TestTheAgentsReviewIsNotPostedWhereItCantGo(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)
	ctx := t.Context()

	_, err := r.steps.PostReview(ctx, r.reviewOf("dismiss", "No."))
	require.ErrorIs(t, err, errNoReviewEvent)
	assert.Empty(t, r.pulls.asked(), "a review this build doesn't know asks GitHub nothing")

	got, err := r.steps.PostReview(ctx, r.reviewOf(autopilot.EventRequestChanges, " "))
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome, "a request for changes with nothing to say")
	assert.Equal(t, "Write something, or add a line comment, first.", got.Words)

	// GitHub's head moved past the commit the agent read between the sheet's read and the
	// approval.
	r.pulls.mu.Lock()
	r.pulls.states[ghNode(ghPullNumber)] = &github.MergeState{
		Found: true, Status: github.MergeStateClean, Head: ghHeadB, State: github.StateOpen,
	}
	r.pulls.mu.Unlock()

	got, err = r.steps.PostReview(ctx, r.reviewOf(autopilot.EventApprove, "Looks right."))
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome)
	assert.Equal(t, "It was pushed to after the agent read it, so its review isn't posted.", got.Words)

	r.later()
	r.pulls.setRead(ghPullNumber, ghPullRead(ghPullNumber, ghAuthor, "User", ghHeadB, github.MergeStateClean))

	got, err = r.steps.PostReview(ctx, r.reviewOf(autopilot.EventComment, "Looks fine."))
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome, "the sheet's read at another head posts nothing")

	assert.Empty(t, r.pulls.writes())
}

func TestTheAgentsReviewWaitsOutTheWriteGateAndFailsOnWhatMayHaveGone(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)

	r.pulls.failWrites(paced())

	got, err := r.steps.PostReview(t.Context(), r.reviewOf(autopilot.EventComment, "Looks fine."))
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubHeld, got.Outcome)

	r.pulls.failWrites(errDropped)

	_, err = r.steps.PostReview(t.Context(), r.reviewOf(autopilot.EventComment, "Looks fine."))
	require.ErrorIs(t, err, errDropped)
}
