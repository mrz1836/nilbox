package scripts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestReadSourceWordsWhatItCantRead(t *testing.T) {
	t.Parallel()

	_, found, err := readSource(t.TempDir(), t3File)
	require.NoError(t, err)
	assert.False(t, found, "a missing file is no source")

	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, t3File), 0o700))

	_, found, err = readSource(dir, t3File)
	assert.True(t, found)
	require.ErrorIs(t, err, ErrCantRun)
	assert.Equal(t, "t3.json isn't a file.", uierr.Message(err), "a folder is refused unread")

	dir = folder(t, map[string]string{tasksFile: "{}"})
	require.NoError(t, os.Chmod(filepath.Join(dir, filepath.FromSlash(tasksFile)), 0o000))

	if _, err = os.ReadFile(filepath.Join(dir, filepath.FromSlash(tasksFile))); err == nil { //nolint:gosec // the test's own file
		t.Skip("this user reads a file whatever its mode")
	}

	_, found, err = readSource(dir, tasksFile)
	assert.True(t, found)
	require.ErrorIs(t, err, os.ErrPermission)
	assert.Equal(t, "Couldn't read .vscode/tasks.json: permission denied.", uierr.Message(err))
}

func TestSumFileTakesFromTheBudget(t *testing.T) {
	t.Parallel()

	path := filepath.Join(folder(t, map[string]string{"main.go": "package main\n"}), "main.go")

	budget := int64(64)
	sum, err := sumFile(path, &budget)
	require.NoError(t, err)
	assert.Len(t, sum, 64, "a SHA-256 in hex")
	assert.Equal(t, int64(64-len("package main\n")), budget)

	budget = 4
	_, err = sumFile(path, &budget)
	require.ErrorIs(t, err, errTooMany, "past the budget")

	_, err = sumFile(filepath.Join(t.TempDir(), "missing.go"), &budget)
	require.ErrorIs(t, err, os.ErrNotExist)
}
