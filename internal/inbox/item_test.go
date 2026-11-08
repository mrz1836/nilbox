package inbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/snooze"
)

// itemHarness refreshes an inbox where #1 is in two lists and #2 in one, both with
// details, and returns the harness with #1's ID.
func itemHarness(t *testing.T) (h *harness, id int64) {
	t.Helper()

	h = newHarness(t)
	a, b := pr(1, "OWNER", false), pr(2, "OWNER", false)
	h.client.inbox = fetched([]github.Item{a, b}, nil, []github.Item{a})
	h.client.details = map[string]github.ItemDetails{nodeID(1): prDetails(), nodeID(2): prDetails()}

	snap := h.refresh(t)

	h.clock.Advance(10 * time.Minute)
	h.client.detailsIDs = nil

	return h, itemByNumber(t, snap.ReviewRequested, 1).ID
}

// itemRate is the budget a single item's query leaves.
func itemRate() github.QueryRate {
	return github.QueryRate{
		Cost: 1, Limit: 5000, Remaining: 4970, Used: 30, ResetAt: time.Date(2026, 1, 10, 13, 0, 0, 0, time.UTC),
	}
}

func TestRefreshItemRewritesAnOpenItem(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	calls := h.events.Count()

	now := pr(1, "OWNER", false)
	now.State = github.StateOpen
	now.Title = "Item 1, retitled"
	now.IsDraft = true
	now.UpdatedAt = h.clock.Now()

	details := prDetails()
	details.Checks = github.Checks{State: "FAILURE", Passed: 10, Failed: 2}
	h.client.items = map[string]*github.ItemUpdate{
		nodeID(1): {Found: true, Item: now, Details: details, RateLimit: itemRate()},
	}

	snap, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)

	assert.Equal(t, []string{nodeID(1)}, h.client.itemIDs)
	assert.Nil(t, h.client.detailsIDs, "nothing else was asked about")

	for _, list := range []List{snap.ReviewRequested, snap.Assigned} {
		it := itemByNumber(t, list, 1)
		assert.Equal(t, "Item 1, retitled", it.Title)
		assert.True(t, it.IsDraft)
		assert.Equal(t, "2026-01-10T12:10:00Z", it.UpdatedAt)
		require.NotNil(t, it.Details)
		assert.Equal(t, Checks{State: "FAILURE", Passed: 10, Failed: 2}, it.Details.Checks)
		assert.Equal(t, "2026-01-10T12:10:00Z", it.Details.FetchedAt)
	}

	assert.Equal(t, [3]bool{true, false, true}, h.reasons(t, 1), "it stays in the lists it was in")
	assert.Equal(t, "Item 2", itemByNumber(t, snap.ReviewRequested, 2).Title)

	assert.Equal(t, 4970, snap.Rate.GraphQL.Remaining, "the item query's budget reading is the latest")
	assert.Equal(t, 1, snap.Rate.GraphQL.Cost)
	assert.Equal(t, 4980, snap.Rate.Core.Remaining, "the REST budgets keep their readings")
	assert.Equal(t, "2026-01-10T12:00:00Z", snap.LastRefreshedAt, "a single item is not a whole refresh")
	assert.Equal(t, calls+1, h.events.Count())
}

func TestRefreshItemTakesAnItemThatIsNoLongerOpenOutOfTheLists(t *testing.T) {
	t.Parallel()

	merged := pr(1, "OWNER", false)
	merged.State = github.StateMerged

	closed := pr(1, "OWNER", false)
	closed.State = github.StateClosed

	archived := pr(1, "OWNER", false)
	archived.State = github.StateOpen
	archived.Repo.IsArchived = true

	tests := []struct {
		name   string
		update github.ItemUpdate
	}{
		{name: "merged", update: github.ItemUpdate{Found: true, Item: merged, Details: prDetails()}},
		{name: "closed", update: github.ItemUpdate{Found: true, Item: closed, Details: prDetails()}},
		{name: "deleted or out of sight", update: github.ItemUpdate{}},
		{name: "archived repository", update: github.ItemUpdate{Found: true, Item: archived, Details: prDetails()}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, id := itemHarness(t)
			tt.update.RateLimit = itemRate()
			h.client.items = map[string]*github.ItemUpdate{nodeID(1): &tt.update}

			snap, err := h.svc.RefreshItem(t.Context(), id)
			require.NoError(t, err)

			assert.Equal(t, []int{2}, numbers(snap.ReviewRequested))
			assert.Empty(t, snap.Assigned.Items)
			assert.Equal(t, [3]bool{false, false, false}, h.reasons(t, 1))
			assert.Equal(t, 4970, snap.Rate.GraphQL.Remaining)
		})
	}
}

