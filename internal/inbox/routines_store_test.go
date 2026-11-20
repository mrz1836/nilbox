package inbox

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// factsHarness lists #1, with its details, and the issue #2 in other-repo, whose details
// were never read, and returns the harness and each item's id by number.
func factsHarness(t *testing.T) (*harness, map[int]int64) {
	t.Helper()

	h := newHarness(t)
	h.client.details = map[string]github.ItemDetails{nodeID(1): prDetails()}
	h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, []github.Item{issue("other-repo", 2, "MEMBER")})

	ids := map[int]int64{}
	for _, s := range h.storedItems(t) {
		ids[s.Number] = s.ID
	}

	return h, ids
}

// factsOf reads the facts' row of the item with id.
func (h *harness) factsOf(t *testing.T, id int64) facts.Row {
	t.Helper()

	rows, err := h.store().FactsRows(t.Context(), []int64{id})
	require.NoError(t, err)
	require.Len(t, rows, 1)

	return rows[0]
}

func TestFactsRowsSayWhatIsKnownOfAnItemAndItsRepository(t *testing.T) {
	t.Parallel()

	h, ids := factsHarness(t)

	never := h.factsOf(t, ids[2])
	assert.Equal(t, item.KindIssue, never.Kind)
	assert.True(t, never.InAssigned)
	assert.Nil(t, never.Details, "an item whose details were never read has none")
	assert.Nil(t, never.Checks)
	assert.Nil(t, never.Files)
	assert.Nil(t, never.Repository.HasClone, "whether its repository has a clone isn't known")

	// The user's clone of other-repo is found, and example-repo's facts are read again.
	require.NoError(t, h.store().RememberClone(t.Context(), h.repoRow(t, "other-repo").ID, "~/projects/other-repo"))
	h.exec(t, `UPDATE repos SET language = 'Go', visibility = 'PRIVATE' WHERE name = 'example-repo'`)

	cloned := h.factsOf(t, ids[2])
	require.NotNil(t, cloned.Repository.HasClone)
	assert.True(t, *cloned.Repository.HasClone)

	read := h.factsOf(t, ids[1])
	assert.True(t, read.Repository.Read)
	require.NotNil(t, read.Repository.Language)
	assert.Equal(t, "Go", *read.Repository.Language)
	assert.Equal(t, "PRIVATE", read.Repository.Visibility)
	require.NotNil(t, read.Details)
	assert.Equal(t, []string{"enhancement"}, read.Details.Labels)

	// A file check of #1's head is stored.
	require.NoError(t, upsertFiles(t.Context(), h.svc.q, newFilesRow(ids[1], fileCheck{files: changed(headA)}, h.clock.Now())))

	files := h.factsOf(t, ids[1]).Files
	require.NotNil(t, files)
	assert.Equal(t, headA, files.Head)
	assert.True(t, files.Complete)
	assert.Empty(t, files.Error)
	assert.Equal(t, []sensitive.File{
		{Path: ".github/workflows/ci.yml", Change: "modified"},
		{Path: ".claude/settings.json", Change: "added"},
		{Path: "main.go", Change: "modified"},
	}, files.Files)
}

func TestFactsRowsNameTheItemWhoseStoredColumnCantBeRead(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name, update, want string
	}{
		{"labels", `UPDATE item_details SET labels = '[1]' WHERE item_id = ?`, "the labels of item %d"},
		{"review requests", `UPDATE item_details SET review_users = '[1]' WHERE item_id = ?`, "the review requests of item %d"},
		{
			"checks", `INSERT INTO item_checks (item_id, checks, fetched_at) VALUES (?, '[1]', '2026-01-10 12:00:00+00:00')`,
			"the checks of item %d",
		},
		{
			"files", `INSERT INTO item_files (item_id, files, checked_at) VALUES (?, '[1]', '2026-01-10 12:00:00+00:00')`,
			"the files of item %d",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			h, ids := factsHarness(t)
			h.exec(t, c.update, ids[1])

			_, err := h.store().FactsRows(t.Context(), []int64{ids[2], ids[1]})
			require.ErrorContains(t, err, fmt.Sprintf(c.want, ids[1]))
			require.ErrorContains(t, err, "read the items' facts")
		})
	}
}

func TestTheRoutinesReadsReportAnUnreadableDatabase(t *testing.T) {
	t.Parallel()

	s := NewStore(testutil.ClosedDB(t), nil)
	ctx := t.Context()

	_, err := s.FactsRows(ctx, []int64{1})
	require.ErrorContains(t, err, "read the repository rules")

	got, err := s.WatchedItems(ctx, []string{depQuery}, Lists{ReviewRequested: true})
	require.ErrorContains(t, err, "read the signed-in account")
	assert.NotNil(t, got, "the list is never nil")

	suggested, err := s.Suggest(ctx)
	require.ErrorContains(t, err, "read the authors")
	assert.NotNil(t, suggested.Authors)
	assert.NotNil(t, suggested.Checks)

	_, err = s.KnownOwners(ctx)
	require.ErrorContains(t, err, "read the owners")

	repos, err := s.KnownRepositories(ctx)
	require.ErrorContains(t, err, "read the repositories")
	assert.NotNil(t, repos, "the list is never nil")

	err = s.NoteStates(ctx, s.StartRead(), []item.Ref{prRef(1)},
		map[string]github.ItemState{prRef(1).Key(): {State: github.StateClosed}})
	require.Error(t, err)
}

