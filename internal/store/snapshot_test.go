package store_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// oldPrompts is a prompts table as an older build might have made it: no model column,
// and a column this build has never heard of.
const oldPrompts = `CREATE TABLE prompts (
    id         INTEGER  PRIMARY KEY,
    name       TEXT     NOT NULL,
    nickname   TEXT     NOT NULL,
    body       TEXT     NOT NULL,
    plan_mode  INTEGER,
    retired    TEXT     NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL
)`

// snapshotDB writes a database at path that holds oldPrompts with two rows, and closes
// it.
func snapshotDB(t *testing.T, path string) {
	t.Helper()

	raw := rawDB(t, path)

	for _, stmt := range []string{
		oldPrompts,
		`INSERT INTO prompts (name, nickname, body, plan_mode, updated_at)
		 VALUES ('Review a pull request', 'Review', 'Review {{.URL}}', 1, '2026-11-05 22:14:47.975532+00:00')`,
		`INSERT INTO prompts (name, nickname, body, plan_mode, updated_at)
		 VALUES ('Triage', 'Triage', 'Triage {{.URL}}', NULL, '2026-11-06 10:00:00+00:00')`,
	} {
		_, err := raw.ExecContext(t.Context(), stmt)
		require.NoError(t, err, stmt)
	}

	require.NoError(t, raw.Close())
}

// openSnapshot opens the snapshot at path, closing it when the test ends.
func openSnapshot(t *testing.T, path string) *store.Snapshot {
	t.Helper()

	snap, err := store.OpenSnapshot(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = snap.Close() })

	return snap
}

// dirNames lists the names in dir.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	return names
}

func TestSnapshotReadsTheColumnsItHas(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db.broken-20261108T165152Z")
	snapshotDB(t, path)

	rows, err := openSnapshot(t, path).Rows(t.Context(), "prompts", "nickname", "model", "plan_mode", "body")
	require.NoError(t, err)
	require.Len(t, rows, 2)

	assert.Equal(t, store.Row{"nickname": "Review", "plan_mode": int64(1), "body": "Review {{.URL}}"}, rows[0],
		"a column the table lacks is left out of the row")
	assert.Equal(t, store.Row{"nickname": "Triage", "plan_mode": nil, "body": "Triage {{.URL}}"}, rows[1])
	assert.Empty(t, rows[0].Text("model"), "and reads as empty")
}

func TestSnapshotReadsDatetimeColumnsAsTimes(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	snapshotDB(t, path)

	rows, err := openSnapshot(t, path).Rows(t.Context(), "prompts", "updated_at")
	require.NoError(t, err)
	require.Len(t, rows, 2)

	at, ok := rows[0]["updated_at"].(time.Time)
	require.True(t, ok, "got %T", rows[0]["updated_at"])
	assert.True(t, at.Equal(time.Date(2026, time.November, 5, 22, 14, 47, 975532000, time.UTC)))
}

func TestSnapshotCountsRowsWhenNoColumnAskedForExists(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	snapshotDB(t, path)

	rows, err := openSnapshot(t, path).Rows(t.Context(), "prompts", "starter_key")
	require.NoError(t, err)
	assert.Equal(t, []store.Row{{}, {}}, rows)
}

func TestSnapshotReportsAMissingTable(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	snapshotDB(t, path)

	_, err := openSnapshot(t, path).Rows(t.Context(), "launch_defaults", "owner")
	require.ErrorIs(t, err, store.ErrNoTable)
}

func TestSnapshotRefusesNamesThatAreNotIdentifiers(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	snapshotDB(t, path)
	snap := openSnapshot(t, path)

	for _, names := range [][]string{
		{`prompts"; DROP TABLE prompts; --`, "name"},
		{"prompts", "name", "body) FROM prompts; --"},
		{"", "name"},
	} {
		_, err := snap.Rows(t.Context(), names[0], names[1:]...)
		require.ErrorIs(t, err, store.ErrNotIdentifier, "%q", names)
	}
}

func TestSnapshotChangesNothingOnDisk(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "app.db.backup-20261107T031018Z-v12")
	snapshotDB(t, path)
	before := readFile(t, path)

	snap, err := store.OpenSnapshot(t.Context(), path)
	require.NoError(t, err)

	_, err = snap.Rows(t.Context(), "prompts", "name")
	require.NoError(t, err)
	require.NoError(t, snap.Close())

	assert.True(t, sameBytes(t, before, path), "the file is read, never written")
	assert.Equal(t, []string{filepath.Base(path)}, dirNames(t, dir), "no -wal or -shm file is left beside it")
}

