package store_test

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/store/migrate"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// laterMigrations returns the embedded migrations plus one more, as a later build
// would ship them, and that one's version.
func laterMigrations(t *testing.T, extra string) (fstest.MapFS, int) {
	t.Helper()

	all, err := migrate.Load(migrate.Embedded())
	require.NoError(t, err)
	require.NotEmpty(t, all)

	later := fstest.MapFS{}

	for _, m := range all {
		later[m.File] = &fstest.MapFile{Data: []byte(m.SQL)}
	}

	next := all[len(all)-1].Version + 1
	later[fmt.Sprintf("%04d_later.sql", next)] = &fstest.MapFile{Data: []byte(extra)}

	return later, next
}

// backupsOf returns the pre-migration backups written next to the database at path.
func backupsOf(t *testing.T, path string) []string {
	t.Helper()

	found, err := filepath.Glob(path + ".backup-*")
	require.NoError(t, err)

	return found
}

// newestVersion reads the newest migration applied to db.
func newestVersion(t *testing.T, db *sql.DB) int {
	t.Helper()

	var version int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT max(version) FROM schema_migrations").Scan(&version))

	return version
}

// storePref opens the database at path, stores an example pref in it, and closes it.
func storePref(t *testing.T, path string) {
	t.Helper()

	opened, err := store.Open(t.Context(), path)
	require.NoError(t, err)
	_, err = opened.DB.ExecContext(t.Context(), `INSERT INTO prefs (key, value, updated_at)
VALUES ('example.key', 'kept', CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	require.NoError(t, opened.DB.Close())
}

func TestOpenUpgradesAnOlderDatabaseAfterABackup(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	storePref(t, path)

	later, newest := laterMigrations(t, "CREATE TABLE example_later (id INTEGER PRIMARY KEY);\n")

	opened, err := store.OpenWith(t.Context(), path, later, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = opened.DB.Close() })

	assert.Nil(t, opened.Reset)
	assert.Equal(t, newest, newestVersion(t, opened.DB), "the pending migration is applied")

	var value string
	require.NoError(t, opened.DB.QueryRowContext(t.Context(),
		"SELECT value FROM prefs WHERE key = 'example.key'").Scan(&value))
	assert.Equal(t, "kept", value, "the data survives the upgrade")

	assert.Len(t, backupsOf(t, path), 1, "the database is backed up before it is changed")
}

func TestOpenKeepsAnExistingFilesPermissions(t *testing.T) {
	t.Parallel()

	testutil.SkipAsRoot(t)

	path := filepath.Join(t.TempDir(), "app.db")
	require.NoError(t, os.WriteFile(path, nil, 0o640)) //nolint:gosec // a mode the app would never choose

	testutil.DBAt(t, path)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm(), "only a new file is created private")
}
