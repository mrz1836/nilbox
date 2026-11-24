package inbox

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
)

// withChecks is a pull request's details with its checks in state.
func withChecks(state string) github.ItemDetails {
	d := prDetails()
	d.Checks = github.Checks{State: state, Passed: 3, Pending: 2}

	if state == "SUCCESS" {
		d.Checks = github.Checks{State: state, Passed: 5}
	}

	return d
}

// followHarness refreshes an inbox of #1 with its checks running, #2 with them done,
// #3 with them expected, and #4 with them running too, and returns the harness and each
// item's ID by number.
func followHarness(t *testing.T) (*harness, map[int]int64) {
	t.Helper()

	h := newHarness(t)
	items := []github.Item{pr(1, "OWNER", false), pr(2, "OWNER", false), pr(3, "OWNER", false), pr(4, "OWNER", false)}
	h.client.inbox = fetched(items, nil, nil)
	h.client.details = map[string]github.ItemDetails{
		nodeID(1): withChecks("PENDING"), nodeID(2): withChecks("SUCCESS"), nodeID(3): withChecks("EXPECTED"),
		nodeID(4): withChecks("PENDING"),
	}

	snap := h.refresh(t)

	ids := map[int]int64{}
	for _, n := range []int{1, 2, 3, 4} {
		ids[n] = itemByNumber(t, snap.ReviewRequested, n).ID
	}

	return h, ids
}

// open is pull request number, still open, with details d, as GitHub answers for it now.
func open(h *harness, number int, d github.ItemDetails) *github.ItemUpdate {
	it := pr(number, "OWNER", false)
	it.State = github.StateOpen
	it.UpdatedAt = h.clock.Now()

	return &github.ItemUpdate{Found: true, Item: it, Details: d, RateLimit: itemRate()}
}

func TestRefreshRunningFollowsOnlyThePullRequestsWhoseChecksRun(t *testing.T) {
	t.Parallel()

	h, ids := followHarness(t)

	require.NoError(t, h.svc.refreshRunning(t.Context()))
	assert.Empty(t, h.client.itemsIDs, "a whole refresh just read them all")

	_, err := h.svc.Dismiss(t.Context(), ids[4])
	require.NoError(t, err)

	h.clock.Advance(FollowInterval)

	h.client.items = map[string]*github.ItemUpdate{
		nodeID(1): open(h, 1, withChecks("SUCCESS")),
		nodeID(3): open(h, 3, withChecks("PENDING")),
	}
	calls := h.events.Count()

	require.NoError(t, h.svc.refreshRunning(t.Context()))

	assert.Equal(t, [][]string{{nodeID(1), nodeID(3)}}, h.client.itemsIDs,
		"one request for every pull request whose checks run, but the dismissed one")
	assert.Empty(t, h.client.itemIDs, "never one request each")
	assert.Equal(t, calls+1, h.events.Count(), "the window hears of it once")

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "SUCCESS", itemByNumber(t, snap.ReviewRequested, 1).Details.Checks.State)
	assert.Equal(t, "PENDING", itemByNumber(t, snap.ReviewRequested, 3).Details.Checks.State)

	h.clock.Advance(FollowInterval)
	require.NoError(t, h.svc.refreshRunning(t.Context()))
	assert.Equal(t, []string{nodeID(3)}, h.client.itemsIDs[1], "a pull request whose checks ended is left alone")

	h.client.items[nodeID(3)] = open(h, 3, withChecks("FAILURE"))
	h.clock.Advance(FollowInterval)
	require.NoError(t, h.svc.refreshRunning(t.Context()))

	h.clock.Advance(FollowInterval)
	require.NoError(t, h.svc.refreshRunning(t.Context()))
	assert.Len(t, h.client.itemsIDs, 3, "once every check ended, a round asks GitHub nothing")
}

// noChecks is a pull request's details while its head commit has no checks at all.
func noChecks() github.ItemDetails {
	d := prDetails()
	d.Checks = github.Checks{}

	return d
}

