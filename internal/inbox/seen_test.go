package inbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
)

// seenHarness signs in as another account first, whose #9 is stored seen, then as the
// user, whose first load stores #1 seen and whose next refresh brings #2, with activity
// before now, and #3, with activity after it, both unread. It returns the snapshot, kept,
// and the other account's #9's id.
func seenHarness(t *testing.T) (*harness, Snapshot, int64) {
	t.Helper()

	h := newHarness(t)

	h.client.inbox = fetched([]github.Item{pr(9, "MEMBER", false)}, nil, nil)
	h.client.inbox.Viewer.Login = "octo-second"
	other := anyItem(t, h.refresh(t), 9).ID

	h.clock.Advance(DefaultInterval)
	h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)

	h.clock.Advance(DefaultInterval)

	future := pr(3, "OWNER", false)
	future.UpdatedAt = h.clock.Now().Add(time.Hour)
	snap := h.seed(t, []github.Item{pr(1, "OWNER", false), pr(2, "OWNER", false), future}, nil, nil)
	require.Equal(t, []int{3, 2}, unreadNumbers(snap))

	return h, snap, other
}

func TestMarkSeenStampsTheLaterOfTheActivityAndNow(t *testing.T) {
	t.Parallel()

	h, snap, other := seenHarness(t)
	before := h.events.Count()

	h.clock.Advance(time.Minute)
	marked := h.clock.Now()

	two, three := anyItem(t, snap, 2).ID, anyItem(t, snap, 3).ID
	require.NoError(t, h.svc.MarkSeen(t.Context(), []int64{two, three, other, 999}))

	h.requireSeenAt(t, 2, marked)
	h.requireSeenAt(t, 3, marked.Add(-time.Minute).Add(time.Hour))
	h.requireSeenAt(t, 9, pr(9, "MEMBER", false).UpdatedAt)

	assert.Equal(t, before, h.events.Count(), "a mark tells no one the inbox changed")

	got, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Empty(t, unreadNumbers(got), "the next snapshot shows them read")

	// A read that lands after the mark, with activity from before it, leaves the item seen;
	// activity after the mark is unread.
	read := func(it github.Item) Snapshot {
		t.Helper()

		it.State = github.StateOpen
		h.client.items = map[string]*github.ItemUpdate{
			it.NodeID: {Found: true, Item: it, Details: prDetails(), RateLimit: itemRate()},
		}

		got, err := h.svc.RefreshItem(t.Context(), two)
		require.NoError(t, err)

		return got
	}

	h.clock.Advance(time.Minute)

	earlier := pr(2, "OWNER", false)
	earlier.UpdatedAt = marked.Add(-time.Second)
	assert.False(t, anyItem(t, read(earlier), 2).Unread)

	later := pr(2, "OWNER", false)
	later.UpdatedAt = marked.Add(time.Second)
	assert.True(t, anyItem(t, read(later), 2).Unread)
}

func TestMarkSeenLeavesAStampAlreadyAsLate(t *testing.T) {
	t.Parallel()

	h, snap, _ := seenHarness(t)
	three := anyItem(t, snap, 3).ID

	require.NoError(t, h.svc.MarkSeen(t.Context(), []int64{three}))
	require.Nil(t, h.svc.snapshots.load().snap, "the mark emptied the kept snapshot")

	_, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)

	// #3's activity is still after now, so marking it again moves nothing, and the kept
	// snapshot stays.
	require.NoError(t, h.svc.MarkSeen(t.Context(), []int64{three}))
	assert.NotNil(t, h.svc.snapshots.load().snap, "nothing changed, so the snapshot is kept")
}

func TestMarkUnseenMakesAnItemUnread(t *testing.T) {
	t.Parallel()

	h, snap, other := seenHarness(t)
	before := h.events.Count()
	one := anyItem(t, snap, 1).ID

	require.NoError(t, h.svc.MarkUnseen(t.Context(), one))
	assert.Nil(t, h.storedSeen(t, 1))

	got, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []int{3, 2, 1}, unreadNumbers(got), "the next snapshot shows it unread")

	// Another account's item, and one never seen, are left alone.
	require.NoError(t, h.svc.MarkUnseen(t.Context(), other))
	h.requireSeenAt(t, 9, pr(9, "MEMBER", false).UpdatedAt)
	require.NoError(t, h.svc.MarkUnseen(t.Context(), one))
	require.NoError(t, h.svc.MarkUnseen(t.Context(), 999))

	assert.Equal(t, before, h.events.Count(), "a mark tells no one the inbox changed")
}

func TestMarksBeforeAnyAccountDoNothing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	require.NoError(t, h.svc.MarkSeen(t.Context(), nil))
	require.NoError(t, h.svc.MarkSeen(t.Context(), []int64{1}))
	require.NoError(t, h.svc.MarkUnseen(t.Context(), 1))
	assert.Zero(t, h.events.Count())
}

func TestAMarkThatCannotWriteFails(t *testing.T) {
	t.Parallel()

	h, snap, _ := seenHarness(t)
	two := anyItem(t, snap, 2).ID

	h.exec(t, `CREATE TRIGGER refuse_seen BEFORE UPDATE OF seen_activity_at ON items
BEGIN SELECT RAISE(ABORT, 'seen refused'); END`)

	require.ErrorContains(t, h.svc.MarkSeen(t.Context(), []int64{two}), "mark the items read: ")
	require.ErrorContains(t, h.svc.MarkUnseen(t.Context(), anyItem(t, snap, 1).ID), "mark the item unread: ")
	assert.Nil(t, h.storedSeen(t, 2))
}
