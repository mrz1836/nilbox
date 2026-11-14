package ghthreads

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/threadrules"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// releaseThread is an unread notification about a release of example-repo.
func releaseThread(id int64) fakeThread {
	return fakeThread{
		id: id, unread: true, reason: "subscribed", updated: start, title: "v1.2.0",
		repo: owner + "/" + exampleRepo, subject: "releases/1", typ: "Release",
	}
}

// waits records the waits for the chore lane's pace a mark asked for, never waiting.
type waits struct {
	mu  sync.Mutex
	got []time.Duration
}

func (w *waits) sleep(_ context.Context, d time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.got = append(w.got, d)

	return nil
}

func (w *waits) all() []time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()

	return append([]time.Duration(nil), w.got...)
}

// markRig is a rig whose mirror lists threads, read by a first pass, then restarted with a
// fresh event sink, as the app's next start, so a test sees only what a mark tells and
// sends: the token's account is asked first. Its pace waits are recorded, never waited.
func markRig(t *testing.T, threads ...fakeThread) (*rig, *waits) {
	t.Helper()

	r := newRig(t)
	r.gh.set(threads)
	r.mustPass()
	r.events = &testutil.Events{}
	r.restart()

	w := &waits{}
	r.svc.paceSleep = w.sleep

	return r, w
}

// gate is the rig's chore lane.
func (r *rig) gate() *fakeGate {
	r.t.Helper()

	gate, ok := r.gh.gate.(*fakeGate)
	require.True(r.t, ok)

	return gate
}

func TestMarkDoneTakesAnyThreadOutOfGitHubAndTheMirror(t *testing.T) {
	t.Parallel()

	r, _ := markRig(t, append(checkThreads(1, start), releaseThread(firstID+1), prThread(firstID+2, 3, start))...)
	// Clearing on its own is off, and would only mark read: a Done marks done whatever.
	r.rules.change(func(rules *threadrules.Rules) { rules.Action = threadrules.ActionRead })

	snap, err := r.svc.MarkDone(t.Context(), []int64{firstID + 2, firstID, firstID + 1, firstID})
	require.NoError(t, err)

	got := r.gh.requests()
	assert.Equal(t, []int64{firstID, firstID + 1, firstID + 2}, idsOf(ofKind(got, kindDone)),
		"a check suite, a release, and a pull request, each once")
	assert.Empty(t, ofKind(got, kindRead))
	assert.Zero(t, snap.Status.Count, "each leaves the mirror")
	assert.Empty(t, snap.Unread)
	assert.Empty(t, r.ledger(), "a Done is no clear of the rules'")
	assert.Equal(t, []event.Event{event.GHThreadsUpdated{}}, r.events.All(), "the window hears it once")

	r.mustPass()
	assert.Zero(t, r.snapshot().Status.Count, "and the next pass lists none of them again")
}

func TestMarkReadKeepsTheThreadsRead(t *testing.T) {
	t.Parallel()

	read := prThread(firstID+2, 3, start)
	read.unread = false

	r, _ := markRig(t, releaseThread(firstID), prThread(firstID+1, 2, start), read)

	snap, err := r.svc.MarkRead(t.Context(), []int64{firstID, firstID + 1, firstID + 2})
	require.NoError(t, err)

	assert.Equal(t, []int64{firstID, firstID + 1}, idsOf(ofKind(r.gh.requests(), kindRead)),
		"only the unread ones, a release among them")
	assert.Equal(t, 3, snap.Status.Count, "read, they stay in the inbox")
	assert.Zero(t, snap.Status.Unread)
	assert.Empty(t, snap.Unread)
	assert.Len(t, r.events.All(), 1)
}

func TestMarkSendsNothingForWhatTheMirrorDoesNotList(t *testing.T) {
	t.Parallel()

	r, _ := markRig(t, prThread(firstID, 1, start))

	snap, err := r.svc.MarkDone(t.Context(), []int64{firstID + 99})
	require.NoError(t, err)
	assert.Empty(t, writes(r.gh.requests()), "a thread it doesn't list")
	assert.Equal(t, 1, snap.Status.Count)

	_, err = r.svc.MarkDone(t.Context(), nil)
	require.NoError(t, err)

	r.rules.off.Store(true)

	_, err = r.svc.MarkDone(t.Context(), []int64{firstID})
	require.NoError(t, err)
	assert.Empty(t, writes(r.gh.requests()), "nor anything while reading is off")
	assert.Empty(t, r.events.All())
}

func TestMarkWaitsOutTheChoreLanesPace(t *testing.T) {
	t.Parallel()

	r, w := markRig(t, prThread(firstID, 1, start), prThread(firstID+1, 2, start))
	r.gate().paced = 2

	_, err := r.svc.MarkDone(t.Context(), []int64{firstID, firstID + 1})
	require.NoError(t, err)
	assert.Equal(t, []int64{firstID, firstID + 1}, idsOf(ofKind(r.gh.requests(), kindDone)))
	assert.Equal(t, []time.Duration{minPaceWait, minPaceWait}, w.all())
}

