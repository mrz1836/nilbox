package ghthreads

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

func TestKeepUndoesQueuedClear(t *testing.T) {
	t.Parallel()

	r := clearingRig(t, prThread(firstID, 1, start))
	r.settle(outcome(OutcomeDismissed, item.KindPR, 1, start))

	snap := r.snapshot()
	require.Len(t, snap.Queued, 1)
	assert.Equal(t, QueuedClear{
		ID: snap.Queued[0].ID, ThreadID: firstID, Owner: owner, Name: exampleRepo, Kind: item.KindPR, Number: 1,
		Title: "Retry the sync worker 1", SendAfter: clock.Format(start.Add(MinHold)),
		Trigger: threadrules.TriggerDismissed, Action: threadrules.ActionDone,
	}, snap.Queued[0], "what the toast's Undo takes back")

	r.clock.Advance(5 * time.Second)

	kept, err := r.svc.Keep(t.Context(), []int64{snap.Queued[0].ID, 999})
	require.NoError(t, err)
	assert.Empty(t, kept.Queued)
	require.Len(t, kept.Recent, 1)
	assert.Equal(t, ClearKept, kept.Recent[0].State)
	assert.Equal(t, clock.Format(r.clock.Now()), kept.Recent[0].At)

	r.clock.Advance(MinHold)
	assert.Empty(t, writes(r.send()), "a kept clear is never sent")

	again, err := r.svc.Keep(t.Context(), nil)
	require.NoError(t, err)
	assert.Equal(t, kept.Recent, again.Recent, "nothing to keep changes nothing")
}

func TestKeptThreadStaysUntilItMoves(t *testing.T) {
	t.Parallel()

	r := clearingRig(t, prThread(firstID, 1, start))
	r.settle(outcome(OutcomeDismissed, item.KindPR, 1, start))

	_, err := r.svc.Keep(t.Context(), []int64{r.snapshot().Queued[0].ID})
	require.NoError(t, err)

	r.clock.Advance(5 * time.Minute)
	assert.Empty(t, writes(r.mustPass()), "the Undo sticks")
	assert.Len(t, r.ledger(), 1, "not queued again")

	preview, err := r.svc.Preview(t.Context())
	require.NoError(t, err)
	assert.Zero(t, preview.Count)
	require.Len(t, preview.Held, 1)
	assert.Equal(t, SkipKept, preview.Held[0].Reason)

	// It moves on GitHub: a bot's comment, after the dismissal.
	r.gh.commented(firstID, 900, "Bot", start.Add(8*time.Minute))
	r.clock.Advance(5 * time.Minute)

	assert.Equal(t, []int64{firstID}, idsOf(ofKind(r.mustPass(), kindDone)), "moved, it clears again")
}

func TestClearByHandWorksWithAutoOff(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set([]fakeThread{prThread(firstID, 1, start)})
	r.mustPass()

	snap, err := r.svc.Clear(t.Context(), []int64{firstID, firstID})
	require.NoError(t, err)
	require.Len(t, snap.Queued, 1, "once, whoever asks twice")
	assert.Equal(t, threadrules.TriggerByHand, snap.Queued[0].Trigger)
	assert.Equal(t, clock.Format(start), snap.Queued[0].SendAfter, "no hold")

	wait, ok := r.svc.untilSend()
	require.True(t, ok)
	assert.LessOrEqual(t, wait, time.Duration(0), "the Syncer sends it at once")

	assert.Equal(t, []int64{firstID}, idsOf(ofKind(r.send(), kindDone)))

	// With reading off, not even by hand.
	r.gh.set([]fakeThread{prThread(firstID+1, 2, start.Add(time.Minute))})
	r.clock.Advance(5 * time.Minute)
	r.mustPass()
	r.rules.off.Store(true)

	snap, err = r.svc.Clear(t.Context(), []int64{firstID + 1})
	require.NoError(t, err)
	assert.Empty(t, snap.Queued)
	assert.Len(t, r.ledger(), 1)
}

