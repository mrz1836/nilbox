package ghthreads

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
)

// issueThread is issue number's thread, unread, updated at.
func issueThread(id int64, number int, at time.Time) fakeThread {
	return fakeThread{
		id: id, unread: true, reason: "author", updated: at, title: "The theme is forgotten " + strconv.Itoa(number),
		repo: owner + "/" + exampleRepo, subject: "issues/" + strconv.Itoa(number), typ: "Issue",
	}
}

// clearingRig returns a rig with clearing on its own turned on, whose first pass listed
// threads, as the app's first pass after the user turned clearing on does.
func clearingRig(t *testing.T, threads ...fakeThread) *rig {
	t.Helper()

	r := newRig(t)
	r.clearing()
	r.gh.set(threads)
	r.mustPass()

	return r
}

// storedMoment is one moment as gh_thread_moments holds it.
type storedMoment struct {
	kind    string
	number  int
	trigger string
	at      time.Time
	heard   time.Time
}

// moments returns the stored moments, by item and trigger.
func (r *rig) moments() []storedMoment {
	r.t.Helper()

	rows, err := r.db.QueryContext(r.t.Context(), `SELECT item_kind, number, trigger, at, created_at
		FROM gh_thread_moments ORDER BY number, trigger`)
	require.NoError(r.t, err)

	defer func() { require.NoError(r.t, rows.Close()) }()

	var out []storedMoment

	for rows.Next() {
		var m storedMoment
		require.NoError(r.t, rows.Scan(&m.kind, &m.number, &m.trigger, &m.at, &m.heard))

		out = append(out, m)
	}

	require.NoError(r.t, rows.Err())

	return out
}

// subject reads what the mirror holds of thread id's item state.
func (r *rig) subject(id int64) (state string, closedAt, checkedAt *time.Time) {
	r.t.Helper()

	require.NoError(r.t, r.db.QueryRowContext(r.t.Context(), `SELECT subject_state, subject_closed_at,
		state_checked_at FROM gh_threads WHERE thread_id = ?`, id).Scan(&state, &closedAt, &checkedAt))

	return state, closedAt, checkedAt
}

func TestSettleRecordsMomentsAndSetsState(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set([]fakeThread{prThread(firstID, 1, start), prThread(firstID+1, 2, start), issueThread(firstID+2, 3, start)})
	r.mustPass()

	r.clock.Advance(time.Minute)
	now := r.clock.Now()
	reviewed := now.Add(-30 * time.Second)

	NewSettler(r.svc).Settle(t.Context(), []Outcome{
		outcome(OutcomeMerged, item.KindPR, 1, now),
		outcome(OutcomeClosed, item.KindIssue, 3, now),
		outcome(OutcomeReviewed, item.KindPR, 2, reviewed),
		outcome(OutcomeDismissed, item.KindPR, 2, now),
		outcome(OutcomeUnrequested, item.KindPR, 2, now),
		outcome(OutcomeSeenClosed, item.KindPR, 1, now),
		{Kind: OutcomeDismissed, Ref: outcome(OutcomeDismissed, item.KindPR, 1, now).Ref, At: now}, // no account
		outcome(OutcomeReviewed, item.KindIssue, 3, now),                                           // an issue isn't reviewed
	})

	assertNoRequests(t, r.gh.requests(), "recording asks GitHub nothing")
	assert.Equal(t, []storedMoment{
		{kind: "pr", number: 1, trigger: "merged", at: now, heard: now},
		{kind: "pr", number: 2, trigger: "dismissed", at: now, heard: now},
		{kind: "pr", number: 2, trigger: "reviewed", at: reviewed, heard: now},
		{kind: "pr", number: 2, trigger: "unassigned", at: now, heard: now},
		{kind: "issue", number: 3, trigger: "issue_closed", at: now, heard: now},
	}, r.moments(), "each at its own time, heard of now; taking oneself off a review is unassigned")

	state, closedAt, checkedAt := r.subject(firstID)
	assert.Equal(t, "MERGED", state, "the user's merge is GitHub's word")
	require.NotNil(t, closedAt)
	assert.True(t, closedAt.Equal(now))
	require.NotNil(t, checkedAt)
	assert.True(t, checkedAt.Equal(now))

	state, _, _ = r.subject(firstID + 2)
	assert.Equal(t, "CLOSED", state)

	state, _, _ = r.subject(firstID + 1)
	assert.Empty(t, state, "a review leaves the state as it was")

	req, ok := r.svc.takeNudge()
	require.True(t, ok, "the Syncer is nudged")
	assert.True(t, req.nudged)
	assert.False(t, req.all)
	assert.Len(t, req.check, 1, "what the inbox saw close is checked")
	assert.NotEmpty(t, req.refs)

	// A dismissal heard again replaces the one before.
	r.clock.Advance(time.Minute)
	NewSettler(r.svc).Settle(t.Context(), []Outcome{outcome(OutcomeDismissed, item.KindPR, 2, r.clock.Now())})
	assert.True(t, r.moments()[1].heard.Equal(r.clock.Now()))

	// Nothing to tell nudges nothing.
	_, _ = r.svc.takeNudge()
	NewSettler(r.svc).Settle(t.Context(), nil)

	_, ok = r.svc.takeNudge()
	assert.False(t, ok)
}

func TestSettleRestoredKeepsQueuedDismissClears(t *testing.T) {
	t.Parallel()

	r := clearingRig(t, prThread(firstID, 1, start))

	assert.Empty(t, writes(r.settle(outcome(OutcomeDismissed, item.KindPR, 1, start))), "held for its Undo")

	queued := r.ledger()
	require.Len(t, queued, 1)
	assert.Equal(t, "queued", queued[0].state)

	r.clock.Advance(5 * time.Second)
	assert.Empty(t, writes(r.settle(outcome(OutcomeRestored, item.KindPR, 1, r.clock.Now()))))

	assert.Empty(t, r.moments(), "the dismissal is taken back")
	assert.Equal(t, "kept", r.ledger()[0].state, "and its clear kept")

	r.clock.Advance(time.Minute)
	assert.Empty(t, writes(r.send()), "nothing is sent")
	assert.Empty(t, writes(r.mustPass()))
	assert.Len(t, r.ledger(), 1, "nor queued again")
}

func TestSettleSeenClosedChecksState(t *testing.T) {
	t.Parallel()

	r := clearingRig(t, prThread(firstID, 1, start))
	assert.Empty(t, stateRequests(r.gh.requests()))

	// Merged elsewhere a moment later; the inbox's read finds it closed.
	r.clock.Advance(10 * time.Second)
	r.gh.configure(func(f *fakeGitHub) { f.states[exampleRef("1")] = github.StateMerged })

	got := r.settle(outcome(OutcomeSeenClosed, item.KindPR, 1, r.clock.Now()))

	assert.Nil(t, pagesOf(got), "the mirror is fresh: no listing")
	assert.Equal(t, [][]string{{exampleRef("1")}}, stateRequests(got), "its state is checked, though it was just checked")
	assert.Equal(t, []int64{firstID}, idsOf(writes(got)), "merged on GitHub's word: cleared at once")
	assert.Equal(t, "merged", r.ledger()[0].trigger)
}
