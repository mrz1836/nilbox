package ghthreads

import (
	"context"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

// prRef is pull request number in the fixture repository.
func prRef(number int) item.Ref {
	return item.Ref{Repo: repo.Name{Owner: owner, Repo: exampleRepo}, Number: number, Kind: item.KindPR}
}

func TestFollowUpsRunEveryFewMinutesThenStop(t *testing.T) {
	t.Parallel()

	n := nudges{wake: make(chan struct{}, 1)}
	at := time.Date(2026, 11, 17, 16, 0, 0, 0, time.UTC)

	_, ok := n.untilFollowUp(at)
	assert.False(t, ok, "nothing followed up")

	n.follow([]item.Ref{prRef(2), prRef(1)}, at)
	n.follow(nil, at)

	wait, ok := n.untilFollowUp(at)
	require.True(t, ok)
	assert.Equal(t, FollowUpEvery, wait)

	_, ok = n.takeFollowUp(at.Add(time.Minute))
	assert.False(t, ok, "not due yet")

	var runs []time.Time

	for now := at; now.Before(at.Add(2 * FollowUpFor)); now = now.Add(time.Minute) {
		if req, due := n.takeFollowUp(now); due {
			runs = append(runs, now)

			assert.True(t, req.nudged, "a nudged run, which lists once the mirror is stale")
			assert.Equal(t, []item.Ref{prRef(1), prRef(2)}, req.refs, "the threads about the items followed up")
			assert.False(t, req.decidesAll())
		}
	}

	assert.Equal(t, []time.Time{
		at.Add(2 * time.Minute), at.Add(4 * time.Minute), at.Add(6 * time.Minute), at.Add(8 * time.Minute),
	}, runs, "every FollowUpEvery, until FollowUpFor")

	_, ok = n.untilFollowUp(at.Add(2 * FollowUpFor))
	assert.False(t, ok, "over")
}

func TestAnItemFollowedUpAgainIsFollowedUpAnew(t *testing.T) {
	t.Parallel()

	n := nudges{wake: make(chan struct{}, 1)}
	at := time.Date(2026, 11, 17, 16, 0, 0, 0, time.UTC)

	n.follow([]item.Ref{prRef(1)}, at)
	n.follow([]item.Ref{prRef(1), prRef(3)}, at.Add(9*time.Minute))

	req, ok := n.takeFollowUp(at.Add(FollowUpFor))
	require.True(t, ok)
	assert.Equal(t, []item.Ref{prRef(1), prRef(3)}, req.refs)
}

func TestOnlyFinishesAreFollowedUp(t *testing.T) {
	t.Parallel()

	for kind, want := range map[OutcomeKind]bool{
		OutcomeMerged: true, OutcomeClosed: true, OutcomeSeenClosed: true, OutcomeUnlisted: true,
		OutcomeReviewed: false, OutcomeDismissed: false, OutcomeRestored: false, OutcomeUnassigned: false,
		OutcomeUnrequested: false, "later": false,
	} {
		assert.Equal(t, want, kind.finishes(), kind)
	}
}

func TestSyncerRunsTheFollowUpsBetweenItsPasses(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		p := newFakePasser()

		stop := runSyncer(t, newSyncer(p, nil, nil))
		defer stop()

		synctest.Wait()
		require.Len(t, p.ran(), 1, "the first pass")

		p.nudges.follow([]item.Ref{prRef(1)}, time.Now())

		time.Sleep(FollowUpEvery - time.Second)
		synctest.Wait()
		assert.Len(t, p.ran(), 1, "not before FollowUpEvery")

		time.Sleep(time.Second)
		synctest.Wait()

		runs := p.ran()
		require.Len(t, runs, 2, "a follow-up run at FollowUpEvery, not at the interval")
		assert.True(t, runs[1].req.nudged)
		assert.Equal(t, []item.Ref{prRef(1)}, runs[1].req.refs)

		time.Sleep(FollowUpFor)
		synctest.Wait()

		followUps := 0

		for _, r := range p.ran() {
			if r.req.nudged {
				followUps++
			}
		}

		assert.Equal(t, 4, followUps)
	})
}

// resurfacingClient is GitHub with one unread thread about pull request #1, merged at
// merged. A thread marked done leaves the listing until GitHub brings it back (comeBack),
// as it does with new activity, such as its own notice of the merge.
type resurfacingClient struct {
	*gatedClient

	merged time.Time

	mu      sync.Mutex
	updated time.Time
	listed  bool
	done    []time.Time
}

func (c *resurfacingClient) ListThreads(context.Context, github.Token, github.ThreadQuery) (*github.ThreadPage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	threads := []github.Thread{}

	if c.listed {
		name := repo.Name{Owner: owner, Repo: exampleRepo}
		threads = append(threads, github.Thread{
			ID: firstID, Unread: true, Reason: "review_requested", UpdatedAt: c.updated, SubjectType: "PullRequest",
			Title: "Bump the sync worker", Repo: name, Ref: prRef(1),
		})
	}

	return &github.ThreadPage{Threads: threads}, nil
}

func (c *resurfacingClient) FetchItemStates(
	_ context.Context, _ github.Token, refs []item.Ref,
) (map[string]github.ItemState, error) {
	out := make(map[string]github.ItemState, len(refs))
	for _, ref := range refs {
		out[ref.Key()] = github.ItemState{PullRequest: true, State: github.StateMerged, ClosedAt: c.merged}
	}

	return out, nil
}

func (c *resurfacingClient) MarkThreadDone(context.Context, github.Token, int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.done = append(c.done, time.Now())
	c.listed = false

	return nil
}

// comeBack has GitHub bring the thread back, updated at.
func (c *resurfacingClient) comeBack(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.updated, c.listed = at.UTC(), true
}

// marked returns when the thread was marked done.
func (c *resurfacingClient) marked() []time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return slices.Clone(c.done)
}

func TestAThreadGitHubBringsBackAfterAMergeIsClearedAgainSoon(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)

	synctest.Test(t, func(t *testing.T) {
		merged := time.Now()
		client := &resurfacingClient{gatedClient: newGatedClient(0), merged: merged.UTC(), updated: merged.UTC(), listed: true}
		rules := &fakeRules{}
		rules.change(func(r *threadrules.Rules) { r.Auto = true })

		svc := NewService(Config{
			Pool: db, Tokens: &fakeTokens{value: "test-token-not-real"}, Client: client, Rules: rules,
		})

		stop := runSyncer(t, NewSyncer(svc, nil, nil))
		defer stop()

		synctest.Wait()
		require.Len(t, client.marked(), 1, "the first pass clears the merged pull request's thread")

		// The inbox tells of the merge; a minute later, GitHub tells of it too, which brings
		// the thread back.
		NewSettler(svc).Settle(t.Context(), []Outcome{outcome(OutcomeSeenClosed, item.KindPR, 1, merged)})
		synctest.Wait()

		time.Sleep(time.Minute)
		client.comeBack(time.Now())

		time.Sleep(time.Minute)
		synctest.Wait()

		marked := client.marked()
		require.Len(t, marked, 2, "a follow-up run clears it again, at FollowUpEvery, not the pass's five minutes")
		assert.True(t, marked[1].Equal(merged.Add(FollowUpEvery)))
	})
}
