//go:build unix

package paths

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pipeWait bounds how long a read of a named pipe may take before the test calls it hung:
// one that refuses the pipe returns at once.
const pipeWait = 10 * time.Second

// refusesInTime runs read, failing the test when it doesn't return within pipeWait, as
// opening a pipe that nothing writes to the usual way never does.
func refusesInTime(t *testing.T, what string, read func() error) {
	t.Helper()

	done := make(chan error, 1)

	go func() { done <- read() }()

	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrNotRegular, what)
	case <-time.After(pipeWait):
		require.FailNow(t, what+" waited on a named pipe")
	}
}

func TestNoReadWaitsOnANamedPipe(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pipe := filepath.Join(dir, "pipe")
	require.NoError(t, syscall.Mkfifo(pipe, 0o600))

	link := filepath.Join(dir, "linked")
	require.NoError(t, os.Symlink(pipe, link))

	for _, path := range []string{pipe, link} {
		refusesInTime(t, "opening "+filepath.Base(path), func() error {
			_, err := OpenRegular(path)

			return err
		})

		refusesInTime(t, "reading the head of "+filepath.Base(path), func() error {
			_, err := ReadHead(path, 64)

			return err
		})

		refusesInTime(t, "reading the tail of "+filepath.Base(path), func() error {
			_, _, err := ReadTail(path, 64)

			return err
		})
	}

	// A pipe swapped in after the path was looked at is opened without waiting, and
	// refused once open.
	refusesInTime(t, "opening a pipe swapped in", func() error {
		f, err := openChecked(pipe)
		if f != nil {
			assert.Fail(t, "a pipe was kept open")

			_ = f.Close()
		}

		return err
	})
}
