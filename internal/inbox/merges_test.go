package inbox

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// Values the merge tests use more than once.
const (
	mergeableClean = "MERGEABLE"
	exampleRepo    = "example-repo"
)

// onBase is a pull request's details, merging into base, GitHub saying mergeable.
func onBase(base, mergeable string) github.ItemDetails {
	d := prDetails()
	d.BaseRef, d.Mergeable = base, mergeable

	return d
}

// mergeHarness refreshes an inbox of #1 to #3 merging into main and #4 into release, in
// example-repo, and #5 into main in other-repo, and returns the harness, a MergeFollower
// over it, and each item's ID by number.
func mergeHarness(t *testing.T) (*harness, *MergeFollower, map[int]int64) {
	t.Helper()

	h := newHarness(t)
	items := []github.Item{
		pr(1, "OWNER", false), pr(2, "OWNER", false), pr(3, "OWNER", false), pr(4, "OWNER", false),
		githubItem("other-repo", 5, "OWNER", true, false),
	}
	h.client.inbox = fetched(items, nil, nil)
	h.client.details = map[string]github.ItemDetails{
		nodeID(1): onBase("main", mergeableClean), nodeID(2): onBase("main", mergeableClean),
		nodeID(3): onBase("main", mergeableClean), nodeID(4): onBase("release", mergeableClean),
		nodeID(5): onBase("main", mergeableClean),
	}

	snap := h.refresh(t)

	ids := map[int]int64{}
	for _, n := range []int{1, 2, 3, 4, 5} {
		ids[n] = itemByNumber(t, snap.ReviewRequested, n).ID
	}

	return h, NewMergeFollower(h.svc, h.logs.Logger()), ids
}

// answer has GitHub answer for each pull request numbered in mergeable with its value,
// still open on main.
func (h *harness) answer(mergeable map[int]string) {
	h.client.items = map[string]*github.ItemUpdate{}

	for n, m := range mergeable {
		h.client.items[nodeID(n)] = open(h, n, onBase("main", m))
	}
}

// mergeFirst has #1 merged through the Actor.
func mergeFirst(t *testing.T, h *harness, ids map[int]int64) {
	t.Helper()

	_, err := NewActor(h.svc).Act(t.Context(), ids[1], acting(Acted{Merged: true}))
	require.NoError(t, err)
}

// owed reports whether any follow is owed now.
func (f *MergeFollower) owed() bool {
	_, ok := f.queue.next(f.now())

	return ok
}

func TestAMergeReadsTheOtherPullRequestsOnItsBase(t *testing.T) {
	t.Parallel()

	h, f, ids := mergeHarness(t)
	h.clock.Advance(time.Minute)
	mergeFirst(t, h, ids)

	f.pass(t.Context())
	assert.Empty(t, h.client.itemsIDs, "GitHub is given a few seconds to start working them out")

	h.clock.Advance(mergeSettle)
	h.answer(map[int]string{2: "CONFLICTING", 3: mergeableClean})
	calls := h.events.Count()

	f.pass(t.Context())

	assert.Equal(t, [][]string{{nodeID(2), nodeID(3)}}, h.client.itemsIDs,
		"one request for the open pull requests on main, but another base's and another repository's")
	assert.Empty(t, h.client.itemIDs, "never one request each")
	assert.Equal(t, calls+1, h.events.Count(), "the window hears of it once")
	assert.Equal(t, []github.Purpose{github.PurposeFollower}, h.client.purposes, "counted as the follower's")

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "CONFLICTING", itemByNumber(t, snap.ReviewRequested, 2).Details.Mergeable)
	assert.False(t, f.owed(), "every answer was worked out")

	h.clock.Advance(time.Hour)
	f.pass(t.Context())
	assert.Len(t, h.client.itemsIDs, 1, "the follow is over")
}

func TestAMergeFollowReadsAgainWhileGitHubIsStillWorkingItOut(t *testing.T) {
	t.Parallel()

	h, f, ids := mergeHarness(t)
	mergeFirst(t, h, ids)
	h.answer(map[int]string{2: mergeableUnknown, 3: mergeableClean})

	h.clock.Advance(mergeSettle)
	f.pass(t.Context())
	require.True(t, f.owed())

	h.clock.Advance(mergeRetry)
	f.pass(t.Context())
	assert.Equal(t, []string{nodeID(2)}, h.client.itemsIDs[1], "only the one GitHub was still working out")

	h.clock.Advance(mergeRetry)
	f.pass(t.Context())
	assert.Len(t, h.client.itemsIDs, mergeRounds)
	assert.False(t, f.owed(), "it gives up after its rounds")

	h.clock.Advance(time.Hour)
	f.pass(t.Context())
	assert.Len(t, h.client.itemsIDs, mergeRounds)
}

func TestAMergeFollowLeavesOutWhatTheUserSetAside(t *testing.T) {
	t.Parallel()

	h, f, ids := mergeHarness(t)

	_, err := h.svc.Dismiss(t.Context(), ids[3])
	require.NoError(t, err)

	mergeFirst(t, h, ids)
	h.answer(map[int]string{2: mergeableClean})
	h.clock.Advance(mergeSettle)
	f.pass(t.Context())

	assert.Equal(t, [][]string{{nodeID(2)}}, h.client.itemsIDs, "a dismissed pull request is left alone")
}

