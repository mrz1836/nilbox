package procs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestParseLsof(t *testing.T) {
	t.Parallel()

	out := "p101\nR1\ncclaude\nfcwd\nn/srv/one\np102\nRx\nccodex\nfcwd\nn/srv/two\nn/srv/ignored\n" +
		"p103\nczsh\nfcwd\np104\nfcwd\nnrelative\nnot-a-field\npnot-a-pid\nn/srv/orphan\n"

	assert.Equal(t, []procs.Process{
		{PID: 101, PPID: 1, Command: "claude", Dir: "/srv/one"},
		{PID: 102, Command: "codex", Dir: "/srv/two"},
	}, procs.ParseLsof(out), "a process with no directory, or a relative one, is left out")

	assert.Empty(t, procs.ParseLsof(""))
	assert.Equal(t, map[int]string{101: "/srv/one"}, procs.CwdsOf(procs.ParseLsof("p101\nn/srv/one\n")))
}

func TestWithin(t *testing.T) {
	t.Parallel()

	root := testutil.RealTempDir(t)
	worktree := filepath.Join(root, "worktrees", "pr-7")
	require.NoError(t, os.MkdirAll(filepath.Join(worktree, "src"), 0o700))

	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(worktree, link))

	list := []procs.Process{
		{PID: 1, Command: "claude", Dir: worktree},
		{PID: 2, Command: "go", Dir: filepath.Join(worktree, "src")},
		{PID: 3, Command: "zsh", Dir: filepath.Join(link, "src")},
		{PID: 4, Command: "codex", Dir: worktree + "-other"},
		{PID: 5, Command: "zsh", Dir: filepath.Dir(worktree)},
		{PID: 6, Command: "none", Dir: ""},
	}

	got := procs.Within(list, worktree)
	assert.Equal(t, []int{1, 2, 3}, pids(got), "inside the worktree, through a link too, but not a sibling or its parent")
	assert.Equal(t, []int{1, 2, 3}, pids(procs.Within(list, link)))
	assert.Empty(t, procs.Within(list, filepath.Join(root, "missing")))
}

func TestOthers(t *testing.T) {
	t.Parallel()

	list := []procs.Process{{PID: 10, PPID: 1}, {PID: 11, PPID: 10}, {PID: 12, PPID: 11}, {PID: 13, PPID: 1}}
	assert.Equal(t, []int{12, 13}, pids(procs.Others(list, 10)), "itself and its own children are left out")
}

// pids returns the processes' IDs, in order.
func pids(list []procs.Process) []int {
	out := make([]int, 0, len(list))
	for _, p := range list {
		out = append(out, p.PID)
	}

	return out
}
