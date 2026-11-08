package store_test

import (
	"bytes"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/store/migrate"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// garbageText is a file that is not a database.
const garbageText = "this is not a database file, just text long enough to have a header"

// asideSuffix is what a database set aside at setAsideAt has added to its name.
const asideSuffix = ".broken-20260102T030405Z"

// setAsideAt is the clock the self-heal tests open with.
func setAsideAt() time.Time { return time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC) }

// damage is what the damaged-database case writes over the schema's root page.
func damage() []byte { return bytes.Repeat([]byte{0xff}, 400) }

// preSquashHistory is the migration history a database had before its migrations were
// combined into one: each version, name, and file checksum.
func preSquashHistory() [][3]any {
	return [][3]any{
		{1, "init", "e0a22b503faef27899852e69e1833fbfa9de8094da4da0e0bf55bda9933ce60b"},
		{2, "item_details", "3e6301eba3e9169d24f98f622bfdd8d35a5309029f9da1fa111d396adf1607e7"},
		{3, "repo_details", "2123dd5c40121c704d0c86dbcba8e02b8355088aa91292bf1c257d8b920acf23"},
		{4, "prompts", "7b9017a1fed8c512701f6e7591d0d9b3e6f0b003b5ce3b87db752f92e7acecbd"},
		{5, "launch_defaults", "bbfe3664d810505a6dfb3676c11a90f355dd0e45b28f1716e7702634cc6acc11"},
		{6, "item_author_type", "9b3dde394849ba245287c06cbf6a3a259730ac824ac0f5ba7f0f451d64e583d2"},
		{7, "item_triage", "da0bd10860024ea26c37da0ad7d1ee0e21a62cffa21d875bb70644b9d42616c0"},
		{8, "snooze_wake", "f0dd51a341b84cfb486c0a69c4c4f46242d64c51592dcdaf2149ed5f4f8c6bcb"},
		{9, "worktree_launches", "da81032c53553190a5162ec27745a8fbe368d131caa8fff1876dbe1466895c97"},
		{10, "worktree_cleanups", "9c14aefebc67ea06ecd22889a516535602c9db270a77522e15602112e87bb4e1"},
		{11, "item_pin", "76dd5d270c3990c08b351f4e83963d2372907856bde2d0b7e12b1bd7e9828516"},
		{12, "item_files", "b09022f79f2630608bc9ff14ce1eae216bcd0ad413309c8a99831f9b8a757f08"},
		{13, "notifications", "f9e617695e05f2964c45c052b660a7eed91b84f7d3184484ee676f64ecebfb97"},
	}
}

// rawDB opens the database file at path with no migrations, closing it when the test
// ends.
func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()

	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })

	return raw
}

// migrated creates a migrated database at path, runs stmts on it, and closes it.
func migrated(t *testing.T, path string, stmts ...string) {
	t.Helper()

	storePref(t, path)

	raw := rawDB(t, path)

	for _, stmt := range stmts {
		_, err := raw.ExecContext(t.Context(), stmt)
		require.NoError(t, err, stmt)
	}

	require.NoError(t, raw.Close())
}

// readFile reads a file the test wrote.
func readFile(t *testing.T, path string) []byte {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // the test's own file
	require.NoError(t, err)

	return body
}

// setAsideFiles lists the files set aside next to the database at path.
func setAsideFiles(t *testing.T, path string) []string {
	t.Helper()

	found, err := filepath.Glob(path + ".broken-*")
	require.NoError(t, err)

	return found
}

// sameBytes reports whether the file at path holds want, without printing either.
func sameBytes(t *testing.T, want []byte, path string) bool {
	t.Helper()

	return bytes.Equal(want, readFile(t, path))
}

// garbage writes a file that is not a database to path.
func garbage(t *testing.T, path string) {
	t.Helper()

	testutil.WriteFile(t, path, garbageText)
}

// assertGarbage checks that the file at path is still the one garbage wrote.
func assertGarbage(t *testing.T, path string) {
	t.Helper()

	assert.Equal(t, garbageText, string(readFile(t, path)))
}

// assertPrefKept checks that the database at path still holds the pref storePref wrote.
func assertPrefKept(t *testing.T, path string) {
	t.Helper()

	var value string
	require.NoError(t, rawDB(t, path).QueryRowContext(t.Context(),
		"SELECT value FROM prefs WHERE key = 'example.key'").Scan(&value))
	assert.Equal(t, "kept", value)
}

