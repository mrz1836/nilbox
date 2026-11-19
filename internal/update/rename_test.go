package update_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/update"
)

func TestARenameAfterALookNeverReplacesWhatIsThere(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	from := filepath.Join(dir, "copy")
	require.NoError(t, os.Mkdir(from, 0o700))

	empty := filepath.Join(dir, "empty")
	require.NoError(t, os.Mkdir(empty, 0o700))

	require.ErrorIs(t, update.RenameAfterLook(from, empty), fs.ErrExist,
		"an empty folder, which a plain rename would replace, is something there")
	assert.DirExists(t, from)

	to := filepath.Join(dir, "placed")
	require.NoError(t, update.RenameAfterLook(from, to))
	assert.DirExists(t, to)
	assert.NoDirExists(t, from)

	file := filepath.Join(dir, "a-file")
	testutil.WriteFile(t, file, "")

	err := update.RenameAfterLook(to, filepath.Join(file, "inside"))
	require.ErrorContains(t, err, "look where the copy goes", "a place it can't look at")
	require.NotErrorIs(t, err, fs.ErrExist)

	err = update.RenameAfterLook(filepath.Join(dir, "missing"), filepath.Join(dir, "elsewhere"))
	require.ErrorContains(t, err, "put the copy in place")
	require.ErrorIs(t, err, fs.ErrNotExist)
}
