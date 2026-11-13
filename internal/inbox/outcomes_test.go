package inbox

import (
	"context"
	"net/http"
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
	"github.com/mrz1836/nilbox/internal/uierr"
)

// recordingSink records every call it is told, and the state of the context it was told
// with, and fails the test when it is told while the inbox holds its store lock or any
// item's lock. then, when set, runs after each call is recorded, as a sink that calls the
// inbox back would.
type recordingSink struct {
	t    *testing.T
	svc  *Service
	then func(ctx context.Context)

	mu      sync.Mutex
	calls   [][]Outcome
	ctxErrs []error
}

// Settle checks the inbox holds no lock, then records the call.
func (r *recordingSink) Settle(ctx context.Context, outcomes []Outcome) {
	if r.svc.order.mu.TryLock() {
		r.svc.order.mu.Unlock()
	} else {
		r.t.Error("the sink was told while the inbox held its store lock")
	}

	if held := heldItems(&r.svc.items); len(held) > 0 {
		r.t.Errorf("the sink was told while the inbox held the lock of items %v", held)
	}

	r.mu.Lock()
	r.calls = append(r.calls, slices.Clone(outcomes))
	r.ctxErrs = append(r.ctxErrs, ctx.Err())
	then := r.then
	r.mu.Unlock()

	if then != nil {
		then(ctx)
	}
}

// contextErrs returns the error each call's context held as it was told.
func (r *recordingSink) contextErrs() []error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.ctxErrs)
}

// told returns the calls recorded so far.
func (r *recordingSink) told() [][]Outcome {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.calls)
}

// all returns every outcome told so far, in order, whatever call told it.
func (r *recordingSink) all() []Outcome {
	var out []Outcome
	for _, call := range r.told() {
		out = append(out, call...)
	}

	return out
}

// heldItems tries each item's lock and returns the items whose lock is held, letting go
// of each it took at once.
func heldItems(l *itemLocks) []int64 {
	l.mu.Lock()
	defer l.mu.Unlock()

	var held []int64

	for id, il := range l.locks {
		select {
		case il.held <- struct{}{}:
			<-il.held
		default:
			held = append(held, id)
		}
	}

	return held
}

// telling replaces the harness's service with one over the same database and fakes that
// tells a recordingSink about outcomes, and returns the sink.
func (h *harness) telling(t *testing.T) *recordingSink {
	t.Helper()

	sink := &recordingSink{t: t}
	h.svc = NewService(Config{
		Pool: h.pool, Tokens: h.tokens, Client: h.client, Clock: h.clock, Events: h.events, Outcomes: sink,
		Logger: h.logs.Logger(),
	})
	sink.svc = h.svc

	return sink
}

// prRef names pull request number in acme-org/example-repo.
func prRef(number int) item.Ref {
	return item.Ref{Repo: repo.Name{Owner: acmeOrg, Repo: "example-repo"}, Number: number, Kind: item.KindPR}
}

// issueRef names issue number in acme-org's repository name.
func issueRef(name string, number int) item.Ref {
	return item.Ref{Repo: repo.Name{Owner: acmeOrg, Repo: name}, Number: number, Kind: item.KindIssue}
}

// told is the outcome of kind about ref, for the signed-in user, at.
func told(kind OutcomeKind, ref item.Ref, at time.Time) Outcome {
	return Outcome{Kind: kind, Login: octoUser, Ref: ref, At: at.UTC()}
}

// acting is an action that reports what it made happen.
func acting(acted Acted) func(context.Context, github.Token, PullTarget) (Acted, error) {
	return func(context.Context, github.Token, PullTarget) (Acted, error) {
		return acted, nil
	}
}

// badCredentials is GitHub refusing a token.
func badCredentials() error {
	return &github.StatusError{StatusCode: http.StatusUnauthorized, Message: "Bad credentials"}
}

