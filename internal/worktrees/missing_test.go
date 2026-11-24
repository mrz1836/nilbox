package worktrees

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// halfRemoved deletes files from a worktree's folder, as a removal that stopped partway
// leaves it: tracked files gone, the index still holding them as HEAD has them.
func halfRemoved(t *testing.T, path string, files ...string) {
	t.Helper()

	for _, f := range files {
		require.NoError(t, os.Remove(filepath.Join(path, f)))
	}
}

func TestRemoveTakesAWorktreeWhoseFilesAreOnlyMissing(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newRepo(t, "alpha")
	half := addWorktree(t, alpha, filepath.Join(e.home, "wt", "half"), "half")
	testutil.Commit(t, half, filepath.Join("src", "app.go"), "package app\n")
	halfRemoved(t, half, "README.md", filepath.Join("src", "app.go"))
	mixed := addWorktree(t, alpha, filepath.Join(e.home, "wt", "mixed"), "mixed")
	halfRemoved(t, mixed, "README.md")
	require.NoError(t, os.WriteFile(filepath.Join(mixed, "notes.txt"), []byte("draft\n"), 0o600))

	snap := e.list(t)

	row := mustRow(t, snap, half)
	assert.True(t, row.Dirty)
	assert.Equal(t, 2, row.DirtyCount)
	assert.Equal(t, 2, row.MissingCount)
	assert.True(t, row.CanRemove, "removing it loses none of its files")
	assert.False(t, row.CanForceRemove, "it has nothing to discard")
	assert.Equal(t, 1, snap.CleanCount, "Remove all clean takes it too")

	row = mustRow(t, snap, mixed)
	assert.Equal(t, 2, row.DirtyCount)
	assert.Equal(t, 1, row.MissingCount)
	assert.False(t, row.CanRemove, "its untracked file would be lost")
	assert.True(t, row.CanForceRemove)

	require.NoError(t, testutil.ErrOf(e.svc.Remove(t.Context(), alpha, half, nil)))
	assert.NoDirExists(t, half)
	assert.NotContains(t, gitWorktreePaths(t, alpha), resolvedPath(half))
	assert.Contains(t, branches(t, alpha), "half", "its branch, whose commit has every missing file, stays")

	var refused *RefusedError

	require.ErrorAs(t, testutil.ErrOf(e.svc.Remove(t.Context(), alpha, mixed, nil)), &refused)
	assert.Equal(t, reasonDirty, refused.Reason, "git's refusal stands")
	assert.FileExists(t, filepath.Join(mixed, "notes.txt"))
}

func TestRemoveForcesNothingWhileTheProcessesAreUnknown(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newRepo(t, "alpha")
	half := addWorktree(t, alpha, filepath.Join(e.home, "wt", "half"), "half")
	halfRemoved(t, half, "README.md")

	svc := e.linked(linkDeps{running: &testutil.Lister[procs.Process]{Err: procs.ErrUnsupported}})

	snap, err := svc.List(t.Context())
	require.NoError(t, err)
	assert.False(t, mustRow(t, snap, half).CanRemove, "its removal needs --force, which waits for the processes")

	err = testutil.ErrOf(svc.Remove(t.Context(), alpha, half, nil))
	require.ErrorIs(t, err, ErrSessionsUnknown)
	assert.Contains(t, uierr.Message(err), "won't remove it while files are missing from its folder")
	assert.DirExists(t, half)
	assert.Contains(t, gitWorktreePaths(t, alpha), resolvedPath(half))
}

