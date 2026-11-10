//go:build !darwin

package worktrees

import "time"

// processStart doesn't read a process's start time here, so a reused PID is not detected
// and a live process always counts as the session. Linux keeps a start only as time since
// boot, which turns into wall-clock time through a boot time the kernel moves whenever the
// clock is stepped, so a live session could otherwise be judged ended.
func processStart(_ int) time.Time {
	return time.Time{}
}