func TestSnapshotReadsTheWritesStillInItsLog(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	live := filepath.Join(dir, "live.db")
	kept := filepath.Join(dir, "kept.db")

	// A connection that never checkpoints leaves its writes in the -wal file, as a
	// database set aside with its log holds them.
	raw := rawDB(t, live)
	for _, stmt := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA wal_autocheckpoint = 0",
		oldPrompts,
		`INSERT INTO prompts (name, nickname, body, updated_at) VALUES ('Logged', 'Logged', 'x', '2026-11-08 00:00:00+00:00')`,
	} {
		_, err := raw.ExecContext(t.Context(), stmt)
		require.NoError(t, err, stmt)
	}

	for _, suffix := range []string{"", "-wal"} {
		testutil.WriteFile(t, kept+suffix, string(readFile(t, live+suffix)))
	}

	keptBefore, logBefore := readFile(t, kept), readFile(t, kept+"-wal")

	snap, err := store.OpenSnapshot(t.Context(), kept)
	require.NoError(t, err)

	rows, err := snap.Rows(t.Context(), "prompts", "nickname")
	require.NoError(t, err)
	assert.Equal(t, []store.Row{{"nickname": "Logged"}}, rows)
	require.NoError(t, snap.Close())

	assert.True(t, sameBytes(t, keptBefore, kept), "the database is left as it was")
	assert.True(t, sameBytes(t, logBefore, kept+"-wal"), "its log is left as it was")
	assert.NoFileExists(t, kept+"-shm", "the log is read from a copy, so no index is made beside it")
}

func TestOpenSnapshotRefusesWhatIsNotADatabase(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	text := filepath.Join(dir, "notes.db")
	garbage(t, text)

	_, err := store.OpenSnapshot(t.Context(), text)
	require.Error(t, err, "a text file")

	_, err = store.OpenSnapshot(t.Context(), dir)
	require.ErrorIs(t, err, store.ErrNotAFile, "a directory")

	_, err = store.OpenSnapshot(t.Context(), filepath.Join(dir, "missing.db"))
	require.ErrorIs(t, err, os.ErrNotExist, "a missing file")

	assertGarbage(t, text)
}

func TestSnapshotsListsTheKeptDatabasesNewestFirst(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")

	for _, name := range []string{
		"app.db",
		"app.db-wal",
		"app.db.backup-20261106T201810Z-v10",
		"app.db.backup-20261107T031018Z-v12",
		"app.db.broken-20261108T165152Z",
		"app.db.broken-20261108T165152Z-wal",
		"app.db.broken-20261108T165152Z-shm",
		"app.db.broken-20261108T165152Z-1",
		"app.db.broken-not-a-stamp",
		"other.db.broken-20261108T165152Z",
		"prompts.json",
	} {
		testutil.WriteFile(t, filepath.Join(dir, name), "x")
	}

	require.NoError(t, os.Mkdir(filepath.Join(dir, "app.db.backup-20261109T000000Z-v13"), 0o700))

	got, err := store.Snapshots(path)
	require.NoError(t, err)

	at := func(s string) time.Time {
		parsed, parseErr := time.Parse("20060102T150405Z", s)
		require.NoError(t, parseErr)

		return parsed
	}

	assert.Equal(t, []store.SnapshotFile{
		{Path: filepath.Join(dir, "app.db.broken-20261108T165152Z-1"), Kind: store.SnapshotSetAside, At: at("20261108T165152Z")},
		{Path: filepath.Join(dir, "app.db.broken-20261108T165152Z"), Kind: store.SnapshotSetAside, At: at("20261108T165152Z")},
		{Path: filepath.Join(dir, "app.db.backup-20261107T031018Z-v12"), Kind: store.SnapshotBackup, At: at("20261107T031018Z")},
		{Path: filepath.Join(dir, "app.db.backup-20261106T201810Z-v10"), Kind: store.SnapshotBackup, At: at("20261106T201810Z")},
	}, got)
}

func TestSnapshotsOfAMissingDirectoryIsEmpty(t *testing.T) {
	t.Parallel()

	got, err := store.Snapshots(filepath.Join(t.TempDir(), "missing", "app.db"))
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.NotNil(t, got)
}
