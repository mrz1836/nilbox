package inbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/heldruns"
)

// checksPassed is GitHub's overall result of a head commit whose checks all passed.
const checksPassed = "SUCCESS"

// heldDetails are a fork's approved pull request with its checks passed, whose merge GitHub
// reports as mergeState, holding runs.
func heldDetails(mergeState string, runs ...github.HeldRun) github.ItemDetails {
	d := prDetails()
	d.MergeState = mergeState
	d.Checks = github.Checks{State: checksPassed, Passed: 23, Held: runs}

	for _, r := range runs {
		if r.OnGitHub {
			d.Checks.AwaitingOnGitHub++
		} else {
			d.Checks.Awaiting++
		}
	}

	return d
}

// requests counts the requests the fake client was sent.
func (c *fakeClient) requests() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.gotTokens)
}

// trigger is the owner's case: a run a review started, which GitHub holds on a fork's pull
// request and approves only on its site.
func trigger() github.HeldRun {
	return github.HeldRun{Workflow: "Auto-merge Trigger", OnGitHub: true}
}

func TestARunHeldOnGitHubDoesntCount(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", true)}, nil, nil)
	h.client.details = map[string]github.ItemDetails{nodeID(1): heldDetails("UNSTABLE", trigger())}

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
	h.client.details = map[string]github.ItemDetails{nodeID(1): heldDetails("BLOCKED", trigger())}

	snap := h.refresh(t)

	assert.Equal(t, Checks{State: checksPassed, Passed: 23, AwaitingOnGitHub: 1},
		itemByNumber(t, snap.ReviewRequested, 1).Details.Checks,
		"while GitHub reports the merge blocked, the run held on GitHub may be what blocks it, and counts")

	// Named, it never counts, blocked or not: the user said so.
	h.setPrefs(t, map[string]string{heldruns.KeyIgnoredWorkflows: `["Auto-merge Trigger"]`})

	assert.Equal(t, Checks{State: checksPassed, Passed: 23},
		itemByNumber(t, h.snapshot(t).ReviewRequested, 1).Details.Checks)
}

func TestIgnoredWorkflowsRecountFromWhatIsStored(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", true)}, nil, nil)
	h.client.details = map[string]github.ItemDetails{
		nodeID(1): heldDetails("UNSTABLE", github.HeldRun{Workflow: "Deploy Preview"}, github.HeldRun{Workflow: "CI"}, trigger()),
	}

	snap := h.refresh(t)
	id := itemByNumber(t, snap.ReviewRequested, 1).ID

	assert.Equal(t, 2, itemByNumber(t, snap.ReviewRequested, 1).Details.Checks.Awaiting,
		"both runs the user can approve count")

	sent := h.client.requests()

	// The user ignores Deploy Preview: an approvable run of a named workflow stops counting at
	// once, read from what is stored.
	h.setPrefs(t, map[string]string{heldruns.KeyIgnoredWorkflows: `["Deploy Preview"]`})

	got := itemByNumber(t, h.snapshot(t).ReviewRequested, 1).Details.Checks
	assert.Equal(t, 1, got.Awaiting)
	assert.Zero(t, got.AwaitingOnGitHub)

	watched, err := h.svc.ItemsByID(t.Context(), []int64{id})
	require.NoError(t, err)
	require.Len(t, watched, 1)
	assert.Equal(t, 1, watched[0].Details.Checks.Awaiting, "a routine's Watching list counts the same")

	// Names match whole and case-sensitively.
	h.setPrefs(t, map[string]string{heldruns.KeyIgnoredWorkflows: `["deploy preview", "CI"]`})
	assert.Equal(t, 1, itemByNumber(t, h.snapshot(t).ReviewRequested, 1).Details.Checks.Awaiting)

	// Taken off the list, it counts again.
	h.setPrefs(t, map[string]string{heldruns.KeyIgnoredWorkflows: `[]`})
	assert.Equal(t, 2, itemByNumber(t, h.snapshot(t).ReviewRequested, 1).Details.Checks.Awaiting)

	assert.Equal(t, sent, h.client.requests(), "no change of the list asks GitHub anything")
}

func TestHeldRunsStoredBeforeTheirNamesCountByKind(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", true)}, nil, nil)
	h.client.details = map[string]github.ItemDetails{
		nodeID(1): heldDetails("UNSTABLE", github.HeldRun{Workflow: "Deploy Preview"}, trigger()),
	}
	h.refresh(t)

	// A row an older build stored has the counts and no names.
	h.exec(t, `UPDATE item_details SET checks_held = '[]'`)
	h.setPrefs(t, map[string]string{heldruns.KeyIgnoredWorkflows: `["Deploy Preview", "Auto-merge Trigger"]`})

	got := itemByNumber(t, h.snapshot(t).ReviewRequested, 1).Details.Checks
	assert.Equal(t, 1, got.Awaiting, "a run of no name matches no ignored workflow, and counts until the next read")
	assert.Zero(t, got.AwaitingOnGitHub, "one held on GitHub doesn't count, named or not")

	// A write outside the service keeps the snapshot it holds, but a change of the list
	// builds the next one again.
	h.exec(t, `UPDATE item_details SET merge_state = 'BLOCKED'`)
	h.setPrefs(t, map[string]string{heldruns.KeyIgnoredWorkflows: `["Auto-merge Trigger"]`})

	got = itemByNumber(t, h.snapshot(t).ReviewRequested, 1).Details.Checks
	assert.Equal(t, 1, got.AwaitingOnGitHub, "until GitHub reports the merge blocked, its name unknown")
}
