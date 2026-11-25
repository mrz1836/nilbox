//go:build linux

package procs

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ttyField is the controlling terminal's position in /proc/<pid>/stat, counted from 1, as
// proc(5) numbers the fields: 0 for none.
const ttyField = 7

// DetailOf reads the process with pid's command line and whether it has a terminal, from
// /proc, and reports whether it could.
func DetailOf(pid int) (Detail, bool) {
	return detailIn(procRoot, pid)
}

// detailIn reads pid's detail from the process table under root: its arguments from
// <root>/<pid>/cmdline, and its terminal from <root>/<pid>/stat.
func detailIn(root string, pid int) (Detail, bool) {
	if !ValidPID(pid) {
		return Detail{}, false
	}

	dir := filepath.Join(root, strconv.Itoa(pid))

	stat, err := os.ReadFile(filepath.Join(dir, "stat")) //nolint:gosec // the kernel's process table
	if err != nil {
		return Detail{}, false
	}

	tty, ok := statField(string(stat), ttyField)
	if !ok {
		return Detail{}, false
	}

	d := Detail{Terminal: tty != 0}

	// A kernel thread, or a process of another user's that hides it, has no command line.
	if cmdline, readErr := os.ReadFile(filepath.Join(dir, "cmdline")); readErr == nil { //nolint:gosec // the kernel's process table
		d.Args = argsLine(strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00"))
	}

	return d, true
}

// statField reads the numeric field at position from a /proc/<pid>/stat line, counted from
// 1 as proc(5) numbers them, from the third on. The command name, the second field, is in
// parentheses and may hold spaces and parentheses itself, so the fields are counted from
// the last closing parenthesis.
func statField(stat string, position int) (int64, bool) {
	_, rest, ok := strings.CutLast(stat, ")")
	if !ok {
		return 0, false
	}

	// The fields after the name start at the third.
	const first = 3

	fields := strings.Fields(rest)
	if position < first || len(fields) < position-first+1 {
		return 0, false
	}

	n, err := strconv.ParseInt(fields[position-first], 10, 64)

	return n, err == nil
}
