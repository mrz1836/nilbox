package testutil_test

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/store/migrate"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// schema is what tells two migrated databases apart: every schema entry, the migration
// history but its times, and the settings the file itself keeps.
type schema struct {
	Entries    []string
	History    []string
	Journal    string
	PageSize   int64
	AutoVacuum int64
	User       int64
}

// schemaOf reads db's schema.
func schemaOf(t *testing.T, db *sql.DB) schema {
	t.Helper()

	var s schema

	s.Entries = column(t, db, `SELECT type || ' ' || name || ' ON ' || tbl_name || ': ' || COALESCE(sql, '')
FROM sqlite_master ORDER BY type, name`)
	s.History = column(t, db, `SELECT version || ' ' || name || ' ' || checksum FROM schema_migrations ORDER BY version`)

	ctx := t.Context()
	require.NoError(t, db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&s.Journal))
	require.NoError(t, db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&s.PageSize))
	require.NoError(t, db.QueryRowContext(ctx, "PRAGMA auto_vacuum").Scan(&s.AutoVacuum))
	require.NoError(t, db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&s.User))

	return s
}

// column returns the one text column query selects, row by row.
func column(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), query)
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	var got []string

	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		got = append(got, s)
	}

	require.NoError(t, rows.Err())

	return got
}

func TestDBMatchesAFreshlyMigratedDatabase(t *testing.T) {
	t.Parallel()

	opened, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = opened.DB.Close() })

	want := schemaOf(t, opened.DB)
	got := schemaOf(t, testutil.DB(t))

	assert.Equal(t, want, got, "a database from the template is the one store.Open migrates")
	assert.Equal(t, "wal", got.Journal)

	migrations, err := migrate.Load(migrate.Embedded())
	require.NoError(t, err)
	require.Len(t, got.History, len(migrations), "every migration is applied")

	newest := migrations[len(migrations)-1]
	assert.Equal(t, fmt.Sprintf("%d %s %s", newest.Version, newest.Name, newest.Checksum), got.History[len(got.History)-1])
}

func TestDBIsMigratedAndFresh(t *testing.T) {
	t.Parallel()

	first, second := testutil.DB(t), testutil.DB(t)
	pristine := schemaOf(t, first)
	assert.NotEmpty(t, pristine.History, "the schema is migrated")

	_, err := first.ExecContext(t.Context(), `INSERT INTO prefs (key, value, updated_at)
VALUES ('example.key', 'first', CURRENT_TIMESTAMP)`)
	require.NoError(t, err)

	_, err = second.ExecContext(t.Context(), `CREATE TABLE scratch (id INTEGER PRIMARY KEY)`)
	require.NoError(t, err)

	var count int
	require.NoError(t, second.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM prefs").Scan(&count))
	assert.Zero(t, count, "a row written to one database is not in the other")

	require.NoError(t, first.QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM sqlite_master WHERE name = 'scratch'").Scan(&count))
	assert.Zero(t, count, "a table made in one database is not in the other")

	// The template itself is never changed: a database made after both writes has neither.
	third := testutil.DB(t)
	assert.Equal(t, pristine, schemaOf(t, third), "a later database has no table made in an earlier one")

	require.NoError(t, third.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM prefs").Scan(&count))
	assert.Zero(t, count, "a later database has no row written to an earlier one")
}

func TestDBAtCreatesAMissingDatabaseAsTheStoreDoes(t *testing.T) {
	t.Parallel()

	testutil.SkipAsRoot(t)

	dir := filepath.Join(t.TempDir(), "nested", "data")
	path := filepath.Join(dir, "app.db")

	testutil.DBAt(t, path)

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), "the folder is private to the user")

	info, err = os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the database is private to the user")

	backups, err := filepath.Glob(path + migrate.BackupInfix + "*")
	require.NoError(t, err)
	assert.Empty(t, backups, "nothing was pending, so nothing was backed up")
}

func TestDBAtOpensAnExistingFileAsItIs(t *testing.T) {
	t.Parallel()

	testutil.SkipAsRoot(t)

	path := filepath.Join(t.TempDir(), "app.db")
	require.NoError(t, os.WriteFile(path, nil, 0o640)) //nolint:gosec // a mode the app would never choose

	db := testutil.DBAt(t, path)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm(), "an existing file is not replaced")
	assert.Equal(t, schemaOf(t, testutil.DB(t)), schemaOf(t, db), "the store migrates an empty file itself")
}