// assertFresh checks that db is a newly created, migrated database with no data in it.
func assertFresh(t *testing.T, db *sql.DB) {
	t.Helper()

	migrations, err := migrate.Load(migrate.Embedded())
	require.NoError(t, err)
	assert.Equal(t, migrations[len(migrations)-1].Version, newestVersion(t, db))

	var prefs int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM prefs").Scan(&prefs))
	assert.Zero(t, prefs, "the fresh database starts empty")
}

func TestOpenSetsAsideADatabaseItCannotUse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		setup  func(t *testing.T, path string)
		reason store.ResetReason
		cause  error
		// kept checks that the database set aside at path is the one setup made.
		kept func(t *testing.T, path string)
	}{
		{
			name:   "garbage file",
			setup:  garbage,
			reason: store.ResetCorrupt,
			kept:   assertGarbage,
		},
		{
			name: "damaged database",
			setup: func(t *testing.T, path string) {
				migrated(t, path)

				body := readFile(t, path)
				// The schema's root page follows the 100-byte file header.
				copy(body[100:], damage())
				require.NoError(t, os.WriteFile(path, body, 0o600))
			},
			reason: store.ResetCorrupt,
			kept: func(t *testing.T, path string) {
				assert.True(t, bytes.Contains(readFile(t, path), damage()), "the damaged file is kept")
			},
		},
		{
			name: "unmanaged table",
			setup: func(t *testing.T, path string) {
				raw := rawDB(t, path)
				_, err := raw.ExecContext(t.Context(), "CREATE TABLE notes (id INTEGER PRIMARY KEY)")
				require.NoError(t, err)
				require.NoError(t, raw.Close())
			},
			reason: store.ResetUnmanaged,
			cause:  migrate.ErrUnmanagedSchema,
			kept: func(t *testing.T, path string) {
				var notes int
				require.NoError(t, rawDB(t, path).QueryRowContext(t.Context(), "SELECT count(*) FROM notes").Scan(&notes))
			},
		},
		{
			name: "edited checksum",
			setup: func(t *testing.T, path string) {
				migrated(t, path, "UPDATE schema_migrations SET checksum = 'edited' WHERE version = 1")
			},
			reason: store.ResetHistory,
			cause:  migrate.ErrChecksumMismatch,
			kept:   assertPrefKept,
		},
		{
			name: "pre-squash history",
			setup: func(t *testing.T, path string) {
				migrated(t, path, "DELETE FROM schema_migrations")

				raw := rawDB(t, path)

				for _, row := range preSquashHistory() {
					_, err := raw.ExecContext(t.Context(), `INSERT INTO schema_migrations (version, name, checksum, applied_at)
VALUES (?, ?, ?, '2026-01-01T00:00:00Z')`, row[0], row[1], row[2])
					require.NoError(t, err)
				}

				require.NoError(t, raw.Close())
			},
			reason: store.ResetHistory,
			cause:  migrate.ErrChecksumMismatch,
			kept:   assertPrefKept,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "app.db")
			tt.setup(t, path)
			before := readFile(t, path)

			opened, err := store.OpenWith(t.Context(), path, nil, setAsideAt)
			require.NoError(t, err)
			t.Cleanup(func() { _ = opened.DB.Close() })

			require.NotNil(t, opened.Reset, "the database is reported as reset")
			assert.Equal(t, path+asideSuffix, opened.Reset.Path)
			assert.Equal(t, tt.reason, opened.Reset.Reason)
			require.Error(t, opened.Reset.Err)
			require.NotErrorIs(t, opened.Reset.Err, store.ErrNewerSchema)

			if tt.cause != nil {
				require.ErrorIs(t, opened.Reset.Err, tt.cause)
			}

			assert.Equal(t, []string{opened.Reset.Path}, setAsideFiles(t, path))
			assert.Empty(t, backupsOf(t, path), "a fresh database has nothing to back up")
			assertFresh(t, opened.DB)
			assert.True(t, sameBytes(t, before, opened.Reset.Path), "the database is set aside exactly as it was")
			tt.kept(t, opened.Reset.Path)
		})
	}
}

func TestSetAsideMovesTheLogFilesWithTheDatabase(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	garbage(t, path)
	testutil.WriteFile(t, path+"-wal", "an old write-ahead log")
	testutil.WriteFile(t, path+"-shm", "an old shared-memory index")

	aside, err := store.SetAside(path, setAsideAt())
	require.NoError(t, err)

	assert.Equal(t, path+asideSuffix, aside)
	assertGarbage(t, aside)
	assert.Equal(t, "an old write-ahead log", string(readFile(t, aside+"-wal")))
	assert.Equal(t, "an old shared-memory index", string(readFile(t, aside+"-shm")))

	for _, gone := range []string{path, path + "-wal", path + "-shm"} {
		assert.NoFileExists(t, gone)
	}
}

