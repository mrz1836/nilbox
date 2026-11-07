package inbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// store returns a Store over the harness's database and clock.
func (h *harness) store() *Store {
	return NewStore(h.pool, h.clock)
}

// stamps reads the created_at and updated_at of the row a query selects them from.
func (h *harness) stamps(t *testing.T, query string, args ...any) (created, updated time.Time) {
	t.Helper()

	require.NoError(t, h.pool.QueryRowContext(t.Context(), query, args...).Scan(&created, &updated))

	return created, updated
}

func TestViewerLogin(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	login, err := h.store().ViewerLogin(t.Context())
	require.NoError(t, err)
	assert.Empty(t, login, "nobody is signed in before the first refresh")

	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
	h.refresh(t)

	login, err = h.store().ViewerLogin(t.Context())
	require.NoError(t, err)
	assert.Equal(t, octoUser, login)

	h.client.inbox.Viewer.Login = "octo-other"
	h.clock.Advance(DefaultInterval)
	h.refresh(t)

	login, err = h.store().ViewerLogin(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "octo-other", login, "the last refresh's account wins")
}

func TestStoreReportsAnUnreadableDatabase(t *testing.T) {
	t.Parallel()

	db := testutil.SQLDB(t)
	require.NoError(t, db.Close())

	s := NewStore(db, nil)

	_, err := s.ViewerLogin(t.Context())
	require.ErrorContains(t, err, "read the signed-in account")

	_, err = s.LaunchItems(t.Context(), []int64{1})
	require.ErrorContains(t, err, "read the items")

	err = s.RememberClone(t.Context(), 1, "~/projects/example-repo")
	require.ErrorContains(t, err, "remember the repository's clone")
}

func TestLaunchItems(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	a, b := pr(1, "CONTRIBUTOR", true), issue("other-repo", 2, "MEMBER")
	a.AuthorType, a.IsDraft = "User", true
	a.Repo.DefaultBranch = "main"

	details := prDetails()
	details.Labels = append(details.Labels, github.Label{Name: "size/L"})
	details.Mergeable = "CONFLICTING"

	h.client.inbox = fetched([]github.Item{a}, nil, []github.Item{b})
	h.client.details = map[string]github.ItemDetails{nodeID(1): details}
	snap := h.refresh(t)

	first, second := itemByNumber(t, snap.ReviewRequested, 1).ID, itemByNumber(t, snap.Assigned, 2).ID

	got, err := h.store().LaunchItems(t.Context(), []int64{second, 999, first})
	require.NoError(t, err)
	require.Len(t, got, 2, "an id with no stored item is left out")

	repoID := h.repoRow(t, "example-repo").ID

	assert.Equal(t, LaunchItem{
		ID: first, Kind: item.KindPR, Number: 1, URL: a.URL, Title: "Item 1", HeadRef: "branch-1",
		AuthorLogin: octoUser, AuthorType: "User", AuthorAssociation: "CONTRIBUTOR", IsCrossRepository: true,
		IsDraft: true, RepoID: repoID, Owner: "acme-org", Name: "example-repo", DefaultBranch: "main",
		Labels: []string{"enhancement", "size/L"}, Additions: 120, Deletions: 30, ChangedFiles: 6, Commits: 3,
		Mergeable: "CONFLICTING", ChecksState: "SUCCESS", ReviewDecision: details.Reviews.Decision,
	}, got[0], "by id")

	assert.Equal(t, second, got[1].ID)
	assert.Equal(t, item.KindIssue, got[1].Kind)
	assert.Equal(t, "other-repo", got[1].Name)
	assert.Equal(t, []string{}, got[1].Labels, "an item whose details were never read has none")
	assert.Zero(t, got[1].Additions)
	assert.Empty(t, got[1].Mergeable)

	none, err := h.store().LaunchItems(t.Context(), nil)
	require.NoError(t, err)
	assert.NotNil(t, none)
	assert.Empty(t, none)
}

func TestRememberClone(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
	h.refresh(t)

	before := h.repoRow(t, "example-repo")

	h.clock.Advance(time.Hour)
	require.NoError(t, h.store().RememberClone(t.Context(), before.ID, "~/projects/example-repo"))

	after := h.repoRow(t, "example-repo")
	assert.Equal(t, "~/projects/example-repo", after.LocalPath)
	assert.True(t, h.clock.Now().Equal(after.UpdatedAt), "updated_at is stamped from the clock")
	assert.True(t, before.CreatedAt.Equal(after.CreatedAt))

	require.NoError(t, h.store().RememberClone(t.Context(), 999, "~/projects/elsewhere"),
		"an unknown repository changes nothing")
}

