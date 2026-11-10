//go:build !darwin && !linux

package procs

import "time"

// StartedAt can't read a process's start time here, and returns the zero time, so a
// reused PID isn't detected.
func StartedAt(_ int) time.Time {
	return time.Time{}
}
