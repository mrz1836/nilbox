//go:build unix

package codex

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadMetaNeverWaitsOnAFIFO(t *testing.T) {
	t.Parallel()

	// A rollout swapped for a FIFO after its folder was listed must not hang the observer:
	// opening a FIFO for reading waits for a writer that never comes.
	path := filepath.Join(t.TempDir(), "rollout-2026-11-09T00-17-28-x.jsonl")
	require.NoError(t, syscall.Mkfifo(path, 0o600))

	done := make(chan rolloutEntry, 1)

	go func() {
		entry, _ := readMeta(path)
		done <- entry
	}()

	select {
	case entry := <-done:
		assert.False(t, entry.ok, "a FIFO is no rollout")
	case <-time.After(5 * time.Second):
		require.Fail(t, "reading a FIFO hung")
	}
}
