package inbox

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// routinesHarness lists #1 and stores #7, watched by the Dependabot search, with their
// details, and returns the harness and each item's ID by number.
func routinesHarness(t *testing.T) (*watchHarness, map[int]int64) {
	t.Helper()

	h := newWatchHarness(t)
	h.startWatching(t)
	h.refresh(t)
	synctest.Wait()

	ids := map[int]int64{}
	for _, s := range h.storedItems(t) {
		ids[s.Number] = s.ID
	}

	return h, ids
}

func TestFactsRowsReadWhatTheRoutinesAreBuiltFrom(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h, ids := routinesHarness(t)
		defer h.stop()

		setRules(t, h.harness, []string{}, []string{"acme-org/example-repo"})

		rows, err := NewStore(h.pool, h.clock).FactsRows(t.Context(), []int64{ids[7], ids[1], 999})
		require.NoError(t, err)
		require.Len(t, rows, 2, "an id with no stored item is left out")

		listed, watched := rows[0], rows[1]
		if listed.ID != ids[1] {
			listed, watched = watched, listed
		}

		assert.True(t, listed.InReviewRequested)
		assert.NotNil(t, listed.ReviewRequestedAt, "when the inbox saw it arrive")
		assert.False(t, watched.InReviewRequested || watched.InAuthored || watched.InAssigned)
		assert.Equal(t, item.KindPR, watched.Kind)
		assert.Equal(t, repo.Name{Owner: acmeOrg, Repo: "example-repo"}, watched.Repo)
		assert.Equal(t, "dependabot", watched.Author)
		assert.True(t, watched.Repository.Muted, "the user's rules for the repository")

		require.NotNil(t, watched.Details, "the watch pass read its details")
		assert.Equal(t, headA, watched.Details.HeadOID)
		assert.Equal(t, "main", watched.Details.BaseRef)
		assert.Equal(t, github.MergeStateClean, watched.Details.MergeState)
		assert.Nil(t, watched.Checks, "no check names were read")

		empty, err := NewStore(h.pool, h.clock).FactsRows(t.Context(), nil)
		require.NoError(t, err)
		assert.NotNil(t, empty)
	})
}

func TestReadChecksStoresThemForTheFacts(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h, ids := routinesHarness(t)
		defer h.stop()

		done := h.clock.Now().Add(-time.Hour)
		h.client.checks = map[string]github.HeadChecks{nodeID(7): {Head: headA, Complete: true, Checks: []github.HeadCheck{
			{Kind: github.CheckKindRun, Name: "test", Workflow: "CI", Outcome: github.CheckFailed, CompletedAt: done},
			{Kind: github.CheckKindRun, Name: "test", Workflow: "CI", Outcome: github.CheckSkipped, CompletedAt: done},
		}}}

		points, err := NewRoutines(h.svc).ReadChecks(t.Context(), []int64{ids[7], 999})
		require.NoError(t, err)
		assert.Equal(t, 1, points, "what the read cost is told")
		assert.Equal(t, [][]string{{nodeID(7)}}, h.client.checksIDs, "only the stored pull requests are asked about")

		rows, err := NewStore(h.pool, h.clock).FactsRows(t.Context(), []int64{ids[7]})
		require.NoError(t, err)
		require.NotNil(t, rows[0].Checks)
		assert.Equal(t, headA, rows[0].Checks.Head)
		assert.True(t, rows[0].Checks.Complete)
		assert.Equal(t, []facts.CheckRow{
			{Name: "test", Workflow: "CI", Outcome: facts.CheckFailed, CompletedAt: done.UTC()},
			{Name: "test", Workflow: "CI", Outcome: facts.CheckSkipped, CompletedAt: done.UTC()},
		}, rows[0].Checks.Checks, "every run, a name once per event that ran it")

		var settled *time.Time

		require.NoError(t, h.pool.QueryRowContext(t.Context(), `SELECT settled_at FROM item_checks`).Scan(&settled))
		require.NotNil(t, settled)
		assert.True(t, done.Equal(*settled))
	})
}

func TestItemsByIDReadsListedAndWatchedItems(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h, ids := routinesHarness(t)
		defer h.stop()

		got, err := h.svc.ItemsByID(t.Context(), []int64{ids[7], ids[1], 999})
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, []int{7, 1}, []int{got[0].Number, got[1].Number}, "newest activity first")
		assert.NotNil(t, got[0].Details)

		none, err := h.svc.ItemsByID(t.Context(), nil)
		require.NoError(t, err)
		assert.NotNil(t, none)
	})
}

func TestWatchedItemsAreTheListsAndTheSearches(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h, ids := routinesHarness(t)
		defer h.stop()

		st := NewStore(h.pool, h.clock)

		got, err := st.WatchedItems(t.Context(), []string{depQuery}, Lists{})
		require.NoError(t, err)
		assert.ElementsMatch(t, []int64{ids[1], ids[7]}, got)

		got, err = st.WatchedItems(t.Context(), nil, Lists{ReviewRequested: true})
		require.NoError(t, err)
		assert.Equal(t, []int64{ids[1]}, got)

		got, err = st.WatchedItems(t.Context(), nil, Lists{Assigned: true})
		require.NoError(t, err)
		assert.Empty(t, got)
		assert.NotNil(t, got)
	})
}