func TestOutcomesMergedToldOnce(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	sink := h.telling(t)
	tries := 0

	_, err := NewActor(h.svc).Act(t.Context(), id, func(context.Context, github.Token, PullTarget) (Acted, error) {
		if tries++; tries == 1 {
			return Acted{}, badCredentials()
		}

		return Acted{Merged: true}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, tries)
	assert.Equal(t, [][]Outcome{{told(OutcomeMerged, prRef(1), h.clock.Now())}}, sink.told(),
		"one call, after the item's lock is let go, however many tries it took")
}

func TestOutcomesClosedPullToldOnce(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	sink := h.telling(t)

	// The text a close posts first is a COMMENT review; the close is what the user did.
	_, err := NewActor(h.svc).Act(t.Context(), id, acting(Acted{
		Reviewed: true, ReviewedAt: h.clock.Now().Add(-time.Second), ReviewEvent: github.ReviewComment, Closed: true,
	}))
	require.NoError(t, err)
	assert.Equal(t, [][]Outcome{{told(OutcomeClosed, prRef(1), h.clock.Now())}}, sink.told())
}

func TestOutcomesIssueOwnCloseToldOnce(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	sink := h.telling(t)

	_, err := NewActor(h.svc).ActIssue(t.Context(), ids[3], func(context.Context, github.Token, IssueTarget) (IssueActed, error) {
		return IssueActed{Closed: true, Own: true}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, [][]Outcome{{told(OutcomeClosed, issueRef("example-repo", 3), h.clock.Now())}}, sink.told())
}

func TestOutcomesIssueClosedFirstNotOwn(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	sink := h.telling(t)

	_, err := NewActor(h.svc).ActIssue(t.Context(), ids[3], func(context.Context, github.Token, IssueTarget) (IssueActed, error) {
		return IssueActed{Closed: true}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, [][]Outcome{{told(OutcomeSeenClosed, issueRef("example-repo", 3), h.clock.Now())}}, sink.told(),
		"a close someone made first is seen, never the user's own")
}

func TestOutcomesReviewedApproveAndRequestChanges(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	sink := h.telling(t)
	actor := NewActor(h.svc)
	approvedAt := time.Date(2026, 1, 10, 12, 5, 0, 0, time.UTC)

	_, err := actor.Act(t.Context(), id, acting(Acted{
		Reviewed: true, ReviewedAt: approvedAt, ReviewEvent: github.ReviewApprove,
	}))
	require.NoError(t, err)

	// GitHub gave no time for this one: it is told at the inbox's.
	_, err = actor.Act(t.Context(), id, acting(Acted{Reviewed: true, ReviewEvent: github.ReviewRequestChanges}))
	require.NoError(t, err)

	assert.Equal(t, [][]Outcome{
		{told(OutcomeReviewed, prRef(1), approvedAt)},
		{told(OutcomeReviewed, prRef(1), h.clock.Now())},
	}, sink.told())
}

func TestOutcomesCommentReviewNotTold(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	sink := h.telling(t)
	actor := NewActor(h.svc)

	_, err := actor.Act(t.Context(), id, acting(Acted{
		Reviewed: true, ReviewedAt: h.clock.Now(), ReviewEvent: github.ReviewComment,
	}))
	require.NoError(t, err)

	// A comment posted before a merge GitHub then refused is a comment all the same.
	_, err = actor.Act(t.Context(), id, func(context.Context, github.Token, PullTarget) (Acted, error) {
		return Acted{Reviewed: true, ReviewEvent: github.ReviewComment}, errMergeRefused
	})
	require.ErrorIs(t, err, errMergeRefused)

	assert.Empty(t, sink.told())
}

func TestOutcomesReviewWithMergeToldAsMergedOnly(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	sink := h.telling(t)
	tries := 0

	// The approval goes on the first try, and the merge on the second, with a fresh token.
	_, err := NewActor(h.svc).Act(t.Context(), id, func(context.Context, github.Token, PullTarget) (Acted, error) {
		approved := Acted{Reviewed: true, ReviewedAt: h.clock.Now(), ReviewEvent: github.ReviewApprove}
		if tries++; tries == 1 {
			return approved, badCredentials()
		}

		approved.Merged = true

		return approved, nil
	})
	require.NoError(t, err)
	assert.Equal(t, [][]Outcome{{told(OutcomeMerged, prRef(1), h.clock.Now())}}, sink.told())
}

func TestOutcomesDismissAndRestore(t *testing.T) {
	t.Parallel()

	h, id, other := triageHarness(t)
	sink := h.telling(t)
	ctx := t.Context()

	_, err := h.svc.Dismiss(ctx, id)
	require.NoError(t, err)

	// A second click dismisses what is dismissed already: nothing changed, nothing told.
	_, err = h.svc.Dismiss(ctx, id)
	require.NoError(t, err)

	h.clock.Advance(time.Minute)
	_, err = h.svc.Restore(ctx, id)
	require.NoError(t, err)

	// Restoring what is neither dismissed nor snoozed does nothing, and tells nothing.
	_, err = h.svc.Restore(ctx, id)
	require.NoError(t, err)

	// A snooze and a pin tell nothing; bringing a snoozed item back is a restore.
	_, err = h.svc.Snooze(ctx, other, 1)
	require.NoError(t, err)
	_, err = h.svc.Pin(ctx, other)
	require.NoError(t, err)
	_, err = h.svc.Restore(ctx, other)
	require.NoError(t, err)

	// Dismissing a snoozed item is a change: it was set aside, but not dismissed.
	_, err = h.svc.Snooze(ctx, other, 1)
	require.NoError(t, err)
	_, err = h.svc.Dismiss(ctx, other)
	require.NoError(t, err)

	_, err = h.svc.Dismiss(ctx, 9999)
	require.ErrorIs(t, err, item.ErrNotFound)

	start := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, [][]Outcome{
		{told(OutcomeDismissed, prRef(1), start)},
		{told(OutcomeRestored, prRef(1), start.Add(time.Minute))},
		{told(OutcomeRestored, prRef(2), start.Add(time.Minute))},
		{told(OutcomeDismissed, prRef(2), start.Add(time.Minute))},
	}, sink.told())
}

func TestOutcomesUnassignedOnceGitHubAnswered(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	sink := h.telling(t)
	h.client.edited = &github.Edited{UpdatedAt: h.clock.Now()}

	_, err := h.svc.Unassign(t.Context(), ids[3])
	require.NoError(t, err)

	// An edit refused before anything is sent tells nothing.
	_, err = h.svc.Unassign(t.Context(), ids[2])
	require.ErrorIs(t, err, ErrNotAssigned)

	assert.Equal(t, [][]Outcome{{told(OutcomeUnassigned, issueRef("example-repo", 3), h.clock.Now())}}, sink.told())
}

func TestOutcomesUnrequestedEvenWhenStoreFails(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	sink := h.telling(t)

	// A team still asked keeps the pull request listed: GitHub took the user off all the same.
	h.client.edited = &github.Edited{
		UpdatedAt: h.clock.Now(), ReviewRequests: github.ReviewRequests{Users: []string{}, Teams: []string{"backend"}},
	}

	snap, err := h.svc.RemoveReviewRequest(t.Context(), ids[2])
	require.NoError(t, err)
	assert.Equal(t, []int{2, 1}, numbers(snap.ReviewRequested), "the team request keeps it listed")

	// GitHub made the edit, but the inbox can't record it.
	h.client.edited = &github.Edited{UpdatedAt: h.clock.Now(), ReviewRequests: github.ReviewRequests{}}
	refuse(t, h, "BEFORE UPDATE OF reason_review_requested ON items", "")

	_, err = h.svc.RemoveReviewRequest(t.Context(), ids[1])
	require.Error(t, err)
	assert.Equal(t, "GitHub made the change, but the inbox could not record it. The next refresh catches it up.",
		uierr.Message(err))
	assert.True(t, h.reasons(t, 1)[0], "nothing was stored")

	assert.Equal(t, [][]Outcome{
		{told(OutcomeUnrequested, prRef(2), h.clock.Now())},
		{told(OutcomeUnrequested, prRef(1), h.clock.Now())},
	}, sink.told())
}

func TestOutcomesNoEditOutcomeWhenGitHubRefuses(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	sink := h.telling(t)

	h.client.editErr = errFakeGate
	_, err := h.svc.Unassign(t.Context(), ids[3])
	require.ErrorIs(t, err, errFakeGate)

	_, err = h.svc.RemoveReviewRequest(t.Context(), ids[1])
	require.ErrorIs(t, err, errFakeGate)

	assert.Empty(t, sink.told())

	// Another account's token is refused before anything is sent. A service started since
	// the refresh has not seen the token yet, and checks it.
	h.client.editErr = nil
	h.client.viewer = "someone-else"
	sink = h.telling(t)

	_, err = h.svc.Unassign(t.Context(), ids[3])
	require.ErrorIs(t, err, ErrOtherAccount)
	assert.Empty(t, sink.told())
}

// closedAs is item it, as GitHub answers for it once it is in state.
func closedAs(it github.Item, state string) github.Item {
	it.State = state

	return it
}

func TestOutcomesSeenClosedFromReadsAndRefresh(t *testing.T) {
	t.Parallel()

	t.Run("a read of a pull request", func(t *testing.T) {
		t.Parallel()

		h, id := itemHarness(t)
		sink := h.telling(t)

		_, err := NewActor(h.svc).ReadPull(t.Context(), id, func(context.Context, github.Token, PullTarget) (PullRead, error) {
			merged := closedAs(pr(1, "OWNER", false), github.StateMerged)

			return PullRead{Update: &github.ItemUpdate{Found: true, Item: merged, Details: prDetails()}}, nil
		})
		require.NoError(t, err)
		assert.Equal(t, [][]Outcome{{told(OutcomeSeenClosed, prRef(1), h.clock.Now())}}, sink.told())
	})

	t.Run("a read of an issue", func(t *testing.T) {
		t.Parallel()

		h, ids := editHarness(t)
		sink := h.telling(t)
		actor := NewActor(h.svc)

		read := func(state string) func(context.Context, github.Token, IssueTarget) (IssueRead, error) {
			return func(context.Context, github.Token, IssueTarget) (IssueRead, error) {
				it := closedAs(byOther(issue("example-repo", 3, "MEMBER")), state)

				return IssueRead{Update: &github.ItemUpdate{Found: true, Item: it}}, nil
			}
		}

		_, err := actor.ReadIssue(t.Context(), ids[3], read(github.StateOpen))
		require.NoError(t, err)
		assert.Empty(t, sink.told(), "an open issue tells nothing")

		_, err = actor.ReadIssue(t.Context(), ids[3], read(github.StateClosed))
		require.NoError(t, err)
		assert.Equal(t, [][]Outcome{{told(OutcomeSeenClosed, issueRef("example-repo", 3), h.clock.Now())}}, sink.told())
	})

	t.Run("an item's own refresh", func(t *testing.T) {
		t.Parallel()

		h, id := itemHarness(t)
		sink := h.telling(t)

		// An item GitHub no longer finds leaves the lists, but was not seen to close.
		h.client.items = map[string]*github.ItemUpdate{nodeID(1): {RateLimit: itemRate()}}
		_, err := h.svc.RefreshItem(t.Context(), id)
		require.NoError(t, err)
		assert.Empty(t, sink.told())

		h.client.items = map[string]*github.ItemUpdate{nodeID(2): {
			Found: true, Item: closedAs(pr(2, "OWNER", false), github.StateClosed), Details: prDetails(), RateLimit: itemRate(),
		}}
		_, err = h.svc.RefreshItem(t.Context(), itemByNumber(t, h.snapshot(t).ReviewRequested, 2).ID)
		require.NoError(t, err)
		assert.Equal(t, [][]Outcome{{told(OutcomeSeenClosed, prRef(2), h.clock.Now())}}, sink.told())
	})

	t.Run("the checks follower", func(t *testing.T) {
		t.Parallel()

		h, _ := followHarness(t)
		sink := h.telling(t)
		h.clock.Advance(FollowInterval)

		merged := open(h, 1, withChecks("SUCCESS"))
		merged.Item.State = github.StateMerged
		h.client.items = map[string]*github.ItemUpdate{nodeID(1): merged, nodeID(3): open(h, 3, withChecks("PENDING"))}

		require.NoError(t, h.svc.refreshRunning(t.Context()))
		assert.Equal(t, [][]Outcome{{told(OutcomeSeenClosed, prRef(1), h.clock.Now())}}, sink.told(),
			"one call for the round, after every pull request's lock is let go")
	})

	t.Run("a whole refresh", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		sink := h.telling(t)
		a, b := pr(1, "OWNER", false), pr(2, "OWNER", false)
		closedLongAgo := closedAs(pr(3, "OWNER", false), github.StateClosed)

		// A cold refresh has no lists to compare with: a closed item its search still
		// returns was never the inbox's.
		h.seed(t, []github.Item{a, b, closedLongAgo}, nil, []github.Item{a})
		assert.Empty(t, sink.told(), "a cold refresh tells nothing")

		// The search still lists #1, in two lists, but its state says it merged.
		h.tick()

		merged := closedAs(a, github.StateMerged)
		snap := h.seed(t, []github.Item{merged, b, closedLongAgo}, nil, []github.Item{merged})
		at := h.clock.Now()

		assert.Equal(t, []int{2}, numbers(snap.ReviewRequested))

		// The search returns them again: they left the lists at the refresh before.
		h.tick()
		h.seed(t, []github.Item{merged, b, closedLongAgo}, nil, []github.Item{merged})

		assert.Equal(t, [][]Outcome{{told(OutcomeSeenClosed, prRef(1), at)}}, sink.told(),
			"told once, of the item the lists held, and never as unlisted too")
	})
}

// snapshot reads the current snapshot, which must succeed.
func (h *harness) snapshot(t *testing.T) Snapshot {
	t.Helper()

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)

	return snap
}

