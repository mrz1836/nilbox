package localrepos

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// fakeWorktree makes a linked worktree of the clone main at dir, as git writes one: a
// .git file pointing at main's .git/worktrees/<name>, whose commondir points back.
func fakeWorktree(t *testing.T, main, dir string) {
	t.Helper()

	admin := filepath.Join(main, ".git", "worktrees", filepath.Base(dir))
	testutil.WriteFile(t, filepath.Join(admin, "commondir"), "../..\n")
	testutil.WriteFile(t, filepath.Join(admin, "gitdir"), filepath.Join(dir, ".git")+"\n")
	testutil.WriteFile(t, filepath.Join(dir, ".git"), "gitdir: "+admin+"\n")
}

func TestClassify(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	folder := mkdir(t, filepath.Join(home, "src"))
	clone := fakeClone(t, filepath.Join(home, "exp"))
	pointed := filepath.Join(home, "pointed")
	testutil.WriteFile(t, filepath.Join(pointed, ".git"), "gitdir: /elsewhere\n")
	testutil.WriteFile(t, filepath.Join(home, "notes"), "x")

	kind, err := Classify(folder, home)
	require.NoError(t, err)
	assert.Equal(t, KindFolder, kind)

	for _, dir := range []string{clone, pointed} {
		kind, err = Classify(dir, home)
		require.NoError(t, err)
		assert.Equal(t, KindClone, kind, "%s", dir)
	}

	_, err = Classify(filepath.Join(home, "missing"), home)
	require.ErrorIs(t, err, ErrLocationNotFound)
	assert.Equal(t, "~/missing doesn't exist.", uierr.Message(err))

	_, err = Classify(filepath.Join(home, "notes"), home)
	require.ErrorIs(t, err, ErrNotAFolder)
	assert.Equal(t, "~/notes is a file, not a folder.", uierr.Message(err))

	assert.True(t, KindFolder.Valid())
	assert.True(t, KindClone.Valid())
	assert.False(t, Kind("").Valid())
}

func TestCheckAddable(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	main := fakeClone(t, filepath.Join(home, "y"))
	wt := filepath.Join(home, "y-wt")
	fakeWorktree(t, main, wt)

	bare := filepath.Join(home, "bare.git")
	bareWT := filepath.Join(home, "bare-wt")
	testutil.WriteFile(t, filepath.Join(bare, "worktrees", "bare-wt", "commondir"), "../..\n")
	testutil.WriteFile(t, filepath.Join(bareWT, ".git"), "gitdir: "+filepath.Join(bare, "worktrees", "bare-wt")+"\n")

	separate := filepath.Join(home, "separate")
	testutil.WriteFile(t, filepath.Join(separate, ".git"), "gitdir: "+filepath.Join(home, "gits", "separate")+"\n")
	mkdir(t, filepath.Join(home, "gits", "separate"))

	require.NoError(t, CheckAddable(main, home))
	require.NoError(t, CheckAddable(mkdir(t, filepath.Join(home, "src")), home))
	require.NoError(t, CheckAddable(separate, home), "a clone whose git folder lives elsewhere is a clone of its own")
	require.NoError(t, CheckAddable(filepath.Join(home, ".config", "repos"), home), "only the folder's own name counts")

	tests := []struct {
		dir  string
		want string
	}{
		{dir: filepath.Join(home, ".hidden"), want: "~/.hidden is hidden, and the app never looks in hidden folders."},
		{
			dir:  filepath.Join(home, "projects", "."+brand.Slug, "worktrees", "api"),
			want: "~/projects/." + brand.Slug + "/worktrees/api is inside the app's own worktrees folder.",
		},
		{dir: filepath.Join(main, ".git", "modules"), want: "~/y/.git/modules is inside a git folder."},
		{dir: wt, want: "~/y-wt is a linked worktree of ~/y; add ~/y instead."},
		{dir: bareWT, want: "~/bare-wt is a linked worktree; add the clone it belongs to instead."},
	}

	for _, tt := range tests {
		err := CheckAddable(tt.dir, home)
		require.ErrorIs(t, err, ErrLocationRefused, "%s", tt.dir)
		assert.Equal(t, tt.want, uierr.Message(err))
	}
}

func TestCommonDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	main := fakeClone(t, filepath.Join(root, "main"))
	wt := filepath.Join(root, "wt")
	fakeWorktree(t, main, wt)

	relative := filepath.Join(root, "relative")
	testutil.WriteFile(t, filepath.Join(relative, ".git"), "gitdir: ../gits/relative\n")
	mkdir(t, filepath.Join(root, "gits", "relative"))

	broken := filepath.Join(root, "broken")
	testutil.WriteFile(t, filepath.Join(broken, ".git"), "not a pointer\n")

	assert.Equal(t, paths.Resolved(filepath.Join(main, ".git")), CommonDir(main))
	assert.Equal(t, paths.Resolved(filepath.Join(main, ".git")), CommonDir(wt), "a linked worktree's common folder")
	assert.Equal(t, paths.Resolved(filepath.Join(root, "gits", "relative")), CommonDir(relative),
		"a relative pointer is taken from the clone, and no commondir means the git folder")
	assert.Empty(t, CommonDir(broken))
	assert.Empty(t, CommonDir(mkdir(t, filepath.Join(root, "plain"))))

	require.NoError(t, os.Remove(filepath.Join(main, ".git", "worktrees", "wt", "commondir")))
	assert.Equal(t, paths.Resolved(filepath.Join(main, ".git", "worktrees", "wt")), CommonDir(wt))
}
