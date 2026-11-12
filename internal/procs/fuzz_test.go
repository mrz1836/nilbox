package procs_test

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/procs"
)

// FuzzParseLsof checks that any `lsof -F pcRn` output reads as processes that each have
// a positive PID and an absolute directory, no more than it starts, and that processes
// written the way lsof writes them, with any command, read back as those processes, each
// with its first directory.
func FuzzParseLsof(f *testing.F) {
	for _, seed := range []struct {
		out, command, dir string
		pid, ppid         uint32
	}{
		{"", "codex", "/srv/projects/example-repo", 4100, 1},
		{"p4100\nR1\nccodex\nfcwd\nn/srv/wt/pr-7\nn/second\np4200\nccat\n", "claude", "/", 1, 0},
		{"p0\nn/zero\np-3\nn/negative\npx\nn/word\nn/before-any\n", "", "/a b/é", 99999, 99999},
		{"p12\nnrelative\nn/abs\nR\nRnot\nc\n", "with\ttab", "/x", 2, 4294967295},
		{"\n\np\nR1\n", "a", "relative", 7, 7},
	} {
		f.Add(seed.out, seed.command, seed.dir, seed.pid, seed.ppid)
	}

	f.Fuzz(func(t *testing.T, out, command, dir string, pid, ppid uint32) {
		list := procs.ParseLsof(out)
		assert.LessOrEqual(t, len(list), strings.Count("\n"+out, "\np"))

		for _, p := range list {
			assert.Positive(t, p.PID)
			assert.True(t, filepath.IsAbs(p.Dir), "%q", p.Dir)
			assert.NotContains(t, p.Dir+p.Command, "\n")
		}

		if pid == 0 || !filepath.IsAbs(dir) || strings.Contains(dir+command, "\n") {
			return
		}

		want := procs.Process{PID: int(pid), PPID: int(ppid), Command: command, Dir: dir}
		one := "p" + strconv.Itoa(want.PID) + "\nR" + strconv.Itoa(want.PPID) + "\nc" + command + "\nfcwd\nn" + dir + "\nn/later\n"

		assert.Equal(t, []procs.Process{want, want}, procs.ParseLsof(one+one))
	})
}
