//go:build unix

package claude

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/paths"
)

func TestUsageRefusesAPipe(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "s.jsonl")
	require.NoError(t, syscall.Mkfifo(path, 0o600))

	done := make(chan error, 1)

	go func() {
		done <- (&transcriptUsage{caps: NewUsage("").caps}).read(path)
	}()

	select {
	case err := <-done:
		require.ErrorIs(t, err, paths.ErrNotRegular, "a pipe where the transcript should be is refused")
	case <-time.After(10 * time.Second):
		require.FailNow(t, "reading a transcript's counts waited on a named pipe")
	}
}
