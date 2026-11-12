package review

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/sensitive"
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

func TestSmallWords(t *testing.T) {
	t.Parallel()

	for status, words := range map[string]string{
		github.MergeStateBlocked: "Branch protection", github.MergeStateBehind: "behind", github.MergeStateDirty: "conflicts",
		github.MergeStateUnstable: "failing", github.MergeStateUnknown: "still working",
	} {
		assert.Contains(t, stateReason(status), words, status)
	}

	assert.Empty(t, waitingOn(nil))
	assert.Empty(t, upperFirst(""))
	assert.True(t, canWrite("ADMIN"))
	assert.True(t, canWrite("MAINTAIN"))
	assert.False(t, canWrite("TRIAGE"))
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
