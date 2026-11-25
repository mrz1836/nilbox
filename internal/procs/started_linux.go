//go:build linux

package procs

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// clockTicks is how many ticks a second /proc/<pid>/stat counts a start time in: USER_HZ,
// which is 100 on every Linux the app is built for.
const clockTicks = 100

// startTimeField is the start time's position in /proc/<pid>/stat, counted from 1, as
// proc(5) numbers the fields.
const startTimeField = 22

// StartedAt returns when the process with pid started, from /proc, or the zero time when
// it isn't running or can't be read. A PID the kernel hands out again belongs to a process
// with a different start time, which is how a caller tells a process it recorded from a
// stranger that reused its PID.
//
// Linux keeps the start as ticks since boot, and the boot time it reports moves whenever
// the wall clock is stepped, so the answer moves with it: compare two readings with some
// slack, and don't compare one with a wall-clock time recorded elsewhere.
func StartedAt(pid int) time.Time {
	return startedAtIn(procRoot, pid)
}

// startedAtIn reads pid's start time from the process table under root: its ticks since
// boot from <root>/<pid>/stat, added to the boot time in <root>/stat.
func startedAtIn(root string, pid int) time.Time {
	if !ValidPID(pid) {
		return time.Time{}
	}

	stat, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "stat")) //nolint:gosec // the kernel's process table
	if err != nil {
		return time.Time{}
	}

	ticks, ok := startTicks(string(stat))
	if !ok {
		return time.Time{}
	}

	boot, ok := bootTime(filepath.Join(root, "stat"))
	if !ok {
		return time.Time{}
	}

	return time.Unix(boot, 0).Add(time.Duration(ticks) * (time.Second / clockTicks))
}

// startTicks reads the start time from a /proc/<pid>/stat line (see statField).
func startTicks(stat string) (int64, bool) {
	ticks, ok := statField(stat, startTimeField)

	return ticks, ok && ticks >= 0
}

// bootTime reads the "btime" line of /proc/stat: when the system booted, in seconds since
// the epoch.
func bootTime(path string) (int64, bool) {
	data, err := os.ReadFile(path) //nolint:gosec // the kernel's process table
	if err != nil {
		return 0, false
	}

	for line := range strings.Lines(string(data)) {
		if value, ok := strings.CutPrefix(line, "btime "); ok {
			boot, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)

			return boot, err == nil && boot > 0
		}
	}

	return 0, false
}
