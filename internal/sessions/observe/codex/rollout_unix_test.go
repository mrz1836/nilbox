//go:build unix

package codex

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/paths"
)

func TestReadMetaNeverWaitsOnAFIFO(t *testing.T) {
	t.Parallel()

	// A rollout swapped for a FIFO after its folder was listed must not hang the observer:
	// opening a FIFO for reading waits for a writer that never comes.
	path := filepath.Join(t.TempDir(), "rollout-2026-11-09T00-17-28-x.jsonl")
	require.NoError(t, syscall.Mkfifo(path, 0o600))

	type read struct {
		entry rolloutEntry
		whole bool
	}

	done := make(chan read, 1)

	go func() {
		entry, whole := readMeta(path)
		done <- read{entry, whole}
	}()

	select {
	case got := <-done:
		assert.False(t, got.entry.ok, "a FIFO is no rollout")
		assert.False(t, got.whole, "and is read again next time, in case a rollout takes its place")
	case <-time.After(5 * time.Second):
		require.Fail(t, "reading a FIFO hung")
	}
}

func TestLastWordsNeverWaitOnAFIFO(t *testing.T) {
	t.Parallel()

	// A rollout swapped for a FIFO after its folder was listed is refused, never opened
	// and waited on.
	path := filepath.Join(t.TempDir(), "rollout-2026-11-09T00-17-28-x.jsonl")
	require.NoError(t, syscall.Mkfifo(path, 0o600))

	done := make(chan error, 1)

	go func() {
		_, err := lastAgentText(path, wordsTailBytes)
		done <- err
	}()

	select {
	case err := <-done:
		require.ErrorIs(t, err, paths.ErrNotRegular, "a FIFO is no rollout")
	case <-time.After(5 * time.Second):
		require.Fail(t, "reading a FIFO hung")
	}
}
