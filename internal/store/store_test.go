package store_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/glebarez/go-sqlite" // registers the "sqlite" database/sql driver
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	settingsmodel "github.com/mrz1836/nilbox/internal/settings/model"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/store/migrate"
)

// open opens a database at path and closes it when the test ends.
func open(t *testing.T, path string) *gorm.DB {
	t.Helper()

	db, err := store.Open(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close(db) })

	return db
}

func TestOpenEnablesWAL(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "app.db"))

	var mode string
	require.NoError(t, db.Raw("PRAGMA journal_mode").Scan(&mode).Error)
	assert.Equal(t, "wal", mode)

	var foreignKeys int
	require.NoError(t, db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error)
	assert.Equal(t, 1, foreignKeys)

	var busyTimeout int
	require.NoError(t, db.Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error)
	assert.Equal(t, 5000, busyTimeout)
}

func TestOpenCreatesParentDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	path := filepath.Join(dir, "app.db")

	open(t, path)

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	info, err = os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the database is private to the user")
}

func TestOpenAppliesMigrations(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "app.db"))

	var newest int
	require.NoError(t, db.Raw("SELECT max(version) FROM schema_migrations").Scan(&newest).Error)

	migrations, err := migrate.Load(migrate.Embedded())
	require.NoError(t, err)
	require.NotEmpty(t, migrations)
	assert.Equal(t, migrations[len(migrations)-1].Version, newest)
}

func TestOpenReopensExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")

	db, err := store.Open(t.Context(), path)
	require.NoError(t, err)
	require.NoError(t, db.Create(&settingsmodel.Pref{Key: "example.key", Value: "kept"}).Error)
	require.NoError(t, store.Close(db))

	db = open(t, path)

	var pref settingsmodel.Pref
	require.NoError(t, db.First(&pref, "key = ?", "example.key").Error)
	assert.Equal(t, "kept", pref.Value)

	// Nothing was pending on reopen, so no backup was written.
	backups, err := filepath.Glob(path + ".backup-*")
	require.NoError(t, err)
	assert.Empty(t, backups)
}

func TestOpenRejectsInvalidPath(t *testing.T) {
	_, err := store.Open(t.Context(), "")
	require.ErrorIs(t, err, store.ErrEmptyPath)

	_, err = store.Open(t.Context(), filepath.Join(t.TempDir(), "odd?name.db"))
	require.ErrorIs(t, err, store.ErrInvalidPath)
}

func TestOpenRefusesUnmanagedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foreign.db")

	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)

	_, err = raw.ExecContext(t.Context(), "CREATE TABLE notes (id INTEGER PRIMARY KEY)")
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	_, err = store.Open(t.Context(), path)
	require.ErrorIs(t, err, migrate.ErrUnmanagedSchema)
	assert.Contains(t, err.Error(), "migrate database")
}

func TestOpenReportsUnusablePaths(t *testing.T) {
	tests := []struct {
		name string
		path func(t *testing.T) string
		want string
	}{
		{
			name: "parent is a file",
			path: func(t *testing.T) string {
				blocker := filepath.Join(t.TempDir(), "blocker")
				require.NoError(t, os.WriteFile(blocker, nil, 0o600))

				return filepath.Join(blocker, "app.db")
			},
			want: "create database directory",
		},
		{
			name: "path is a directory",
			path: func(t *testing.T) string { return t.TempDir() },
			want: "open database",
		},
		{
			name: "directory is not writable",
			path: func(t *testing.T) string {
				dir := filepath.Join(t.TempDir(), "locked")
				require.NoError(t, os.Mkdir(dir, 0o500))
				t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // restore so the temp dir can be removed

				return filepath.Join(dir, "app.db")
			},
			want: "create database file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "directory is not writable" && os.Geteuid() == 0 {
				t.Skip("directory permissions do not apply to root")
			}

			_, err := store.Open(t.Context(), tt.path(t))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestCloseClosesPool(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.db"))
	require.NoError(t, err)
	require.NoError(t, store.Close(db))

	require.Error(t, db.Exec("SELECT 1").Error)
	require.NoError(t, store.Close(db), "closing an already closed pool is harmless")
}
