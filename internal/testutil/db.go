package testutil

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/store"
)

// DB opens a fresh, migrated database in a temporary directory and closes it when the
// test ends.
func DB(t testing.TB) *gorm.DB {
	t.Helper()

	return DBAt(t, filepath.Join(t.TempDir(), "app.db"))
}

// DBAt opens the database at path, creating and migrating it when needed, and closes it
// when the test ends. A test of what survives a restart opens the same path twice.
func DBAt(t testing.TB, path string) *gorm.DB {
	t.Helper()

	db, err := store.Open(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close(db) })

	return db
}

// ClosedDB returns a migrated database that is already closed, so every query on it
// fails, for testing how code reports a database it cannot use.
func ClosedDB(t testing.TB) *gorm.DB {
	t.Helper()

	db := DB(t)
	require.NoError(t, store.Close(db))

	return db
}
