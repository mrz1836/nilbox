package ghthreads

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
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
	r.tokens.Swap("another-test-token-not-real")
	r.gh.configure(func(f *fakeGitHub) { f.viewer = otherLogin })
	r.clock.Advance(MinHold)

	got := r.send()
	assert.Equal(t, []listedRequest{{kind: "viewer"}}, got, "the new token's account is learned, and nothing is sent")
	assert.Equal(t, "queued", r.ledger()[0].state, "the clear waits")

	_, waiting := r.svc.untilSend()
	assert.False(t, waiting, "until the next pass, not on a timer")

	// Switched back: the next pass sends it.
	r.tokens.Swap("test-token-not-real")
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
		loans := r.tokens.Loans()

		r.rules.off.Store(true)
		r.clock.Advance(MinHold)

		assertNoRequests(t, r.send(), "reading off: no request at all")
		assert.Equal(t, loans, r.tokens.Loans(), "not even a token borrowed")
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
	_, err := r.svc.clearByHand(t.Context(), []int64{firstID + 1})
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

// errRulesUnread is a rules setting that can't be read.
var errRulesUnread = errors.New("the rules can't be read")

// heldClear returns a rig with clearing on, whose one thread, about pull request 1, the
// user dismissed in the app at start: its clear, to mark it done, is held for MinHold.
func heldClear(t *testing.T) *rig {
	t.Helper()

	r := clearingRig(t, prThread(firstID, 1, start))
	r.settle(outcome(OutcomeDismissed, item.KindPR, 1, start))

	ledger := r.ledger()
	require.Len(t, ledger, 1)
	require.Equal(t, "queued", ledger[0].state)

	return r
}

// queuedOnly asserts the ledger holds the one clear, still queued, with no attempt spent.
func queuedOnly(t *testing.T, r *rig, msg string) {
	t.Helper()

	ledger := r.ledger()
	require.Len(t, ledger, 1, msg)
	assert.Equal(t, "queued", ledger[0].state, msg)
	assert.Zero(t, ledger[0].attempts, msg)
}

func TestSendWaitsAMinuteWhenTheTokensAccountCantBeLearned(t *testing.T) {
	t.Parallel()

	r := heldClear(t)

	// The token changes, and GitHub fails to say whose the new one is.
	r.tokens.Swap("another-test-token-not-real")
	r.gh.configure(func(f *fakeGitHub) { f.viewerStatus = http.StatusBadGateway })
	r.clock.Advance(MinHold)

	got := r.send()
	assert.NotEmpty(t, ofKind(got, "viewer"), "whose token it is was asked")
	assert.Empty(t, writes(got), "nothing is sent before the account is known")
	queuedOnly(t, r, "GitHub didn't refuse the clear: no attempt is spent")
	assert.Equal(t, FailedRetry, r.untilSend(), "tried again a minute on")

	r.gh.configure(func(f *fakeGitHub) { f.viewerStatus = 0 })
	r.clock.Advance(FailedRetry)

	assert.Equal(t, []int64{firstID}, idsOf(ofKind(r.send(), kindDone)))
	assert.Equal(t, "sent", r.ledger()[0].state)
}

func TestSendWaitsForAPassWhileNoTokenCanBeBorrowed(t *testing.T) {
	t.Parallel()

	r := heldClear(t)

	// gh auth logout, before the hold ends.
	r.tokens.Fail(github.ErrNotLoggedIn)
	r.clock.Advance(MinHold)

	assertNoRequests(t, r.send(), "no token: no request")
	queuedOnly(t, r, "no attempt is spent")

	_, waiting := r.svc.untilSend()
	assert.False(t, waiting, "until the next pass, not on a timer")

	r.tokens.Fail(nil)
	r.clock.Advance(time.Minute)

	assert.Equal(t, []int64{firstID}, idsOf(ofKind(r.mustPass(), kindDone)), "logged in again, the next pass sends it")
}

func TestAClientWithNoChoreLaneSendsNoClears(t *testing.T) {
	t.Parallel()

	r := heldClear(t)
	r.gh.gate = nil
	r.restart()
	r.clock.Advance(MinHold)

	assert.Empty(t, writes(r.send()), "the client refuses every chore")
	queuedOnly(t, r, "a refusal of the client's own is no attempt")

	_, waiting := r.svc.untilSend()
	assert.False(t, waiting, "it waits for a pass, not a timer that would only be refused again")
}