func TestAMergeFollowLeavesAMutedRepositoryAlone(t *testing.T) {
	t.Parallel()

	h, f, ids := mergeHarness(t)
	setRules(t, h, []string{}, []string{"acme-org/example-repo"})

	mergeFirst(t, h, ids)
	h.clock.Advance(mergeSettle)
	f.pass(t.Context())

	assert.Empty(t, h.client.itemsIDs)
	assert.False(t, f.owed())
}

func TestAMergeFollowLeavesAloneWhatWasReadSince(t *testing.T) {
	t.Parallel()

	h, f, ids := mergeHarness(t)
	mergeFirst(t, h, ids)

	// A whole refresh reads them after GitHub started working them out.
	h.clock.Advance(mergeSettle + time.Second)
	h.client.inbox = fetched([]github.Item{pr(2, "OWNER", false), pr(3, "OWNER", false)}, nil, nil)
	h.refresh(t)

	f.pass(t.Context())
	assert.Empty(t, h.client.itemsIDs, "the refresh already read them")
}

func TestAMergeARefreshOfTheItemSeesIsFollowed(t *testing.T) {
	t.Parallel()

	h, f, ids := mergeHarness(t)

	merged := pr(1, "OWNER", false)
	merged.State = github.StateMerged
	h.client.items = map[string]*github.ItemUpdate{
		nodeID(1): {Found: true, Item: merged, Details: onBase("main", mergeableClean), RateLimit: itemRate()},
	}

	_, err := h.svc.RefreshItem(t.Context(), ids[1])
	require.NoError(t, err)

	h.answer(map[int]string{2: mergeableClean, 3: mergeableClean})
	h.clock.Advance(mergeSettle)
	f.pass(t.Context())

	assert.Equal(t, [][]string{{nodeID(2), nodeID(3)}}, h.client.itemsIDs)
}

func TestAMergeAWholeRefreshSeesIsFollowed(t *testing.T) {
	t.Parallel()

	h, f, _ := mergeHarness(t)

	// GitHub's index still lists #1, though it merged.
	merged := pr(1, "OWNER", false)
	merged.State = github.StateMerged
	h.client.inbox = fetched([]github.Item{merged, pr(2, "OWNER", false), pr(3, "OWNER", false)}, nil, nil)
	h.refresh(t)

	h.answer(map[int]string{2: mergeableClean, 3: mergeableClean})
	h.clock.Advance(mergeSettle)
	f.pass(t.Context())

	assert.Equal(t, [][]string{{nodeID(2), nodeID(3)}}, h.client.itemsIDs)

	// The next refresh, with the index still trailing, starts no other follow.
	h.refresh(t)
	assert.False(t, f.owed())
}

func TestTwoMergesInARepositoryShareTheirRound(t *testing.T) {
	t.Parallel()

	h, f, ids := mergeHarness(t)
	mergeFirst(t, h, ids)

	h.clock.Advance(mergeSettle / 2)

	_, err := NewActor(h.svc).Act(t.Context(), ids[2], acting(Acted{Merged: true}))
	require.NoError(t, err)

	h.clock.Advance(mergeSettle / 2)
	f.pass(t.Context())
	assert.Empty(t, h.client.itemsIDs, "the round waits for the second merge to settle too")

	h.answer(map[int]string{3: "CONFLICTING"})
	h.clock.Advance(mergeSettle / 2)
	f.pass(t.Context())

	assert.Equal(t, [][]string{{nodeID(3)}}, h.client.itemsIDs, "one round for both merges")
}

func TestAMergeIntoAnUnknownBaseReadsEveryBase(t *testing.T) {
	t.Parallel()

	h, f, _ := mergeHarness(t)
	h.svc.noteMerged(nodeID(9), 1, repo.Name{Owner: acmeOrg, Repo: exampleRepo}, "")
	h.answer(map[int]string{1: mergeableClean, 2: mergeableClean, 3: mergeableClean, 4: mergeableClean})

	h.clock.Advance(mergeSettle)
	f.pass(t.Context())

	require.Len(t, h.client.itemsIDs, 1)
	assert.ElementsMatch(t, []string{nodeID(1), nodeID(2), nodeID(3), nodeID(4)}, h.client.itemsIDs[0])
}

func TestAMergeFollowThatFailsIsTriedAgain(t *testing.T) {
	t.Parallel()

	h, f, ids := mergeHarness(t)
	mergeFirst(t, h, ids)
	h.client.itemErr = errFakeNetwork

	h.clock.Advance(mergeSettle)
	f.pass(t.Context())
	assert.Contains(t, h.logs.String(), mergeJob+" failed")
	require.True(t, f.owed())

	h.client.itemErr = nil
	h.answer(map[int]string{2: mergeableClean, 3: mergeableClean})
	h.clock.Advance(mergeRetry)
	f.pass(t.Context())

	assert.Len(t, h.client.itemsIDs, 2)
	assert.False(t, f.owed())
}

