package ghthreads

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/threadrules"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestSendRechecksStaleDecision(t *testing.T) {
	t.Parallel()

	r := clearingRig(t, prThread(firstID, 1, start))
	r.settle(outcome(OutcomeDismissed, item.KindPR, 1, start))

	// Someone comments after the dismissal, past the grace, before the clear goes, and a
	// listing reads it.
	r.gh.commented(firstID, 900, "User", start.Add(3*time.Minute))
	r.clock.Advance(4 * time.Minute)

	got := r.mustPass()
	assert.Empty(t, writes(got), "decided again on the new update: held, so nothing sent")

	ledger := r.ledger()
	require.Len(t, ledger, 1)
	assert.Equal(t, "skipped", ledger[0].state)
	assert.Equal(t, string(SkipChanged), ledger[0].reason)

	r.clock.Advance(5 * time.Minute)

	got = r.mustPass()
	assert.Len(t, ofKind(got, "comment"), 1, "who commented is read")
	assert.Empty(t, writes(got), "a person's comment: held")

	// The user reads it on GitHub: seen, so it clears.
	r.gh.read(firstID)
	r.clock.Advance(5 * time.Minute)

	assert.Equal(t, []int64{firstID}, idsOf(ofKind(r.mustPass(), kindDone)))

	ledger = r.ledger()
	require.Len(t, ledger, 2)
	assert.Equal(t, "sent", ledger[1].state, "a fresh clear, at the new update")
}

func TestSendWaitsOnAccountMismatch(t *testing.T) {
	t.Parallel()

	r := clearingRig(t, prThread(firstID, 1, start))
	r.settle(outcome(OutcomeReviewed, item.KindPR, 1, start))

	// gh auth switch, before the hold ends.
	r.tokens.swap("another-test-token-not-real")
	r.gh.configure(func(f *fakeGitHub) { f.viewer = otherLogin })
	r.clock.Advance(MinHold)

	got := r.send()
	assert.Equal(t, []listedRequest{{kind: "viewer"}}, got, "the new token's account is learned, and nothing is sent")
	assert.Equal(t, "queued", r.ledger()[0].state, "the clear waits")

	_, waiting := r.svc.untilSend()
	assert.False(t, waiting, "until the next pass, not on a timer")

	// Switched back: the next pass sends it.
	r.tokens.swap("test-token-not-real")
	r.gh.configure(func(f *fakeGitHub) { f.viewer = viewerLogin })
	r.clock.Advance(time.Minute)

	assert.Equal(t, []int64{firstID}, idsOf(ofKind(r.mustPass(), kindDone)))
	assert.Equal(t, "sent", r.ledger()[0].state)
}

func TestSendNeverClearsUnlistedThread(t *testing.T) {
	t.Parallel()

	threads := []fakeThread{prThread(firstID, 1, start), prThread(firstID+1, 2, start.Add(-time.Minute))}
	r := clearingRig(t, threads...)
	r.settle(outcome(OutcomeReviewed, item.KindPR, 1, start))

	// A listing that races the thread's update drops it for one pass.
	r.gh.remove(firstID)
	r.clock.Advance(time.Minute)

	got := r.mustPass()
	assert.Empty(t, writes(got), "not listed: never cleared")

	_, listed := r.row(viewerLogin, firstID)
	require.False(t, listed)

	ledger := r.ledger()
	require.Len(t, ledger, 1)
	assert.Equal(t, "skipped", ledger[0].state)
	assert.Equal(t, string(SkipNotListed), ledger[0].reason)

	// The next listing has it again, and it still clears.
	r.gh.set(threads)
	r.clock.Advance(time.Minute)

	assert.Equal(t, []int64{firstID}, idsOf(ofKind(r.mustPass(), kindDone)))
	assert.Len(t, r.ledger(), 2)
}

