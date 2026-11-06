package workspacetrust

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLockTakesAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json.lock")

	unlock, err := lock(t.Context(), path, time.Second)
	require.NoError(t, err)
	assert.DirExists(t, path, "the lock is a directory, as the agent makes it")

	unlock()
	assert.NoDirExists(t, path)
}

func TestLockWaitsForALiveHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json.lock")
	require.NoError(t, os.Mkdir(path, lockPerm))

	_, err := lock(t.Context(), path, 50*time.Millisecond)
	require.ErrorIs(t, err, ErrBusy)
	assert.DirExists(t, path, "a live holder's lock is left alone")

	// A holder that lets go in time hands the lock over.
	go func() {
		time.Sleep(50 * time.Millisecond)

		_ = os.Remove(path)
	}()

	unlock, err := lock(t.Context(), path, 5*time.Second)
	require.NoError(t, err)
	unlock()
}

func TestLockClearsAStaleLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json.lock")
	require.NoError(t, os.Mkdir(path, lockPerm))

	old := time.Now().Add(-2 * lockStale)
	require.NoError(t, os.Chtimes(path, old, old))

	unlock, err := lock(t.Context(), path, 50*time.Millisecond)
	require.NoError(t, err, "a lock left by a process that is gone is cleared")

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now(), info.ModTime(), time.Minute, "the lock is a fresh one")

	unlock()
}

func TestLockStopsWhenCancelled(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json.lock")
	require.NoError(t, os.Mkdir(path, lockPerm))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := lock(ctx, path, time.Minute)
	require.ErrorIs(t, err, context.Canceled)
}

func TestLockFailsWithoutItsDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", ".claude.json.lock")

	_, err := lock(t.Context(), path, time.Second)
	require.ErrorIs(t, err, os.ErrNotExist)
}
