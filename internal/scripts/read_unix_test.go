//go:build unix

package scripts

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestNoSourceReadWaitsOnANamedPipe(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".vscode"), 0o700))
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, t3File), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(dir, t3File), filepath.Join(dir, filepath.FromSlash(tasksFile))))

	type read struct {
		t3, tasks, sum error
	}

	done := make(chan read, 1)

	go func() {
		var r read

		_, _, r.t3 = readSource(dir, t3File)
		_, _, r.tasks = readSource(dir, tasksFile)
		budget := int64(maxMagefileBytes)
		_, r.sum = sumFile(filepath.Join(dir, t3File), &budget) // a pipe swapped in for a magefile

		done <- r
	}()

	select {
	case r := <-done:
		assert.Equal(t, "t3.json isn't a file.", uierr.Message(r.t3))
		assert.Equal(t, ".vscode/tasks.json isn't a file.", uierr.Message(r.tasks), "a link to a pipe")
		require.ErrorIs(t, r.sum, paths.ErrNotRegular)
	case <-time.After(10 * time.Second):
		require.FailNow(t, "a read of the clone's files waited on a named pipe")
	}
}