func TestOutcomesUnlistedFromRefresh(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	sink := h.telling(t)
	review, mine, other := pr(1, "OWNER", false), pr(2, "OWNER", false), pr(4, "OWNER", false)
	assigned := byOther(issue("other-repo", 6, "MEMBER"))
	shelved := byOther(issue("shelved-repo", 7, "MEMBER"))

	h.seed(t, []github.Item{review}, []github.Item{mine, other}, []github.Item{assigned, shelved})
	assert.Empty(t, sink.told(), "the first refresh has no lists to compare with")

	// #2, #6, and #7 leave their lists. Authored is cut short, so #2 may only be past the
	// page, while Assigned came back whole, so #6 left it. #7's repository was archived,
	// which is no news of the item.
	h.tick()

	shelved.Repo.IsArchived = true
	h.client.inbox = fetched([]github.Item{review}, []github.Item{other}, []github.Item{shelved})
	h.client.inbox.Authored.IssueCount = 5
	h.refresh(t)
	assert.Equal(t, [3]bool{false, false, false}, h.reasons(t, 7), "#7 left the lists")

	assert.Equal(t, [][]Outcome{{told(OutcomeUnlisted, issueRef("other-repo", 6), h.clock.Now())}}, sink.told())

	// Every list whole again: #4 left too.
	h.tick()
	h.seed(t, []github.Item{review}, nil, nil)

	require.Len(t, sink.told(), 2)
	assert.ElementsMatch(t, []Outcome{told(OutcomeUnlisted, prRef(4), h.clock.Now())}, sink.told()[1],
		"#2 left at the refresh before, unseen; only what was listed at this one's start counts")
}

