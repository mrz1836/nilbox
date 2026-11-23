package ghthreads

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

func TestSnapshotBeforeAnyPass(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	assert.Equal(t, Snapshot{
		Status:   Status{Reading: true, Access: AccessUnchecked},
		Unread:   []Thread{},
		Queued:   []QueuedClear{},
		Recent:   []SettledClear{},
		BulkDone: noBulkDone(),
	}, r.snapshot(), "nothing read yet, and never null lists")
	assertNoRequests(t, r.gh.requests(), "a snapshot never asks GitHub")
}

func TestSnapshotCountsGroupsAndCaps(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	// Twenty-five unread pull requests, the newest first; a second, read, thread about the
	// first of them; an unread release; and a read issue.
	threads := prThreads(25, start)

	mention := prThread(firstID+100, 1, start.Add(-time.Hour))
	mention.unread, mention.reason = false, "mention"

	threads = append(threads, mention,
		fakeThread{
			id: firstID + 101, unread: true, reason: "subscribed", updated: start.Add(-90 * time.Minute), title: "v1.2.0",
			repo: owner + "/" + exampleRepo, subject: "releases/1", typ: "Release",
		},
		fakeThread{
			id: firstID + 102, unread: false, reason: "author", updated: start.Add(-2 * time.Hour), title: "The theme is forgotten",
			repo: owner + "/" + exampleRepo, subject: "issues/70", typ: "Issue",
		},
	)
	r.gh.set(threads)
	r.mustPass()

	// Another account's thread is never shown.
	otherAccountRow(t, r, firstID+200, start)

	snap := r.snapshot()

	assert.Equal(t, Status{
		Reading: true, Account: viewerLogin, Access: AccessOK, LastPassAt: clock.Format(start),
		LastFullWalkAt: clock.Format(start), Count: 28, Unread: 26,
	}, snap.Status)

	require.Len(t, snap.Unread, MaxUnread, "the newest 20 unread")
	assert.Equal(t, Thread{
		ID: firstID, Owner: owner, Name: exampleRepo, Kind: item.KindPR, Number: 1, SubjectType: "PullRequest",
		Title: "Retry the sync worker 1", Reason: "review_requested", UpdatedAt: clock.Format(start), Unread: true,
		State: threadrules.StateUnknown,
	}, snap.Unread[0], "a state GitHub didn't report stays unknown")

	for i, thread := range snap.Unread {
		assert.Equal(t, firstID+int64(i), thread.ID, "newest first")
	}
}

func TestSnapshotOfReleasesAndChecks(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set([]fakeThread{{
		id: firstID, unread: true, reason: "ci_activity", updated: start, title: "CI failed",
		repo: owner + "/" + exampleRepo, typ: "CheckSuite",
	}})
	r.mustPass()

	snap := r.snapshot()
	assert.Equal(t, 1, snap.Status.Count, "counted")
	require.Len(t, snap.Unread, 1)
	assert.Equal(t, Thread{
		ID: firstID, Owner: owner, Name: exampleRepo, SubjectType: "CheckSuite", Title: "CI failed", Reason: "ci_activity",
		UpdatedAt: clock.Format(start), Unread: true,
	}, snap.Unread[0])
}

// list reads the List.
func (r *rig) list() ThreadList {
	r.t.Helper()

	got, err := r.svc.List(r.t.Context())
	require.NoError(r.t, err)

	return got
}

func TestListBeforeAnyPass(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	assert.Equal(t, ThreadList{
		Status: Status{Reading: true, Access: AccessUnchecked}, Threads: []Thread{},
	}, r.list(), "no account read yet, and never a null list")
	assertNoRequests(t, r.gh.requests(), "a list never asks GitHub")
}

