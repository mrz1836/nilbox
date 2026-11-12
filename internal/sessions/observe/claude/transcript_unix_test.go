//go:build unix

package claude_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/sessions/observe/claude"
)

func TestLastMessageRefusesAPipe(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	require.NoError(t, syscall.Mkfifo(path, 0o600))

	link := filepath.Join(dir, "linked.jsonl")
	require.NoError(t, os.Symlink(path, link))

	done := make(chan [2]error, 1)

	go func() {
		_, pipeErr := claude.LastMessage(path)

		_, linkErr := claude.LastMessage(link)

		done <- [2]error{pipeErr, linkErr}
	}()

	select {
	case errs := <-done:
		require.ErrorIs(t, errs[0], paths.ErrNotRegular, "a pipe where the transcript should be is refused")
		require.ErrorIs(t, errs[1], paths.ErrNotRegular, "and so is a link to one")
	case <-time.After(10 * time.Second):
		require.FailNow(t, "reading a transcript waited on a named pipe")
	}
}
