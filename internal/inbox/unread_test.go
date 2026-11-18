package inbox

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
)

// storedSeen returns the seen stamp stored for the item with a number, nil when it was
// never seen.
func (h *harness) storedSeen(t *testing.T, number int) *time.Time {
	t.Helper()

	rows := h.rows(t, `SELECT seen_activity_at FROM items WHERE number = ?`, number)
	require.Len(t, rows, 1)

	switch v := rows[0]["seen_activity_at"].(type) {
	case nil:
		return nil
	case time.Time:
		return &v
	default:
		require.Failf(t, "unexpected seen stamp", "%T %v", v, v)

		return nil
	}
}

// requireSeenAt checks that the item with a number is stored seen at at.
func (h *harness) requireSeenAt(t *testing.T, number int, at time.Time) {
	t.Helper()

	seen := h.storedSeen(t, number)
	require.NotNil(t, seen, "#%d is seen", number)
	assert.True(t, seen.Equal(at), "#%d is seen at %v, not %v", number, at, *seen)
}

// unreadNumbers returns the numbers of the unread items in either list, in order.
func unreadNumbers(s Snapshot) []int {
	out := []int{}

	for _, l := range s.Lists() {
		for _, it := range l.Items {
			if it.Unread {
				out = append(out, it.Number)
			}
		}
	}

	return out
}

func TestAnItemIsUnreadUntilItsActivityIsSeen(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	items := []github.Item{pr(1, "OWNER", false), pr(2, "OWNER", false), pr(3, "OWNER", false), pr(4, "OWNER", false)}
	snap := h.seed(t, items, nil, nil)
	require.Empty(t, unreadNumbers(snap))

	activity := func(number int) time.Time { return items[number-1].UpdatedAt }

	h.exec(t, `UPDATE items SET seen_activity_at = NULL WHERE number = 1`)
	h.exec(t, `UPDATE items SET seen_activity_at = ? WHERE number = 2`, activity(2).Add(-time.Second))
	h.exec(t, `UPDATE items SET seen_activity_at = ? WHERE number = 3`, activity(3))
	h.exec(t, `UPDATE items SET seen_activity_at = ? WHERE number = 4`, activity(4).Add(time.Hour))

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []int{2, 1}, unreadNumbers(snap),
		"never seen, or seen before its last activity, is unread; seen at it or after is read")

	// A routine's Watching list reads it as the lists do.
	ids := []int64{anyItem(t, snap, 1).ID, anyItem(t, snap, 2).ID, anyItem(t, snap, 3).ID, anyItem(t, snap, 4).ID}
	rows, err := h.svc.ItemsByID(t.Context(), ids)
	require.NoError(t, err)

	unread := map[int]bool{}
	for _, it := range rows {
		unread[it.Number] = it.Unread
	}

	assert.Equal(t, map[int]bool{1: true, 2: true, 3: false, 4: false}, unread)
}

func TestTheFirstLoadOfAnAccountStoresItsItemsSeen(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	first, second := pr(1, "OWNER", false), issue("example-repo", 2, "OWNER")
	snap := h.seed(t, []github.Item{first}, nil, []github.Item{second})

	assert.Empty(t, unreadNumbers(snap), "a first sign-in isn't a wall of dots")
	h.requireSeenAt(t, 1, first.UpdatedAt)
	h.requireSeenAt(t, 2, second.UpdatedAt)

	// A later refresh stores a new item unread, and leaves what the user saw of the items
	// stored: newer activity on one makes it unread.
	h.clock.Advance(DefaultInterval)

	moved := first
	moved.UpdatedAt = first.UpdatedAt.Add(time.Hour)
	snap = h.seed(t, []github.Item{moved, pr(3, "OWNER", false)}, nil, []github.Item{second})

	assert.Equal(t, []int{3, 1}, unreadNumbers(snap))
	assert.Nil(t, h.storedSeen(t, 3), "the new item was never seen")
	h.requireSeenAt(t, 1, first.UpdatedAt)
	h.requireSeenAt(t, 2, second.UpdatedAt)

	// Another account's first load is its own.
	h.clock.Advance(DefaultInterval)
	h.client.inbox = fetched([]github.Item{pr(4, "MEMBER", false)}, nil, nil)
	h.client.inbox.Viewer.Login = "octo-second"
	snap = h.refresh(t)

	assert.Equal(t, []int{4}, numbers(snap.ReviewRequested))
	assert.Empty(t, unreadNumbers(snap))
	h.requireSeenAt(t, 4, pr(4, "MEMBER", false).UpdatedAt)
}

func TestAStoredFixtureCountsAsAFirstLoad(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	f := Fixture{Viewer: octoUser, At: h.clock.Now(), Items: []FixtureItem{{Item: pr(1, "OWNER", false), Authored: true}}}

	_, err := StoreFixture(t.Context(), h.pool, f)
	require.NoError(t, err)
	h.requireSeenAt(t, 1, pr(1, "OWNER", false).UpdatedAt)

	f.Items = []FixtureItem{{Item: pr(2, "OWNER", false), Authored: true}}
	_, err = StoreFixture(t.Context(), h.pool, f)
	require.NoError(t, err)
	assert.Nil(t, h.storedSeen(t, 2), "a later fixture's item starts unread")
}

func TestAnItemsOwnReadWithNewerActivityMakesItUnread(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	it := pr(1, "OWNER", false)
	snap := h.seed(t, []github.Item{it}, nil, nil)
	require.False(t, anyItem(t, snap, 1).Unread)

	id := anyItem(t, snap, 1).ID

	read := func(it github.Item) Snapshot {
		t.Helper()

		it.State = github.StateOpen
		h.client.items = map[string]*github.ItemUpdate{
			nodeID(1): {Found: true, Item: it, Details: prDetails(), RateLimit: itemRate()},
		}

		got, err := h.svc.RefreshItem(t.Context(), id)
		require.NoError(t, err)

		return got
	}

	snap = read(it)
	assert.False(t, anyItem(t, snap, 1).Unread, "a read of the activity seen keeps it read")

	moved := it
	moved.UpdatedAt = it.UpdatedAt.Add(time.Minute)
	snap = read(moved)
	assert.True(t, anyItem(t, snap, 1).Unread, "newer activity is unread")
	h.requireSeenAt(t, 1, it.UpdatedAt)
}

func TestAWatchedItemIsStoredSeen(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		// The first load stores #1; the routine's search then brings #7, new to the
		// inbox, after it.
		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		h.requireSeenAt(t, 7, dependabot(7).UpdatedAt)

		stored := h.storedItems(t)
		ids := make([]int64, 0, len(stored))

		for _, row := range stored {
			ids = append(ids, row.ID)
		}

		rows, err := h.svc.ItemsByID(t.Context(), ids)
		require.NoError(t, err)
		require.Len(t, rows, 2)

		for _, it := range rows {
			assert.False(t, it.Unread, "#%d isn't news", it.Number)
		}
	})
}
