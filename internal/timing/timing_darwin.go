//go:build darwin

package timing

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

// osProcessStart reads the process start time the kernel recorded for pid.
func osProcessStart(pid int) (time.Time, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return time.Time{}, fmt.Errorf("read the process start time: %w", err)
	}

	tv := info.Proc.P_starttime

	return time.Unix(tv.Sec, int64(tv.Usec)*int64(time.Microsecond)), nil
}
