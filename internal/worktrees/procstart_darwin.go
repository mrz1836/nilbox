//go:build darwin

package worktrees

import (
	"time"

	"golang.org/x/sys/unix"
)

// processStart returns when pid started, from the kernel's process table, or zero when
// it cannot be read.
func processStart(pid int) time.Time {
	if pid <= 0 {
		return time.Time{}
	}

	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || info == nil || info.Proc.P_pid != int32(pid) { //nolint:gosec // pid fits: the lock pattern allows at most 10 digits and the kernel's PIDs are 32-bit
		return time.Time{}
	}

	start := info.Proc.P_starttime
	if start.Sec <= 0 {
		return time.Time{}
	}

	return time.Unix(start.Sec, int64(start.Usec)*int64(time.Microsecond))
}
