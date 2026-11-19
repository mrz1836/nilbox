package inbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
)

func TestAFollowThatCantStoreAnAnswerReportsItAndKeepsTheRest(t *testing.T) {
	t.Parallel()

	h, _ := followHarness(t)
	h.clock.Advance(FollowInterval)

	h.client.items = map[string]*github.ItemUpdate{
		nodeID(1): open(h, 1, withChecks("SUCCESS")), nodeID(4): open(h, 4, withChecks("SUCCESS")),
	}

	// #4's answer is stored after #1's, and its write is refused.
	refuse(t, h, "BEFORE UPDATE OF fetched_at ON items", "NEW.number = 4")

	calls := h.events.Count()

	err := h.svc.refreshRunning(t.Context())
	require.ErrorContains(t, err, "follow the running checks: save the item")

	snap, snapErr := h.svc.Snapshot(t.Context())
	require.NoError(t, snapErr)
	assert.Equal(t, "SUCCESS", itemByNumber(t, snap.ReviewRequested, 1).Details.Checks.State, "what was stored before stands")
	assert.Equal(t, "PENDING", itemByNumber(t, snap.ReviewRequested, 4).Details.Checks.State, "what couldn't be stored waits")
	assert.Equal(t, calls+1, h.events.Count(), "and the window hears of what was stored")
}

func TestAFollowReportsWhatItCantRead(t *testing.T) {
	t.Parallel()

	for _, table := range []string{"prefs", "item_details"} {
		t.Run(table, func(t *testing.T) {
			t.Parallel()

			h, _ := followHarness(t)
			h.clock.Advance(FollowInterval)

			// Renaming keeps the rows, while every read of the table by its name fails.
			h.exec(t, "ALTER TABLE "+table+" RENAME TO "+table+"_gone")

			err := h.svc.refreshRunning(t.Context())
			require.ErrorContains(t, err, "read the pull requests whose checks run")
			assert.Empty(t, h.client.itemsIDs, "nothing is asked of GitHub")
		})
	}
}
