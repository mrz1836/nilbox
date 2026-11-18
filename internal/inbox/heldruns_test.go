package inbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
)

// checksPassed is GitHub's overall result of a head commit whose checks all passed.
const checksPassed = "SUCCESS"

// heldDetails are a fork's approved pull request with its checks passed, whose merge GitHub
// reports as mergeState, holding a run GitHub approves only on its site, as a run a review
// started does.
func heldDetails(mergeState string) github.ItemDetails {
	d := prDetails()
	d.MergeState = mergeState
	d.Checks = github.Checks{State: checksPassed, Passed: 23, AwaitingOnGitHub: 1}

	return d
}

func TestARunHeldOnGitHubDoesntCount(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", true)}, nil, nil)
	h.client.details = map[string]github.ItemDetails{nodeID(1): heldDetails("UNSTABLE")}

	snap := h.refresh(t)

	assert.Equal(t, Checks{State: checksPassed, Passed: 23}, itemByNumber(t, snap.ReviewRequested, 1).Details.Checks,
		"23 checks passed and the run GitHub holds on its site counts toward nothing: the card reads as passed")

	rows, err := h.store().FactsRows(t.Context(), []int64{itemByNumber(t, snap.ReviewRequested, 1).ID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].Details.ChecksAwaiting, "a routine still counts every held run")
}

func TestABlockedMergeCountsTheRunHeldOnGitHub(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", true)}, nil, nil)
	h.client.details = map[string]github.ItemDetails{nodeID(1): heldDetails("BLOCKED")}

	snap := h.refresh(t)

	assert.Equal(t, Checks{State: checksPassed, Passed: 23, AwaitingOnGitHub: 1},
		itemByNumber(t, snap.ReviewRequested, 1).Details.Checks,
		"while GitHub reports the merge blocked, the run held on GitHub may be what blocks it, and counts")
}
