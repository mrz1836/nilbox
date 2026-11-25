package inbox

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// closingPR is the pull request a closeHarness issue names, in example-repo, its repository
// spelled as given.
func closingPR(repoName string, number int) github.ClosingPull {
	return github.ClosingPull{Repo: repoName, Number: number, AuthorLogin: octoUser, AuthorAssociation: "OWNER"}
}

// closeHarness refreshes an inbox of the pull requests #1 and #2 in example-repo, asking
// for the user's review, and the issues assigned to the user that name them: #6 names #1,
// #7 names #2, #8 in other-repo names #1 (its repository spelled in another case), and #9
// names none. It returns the harness, a MergeFollower over it, and each item's ID by number.
func closeHarness(t *testing.T) (*harness, *MergeFollower, map[int]int64) {
	t.Helper()

	h := newHarness(t)
	h.client.inbox = fetched(
		[]github.Item{pr(1, "OWNER", false), pr(2, "OWNER", false)}, nil,
		[]github.Item{
			issue(exampleRepo, 6, "OWNER"), issue(exampleRepo, 7, "OWNER"), issue("other-repo", 8, "OWNER"),
			issue(exampleRepo, 9, "OWNER"),
		},
	)
	h.client.details = map[string]github.ItemDetails{
		nodeID(1): prDetails(), nodeID(2): prDetails(),
		nodeID(6): {ClosingPulls: []github.ClosingPull{closingPR("acme-org/example-repo", 1)}},
		nodeID(7): {ClosingPulls: []github.ClosingPull{closingPR("acme-org/example-repo", 2)}},
		nodeID(8): {ClosingPulls: []github.ClosingPull{closingPR("Acme-Org/Example-Repo", 1)}},
		nodeID(9): {},
	}

	snap := h.refresh(t)

	ids := map[int]int64{}
	for _, n := range []int{1, 2} {
		ids[n] = itemByNumber(t, snap.ReviewRequested, n).ID
	}

	for _, n := range []int{6, 7, 8, 9} {
		ids[n] = itemByNumber(t, snap.Yours, n).ID
	}

	return h, NewMergeFollower(h.svc, h.logs.Logger()), ids
}

// answerIssues has GitHub answer for each issue numbered in states with that state, in its
// repository as closeHarness lists it, still naming #1, merged now, among its closing pull
// requests, as GitHub lists a merged one.
func (h *harness) answerIssues(states map[int]string) {
	h.client.items = map[string]*github.ItemUpdate{}

	for n, state := range states {
		name := exampleRepo
		if n == 8 {
			name = "other-repo"
		}

		it := issue(name, n, "OWNER")
		it.State, it.UpdatedAt = state, h.clock.Now()

		if state == github.StateClosed {
			it.ClosedAt = h.clock.Now()
		}

		merged := closingPR("acme-org/example-repo", 1)
		merged.Merged = true

		h.client.items[nodeID(n)] = &github.ItemUpdate{
			Found: true, Item: it, Details: github.ItemDetails{ClosingPulls: []github.ClosingPull{merged}},
			RateLimit: itemRate(),
		}
	}
}

// issueReads are the requests that read an issue of closeHarness, leaving out the merge
// round's reads of the pull requests.
func issueReads(h *harness) [][]string {
	issues := map[string]bool{nodeID(6): true, nodeID(7): true, nodeID(8): true, nodeID(9): true}

	var out [][]string

	for _, ids := range h.client.itemsIDs {
		if slices.ContainsFunc(ids, func(id string) bool { return issues[id] }) {
			out = append(out, ids)
		}
	}

	return out
}

// yoursNumbers are the numbers of the items in Yours, in any order.
func yoursNumbers(t *testing.T, h *harness) []int {
	t.Helper()

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)

	out := make([]int, 0, len(snap.Yours.Items))
	for _, it := range snap.Yours.Items {
		out = append(out, it.Number)
	}

	slices.Sort(out)

	return out
}