func TestOutcomesNoneOnAccountSwitch(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	sink := h.telling(t)
	h.seed(t, []github.Item{pr(1, "OWNER", false)}, []github.Item{pr(2, "OWNER", false)}, nil)

	// Another account's lists replace the first's: nothing of the first's left them, and a
	// closed item the second's search returns was never in its lists.
	h.tick()
	h.client.inbox = fetched([]github.Item{byOther(pr(5, "MEMBER", false))}, nil,
		[]github.Item{closedAs(byOther(pr(8, "MEMBER", false)), github.StateClosed)})
	h.client.inbox.Viewer.Login = "octo-second"
	h.refresh(t)

	// Back to the first account, whose lists are another time's now.
	h.tick()
	h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, []github.Item{closedAs(pr(2, "OWNER", false), github.StateClosed)})

	assert.Empty(t, sink.told())
	assert.Equal(t, [3]bool{false, false, false}, h.reasons(t, 2), "#2 left the lists all the same")
}

func TestOutcomesNilSinkIsNoOp(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	require.Nil(t, h.svc.outcomes)

	h.svc.settle(t.Context(), []Outcome{told(OutcomeMerged, prRef(1), h.clock.Now())})

	h.client.edited = &github.Edited{UpdatedAt: h.clock.Now()}
	_, err := h.svc.Unassign(t.Context(), ids[3])
	require.NoError(t, err)

	_, err = h.svc.Dismiss(t.Context(), ids[2])
	require.NoError(t, err)

	snap, err := NewActor(h.svc).Act(t.Context(), ids[1], acting(Acted{Merged: true}))
	require.NoError(t, err)
	assert.Equal(t, []int{2}, numbers(snap.ReviewRequested))

	h.tick()
	snap = h.seed(t, []github.Item{closedAs(byOther(pr(2, "MEMBER", false)), github.StateClosed)}, nil, nil)
	assert.Empty(t, snap.ReviewRequested.Items)
	assert.Empty(t, snap.Assigned.Items)
	assert.Empty(t, h.svc.pending.take(), "a refresh works out nothing to tell")

	// Nothing is read for a sink that isn't there.
	_, found, err := h.svc.triageBefore(t.Context(), h.svc.q, ids[2])
	require.NoError(t, err)
	assert.False(t, found, "the triage is not read")
}