func TestStoreFixture(t *testing.T) {
	t.Parallel()

	db := testutil.SQLDB(t)
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	details := prDetails()

	ids, err := StoreFixture(t.Context(), db, Fixture{
		Viewer: octoUser,
		At:     at,
		Items: []FixtureItem{
			{Item: pr(1, "OWNER", false), ReviewRequested: true, Details: &details},
			{Item: issue("other-repo", 2, "MEMBER"), Assigned: true},
			{Item: pr(3, "OWNER", false)},
		},
	})
	require.NoError(t, err)
	require.Len(t, ids, 3)

	got, err := NewStore(db, nil).LaunchItems(t.Context(), ids)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, []string{"enhancement"}, got[0].Labels, "the details are stored")
	assert.Equal(t, []string{}, got[1].Labels)

	counts, err := NewStore(db, nil).RepoCounts(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []RepoCount{
		{Repo: repo.Name{Owner: "acme-org", Repo: "example-repo"}, Items: 1},
		{Repo: repo.Name{Owner: "acme-org", Repo: "other-repo"}, Items: 1},
	}, counts, "an item in no list is stored but not listed")

	// The fixture is stored as the account's: a Service signed in as it lists the items.
	require.NoError(t, prefs.New(db, nil).Set(t.Context(), prefViewer, octoUser))

	snap, err := NewService(Config{Pool: db, Clock: testutil.NewClock(at)}).Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []int{1}, numbers(snap.ReviewRequested))
	assert.Equal(t, []int{2}, numbers(snap.Assigned))
	assert.Equal(t, "2026-06-01T00:00:00Z", itemByNumber(t, snap.ReviewRequested, 1).Details.FetchedAt,
		"stamped at the fixture's time")
}

func TestStoreFixtureStoresNothingWhenAWriteFails(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	refuse(t, h, "BEFORE INSERT ON items", "NEW.number = 2")

	_, err := StoreFixture(t.Context(), h.pool, Fixture{Viewer: octoUser, Items: []FixtureItem{
		{Item: pr(1, "OWNER", false)}, {Item: pr(2, "OWNER", false)},
	}})
	require.ErrorContains(t, err, "store the fixture")
	require.ErrorContains(t, err, "save the item acme-org/example-repo#2")

	for _, table := range []string{"accounts", "repos", "items"} {
		assert.Zero(t, h.count(t, table), table)
	}
}

func TestUnknownItemsAreNotFound(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
	h.refresh(t)

	const unknown = 999

	for name, call := range map[string]func() error{
		"dismiss":       func() error { _, err := h.svc.Dismiss(t.Context(), unknown); return err },
		"snooze":        func() error { _, err := h.svc.Snooze(t.Context(), unknown, 3); return err },
		"pin":           func() error { _, err := h.svc.Pin(t.Context(), unknown); return err },
		"refresh item":  func() error { _, err := h.svc.RefreshItem(t.Context(), unknown); return err },
		"stored files":  func() error { _, err := h.svc.StoredFiles(t.Context(), unknown); return err },
		"checked files": func() error { _, err := h.svc.CheckFiles(t.Context(), unknown); return err },
	} {
		require.ErrorIs(t, call(), item.ErrNotFound, name)
	}

	// Clearing what an unknown item never had is nothing to report.
	_, err := h.svc.Restore(t.Context(), unknown)
	require.NoError(t, err)

	_, err = h.svc.Unpin(t.Context(), unknown)
	require.NoError(t, err)

	assert.Zero(t, h.count(t, "item_local_state"), "nothing is stored for an unknown item")
}

func TestUpdatesStampUpdatedAt(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
	first := h.clock.Now()
	id := itemByNumber(t, h.refresh(t).ReviewRequested, 1).ID

	for _, table := range []string{"accounts", "repos"} {
		created, updated := h.stamps(t, "SELECT created_at, updated_at FROM "+table)
		assert.True(t, first.Equal(created), table)
		assert.True(t, first.Equal(updated), table)
	}

	second := h.clock.Advance(DefaultInterval)
	h.refresh(t)

	for _, table := range []string{"accounts", "repos"} {
		created, updated := h.stamps(t, "SELECT created_at, updated_at FROM "+table)
		assert.True(t, first.Equal(created), "%s keeps the time it was first stored", table)
		assert.True(t, second.Equal(updated), "%s is stamped by the refresh that rewrote it", table)
	}

	stateStamp := func() time.Time {
		var updated time.Time
		require.NoError(t, h.pool.QueryRowContext(t.Context(),
			"SELECT updated_at FROM item_local_state WHERE item_id = ?", id).Scan(&updated))

		return updated
	}

	for _, step := range []struct {
		name string
		do   func() (Snapshot, error)
	}{
		{"pin", func() (Snapshot, error) { return h.svc.Pin(t.Context(), id) }},
		{"dismiss", func() (Snapshot, error) { return h.svc.Dismiss(t.Context(), id) }},
		{"restore", func() (Snapshot, error) { return h.svc.Restore(t.Context(), id) }},
		{"snooze", func() (Snapshot, error) { return h.svc.Snooze(t.Context(), id, 3) }},
		{"unpin", func() (Snapshot, error) { return h.svc.Unpin(t.Context(), id) }},
	} {
		at := h.clock.Advance(time.Minute)

		_, err := step.do()
		require.NoError(t, err, step.name)
		assert.True(t, at.Equal(stateStamp()), "%s stamps the item's local state", step.name)
	}
}