func TestSendDoesWhatTheRulesSayClearingDoesWhenItGoes(t *testing.T) {
	t.Parallel()

	r := heldClear(t)
	require.Equal(t, kindDone, r.ledger()[0].action, "queued to mark it done")

	// During the hold, the user has clearing mark threads read instead.
	r.rules.change(func(rules *threadrules.Rules) { rules.Action = threadrules.ActionRead })
	r.clock.Advance(MinHold)

	got := r.send()
	assert.Equal(t, []int64{firstID}, idsOf(ofKind(got, kindRead)), "marked read, as the rules say now")
	assert.Empty(t, ofKind(got, kindDone))

	ledger := r.ledger()
	assert.Equal(t, kindRead, ledger[0].action, "the ledger says what was done")
	assert.Equal(t, "sent", ledger[0].state)

	_, listed := r.row(viewerLogin, firstID)
	assert.True(t, listed, "marked read, it stays in the mirror")
}

func TestASendCutOffAsTheAppQuitsCostsNoAttempt(t *testing.T) {
	t.Parallel()

	r := heldClear(t)
	ctx, cancel := context.WithCancel(t.Context())

	// The app quits as the clear is on its way; GitHub takes it all the same.
	r.gh.configure(func(f *fakeGitHub) {
		f.onWrite = func(req *http.Request) {
			cancel()
			<-req.Context().Done()
		}
	})
	r.clock.Advance(MinHold)

	require.ErrorIs(t, r.svc.runPass(ctx, passRequest{send: true}), context.Canceled)
	assert.Len(t, writes(r.gh.requests()), 1)
	queuedOnly(t, r, "cut off, not refused: no attempt is spent")

	// The next run: GitHub has no such thread now, which counts as done.
	r.gh.configure(func(f *fakeGitHub) { f.onWrite = nil })

	assert.Equal(t, []int64{firstID}, idsOf(ofKind(r.send(), kindDone)))
	assert.Equal(t, "sent", r.ledger()[0].state)
	assert.Zero(t, r.ledger()[0].attempts)
}

func TestASendRunStopsAtOnceWhenTheRulesChangeBetweenClears(t *testing.T) {
	t.Parallel()

	// twoDue returns a rig with clearing on whose next pass finds two threads merged
	// elsewhere, both due at once, and runs change once, as GitHub takes the first.
	twoDue := func(t *testing.T, change func(r *rig)) *rig {
		t.Helper()

		r := newRig(t)
		r.clearing()
		r.gh.set(r.gh.mergedThreads(2))

		var once sync.Once

		r.gh.configure(func(f *fakeGitHub) {
			f.onWrite = func(*http.Request) { once.Do(func() { change(r) }) }
		})

		return r
	}

	// states returns the ledger's states, in order.
	states := func(r *rig) []string {
		ledger := r.ledger()
		out := make([]string, 0, len(ledger))

		for _, l := range ledger {
			out = append(out, l.state+l.reason)
		}

		return out
	}

	t.Run("reading turned off", func(t *testing.T) {
		t.Parallel()

		r := twoDue(t, func(r *rig) { r.rules.off.Store(true) })

		assert.Len(t, writes(r.mustPass()), 1, "the first went; the second waits")
		assert.Equal(t, []string{"sent", "queued"}, states(r))

		assertNoRequests(t, r.send(), "reading off: nothing more")
		assert.Equal(t, []string{"sent", "skipped" + string(SkipOff)}, states(r))
	})

	t.Run("clearing on its own turned off", func(t *testing.T) {
		t.Parallel()

		r := twoDue(t, func(r *rig) { r.rules.change(func(rules *threadrules.Rules) { rules.Auto = false }) })

		assert.Len(t, writes(r.mustPass()), 1, "the first went")
		assert.Equal(t, []string{"sent", "skipped" + string(SkipOff)}, states(r), "the second is skipped as it would go")
	})

	t.Run("the rules unreadable", func(t *testing.T) {
		t.Parallel()

		r := twoDue(t, func(r *rig) { r.rules.fail(errRulesUnread) })

		got, err := r.pass()
		require.ErrorIs(t, err, errRulesUnread)
		assert.Len(t, writes(got), 1, "the first went")
		assert.Equal(t, []string{"sent", "queued"}, states(r))

		r.rules.fail(nil)
		assert.Equal(t, FailedRetry, r.untilSend(), "the second is tried a minute on")

		r.clock.Advance(FailedRetry)
		assert.Len(t, writes(r.send()), 1)
		assert.Equal(t, []string{"sent", "sent"}, states(r))
	})
}

