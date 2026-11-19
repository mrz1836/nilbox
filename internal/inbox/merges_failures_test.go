package inbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAMergeFollowThatCantReadThePullRequestsIsTriedAgain(t *testing.T) {
	t.Parallel()

	for _, table := range []string{"prefs", "item_local_state"} {
		t.Run(table, func(t *testing.T) {
			t.Parallel()

			h, f, ids := mergeHarness(t)
			mergeFirst(t, h, ids)

			// Renaming keeps the rows, while every read of the table by its name fails.
			h.exec(t, "ALTER TABLE "+table+" RENAME TO "+table+"_gone")

			h.clock.Advance(mergeSettle)
			f.pass(t.Context())

			assert.Contains(t, h.logs.String(), mergeJob+" failed")
			assert.Contains(t, h.logs.String(), "read the pull requests a merge into acme-org/example-repo changed")
			assert.Empty(t, h.client.itemsIDs, "nothing is asked of GitHub")
			assert.True(t, f.owed(), "the follow is owed still")

			h.exec(t, "ALTER TABLE "+table+"_gone RENAME TO "+table)
			h.answer(map[int]string{2: mergeableClean, 3: mergeableClean})
			h.clock.Advance(mergeRetry)
			f.pass(t.Context())

			assert.Len(t, h.client.itemsIDs, 1, "the next round reads them")
			assert.False(t, f.owed())
		})
	}
}

func TestAMergeThatCantLeaveTheRoutinesSearchesIsReported(t *testing.T) {
	t.Parallel()

	h, _, ids := mergeHarness(t)
	h.exec(t, `INSERT INTO item_watches (watch_key, item_id, seen_at) VALUES (?, ?, ?)`, depQuery, ids[1],
		h.clock.Now().UTC())

	refuse(t, h, "BEFORE DELETE ON item_watches", "")

	_, err := NewActor(h.svc).Act(t.Context(), ids[1], acting(Acted{Merged: true}))
	require.ErrorContains(t, err, "take the merged or closed item out of the routines' searches")
	assert.Equal(t, int64(1), h.count(t, "item_watches"))
	assert.Equal(t, [3]bool{true, false, false}, h.reasons(t, 1), "the item stays listed until a read stores the merge")
}