func TestStoredJSONThatCannotBeReadIsAnError(t *testing.T) {
	t.Parallel()

	// Both columns only take a JSON array; an array of numbers is one, but holds no
	// label or file.
	for _, tt := range []struct{ name, corrupt string }{
		{name: "labels", corrupt: "UPDATE item_details SET labels = '[1]'"},
		{name: "files", corrupt: "UPDATE item_files SET files = '[1]'"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := filesHarness(t, from(12, "forker", "CONTRIBUTOR", true))
			id := itemByNumber(t, h.refresh(t).ReviewRequested, 12).ID
			h.exec(t, tt.corrupt)

			_, err := h.svc.Snapshot(t.Context())
			require.ErrorContains(t, err, "read the inbox items")

			if tt.name == "labels" {
				_, err = h.store().LaunchItems(t.Context(), []int64{id})
				require.ErrorContains(t, err, "read the items")

				return
			}

			_, err = h.svc.StoredFiles(t.Context(), id)
			require.ErrorContains(t, err, "read the item")
		})
	}
}

func TestStoreFixtureReportsEachRefusedWrite(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ table, want string }{
		{table: "accounts", want: "save the account"},
		{table: "repos", want: "save the repository acme-org/example-repo"},
		{table: "item_details", want: "save the details of acme-org/example-repo#1"},
	} {
		t.Run(tt.table, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			refuse(t, h, "BEFORE INSERT ON "+tt.table, "")

			details := prDetails()

			_, err := StoreFixture(t.Context(), h.pool, Fixture{Viewer: octoUser, Items: []FixtureItem{
				{Item: pr(1, "OWNER", false), Details: &details},
			}})
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestStateWritesThatFailAreReported(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
	id := itemByNumber(t, h.refresh(t).ReviewRequested, 1).ID

	_, err := h.svc.Dismiss(t.Context(), id)
	require.NoError(t, err)

	refuse(t, h, "BEFORE UPDATE ON item_local_state", "")

	_, err = h.svc.Restore(t.Context(), id)
	require.ErrorContains(t, err, "restore the item")

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.True(t, itemByNumber(t, snap.ReviewRequested, 1).Dismissed(), "the item stays dismissed")
}

func TestSnoozeBookkeepingThatFailsKeepsTheStoredInbox(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string

		// detailsFirst reads the item's details before it is snoozed, so the snooze has
		// a baseline and a later commit wakes it.
		detailsFirst bool
		event, want  string
	}{
		{
			name: "baseline", event: "BEFORE UPDATE OF snooze_baseline ON item_local_state",
			want: "record the snooze baseline",
		},
		{
			name: "wake", detailsFirst: true, event: "BEFORE UPDATE OF wake_reasons ON item_local_state",
			want: "wake the snoozed item",
		},
		{name: "running snoozes", detailsFirst: true, event: "", want: "read the snoozed items"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)

			details := prDetails()
			if tt.detailsFirst {
				h.client.details = map[string]github.ItemDetails{nodeID(1): details}
			}

			id := itemByNumber(t, h.refresh(t).ReviewRequested, 1).ID

			_, err := h.svc.Snooze(t.Context(), id, 3)
			require.NoError(t, err)

			if tt.event == "" {
				h.exec(t, "ALTER TABLE item_local_state RENAME TO item_local_state_gone")
			} else {
				refuse(t, h, tt.event, "")
			}

			details.Commits += 2
			h.client.details = map[string]github.ItemDetails{nodeID(1): details}
			h.clock.Advance(time.Hour)

			_, err = h.svc.Refresh(t.Context())
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestFileChecksThatCannotBeStoredAreReported(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(12, "forker", "CONTRIBUTOR", true))
	refuse(t, h, "BEFORE INSERT ON item_files", "")

	_, err := h.svc.Refresh(t.Context())
	require.ErrorContains(t, err, "save the files of acme-org/example-repo#12")

	h.exec(t, "DROP TRIGGER refused_write")

	id := itemByNumber(t, h.refresh(t).ReviewRequested, 12).ID

	refuse(t, h, "BEFORE UPDATE ON item_files", "")

	_, err = h.svc.CheckFiles(t.Context(), id)
	require.ErrorContains(t, err, "save the checked files")
}
