//go:build linux

package procs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeProc writes a fake process pid under root, with its command line's arguments and
// its controlling terminal (0 for none).
func writeProc(t *testing.T, root, pid string, args []string, tty string) {
	t.Helper()

	dir := filepath.Join(root, pid)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmdline"), []byte(strings.Join(args, "\x00")+"\x00"), 0o600))

	// The command name holds a space and a parenthesis, which the fields are counted past.
	stat := pid + " (odd) name) S 1 " + pid + " " + pid + " " + tty + " -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 1234 0 0"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o600))
}

func TestDetailInReadsTheProcessTable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeProc(t, root, "75083", []string{"/bin/sh", "-c", "while true\ndo sleep 0.05; done"}, "0")
	writeProc(t, root, "4100", []string{"-zsh"}, "34816")
	writeProc(t, root, "51", []string{"node", strings.Repeat("x", 1200)}, "0")

	got, ok := detailIn(root, 75083)
	require.True(t, ok)
	assert.Equal(t, Detail{Args: "/bin/sh -c while true do sleep 0.05; done"}, got,
		"its arguments on one line, and no terminal")

	got, ok = detailIn(root, 4100)
	require.True(t, ok)
	assert.Equal(t, Detail{Args: "-zsh", Terminal: true}, got)

	got, ok = detailIn(root, 51)
	require.True(t, ok)
	assert.Len(t, []rune(got.Args), maxArgs, "a long command line is cut short")
	assert.True(t, strings.HasSuffix(got.Args, "…"))
}

func TestDetailInWithoutACommandLine(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeProc(t, root, "2", nil, "0")
	require.NoError(t, os.Remove(filepath.Join(root, "2", "cmdline")))

	got, ok := detailIn(root, 2)
	require.True(t, ok, "a kernel thread has a terminal to read, and no command line")
	assert.Equal(t, Detail{}, got)

	_, ok = detailIn(root, 3)
	assert.False(t, ok, "a process that is gone")

	require.NoError(t, os.MkdirAll(filepath.Join(root, "4"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "4", "stat"), []byte("4 (cut"), 0o600))
	_, ok = detailIn(root, 4)
	assert.False(t, ok, "a stat line it can't read")
}

func TestStatField(t *testing.T) {
	t.Parallel()

	stat := "7 (a) b) S 1 7 7 34816 -1"

	for position, want := range map[int]int64{4: 1, 5: 7, 7: 34816, 8: -1} {
		got, ok := statField(stat, position)
		assert.True(t, ok, "field %d", position)
		assert.Equal(t, want, got, "field %d", position)
	}

	for _, position := range []int{1, 2, 3, 9} {
		_, ok := statField(stat, position)
		assert.False(t, ok, "field %d is no number after the name", position)
	}
}
