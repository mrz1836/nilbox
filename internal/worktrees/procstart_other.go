//go:build !darwin

package worktrees

import "time"

// processStart cannot read a process's start time here, so a reused PID is not detected
// and a live process always counts as the session.
func processStart(_ int) time.Time {
	return time.Time{}
}