// uncheckedHarness refreshes an inbox of #1, opened a moment ago with no checks yet, #2
// with none and no activity for days, #3 opened a moment ago with its only run held for
// approval, and #4 opened a moment ago with its checks passed, and returns the harness and
// when the three were opened.
func uncheckedHarness(t *testing.T) (*harness, time.Time) {
	t.Helper()

	h := newHarness(t)
	opened := h.clock.Now().Add(-10 * time.Second)

	items := []github.Item{pr(1, "OWNER", false), pr(2, "OWNER", false), pr(3, "OWNER", false), pr(4, "OWNER", false)}
	for _, i := range []int{0, 2, 3} {
		items[i].CreatedAt, items[i].UpdatedAt = opened, opened
	}

	held := noChecks()
	held.Checks.Awaiting = 1

	h.client.inbox = fetched(items, nil, nil)
	h.client.details = map[string]github.ItemDetails{
		nodeID(1): noChecks(), nodeID(2): noChecks(), nodeID(3): held, nodeID(4): withChecks("SUCCESS"),
	}

	h.refresh(t)

	return h, opened
}

func TestRefreshRunningFollowsAPullRequestWhoseChecksHaveNotStarted(t *testing.T) {
	t.Parallel()

	h, _ := uncheckedHarness(t)

	require.NoError(t, h.svc.refreshRunning(t.Context()))
	assert.Empty(t, h.client.itemsIDs, "a whole refresh just read them all")

	h.clock.Advance(FollowInterval)
	h.client.items = map[string]*github.ItemUpdate{nodeID(1): open(h, 1, withChecks("PENDING"))}

	require.NoError(t, h.svc.refreshRunning(t.Context()))
	assert.Equal(t, [][]string{{nodeID(1)}}, h.client.itemsIDs,
		"only the one opened a moment ago with no checks yet: not the one quiet for days, the held one, or the passed one")

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "PENDING", itemByNumber(t, snap.ReviewRequested, 1).Details.Checks.State,
		"its card shows the checks GitHub queued since the refresh")

	h.clock.Advance(followUnchecked + time.Second)
	h.client.items[nodeID(1)] = open(h, 1, withChecks("SUCCESS"))

	require.NoError(t, h.svc.refreshRunning(t.Context()))
	assert.Equal(t, []string{nodeID(1)}, h.client.itemsIDs[1],
		"once its checks run, it is followed as any other, however long ago it was active")

	h.clock.Advance(FollowInterval)
	require.NoError(t, h.svc.refreshRunning(t.Context()))
	assert.Len(t, h.client.itemsIDs, 2, "once its checks ended, a round asks GitHub nothing")
}

func TestRefreshRunningLetsGoOfAPullRequestThatNeverHasChecks(t *testing.T) {
	t.Parallel()

	h, opened := uncheckedHarness(t)

	// GitHub answers as for a repository that runs no checks: nothing moved since it opened.
	quiet := pr(1, "OWNER", false)
	quiet.State = github.StateOpen
	quiet.CreatedAt, quiet.UpdatedAt = opened, opened
	h.client.items = map[string]*github.ItemUpdate{
		nodeID(1): {Found: true, Item: quiet, Details: noChecks(), RateLimit: itemRate()},
	}

	for round := 1; round <= 3; round++ {
		h.clock.Advance(FollowInterval)
		require.NoError(t, h.svc.refreshRunning(t.Context()))
		require.Len(t, h.client.itemsIDs, round, "read every round while it was active within followUnchecked")
		assert.LessOrEqual(t, h.clock.Now().Sub(opened), followUnchecked)
	}

	h.clock.Advance(FollowInterval)
	require.NoError(t, h.svc.refreshRunning(t.Context()))
	assert.Len(t, h.client.itemsIDs, 3, "past followUnchecked, one with no checks is left to the whole refresh")
}

