package review

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/heldruns"
)

// running is #7 with its one required check still running, GitHub's rollup counting it.
func running(head string) *github.PullReview {
	r := pullReview(head, github.MergeStateBlocked)
	r.Update.Details.Checks = github.Checks{State: "PENDING", Pending: 1}
	r.Checks = []github.Check{
		{Kind: github.CheckKindRun, Name: "test", State: "IN_PROGRESS", Outcome: github.CheckPending, Required: true},
	}

	return r
}

// readRunning reads #7 at testNow while its check runs, the inbox storing the card from the
// same answer, and returns the harness an hour later, the sheet having read nothing since.
func readRunning(t *testing.T) *harness {
	t.Helper()

	h := newHarness(t)
	h.github.review = running(headA)
	h.inbox.pull.ChecksState, h.inbox.pull.ChecksPending = "PENDING", 1
	h.inbox.pull.MergeState, h.inbox.pull.DetailsAt = github.MergeStateBlocked, testNow

	r := h.refreshed(t)
	require.Equal(t, ReadyBlocked, r.Readiness.State)
	require.Equal(t, CheckCounts{State: "PENDING", Pending: 1, At: "2026-11-09T17:00:00Z"}, r.Counts,
		"the read's own counts")
	require.False(t, r.Moved)

	h.clock.Advance(time.Hour)

	return h
}

// finished makes the inbox's card of #7 say what the checks follower read at after: the
// check passed, and GitHub would merge it.
func finished(h *harness, after time.Duration) {
	h.inbox.pull.ChecksState, h.inbox.pull.ChecksPassed, h.inbox.pull.ChecksPending = "SUCCESS", 1, 0
	h.inbox.pull.MergeState, h.inbox.pull.DetailsAt = github.MergeStateClean, testNow.Add(after)
}

func TestOpenShowsTheInboxsNewerChecksWithoutARequest(t *testing.T) {
	t.Parallel()

	h := readRunning(t)
	finished(h, 59*time.Minute)

	for name, open := range map[string]func() (Review, error){
		"open":    func() (Review, error) { return h.svc.Open(t.Context(), pullID) },
		"rebuild": func() (Review, error) { return h.svc.Rebuild(t.Context(), pullID) },
	} {
		r, err := open()
		require.NoError(t, err, name)

		assert.Equal(t, CheckCounts{State: "SUCCESS", Passed: 1, At: "2026-11-09T17:59:00Z", Newer: true}, r.Counts,
			"%s: the inbox's counts, newer than the sheet's read", name)
		assert.Equal(t, ReadyMerge, r.Readiness.State, "%s: the readiness follows the inbox's merge state", name)
		assert.Equal(t, ActionApproveMerge, r.Primary, name)
		require.Len(t, r.Checks, 1, name)
		assert.Equal(t, github.CheckPending, r.Checks[0].Outcome, "%s: the list stays as the sheet read it", name)
		assert.Equal(t, "2026-11-09T17:00:00Z", r.FetchedAt, "%s: and says when", name)
		assert.True(t, r.Moved, "%s: its checks moved since the sheet's read", name)
		assert.False(t, r.Stale, "%s: nothing pushed", name)
		assert.Zero(t, r.MoreChecks, name)
	}

	assert.Equal(t, 1, h.github.called(callReview), "neither asks GitHub anything")

	h.github.review = pullReview(headA, github.MergeStateClean)
	h.github.review.Update.Details.Checks = github.Checks{State: "SUCCESS", Passed: 1}
	r := h.refreshed(t)

	assert.Equal(t, 2, h.github.called(callReview), "the read brings the list up to them")
	assert.Equal(t, CheckCounts{State: "SUCCESS", Passed: 1, At: "2026-11-09T18:00:00Z"}, r.Counts)
	assert.Equal(t, github.CheckPassed, r.Checks[0].Outcome)
	assert.False(t, r.Moved)
}

func TestTheSheetsOwnReadWinsOverAnOlderCard(t *testing.T) {
	t.Parallel()

	h := readRunning(t)
	finished(h, -time.Minute)

	r, err := h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)

	assert.Equal(t, CheckCounts{State: "PENDING", Pending: 1, At: "2026-11-09T17:00:00Z"}, r.Counts,
		"the card read them before the sheet did")
	assert.Equal(t, ReadyBlocked, r.Readiness.State)
	assert.False(t, r.Moved)
}

func TestANewerCardThatMovedNothingAsksForNoRead(t *testing.T) {
	t.Parallel()

	h := readRunning(t)
	h.inbox.pull.DetailsAt = testNow.Add(30 * time.Minute)

	r, err := h.svc.Rebuild(t.Context(), pullID)
	require.NoError(t, err)

	assert.True(t, r.Counts.Newer)
	assert.Equal(t, "2026-11-09T17:30:00Z", r.Counts.At)
	assert.False(t, r.Moved, "the same checks, the same head")

	h.inbox.pull.HeadOID = headB

	r, err = h.svc.Rebuild(t.Context(), pullID)
	require.NoError(t, err)
	assert.True(t, r.Moved, "pushed to since")
	assert.True(t, r.Stale)

	h.inbox.pull.HeadOID, h.inbox.pull.ChecksAwaiting = headA, 1
	h.inbox.pull.HeldRuns = []heldruns.Run{{Workflow: "Deploy Preview"}}

	r, err = h.svc.Rebuild(t.Context(), pullID)
	require.NoError(t, err)
	assert.True(t, r.Moved, "a run held since")
}