// closingNumbers are the numbers of the items in Yours a snapshot read now shows closing,
// in order.
func closingNumbers(t *testing.T, h *harness) []int {
	t.Helper()

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)

	var out []int

	for _, it := range snap.Yours.Items {
		if it.Closing {
			out = append(out, it.Number)
		}
	}

	slices.Sort(out)

	return out
}

// closesOwed reports whether a follow of a merge's issues is owed now.
func (f *MergeFollower) closesOwed() bool {
	_, ok := f.closes.next(f.now())

	return ok
}

func TestAMergeReadsTheIssuesItClosesAndTheyLeave(t *testing.T) {
	t.Parallel()

	h, f, ids := closeHarness(t)
	h.clock.Advance(time.Minute)
	mergeFirst(t, h, ids)

	f.pass(t.Context())
	assert.Empty(t, h.client.itemsIDs, "GitHub is given a few seconds to close them")

	h.clock.Advance(closeSettle)
	h.answerIssues(map[int]string{6: github.StateClosed, 8: github.StateClosed})
	calls := h.events.Count()

	f.pass(t.Context())

	require.Len(t, h.client.itemsIDs, 1, "one request for both, and none yet for the pull requests on main")
	assert.ElementsMatch(t, []string{nodeID(6), nodeID(8)}, h.client.itemsIDs[0],
		"the issues that name the merged pull request, in any repository, its name in any case")
	assert.Empty(t, h.client.itemIDs, "never one request each")
	assert.Equal(t, []github.Purpose{github.PurposeFollower}, h.client.purposes, "counted as the follower's")
	assert.Equal(t, calls+1, h.events.Count(), "the window hears of it once")
	assert.Equal(t, []int{7, 9}, yoursNumbers(t, h), "the closed issues left the inbox")
	assert.False(t, f.closesOwed(), "every issue it closes is closed")
}

func TestAMergeReadsItsIssuesAgainWhileGitHubHasThemOpen(t *testing.T) {
	t.Parallel()

	h, f, ids := closeHarness(t)
	mergeFirst(t, h, ids)

	h.clock.Advance(closeSettle)
	h.answerIssues(map[int]string{6: github.StateOpen, 8: github.StateClosed})
	f.pass(t.Context())
	assert.Equal(t, []int{6, 7, 9}, yoursNumbers(t, h))
	require.True(t, f.closesOwed(), "GitHub hasn't closed #6 yet")

	h.clock.Advance(closeRetry)
	h.answerIssues(map[int]string{6: github.StateClosed})
	f.pass(t.Context())

	reads := issueReads(h)
	require.Len(t, reads, 2)
	assert.Equal(t, []string{nodeID(6)}, reads[1], "only the one still open")
	assert.Equal(t, []int{7, 9}, yoursNumbers(t, h))
	assert.False(t, f.closesOwed())
}

func TestAMergeGivesUpOnAnIssueGitHubKeepsOpen(t *testing.T) {
	t.Parallel()

	h, f, ids := closeHarness(t)
	mergeFirst(t, h, ids)
	h.answerIssues(map[int]string{6: github.StateOpen, 8: github.StateOpen})

	h.clock.Advance(closeSettle)

	for range closeRounds {
		f.pass(t.Context())
		h.clock.Advance(closeRetry)
	}

	assert.Len(t, issueReads(h), closeRounds)
	assert.False(t, f.closesOwed(), "it gives up after its rounds")
	assert.Equal(t, []int{6, 7, 8, 9}, yoursNumbers(t, h), "an issue GitHub keeps open stays")

	h.clock.Advance(time.Hour)
	f.pass(t.Context())
	assert.Len(t, issueReads(h), closeRounds, "nor does the merge's own follow read an issue")
}