func TestMarkGivesUpOnAPaceLongerThanItWaits(t *testing.T) {
	t.Parallel()

	r, w := markRig(t, prThread(firstID, 1, start))
	r.gate().paced = 1000

	_, err := r.svc.MarkDone(t.Context(), []int64{firstID})
	require.Error(t, err)
	assert.Equal(t, "That's as many changes to GitHub as go in a while. Try again in a minute.", uierr.Message(err))

	total := time.Duration(0)
	for _, d := range w.all() {
		total += d
	}

	assert.LessOrEqual(t, total, MaxPaceWait, "it never waits past MaxPaceWait")
	assert.Empty(t, writes(r.gh.requests()))
	assert.Equal(t, 1, r.snapshot().Status.Count)
}

func TestMarkStandsForEveryThreadAboutItsItem(t *testing.T) {
	t.Parallel()

	mention := prThread(firstID+1, 1, start)
	mention.reason = "mention"
	read := prThread(firstID+2, 1, start)
	read.unread = false

	r, _ := markRig(t, prThread(firstID, 1, start), mention, read, prThread(firstID+3, 2, start),
		releaseThread(firstID+4))

	snap, err := r.svc.MarkRead(t.Context(), []int64{firstID})
	require.NoError(t, err)
	assert.Equal(t, []int64{firstID, firstID + 1}, idsOf(ofKind(r.gh.requests(), kindRead)),
		"the item's unread threads, not another item's nor a release")
	assert.Equal(t, 2, snap.Status.Unread)

	snap, err = r.svc.MarkDone(t.Context(), []int64{firstID + 1})
	require.NoError(t, err)
	assert.Equal(t, []int64{firstID, firstID + 1, firstID + 2}, idsOf(ofKind(r.gh.requests(), kindDone)),
		"every thread about the item, read ones too")
	assert.Equal(t, 2, snap.Status.Count, "pull request #2's and the release's stay")
}

func TestMarkDoneSettlesAClearQueuedForTheThread(t *testing.T) {
	t.Parallel()

	r := clearingRig(t, prThread(firstID, 1, start))
	r.settle(outcome(OutcomeDismissed, item.KindPR, 1, start))
	require.Len(t, r.snapshot().Queued, 1, "held for its Undo")

	r.svc.paceSleep = (&waits{}).sleep

	snap, err := r.svc.MarkDone(t.Context(), []int64{firstID})
	require.NoError(t, err)
	assert.Empty(t, snap.Queued, "the queued clear is settled with the Done")
	require.Len(t, snap.Recent, 1)
	assert.Equal(t, ClearSkipped, snap.Recent[0].State)
	assert.Equal(t, SkipNotListed, snap.Recent[0].Reason)
	assert.Equal(t, []int64{firstID}, idsOf(ofKind(r.gh.requests(), kindDone)), "the Done alone")

	r.clock.Advance(MinHold)
	assert.Empty(t, writes(r.send()), "and never sent")
}

func TestMarkRefusedSaysWhyAndKeepsWhatWentBefore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setUp func(r *rig)
		says  string
	}{
		{
			name:  "GitHub asked for a pause",
			setUp: func(r *rig) { r.gate().refuse = budget.ErrWritesHeld },
			says:  "GitHub asked for a pause before the next change. Try again in a minute.",
		},
		{
			name:  "the budget's reserve",
			setUp: func(r *rig) { r.gate().refuse = budget.ErrBudgetReserved },
			says:  "The GitHub budget is down to its reserve. Try again once it refills.",
		},
		{
			name:  "GitHub failing",
			setUp: func(r *rig) { r.gh.writeStatus = http.StatusBadGateway },
			says:  "GitHub didn't mark it done:",
		},
		{
			name:  "the token is another account's",
			setUp: func(r *rig) { r.gh.viewer = otherLogin },
			says:  "Your GitHub token is for another account now. Check again to read its notifications.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, _ := markRig(t, prThread(firstID, 1, start))
			tt.setUp(r)

			_, err := r.svc.MarkDone(t.Context(), []int64{firstID})
			require.Error(t, err)
			assert.Contains(t, uierr.Message(err), tt.says)
			assert.Equal(t, 1, r.snapshot().Status.Count, "the thread stays")
			assert.Empty(t, r.events.All())
		})
	}
}

func TestMarkDoneOfAThreadGitHubNoLongerHasDropsIt(t *testing.T) {
	t.Parallel()

	r, _ := markRig(t, prThread(firstID, 1, start))
	r.gh.writeStatus = http.StatusNotFound

	snap, err := r.svc.MarkDone(t.Context(), []int64{firstID})
	require.NoError(t, err)
	assert.Zero(t, snap.Status.Count, "done elsewhere already")
}
