package store_test

import (
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	inboxmodel "github.com/mrz1836/nilbox/internal/inbox/model"
	profilesmodel "github.com/mrz1836/nilbox/internal/profiles/model"
	settingsmodel "github.com/mrz1836/nilbox/internal/settings/model"
	"github.com/mrz1836/nilbox/internal/store"
)

// migratedTables lists the application tables (everything but SQLite internals and the
// migration history).
func migratedTables(t *testing.T, db *gorm.DB) []string {
	t.Helper()

	var names []string
	require.NoError(t, db.Raw(`SELECT name FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' AND name <> 'schema_migrations'
		ORDER BY name`).Scan(&names).Error)

	return names
}

// tableColumns returns the column names of table.
func tableColumns(t *testing.T, db *gorm.DB, table string) []string {
	t.Helper()

	var cols []struct{ Name string }
	require.NoError(t, db.Raw("SELECT name FROM pragma_table_info(?)", table).Scan(&cols).Error)

	names := make([]string, 0, len(cols))
	for _, c := range cols {
		names = append(names, c.Name)
	}

	sort.Strings(names)

	return names
}

func TestModelsMatchMigratedSchema(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "app.db"))

	modelTables := make(map[string]bool)

	for _, m := range store.AllModels() {
		s, err := schema.Parse(m, &sync.Map{}, db.NamingStrategy)
		require.NoError(t, err)

		require.False(t, modelTables[s.Table], "table %s has more than one model", s.Table)
		modelTables[s.Table] = true

		var fields []string

		for _, f := range s.Fields {
			if f.DBName != "" {
				fields = append(fields, f.DBName)
			}
		}

		sort.Strings(fields)

		cols := tableColumns(t, db, s.Table)
		require.NotEmpty(t, cols, "model %s maps to missing table %s", s.Name, s.Table)
		assert.Equal(t, cols, fields, "model %s columns differ from table %s", s.Name, s.Table)
	}

	for _, table := range migratedTables(t, db) {
		assert.True(t, modelTables[table], "migrated table %s has no model", table)
	}
}

func TestAllModelsAggregatesBoundedContexts(t *testing.T) {
	want := slices.Concat(inboxmodel.AllModels(), settingsmodel.AllModels(), profilesmodel.AllModels())

	got := store.AllModels()
	require.Len(t, got, len(want))

	for i := range want {
		assert.Equal(t, reflect.TypeOf(want[i]), reflect.TypeOf(got[i]))
	}
}

