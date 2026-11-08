package workspacetrust

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLockTakesAndReleases(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".claude.json.lock")

	unlock, err := lock(t.Context(), path, time.Second)
	require.NoError(t, err)
	assert.DirExists(t, path, "the lock is a directory, as the agent makes it")

	unlock()
	assert.NoDirExists(t, path)
}

func TestLockWaitsForALiveHolder(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".claude.json.lock")
	require.NoError(t, os.Mkdir(path, lockPerm))

	// The bubble's clock starts years before the lock directory's real modification
	// time, so the holder looks live for the whole test, and the waits take no real time.
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()

		_, err := lock(t.Context(), path, time.Second)
		require.ErrorIs(t, err, ErrBusy)
		assert.GreaterOrEqual(t, time.Since(start), time.Second, "a live holder is waited for the whole wait")
		assert.DirExists(t, path, "a live holder's lock is left alone")

		// A holder that lets go in time hands the lock over.
		const holdFor = 2 * time.Second

		go func() {
			time.Sleep(holdFor)

			_ = os.Remove(path)
		}()

		start = time.Now()

		unlock, err := lock(t.Context(), path, 5*time.Second)
		require.NoError(t, err)
		assert.Less(t, time.Since(start), holdFor+2*lockPoll, "the lock is taken at the first poll after the holder lets go")
		unlock()
	})
}

func TestLockClearsAStaleLock(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".claude.json.lock")
	require.NoError(t, os.Mkdir(path, lockPerm))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := lock(ctx, path, time.Minute)
	require.ErrorIs(t, err, context.Canceled)
}

func TestLockFailsWithoutItsDirectory(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing", ".claude.json.lock")

	_, err := lock(t.Context(), path, time.Second)
	require.ErrorIs(t, err, os.ErrNotExist)
}
