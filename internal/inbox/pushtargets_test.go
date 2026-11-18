package inbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/repo"
)

// headed is a pull request's details at head.
func headed(head string) github.ItemDetails {
	d := prDetails()
	d.HeadOID = head

	return d
}

// pushTargets reads the push watch's pull requests, which must succeed.
func pushTargets(t *testing.T, h *harness) []PushTarget {
	t.Helper()

	got, err := NewStore(h.pool, h.clock).PushTargets(t.Context())
	require.NoError(t, err)

	return got
}

func TestPushTargetsAreNoneBeforeAnAccountIsKnown(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []PushTarget{}, pushTargets(t, newHarness(t)), "an empty list, never nil")
}

func TestPushTargetsAreTheOpenListedPullRequestsOnTheirOwnBranches(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.details = map[string]github.ItemDetails{
		nodeID(1): headed(headA), nodeID(5): headed(headB), nodeID(9): headed(headA),
	}
	snap := h.seed(t,
		[]github.Item{pr(5, "MEMBER", false)},
		[]github.Item{
			pr(1, "OWNER", false), pr(2, "OWNER", true), inRepo(acmeOrg, "muted-repo", 3, true),
			issue("example-repo", 4, "OWNER"),
		},
		[]github.Item{pr(6, "OWNER", false), pr(7, "OWNER", false), pr(8, "OWNER", false), pr(9, "OWNER", false)},
	)
	idOf := func(l List, number int) int64 { return itemByNumber(t, l, number).ID }

	setRules(t, h, []string{}, []string{"acme-org/muted-repo"})

	_, err := h.svc.Dismiss(t.Context(), idOf(snap.Yours, 6))
	require.NoError(t, err)

	_, err = h.svc.Snooze(t.Context(), idOf(snap.Yours, 7), 1)
	require.NoError(t, err)

	h.exec(t, "UPDATE items SET state = 'MERGED' WHERE id = ?", idOf(snap.Yours, 8))
	h.exec(t, "DELETE FROM item_details WHERE item_id = ?", idOf(snap.Yours, 9))

	exampleRepo := repo.Name{Owner: acmeOrg, Repo: "example-repo"}
	assert.Equal(t, []PushTarget{
		{ID: idOf(snap.Yours, 1), Repo: exampleRepo, Branch: "branch-1", Head: headA},
		{ID: idOf(snap.Yours, 9), Repo: exampleRepo, Branch: "branch-9"},
		{ID: idOf(snap.ReviewRequested, 5), Repo: exampleRepo, Branch: "branch-5", Head: headB},
	}, pushTargets(t, h), "the ones the user opened first, then the newest; never a fork's branch, an issue, a "+
		"muted repository's, a dismissed or snoozed one, or a merged one; a head not yet read is empty")

	h.clock.Advance(2 * 24 * time.Hour)

	got := pushTargets(t, h)
	require.Len(t, got, 4)
	assert.Equal(t, idOf(snap.Yours, 7), got[2].ID, "a snooze that ended watches it again")
}