func TestAMergeShowsTheIssuesItClosesClosingAtOnce(t *testing.T) {
	t.Parallel()

	h, f, ids := closeHarness(t)
	assert.Empty(t, closingNumbers(t, h), "nothing merged yet")

	snap, err := NewActor(h.svc).Act(t.Context(), ids[1], acting(Acted{Merged: true}))
	require.NoError(t, err)

	var closing []int

	for _, it := range snap.Yours.Items {
		if it.Closing {
			closing = append(closing, it.Number)
		}
	}

	assert.ElementsMatch(t, []int{6, 8}, closing,
		"the snapshot the merge answers with shows the issues that name it closing, in any repository")
	assert.Empty(t, h.client.itemsIDs, "it asks GitHub nothing")

	h.clock.Advance(closeSettle)
	h.answerIssues(map[int]string{6: github.StateClosed, 8: github.StateClosed})
	calls := h.events.Count()

	f.pass(t.Context())

	assert.Equal(t, calls+1, h.events.Count(), "the round's own update takes them out, and nothing more is told")
	assert.Equal(t, []int{7, 9}, yoursNumbers(t, h))
	assert.Empty(t, closingNumbers(t, h))
}

func TestAnIssueGitHubKeepsOpenStopsShowingClosingWhenItsFollowEnds(t *testing.T) {
	t.Parallel()

	h, f, ids := closeHarness(t)
	mergeFirst(t, h, ids)
	h.answerIssues(map[int]string{6: github.StateOpen, 8: github.StateOpen})
	h.clock.Advance(closeSettle)

	for range closeRounds - 1 {
		f.pass(t.Context())
		assert.Equal(t, []int{6, 8}, closingNumbers(t, h), "closing while the follow reads them again")
		h.clock.Advance(closeRetry)
	}

	updates := inboxUpdates(h.events)

	f.pass(t.Context())

	assert.False(t, f.closesOwed())
	assert.Equal(t, updates+2, inboxUpdates(h.events), "the round's own update, then the follow's end")
	assert.Empty(t, closingNumbers(t, h), "an issue GitHub keeps open shows as it is")
	assert.Equal(t, []int{6, 7, 8, 9}, yoursNumbers(t, h))
}

func TestAFailedLastRoundStopsShowingTheIssuesClosing(t *testing.T) {
	t.Parallel()

	h, f, ids := closeHarness(t)
	mergeFirst(t, h, ids)
	h.client.itemErr = errFakeNetwork
	h.clock.Advance(closeSettle)

	for range closeRounds - 1 {
		f.pass(t.Context())
		h.clock.Advance(closeRetry)
	}

	updates := inboxUpdates(h.events)

	f.pass(t.Context())

	assert.False(t, f.closesOwed())
	assert.Equal(t, updates+1, inboxUpdates(h.events), "the window is told once the follow gives up, though it stored nothing")
	assert.Empty(t, closingNumbers(t, h))
}

func TestShowingIssuesClosingLeavesTheKeptSnapshotAlone(t *testing.T) {
	t.Parallel()

	h, f, _ := closeHarness(t)

	// A merge noted with no write, as a read that sees it merged again could, shows its
	// issues closing over the snapshot kept since the refresh.
	pull := item.Ref{Repo: repo.Name{Owner: acmeOrg, Repo: exampleRepo}, Number: 1}
	h.svc.noteMerged(nodeID(1), 1, pull, "main")
	assert.Equal(t, []int{6, 8}, closingNumbers(t, h))

	kept := h.svc.snapshots.load()
	require.NotNil(t, kept.snap, "read from the kept snapshot")

	for _, it := range kept.snap.Yours.Items {
		assert.False(t, it.Closing, "#%d is closing only in the copy returned", it.Number)
	}

	h.answerIssues(map[int]string{6: github.StateOpen, 8: github.StateOpen})
	h.clock.Advance(closeSettle)

	for range closeRounds {
		f.pass(t.Context())
		h.clock.Advance(closeRetry)
	}

	assert.Empty(t, closingNumbers(t, h))
}

func TestAMergeOfAnotherAccountShowsNothingClosing(t *testing.T) {
	t.Parallel()

	h, f, _ := closeHarness(t)
	pull := item.Ref{Repo: repo.Name{Owner: acmeOrg, Repo: exampleRepo}, Number: 1}
	h.svc.noteMerged(nodeID(1), 2, pull, "main")

	assert.Empty(t, closingNumbers(t, h), "the issues are another account's")

	updates := inboxUpdates(h.events)

	h.clock.Advance(closeSettle)
	f.pass(t.Context())

	assert.False(t, f.closesOwed())
	assert.Equal(t, updates, inboxUpdates(h.events), "a follow that showed nothing closing tells nothing")
}