func TestListShowsEveryThreadReadAndUnread(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	// Twenty-five unread pull requests, more than the Snapshot lists; a read thread about
	// the first, merged; and a read release.
	threads := prThreads(25, start)

	merged := prThread(firstID+100, 1, start.Add(-time.Hour))
	merged.unread, merged.reason = false, "author"

	threads = append(threads, merged, fakeThread{
		id: firstID + 101, unread: false, reason: "subscribed", updated: start.Add(-90 * time.Minute), title: "v1.2.0",
		repo: owner + "/" + exampleRepo, subject: "releases/1", typ: "Release",
	})
	r.gh.set(threads)
	r.gh.configure(func(f *fakeGitHub) { f.states[exampleRef("1")] = github.StateMerged })
	r.mustPass()

	// Another account's thread is never listed.
	otherAccountRow(t, r, firstID+200, start)

	asked := len(r.gh.requests())
	got := r.list()
	assert.Len(t, r.gh.requests(), asked, "a list never asks GitHub")

	assert.Equal(t, r.snapshot().Status, got.Status, "the Snapshot's status, counts and all")
	assert.Equal(t, 27, got.Status.Count)
	assert.Equal(t, 25, got.Status.Unread)
	require.Len(t, got.Threads, 27, "every one, past the Snapshot's newest unread")

	for i := 1; i < len(got.Threads); i++ {
		assert.GreaterOrEqual(t, got.Threads[i-1].UpdatedAt, got.Threads[i].UpdatedAt, "the newest first")
	}

	assert.Equal(t, Thread{
		ID: firstID + 100, Owner: owner, Name: exampleRepo, Kind: item.KindPR, Number: 1, SubjectType: "PullRequest",
		Title: "Retry the sync worker 1", Reason: "author", UpdatedAt: clock.Format(start.Add(-time.Hour)),
		State: threadrules.StateMerged,
	}, got.Threads[25], "read, with its item's state")
	assert.Equal(t, threadrules.StateMerged, got.Threads[0].State, "every thread about the merged pull request")
	assert.True(t, got.Threads[0].Unread)
	assert.Equal(t, "Release", got.Threads[26].SubjectType)
	assert.Equal(t, threadrules.StateUnknown, got.Threads[26].State, "a release has no state")

	r.rules.off.Store(true)

	off := r.list()
	assert.False(t, off.Status.Reading)
	assert.Empty(t, off.Threads, "nothing with reading off")
	assert.NotNil(t, off.Threads)
	assert.Zero(t, off.Status.Count)
}

func TestListFailsWhenTheMirrorCantBeRead(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set(prThreads(1, start))
	r.mustPass()

	testutil.Exec(t, r.db, `DROP TABLE gh_threads`)

	_, err := r.svc.List(t.Context())
	require.ErrorContains(t, err, "read the GitHub notifications")

	testutil.Exec(t, r.db, `DROP TABLE prefs`)

	_, err = r.svc.List(t.Context())
	require.Error(t, err, "nor the state of the passes")
}

func TestAChangeToAReadThreadIsTold(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	read := prThread(firstID, 1, start)
	read.unread = false
	r.gh.set([]fakeThread{read})

	r.mustPass()
	assert.Equal(t, []event.Event{event.GHThreadsUpdated{}}, r.events.All(), "the first pass found a thread")

	before := r.snapshot()
	assert.Empty(t, before.Unread, "nothing the Snapshot lists")

	// GitHub merges its pull request; the thread doesn't move, so only the state recheck
	// sees it.
	r.gh.configure(func(f *fakeGitHub) { f.states[exampleRef("1")] = github.StateMerged })
	r.clock.Advance(StateRecheck + time.Minute)
	r.mustPass()

	after := r.snapshot()
	assert.Equal(t, before.Status.Count, after.Status.Count)
	assert.Equal(t, before.Unread, after.Unread, "nothing the window sees in the Snapshot changed")
	assert.Equal(t, 2, r.events.Count(), "but the page's list did, so the window hears it")
	assert.Equal(t, threadrules.StateMerged, r.list().Threads[0].State)

	r.clock.Advance(5 * time.Minute)
	r.mustPass()
	assert.Equal(t, 2, r.events.Count(), "a pass that changed nothing tells nothing")
}