func TestSendNotFoundCountsAsDone(t *testing.T) {
	t.Parallel()

	r := clearingRig(t, prThread(firstID, 1, start))
	r.settle(outcome(OutcomeDismissed, item.KindPR, 1, start))

	// Marked done on github.com meanwhile: GitHub no longer has it.
	r.gh.remove(firstID)
	r.clock.Advance(MinHold)

	assert.Equal(t, []int64{firstID}, idsOf(ofKind(r.send(), kindDone)), "GitHub answers 404")

	ledger := r.ledger()
	require.Len(t, ledger, 1)
	assert.Equal(t, "sent", ledger[0].state, "done already counts as done")
	assert.Zero(t, ledger[0].attempts)

	_, ok := r.row(viewerLogin, firstID)
	assert.False(t, ok, "and out of the mirror")

	r.clock.Advance(time.Hour)
	assert.Empty(t, writes(r.send()), "never sent again")
}

func TestSendFailsAfterThreeAttempts(t *testing.T) {
	t.Parallel()

	r := clearingRig(t, prThread(firstID, 1, start))
	r.settle(outcome(OutcomeDismissed, item.KindPR, 1, start))
	r.gh.configure(func(f *fakeGitHub) { f.writeStatus = http.StatusBadGateway })
	r.clock.Advance(MinHold)

	for attempt := 1; attempt <= MaxAttempts; attempt++ {
		assert.Len(t, writes(r.send()), 1, "attempt %d", attempt)

		l := r.ledger()[0]
		assert.Equal(t, attempt, l.attempts)
		assert.NotEmpty(t, l.err, "the failure in the window's words")

		if attempt < MaxAttempts {
			assert.Equal(t, "queued", l.state)
			assert.Equal(t, FailedRetry, r.untilSend(), "tried again a minute on")
			r.clock.Advance(FailedRetry)
		}
	}

	failed := r.ledger()[0]
	assert.Equal(t, "failed", failed.state, "three attempts at most")

	snap := r.snapshot()
	require.Len(t, snap.Recent, 1)
	assert.Equal(t, ClearFailed, snap.Recent[0].State)
	assert.Equal(t, failed.err, snap.Recent[0].Error)

	r.clock.Advance(time.Hour)
	assert.Empty(t, writes(r.send()), "never tried again")
}

func TestSendWaitsForChoreLane(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	tracker := budget.NewTracker(r.clock, nil)
	r.gh.gate = tracker
	r.restart()
	r.clearing()
	r.gh.set(r.gh.mergedThreads(3))

	assert.Len(t, writes(r.mustPass()), 1, "one, then the lane's second wait")
	assert.Equal(t, budget.ChoreGap, r.untilSend(), "the next goes when the lane lets it")

	for _, l := range r.ledger() {
		assert.Zero(t, l.attempts, "a refusal of the lane is no attempt")
	}

	r.clock.Advance(budget.ChoreGap)
	assert.Len(t, writes(r.send()), 1)

	// GitHub asks the app to slow down: the last waits as long.
	tracker.HoldWrites(30 * time.Second)
	r.clock.Advance(budget.ChoreGap)
	assert.Empty(t, writes(r.send()))
	assert.Equal(t, 30*time.Second-budget.ChoreGap, r.untilSend())

	r.clock.Advance(r.untilSend())
	assert.Len(t, writes(r.send()), 1)

	for _, l := range r.ledger() {
		assert.Equal(t, "sent", l.state)
		assert.Zero(t, l.attempts)
	}
}