func TestAMergeNoListedIssueNamesAsksGitHubNothing(t *testing.T) {
	t.Parallel()

	h, f, _ := closeHarness(t)
	h.svc.noteMerged(nodeID(3), 1, item.Ref{Repo: repo.Name{Owner: acmeOrg, Repo: exampleRepo}, Number: 3}, "main")

	h.clock.Advance(closeSettle)
	f.pass(t.Context())

	assert.Empty(t, h.client.itemsIDs)
	assert.False(t, f.closesOwed(), "the follow is over at once")
}

func TestAMergeLeavesAnIssueClosedBySomethingElseAlone(t *testing.T) {
	t.Parallel()

	h, f, ids := closeHarness(t)

	// #6 closed already, as a whole refresh found.
	h.answerIssues(map[int]string{6: github.StateClosed})
	_, err := h.svc.RefreshItem(t.Context(), ids[6])
	require.NoError(t, err)

	mergeFirst(t, h, ids)
	h.answerIssues(map[int]string{8: github.StateClosed})
	h.clock.Advance(closeSettle)
	f.pass(t.Context())

	assert.Equal(t, [][]string{{nodeID(8)}}, issueReads(h), "a closed issue is never read again")
}

func TestAMergeSeenAgainReadsItsIssuesOnce(t *testing.T) {
	t.Parallel()

	h, f, ids := closeHarness(t)
	mergeFirst(t, h, ids)

	// A read of the merged pull request sees it merged again.
	pull := item.Ref{Repo: repo.Name{Owner: acmeOrg, Repo: exampleRepo}, Number: 1}
	h.svc.noteMerged(nodeID(1), 1, pull, "main")

	h.answerIssues(map[int]string{6: github.StateClosed, 8: github.StateClosed})
	h.clock.Advance(closeSettle)
	f.pass(t.Context())
	h.clock.Advance(closeRetry)
	f.pass(t.Context())

	assert.Len(t, issueReads(h), 1)
}

func TestAFollowOfAMergesIssuesThatFailsIsTriedAgain(t *testing.T) {
	t.Parallel()

	h, f, ids := closeHarness(t)
	mergeFirst(t, h, ids)
	h.client.itemErr = errFakeNetwork

	h.clock.Advance(closeSettle)
	f.pass(t.Context())
	assert.Contains(t, h.logs.String(), closeJob+" failed")
	assert.Contains(t, h.logs.String(), "follow the issues acme-org/example-repo#1 closes")
	require.True(t, f.closesOwed())

	h.client.itemErr = nil
	h.answerIssues(map[int]string{6: github.StateClosed, 8: github.StateClosed})
	h.clock.Advance(closeRetry)
	f.pass(t.Context())

	assert.Equal(t, []int{7, 9}, yoursNumbers(t, h))
	assert.False(t, f.closesOwed())
}

func TestAFollowOfAMergesIssuesThatCantReadThemIsTriedAgain(t *testing.T) {
	t.Parallel()

	h, f, ids := closeHarness(t)
	mergeFirst(t, h, ids)

	// A row whose closing pull requests can't be read fails the round, asking GitHub nothing.
	h.exec(t, `UPDATE item_details SET closing_pulls = '[{"number": "one"}]' WHERE item_id = ?`, ids[7])

	h.clock.Advance(closeSettle)
	f.pass(t.Context())
	assert.Contains(t, h.logs.String(), "read the pull requests that close issue")
	assert.Empty(t, h.client.itemsIDs)
	assert.True(t, f.closesOwed())
}

