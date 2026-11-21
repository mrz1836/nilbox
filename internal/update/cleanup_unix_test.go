//go:build unix

package update_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/update"
)

// pipeWait bounds how long a clean-up may take beside a named pipe before the test calls
// it hung: one that never opens the pipe returns at once.
const pipeWait = 5 * time.Second

func TestCleanUpLeavesANamedPipeAlone(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 15, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * time.Hour)
	apps := t.TempDir()
	bundle := filepath.Join(apps, brand.Name+".app")
	tmp := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(bundle, "Contents"), 0o700))

	pipe := func(path string) string {
		require.NoError(t, syscall.Mkfifo(path, 0o600))
		require.NoError(t, os.Chtimes(path, old, old))

		return path
	}

	// A regular helper log of an earlier install of this app's, whose readiness file is
	// a pipe.
	log := filepath.Join(tmp, "wails-update-78.log")
	testutil.WriteFile(t, log, "2026-11-15T09:00:00Z: helper start: target="+bundle+" new=/x pid=78\n")
	require.NoError(t, os.Chtimes(log, old, old))

	stale := pipe(filepath.Join(tmp, "wails-update-77.log"))

	linked := filepath.Join(tmp, "wails-update-79.log")
	require.NoError(t, os.Symlink(stale, linked))

	kept := []string{
		// Named for the last install's process.
		pipe(filepath.Join(tmp, "wails-update-4242.log")),
		pipe(filepath.Join(tmp, "wails-update-4242-1.ready")),
		// Old enough that a regular file of the name would be read for its target.
		stale,
		// A link to it, under another log's name.
		linked,
		// The readiness file of a log of this app's.
		pipe(filepath.Join(tmp, "wails-update-78-1.ready")),
		// Named as a staging folder beside the app.
		pipe(filepath.Join(apps, "wails-update-5")),
	}

	done := make(chan []error, 1)

	go func() { done <- update.CleanUp(bundle, tmp, 4242, now) }()

	select {
	case errs := <-done:
		assert.Empty(t, errs)
	case <-time.After(pipeWait):
		require.FailNow(t, "a clean-up waited on a named pipe")
	}

	assert.NoFileExists(t, log, "a regular helper log of this app's is removed")

	for _, path := range kept {
		_, err := os.Lstat(path)
		assert.NoError(t, err, "%s is left alone", filepath.Base(path))
	}
}
