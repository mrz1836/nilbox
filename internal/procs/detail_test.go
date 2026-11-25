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