func TestModelsRoundTrip(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "app.db"))
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

	account := inboxmodel.Account{Login: "octo-user"}
	require.NoError(t, db.Create(&account).Error)

	repo := inboxmodel.Repo{
		Owner: "acme-org", Name: "example-repo", Pinned: true, LocalPath: "~/projects/example-repo",
		NodeID: "R_fakeRepoExample", Visibility: "PRIVATE", IsFork: true, Parent: "acme-org/upstream-repo",
		DefaultBranch: "main", ViewerPermission: "WRITE", ViewerMergeMethod: "SQUASH", SquashMergeAllowed: true,
		Language: "Go", LanguageColor: "#00add8", PushedAt: &at, FetchedAt: &at,
	}
	require.NoError(t, db.Create(&repo).Error)

	item := inboxmodel.Item{
		AccountID: account.ID, RepoID: repo.ID, Kind: inboxmodel.KindPR, Number: 7,
		NodeID: "PR_node", Title: "Add a widget", URL: "https://github.com/acme-org/example-repo/pull/7",
		AuthorLogin: "octo-user", AuthorAssociation: "CONTRIBUTOR", IsCrossRepository: true,
		HeadRef: "feat/widget", ItemCreatedAt: at, ItemUpdatedAt: at.Add(time.Hour),
		ReasonReviewRequested: true, FetchedAt: at,
	}
	require.NoError(t, db.Create(&item).Error)

	done := at.Add(2 * time.Hour)
	require.NoError(t, db.Create(&inboxmodel.ItemLocalState{ItemID: item.ID, DoneAt: &done}).Error)
	require.NoError(t, db.Create(&inboxmodel.ItemDetails{
		ItemID: item.ID, Labels: inboxmodel.Labels{{Name: "bug", Color: "#d73a4a"}}, Comments: 4,
		Additions: 120, Deletions: 30, ChangedFiles: 6, Commits: 3, Mergeable: "MERGEABLE",
		ChecksState: "SUCCESS", ChecksPassed: 12, ChecksSkipped: 3,
		ReviewDecision: "APPROVED", Reviewers: 2, Reviewed: 1, Approvals: 1, FetchedAt: at,
	}).Error)
	require.NoError(t, db.Create(&inboxmodel.RateSnapshot{
		AccountID: account.ID, Resource: "graphql", LimitTotal: 5000, Remaining: 4999, Used: 1, Cost: 1,
		ResetAt: at, TakenAt: at,
	}).Error)

	profile := profilesmodel.ClaudeProfile{
		Name: "work", ConfigDir: "~/.claude_work", Effort: "high", PlanMode: true,
		AddDirs: profilesmodel.StringList{"~/projects/shared"}, Env: profilesmodel.StringMap{"EXAMPLE": "1"},
	}
	require.NoError(t, db.Create(&profile).Error)

	var gotItem inboxmodel.Item
	require.NoError(t, db.First(&gotItem, item.ID).Error)
	assert.Equal(t, "Add a widget", gotItem.Title)
	assert.True(t, gotItem.IsCrossRepository)
	assert.True(t, gotItem.ReasonReviewRequested)
	assert.False(t, gotItem.ReasonAuthored)
	assert.True(t, at.Add(time.Hour).Equal(gotItem.ItemUpdatedAt))

	var gotRepo inboxmodel.Repo
	require.NoError(t, db.First(&gotRepo, "owner = ? AND name = ?", "ACME-ORG", "Example-Repo").Error,
		"owner and name match case-insensitively")
	assert.True(t, gotRepo.Pinned)
	assert.False(t, gotRepo.Muted)
	assert.Equal(t, "acme-org/upstream-repo", gotRepo.Parent)
	assert.True(t, gotRepo.SquashMergeAllowed)
	assert.False(t, gotRepo.IsArchived)
	assert.Nil(t, gotRepo.ArchivedAt, "never archived")
	require.NotNil(t, gotRepo.PushedAt)
	assert.True(t, at.Equal(*gotRepo.PushedAt))

	var gotDetails inboxmodel.ItemDetails
	require.NoError(t, db.First(&gotDetails, item.ID).Error)
	assert.Equal(t, inboxmodel.Labels{{Name: "bug", Color: "#d73a4a"}}, gotDetails.Labels)
	assert.Equal(t, 12, gotDetails.ChecksPassed)
	assert.Equal(t, "APPROVED", gotDetails.ReviewDecision)

	var gotState inboxmodel.ItemLocalState
	require.NoError(t, db.First(&gotState, item.ID).Error)
	require.NotNil(t, gotState.DoneAt)
	assert.True(t, done.Equal(*gotState.DoneAt))

	var gotProfile profilesmodel.ClaudeProfile
	require.NoError(t, db.First(&gotProfile, profile.ID).Error)
	assert.Equal(t, profilesmodel.StringList{"~/projects/shared"}, gotProfile.AddDirs)
	assert.Empty(t, gotProfile.ExtraArgs)
	assert.Equal(t, profilesmodel.StringMap{"EXAMPLE": "1"}, gotProfile.Env)
	assert.Equal(t, time.UTC, gotProfile.CreatedAt.Location(), "GORM timestamps are stored in UTC")

	// Deleting the account cascades to its items, their local state, and its readings.
	require.NoError(t, db.Delete(&account).Error)

	var remaining int64
	require.NoError(t, db.Model(&inboxmodel.ItemLocalState{}).Count(&remaining).Error)
	assert.Zero(t, remaining)
	require.NoError(t, db.Model(&inboxmodel.ItemDetails{}).Count(&remaining).Error)
	assert.Zero(t, remaining)
	require.NoError(t, db.Model(&inboxmodel.RateSnapshot{}).Count(&remaining).Error)
	assert.Zero(t, remaining)
}

func TestSchemaRejectsInvalidRows(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "app.db"))

	account := inboxmodel.Account{Login: "octo-user"}
	require.NoError(t, db.Create(&account).Error)

	repo := inboxmodel.Repo{Owner: "acme-org", Name: "example-repo"}
	require.NoError(t, db.Create(&repo).Error)

	// Duplicate repository, differing only by case.
	require.Error(t, db.Create(&inboxmodel.Repo{Owner: "Acme-Org", Name: "EXAMPLE-REPO"}).Error)

	// Repository flags are 0 or 1.
	require.Error(t, db.Exec(`UPDATE repos SET is_archived = 2 WHERE id = ?`, repo.ID).Error)

	// Unknown item kind.
	require.Error(t, db.Create(&inboxmodel.Item{AccountID: account.ID, RepoID: repo.ID, Kind: "discussion", Number: 1}).Error)

	// Item for a repository that does not exist.
	require.Error(t, db.Create(&inboxmodel.Item{AccountID: account.ID, RepoID: repo.ID + 100, Kind: inboxmodel.KindIssue, Number: 1}).Error)

	// JSON columns must hold the right JSON shape.
	require.Error(t, db.Exec(`INSERT INTO claude_profiles (name, add_dirs, created_at, updated_at)
		VALUES ('broken', '{"not": "an array"}', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`).Error)

	item := inboxmodel.Item{AccountID: account.ID, RepoID: repo.ID, Kind: inboxmodel.KindPR, Number: 1}
	require.NoError(t, db.Create(&item).Error)

	// Item details: labels must be an array, counts cannot go negative, and no more
	// reviewers can have reviewed than there are.
	for _, bad := range []string{
		`INSERT INTO item_details (item_id, labels, fetched_at) VALUES (?, '{"not": "an array"}', '2026-01-01 00:00:00')`,
		`INSERT INTO item_details (item_id, comments, fetched_at) VALUES (?, -1, '2026-01-01 00:00:00')`,
		`INSERT INTO item_details (item_id, reviewers, reviewed, fetched_at) VALUES (?, 1, 2, '2026-01-01 00:00:00')`,
	} {
		require.Error(t, db.Exec(bad, item.ID).Error, bad)
	}

	// Details for an item that does not exist.
	require.Error(t, db.Create(&inboxmodel.ItemDetails{ItemID: item.ID + 100}).Error)
}
