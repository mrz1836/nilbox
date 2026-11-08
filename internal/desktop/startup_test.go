package desktop_test

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

var errFakeStartup = errors.New("create data directory: the disk is full")

// lockedDatabaseError returns the error a write meets on a database another connection
// holds locked, at once rather than after the usual wait.
func lockedDatabaseError(t *testing.T) error {
	t.Helper()

	path := filepath.Join(t.TempDir(), "app.db")

	holder, err := testutil.DBAt(t, path).Conn(t.Context())
	require.NoError(t, err)

	_, err = holder.ExecContext(t.Context(), "BEGIN EXCLUSIVE")
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = holder.ExecContext(t.Context(), "ROLLBACK")
		_ = holder.Close()
	})

	other, err := testutil.DBAt(t, path).Conn(t.Context())
	require.NoError(t, err)

	t.Cleanup(func() { _ = other.Close() })

	_, err = other.ExecContext(t.Context(), "PRAGMA busy_timeout = 0")
	require.NoError(t, err)

	_, err = other.ExecContext(t.Context(), "BEGIN IMMEDIATE")
	require.Error(t, err)

	return fmt.Errorf("open the database: %w", err)
}

func TestAStartupFailureSaysWhatHappened(t *testing.T) {
	t.Parallel()

	couldNotStart := brand.Name + " couldn't start"

	t.Run("a database from a newer build", func(t *testing.T) {
		t.Parallel()

		got := desktop.StartupFailure(fmt.Errorf("open database: %w", store.ErrNewerSchema))

		assert.Equal(t, brand.Name+" needs an update", got.Title)
		assert.Equal(t, "The data was saved by a newer version of "+brand.Name+"; update "+brand.Name+" to open it.",
			got.Message)
	})

	t.Run("a database another copy holds", func(t *testing.T) {
		t.Parallel()

		err := lockedDatabaseError(t)
		require.True(t, store.IsBusy(err), "the fixture is a lock: %v", err)

		got := desktop.StartupFailure(err)

		assert.Equal(t, couldNotStart, got.Title)
		assert.True(t, strings.HasPrefix(got.Message, "Another copy of "+brand.Name+" may still be running"), got.Message)
		assert.Contains(t, got.Message, "\n\nOpen the database: ")
	})

	t.Run("data the app may not touch", func(t *testing.T) {
		t.Parallel()

		err := fmt.Errorf("create data directory: %w", &fs.PathError{Op: "mkdir", Path: "data", Err: fs.ErrPermission})
		got := desktop.StartupFailure(err)

		assert.Equal(t, couldNotStart, got.Title)
		assert.True(t, strings.HasPrefix(got.Message, brand.Name+" isn't allowed to read or write its data."), got.Message)
		assert.True(t, strings.HasSuffix(got.Message, "\n\nCreate data directory: mkdir data: permission denied."),
			got.Message)
	})

	t.Run("anything else", func(t *testing.T) {
		t.Parallel()

		got := desktop.StartupFailure(errFakeStartup)

		assert.Equal(t, desktop.Failure{Title: couldNotStart, Message: "Create data directory: the disk is full."}, got)
	})
}
