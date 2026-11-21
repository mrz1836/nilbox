package testutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// DirNames returns the names of what dir holds, sorted, as os.ReadDir lists them: an
// empty list, never nil, for an empty folder. It fails the test when dir can't be read,
// a missing one included.
func DirNames(t testing.TB, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	return names
}

// Tree returns every path under root, root first, each as filepath.WalkDir names it
// (root joined with its path there) and in the order it walks them: lexical, a folder
// before what it holds. A link is listed, never followed, so what it points to is left
// out. It fails the test when any of it can't be read, a missing root included.
//
// A test takes a Tree before and after the code it checks to show what that code wrote.
func Tree(t testing.TB, root string) []string {
	t.Helper()

	var out []string

	require.NoError(t, filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		out = append(out, path)

		return err
	}))

	return out
}