func TestSuggestReadsTheStoredItems(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h, ids := routinesHarness(t)
		defer h.stop()

		h.client.checks = map[string]github.HeadChecks{nodeID(7): {Head: headA, Complete: true, Checks: []github.HeadCheck{
			{Name: "lint", Workflow: "CI", Outcome: github.CheckPassed},
			{Name: "CodeQL", Outcome: github.CheckPassed},
		}}}
		_, err := NewRoutines(h.svc).ReadChecks(t.Context(), []int64{ids[7]})
		require.NoError(t, err)

		got, err := NewStore(h.pool, h.clock).Suggest(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{"dependabot"}, got.Bots)
		assert.Equal(t, []string{octoUser}, got.Authors)
		assert.Equal(t, []string{acmeOrg}, got.Owners)
		assert.Equal(t, []string{"enhancement"}, got.Labels)
		assert.Equal(t, []string{"CI / lint", "CodeQL"}, got.Checks)

		owners, err := NewStore(h.pool, h.clock).KnownOwners(t.Context())
		require.NoError(t, err)
		assert.True(t, owners[acmeOrg])

		repos, err := NewStore(h.pool, h.clock).KnownRepositories(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{acmeOrg + "/" + exampleRepo}, repos)

		_, err = h.pool.ExecContext(t.Context(), `UPDATE repos SET is_archived = 1`)
		require.NoError(t, err)

		repos, err = NewStore(h.pool, h.clock).KnownRepositories(t.Context())
		require.NoError(t, err)
		assert.Empty(t, repos, "an archived repository is no routine's to name")
		assert.NotNil(t, repos)
	})
}

func TestSearchNowStoresItsResultsWithoutWatchingThem(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h, _ := routinesHarness(t)
		defer h.stop()

		h.client.watches[otherQuery] = answer(watched(dependabot(9), headA, "FAILURE"))
		h.client.details[nodeID(9)] = watchedPRDetails(headA, "FAILURE")

		got, points, err := NewRoutines(h.svc).SearchNow(t.Context(), []string{otherQuery, "is:pr org:none-such"})
		require.NoError(t, err)
		assert.Equal(t, 0, points, "the fake answers at no cost")

		found := got[otherQuery]
		require.NoError(t, found.Err)
		require.Len(t, found.IDs, 1)
		require.Error(t, got["is:pr org:none-such"].Err)

		var rows int

		require.NoError(t, h.pool.QueryRowContext(t.Context(),
			`SELECT COUNT(*) FROM item_watches WHERE item_id = ?`, found.IDs[0]).Scan(&rows))
		assert.Zero(t, rows, "a test watches nothing")

		facts, err := NewStore(h.pool, h.clock).FactsRows(t.Context(), found.IDs)
		require.NoError(t, err)
		require.Len(t, facts, 1)
		assert.NotNil(t, facts[0].Details, "its details were read")
	})
}

func TestNoteStatesRecordsAnotherServicesRead(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ref := item.Ref{Repo: repo.Name{Owner: acmeOrg, Repo: "example-repo"}, Number: 1, Kind: item.KindPR}
	closed := time.Date(2026, 1, 9, 0, 0, 0, 0, time.UTC)
	notes := NewStore(h.pool, h.clock)

	// A read that goes out before the item is stored.
	early := notes.StartRead()

	h.clock.Advance(time.Minute)
	h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)
	id := h.storedItems(t)[0].ID

	state := func() (string, *time.Time) {
		var (
			s  string
			at *time.Time
		)

		require.NoError(t, h.pool.QueryRowContext(t.Context(), `SELECT state, closed_at FROM items WHERE id = ?`, id).
			Scan(&s, &at))

		return s, at
	}

	note := func(read StateRead, s string, at time.Time) {
		require.NoError(t, notes.NoteStates(t.Context(), read, []item.Ref{ref},
			map[string]github.ItemState{ref.Key(): {State: s, ClosedAt: at}}))
	}

	note(early, github.StateClosed, closed)

	got, _ := state()
	assert.NotEqual(t, github.StateClosed, got, "a read that went out before the item was stored keeps the newer read")

	h.clock.Advance(time.Minute)
	note(notes.StartRead(), github.StateMerged, closed)

	got, at := state()
	assert.Equal(t, github.StateMerged, got)
	require.NotNil(t, at)
	assert.True(t, closed.Equal(*at))

	h.clock.Advance(time.Minute)
	note(notes.StartRead(), github.StateOpen, time.Time{})

	got, _ = state()
	assert.Equal(t, github.StateMerged, got, "nothing comes back from a merge")

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.NotEmpty(t, snap.ReviewRequested.Items, "the lists are the inbox's own refresh's to change")
}
