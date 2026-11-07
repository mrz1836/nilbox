package store_test

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	_ "github.com/glebarez/go-sqlite" // registers the "sqlite" database/sql driver
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/store/migrate"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// poolSize is how many connections the store's pool keeps.
const poolSize = 4

// holdConns takes n connections from db at once, so the pool has to open n distinct
// ones, and returns them to the pool when the test ends.
func holdConns(t *testing.T, db *sql.DB, n int) []*sql.Conn {
	t.Helper()

	conns := make([]*sql.Conn, 0, n)

	for range n {
		conn, err := db.Conn(t.Context())
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })

		conns = append(conns, conn)
	}

	return conns
}

// pragma reads one PRAGMA's value on conn.
func pragma[T any](t *testing.T, conn *sql.Conn, name string) T {
	t.Helper()

	var v T
	require.NoError(t, conn.QueryRowContext(t.Context(), "PRAGMA "+name).Scan(&v))

	return v
}

func TestOpenConfiguresEveryPooledConnection(t *testing.T) {
	t.Parallel()

	db := testutil.SQLDB(t)
	conns := holdConns(t, db, poolSize)
	require.Equal(t, poolSize, db.Stats().OpenConnections, "each connection is a separate one")

	for i, conn := range conns {
		assert.Equal(t, "wal", pragma[string](t, conn, "journal_mode"), "connection %d", i)
		assert.Equal(t, 1, pragma[int](t, conn, "foreign_keys"), "connection %d", i)
		assert.Equal(t, 5000, pragma[int](t, conn, "busy_timeout"), "connection %d", i)
	}
}

func TestOpenKeepsAFewConnectionsOpen(t *testing.T) {
	t.Parallel()

	db := testutil.SQLDB(t)
	assert.Equal(t, poolSize, db.Stats().MaxOpenConnections)

	for _, conn := range holdConns(t, db, poolSize) {
		require.NoError(t, conn.Close())
	}

	assert.Equal(t, poolSize, db.Stats().Idle, "every connection stays open once it is returned")
}

// stamp is an example pref's updated_at as the database holds it.
type stamp struct {
	key  string
	text string
	typ  string // its SQLite storage class
}

// storedStamps returns the example prefs' updated_at values, ordered by them.
func storedStamps(t *testing.T, db *sql.DB) []stamp {
	t.Helper()

	// The cast keeps the driver from parsing the DATETIME column back into a time.Time.
	rows, err := db.QueryContext(t.Context(), `SELECT key, CAST(updated_at AS TEXT), typeof(updated_at)
FROM prefs WHERE key LIKE 'example.%' ORDER BY updated_at, key`)
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	var got []stamp

	for rows.Next() {
		var s stamp
		require.NoError(t, rows.Scan(&s.key, &s.text, &s.typ))

		got = append(got, s)
	}

	require.NoError(t, rows.Err())

	return got
}

// keysAndTexts splits stamps into their keys and their texts.
func keysAndTexts(stamps []stamp) (keys, texts []string) {
	for _, s := range stamps {
		keys = append(keys, s.key)
		texts = append(texts, s.text)
	}

	return keys, texts
}

// assertUTCText checks that every stamp is TEXT in the one format the stored timestamps
// use.
func assertUTCText(t *testing.T, stamps []stamp) {
	t.Helper()

	format := regexp.MustCompile(`^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d(\.\d+)?\+00:00$`)

	for _, s := range stamps {
		assert.Equal(t, "text", s.typ, s.text)
		assert.Regexp(t, format, s.text)
	}
}

func TestTimestampsAreStoredAsSortableUTCText(t *testing.T) {
	t.Parallel()

	db := testutil.SQLDB(t)
	last := time.Date(2026, time.December, 31, 23, 59, 59, 0, time.UTC)

	// In time order, with the cases a text comparison could get wrong: a whole second
	// beside fractions of it, fractions of different lengths, and a rollover into the
	// next minute, day, and year.
	times := []time.Time{
		last.AddDate(0, 0, -1),
		last,
		last.Add(100 * time.Millisecond),
		last.Add(123 * time.Millisecond),
		last.Add(123400 * time.Microsecond),
		last.Add(time.Second - time.Nanosecond),
		last.Add(time.Second),
		last.Add(time.Second + time.Nanosecond),
	}

	// Stored newest first, so the database does the sorting.
	for i := len(times) - 1; i >= 0; i-- {
		_, err := db.ExecContext(t.Context(), "INSERT INTO prefs (key, value, updated_at) VALUES (?, 'v', ?)",
			fmt.Sprintf("example.%d", i), times[i])
		require.NoError(t, err)
	}

	wantKeys := make([]string, 0, len(times))
	wantTexts := make([]string, 0, len(times))

	for i, at := range times {
		wantKeys = append(wantKeys, fmt.Sprintf("example.%d", i))
		wantTexts = append(wantTexts, at.Format("2006-01-02 15:04:05.999999999-07:00"))
	}

	stamps := storedStamps(t, db)
	assertUTCText(t, stamps)

	keys, texts := keysAndTexts(stamps)
	assert.Equal(t, wantKeys, keys, "ordered as text, the stamps are in time order")
	assert.Equal(t, wantTexts, texts)
}

func TestOpenCreatesParentDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	path := filepath.Join(dir, "app.db")

	testutil.DBAt(t, path)

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	info, err = os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the database is private to the user")
}

func TestOpenAppliesMigrations(t *testing.T) {
	t.Parallel()

	db := testutil.SQLDB(t)

	var newest int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT max(version) FROM schema_migrations").Scan(&newest))

	migrations, err := migrate.Load(migrate.Embedded())
	require.NoError(t, err)
	require.NotEmpty(t, migrations)
	assert.Equal(t, migrations[len(migrations)-1].Version, newest)
}

func TestOpenReopensExistingDatabase(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")

	opened, err := store.Open(t.Context(), path)
	require.NoError(t, err)
	_, err = opened.DB.ExecContext(t.Context(), `INSERT INTO prefs (key, value, updated_at)
VALUES ('example.key', 'kept', CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	require.NoError(t, opened.DB.Close())

	var value string
	require.NoError(t, testutil.SQLDBAt(t, path).QueryRowContext(t.Context(),
		"SELECT value FROM prefs WHERE key = 'example.key'").Scan(&value))
	assert.Equal(t, "kept", value)

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
			if tt.name == "directory is not writable" {
				testutil.SkipAsRoot(t)
			}

			_, err := store.Open(t.Context(), tt.path(t))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}
