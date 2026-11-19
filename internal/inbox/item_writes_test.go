package inbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/github/githubtest"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestAnItemsOwnRefreshThatCantLeaveAListKeepsTheItem(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		update func(h *harness, it github.Item) *github.ItemUpdate
		event  string
		want   string
	}{
		{
			name: "a review GitHub no longer asks for",
			update: func(_ *harness, it github.Item) *github.ItemUpdate {
				return &github.ItemUpdate{Found: true, Item: it, Details: asking([]string{}, []string{})}
			},
			event: "BEFORE UPDATE OF review_left_at ON items", want: "take acme-org/example-repo#1 out of Review requested",
		},
		{
			name: "a merged item a routine's search watches",
			update: func(h *harness, it github.Item) *github.ItemUpdate {
				it.State, it.ClosedAt = github.StateMerged, h.clock.Now()

				return &github.ItemUpdate{Found: true, Item: it, Details: prDetails()}
			},
			event: "BEFORE DELETE ON item_watches", want: "take the item out of the routines' searches",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, id := itemHarness(t)
			h.exec(t, `INSERT INTO item_watches (watch_key, item_id, seen_at) VALUES (?, ?, ?)`, depQuery, id,
				h.clock.Now().UTC())

			now := pr(1, "OWNER", false)
			now.Title = "Item 1, retitled"
			h.client.items = map[string]*github.ItemUpdate{nodeID(1): tt.update(h, now)}

			refuse(t, h, tt.event, "")

			_, err := h.svc.RefreshItem(t.Context(), id)
			require.ErrorContains(t, err, tt.want)
			assert.Equal(t, "Item 1", h.itemTitle(t, id), "the transaction left the item as it was")
			assert.Equal(t, [3]bool{true, false, true}, h.reasons(t, 1))
			assert.Equal(t, int64(1), h.count(t, "item_watches"), "and in the search that watches it")
		})
	}
}

func TestAnItemsOwnRefreshReportsWhatItCantRead(t *testing.T) {
	t.Parallel()

	closed := NewService(Config{Pool: testutil.ClosedDB(t), Tokens: githubtest.NewTokens(), Client: &fakeClient{}})

	_, err := closed.RefreshItem(t.Context(), 1)
	require.ErrorContains(t, err, "refresh the item: read the item")

	h, id := itemHarness(t)

	// Renaming keeps the rows, while every read of the settings by their name fails.
	h.exec(t, "ALTER TABLE prefs RENAME TO prefs_gone")

	_, err = h.svc.RefreshItem(t.Context(), id)
	require.ErrorContains(t, err, "read whether projects can be read")
	assert.Empty(t, h.client.itemIDs, "nothing is asked of GitHub")
}
