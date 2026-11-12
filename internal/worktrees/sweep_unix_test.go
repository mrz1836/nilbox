//go:build unix

package worktrees

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestCountWorktreesNeverWaitsOnAPipe(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	// A clone whose .git is a named pipe, which nothing writes to.
	piped := filepath.Join(root, "piped")
	require.NoError(t, os.MkdirAll(piped, 0o700))
	require.NoError(t, syscall.Mkfifo(filepath.Join(piped, ".git"), 0o600))

	// A clone whose .git file points at a git directory with one registered worktree,
	// and whose commondir there is a named pipe: read as no commondir, as a missing one
	// is, so the git directory is the common one.
	gitDir := filepath.Join(t.TempDir(), "elsewhere.git")
	testutil.WriteFile(t, filepath.Join(gitDir, "worktrees", "one", "gitdir"), "/srv/wt/one/.git\n")
	require.NoError(t, syscall.Mkfifo(filepath.Join(gitDir, "commondir"), 0o600))
	testutil.WriteFile(t, filepath.Join(root, "pointed", ".git"), "gitdir: "+gitDir+"\n")

	done := make(chan int, 1)

	go func() { done <- countWorktrees(root) }()

	select {
	case count := <-done:
		assert.Equal(t, 1, count, "the pointed clone's one worktree, and nothing from the pipe")
	case <-time.After(10 * time.Second):
		require.FailNow(t, "counting the worktrees waited on a named pipe")
	}
}
