package paths_test

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/paths"
)

// file writes text to name in a fresh folder and returns its path.
func file(t *testing.T, name, text string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))

	return path
}

func TestOpenRegular(t *testing.T) {
	t.Parallel()

	path := file(t, "go.mod", "module example.com/acme\n")

	f, err := paths.OpenRegular(path)
	require.NoError(t, err)

	data, err := io.ReadAll(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	assert.Equal(t, "module example.com/acme\n", string(data))

	link := filepath.Join(t.TempDir(), "linked.mod")
	require.NoError(t, os.Symlink(path, link))

	f, err = paths.OpenRegular(link)
	require.NoError(t, err, "a link to a regular file is followed")
	require.NoError(t, f.Close())

	_, err = paths.OpenRegular(t.TempDir())
	require.ErrorIs(t, err, paths.ErrNotRegular, "a folder")

	_, err = paths.OpenRegular(filepath.Join(t.TempDir(), "missing"))
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestReadHead(t *testing.T) {
	t.Parallel()

	path := file(t, "t3.json", "0123456789")

	tests := []struct {
		limit int64
		want  string
	}{
		{limit: 4, want: "0123"},
		{limit: 10, want: "0123456789"},
		{limit: 64, want: "0123456789"},
		{limit: 0, want: ""},
	}

	for _, tt := range tests {
		got, err := paths.ReadHead(path, tt.limit)
		require.NoError(t, err)
		assert.Equal(t, tt.want, string(got), "limit %d", tt.limit)
	}

	_, err := paths.ReadHead(t.TempDir(), 4)
	require.ErrorIs(t, err, paths.ErrNotRegular, "a folder")

	_, err = paths.ReadHead(filepath.Join(t.TempDir(), "missing"), 4)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestReadTail(t *testing.T) {
	t.Parallel()

	path := file(t, "s.jsonl", "0123456789")

	tests := []struct {
		limit int64
		want  string
		cut   bool
	}{
		{limit: 4, want: "6789", cut: true},
		{limit: 10, want: "0123456789"},
		{limit: 64, want: "0123456789"},
		{limit: 0, want: "", cut: true},
		{limit: -1, want: "", cut: true},
	}

	for _, tt := range tests {
		got, cut, err := paths.ReadTail(path, tt.limit)
		require.NoError(t, err)
		assert.Equal(t, tt.want, string(got), "limit %d", tt.limit)
		assert.Equal(t, tt.cut, cut, "limit %d", tt.limit)
	}

	empty, cut, err := paths.ReadTail(file(t, "empty.jsonl", ""), 4)
	require.NoError(t, err)
	assert.Empty(t, empty)
	assert.False(t, cut, "an empty file is whole")

	long := file(t, "long.jsonl", strings.Repeat("x", 1<<20)+"end")
	got, cut, err := paths.ReadTail(long, 3)
	require.NoError(t, err)
	assert.Equal(t, "end", string(got))
	assert.True(t, cut)

	_, _, err = paths.ReadTail(t.TempDir(), 4)
	require.ErrorIs(t, err, paths.ErrNotRegular, "a folder")

	_, _, err = paths.ReadTail(filepath.Join(t.TempDir(), "missing"), 4)
	require.ErrorIs(t, err, fs.ErrNotExist)
}
