package inbox

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/reporules"
)

// acmeOrg is the org the fake inbox's items are in.
const acmeOrg = "acme-org"

// setRules stores the pinned and muted repositories as settings would, past the service,
// so its snapshot cache is left as it was.
func setRules(t *testing.T, h *harness, pinned, muted []string) {
	t.Helper()

	rules := reporules.Rules{Pinned: pinned, Muted: muted}
	require.NoError(t, prefs.New(h.pool, nil).SetMany(t.Context(), rules.Encode()))
}

// inRepo is a fetched item in owner/name.
func inRepo(owner, name string, number int, isPR bool) github.Item {
	it := githubItem(name, number, "MEMBER", isPR, false)
	it.RepoOwner = owner
	it.URL = fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, name, number)

	return it
}

// rulesOf lists each item of l as "#number rule", in the list's order.
func rulesOf(l List) []string {
	out := make([]string, 0, len(l.Items))
	for _, it := range l.Items {
		out = append(out, fmt.Sprintf("#%d %s", it.Number, it.RepoRule))
	}

	return out
}

func TestSnapshotMarksEachItemsRepoRuleAndPinsItsRepositorysItems(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	snap := h.seed(t, nil, nil, []github.Item{
		inRepo(acmeOrg, "example-repo", 1, true),
		inRepo(acmeOrg, "other-repo", 2, false),
		inRepo("beta-org", "beta-repo", 3, false),
		inRepo(acmeOrg, "pinned-repo", 4, true),
		inRepo("beta-org", "beta-repo", 5, false),
	})
	assert.Equal(t, []string{"#5 ", "#4 ", "#3 ", "#2 ", "#1 "}, rulesOf(snap.Assigned), "newest first, no rules")

	_, err := h.svc.Pin(t.Context(), itemByNumber(t, snap.Assigned, 2).ID)
	require.NoError(t, err)

	setRules(t, h, []string{"acme-org/pinned-repo", "Beta-Org/beta-repo"}, []string{acmeOrg})

	snap, err = h.svc.Snapshot(t.Context())
	require.NoError(t, err, "a rule stored past the service is read: the kept snapshot was built under others")
	assert.Equal(t, []string{"#2 muted", "#5 pinned", "#4 pinned", "#3 pinned", "#1 muted"}, rulesOf(snap.Assigned),
		"the item pinned itself first, then those pinned with their repository, each newest first, then the rest; "+
			"a repository's own pin wins over its org's mute")

	setRules(t, h, []string{}, []string{})

	snap, err = h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"#2 ", "#5 ", "#4 ", "#3 ", "#1 "}, rulesOf(snap.Assigned), "taking the rules off")
}

func TestAMutedItemNeedsNoOne(t *testing.T) {
	t.Parallel()

	muted := Item{ID: 1, RepoRule: reporules.RuleMuted}
	pinned := Item{ID: 2, RepoRule: reporules.RulePinned}
	snoozedMuted := Item{ID: 3, RepoRule: reporules.RuleMuted, SnoozedUntil: "2026-03-04T13:00:00Z"}
	snoozed := Item{ID: 4, SnoozedUntil: "2026-03-05T09:30:00Z"}

	assert.True(t, muted.Muted())
	assert.False(t, muted.NeedsYou(needsYouNow))
	assert.True(t, pinned.NeedsYou(needsYouNow))
	assert.False(t, Item{ID: 5, RepoRule: reporules.RuleMuted, PinnedAt: "2026-03-01T00:00:00Z"}.NeedsYou(needsYouNow),
		"an item pinned itself in a muted repository is muted too")

	snap := Snapshot{
		ReviewRequested: List{Items: []Item{muted, pinned}},
		Authored:        List{Items: []Item{snoozedMuted, snoozed}},
		Assigned:        List{Items: []Item{muted}},
	}

	assert.Equal(t, 1, snap.NeedsYou(needsYouNow), "only the pinned repository's item")
	assert.Equal(t, []Item{pinned}, snap.ReviewRequested.Waiting(needsYouNow))
	assert.Equal(t, "2026-03-05T09:30:00Z", snap.NextSnoozeEnd(needsYouNow).Format("2006-01-02T15:04:05Z"),
		"a muted item's snooze ending changes no count")
}

func TestAMutedRepositorysItemsDoNotArrive(t *testing.T) {
	t.Parallel()

	h, sink := arrivalHarness(t)
	h.seed(t, []github.Item{pr(1, "MEMBER", false)}, nil, nil)

	setRules(t, h, []string{}, []string{"acme-org/muted-repo", "beta-org"})

	h.tick()
	h.client.inbox = fetched(
		[]github.Item{pr(1, "MEMBER", false), byOther(inRepo(acmeOrg, "muted-repo", 3, true))},
		nil,
		[]github.Item{byOther(issue("example-repo", 4, "MEMBER")), byOther(inRepo("beta-org", "beta-repo", 5, false))},
	)
	h.refresh(t)

	require.Len(t, sink.calls, 1)
	assert.Equal(t, []string{"assigned #4"}, kindsAndNumbers(sink.last()), "neither a muted repository's nor a muted org's")
}

func TestTheChecksFollowLeavesAMutedRepositoryAlone(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false), inRepo(acmeOrg, "muted-repo", 2, true)}, nil, nil)
	h.client.details = map[string]github.ItemDetails{nodeID(1): withChecks("PENDING"), nodeID(2): withChecks("PENDING")}
	h.refresh(t)

	setRules(t, h, []string{}, []string{"acme-org/muted-repo"})

	h.clock.Advance(FollowInterval)
	h.client.items = map[string]*github.ItemUpdate{nodeID(1): open(h, 1, withChecks("PENDING"))}

	require.NoError(t, h.svc.refreshRunning(t.Context()))
	assert.Equal(t, [][]string{{nodeID(1)}}, h.client.itemsIDs, "only the pull request whose repository isn't muted")
}

func TestFollowableTakesTheFirstOfThoseNotMuted(t *testing.T) {
	t.Parallel()

	listed := make([]queries.ListRunningChecksRow, 0, 2*followMax)
	for i := range 2 * followMax {
		name := "example-repo"
		if i%2 == 0 {
			name = "muted-repo"
		}

		listed = append(listed, queries.ListRunningChecksRow{ID: int64(i), RepoOwner: acmeOrg, RepoName: name})
	}

	got := followable(listed, reporules.Rules{Muted: []string{"acme-org/muted-repo"}})
	require.Len(t, got, followMax)
	assert.Equal(t, int64(1), got[0].ID)
	assert.Equal(t, int64(2*followMax-1), got[followMax-1].ID, "in the list's order")

	assert.Len(t, followable(listed, reporules.Rules{}), followMax, "at most followMax a round")
	assert.Empty(t, followable(nil, reporules.Rules{}))
}

func TestARefreshChecksNoFilesInAMutedRepository(t *testing.T) {
	t.Parallel()

	quiet := from(2, "outsider", "NONE", false)
	quiet.RepoName = "muted-repo"

	h := filesHarness(t, from(1, "outsider", "NONE", false), quiet)
	setRules(t, h, []string{}, []string{"acme-org/muted-repo"})

	snap := h.refresh(t)

	assert.Equal(t, []string{refKey(1)}, h.client.fileReads, "only the pull request whose repository isn't muted")

	muted := itemByNumber(t, snap.ReviewRequested, 2)
	assert.Equal(t, reporules.RuleMuted, muted.RepoRule)
	assert.NotNil(t, muted.Details, "its details still come with the refresh, so its card is whole")
}
