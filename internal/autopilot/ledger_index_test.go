package autopilot_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// The window reads an item's matches (its chip, its history) whenever Autopilot changes,
// so the ledger finds them by item without reading every match.
func TestTheLedgerFindsAnItemsMatchesByItsIndex(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)

	rows, err := db.QueryContext(t.Context(), `EXPLAIN QUERY PLAN SELECT id FROM autopilot_matches WHERE item_id IN (1, 2, 3)`)
	require.NoError(t, err)

	defer func() { require.NoError(t, rows.Close()) }()

	var plan []string

	for rows.Next() {
		var id, parent, unused int

		var detail string

		require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
		plan = append(plan, detail)
	}

	require.NoError(t, rows.Err())
	assert.Contains(t, strings.Join(plan, "\n"), "SEARCH autopilot_matches USING COVERING INDEX autopilot_matches_item (item_id=?)")
}
