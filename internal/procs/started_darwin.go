//go:build darwin

package procs

import (
	"time"

	"golang.org/x/sys/unix"
)

// StartedAt returns when the process with pid started, from the kernel's process table,
// or the zero time when it isn't running or can't be read. A PID the kernel hands out
// again belongs to a process with a different start time, which is how a caller tells a
// process it recorded from a stranger that reused its PID.
func StartedAt(pid int) time.Time {
	if !ValidPID(pid) {
		return time.Time{}
	}

	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || info == nil || info.Proc.P_pid != int32(pid) { //nolint:gosec // ValidPID bounds pid by MaxInt32
		return time.Time{}
	}

	start := info.Proc.P_starttime
	if start.Sec <= 0 {
		return time.Time{}
	}

	return time.Unix(start.Sec, int64(start.Usec)*int64(time.Microsecond))
}