func TestOutcomesRefreshToldOutsideItsFlight(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	sink := h.telling(t)
	a := pr(1, "OWNER", false)
	h.seed(t, []github.Item{a}, nil, nil)

	// A sink that refreshes the inbox again, as it is told, once: inside the flight of the
	// refresh that told it, it would wait for that refresh to end, which waits for it.
	var again error

	sink.then = func(ctx context.Context) {
		sink.then = nil

		bounded, cancel := context.WithTimeout(ctx, waitTimeout)
		defer cancel()

		_, again = h.svc.Refresh(bounded)
	}

	h.tick()
	h.seed(t, []github.Item{closedAs(a, github.StateMerged)}, nil, nil)

	require.NoError(t, again, "the sink's own refresh ran, and did not wait for the one that told it")
	assert.Equal(t, [][]Outcome{{told(OutcomeSeenClosed, prRef(1), h.clock.Now())}}, sink.told())
}

func TestOutcomesJoinedRefreshToldOnce(t *testing.T) {
	t.Parallel()

	// In a synctest bubble, synctest.Wait returns once every goroutine in it is blocked,
	// which is when the second caller has joined the refresh in flight.
	synctest.Test(t, func(t *testing.T) {
		h, _ := itemHarness(t)
		sink := h.telling(t)
		c := &gatedClient{fakeClient: h.client}
		h.svc = NewService(Config{Pool: h.pool, Tokens: h.tokens, Client: c, Clock: h.clock, Outcomes: sink})
		sink.svc = h.svc

		merged := closedAs(pr(1, "OWNER", false), github.StateMerged)
		h.setInbox(fetched([]github.Item{merged, pr(2, "OWNER", false)}, nil, []github.Item{merged}))

		first := refreshUntilGate(t, h, c)
		second := async(func() (Snapshot, error) { return h.svc.Refresh(t.Context()) })

		synctest.Wait()
		c.inboxGate.release()

		require.NoError(t, await(t, first).err)
		require.NoError(t, await(t, second).err)
		assert.Equal(t, [][]Outcome{{told(OutcomeSeenClosed, prRef(1), h.clock.Now())}}, sink.told(),
			"one refresh, told once, by whichever caller came back first")
	})
}