func TestCloseQueueOwesAMergeAFollow(t *testing.T) {
	t.Parallel()

	wake := make(chan struct{}, 1)
	q := newCloseQueue(wake)
	at := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	name := repo.Name{Owner: acmeOrg, Repo: exampleRepo}

	q.add(1, item.Ref{Repo: repo.Name{}, Number: 1}, at)
	q.add(1, item.Ref{Repo: name, Number: 0}, at)

	_, owed := q.next(at)
	assert.False(t, owed, "a pull request without a repository or a number is owed nothing")
	assert.Empty(t, wake)

	q.add(2, item.Ref{Repo: name, Number: 4}, at)
	q.add(1, item.Ref{Repo: repo.Name{Owner: "Acme-Org", Repo: "Example-Repo"}, Number: 4}, at.Add(time.Second))
	q.add(1, item.Ref{Repo: name, Number: 3}, at)
	assert.Len(t, wake, 1, "the follower is woken")

	wait, owed := q.next(at)
	require.True(t, owed)
	assert.Equal(t, closeSettle, wait)

	got := q.due(at.Add(closeSettle))
	require.Len(t, got, 2, "the merge noted a second later isn't due yet")
	assert.Equal(t, 3, got[0].pull.Number, "by account, then pull request")
	assert.Equal(t, int64(2), got[1].accountID)

	q.done(got[0], true, at.Add(closeSettle))
	q.done(got[1], false, at.Add(closeSettle))
	q.done(closeFollow{accountID: 9, pull: item.Ref{Repo: name, Number: 1}}, true, at)

	due := q.due(at.Add(closeSettle + closeRetry))
	require.Len(t, due, 2)
	assert.Equal(t, 3, due[0].pull.Number, "read again while an issue is open")
	assert.Equal(t, "Acme-Org/Example-Repo#4", due[1].pull.String(), "named as noted")
}

func TestMergeFollowerReadsAMergesIssuesBeforeItsPullRequests(t *testing.T) {
	t.Parallel()

	// The bubble's clock moves only when every goroutine in it is blocked, so the waits
	// take no real time and the counts are exact.
	synctest.Test(t, func(t *testing.T) {
		var merges, closes atomic.Int32

		q := newMergeQueue()
		f := &MergeFollower{
			queue: q, closes: newCloseQueue(q.wake), now: time.Now, logger: (&testutil.Logs{}).Logger(),
			round: func(context.Context, mergeFollow) (bool, error) {
				merges.Add(1)

				return false, nil
			},
			closeRound: func(context.Context, closeFollow) (bool, error) {
				return closes.Add(1) == 1, nil // GitHub closes the issue after the first round
			},
		}

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			f.Run(ctx)
		}()

		synctest.Wait()

		pull := item.Ref{Repo: repo.Name{Owner: acmeOrg, Repo: exampleRepo}, Number: 1}
		now := time.Now()

		require.True(t, q.add("N1", 1, pull.Repo, "main", now))
		f.closes.add(1, pull, now)

		time.Sleep(closeSettle)
		synctest.Wait()
		assert.Equal(t, [2]int32{1, 0}, [2]int32{closes.Load(), merges.Load()}, "the issues are read first")

		time.Sleep(closeRetry)
		synctest.Wait()
		assert.Equal(t, [2]int32{2, 1}, [2]int32{closes.Load(), merges.Load()})

		time.Sleep(time.Hour)
		synctest.Wait()
		assert.Equal(t, [2]int32{2, 1}, [2]int32{closes.Load(), merges.Load()}, "both follows are over")

		cancel()
		<-done
	})
}

func TestMergeFollowerRecoversAPanickingIssuesRound(t *testing.T) {
	t.Parallel()

	logs := &testutil.Logs{}
	q := newMergeQueue()
	f := &MergeFollower{
		queue: q, closes: newCloseQueue(q.wake), now: time.Now, logger: logs.Logger(),
		round:      func(context.Context, mergeFollow) (bool, error) { return false, nil },
		closeRound: func(context.Context, closeFollow) (bool, error) { panic("boom") },
	}

	f.closes.add(1, item.Ref{Repo: repo.Name{Owner: acmeOrg, Repo: exampleRepo}, Number: 1}, time.Now().Add(-closeSettle))
	f.pass(t.Context())

	assert.Contains(t, logs.String(), closeJob+" panicked")
	assert.False(t, f.closesOwed(), "a panicking round isn't tried again")
}
