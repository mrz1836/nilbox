package testutil

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store"
)

// DB opens a fresh, migrated database in a temporary directory and closes it when the
// test ends.
func DB(t testing.TB) *sql.DB {
	t.Helper()

	return DBAt(t, filepath.Join(t.TempDir(), "app.db"))
}

// DBAt opens the database at path, creating and migrating it when needed, and closes it
// when the test ends. A test of what survives a restart opens the same path twice. The
// test fails if the database at path could not be used and was set aside.
func DBAt(t testing.TB, path string) *sql.DB {
	t.Helper()

	opened, err := store.Open(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = opened.DB.Close() })
	require.Nil(t, opened.Reset, "the database at %s was set aside", path)

	return opened.DB
}

// ClosedDB returns a migrated database that is already closed, so every query on it
// fails, for testing how code reports a database it cannot use.
func ClosedDB(t testing.TB) *sql.DB {
	t.Helper()

	db := DB(t)
	require.NoError(t, db.Close())

	return db
}