func TestRefreshItemRecordsAnArchivedRepository(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)

	archived := pr(1, "OWNER", false)
	archived.Repo.IsArchived = true
	h.client.items = map[string]*github.ItemUpdate{nodeID(1): {Found: true, Item: archived}}

	_, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)

	assert.True(t, h.repoRow(t, "example-repo").IsArchived)
}

func TestARefreshDoesNotBringBackAnItemMergedSinceIndexed(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)

	merged := pr(1, "OWNER", false)
	merged.State = github.StateMerged
	h.client.items = map[string]*github.ItemUpdate{nodeID(1): {Found: true, Item: merged}}

	_, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)

	// GitHub's index has not caught up, so the searches still return #1, but with its
	// state as of now.
	h.client.inbox = fetched([]github.Item{merged, pr(2, "OWNER", false)}, nil, []github.Item{merged})
	snap := h.refresh(t)

	assert.Equal(t, []int{2}, numbers(snap.ReviewRequested))
	assert.Empty(t, snap.Assigned.Items)
	assert.Equal(t, 2, snap.ReviewRequested.Total, "totals stay as GitHub counted them")
	assert.Equal(t, [][]string{{nodeID(2)}}, h.client.detailsIDs, "nothing is asked about the merged item")
}

func TestRefreshItemCanEndASnooze(t *testing.T) {
	t.Parallel()

	h, id, details := wakeHarness(t)

	_, err := h.svc.Snooze(t.Context(), id, 3)
	require.NoError(t, err)

	h.clock.Advance(time.Hour)

	details.Commits = 5
	details.HeadOID = "2222222222222222222222222222222222222222"

	now := pr(1, "OWNER", false)
	now.State = github.StateOpen
	h.client.items = map[string]*github.ItemUpdate{nodeID(1): {Found: true, Item: now, Details: details}}

	snap, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)

	it := itemByNumber(t, snap.ReviewRequested, 1)
	assert.Equal(t, "2026-01-10T13:00:00Z", it.SnoozedUntil, "the snooze ends at the refresh that saw the commits")
	require.NotNil(t, it.WokeBy)
	assert.Equal(t, snooze.Reasons{Commits: 2}, *it.WokeBy)
}

func TestRefreshItemFailuresKeepTheStoredInbox(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		id      func(id int64) int64
		prepare func(h *harness)
		wantErr error
	}{
		{
			name:    "unknown item",
			id:      func(id int64) int64 { return id + 100 },
			prepare: func(*harness) {},
			wantErr: item.ErrNotFound,
		},
		{
			name:    "no token",
			id:      func(id int64) int64 { return id },
			prepare: func(h *harness) { h.tokens.err = errFakeNetwork },
			wantErr: errFakeNetwork,
		},
		{
			name:    "the fetch fails",
			id:      func(id int64) int64 { return id },
			prepare: func(h *harness) { h.client.itemErr = errFakeNetwork },
			wantErr: errFakeNetwork,
		},
		{
			name:    "no answer",
			id:      func(id int64) int64 { return id },
			prepare: func(h *harness) { h.client.items = map[string]*github.ItemUpdate{nodeID(1): nil} },
			wantErr: ErrIncompleteFetch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, id := itemHarness(t)
			calls := h.events.Count()
			tt.prepare(h)

			_, err := h.svc.RefreshItem(t.Context(), tt.id(id))
			require.ErrorIs(t, err, tt.wantErr)
			require.ErrorContains(t, err, "refresh the item")

			snap, err := h.svc.Snapshot(t.Context())
			require.NoError(t, err)
			assert.Empty(t, snap.LastError, "a single item's failure is not the inbox's last error")
			assert.Equal(t, "Item 1", itemByNumber(t, snap.ReviewRequested, 1).Title)
			assert.Equal(t, [3]bool{true, false, true}, h.reasons(t, 1))
			assert.Equal(t, calls, h.events.Count())
		})
	}
}