func TestClearItemClearsOnlyThatItem(t *testing.T) {
	t.Parallel()

	second := prThread(firstID+1, 1, start.Add(-time.Minute))
	second.reason = "mention"

	r := newRig(t)
	r.gh.set([]fakeThread{prThread(firstID, 1, start), second, prThread(firstID+2, 2, start.Add(-2*time.Minute))})
	r.mustPass()

	snap, err := r.svc.ClearItem(t.Context(), "Acme-Org", "Example-Repo", 1)
	require.NoError(t, err)
	assert.Len(t, snap.Queued, 2, "both of its threads")

	assert.ElementsMatch(t, []int64{firstID, firstID + 1}, idsOf(ofKind(r.send(), kindDone)))

	_, ok := r.row(viewerLogin, firstID+2)
	assert.True(t, ok, "the other item's thread is left alone")

	snap, err = r.svc.ClearItem(t.Context(), owner, exampleRepo, 404)
	require.NoError(t, err)
	assert.Empty(t, snap.Queued, "an item with no thread clears nothing")
}

func TestClearNeverTouchesNonItemThreads(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clearing()
	r.gh.set(append(checkThreads(1, start), fakeThread{
		id: firstID + 1, unread: true, reason: "subscribed", updated: start, title: "v1.2.0",
		repo: owner + "/" + exampleRepo, subject: "releases/1", typ: "Release",
	}, prThread(firstID+2, 3, start)))
	r.mustPass()

	snap, err := r.svc.Clear(t.Context(), []int64{firstID, firstID + 1, firstID + 2})
	require.NoError(t, err)
	require.Len(t, snap.Queued, 1, "only the pull request's")
	assert.Equal(t, firstID+2, snap.Queued[0].ThreadID)

	assert.Equal(t, []int64{firstID + 2}, idsOf(writes(r.send())))
	assert.Equal(t, 2, r.snapshot().Status.Count, "a check suite and a release are only counted")
}

func TestPreviewCountsWithAutoOff(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	other := prThread(firstID+3, 7, start.Add(-time.Hour))
	other.repo = owner + "/other-repo"

	busy := prThread(firstID+4, 8, start.Add(10*time.Minute)) // a person commented after the merge
	threads := append(r.gh.mergedThreads(3), other, busy, prThread(firstID+5, 9, start))

	r.gh.configure(func(f *fakeGitHub) {
		f.states[owner+"/other-repo#7"] = github.StateMerged
		f.states[exampleRef("8")] = github.StateMerged
		f.states[exampleRef("9")] = github.StateOpen
	})
	r.gh.set(threads)
	r.clock.Advance(11 * time.Minute)

	assert.Empty(t, writes(r.mustPass()), "clearing is off: nothing is sent")
	assert.Empty(t, r.ledger(), "nor queued")

	preview, err := r.svc.Preview(t.Context())
	require.NoError(t, err)
	assert.True(t, preview.Reading)
	assert.False(t, preview.Auto)
	assert.Equal(t, threadrules.ActionDone, preview.Action)
	assert.Equal(t, 4, preview.Count, "decided as if clearing were on")

	require.Len(t, preview.Repos, 2, "by repository")
	assert.Equal(t, exampleRepo, preview.Repos[0].Name)
	assert.Len(t, preview.Repos[0].Threads, 3)
	assert.Equal(t, "other-repo", preview.Repos[1].Name)
	assert.Equal(t, PreviewThread{
		ID: firstID + 3, Owner: owner, Name: "other-repo", Kind: item.KindPR, Number: 7, Title: "Retry the sync worker 7",
		Reason: "review_requested", Unread: true, UpdatedAt: clock.Format(start.Add(-time.Hour)), Trigger: threadrules.TriggerMerged,
	}, preview.Repos[1].Threads[0])

	require.Len(t, preview.Held, 1, "the open one has no moment; the busy one is held")
	assert.Equal(t, SkipNewActivity, preview.Held[0].Reason)
	assert.Equal(t, 1, preview.Held[0].Count)
	assert.Equal(t, firstID+4, preview.Held[0].Threads[0].ID)

	r.rules.off.Store(true)

	preview, err = r.svc.Preview(t.Context())
	require.NoError(t, err)
	assert.False(t, preview.Reading)
	assert.Zero(t, preview.Count)
	assert.NotNil(t, preview.Repos)
	assert.NotNil(t, preview.Held)
}