func TestMergeQueueNotesAMergeOnce(t *testing.T) {
	t.Parallel()

	q := newMergeQueue()
	at := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	name := repo.Name{Owner: acmeOrg, Repo: exampleRepo}

	assert.True(t, q.add("N1", 1, name, "main", at))
	assert.False(t, q.add("N1", 1, name, "main", at.Add(time.Minute)), "the same merge seen again")
	assert.True(t, q.add("N1", 1, name, "main", at.Add(mergeRecent)), "forgotten after a while")
	assert.False(t, q.add("N2", 1, repo.Name{}, "main", at), "no repository")

	got := q.due(at.Add(mergeRecent + mergeSettle))
	require.Len(t, got, 1)
	assert.Equal(t, []string{"main"}, got[0].bases)
	assert.False(t, got[0].anyBase)
}

func TestMergeQueueKeysARepositoryIgnoringCase(t *testing.T) {
	t.Parallel()

	q := newMergeQueue()
	at := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)

	q.add("N1", 1, repo.Name{Owner: "Acme-Org", Repo: "Example-Repo"}, "main", at)
	q.add("N2", 1, repo.Name{Owner: acmeOrg, Repo: exampleRepo}, "release", at)
	q.add("N3", 2, repo.Name{Owner: acmeOrg, Repo: exampleRepo}, "", at)

	got := q.due(at.Add(mergeSettle))
	require.Len(t, got, 2, "one follow per repository and account")
	assert.Equal(t, []string{"main", "release"}, got[0].bases)
	assert.Equal(t, "Acme-Org/Example-Repo", got[0].repo.String(), "named as first noted")
	assert.True(t, got[1].anyBase)
}

func TestMergeQueueKeepsAMergeNotedDuringItsRound(t *testing.T) {
	t.Parallel()

	q := newMergeQueue()
	at := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	name := repo.Name{Owner: acmeOrg, Repo: exampleRepo}

	q.add("N1", 1, name, "main", at)
	round := q.due(at.Add(mergeSettle))
	require.Len(t, round, 1)

	q.add("N2", 1, name, "main", at.Add(mergeSettle))
	q.done(round[0], false, at.Add(mergeSettle))

	wait, ok := q.next(at.Add(mergeSettle))
	require.True(t, ok, "the second merge keeps its round")
	assert.Equal(t, mergeSettle, wait)
}

func TestMergeFollowerRunsEachFollowsRounds(t *testing.T) {
	t.Parallel()

	// The bubble's clock moves only when every goroutine in it is blocked, so the waits
	// take no real time and the counts are exact.
	synctest.Test(t, func(t *testing.T) {
		var rounds atomic.Int32

		q := newMergeQueue()
		f := &MergeFollower{
			queue: q, now: time.Now, logger: (&testutil.Logs{}).Logger(),
			round: func(context.Context, mergeFollow) (bool, error) {
				rounds.Add(1)

				return true, nil // GitHub is always still working it out
			},
		}

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			f.Run(ctx)
		}()

		synctest.Wait()
		q.add("N1", 1, repo.Name{Owner: acmeOrg, Repo: exampleRepo}, "main", time.Now())

		synctest.Wait()
		assert.Equal(t, int32(0), rounds.Load(), "nothing reads before the merge settles")

		time.Sleep(mergeSettle)
		synctest.Wait()
		assert.Equal(t, int32(1), rounds.Load())

		time.Sleep(mergeRetry)
		synctest.Wait()
		assert.Equal(t, int32(2), rounds.Load())

		time.Sleep(mergeRetry)
		synctest.Wait()
		assert.Equal(t, int32(mergeRounds), rounds.Load())

		time.Sleep(time.Hour)
		synctest.Wait()
		assert.Equal(t, int32(mergeRounds), rounds.Load(), "it gives up after its rounds")

		cancel()
		<-done
	})
}

func TestMergeFollowerRecoversAPanickingRound(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var rounds atomic.Int32

		logs := &testutil.Logs{}
		q := newMergeQueue()
		f := &MergeFollower{
			queue: q, now: time.Now, logger: logs.Logger(),
			round: func(context.Context, mergeFollow) (bool, error) {
				if rounds.Add(1) == 1 {
					panic("boom")
				}

				return false, nil
			},
		}

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			f.Run(ctx)
		}()

		q.add("N1", 1, repo.Name{Owner: acmeOrg, Repo: exampleRepo}, "main", time.Now())
		time.Sleep(mergeSettle)
		synctest.Wait()
		assert.Equal(t, int32(1), rounds.Load())
		assert.Contains(t, logs.String(), mergeJob+" panicked")

		q.add("N2", 1, repo.Name{Owner: acmeOrg, Repo: exampleRepo}, "main", time.Now())
		time.Sleep(mergeSettle)
		synctest.Wait()
		assert.Equal(t, int32(2), rounds.Load(), "it keeps going")

		cancel()
		<-done
	})
}
