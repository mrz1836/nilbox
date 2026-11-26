package terminal

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// awkwardFolder is a folder path with a space, both quotes, and a backslash in it.
const awkwardFolder = `/tmp/work dir/it's "x"\y`

func TestWarpFolderArgs(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{"/work/example-repo", awkwardFolder, "/tmp/Application Support/example/worktrees/pr-7"} {
		assert.Equal(t, []string{"-b", "dev.warp.Warp-Stable", dir}, warpFolderArgs(dir),
			"Warp is opened by its bundle ID, and the folder is one argv element, never quoted")
	}
}

func TestCheckFolder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	spaced := filepath.Join(root, `it's "a" folder`)
	require.NoError(t, os.Mkdir(spaced, 0o700))

	file := filepath.Join(root, "notes.txt")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	for name, dir := range map[string]string{"a folder": root, "an awkward name": spaced} {
		require.NoError(t, CheckFolder(dir), name)
	}

	tests := []struct {
		name    string
		dir     string
		missing bool
	}{
		{name: "relative", dir: "work/example-repo"},
		{name: "empty", dir: ""},
		{name: "a newline", dir: root + "\nrm -rf ~"},
		{name: "an escape", dir: root + "\x1b[2J"},
		{name: "a file", dir: file},
		{name: "missing", dir: filepath.Join(root, "gone"), missing: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := CheckFolder(tt.dir)
			require.ErrorIs(t, err, ErrInvalidFolder)
			assert.Equal(t, tt.missing, errors.Is(err, fs.ErrNotExist), "only a folder that isn't there is fs.ErrNotExist")
		})
	}
}