func TestRefreshItemNeedsANodeID(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	h.exec(t, "UPDATE items SET node_id = '' WHERE id = ?", id)

	_, err := h.svc.RefreshItem(t.Context(), id)
	require.ErrorIs(t, err, item.ErrNotFound)
	assert.Empty(t, h.client.itemIDs, "GitHub was not asked")
}

func TestRefreshItemDatabaseErrors(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	h.exec(t, "DROP TABLE rate_snapshots")

	now := pr(1, "OWNER", false)
	h.client.items = map[string]*github.ItemUpdate{nodeID(1): {Found: true, Item: now}}

	_, err := h.svc.RefreshItem(t.Context(), id)
	require.ErrorContains(t, err, "save the item")
	assert.Equal(t, "Item 1", h.itemTitle(t, id), "the transaction left the item as it was")
}

// itemTitle reads an item's stored title.
func (h *harness) itemTitle(t *testing.T, id int64) string {
	t.Helper()

	titles := h.column(t, "SELECT title FROM items WHERE id = ?", id)
	require.Len(t, titles, 1)

	return titles[0]
}

func TestDropClosed(t *testing.T) {
	t.Parallel()

	open := pr(1, "OWNER", false)
	open.State = github.StateOpen

	unknown := pr(2, "OWNER", false)

	merged := pr(3, "OWNER", false)
	merged.State = github.StateMerged

	closed := issue("example-repo", 4, "OWNER")
	closed.State = github.StateClosed

	inbox := fetched([]github.Item{open, merged}, []github.Item{unknown, merged}, []github.Item{closed, open})

	dropClosed(inbox)

	assert.Equal(t, []github.Item{open}, inbox.ReviewRequested.Items)
	assert.Equal(t, []github.Item{unknown}, inbox.Authored.Items, "an item whose state is unknown stays")
	assert.Equal(t, []github.Item{open}, inbox.Assigned.Items)
	assert.Equal(t, 2, inbox.ReviewRequested.IssueCount, "totals stay as GitHub counted them")
}

func TestRefreshItemThatCannotBeSavedKeepsTheItem(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		update func(it github.Item) *github.ItemUpdate
		event  string
		want   string
	}{
		{
			name:   "item",
			update: func(it github.Item) *github.ItemUpdate { return &github.ItemUpdate{Found: true, Item: it} },
			event:  "BEFORE UPDATE OF title ON items", want: "update the item",
		},
		{
			name: "details",
			update: func(it github.Item) *github.ItemUpdate {
				return &github.ItemUpdate{Found: true, Item: it, Details: prDetails()}
			},
			event: "BEFORE UPDATE ON item_details", want: "save the details of acme-org/example-repo#1",
		},
		{
			name:   "unlisted",
			update: func(github.Item) *github.ItemUpdate { return &github.ItemUpdate{} },
			event:  "BEFORE UPDATE OF reason_review_requested ON items", want: "take the item out of the lists",
		},
		{
			name: "archived repository",
			update: func(it github.Item) *github.ItemUpdate {
				it.Repo.IsArchived = true

				return &github.ItemUpdate{Found: true, Item: it}
			},
			event: "BEFORE UPDATE ON repos", want: "save the repository acme-org/example-repo",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, id := itemHarness(t)

			now := pr(1, "OWNER", false)
			now.Title = "Item 1, retitled"
			h.client.items = map[string]*github.ItemUpdate{nodeID(1): tt.update(now)}

			refuse(t, h, tt.event, "")

			_, err := h.svc.RefreshItem(t.Context(), id)
			require.ErrorContains(t, err, tt.want)
			assert.Equal(t, "Item 1", h.itemTitle(t, id), "the transaction left the item as it was")
			assert.Equal(t, [3]bool{true, false, true}, h.reasons(t, 1))
		})
	}
}
