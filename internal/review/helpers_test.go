package review

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestFactsCountTheChecks(t *testing.T) {
	t.Parallel()

	c := &cached{head: headA, context: pullContext{
		Checks: []Check{
			{Outcome: github.CheckFailed, Required: true},
			{Outcome: github.CheckFailed},
			{Outcome: github.CheckPending, Required: true},
			{Outcome: github.CheckPending},
			{Outcome: github.CheckPassed},
		},
		ViewerReview: "APPROVED", ViewerReviewAt: headA, PendingReviews: 1, CanAutoMerge: true,
	}}

	pull := storedPull()
	f := factsOf(pull, c, Review{BaseRef: "main", State: github.StateOpen})

	assert.Equal(t, 2, f.Failing)
	assert.Equal(t, 1, f.RequiredFailing)
	assert.Equal(t, 2, f.Pending)
	assert.Equal(t, 1, f.RequiredPending)
	assert.True(t, f.ApprovedAtHead)
	assert.True(t, f.PendingReview)
	assert.True(t, f.CanAutoMerge)

	pull.AutoMergeAllowed = false
	assert.False(t, factsOf(pull, c, Review{}).CanAutoMerge, "the repository doesn't allow auto-merge")
}

func TestFactsCountTheWorkflowRunsThatRanNoCheck(t *testing.T) {
	t.Parallel()

	c := &cached{head: headA, context: contextOf(&github.PullReview{Unrun: []github.UnrunSuite{
		{
			Name: "PR Management", Conclusion: github.SuiteAwaitsApproval, RunID: 106,
			URL: "https://github.com/acme-org/example-repo/actions/runs/106",
		},
		{Name: "Auto-merge Trigger", Conclusion: github.SuiteAwaitsApproval},
		{Name: "Auto-merge Trigger", Conclusion: "FAILURE", RunID: 107},
	}}, octoUser)}

	assert.Equal(t, []Unrun{
		{Name: "PR Management", Approval: true, RunID: 106, URL: "https://github.com/acme-org/example-repo/actions/runs/106"},
		{Name: "Auto-merge Trigger", Approval: true},
		{Name: "Auto-merge Trigger", RunID: 107},
	}, c.context.Unrun)

	f := factsOf(storedPull(), c, Review{BaseRef: "main", State: github.StateOpen})
	assert.Equal(t, 2, f.AwaitingApproval)
	assert.Equal(t, 1, f.Approvable, "a run held without its ID, as an older build cached it, can't be approved yet")
	assert.Equal(t, 1, f.NotStarted)
	assert.Zero(t, f.Failing, "none of them is a check")

	sheet := build(storedPull(), c, nil, PrefRepo)
	assert.Equal(t, c.context.Unrun, sheet.Unrun, "the sheet names them beneath its checks")
	assert.Equal(t, []Unrun{}, build(storedPull(), nil, nil, PrefRepo).Unrun)
}

func TestSmallWords(t *testing.T) {
	t.Parallel()

	for status, words := range map[string]string{
		github.MergeStateBlocked: "Branch protection", github.MergeStateBehind: "behind", github.MergeStateDirty: "conflicts",
		github.MergeStateUnstable: "workflow run is passing", github.MergeStateUnknown: "still working",
	} {
		assert.Contains(t, stateReason(status), words, status)
	}

	assert.Empty(t, waitingOn(nil))
	assert.Empty(t, upperFirst(""))
	assert.Equal(t, MethodSquash, PrefSquash.method())
	assert.Equal(t, MethodRebase, PrefRebase.method())
	assert.Empty(t, PrefRepo.method())
	assert.Empty(t, MethodPref("fast").method())
}

func TestFillReadsACacheWithNoStateAsOpen(t *testing.T) {
	t.Parallel()

	var r Review

	r.fill(&cached{head: headA}, nil)
	assert.Equal(t, github.StateOpen, r.State, "a cache that holds no state")

	r = Review{}
	r.fill(&cached{head: headA, context: pullContext{State: github.StateClosed}}, nil)
	assert.Equal(t, github.StateClosed, r.State, "the state the cache holds")
}

