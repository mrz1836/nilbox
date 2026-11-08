package appdata_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
)

func TestWriteFileReplacesTheFileWhole(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "copy.json")

	require.NoError(t, appdata.WriteFile(path, []byte("first")))
	require.NoError(t, appdata.WriteFile(path, []byte("second")))

	got, err := appdata.ReadFile(path, 100)
	require.NoError(t, err)
	assert.Equal(t, "second", string(got))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, appdata.PrivateFilePerm, info.Mode().Perm())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file is left behind")
}

func TestWriteFileLeavesNothingWhenItCannotWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "taken")
	require.NoError(t, os.Mkdir(target, appdata.PrivateDirPerm))
	require.NoError(t, os.WriteFile(filepath.Join(target, "inside"), nil, appdata.PrivateFilePerm))

	require.Error(t, appdata.WriteFile(target, []byte("x")), "a directory that is not empty can't be replaced")
	require.ErrorContains(t, appdata.WriteFile(filepath.Join(dir, "missing", "x.json"), []byte("x")), "write x.json")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "the temporary file is removed")
}

func TestReadFileRefusesWhatItShouldNotRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	big := filepath.Join(dir, "big")
	require.NoError(t, os.WriteFile(big, []byte("0123456789"), appdata.PrivateFilePerm))

	_, err := appdata.ReadFile(big, 5)
	require.ErrorIs(t, err, appdata.ErrTooLarge)

	_, err = appdata.ReadFile(dir, 5)
	require.ErrorIs(t, err, appdata.ErrNotAFile)

	_, err = appdata.ReadFile(filepath.Join(dir, "missing"), 5)
	require.ErrorIs(t, err, os.ErrNotExist)
}
