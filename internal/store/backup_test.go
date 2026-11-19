package store_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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
		assert.Equal(t, newest, s.Schema, "with the schema it holds")
	}

	known, err := store.KnownSchemas()
	require.NoError(t, err)
	assert.Equal(t, newest, known.Newest, "this build's newest migration")
	assert.True(t, known.Opens(newest), "a backup of this build's own opens here")
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
	assert.Empty(t, restoreCopies(t, path), "the copy made first is removed")
}

func TestRestoreBackupCopiesBeforeItMovesAnything(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	setPref(t, path, "before the update")

	opened, err := store.Open(t.Context(), path)
	require.NoError(t, err)

	backup, err := store.Backup(t.Context(), opened.DB, path)
	require.NoError(t, err)
	require.NoError(t, opened.DB.Close())

	setPref(t, path, "after the update")

	// By the time the copy is renamed into place, the copy is whole and the database aside.
	var copied, asideByThen []string

	_, err = store.RestoreBackupRenaming(t.Context(), path, backup, func(oldPath, newPath string) error {
		copied = restoreCopies(t, path)
		asideByThen = setAsideFiles(t, path)

		return os.Rename(oldPath, newPath)
	})
	require.NoError(t, err)
	assert.Len(t, copied, 1, "the backup was copied beside the database first")
	assert.Len(t, asideByThen, 1, "then the database set aside")
	assert.Empty(t, restoreCopies(t, path))
	assert.Equal(t, "before the update", prefIn(t, path))
}

func TestRestoreBackupPutsTheDatabaseBackWhenTheRenameFails(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	setPref(t, path, "before the update")

	opened, err := store.Open(t.Context(), path)
	require.NoError(t, err)

	backup, err := store.Backup(t.Context(), opened.DB, path)
	require.NoError(t, err)
	require.NoError(t, opened.DB.Close())

	setPref(t, path, "after the update")

	_, err = store.RestoreBackupRenaming(t.Context(), path, backup, func(string, string) error {
		return os.ErrPermission
	})
	require.ErrorIs(t, err, os.ErrPermission)
	assert.Empty(t, setAsideFiles(t, path), "the database is put back")
	assert.Empty(t, restoreCopies(t, path), "and the copy removed")
	assert.Equal(t, "after the update", prefIn(t, path), "nothing changed")
	assert.FileExists(t, backup)
}

func TestRestoreBackupWithNoDatabaseRemovesTheLogsLeftBehind(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	setPref(t, path, "kept")

	opened, err := store.Open(t.Context(), path)
	require.NoError(t, err)

	backup, err := store.Backup(t.Context(), opened.DB, path)
	require.NoError(t, err)
	require.NoError(t, opened.DB.Close())

	require.NoError(t, os.Remove(path))
	testutil.WriteFile(t, path+"-wal", "the log of a database that is gone")
	testutil.WriteFile(t, path+"-shm", "its index")

	aside, err := store.RestoreBackup(t.Context(), path, backup)
	require.NoError(t, err)
	assert.Empty(t, aside)
	assert.NoFileExists(t, path+"-wal", "the copy isn't paired with another database's log")
	assert.NoFileExists(t, path+"-shm")
	assert.Equal(t, "kept", prefIn(t, path))
}

func TestOpenSweepsTheCopiesARestoreCutShortLeft(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	old := time.Now().Add(-store.StaleRestoreAfter - time.Minute)

	files := map[string]bool{
		".app.db.restore-123.tmp":      true,  // a copy a crash left
		".app.db.restore-4.tmp":        true,  // any digits
		".app.db.restore-abc.tmp":      false, // not os.CreateTemp's digits
		".app.db.restore-.tmp":         false, // no digits at all
		".app.db.restore-123.tmp.keep": false,
		"app.db.restore-123.tmp":       false, // not hidden
		".other.db.restore-123.tmp":    false, // another database's
	}

	for name := range files {
		testutil.WriteFile(t, filepath.Join(dir, name), "a copy")
		require.NoError(t, os.Chtimes(filepath.Join(dir, name), old, old))
	}

	fresh := filepath.Join(dir, ".app.db.restore-456.tmp")
	testutil.WriteFile(t, fresh, "a restore copying now")

	folder := filepath.Join(dir, ".app.db.restore-789.tmp")
	require.NoError(t, os.Mkdir(folder, 0o700))
	require.NoError(t, os.Chtimes(folder, old, old))

	opened, err := store.Open(t.Context(), path)
	require.NoError(t, err)
	require.NoError(t, opened.DB.Close())

	for name, swept := range files {
		if swept {
			assert.NoFileExists(t, filepath.Join(dir, name), name)
		} else {
			assert.FileExists(t, filepath.Join(dir, name), name)
		}
	}

	assert.FileExists(t, fresh, "a copy under way is left alone")
	assert.DirExists(t, folder, "only a file is removed")
}

// restoreCopies lists the copies of a backup a restore left beside the database at path.
func restoreCopies(t *testing.T, path string) []string {
	t.Helper()

	found, err := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".restore-*"))
	require.NoError(t, err)

	return found
}

// kinds lists the kinds of snapshots.
func kinds(snapshots []store.SnapshotFile) []store.SnapshotKind {
	out := make([]store.SnapshotKind, 0, len(snapshots))
	for _, s := range snapshots {
		out = append(out, s.Kind)
	}

	return out
}