func TestFilesAreJudgedByEitherPath(t *testing.T) {
	t.Parallel()

	matcher := sensitive.NewMatcher(sensitive.DefaultConfig())

	moved := fileOf(cachedFile{path: "ci/old.yml", previousPath: ".github/workflows/ci.yml", status: "renamed"}, matcher)
	assert.Equal(t, "Workflows", moved.Category, "a workflow moved away")

	assert.Equal(t, PatchCapped, fileOf(cachedFile{path: "a.go", state: storedCapped}, matcher).PatchState)

	patch, state := patchOf(cachedFile{state: storedCapped}, nil)
	assert.Empty(t, patch)
	assert.Equal(t, PatchCapped, state)
}

func TestTheUpdateBranchOfferNeedsTheRight(t *testing.T) {
	t.Parallel()

	f := facts(github.MergeStateBehind)
	f.CanUpdateBranch = false

	o, ok := Decide(f, PrefRepo).offer(ActionUpdateBranch)
	require.True(t, ok)
	assert.False(t, o.Enabled)
	assert.Equal(t, "Your account can't update this branch.", o.Reason)
}

func TestTheCacheFailsWithItsDatabase(t *testing.T) {
	t.Parallel()

	c := newCacheStore(testutil.ClosedDB(t))
	ctx := t.Context()

	_, err := c.load(ctx, pullID, 0)
	require.Error(t, err)
	require.Error(t, c.save(ctx, pullID, pullContext{}, nil, testNow))
	require.Error(t, c.touch(ctx, pullID, testNow))
	require.Error(t, c.drop(ctx, pullID))

	_, err = c.patches(ctx, pullID, []int64{0})
	require.Error(t, err)
}

func TestActFailsWithItsSettings(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateClean)
	svc := h.service(fakeSettings{err: errFakeRead}, fakeReplies{})

	_, err := svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionApprove, Head: headA})
	require.ErrorIs(t, err, errFakeRead)

	h.inbox.pulled = errFakeRead

	_, err = h.svc.Act(t.Context(), ActRequest{ItemID: pullID, Action: ActionApprove, Head: headA})
	require.ErrorIs(t, err, errFakeRead)
}

func TestTheCacheKeepsEachHeldRunsIDAndPage(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	unrun := []Unrun{
		{Name: "CI", Approval: true, RunID: 30433642, URL: "https://github.com/acme-org/example-repo/actions/runs/30433642"},
		{Name: "Example CI"},
	}
	pc := pullContext{Head: headA, State: github.StateOpen, MergeState: github.MergeStateUnstable, Unrun: unrun}

	require.NoError(t, h.svc.cache.save(t.Context(), pullID, pc, pullDiff(headA), testNow))

	c, err := h.svc.cache.load(t.Context(), pullID, 0)
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, unrun, c.context.Unrun)

	// A context an older build cached has no run IDs: those runs read as not read yet.
	old, err := store.DecodeJSON[pullContext](`{"head":"` + headA + `","unrun":[{"name":"CI","approval":true}]}`)
	require.NoError(t, err)
	assert.Equal(t, []Unrun{{Name: "CI", Approval: true}}, old.Unrun)
}

func TestFactsSayWhenThePullRequestChangesWorkflows(t *testing.T) {
	t.Parallel()

	c := &cached{head: headA}

	f := factsOf(storedPull(), c, Review{Files: []File{
		{Path: ".github/workflows/ci.yml", Category: "Workflows", Level: sensitive.LevelDanger},
	}})
	assert.True(t, f.ChangesWorkflows)
	assert.Equal(t, []string{"Workflows"}, f.Sensitive)

	f = factsOf(storedPull(), c, Review{Files: []File{
		{Path: ".claude/settings.json", Category: "Agent config", Level: sensitive.LevelDanger},
		{Path: "main.go"},
	}})
	assert.False(t, f.ChangesWorkflows, "another sensitive category is no workflow")
}
