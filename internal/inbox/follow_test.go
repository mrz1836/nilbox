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
		follower := &ChecksFollower{follow: f.follow, interval: FollowInterval, logger: slog.New(slog.DiscardHandler)}

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
	assert.Equal(t, FollowInterval, NewChecksFollower(newHarness(t).svc, nil).interval)
}
