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

func TestLocatedAnswersAsWithinDoes(t *testing.T) {
	t.Parallel()

	root := testutil.RealTempDir(t)
	worktrees := filepath.Join(root, "worktrees")
	first := filepath.Join(worktrees, "pr-7")
	second := filepath.Join(worktrees, "pr-8")
	require.NoError(t, os.MkdirAll(filepath.Join(first, "src"), 0o700))
	require.NoError(t, os.MkdirAll(second, 0o700))

	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(second, link))

	list := []procs.Process{
		{PID: 1, Command: "claude", Dir: first},
		{PID: 2, Command: "go", Dir: filepath.Join(first, "src")},
		{PID: 3, Command: "fish", Dir: filepath.Join(link, "missing")},
		{PID: 4, Command: "codex", Dir: first + "-other"},
		{PID: 5, Command: "bash", Dir: worktrees},
		{PID: 6, Command: "none", Dir: ""},
		{PID: 7, Command: "vim", Dir: second},
	}

	located := procs.Locate(list)
	folders := map[string][]int{
		first:                               {1, 2},
		first + string(filepath.Separator):  {1, 2},
		second:                              {3, 7},
		link:                                {3, 7},
		worktrees:                           {1, 2, 3, 4, 5, 7},
		filepath.Join(first, "src"):         {2},
		filepath.Join(root, "missing"):      nil,
		filepath.Join(link, "missing"):      {3},
		filepath.Join(link, "missing", "x"): nil,
	}

	for dir, want := range folders {
		got := located.Within(dir)
		assert.Equal(t, procs.Within(list, dir), got, "asked of %s", dir)

		if want == nil {
			assert.Nil(t, got, "none in %s", dir)
		} else {
			assert.Equal(t, want, pids(got), "in %s, by PID", dir)
		}
	}

	assert.Nil(t, procs.Locate(nil).Within(root))
	assert.Nil(t, procs.Within(nil, root))
}

func TestLocatedResolvesEachDirectoryOnce(t *testing.T) {
	t.Parallel()

	root := testutil.RealTempDir(t)
	first := filepath.Join(root, "pr-7")
	second := filepath.Join(root, "pr-8")

	require.NoError(t, os.MkdirAll(first, 0o700))
	require.NoError(t, os.MkdirAll(second, 0o700))

	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(first, link))

	list := []procs.Process{{PID: 1, Command: "fish", Dir: link}}
	located := procs.Locate(list)
	require.Equal(t, []int{1}, pids(located.Within(first)))

	require.NoError(t, os.Remove(link))
	require.NoError(t, os.Symlink(second, link))

	assert.Empty(t, located.Within(second), "the directory stays as it resolved the first time")
	assert.Equal(t, []int{1}, pids(located.Within(first)))
	assert.Equal(t, []int{1}, pids(procs.Within(list, second)), "a fresh look resolves it again")
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

func TestWithoutServices(t *testing.T) {
	t.Parallel()

	commands := []string{"claude", "codex"}
	list := []procs.Process{
		// Codex's app-server daemon, which launchd runs, and its worker.
		{PID: 15337, PPID: 1, Command: "codex", Dir: "/srv/wt"},
		{PID: 22072, PPID: 15337, Command: "codex", Dir: "/srv/wt"},
		{PID: 22100, PPID: 22072, Command: "zsh", Dir: "/srv/wt"},
		// A session in a terminal.
		{PID: 4000, PPID: 1, Command: "iTerm2", Dir: "/"},
		{PID: 4100, PPID: 4000, Command: "zsh", Dir: "/srv/wt"},
		{PID: 4242, PPID: 4100, Command: "/usr/local/bin/codex", Dir: "/srv/wt"},
		// A session an editor runs, with no terminal.
		{PID: 83937, PPID: 1, Command: "Code Helper", Dir: "/"},
		{PID: 87508, PPID: 83937, Command: "claude", Dir: "/srv/wt"},
		// A service a user's systemd runs, and a daemon that is no agent's.
		{PID: 900, PPID: 1, Command: "systemd", Dir: "/"},
		{PID: 901, PPID: 900, Command: "claude", Dir: "/srv/wt"},
		{PID: 950, PPID: 1, Command: "gradle", Dir: "/srv/wt"},
	}

	got := procs.WithoutServices(list, commands)
	kept := make([]int, 0, len(got))

	for _, p := range got {
		kept = append(kept, p.PID)
	}

	assert.Equal(t, []int{4000, 4100, 4242, 83937, 87508, 900, 950}, kept)
	assert.Equal(t, list, procs.WithoutServices(list, nil), "with no commands, nothing is a service")
}

func TestWithoutServicesStopsAtALoop(t *testing.T) {
	t.Parallel()

	// A reused PID can make a process its own grandparent.
	list := []procs.Process{
		{PID: 10, PPID: 11, Command: "zsh", Dir: "/srv"},
		{PID: 11, PPID: 10, Command: "zsh", Dir: "/srv"},
		{PID: 12, PPID: 12, Command: "zsh", Dir: "/srv"},
	}

	assert.Len(t, procs.WithoutServices(list, []string{"codex"}), 3)
}

func TestInMatchesTheFolderItself(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inner := filepath.Join(root, "inner")
	require.NoError(t, os.MkdirAll(inner, 0o700))

	list := []procs.Process{{PID: 1, Dir: root}, {PID: 2, Dir: inner}, {PID: 3, Dir: ""}, {PID: 4, Dir: root + "/"}}

	got := procs.In(list, root)
	require.Len(t, got, 2)
	assert.Equal(t, 1, got[0].PID)
	assert.Equal(t, 4, got[1].PID)
}

func TestAlive(t *testing.T) {
	t.Parallel()

	assert.True(t, procs.Alive(os.Getpid()), "this test runs")
	assert.False(t, procs.Alive(0))
	assert.False(t, procs.Alive(-1))
	assert.False(t, procs.Alive(1<<32-1), "a PID the kernel would cut to -1 reaches no process")
	assert.False(t, procs.Alive(procs.MaxPID+1))
}

func TestValidPID(t *testing.T) {
	t.Parallel()

	assert.True(t, procs.ValidPID(1))
	assert.True(t, procs.ValidPID(procs.MaxPID))
	assert.False(t, procs.ValidPID(0))
	assert.False(t, procs.ValidPID(-5))
	assert.False(t, procs.ValidPID(procs.MaxPID+1))
}
