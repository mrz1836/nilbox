package inbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestPushTargetsReportWhatTheyCantRead(t *testing.T) {
	t.Parallel()

	_, err := NewStore(testutil.ClosedDB(t), nil).PushTargets(t.Context())
	require.ErrorContains(t, err, "read the pull requests to watch for pushes")

	h := newHarness(t)
	h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)

	got, err := h.store().PushTargets(t.Context())
	require.NoError(t, err)
	require.Len(t, got, 1)

	// Renaming keeps the rows, while every read of the table by its name fails.
	h.exec(t, "ALTER TABLE item_local_state RENAME TO item_local_state_gone")

	_, err = h.store().PushTargets(t.Context())
	require.ErrorContains(t, err, "read the pull requests to watch for pushes")
}

func TestAFileCheckReportsAStoredCheckItCantRead(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(12, "forker", "CONTRIBUTOR", true))
	id := itemByNumber(t, h.refresh(t).ReviewRequested, 12).ID

	// Renaming keeps the rows, while every read of the table by its name fails.
	h.exec(t, "ALTER TABLE item_files RENAME TO item_files_gone")

	_, err := h.svc.CheckFiles(t.Context(), id)
	require.ErrorContains(t, err, "read the item")

	// An item's own refresh checks its files when its head moves, so it reads them first.
	h.client.items = map[string]*github.ItemUpdate{nodeID(12): open(h, 12, prDetails())}

	_, err = h.svc.RefreshItem(t.Context(), id)
	require.ErrorContains(t, err, "read the item")
	assert.Equal(t, "Item 12", h.itemTitle(t, id), "nothing of the read is stored")
}

func TestARefreshThatCantReadTheSettingsSendsNothingAndSaysWhyItCantRecordIt(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)

	sent := len(h.client.gotTokens)

	h.exec(t, "ALTER TABLE prefs RENAME TO prefs_gone")

	_, err := h.svc.Refresh(t.Context())
	require.ErrorContains(t, err, "read the listed items")
	require.ErrorContains(t, err, "record the refresh error", "the failure it couldn't record is said too")
	assert.Len(t, h.client.gotTokens, sent, "nothing is asked of GitHub")
}

func TestARefreshThatCantReadTheCheckedFilesKeepsTheStoredInbox(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(12, "forker", "CONTRIBUTOR", true))
	h.refresh(t)

	stored := h.storedItems(t)

	h.exec(t, "ALTER TABLE item_files RENAME TO item_files_gone")

	h.client.inbox = fetched([]github.Item{from(12, "forker", "CONTRIBUTOR", true), pr(13, "OWNER", false)}, nil, nil)
	h.clock.Advance(DefaultInterval)

	_, err := h.svc.Refresh(t.Context())
	require.ErrorContains(t, err, "read the checked files")
	assert.Equal(t, stored, h.storedItems(t), "nothing of the refresh is stored")

	status, err := h.store().ReadStatus(t.Context())
	require.NoError(t, err)
	assert.Equal(t, h.clock.Now().Add(-DefaultInterval).UTC(), status.LastRefresh, "the last good refresh stands")
}
