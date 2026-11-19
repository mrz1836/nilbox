package inbox

import (
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/github/githubtest"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestItemsByIDReportsWhatItCantRead(t *testing.T) {
	t.Parallel()

	closed := NewService(Config{Pool: testutil.ClosedDB(t), Tokens: githubtest.NewTokens(), Client: &fakeClient{}})

	got, err := closed.ItemsByID(t.Context(), []int64{1})
	require.ErrorContains(t, err, "read the inbox status")
	assert.NotNil(t, got, "the list is never nil")

	h, ids := factsHarness(t)

	none, err := newHarness(t).svc.ItemsByID(t.Context(), []int64{ids[1]})
	require.NoError(t, err)
	assert.Empty(t, none, "with no one signed in, none")

	h.exec(t, `UPDATE item_details SET labels = '[1]' WHERE item_id = ?`, ids[1])

	_, err = h.svc.ItemsByID(t.Context(), []int64{ids[1], ids[2]})
	require.ErrorContains(t, err, "read the items")

	h.exec(t, "DROP TABLE item_details")

	_, err = h.svc.ItemsByID(t.Context(), []int64{ids[1]})
	require.ErrorContains(t, err, "read the items")
}

func TestReadChecksReportsWhatItCouldntReadOrStore(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h, ids := routinesHarness(t)
		defer h.stop()

		routines := NewRoutines(h.svc)
		h.client.checks = map[string]github.HeadChecks{nodeID(7): {Head: headA, Complete: true, Checks: []github.HeadCheck{
			{Kind: github.CheckKindRun, Name: "test", Workflow: "CI", Outcome: github.CheckPassed},
		}}}

		points, err := routines.ReadChecks(t.Context(), nil)
		require.NoError(t, err)
		assert.Zero(t, points)

		points, err = routines.ReadChecks(t.Context(), []int64{999})
		require.NoError(t, err)
		assert.Zero(t, points, "no stored pull request, no request")
		assert.Empty(t, h.client.checksIDs)

		h.tokens.Fail(githubtest.BadCredentials())

		_, err = routines.ReadChecks(t.Context(), []int64{ids[7]})
		require.ErrorContains(t, err, "read the checks")
		h.tokens.Fail(nil)

		told := len(h.changed.all())

		refuse(t, h.harness, "BEFORE INSERT ON item_checks", "")

		points, err = routines.ReadChecks(t.Context(), []int64{ids[7]})
		require.ErrorContains(t, err, "save the checks of item")
		assert.Equal(t, 1, points, "what the read cost is told even when it can't be stored")
		assert.Zero(t, h.count(t, "item_checks"))
		assert.Len(t, h.changed.all(), told, "and nothing is told as changed")

		// With no reading of the GraphQL budget at all, nothing holds the read back.
		h.exec(t, "DROP TRIGGER refused_write")
		h.exec(t, "DELETE FROM rate_snapshots")

		points, err = routines.ReadChecks(t.Context(), []int64{ids[7]})
		require.NoError(t, err)
		assert.Equal(t, 1, points)
		assert.Equal(t, int64(1), h.count(t, "item_checks"))
		assert.Equal(t, []int64{ids[7]}, h.changed.all()[told:])

		h.exec(t, "DROP TABLE item_watches")
		h.exec(t, "DROP TABLE items")

		_, err = routines.ReadChecks(t.Context(), []int64{ids[7]})
		require.ErrorContains(t, err, "read the pull requests")
	})
}

func TestSearchNowBeforeAnyoneSignedInStoresNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.client.watches = map[string]github.WatchResult{otherQuery: answer(watched(dependabot(9), headA, "SUCCESS"))}

		got, _, err := NewRoutines(h.svc).SearchNow(t.Context(), []string{otherQuery})
		require.NoError(t, err)
		assert.Equal(t, 1, got[otherQuery].Count, "what GitHub matched is told")
		assert.Empty(t, got[otherQuery].IDs, "but no account holds its results")
		assert.NotNil(t, got[otherQuery].IDs)
		assert.Zero(t, h.count(t, "items"))
	})
}

func TestSearchNowReportsWhatItCouldntReadOrStore(t *testing.T) {
	t.Parallel()

	// tested serves #9 to the other search and runs a routine's test of it.
	tested := func(t *testing.T, h *watchHarness) (map[string]SearchOutcome, error) {
		t.Helper()

		h.client.mu.Lock()
		h.client.watches[otherQuery] = answer(watched(dependabot(9), headA, "SUCCESS"))
		h.client.details[nodeID(9)] = watchedPRDetails(headA, "SUCCESS")
		h.client.mu.Unlock()

		got, _, err := NewRoutines(h.svc).SearchNow(t.Context(), []string{otherQuery})

		return got, err
	}

	t.Run("no token", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			h, _ := routinesHarness(t)
			defer h.stop()

			h.tokens.Fail(github.ErrNotLoggedIn)

			_, err := tested(t, h)
			require.ErrorIs(t, err, github.ErrNotLoggedIn)
			assert.Len(t, h.storedItems(t), 2, "nothing is stored")
		})
	})

	t.Run("details that don't come back", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			h, _ := routinesHarness(t)
			defer h.stop()

			h.client.watchDetailsErr = errFakeNetwork

			got, err := tested(t, h)
			require.NoError(t, err, "the test decides on what it has")
			require.Len(t, got[otherQuery].IDs, 1, "the result is stored without its details")
			assert.Contains(t, h.logs.String(), "a routine's test could not read the items' details")
			assert.Nil(t, h.factsOf(t, got[otherQuery].IDs[0]).Details)
		})
	})

	t.Run("an item that can't be stored", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			h, _ := routinesHarness(t)
			defer h.stop()

			refuse(t, h.harness, "BEFORE INSERT ON items", "NEW.number = 9")

			_, err := tested(t, h)
			require.ErrorContains(t, err, "save the searched items")
			assert.Len(t, h.storedItems(t), 2)
		})
	})

	t.Run("what is stored can't be read", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			h, _ := routinesHarness(t)
			defer h.stop()

			h.exec(t, "DROP TABLE item_watches")

			_, err := tested(t, h)
			require.ErrorContains(t, err, "read the watched items")
		})
	})

	t.Run("what is stored can't be read again", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			h, _ := routinesHarness(t)
			defer h.stop()

			h.client.watchDetailsGate = newGate(t)

			done := make(chan error, 1)

			go func() {
				_, err := tested(t, h)
				done <- err
			}()

			// While the test reads #9's details, the table it reads again under the lock goes.
			h.client.watchDetailsGate.waitArrived(t)
			h.exec(t, "DROP TABLE item_watches")
			h.client.watchDetailsGate.release()

			err := <-done
			require.ErrorContains(t, err, "save the searched items")
			require.ErrorContains(t, err, "read the watched items again")
		})
	})
}
