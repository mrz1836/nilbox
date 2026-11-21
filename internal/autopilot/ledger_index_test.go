package autopilot_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// The window reads an item's matches (its chip, its history) whenever Autopilot changes,
// so the ledger finds them by item without reading every match.
func TestTheLedgerFindsAnItemsMatchesByItsIndex(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)

	plan := testutil.Query(t, db, func(detail *string) []any {
		var id, parent, unused int

		return []any{&id, &parent, &unused, detail}
	}, `EXPLAIN QUERY PLAN SELECT id FROM autopilot_matches WHERE item_id IN (1, 2, 3)`)
	assert.Contains(t, strings.Join(plan, "\n"), "SEARCH autopilot_matches USING COVERING INDEX autopilot_matches_item (item_id=?)")
}
