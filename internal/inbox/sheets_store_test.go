package inbox

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// launchItemOf reads the one stored item with id as a launch reads it.
func (h *harness) launchItemOf(t *testing.T, id int64) LaunchItem {
	t.Helper()

	got, err := h.store().LaunchItems(t.Context(), []int64{id})
	require.NoError(t, err)
	require.Len(t, got, 1)

	return got[0]
}

func TestALaunchNamesTheChecksThatFailedAtTheHead(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	details := prDetails()
	details.HeadOID = headA
	h.client.details = map[string]github.ItemDetails{nodeID(1): details}
	h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)

	id := h.storedItemNumbered(t, 1).ID
	checks := func(head string) github.HeadChecks {
		return github.HeadChecks{Head: head, Complete: true, Checks: []github.HeadCheck{
			{Kind: github.CheckKindRun, Name: "test", Workflow: "CI", Outcome: github.CheckFailed},
			{Kind: github.CheckKindRun, Name: "lint", Workflow: "CI", Outcome: github.CheckPassed},
			{Kind: github.CheckKindStatus, Name: "codecov/patch", Outcome: github.CheckFailed},
			{Kind: github.CheckKindRun, Name: "test", Workflow: "CI", Outcome: github.CheckFailed},
		}}
	}

	assert.Equal(t, []string{}, h.launchItemOf(t, id).FailedChecks, "no check names read yet")

	require.NoError(t, upsertChecks(t.Context(), h.svc.q, id, checks(headB), h.clock.Now()))
	assert.Equal(t, []string{}, h.launchItemOf(t, id).FailedChecks, "a read of another commit says nothing of the head")

	require.NoError(t, upsertChecks(t.Context(), h.svc.q, id, checks(headA), h.clock.Now()))
	assert.Equal(t, []string{"CI / test", "codecov/patch"}, h.launchItemOf(t, id).FailedChecks,
		"each failed check once, as a routine's conditions name them")

	h.exec(t, `UPDATE item_checks SET checks = '[1]' WHERE item_id = ?`, id)

	_, err := h.store().LaunchItems(t.Context(), []int64{id})
	require.ErrorContains(t, err, fmt.Sprintf("the checks of item %d", id))

	h.exec(t, `UPDATE item_details SET labels = '[1]' WHERE item_id = ?`, id)

	_, err = h.store().LaunchItems(t.Context(), []int64{id})
	require.ErrorContains(t, err, fmt.Sprintf("the labels of item %d", id))
}

func TestTheSheetsReadsNameTheItemWhoseStoredColumnCantBeRead(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		column, want string
		issue        bool
	}{
		{column: "labels", want: "the labels of item %d"},
		{column: "review_users", want: "the review requests of item %d"},
		{column: "review_teams", want: "the review requests of item %d"},
		{column: "checks_held", want: "the held workflow runs of item %d"},
		{column: "labels", want: "the labels of item %d", issue: true},
		{column: "projects", want: "the projects of item %d", issue: true},
	} {
		t.Run(fmt.Sprintf("%s of an issue: %t", c.column, c.issue), func(t *testing.T) {
			t.Parallel()

			h, ids := factsHarness(t)

			id := ids[1]
			read := func() error {
				_, err := h.store().ReviewItem(t.Context(), id)

				return err
			}

			if c.issue {
				id = ids[2]
				read = func() error {
					_, err := h.store().IssueItem(t.Context(), id)

					return err
				}

				h.client.details = map[string]github.ItemDetails{nodeID(1): prDetails(), nodeID(2): {}}
				h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, []github.Item{issue("other-repo", 2, "MEMBER")})
				h.refresh(t)
			}

			require.NoError(t, read(), "the sheet reads it before the column is spoiled")

			h.exec(t, "UPDATE item_details SET "+c.column+" = '[1]' WHERE item_id = ?", id)

			require.ErrorContains(t, read(), fmt.Sprintf(c.want, id))
		})
	}
}

func TestTheSheetsReadsReportAnUnreadableDatabase(t *testing.T) {
	t.Parallel()

	s := NewStore(testutil.ClosedDB(t), nil)

	_, err := s.ReviewItem(t.Context(), 1)
	require.ErrorContains(t, err, "read the item")

	_, err = s.IssueItem(t.Context(), 1)
	require.ErrorContains(t, err, "read the item")
}
