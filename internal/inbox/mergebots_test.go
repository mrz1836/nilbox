package inbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/trust"
)

// automergeBot is an org's own auto-merge app, as GitHub gives its slug.
const automergeBot = "acme-org-automerge"

// botApproved is a pull request's details whose latest reviews include the approvals of
// bots.
func botApproved(bots ...string) github.ItemDetails {
	d := prDetails()
	d.ApprovingBots = bots

	return d
}

func TestAListedBotsApprovalShowsOnlyWhileOn(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false), pr(2, "OWNER", false)}, nil, []github.Item{
		issue("example-repo", 3, "OWNER"),
	})
	h.client.details = map[string]github.ItemDetails{
		nodeID(1): botApproved("example-helper", automergeBot),
		nodeID(2): botApproved("example-helper"),
		nodeID(3): {Comments: 1},
	}

	snap := h.refresh(t)

	assert.Empty(t, itemByNumber(t, snap.ReviewRequested, 1).Details.MergeBot,
		"off by default, the card names no bot, listed or not")

	stored := h.column(t, `SELECT d.approving_bots FROM item_details d JOIN items i ON i.id = d.item_id
		WHERE i.number = 1`)
	require.Len(t, stored, 1)
	assert.JSONEq(t, `["example-helper", "acme-org-automerge"]`, stored[0], "every bot that approved is stored")

	sent := h.client.requests()

	// Listed but off: nothing yet.
	h.setPrefs(t, map[string]string{trust.PrefMergeBots: `["acme-org-automerge"]`})
	assert.Empty(t, itemByNumber(t, h.snapshot(t).ReviewRequested, 1).Details.MergeBot)

	// On: the card names the listed bot that approved, read from what is stored.
	h.setPrefs(t, map[string]string{trust.PrefMergeBotsOn: "true"})

	snap = h.snapshot(t)
	assert.Equal(t, automergeBot, itemByNumber(t, snap.ReviewRequested, 1).Details.MergeBot)
	assert.Empty(t, itemByNumber(t, snap.ReviewRequested, 2).Details.MergeBot, "only a bot on the list counts")
	assert.Empty(t, itemByNumber(t, snap.Yours, 3).Details.MergeBot, "an issue has no approvals")

	watched, err := h.svc.ItemsByID(t.Context(), []int64{itemByNumber(t, snap.ReviewRequested, 1).ID})
	require.NoError(t, err)
	require.Len(t, watched, 1)
	assert.Equal(t, automergeBot, watched[0].Details.MergeBot, "a routine's Watching list says the same")

	// Taken off the list, or turned off, the card says nothing again.
	h.setPrefs(t, map[string]string{trust.PrefMergeBots: `["example-merger"]`})
	assert.Empty(t, itemByNumber(t, h.snapshot(t).ReviewRequested, 1).Details.MergeBot)

	h.setPrefs(t, map[string]string{trust.PrefMergeBots: `["acme-org-automerge"]`, trust.PrefMergeBotsOn: "false"})
	assert.Empty(t, itemByNumber(t, h.snapshot(t).ReviewRequested, 1).Details.MergeBot)

	assert.Equal(t, sent, h.client.requests(), "no change of the setting asks GitHub anything")
}

func TestDetailsNoBotApprovedStoreNone(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
	h.client.details = map[string]github.ItemDetails{nodeID(1): prDetails()}
	h.refresh(t)

	assert.Equal(t, []string{`[]`}, h.column(t, `SELECT approving_bots FROM item_details`),
		"details read with no bot's approval store none, never null")

	h.setPrefs(t, map[string]string{trust.PrefMergeBotsOn: "true", trust.PrefMergeBots: `["acme-org-automerge"]`})
	assert.Empty(t, itemByNumber(t, h.snapshot(t).ReviewRequested, 1).Details.MergeBot)
}
