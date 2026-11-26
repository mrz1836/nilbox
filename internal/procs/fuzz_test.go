package procs_test

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/procs"
)

// FuzzParseProcEnv checks that any kern.procargs2 buffer reads without a panic, that a
// value it finds is one NUL-terminated NAME=value string of the buffer's, and that a
// variable written into a buffer laid out as macOS lays one out reads back as its value,
// never as the same NAME=value written among the arguments.
func FuzzParseProcEnv(f *testing.F) {
	link := "warp://session/0123456789abcdef0123456789abcdef"

	for _, seed := range []struct {
		buf         []byte
		name, value string
	}{
		{procArgs(2, []string{"/bin/sh", "-c"}, []string{"WARP_FOCUS_URL=" + link}), "WARP_FOCUS_URL", link},
		{procArgs(1, []string{"node"}, []string{"WARP_FOCUS_URL_X=x", "", "WARP_FOCUS_URL=y"}), "WARP_FOCUS_URL", ""},
		{procArgs(9, []string{"a"}, []string{"A=1"}), "A", "b=c"},
		{procArgs(0, nil, nil), "", "x"},
		{[]byte{1, 0, 0, 0, '/', 0, 0, 'A', '=', 0}, "A=", "\x00"},
		{nil, "\x00", "é"},
	} {
		f.Add(seed.buf, seed.name, seed.value)
	}

	f.Fuzz(func(t *testing.T, buf []byte, name, value string) {
		got, found := procs.ParseProcEnv(buf, name)
		if found {
			assert.NotContains(t, got, "\x00")
			assert.True(t, bytes.Contains(buf, []byte(name+"="+got+"\x00")), "%q is not one of the buffer's strings", got)
		} else {
			assert.Empty(t, got)
		}

		if name == "" || strings.ContainsAny(name, "=\x00") || strings.Contains(value, "\x00") {
			return
		}

		written := procArgs(2, []string{"/bin/sh", name + "=decoy"}, []string{name + "=" + value, "OTHER=1", ""})
		got, found = procs.ParseProcEnv(written, name)
		assert.True(t, found)
		assert.Equal(t, value, got)
	})
}

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
