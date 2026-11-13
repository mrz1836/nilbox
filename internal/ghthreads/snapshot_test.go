package ghthreads

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/item"
)

func TestSnapshotBeforeAnyPass(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	assert.Equal(t, Snapshot{
		Status: Status{Reading: true, Access: AccessUnchecked},
		ByItem: []ItemThreads{},
		Unread: []Thread{},
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
		Title: "Retry the sync worker 1", Reason: "review_requested", UpdatedAt: clock.Format(start),
	}, snap.Unread[0])

	for i, thread := range snap.Unread {
		assert.Equal(t, firstID+int64(i), thread.ID, "newest first")
	}

	require.Len(t, snap.ByItem, 26, "one entry per pull request or issue; the release has none")
	assert.Equal(t, ItemThreads{
		Owner: owner, Name: exampleRepo, Kind: item.KindPR, Number: 1, Unread: 1, Total: 2,
		Reasons: []string{"review_requested", "mention"},
	}, snap.ByItem[0], "both threads about the first pull request, the newest's reason first")
	assert.Equal(t, ItemThreads{
		Owner: owner, Name: exampleRepo, Kind: item.KindIssue, Number: 70, Total: 1, Reasons: []string{"author"},
	}, snap.ByItem[25])

	for _, entry := range snap.ByItem[1:25] {
		assert.Equal(t, 1, entry.Total, strconv.Itoa(entry.Number))
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
	assert.Empty(t, snap.ByItem, "but about no item")
	require.Len(t, snap.Unread, 1)
	assert.Equal(t, Thread{
		ID: firstID, Owner: owner, Name: exampleRepo, SubjectType: "CheckSuite", Title: "CI failed", Reason: "ci_activity",
		UpdatedAt: clock.Format(start),
	}, snap.Unread[0])
}