func TestSetAsidePutsTheLogFilesBackWhenTheDatabaseCannotMove(t *testing.T) {
	t.Parallel()

	// The log files are there but the database is not, so its rename fails after
	// theirs.
	path := filepath.Join(t.TempDir(), "app.db")
	testutil.WriteFile(t, path+"-wal", "a write-ahead log")
	testutil.WriteFile(t, path+"-shm", "a shared-memory index")

	_, err := store.SetAside(path, setAsideAt())
	require.ErrorIs(t, err, fs.ErrNotExist)

	assert.Equal(t, "a write-ahead log", string(readFile(t, path+"-wal")))
	assert.Equal(t, "a shared-memory index", string(readFile(t, path+"-shm")))
	assert.Empty(t, setAsideFiles(t, path), "nothing is left half moved")
}

func TestSetAsideGivesUpWhenEveryNameIsTaken(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	garbage(t, path)
	testutil.WriteFile(t, path+asideSuffix, "")

	for i := 1; i <= 100; i++ {
		testutil.WriteFile(t, fmt.Sprintf("%s%s-%d", path, asideSuffix, i), "")
	}

	_, err := store.SetAside(path, setAsideAt())
	require.ErrorIs(t, err, store.ErrNoAsideName)
	assertGarbage(t, path)
}

func TestOpenSetsAsideUnderAFreeName(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	garbage(t, path)

	// An earlier reset in the same second took the name, and another left a log behind.
	testutil.WriteFile(t, path+asideSuffix, "an earlier reset")
	testutil.WriteFile(t, path+asideSuffix+"-1-wal", "an earlier reset's log")

	opened, err := store.OpenWith(t.Context(), path, nil, setAsideAt)
	require.NoError(t, err)
	t.Cleanup(func() { _ = opened.DB.Close() })

	require.NotNil(t, opened.Reset)
	assert.Equal(t, path+asideSuffix+"-2", opened.Reset.Path)
	assert.Equal(t, "an earlier reset", string(readFile(t, path+asideSuffix)), "an earlier file is left alone")
}

func TestOpenNamesASetAsideDatabaseByTheTime(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	garbage(t, path)

	opened, err := store.Open(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = opened.DB.Close() })

	require.NotNil(t, opened.Reset)
	assert.Regexp(t, `app\.db\.broken-\d{8}T\d{6}Z$`, opened.Reset.Path)
}

func TestOpenSetsAsideAnOutOfOrderHistory(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	later, next := laterMigrations(t, "CREATE TABLE example_later (id INTEGER PRIMARY KEY);\n")
	later[fmt.Sprintf("%04d_ahead.sql", next+1)] = &fstest.MapFile{Data: []byte("SELECT 1;\n")}

	all, err := migrate.Load(later)
	require.NoError(t, err)

	ahead := all[len(all)-1]

	// The database records the migration after next, unchanged, but next never ran.
	migrated(t, path, fmt.Sprintf(`INSERT INTO schema_migrations (version, name, checksum, applied_at)
VALUES (%d, '%s', '%s', '2026-01-01T00:00:00Z')`, ahead.Version, ahead.Name, ahead.Checksum))

	opened, err := store.OpenWith(t.Context(), path, later, setAsideAt)
	require.NoError(t, err)
	t.Cleanup(func() { _ = opened.DB.Close() })

	require.NotNil(t, opened.Reset)
	assert.Equal(t, store.ResetHistory, opened.Reset.Reason)
	require.ErrorIs(t, opened.Reset.Err, migrate.ErrOutOfOrder)
	assert.Equal(t, path+asideSuffix, opened.Reset.Path)
}

func TestOpenRefusesADatabaseFromANewerBuild(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	migrated(t, path, `INSERT INTO schema_migrations (version, name, checksum, applied_at)
VALUES (9999, 'from_the_future', 'abc', '2026-01-01T00:00:00Z')`)

	before := readFile(t, path)
	stat, err := os.Stat(path)
	require.NoError(t, err)

	_, err = store.Open(t.Context(), path)
	require.ErrorIs(t, err, store.ErrNewerSchema)
	require.ErrorIs(t, err, migrate.ErrNewerSchema)
	assert.Contains(t, err.Error(), "newer version of "+brand.Name)
	assert.Contains(t, err.Error(), "update "+brand.Name)

	after, err := os.Stat(path)
	require.NoError(t, err)
	assert.True(t, sameBytes(t, before, path), "the file is left exactly as it was")
	assert.Equal(t, stat.ModTime(), after.ModTime(), "the file is not even written")
	assert.Empty(t, setAsideFiles(t, path))
	assert.Empty(t, backupsOf(t, path))
}

