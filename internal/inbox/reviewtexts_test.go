package inbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
)

func TestACardCountsTheReviewsThatSaySomethingAsComments(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false), pr(2, "OWNER", false)}, nil, nil)

	said := prDetails()
	said.Comments, said.ReviewTexts = 0, 1 // a bot's note as it approves, and nothing else

	both := prDetails()
	both.Comments, both.ReviewTexts = 2, 3

	h.client.details = map[string]github.ItemDetails{nodeID(1): said, nodeID(2): both}

	snap := h.refresh(t)

	assert.Equal(t, 1, itemByNumber(t, snap.ReviewRequested, 1).Details.Comments,
		"a review's own text counts, as the review sheet counts it")
	assert.Equal(t, 5, itemByNumber(t, snap.ReviewRequested, 2).Details.Comments)

	stored := h.column(t, `SELECT d.comments || ' ' || d.review_texts FROM item_details d
		JOIN items i ON i.id = d.item_id ORDER BY i.number`)
	require.Equal(t, []string{"0 1", "2 3"}, stored,
		"GitHub's own count is stored apart, so a snooze's wake on comments compares it alone")
}