func TestASendTheDatabaseFailsSaysSoAndWaitsAMinute(t *testing.T) {
	t.Parallel()

	// refuse makes the database refuse sql's statement on table while when holds.
	refuse := func(statement, table, when string) func(r *rig) {
		return func(r *rig) {
			testutil.Exec(r.t, r.db, "CREATE TRIGGER refused BEFORE "+statement+" ON "+table+" WHEN "+when+
				" BEGIN SELECT RAISE(ABORT, 'refused by the test'); END")
		}
	}

	// drop drops table, as a damaged database might lose it.
	drop := func(table string) func(r *rig) {
		return func(r *rig) { testutil.Exec(r.t, r.db, "DROP TABLE "+table) }
	}

	// stale lets the clear wait past QueuedFor first, so it is decided again.
	stale := func(then func(r *rig)) func(r *rig) {
		return func(r *rig) {
			r.clock.Advance(QueuedFor)
			then(r)
		}
	}

	// failing has GitHub fail every write, after the clear's attempts reach attempts.
	failing := func(attempts int, then func(r *rig)) func(r *rig) {
		return func(r *rig) {
			testutil.Exec(r.t, r.db, "UPDATE gh_thread_clears SET attempts = ?", attempts)
			r.gh.configure(func(f *fakeGitHub) { f.writeStatus = http.StatusBadGateway })
			then(r)
		}
	}

	tests := []struct {
		name     string
		setUp    func(r *rig)
		words    string
		attempts int
	}{
		{name: "its moments gone", setUp: drop("gh_thread_moments"), words: "read the moments that clear"},
		{name: "the mirror gone", setUp: drop("gh_threads"), words: "read a GitHub notification to clear"},
		{name: "a stale clear's moments gone", setUp: stale(drop("gh_thread_moments")), words: "read the moments that clear"},
		{
			name: "a stale clear's skip refused", setUp: stale(refuse("UPDATE", "gh_thread_clears", "NEW.state = 'skipped'")),
			words: "record a clear as skipped",
		},
		{
			name: "a stale clear queued again refused", setUp: stale(refuse("INSERT", "gh_thread_clears", "1")),
			words: "queue a GitHub notification's clear again",
		},
		{
			name:  "a failed attempt not recorded",
			setUp: failing(0, refuse("UPDATE", "gh_thread_clears", "NEW.attempts > OLD.attempts")), words: "record a failed clear",
		},
		{
			name:  "the last failed attempt not recorded",
			setUp: failing(MaxAttempts-1, refuse("UPDATE", "gh_thread_clears", "NEW.state = 'failed'")),
			words: "record a failed clear", attempts: MaxAttempts - 1,
		},
		{
			name: "GitHub took it, the mirror refused it", setUp: refuse("DELETE", "gh_threads", "1"),
			words: "record a cleared GitHub notification",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := heldClear(t)
			tt.setUp(r)
			r.clock.Advance(MinHold)

			_, err := r.trySend()
			require.ErrorContains(t, err, tt.words)
			assert.Equal(t, FailedRetry, r.untilSend(), "tried again a minute on")

			ledger := r.ledger()
			require.Len(t, ledger, 1, "nothing half-stored")
			assert.Equal(t, "queued", ledger[0].state)
			assert.Equal(t, tt.attempts, ledger[0].attempts)
		})
	}
}

func TestAClearGitHubTookThatTheMirrorRefusedCountsAsDoneNextTime(t *testing.T) {
	t.Parallel()

	r := heldClear(t)
	testutil.Exec(t, r.db, `CREATE TRIGGER refused BEFORE DELETE ON gh_threads
		BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	r.clock.Advance(MinHold)

	got, err := r.trySend()
	require.Error(t, err)
	assert.Len(t, writes(got), 1, "GitHub took it")

	testutil.Exec(t, r.db, `DROP TRIGGER refused`)
	r.clock.Advance(FailedRetry)

	got = r.send()
	assert.Equal(t, []int64{firstID}, idsOf(ofKind(got, kindDone)), "sent again, GitHub answers 404")
	assert.Equal(t, "sent", r.ledger()[0].state)
	assert.Zero(t, r.ledger()[0].attempts)

	_, listed := r.row(viewerLogin, firstID)
	assert.False(t, listed, "and out of the mirror")
}
