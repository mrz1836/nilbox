package ghthreads

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

// afterMomentOf reads who the mirror holds acted on thread id after its moment.
func (r *rig) afterMomentOf(id int64) string {
	r.t.Helper()

	var who string

	require.NoError(r.t, r.db.QueryRowContext(r.t.Context(),
		`SELECT after_moment FROM gh_threads WHERE thread_id = ?`, id).Scan(&who))

	return who
}

func TestSubjectURLCountsAsABotOnlyOnceTheItemIsFinished(t *testing.T) {
	t.Parallel()

	t.Run("reviewed, then the author pushes and asks again", func(t *testing.T) {
		t.Parallel()

		r := newRig(t)
		r.clearing()
		r.gh.configure(func(f *fakeGitHub) { f.states[exampleRef("1")] = github.StateOpen })

		// The author pushed and asked for a review again after the user's review: GitHub
		// gives the pull request's own URL as the latest comment's, and no comment.
		r.gh.set([]fakeThread{prThread(firstID, 1, start)})
		r.mustPass()

		got := r.settle(outcome(OutcomeReviewed, item.KindPR, 1, start.Add(-10*time.Minute)))
		assert.Empty(t, ofKind(got, "comment"), "the subject's own URL: nothing to read")
		assert.Empty(t, writes(got), "an event on an open pull request may be a person's: held")
		assert.Equal(t, string(threadrules.AfterPerson), r.afterMomentOf(firstID))
		assert.Empty(t, r.ledger())

		for range 3 {
			r.clock.Advance(5 * time.Minute)
			assert.Empty(t, writes(r.mustPass()), "never cleared while unread")
		}

		r.gh.read(firstID)
		r.clock.Advance(5 * time.Minute)
		assert.Equal(t, []int64{firstID}, idsOf(ofKind(r.mustPass(), kindDone)), "read, it clears")
	})

	t.Run("merged in the app", func(t *testing.T) {
		t.Parallel()

		r := clearingRig(t, prThread(firstID, 1, start))
		r.clock.Advance(time.Minute)

		// Merged ten minutes ago, the thread updated since by the merge's own events.
		got := r.settle(outcome(OutcomeMerged, item.KindPR, 1, start.Add(-10*time.Minute)))
		assert.Empty(t, ofKind(got, "comment"))
		assert.Equal(t, string(threadrules.AfterBot), r.afterMomentOf(firstID), "the merge itself")

		r.clock.Advance(MinHold)
		assert.Equal(t, []int64{firstID}, idsOf(ofKind(r.send(), kindDone)))
	})
}

func TestFailedClearStaysUntilItMoves(t *testing.T) {
	t.Parallel()

	// failed returns a rig whose clear of its one thread, dismissed in the app, failed.
	failed := func(t *testing.T) *rig {
		t.Helper()

		r := clearingRig(t, prThread(firstID, 1, start))
		r.settle(outcome(OutcomeDismissed, item.KindPR, 1, start))
		r.gh.configure(func(f *fakeGitHub) { f.writeStatus = http.StatusBadGateway })
		r.clock.Advance(MinHold)

		for range MaxAttempts {
			r.send()
			r.clock.Advance(FailedRetry)
		}

		require.Equal(t, "failed", r.ledger()[0].state)
		r.gh.configure(func(f *fakeGitHub) { f.writeStatus = 0 })

		return r
	}

	t.Run("it waits", func(t *testing.T) {
		t.Parallel()

		r := failed(t)

		for range 3 {
			r.clock.Advance(5 * time.Minute)
			assert.Empty(t, writes(r.mustPass()), "not queued afresh on its own")
		}

		assert.Len(t, r.ledger(), 1)

		preview, err := r.svc.Preview(t.Context())
		require.NoError(t, err)
		require.Len(t, preview.Held, 1)
		assert.Equal(t, SkipFailed, preview.Held[0].Reason)

		// It moves on GitHub: a bot's comment.
		r.gh.commented(firstID, 900, "Bot", r.clock.Now())
		r.clock.Advance(5 * time.Minute)

		assert.Equal(t, []int64{firstID}, idsOf(ofKind(r.mustPass(), kindDone)), "moved, it is queued again")
		assert.Len(t, r.ledger(), 2)
	})

	t.Run("by hand", func(t *testing.T) {
		t.Parallel()

		r := failed(t)

		_, err := r.svc.Clear(t.Context(), []int64{firstID})
		require.NoError(t, err)
		assert.Equal(t, []int64{firstID}, idsOf(ofKind(r.send(), kindDone)), "the user may still clear it")
	})
}

