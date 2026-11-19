package store_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// prefsDB is a migrated database at a path, holding three prefs, and that path.
func prefsDB(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "app.db")
	db := testutil.DBAt(t, path)

	for _, key := range []string{"a", "b", "c"} {
		_, err := db.ExecContext(t.Context(),
			"INSERT INTO prefs (key, value, updated_at) VALUES (?, ?, '2026-06-15 12:00:00+00:00')", key, "v-"+key)
		require.NoError(t, err)
	}

	return path
}

func TestReadOnlyQuery(t *testing.T) {
	t.Parallel()

	path := prefsDB(t)

	t.Run("reads the rows a query finds, each value as text", func(t *testing.T) {
		t.Parallel()

		got, err := store.ReadOnlyQuery(t.Context(), path, "SELECT key, value FROM prefs ORDER BY key", 10)
		require.NoError(t, err)

		assert.Equal(t, store.QueryResult{
			Columns: []string{"key", "value"},
			Rows:    [][]string{{"a", "v-a"}, {"b", "v-b"}, {"c", "v-c"}},
		}, got)
	})

	t.Run("stops at the rows allowed and says it had more", func(t *testing.T) {
		t.Parallel()

		got, err := store.ReadOnlyQuery(t.Context(), path, "SELECT key FROM prefs ORDER BY key", 2)
		require.NoError(t, err)

		assert.Equal(t, [][]string{{"a"}, {"b"}}, got.Rows)
		assert.True(t, got.Truncated)
	})

	t.Run("words NULL, a blob, a number, and text", func(t *testing.T) {
		t.Parallel()

		got, err := store.ReadOnlyQuery(t.Context(), path, "SELECT NULL, x'ff00', x'6869', 42, 'it''s; fine'", 1)
		require.NoError(t, err)

		assert.Equal(t, [][]string{{"NULL", "<2 bytes>", "hi", "42", "it's; fine"}}, got.Rows)
	})

	t.Run("runs one statement, whatever its comments and quotes hold", func(t *testing.T) {
		t.Parallel()

		got, err := store.ReadOnlyQuery(t.Context(), path,
			"/* a; b */ SELECT count(*) AS \"n;\" FROM [prefs] -- trailing; comment\n;", 1)
		require.NoError(t, err)

		assert.Equal(t, []string{"n;"}, got.Columns)
		assert.Equal(t, [][]string{{"3"}}, got.Rows)
	})

	t.Run("reads a PRAGMA that sets nothing", func(t *testing.T) {
		t.Parallel()

		got, err := store.ReadOnlyQuery(t.Context(), path, "PRAGMA query_only", 1)
		require.NoError(t, err)

		assert.Equal(t, [][]string{{"1"}}, got.Rows)
	})
}

func TestReadOnlyQueryRefusesWrites(t *testing.T) {
	t.Parallel()

	path := prefsDB(t)

	for _, query := range []string{
		"DELETE FROM prefs",
		"SELECT 1; DELETE FROM prefs",
		"PRAGMA query_only = 0",
		"ATTACH 'other.db' AS other",
		"   ",
	} {
		_, err := store.ReadOnlyQuery(t.Context(), path, query, 10)
		require.ErrorIs(t, err, store.ErrNotReadOnly, query)
	}

	// A statement that reads first and writes after gets past the check, and the
	// read-only connection refuses it.
	_, err := store.ReadOnlyQuery(t.Context(), path, "WITH gone AS (SELECT 'a') DELETE FROM prefs WHERE key IN gone", 10)
	require.Error(t, err)

	got, err := store.ReadOnlyQuery(t.Context(), path, "SELECT count(*) FROM prefs", 1)
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"3"}}, got.Rows, "nothing was deleted")
}

func TestReadOnlyQueryNeedsAPath(t *testing.T) {
	t.Parallel()

	_, err := store.ReadOnlyQuery(t.Context(), "", "SELECT 1", 1)
	require.ErrorIs(t, err, store.ErrEmptyPath)

	_, err = store.Tables(t.Context(), "app.db?mode=rw")
	require.ErrorIs(t, err, store.ErrInvalidPath)
}

func TestReadOnlyQueryReportsABadQuery(t *testing.T) {
	t.Parallel()

	_, err := store.ReadOnlyQuery(t.Context(), prefsDB(t), "SELECT nope FROM prefs", 1)
	require.ErrorContains(t, err, "run the query")
}

func TestTables(t *testing.T) {
	t.Parallel()

	tables, err := store.Tables(t.Context(), prefsDB(t))
	require.NoError(t, err)

	assert.Contains(t, tables, store.TableInfo{Name: "prefs", Rows: 3})
	assert.IsNonDecreasing(t, names(tables), "the tables come in order")
}

// names returns the tables' names, in order.
func names(tables []store.TableInfo) []string {
	out := make([]string, 0, len(tables))
	for _, table := range tables {
		out = append(out, table.Name)
	}

	return out
}