func TestOpenRefusesANewerRollbackJournalDatabaseWithoutChangingIt(t *testing.T) {
	t.Parallel()

	// A database in the default rollback-journal mode, which the pool's connections
	// would switch to WAL mode as they connect.
	path := filepath.Join(t.TempDir(), "app.db")
	raw := rawDB(t, path)
	_, err := migrate.Apply(t.Context(), raw, path, migrate.Options{})
	require.NoError(t, err)
	_, err = raw.ExecContext(t.Context(), `INSERT INTO schema_migrations (version, name, checksum, applied_at)
VALUES (9999, 'from_the_future', 'abc', '2026-01-01T00:00:00Z')`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	before := readFile(t, path)
	require.Equal(t, []byte{1, 1}, before[18:20], "the header says rollback journal")

	_, err = store.Open(t.Context(), path)
	require.ErrorIs(t, err, store.ErrNewerSchema)

	assert.True(t, sameBytes(t, before, path), "the file is left exactly as it was")
	assert.NoFileExists(t, path+"-wal")
	assert.NoFileExists(t, path+"-shm")
}

func TestOpenPutsTheDatabaseBackWhenNoFreshOneStarts(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	garbage(t, path)

	// The fresh database fails to migrate, after the unusable one was set aside.
	broken := fstest.MapFS{"0001_broken.sql": &fstest.MapFile{Data: []byte("INSERT INTO missing_table VALUES (1);\n")}}

	_, err := store.OpenWith(t.Context(), path, broken, setAsideAt)
	require.ErrorIs(t, err, migrate.ErrMigrationFailed)
	assert.Contains(t, err.Error(), "start a fresh database in its place")

	assertGarbage(t, path)
	assert.Empty(t, setAsideFiles(t, path), "nothing is left set aside")
	assert.NoFileExists(t, path+"-wal")
	assert.NoFileExists(t, path+"-shm")

	// The next start meets the same file, sets it aside, and says so.
	opened, err := store.OpenWith(t.Context(), path, nil, setAsideAt)
	require.NoError(t, err)
	t.Cleanup(func() { _ = opened.DB.Close() })
	require.NotNil(t, opened.Reset)
	assertGarbage(t, opened.Reset.Path)
}

func TestOpenLeavesADatabaseAnotherConnectionLocks(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")

	// A database with a table the migrations did not create is set aside when it can be
	// read; locked, it must not be.
	raw := rawDB(t, path)
	conn, err := raw.Conn(t.Context())
	require.NoError(t, err)

	_, err = conn.ExecContext(t.Context(), "CREATE TABLE notes (id INTEGER PRIMARY KEY)")
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), "BEGIN EXCLUSIVE")
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = conn.ExecContext(t.Context(), "ROLLBACK")
		_ = conn.Close()
	})

	_, err = store.Open(t.Context(), path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "locked")
	assert.True(t, store.IsBusy(err), "the startup dialog tells a lock apart: %v", err)
	assert.Empty(t, setAsideFiles(t, path), "a lock is not mistaken for an unusable file")
}

func TestOpenLeavesAWALDatabaseAnotherConnectionHasOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// use has the other connection use the database, and returns what it does once
		// Open gave up, if anything.
		use func(t *testing.T, conn *sql.Conn) func()
		// wrote says the other connection stores the example.other pref.
		wrote bool
	}{
		{
			name: "an idle reader",
			use: func(t *testing.T, conn *sql.Conn) func() {
				var prefs int
				require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT count(*) FROM prefs").Scan(&prefs))

				return nil
			},
		},
		{
			name: "a writer mid-transaction",
			use: func(t *testing.T, conn *sql.Conn) func() {
				_, err := conn.ExecContext(t.Context(), "BEGIN IMMEDIATE")
				require.NoError(t, err)
				_, err = conn.ExecContext(t.Context(), `INSERT INTO prefs (key, value, updated_at)
VALUES ('example.other', 'written', CURRENT_TIMESTAMP)`)
				require.NoError(t, err)

				return func() {
					_, err = conn.ExecContext(t.Context(), "COMMIT")
					require.NoError(t, err)
				}
			},
			wrote: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// The history disagrees with this build's, so with no one else using it the
			// database would be set aside.
			path := filepath.Join(t.TempDir(), "app.db")
			migrated(t, path, "UPDATE schema_migrations SET checksum = 'edited' WHERE version = 1")

			conn, err := rawDB(t, path).Conn(t.Context())
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })

			var mode string
			require.NoError(t, conn.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&mode))
			require.Equal(t, "wal", mode)

			finish := tt.use(t, conn)

			_, err = store.OpenWith(t.Context(), path, nil, setAsideAt)
			require.ErrorIs(t, err, migrate.ErrChecksumMismatch, "the error says what is wrong with it")
			assert.Contains(t, err.Error(), "no other process has the database open")
			assert.Empty(t, setAsideFiles(t, path), "a database in use is not moved")

			if finish != nil {
				finish()
			}

			require.NoError(t, conn.Close())
			assertPrefKept(t, path)

			if tt.wrote {
				var value string
				require.NoError(t, rawDB(t, path).QueryRowContext(t.Context(),
					"SELECT value FROM prefs WHERE key = 'example.other'").Scan(&value))
				assert.Equal(t, "written", value, "the other connection's write is in the database at the path")
			}
		})
	}
}

