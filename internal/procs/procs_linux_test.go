//go:build linux

package procs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadProc(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	for pid, dir := range map[string]string{"12": "/srv/a", "7": "/srv/b"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, pid), 0o700))
		require.NoError(t, os.Symlink(dir, filepath.Join(root, pid, "cwd")))
		require.NoError(t, os.WriteFile(filepath.Join(root, pid, "comm"), []byte("codex\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(root, pid, "status"), []byte("Name:\tcodex\nPPid:\t1\n"), 0o600))
	}

	require.NoError(t, os.MkdirAll(filepath.Join(root, "self"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "99"), 0o700))

	got, err := readProc(t.Context(), root)
	require.NoError(t, err)
	assert.ElementsMatch(t, []Process{{PID: 12, PPID: 1, Command: "codex", Dir: "/srv/a"}, {PID: 7, PPID: 1, Command: "codex", Dir: "/srv/b"}}, got,
		"a process whose directory cannot be read is left out")

	_, err = readProc(t.Context(), filepath.Join(root, "missing"))
	require.Error(t, err)
}

func TestSystemListsThisProcess(t *testing.T) {
	t.Parallel()

	got, err := System{}.Processes(t.Context())
	require.NoError(t, err)

	self, err := os.Getwd()
	require.NoError(t, err)

	found := false

	for _, p := range got {
		if p.PID == os.Getpid() {
			found = true

			assert.Equal(t, self, p.Dir)
		}
	}

	assert.True(t, found)
}
