package review

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
)

// sentAll counts every request the fake GitHub was sent.
func (g *fakeGitHub) sentAll() int {
	g.mu.Lock()
	defer g.mu.Unlock()

	return len(g.order)
}

// offersApproval reports whether r offers to approve its held runs.
func offersApproval(r Review) bool {
	return slices.ContainsFunc(r.Offers, func(o Offer) bool { return o.Action == ActionApproveRuns })
}

// triggerSuite is the owner's case: a run a review started on a fork's pull request, which
// GitHub holds and approves only on its site.
func triggerSuite() github.UnrunSuite {
	return github.UnrunSuite{
		Name: "Auto-merge Trigger", Conclusion: github.SuiteAwaitsApproval, RunID: 102, OnGitHub: true,
		URL: "https://github.com/acme-org/example-repo/actions/runs/102",
	}
}

// previewSuite is a fork's own run, which the user can approve from the sheet.
func previewSuite() github.UnrunSuite {
	return github.UnrunSuite{
		Name: "Deploy Preview", Conclusion: github.SuiteAwaitsApproval, RunID: 101,
		URL: "https://github.com/acme-org/example-repo/actions/runs/101",
	}
}

func TestARunHeldOnGitHubLeavesThePullRequestReadyToMerge(t *testing.T) {
	t.Parallel()

	// Every check passed; GitHub calls the merge UNSTABLE for the one run it holds.
	h := newHarness(t)
	h.github.review = pullReview(headA, github.MergeStateUnstable)
	h.github.review.Unrun = []github.UnrunSuite{triggerSuite()}

	r := h.refreshed(t)

	assert.Equal(t, ReadyMerge, r.Readiness.State)
	assert.Equal(t, "Ready to merge", r.Readiness.Sentence,
		"no workflow run awaiting approval is said of a run held on GitHub")
	require.Len(t, r.Unrun, 1)
	assert.True(t, r.Unrun[0].Ignored, "named quietly beneath the checks")
	assert.False(t, offersApproval(r))

	i := slices.IndexFunc(r.Offers, func(o Offer) bool { return o.Action == ActionMerge })
	require.GreaterOrEqual(t, i, 0)
	assert.True(t, r.Offers[i].Enabled)
	assert.Contains(t, r.Offers[i].Warnings[0], "1 workflow run is waiting for approval on GitHub.",
		"the merge still says what GitHub counts as not passing")
}

func TestABlockedMergeKeepsTheRunHeldOnGitHubAsItsLikelyBlocker(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.review = pullReview(headA, github.MergeStateBlocked)
	h.github.review.Unrun = []github.UnrunSuite{triggerSuite()}

	r := h.refreshed(t)

	require.Len(t, r.Unrun, 1)
	assert.False(t, r.Unrun[0].Ignored, "while GitHub reports the merge blocked, the strip names it")
	assert.Equal(t, ReadyBlocked, r.Readiness.State)

	// Named, it never counts, blocked or not.
	h.ignored.set("Auto-merge Trigger")

	r, err := h.svc.Rebuild(t.Context(), pullID)
	require.NoError(t, err)
	assert.True(t, r.Unrun[0].Ignored)
}

func TestIgnoredWorkflowsRebuildTheSheetFromWhatItRead(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.review = pullReview(headA, github.MergeStateUnstable)
	h.github.review.Unrun = []github.UnrunSuite{previewSuite(), triggerSuite()}

	r := h.refreshed(t)

	assert.Equal(t, "Ready to merge · 1 workflow run awaiting approval", r.Readiness.Sentence,
		"the run the user can approve counts, the one held on GitHub doesn't")
	assert.True(t, offersApproval(r))

	sent := h.github.sentAll()

	// The user ignores Deploy Preview while the sheet is open.
	h.ignored.set("Deploy Preview")

	r, err := h.svc.Rebuild(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, sent, h.github.sentAll(), "a rebuild asks GitHub nothing")
	assert.Equal(t, ReadyMerge, r.Readiness.State)
	assert.Equal(t, "Ready to merge", r.Readiness.Sentence)
	assert.True(t, r.Unrun[0].Ignored)
	assert.Equal(t, "Deploy Preview", r.Unrun[0].Name)
	assert.False(t, offersApproval(r), "a run of an ignored workflow isn't offered to approve")

	_, err = h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionApproveRuns, Head: headA, Accept: true})
	require.ErrorIs(t, err, ErrNotOffered)
	assert.Zero(t, h.github.called(callApprove))

	// A rebuild keeps the visit the sheet opened.
	opened, err := h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)

	h.ignored.set()

	r, err = h.svc.Rebuild(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, opened.LastSeenAt, r.LastSeenAt)
	assert.False(t, r.Unrun[0].Ignored, "taken off the list, it counts again")
	assert.True(t, offersApproval(r))
}

func TestAnApprovalLeavesOutTheRunsOfIgnoredWorkflows(t *testing.T) {
	t.Parallel()

	h := heldRunsHarness(t, 101, 103)
	h.ignored.set("Workflow 103")

	res := h.act(t, ActRequest{Action: ActionApproveRuns, Accept: true})
	assert.Equal(t, OutcomeRunsApproved, res.Outcome)
	assert.Equal(t, []string{"acme-org/example-repo:101"}, h.github.approved, "the ignored workflow's run is never sent")
}

func TestARebuildOfAPullRequestNeverReadIsItsCard(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	r, err := h.svc.Rebuild(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, []Unrun{}, r.Unrun)
	assert.Zero(t, h.github.sentAll())
}

func TestTheIgnoredWorkflowsThatCantBeReadFailTheSheet(t *testing.T) {
	t.Parallel()

	h := heldRunsHarness(t, 101)
	svc := h.service(fakeSettings{err: errFakeRead}, fakeReplies{})

	_, err := svc.Rebuild(t.Context(), pullID)
	require.ErrorIs(t, err, errFakeRead)

	_, err = svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionApproveRuns, Head: headA, Accept: true})
	require.ErrorIs(t, err, errFakeRead)
}

func TestTheReadinessLeavesOutTheHeldRunsThatDontCount(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name  string
		facts Facts
		state ReadyState
		says  string
	}{
		{
			"only runs that don't count",
			Facts{AwaitingApproval: 2, OnGitHub: 1, Ignored: 2, IgnoredApprovable: 1},
			ReadyMerge, "Ready to merge",
		},
		{
			"a run that counts beside one that doesn't",
			Facts{AwaitingApproval: 2, OnGitHub: 1, Ignored: 1},
			ReadyUnstable, "Ready to merge · 1 workflow run awaiting approval",
		},
		{
			"an optional check failing beside a run that doesn't count",
			Facts{Failing: 1, AwaitingApproval: 1, OnGitHub: 1, Ignored: 1},
			ReadyUnstable, "Ready to merge · 1 optional check failing",
		},
		{"nothing the sheet read", Facts{}, ReadyUnstable, "Ready to merge · a status isn't passing"},
	} {
		c.facts.MergeState = github.MergeStateUnstable

		r := Decide(c.facts, PrefRepo).Readiness
		assert.Equal(t, c.state, r.State, c.name)
		assert.Equal(t, c.says, r.Sentence, c.name)
	}
}
