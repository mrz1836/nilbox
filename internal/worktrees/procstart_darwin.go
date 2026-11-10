//go:build darwin

package worktrees

import (
	"time"

	"github.com/mrz1836/nilbox/internal/procs"
)

// processStart returns when pid started, from the kernel's process table, or zero when
// it cannot be read. macOS records a process's start as wall-clock time, so it compares
// with the start the agent wrote.
func processStart(pid int) time.Time {
	return procs.StartedAt(pid)
}