func TestRemoveLeavesAWorktreeWithASubmoduleToGit(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newRepo(t, "alpha")
	lib := e.repoAt(t, filepath.Join(e.home, "lib"))
	half := addWorktree(t, alpha, filepath.Join(e.home, "wt", "half"), "half")
	testutil.RunGit(t, half, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "lib")
	testutil.RunGit(t, half, "commit", "-q", "-m", "add lib")
	halfRemoved(t, half, "README.md")

	row := mustRow(t, e.list(t), half)
	require.True(t, row.onlyMissing(), "a clean submodule is no change of its own")

	var refused *RefusedError

	require.ErrorAs(t, testutil.ErrOf(e.svc.Remove(t.Context(), alpha, half, nil)), &refused)
	assert.NotEqual(t, reasonDirty, refused.Reason,
		"git checks the submodules before the changes, so it refuses the worktree for them, and nothing forces it")
	assert.FileExists(t, filepath.Join(half, "lib", "README.md"), "the submodule's folder is untouched")
	assert.Contains(t, gitWorktreePaths(t, alpha), resolvedPath(half))
}

func TestCleanUpRemovesAFinishedWorktreeWhoseFilesAreOnlyMissing(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	repo := e.newGitHubRepo(t, "example-repo")
	path, head := agentWorktree(t, repo, "pr-184")
	pushed(t, repo, "feat/locktimes", head)
	halfRemoved(t, path, "README.md", "fix.txt")

	gh := &fakeGitHub{}
	gh.set("example-repo", 184, pr(ItemMerged, head))
	svc := e.linked(linkDeps{gh: gh})

	snap, err := svc.List(t.Context())
	require.NoError(t, err)

	row := mustRow(t, snap, path)
	assert.Equal(t, StageFinished, row.Stage, "a removal that stopped partway left nothing to lose")
	assert.True(t, row.CanCleanUp)

	res, err := svc.CleanUp(t.Context(), repo, path, []string{"worktree-pr-184"})
	require.NoError(t, err)
	assert.True(t, res.Removed)
	assert.Equal(t, []string{"worktree-pr-184"}, res.DeletedBranches, "every commit on it is on GitHub")
	assert.NotContains(t, gitWorktreePaths(t, repo), resolvedPath(path))
}

func TestSweepCleansUpAWorktreeAStoppedRemovalLeft(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	repo := e.newGitHubRepo(t, "example-repo")
	half, head := agentWorktree(t, repo, "pr-8")
	pushed(t, repo, "eight", head)
	halfRemoved(t, half, "README.md")

	changed, changedHead := agentWorktree(t, repo, "pr-9")
	pushed(t, repo, "nine", changedHead)
	halfRemoved(t, changed, "README.md")
	require.NoError(t, os.WriteFile(filepath.Join(changed, "fix.txt"), []byte("more\n"), 0o600))

	gh := &fakeGitHub{}
	gh.set("example-repo", 8, pr(ItemMerged, head))
	gh.set("example-repo", 9, pr(ItemMerged, changedHead))

	report := e.linked(linkDeps{gh: gh, auto: true}).Sweep(t.Context())
	assert.Empty(t, report.Failed)
	require.Len(t, report.Cleaned, 1)
	assert.Equal(t, "acme-org/example-repo#8", report.Cleaned[0].Item)
	assert.Equal(t, 1, report.Leftover, "pr-9 holds a change of its own, so it needs a look")
	assert.NotContains(t, gitWorktreePaths(t, repo), resolvedPath(half))
	assert.FileExists(t, filepath.Join(changed, "fix.txt"))
}

func TestCleanUpSaysWhatKeepsAWorktreeWithMissingFiles(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		wt   Worktree
		want string
	}{
		{
			name: "only missing files, but commits not on GitHub",
			wt:   Worktree{Item: &LinkedItem{State: ItemMerged}, Dirty: true, DirtyCount: 3, MissingCount: 3},
			want: "it has commits that are not on GitHub",
		},
		{
			name: "missing files beside a change of its own",
			wt:   Worktree{Item: &LinkedItem{State: ItemMerged}, Dirty: true, DirtyCount: 3, MissingCount: 2},
			want: "it has uncommitted changes",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, notFinishedReason(tt.wt))
		})
	}
}