func TestSendNothingWithReadingOffOrRefused(t *testing.T) {
	t.Parallel()

	t.Run("reading off", func(t *testing.T) {
		t.Parallel()

		r := clearingRig(t, prThread(firstID, 1, start))
		r.settle(outcome(OutcomeDismissed, item.KindPR, 1, start))
		loans := r.tokens.loans()

		r.rules.off.Store(true)
		r.clock.Advance(MinHold)

		assertNoRequests(t, r.send(), "reading off: no request at all")
		assert.Equal(t, loans, r.tokens.loans(), "not even a token borrowed")
		assert.Equal(t, "skipped", r.ledger()[0].state)
		assert.Equal(t, string(SkipOff), r.ledger()[0].reason)

		_, waiting := r.svc.untilSend()
		assert.False(t, waiting)
	})

	t.Run("refused", func(t *testing.T) {
		t.Parallel()

		r := clearingRig(t, prThread(firstID, 1, start))
		r.settle(outcome(OutcomeDismissed, item.KindPR, 1, start))

		r.gh.configure(func(f *fakeGitHub) { f.listStatus, f.listMessage = http.StatusForbidden, "Resource not accessible" })
		r.gh.update(firstID, start.Add(time.Second))
		r.clock.Advance(time.Minute)

		_, err := r.pass()
		require.Error(t, err)
		require.Equal(t, AccessRefused, r.snapshot().Status.Access)

		assertNoRequests(t, r.send(), "nothing is sent while GitHub refuses the listing")
		assert.Equal(t, "queued", r.ledger()[0].state, "it waits")

		r.clock.Advance(time.Hour)
		assertNoRequests(t, r.mustPass(), "backing off: nothing at all")
	})
}

func TestAutoOffSkipsQueued(t *testing.T) {
	t.Parallel()

	r := clearingRig(t, prThread(firstID, 1, start), prThread(firstID+1, 2, start.Add(-time.Minute)))
	r.settle(outcome(OutcomeDismissed, item.KindPR, 1, start))

	// The user clears the other by hand, and turns clearing off before anything is sent.
	r.svc.sendBy(start.Add(time.Hour)) // no send run yet
	_, err := r.svc.Clear(t.Context(), []int64{firstID + 1})
	require.NoError(t, err)
	require.Len(t, r.snapshot().Queued, 2)

	r.rules.change(func(rules *threadrules.Rules) { rules.Auto = false })
	NewSyncer(r.svc, nil, nil).RulesChanged()

	got := r.nudged()
	assert.Equal(t, []int64{firstID + 1}, idsOf(writes(got)), "the one asked for by hand still goes")

	ledger := r.ledger()
	require.Len(t, ledger, 2)
	assert.Equal(t, "skipped", ledger[0].state, "clearing off stops the rest at once")
	assert.Equal(t, string(SkipOff), ledger[0].reason)
	assert.Equal(t, "sent", ledger[1].state)

	r.clock.Advance(time.Hour)
	assert.Empty(t, writes(r.mustPass()))
}

func TestQueuedOverADayIsDecidedAgain(t *testing.T) {
	t.Parallel()

	r := clearingRig(t, prThread(firstID, 1, start))
	r.settle(outcome(OutcomeReviewed, item.KindPR, 1, start))

	// Nothing ran for a day: the app was asleep.
	r.clock.Advance(QueuedFor + time.Hour)

	assert.Equal(t, []int64{firstID}, idsOf(ofKind(r.send(), kindDone)), "it still clears: sent once decided again")

	ledger := r.ledger()
	require.Len(t, ledger, 2)
	assert.Equal(t, "skipped", ledger[0].state)
	assert.Equal(t, string(SkipStale), ledger[0].reason)
	assert.Equal(t, "sent", ledger[1].state)
	assert.True(t, ledger[1].queuedAt.Equal(r.clock.Now()), "decided now")
}

func TestARefusedChoreLaneIsNotAFailure(t *testing.T) {
	t.Parallel()

	r := clearingRig(t, prThread(firstID, 1, start))
	r.settle(outcome(OutcomeDismissed, item.KindPR, 1, start))

	gate, ok := r.gh.gate.(*fakeGate)
	require.True(t, ok)

	gate.mu.Lock()
	gate.refuse = uierr.New("Not now.", budget.ErrWritesHeld)
	gate.mu.Unlock()

	r.clock.Advance(MinHold)
	assert.Empty(t, writes(r.send()))
	assert.Zero(t, r.ledger()[0].attempts)
	assert.Equal(t, "queued", r.ledger()[0].state)
}