// commentedRig returns a rig with clearing on, whose first pass already ran, and n threads
// about pull requests merged at start, each with a bot's comment five minutes on.
func commentedRig(t *testing.T, n int) *rig {
	t.Helper()

	r := clearingRig(t, prThread(firstID+900, 900, start))

	threads := r.gh.mergedFrom(firstID, 1, n, start)
	r.gh.set(append(threads, prThread(firstID+900, 900, start)))

	for i := range threads {
		r.gh.commented(threads[i].id, int64(5000+i), "Bot", start.Add(5*time.Minute))
	}

	r.clock.Advance(6 * time.Minute)

	return r
}

func TestAfterMomentChecksCappedPerPass(t *testing.T) {
	t.Parallel()

	r := commentedRig(t, MaxCommentChecks+10)

	got := r.mustPass()
	assert.Len(t, ofKind(got, "comment"), MaxCommentChecks, "50 a pass at most")
	assert.Len(t, r.ledger(), MaxCommentChecks, "those checked clear")

	r.clock.Advance(5 * time.Minute)

	got = r.mustPass()
	assert.Len(t, ofKind(got, "comment"), 10, "the rest on the next pass, each once")
	assert.Len(t, r.ledger(), MaxCommentChecks+10)

	r.clock.Advance(5 * time.Minute)
	assert.Empty(t, ofKind(r.mustPass(), "comment"), "nothing is asked twice")
}

func TestAfterMomentChecksStopAtARateLimit(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusTooManyRequests, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			r := commentedRig(t, 5)
			r.gh.configure(func(f *fakeGitHub) { f.commentStatus = status })

			got := r.mustPass()
			assert.Len(t, ofKind(got, "comment"), 1, "the first refusal stops the checks")
			assert.Empty(t, writes(got), "nothing checked, nothing cleared")

			r.gh.configure(func(f *fakeGitHub) { f.commentStatus = 0 })
			r.clock.Advance(5 * time.Minute)

			got = r.mustPass()
			assert.Len(t, ofKind(got, "comment"), 5, "the next pass asks them all")
			assert.Len(t, writes(got), 5)
		})
	}
}

func TestAHeldBacklogReadsNoStateAgain(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clearing()
	r.gh.set(r.gh.mergedThreads(FirstCap + 5))

	got := r.mustPass()
	require.Len(t, stateRequests(got), 1, "the first pass reads their states")
	require.Equal(t, FirstCap+5, r.snapshot().Backlog, "too many: held for review")

	// Hours on, GitHub's word on their states is old, but the backlog stands.
	for range 3 {
		r.clock.Advance(2 * time.Hour)

		got = r.mustPass()
		assert.Empty(t, stateRequests(got), "no state is read again only to count the backlog")
		assert.Equal(t, FirstCap+5, r.snapshot().Backlog, "still held")
	}

	preview, err := r.svc.Preview(t.Context())
	require.NoError(t, err)
	assert.Equal(t, FirstCap+5, preview.Count, "the sheet still shows them")

	// Other rules: the states are read again, once.
	r.rules.change(func(rules *threadrules.Rules) { rules.BotActivity = false })
	r.clock.Advance(time.Minute)

	got = r.mustPass()
	assert.Len(t, stateRequests(got), 1)
	assert.Equal(t, FirstCap+5, r.snapshot().Backlog)

	r.clock.Advance(2 * time.Hour)
	assert.Empty(t, stateRequests(r.mustPass()), "and not again while the backlog stands under them")
}