func TestARecentReadIsReadAgainOnceTheCardsChecksMoved(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.review = running(headA)
	h.inbox.pull.ChecksState, h.inbox.pull.ChecksPending = "PENDING", 1

	h.refreshed(t)
	h.clock.Advance(5 * time.Second)

	h.refreshed(t)
	assert.Equal(t, 1, h.github.called(callReview), "read 5 seconds ago, and nothing moved")

	// The check ended, which moves no activity: only the card's counts say so.
	finished(h, 4*time.Second)

	h.refreshed(t)
	assert.Equal(t, 2, h.github.called(callReview), "a reopen reads what the card says moved")

	h.github.review = pullReview(headA, github.MergeStateClean)
	h.github.review.Update.Details.Checks = github.Checks{State: "SUCCESS", Passed: 1}
	h.refreshed(t)
	h.clock.Advance(time.Second)
	h.refreshed(t)
	assert.Equal(t, 3, h.github.called(callReview), "read once more, and then it matches")
	h.refreshed(t)
	assert.Equal(t, 3, h.github.called(callReview))
}

func TestACacheAnOlderBuildKeptCountsNothingAndIsReadAgain(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.refreshed(t)

	c := mustCache(t, h)
	c.context.Counts = nil
	require.NoError(t, h.svc.cache.save(t.Context(), pullID, c.context, nil, testNow))

	r, err := h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, CheckCounts{}, r.Counts, "the list alone counts its checks")
	assert.Zero(t, r.MoreChecks)

	h.refreshed(t)
	assert.Equal(t, 2, h.github.called(callReview), "the read is too old to say whether the checks moved")
}

func TestASheetNeverReadShowsTheCardsCounts(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.inbox.pull.ChecksState, h.inbox.pull.ChecksFailed, h.inbox.pull.ChecksPassed = "FAILURE", 1, 3
	h.inbox.pull.DetailsAt = testNow.Add(-time.Minute)

	r, err := h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)

	assert.Equal(t, CheckCounts{State: "FAILURE", Passed: 3, Failed: 1, At: "2026-11-09T16:59:00Z", Newer: true}, r.Counts)
	assert.Equal(t, []Check{}, r.Checks)
	assert.False(t, r.Moved, "the open's own read brings the rest")
	assert.Equal(t, ReadyUnknown, r.Readiness.State, "nothing merges before the sheet read it")
}

func TestTheSheetSaysHowManyChecksItsListLeavesOut(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.review.Update.Details.Checks = github.Checks{State: "SUCCESS", Passed: 130}
	h.inbox.pull.ChecksState, h.inbox.pull.ChecksPassed = "SUCCESS", 130

	r := h.refreshed(t)
	assert.Len(t, r.Checks, 1)
	assert.Equal(t, 129, r.MoreChecks, "the rollup counts every check, the list the first ones")
	assert.Equal(t, 130, r.Counts.Passed)
}

func TestTheInboxsNewerFacts(t *testing.T) {
	t.Parallel()

	failing := func(required bool) Check {
		return Check{Name: "lint", Outcome: github.CheckFailed, Required: required}
	}
	cache := func(checks ...Check) *cached {
		return &cached{
			head: headA, contextAt: testNow,
			context: pullContext{MergeState: github.MergeStateBlocked, Checks: checks, Counts: &contextCounts{}},
		}
	}
	newer := func(mergeState string, failed int) StoredPull {
		p := storedPull()
		p.DetailsAt, p.MergeState, p.ChecksFailed, p.ChecksState = testNow.Add(time.Minute), mergeState, failed, "FAILURE"

		return p
	}

	f := factsOf(newer(github.MergeStateBlocked, 1), cache(failing(true), failing(true)), Review{State: github.StateOpen})
	assert.Equal(t, 1, f.Failing)
	assert.Equal(t, 1, f.RequiredFailing, "no more required failing than fail now")

	f = factsOf(newer(github.MergeStateUnstable, 1), cache(failing(true)), Review{State: github.StateOpen})
	assert.Equal(t, github.MergeStateUnstable, f.MergeState)
	assert.Zero(t, f.RequiredFailing, "GitHub would merge it: nothing required fails")
	assert.Equal(t, "Ready to merge · 1 optional check failing", readiness(f).Sentence)

	f = factsOf(newer(github.MergeStateUnknown, 0), cache(failing(true)), Review{State: github.StateOpen})
	assert.Equal(t, github.MergeStateBlocked, f.MergeState, "GitHub was still working it out: the sheet's stands")
	assert.Zero(t, f.RequiredFailing)

	older := newer(github.MergeStateClean, 0)
	older.DetailsAt = testNow.Add(-time.Minute)
	f = factsOf(older, cache(failing(true)), Review{State: github.StateOpen})
	assert.Equal(t, github.MergeStateBlocked, f.MergeState, "the sheet read it after the card")
	assert.Equal(t, 1, f.RequiredFailing)
}

func TestTheInboxsNewerHeldRuns(t *testing.T) {
	t.Parallel()

	h := readRunning(t)
	h.ignored.set("Labeler")
	finished(h, 59*time.Minute)
	h.inbox.pull.MergeState, h.inbox.pull.ChecksAwaiting = github.MergeStateUnstable, 2
	h.inbox.pull.HeldRuns = []heldruns.Run{{Workflow: "Deploy Preview"}, {Workflow: "Labeler"}}

	r, err := h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)

	assert.Equal(t, "Ready to merge · 1 workflow run awaiting approval", r.Readiness.Sentence,
		"the run of an ignored workflow doesn't count")
	assert.Equal(t, []Unrun{}, r.Unrun, "the runs the sheet read stay as it read them")

	approve, ok := Decide(factsOf(h.inbox.pull, mustCache(t, h), r), PrefRepo).offer(ActionApproveRuns)
	require.True(t, ok)
	assert.False(t, approve.Enabled, "an approval names runs the sheet read")
	assert.Equal(t, "Wait for it to load from GitHub.", approve.Reason)
}
