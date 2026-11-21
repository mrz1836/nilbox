package testutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// plantTree fills root with folders, files, a hidden file, and a link to a folder outside
// it that holds a file of its own.
func plantTree(t *testing.T, root string) {
	t.Helper()

	elsewhere := t.TempDir()
	testutil.WriteFile(t, filepath.Join(elsewhere, "outside.txt"), "x")

	testutil.WriteFile(t, filepath.Join(root, "b", "c.txt"), "x")
	testutil.WriteFile(t, filepath.Join(root, "a.txt"), "x")
	testutil.WriteFile(t, filepath.Join(root, "a-b", "x"), "x")
	testutil.WriteFile(t, filepath.Join(root, ".hidden"), "x")
	require.NoError(t, os.Mkdir(filepath.Join(root, "empty"), 0o700))
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(root, "link")))
}

func TestDirNamesListsAFolderSorted(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	plantTree(t, root)

	assert.Equal(t, []string{".hidden", "a-b", "a.txt", "b", "empty", "link"}, testutil.DirNames(t, root),
		"every entry, hidden ones and links among them, by name, and nothing under them")
}

func TestDirNamesOfAnEmptyFolderIsAnEmptyList(t *testing.T) {
	t.Parallel()

	got := testutil.DirNames(t, t.TempDir())

	require.NotNil(t, got)
	assert.Empty(t, got)
}

func TestDirNamesFailsForAMissingFolder(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing")

	assert.True(t, fails(t, func(tb testing.TB) { testutil.DirNames(tb, missing) }))
}

func TestTreeListsEveryPathUnderRootAsItWalks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	plantTree(t, root)

	assert.Equal(t, []string{
		root,
		filepath.Join(root, ".hidden"),
		filepath.Join(root, "a-b"),
		filepath.Join(root, "a-b", "x"),
		filepath.Join(root, "a.txt"),
		filepath.Join(root, "b"),
		filepath.Join(root, "b", "c.txt"),
		filepath.Join(root, "empty"),
		filepath.Join(root, "link"),
	}, testutil.Tree(t, root), "root first, a folder before what it holds, and a link never followed")
}

func TestTreeOfAnEmptyFolderIsTheFolder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	assert.Equal(t, []string{root}, testutil.Tree(t, root))
}

func TestTreeShowsWhatWasWritten(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	plantTree(t, root)

	before := testutil.Tree(t, root)
	assert.Equal(t, before, testutil.Tree(t, root), "nothing written, the same tree")

	testutil.WriteFile(t, filepath.Join(root, "b", "new.txt"), "x")

	after := testutil.Tree(t, root)
	assert.ElementsMatch(t, append(before, filepath.Join(root, "b", "new.txt")), after)
}

func TestTreeFailsForAMissingRoot(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing")

	assert.True(t, fails(t, func(tb testing.TB) { testutil.Tree(tb, missing) }))
}