func TestOpenLeavesADatabaseInAReadOnlyDirectory(t *testing.T) {
	t.Parallel()

	testutil.SkipAsRoot(t)

	tests := []struct {
		name  string
		setup func(t *testing.T, path string)
	}{
		{
			name: "a database it could use",
			setup: func(t *testing.T, path string) {
				migrated(t, path)
			},
		},
		{
			name: "a file it would set aside",
			setup: func(t *testing.T, path string) {
				garbage(t, path)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "data")
			require.NoError(t, os.Mkdir(dir, 0o700))

			path := filepath.Join(dir, "app.db")
			tt.setup(t, path)
			before := readFile(t, path)

			require.NoError(t, os.Chmod(dir, 0o500))       //nolint:gosec // a read-only directory is the case under test
			t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // restore so the temp dir can be removed

			_, err := store.Open(t.Context(), path)
			require.Error(t, err)
			assert.True(t, sameBytes(t, before, path), "the file stays where it is, as it was")
			assert.Empty(t, setAsideFiles(t, path))
		})
	}
}

func TestOpenLeavesADatabaseWhoseMigrationFailed(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	storePref(t, path)

	broken, _ := laterMigrations(t, "INSERT INTO missing_table VALUES (1);\n")

	_, err := store.OpenWith(t.Context(), path, broken, setAsideAt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migrate database")
	assert.Empty(t, setAsideFiles(t, path), "a failed migration does not discard the data")
	assert.Len(t, backupsOf(t, path), 1, "the database was backed up before the migration ran")

	db := testutil.DBAt(t, path)

	var value string
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT value FROM prefs WHERE key = 'example.key'").Scan(&value))
	assert.Equal(t, "kept", value)
}

func TestOpenLeavesADatabaseWhoseMigrationFailedAsDamaged(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	storePref(t, path)

	// The migration leaves a schema entry SQLite cannot parse and makes it read the
	// schema again, which fails as a damaged database does.
	corrupting, _ := laterMigrations(t, `PRAGMA writable_schema = ON;
INSERT INTO sqlite_master (type, name, tbl_name, rootpage, sql) VALUES ('table', 'bad', 'bad', 0, 'CREATE TABLE bad (');
PRAGMA writable_schema = RESET;
SELECT * FROM bad;
`)

	_, err := store.OpenWith(t.Context(), path, corrupting, setAsideAt)
	require.ErrorIs(t, err, migrate.ErrMigrationFailed)
	assert.Contains(t, err.Error(), "malformed")
	assert.Empty(t, setAsideFiles(t, path), "a failed migration does not discard the data")
	assert.Len(t, backupsOf(t, path), 1, "the database was backed up before the migration ran")

	// The migration was rolled back, so the database still opens as it was.
	db := testutil.DBAt(t, path)

	var value string
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT value FROM prefs WHERE key = 'example.key'").Scan(&value))
	assert.Equal(t, "kept", value)
}

func TestResetReasonPutsAFailedMigrationFirst(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	migrated(t, path)

	body := readFile(t, path)
	copy(body[100:], damage())
	require.NoError(t, os.WriteFile(path, body, 0o600))

	var tables int

	damaged := rawDB(t, path).QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master").Scan(&tables)
	require.Error(t, damaged)

	reason, unusable := store.ResetReasonOf(damaged)
	assert.True(t, unusable)
	assert.Equal(t, store.ResetCorrupt, reason)

	_, unusable = store.ResetReasonOf(fmt.Errorf("%w: apply 0002_example.sql: %w", migrate.ErrMigrationFailed, damaged))
	assert.False(t, unusable, "a migration that failed is never a reason to set the database aside")
}
