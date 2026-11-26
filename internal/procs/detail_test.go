package procs_test

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/procs"
)

// procArgs builds a kern.procargs2 buffer, as macOS answers it: the argument count, the
// executable's path (/bin/sh), NUL padding, the arguments, and the environment after them.
func procArgs(argc uint32, args, env []string) []byte {
	buf := binary.NativeEndian.AppendUint32(nil, argc)
	buf = append(buf, "/bin/sh"...)
	buf = append(buf, 0, 0, 0, 0)

	for _, s := range append(args, env...) {
		buf = append(buf, s...)
		buf = append(buf, 0)
	}

	return buf
}

func TestParseProcArgs(t *testing.T) {
	t.Parallel()

	loop := []string{"/bin/sh", "-c", "while true; do sleep 0.05; done"}
	env := []string{"TOKEN=example-secret", "HOME=/home/user"}

	assert.Equal(t, loop, procs.ParseProcArgs(procArgs(3, loop, env)),
		"the arguments, never the environment after them")
	assert.Equal(t, []string{"/bin/sh", "-c"}, procs.ParseProcArgs(procArgs(2, loop, env)),
		"no more than the count")

	cut := procArgs(3, loop, nil)
	assert.Equal(t, []string{"/bin/sh", "-c", "while true"}, procs.ParseProcArgs(cut[:len(cut)-len("; do sleep 0.05; done")-1]),
		"a buffer cut short keeps what it holds")

	assert.Nil(t, procs.ParseProcArgs(nil))
	assert.Nil(t, procs.ParseProcArgs([]byte{1, 0}), "too short for the count")
	assert.Nil(t, procs.ParseProcArgs(procArgs(0, nil, env)), "no arguments")
	assert.Nil(t, procs.ParseProcArgs(binary.NativeEndian.AppendUint32(nil, 2)), "no path")
}

func TestParseProcEnv(t *testing.T) {
	t.Parallel()

	const (
		name = "WARP_FOCUS_URL"
		link = "warp://session/0123456789abcdef0123456789abcdef"
	)

	loop := []string{"/bin/sh", "-c", "while true; do sleep 0.05; done"}
	cut := procArgs(3, loop, []string{"HOME=/home/user", name + "=" + link})

	tests := []struct {
		name      string
		buf       []byte
		lookFor   string
		want      string
		wantFound bool
	}{
		{
			name: "among the others", buf: procArgs(3, loop, []string{"HOME=/home/user", name + "=" + link, "TOKEN=x"}),
			lookFor: name, want: link, wantFound: true,
		},
		{
			name: "a name it is the start of comes first", buf: procArgs(3, loop, []string{name + "_X=other", name + "=" + link}),
			lookFor: name, want: link, wantFound: true,
		},
		{
			name: "only a name it is the start of", buf: procArgs(3, loop, []string{name + "_X=" + link}),
			lookFor: name,
		},
		{
			name: "only a name it is the end of", buf: procArgs(3, loop, []string{"OLD_" + name + "=" + link}),
			lookFor: name,
		},
		{name: "an empty value", buf: procArgs(3, loop, []string{name + "="}), lookFor: name, wantFound: true},
		{name: "a value holding =", buf: procArgs(1, loop[:1], []string{"A=b=c"}), lookFor: "A", want: "b=c", wantFound: true},
		{name: "the first one set wins", buf: procArgs(3, loop, []string{name + "=" + link, name + "=later"}), lookFor: name, want: link, wantFound: true},
		{name: "a missing name", buf: procArgs(3, loop, []string{"HOME=/home/user"}), lookFor: name},
		{name: "no environment, as for a platform binary", buf: procArgs(3, loop, nil), lookFor: name},
		{
			name: "only among the arguments", buf: procArgs(3, []string{"/bin/sh", "-c", name + "=" + link}, []string{"HOME=/home/user"}),
			lookFor: name,
		},
		{
			name: "past the empty string that ends it", buf: procArgs(3, loop, []string{"HOME=/home/user", "", name + "=" + link}),
			lookFor: name,
		},
		{name: "no arguments", buf: procArgs(0, nil, []string{name + "=" + link}), lookFor: name, want: link, wantFound: true},
		{
			// node with process.title = "claude": the title over the arguments, then NULs
			// where the rest of them were, and argc unchanged.
			name:    "arguments the process renamed itself over",
			buf:     procArgs(2, []string{"claude", "", "", "", "", "", "", "", ""}, []string{"HOME=/home/user", name + "=" + link}),
			lookFor: name, want: link, wantFound: true,
		},
		{name: "argc past the end", buf: procArgs(9, loop, []string{"HOME=/home/user", name + "=" + link}), lookFor: name},
		{name: "the largest argc", buf: procArgs(1<<32-1, loop, []string{name + "=" + link}), lookFor: name},
		{name: "a value cut short", buf: cut[:len(cut)-1], lookFor: name},
		{name: "no buffer", lookFor: name},
		{name: "too short for the count", buf: []byte{1, 0}, lookFor: name},
		{name: "no path", buf: append(binary.NativeEndian.AppendUint32(nil, 0), name+"="+link...), lookFor: name},
		{name: "an empty name", buf: procArgs(3, loop, []string{"=" + link}), lookFor: ""},
		{name: "a name holding =", buf: procArgs(3, loop, []string{name + "=" + link}), lookFor: name + "=warp:"},
		{name: "a name holding a NUL", buf: procArgs(3, loop, []string{name + "=" + link}), lookFor: name + "\x00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, found := procs.ParseProcEnv(tt.buf, tt.lookFor)
			assert.Equal(t, tt.wantFound, found)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEnvOfRefusesAnInvalidPID(t *testing.T) {
	t.Parallel()

	for _, pid := range []int{0, -1, procs.MaxPID + 1} {
		_, ok := procs.EnvOf(pid, "PATH")
		assert.False(t, ok, "pid %d", pid)
	}
}

func TestAdopted(t *testing.T) {
	t.Parallel()

	list := []procs.Process{
		// A test's child its test binary left behind, which launchd adopted.
		{PID: 75083, PPID: 1, Command: "bash"},
		// A session in a terminal.
		{PID: 4000, PPID: 1, Command: "iTerm2"},
		{PID: 4100, PPID: 4000, Command: "zsh"},
		// A user's systemd adopts on Linux.
		{PID: 900, PPID: 1, Command: "/usr/lib/systemd/systemd"},
		{PID: 901, PPID: 900, Command: "node"},
		// A parent the list doesn't hold, and one that is its own.
		{PID: 50, PPID: 49, Command: "make"},
		{PID: 12, PPID: 12, Command: "systemd"},
	}
	byPID := procs.ByPID(list)

	adopted := make([]int, 0, len(list))

	for _, p := range list {
		if procs.Adopted(p, byPID) {
			adopted = append(adopted, p.PID)
		}
	}

	assert.Equal(t, []int{75083, 4000, 900, 901}, adopted)
	assert.False(t, procs.Adopted(procs.Process{PID: 7}, byPID), "an unknown parent is no manager")
}

func TestDetailOfRefusesAnInvalidPID(t *testing.T) {
	t.Parallel()

	for _, pid := range []int{0, -1, procs.MaxPID + 1} {
		_, ok := procs.DetailOf(pid)
		assert.False(t, ok, "pid %d", pid)
	}
}
