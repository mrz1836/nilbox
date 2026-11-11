package hooks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// changesRig is a clone as run-whitespace-dirty-file found it, and a Service that reads it.
type changesRig struct {
	dir string
	svc *Service
}

// newChangesRig builds a clone with README.md committed with trailing spaces, an
// unstaged edit in docs/setup.md, a staged edit in main.go, a file both staged and
// edited, and an untracked note; or skips the test without git.
func newChangesRig(t *testing.T) changesRig {
	t.Helper()

	env := testutil.Git(t)
	dir := filepath.Join(testutil.RealTempDir(t), "clone")

	testutil.InitRepo(t, dir)
	writeFile(t, dir, "README.md", "# clone  \n", 0o600)
	writeFile(t, dir, "docs/setup.md", "setup\n", 0o600)
	writeFile(t, dir, "main.go", "package main\n", 0o600)
	writeFile(t, dir, "both.go", "package main\n", 0o600)
	testutil.RunGit(t, dir, "add", ".")
	testutil.RunGit(t, dir, "commit", "-q", "-m", "first")

	writeFile(t, dir, "docs/setup.md", "setup\nmore  \n", 0o600)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n", 0o600)
	testutil.RunGit(t, dir, "add", "main.go")
	writeFile(t, dir, "both.go", "package main // staged\n", 0o600)
	testutil.RunGit(t, dir, "add", "both.go")
	writeFile(t, dir, "both.go", "package main // staged, then edited\n", 0o600)
	writeFile(t, dir, "notes.txt", "notes\n", 0o600)

	git := gitread.New(gitread.Config{Env: func() []string { return env }})

	return changesRig{dir: dir, svc: NewService(Config{Git: git})}
}

func TestChangedFilesFindsEveryChange(t *testing.T) {
	t.Parallel()

	r := newChangesRig(t)
	before := r.svc.take(t.Context(), r.dir)
	require.NoError(t, before.err)
	assert.True(t, before.hashed)
	assert.Len(t, before.entries, 4)

	// What whitespace does: fix README.md and stage it, and fix docs/setup.md, already
	// dirty, staging it with the user's own edit.
	writeFile(t, r.dir, "README.md", "# clone\n", 0o600)
	writeFile(t, r.dir, "docs/setup.md", "setup\nmore\n", 0o600)
	testutil.RunGit(t, r.dir, "add", "README.md", "docs/setup.md")

	// Rewrite the dirty both.go without changing its status: only its hash tells.
	writeFile(t, r.dir, "both.go", "package main // staged, then edited, then fixed\n", 0o600)

	// Bring main.go back to its last commit, index and all.
	testutil.RunGit(t, r.dir, "reset", "-q", "main.go")
	writeFile(t, r.dir, "main.go", "package main\n", 0o600)

	// A new untracked file.
	writeFile(t, r.dir, "fixed.txt", "new\n", 0o600)

	after := r.svc.takeAfter(t.Context(), r.dir, before)
	require.NoError(t, after.err)

	assert.Equal(t, []ChangedFile{
		{Path: "README.md", Staged: true, By: []Check{}},
		{Path: "both.go", WasDirty: true, By: []Check{}},
		{Path: "docs/setup.md", Staged: true, WasDirty: true, By: []Check{}},
		{Path: "fixed.txt", By: []Check{}},
		{Path: "main.go", Staged: true, WasDirty: true, By: []Check{}},
	}, changedFiles(before, after))
}

func TestChangedFilesOfARunThatChangedNothing(t *testing.T) {
	t.Parallel()

	r := newChangesRig(t)
	before := r.svc.take(t.Context(), r.dir)
	after := r.svc.takeAfter(t.Context(), r.dir, before)

	assert.Empty(t, changedFiles(before, after))
	assert.NotNil(t, changedFiles(before, after))
}

func TestChangedFilesSeesAFileRewrittenWithItsSize(t *testing.T) {
	t.Parallel()

	r := newChangesRig(t)

	// Past maxHashed files, sizes and times tell files apart, before and after alike.
	before := r.svc.read(t.Context(), r.dir, func(int) bool { return false })
	assert.False(t, before.hashed)

	writeFile(t, r.dir, "notes.txt", "longer notes\n", 0o600)

	after := r.svc.takeAfter(t.Context(), r.dir, before)
	assert.False(t, after.hashed)
	assert.Equal(t, []ChangedFile{{Path: "notes.txt", WasDirty: true, By: []Check{}}}, changedFiles(before, after))
}

func TestSnapshotOfAFolderGitCantRead(t *testing.T) {
	t.Parallel()

	r := newChangesRig(t)
	snap := r.svc.take(t.Context(), t.TempDir())
	require.Error(t, snap.err)
}

func TestFileSum(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "a", 0o600)
	require.NoError(t, os.Symlink("a.txt", filepath.Join(dir, "link")))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o700))

	assert.Equal(t, "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb", fileSum(dir, "a.txt", true))
	assert.Contains(t, fileSum(dir, "a.txt", false), "size:1 time:")
	assert.Equal(t, "link:a.txt", fileSum(dir, "link", true))
	assert.Equal(t, "folder", fileSum(dir, "sub/", true))
	assert.Equal(t, "gone", fileSum(dir, "missing", true))
	assert.Equal(t, "outside", fileSum(dir, "../a.txt", true))
	assert.Equal(t, "outside", fileSum(dir, "/etc/hosts", true))
}
