package store_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/store/migrate"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// setPref stores value under example.key in the database at path, opened and closed.
func setPref(t *testing.T, path, value string) {
	t.Helper()

	opened, err := store.Open(t.Context(), path)
	require.NoError(t, err)

	_, err = opened.DB.ExecContext(t.Context(), `INSERT OR REPLACE INTO prefs (key, value, updated_at)
VALUES ('example.key', ?, CURRENT_TIMESTAMP)`, value)
	require.NoError(t, err)
	require.NoError(t, opened.DB.Close())
}

// prefIn reads example.key from the database at path, opened with Open as the app does.
func prefIn(t *testing.T, path string) string {
	t.Helper()

	opened, err := store.Open(t.Context(), path)
	require.NoError(t, err)

	defer func() { _ = opened.DB.Close() }()

	require.Nil(t, opened.Reset, "the database opens as it is")

	var value string
	require.NoError(t, opened.DB.QueryRowContext(t.Context(),
		"SELECT value FROM prefs WHERE key = 'example.key'").Scan(&value))

	return value
}

func TestBackupNamesAndPrunesAsTheMigrationsDo(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	db := testutil.DBAt(t, path)

	migrations, err := migrate.Load(migrate.Embedded())
	require.NoError(t, err)

	newest := migrations[len(migrations)-1].Version

	written := make([]string, 0, 4)

	for range 4 {
		backup, backupErr := store.Backup(t.Context(), db, path)
		require.NoError(t, backupErr)
		assert.True(t, strings.HasPrefix(filepath.Base(backup), "app.db"+migrate.BackupInfix), backup)
		assert.Regexp(t, `-v`+strconv.Itoa(newest)+`(-\d+)?$`, backup, "named for the schema")

		written = append(written, backup)
	}

	assert.ElementsMatch(t, written[1:], backupsOf(t, path), "the newest three are kept")

	snapshots, err := store.Snapshots(path)
	require.NoError(t, err)
	require.Len(t, snapshots, 3)

	for _, s := range snapshots {
		assert.Equal(t, store.SnapshotBackup, s.Kind, "listed as a backup")
	}
}

func TestBackupOfAClosedDatabaseFails(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	db := testutil.DBAt(t, path)
	require.NoError(t, db.Close())

	_, err := store.Backup(t.Context(), db, path)
	require.ErrorContains(t, err, "back up the database")
	assert.Empty(t, backupsOf(t, path))
}

func TestRestoreBackupPutsTheBackupInPlaceAndKeepsTheNewerDatabase(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	setPref(t, path, "before the update")

	opened, err := store.Open(t.Context(), path)
	require.NoError(t, err)

	backup, err := store.Backup(t.Context(), opened.DB, path)
	require.NoError(t, err)
	require.NoError(t, opened.DB.Close())

	setPref(t, path, "after the update")

	aside, err := store.RestoreBackup(t.Context(), path, backup)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(aside, path+".broken-"), aside)
	assert.NoFileExists(t, path+"-wal", "no log of the newer database is left beside the restored one")
	assert.NoFileExists(t, path+"-shm")
	assert.FileExists(t, backup, "the backup is copied, not moved")
	assert.Equal(t, "after the update", prefIn(t, aside), "the newer database is kept whole")

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	assert.Equal(t, "before the update", prefIn(t, path), "the restored database opens, holding the backup's data")

	snapshots, err := store.Snapshots(path)
	require.NoError(t, err)
	assert.Contains(t, kinds(snapshots), store.SnapshotSetAside, "the newer database is kept, set aside")
}

func TestRestoreBackupWithNoDatabaseCopiesTheBackup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	setPref(t, path, "kept")

	opened, err := store.Open(t.Context(), path)
	require.NoError(t, err)

	backup, err := store.Backup(t.Context(), opened.DB, path)
	require.NoError(t, err)
	require.NoError(t, opened.DB.Close())

	for _, suffix := range []string{"", "-wal", "-shm"} {
		require.NoError(t, os.RemoveAll(path+suffix))
	}

	aside, err := store.RestoreBackup(t.Context(), path, backup)
	require.NoError(t, err)
	assert.Empty(t, aside, "nothing to set aside")
	assert.Equal(t, "kept", prefIn(t, path))
}

func TestRestoreBackupRefusesAFileThatIsntOneOfTheDatabasesBackups(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	setPref(t, path, "current")

	elsewhere := filepath.Join(t.TempDir(), "app.db"+migrate.BackupInfix+"20260102T030405Z-v1")
	testutil.WriteFile(t, elsewhere, "SQLite format 3\x00")

	notADatabase := path + migrate.BackupInfix + "20260102T030405Z-v1"
	testutil.WriteFile(t, notADatabase, "not a database")

	setAside := path + ".broken-20260102T030405Z"
	testutil.WriteFile(t, setAside, "SQLite format 3\x00")

	folder := path + migrate.BackupInfix + "20260102T030406Z-v1"
	require.NoError(t, os.Mkdir(folder, 0o700))

	for name, backup := range map[string]string{
		"in another folder": elsewhere, "not a database": notADatabase, "set aside, not backed up": setAside,
		"a folder": folder, "another database's": filepath.Join(dir, "other.db"+migrate.BackupInfix+"20260102T030405Z-v1"),
		"the database itself": path,
	} {
		_, err := store.RestoreBackup(t.Context(), path, backup)
		require.ErrorIs(t, err, store.ErrNotABackup, name)
	}

	assert.Equal(t, "current", prefIn(t, path), "nothing moved")
	assert.Equal(t, []string{setAside}, setAsideFiles(t, path), "nothing was set aside but the test's own file")
}

func TestRestoreBackupRefusesWhileTheDatabaseIsOpen(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	db := testutil.DBAt(t, path)

	backup, err := store.Backup(t.Context(), db, path)
	require.NoError(t, err)

	conn, err := db.Conn(t.Context())
	require.NoError(t, err)

	t.Cleanup(func() { _ = conn.Close() })

	_, err = conn.ExecContext(t.Context(), "BEGIN IMMEDIATE")
	require.NoError(t, err)

	t.Cleanup(func() { _, _ = conn.ExecContext(t.Context(), "ROLLBACK") })

	waits := 0

	_, err = store.RestoreBackupWith(t.Context(), path, backup, 3, func(context.Context) error {
		waits++

		return nil
	})
	require.Error(t, err)
	assert.True(t, store.IsBusy(err), "the database is busy: %v", err)
	assert.Equal(t, 2, waits, "it waits between its tries for the other copy to let go")
	assert.Empty(t, setAsideFiles(t, path), "nothing moved while the database is open")
}

// kinds lists the kinds of snapshots.
func kinds(snapshots []store.SnapshotFile) []store.SnapshotKind {
	out := make([]store.SnapshotKind, 0, len(snapshots))
	for _, s := range snapshots {
		out = append(out, s.Kind)
	}

	return out
}
