//go:build unix

package claude_test

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/sessions/observe/claude"
)

func TestLastMessageRefusesAPipe(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "s.jsonl")
	require.NoError(t, syscall.Mkfifo(path, 0o600))

	_, err := claude.LastMessage(path)
	require.Error(t, err, "a pipe where the transcript should be is refused, never opened and waited on")
}