func TestRefreshRunningLeavesAPullRequestGitHubDidNotAnswerFor(t *testing.T) {
	t.Parallel()

	h, _ := followHarness(t)
	h.clock.Advance(FollowInterval)
	h.client.items = map[string]*github.ItemUpdate{}
	calls := h.events.Count()

	require.NoError(t, h.svc.refreshRunning(t.Context()))

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Len(t, snap.ReviewRequested.Items, 4, "a null answer never takes an item out of the lists")
	assert.Equal(t, calls, h.events.Count(), "nothing was stored")
}

func TestRefreshRunningTakesAMergedPullRequestOutOfTheLists(t *testing.T) {
	t.Parallel()

	h, _ := followHarness(t)
	h.clock.Advance(FollowInterval)

	merged := pr(1, "OWNER", false)
	merged.State = github.StateMerged
	h.client.items = map[string]*github.ItemUpdate{
		nodeID(1): {Found: true, Item: merged, Details: withChecks("SUCCESS"), RateLimit: itemRate()},
	}

	require.NoError(t, h.svc.refreshRunning(t.Context()))

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Len(t, snap.ReviewRequested.Items, 3)
}

func TestRefreshRunningFailuresKeepTheStoredInbox(t *testing.T) {
	t.Parallel()

	h, _ := followHarness(t)
	h.clock.Advance(FollowInterval)
	h.client.itemErr = errFakeNetwork
	calls := h.events.Count()

	require.ErrorIs(t, h.svc.refreshRunning(t.Context()), errFakeNetwork)
	assert.Equal(t, calls, h.events.Count())
}

// countingFollower counts its rounds.
type countingFollower struct {
	count atomic.Int32
}

func (f *countingFollower) follow(context.Context) error {
	f.count.Add(1)

	return nil
}

func TestChecksFollowerFollowsEveryIntervalUntilCanceled(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		f := &countingFollower{}
		follower := newChecksFollower(f.follow, nil, slog.New(slog.DiscardHandler))

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			follower.Run(ctx)
		}()

		synctest.Wait()
		assert.Equal(t, int32(1), f.count.Load())

		time.Sleep(2*FollowInterval + time.Second)
		synctest.Wait()
		assert.Equal(t, int32(3), f.count.Load(), "every 45 seconds")

		cancel()
		<-done
	})

	assert.Equal(t, 45*time.Second, FollowInterval)
	assert.Equal(t, FollowInterval, NewChecksFollower(newHarness(t).svc, nil, nil).job.Interval(t.Context()))
}

func TestChecksFollowerOffFollowsNothingUntilOn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var on atomic.Bool

		f := &countingFollower{}
		follower := newChecksFollower(f.follow, func(context.Context) time.Duration {
			if on.Load() {
				return 30 * time.Second
			}

			return 0
		}, nil)

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			follower.Run(ctx)
		}()

		time.Sleep(time.Hour)
		synctest.Wait()
		assert.Zero(t, f.count.Load(), "off follows nothing, not even at once")

		on.Store(true)
		follower.Wake()
		synctest.Wait()
		assert.Equal(t, int32(1), f.count.Load())

		cancel()
		<-done
	})
}

func TestRefreshRunningTakesAReviewGitHubNoLongerAsksOutOfReviewRequested(t *testing.T) {
	t.Parallel()

	h, ids := followHarness(t)
	h.clock.Advance(FollowInterval)

	reviewed := withChecks("PENDING")
	reviewed.ReviewRequests = github.ReviewRequests{Users: []string{}, Teams: []string{}}
	still := withChecks("PENDING")
	still.ReviewRequests = github.ReviewRequests{Users: []string{octoUser}, Teams: []string{}}
	h.client.items = map[string]*github.ItemUpdate{nodeID(1): open(h, 1, reviewed), nodeID(3): open(h, 3, still)}

	require.NoError(t, h.svc.refreshRunning(t.Context()))

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.ElementsMatch(t, []int{2, 3, 4}, numbers(snap.ReviewRequested), "only the pull request GitHub no longer asks leaves")
	assert.Equal(t, "2026-01-10 12:00:45+00:00", h.reviewLeftAt(t, ids[1]))
	assert.Empty(t, h.reviewLeftAt(t, ids[3]))
}