func TestTheRoutinesReadsReportATableTheyCantRead(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		table string
		read  func(t *testing.T, s *Store) error
		want  string
	}{
		{"item_checks", func(t *testing.T, s *Store) error {
			_, err := s.FactsRows(t.Context(), []int64{1})

			return err
		}, "read the items' facts"},
		{"item_watches", func(t *testing.T, s *Store) error {
			_, err := s.WatchedItems(t.Context(), []string{depQuery}, Lists{})

			return err
		}, "read the watched items"},
		{"item_details", func(t *testing.T, s *Store) error {
			_, err := s.Suggest(t.Context())

			return err
		}, "read the labels"},
		{"item_checks", func(t *testing.T, s *Store) error {
			_, err := s.Suggest(t.Context())

			return err
		}, "read the checks"},
	} {
		t.Run(c.want, func(t *testing.T) {
			t.Parallel()

			h, _ := factsHarness(t)
			h.exec(t, "DROP TABLE "+c.table)

			require.ErrorContains(t, c.read(t, h.store()), c.want)
		})
	}
}

func TestSuggestSkipsWhatItCantReadAndCountsNamesOnce(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	// #1 and #2 carry the labels "Bug" and "bug", and #3 carries "docs" and a blank name,
	// so "Bug" is the most common, spelled as first stored; #4's labels can't be read.
	withLabels := func(names ...string) github.ItemDetails {
		d := prDetails()
		d.Labels = nil

		for _, n := range names {
			d.Labels = append(d.Labels, github.Label{Name: n})
		}

		return d
	}

	h.client.details = map[string]github.ItemDetails{
		nodeID(1): withLabels("Bug"), nodeID(2): withLabels("bug", "zeta"), nodeID(3): withLabels("docs", "  "),
		nodeID(4): withLabels("ignored"),
	}
	h.seed(t, []github.Item{pr(1, "OWNER", false), pr(2, "OWNER", false), pr(3, "OWNER", false), pr(4, "OWNER", false)},
		nil, nil)

	id4 := h.storedItemNumbered(t, 4).ID
	h.exec(t, `UPDATE item_details SET labels = '[1]' WHERE item_id = ?`, id4)

	// #1's checks name a job twice and a status once; #4's checks can't be read.
	h.exec(t, `INSERT INTO item_checks (item_id, checks, fetched_at) VALUES (?, ?, ?), (?, '[1]', ?)`,
		h.storedItemNumbered(t, 1).ID,
		`[{"name": "test", "workflow": "CI"}, {"name": "test", "workflow": "ci"}, {"name": "codecov/patch"}]`,
		h.clock.Now().UTC(), id4, h.clock.Now().UTC())

	got, err := h.store().Suggest(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"Bug", "docs", "zeta"}, got.Labels,
		"names count ignoring case, the most common first, ties by name; a blank name and an unreadable column don't count")
	assert.Equal(t, []string{"CI / test", "codecov/patch"}, got.Checks)
	assert.Equal(t, []string{octoUser}, got.Authors)
	assert.Empty(t, got.Bots)
	assert.NotNil(t, got.Bots)
}

func TestNoteStatesStoresOnlyTheStatesItWasGiven(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, []github.Item{pr(1, "OWNER", false), pr(2, "OWNER", false)}, nil, nil)

	notes := h.store()
	state := func(number int) string {
		var s string

		require.NoError(t, h.pool.QueryRowContext(t.Context(), `SELECT state FROM items WHERE number = ?`, number).Scan(&s))

		return s
	}

	h.clock.Advance(time.Minute)

	closed := map[string]github.ItemState{prRef(1).Key(): {State: github.StateClosed}}
	require.NoError(t, notes.NoteStates(t.Context(), StateRead{}, []item.Ref{prRef(1)}, closed),
		"the zero StateRead records nothing")
	require.NoError(t, notes.NoteStates(t.Context(), notes.StartRead(), []item.Ref{prRef(1)}, nil))
	assert.Empty(t, state(1))

	require.NoError(t, notes.NoteStates(t.Context(), notes.StartRead(), []item.Ref{prRef(1), prRef(2), prRef(3)},
		map[string]github.ItemState{prRef(1).Key(): {State: ""}, prRef(2).Key(): {State: github.StateClosed}}))
	assert.Empty(t, state(1), "a read that said no state leaves it")
	assert.Equal(t, github.StateClosed, state(2))

	refuse(t, h, "BEFORE UPDATE OF state ON items", "")

	err := notes.NoteStates(t.Context(), notes.StartRead(), []item.Ref{prRef(1)}, closed)
	require.ErrorContains(t, err, "save the state of acme-org/example-repo#1")
	assert.Empty(t, state(1))
}