func TestOutcomesToldWithALiveContext(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	sink := h.telling(t)
	ctx, cancel := context.WithCancel(t.Context())

	// The caller gives up right after GitHub made the merge.
	_, err := NewActor(h.svc).Act(ctx, id, func(context.Context, github.Token, PullTarget) (Acted, error) {
		cancel()

		return Acted{Merged: true}, nil
	})
	require.ErrorIs(t, err, context.Canceled, "the inbox could not record it, and says so")

	assert.Equal(t, [][]Outcome{{told(OutcomeMerged, prRef(1), h.clock.Now())}}, sink.told(),
		"what GitHub made is told all the same")
	assert.Equal(t, []error{nil}, sink.contextErrs(), "with a context the caller's cancel never reaches")
}

func TestOutcomesSinkCheckSeesAHeldItemLock(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	unlock, err := h.svc.items.lock(t.Context(), 7)
	require.NoError(t, err)
	assert.Equal(t, []int64{7}, heldItems(&h.svc.items), "the check the sink makes sees a held lock")

	unlock()
	assert.Empty(t, heldItems(&h.svc.items))
}

func TestOutcomesSinkSeesEmptyCallsNever(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	sink := h.telling(t)

	h.svc.settle(t.Context(), nil)
	h.svc.settle(t.Context(), []Outcome{})

	assert.Empty(t, sink.told())
	assert.Empty(t, sink.all())
}
