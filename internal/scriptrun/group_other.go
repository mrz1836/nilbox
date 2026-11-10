//go:build !unix

package scriptrun

import (
	"fmt"
	"syscall"

	"github.com/mrz1836/nilbox/internal/procs"
)

// signalGroup can't reach a process group here, where there are none.
func signalGroup(pgid int, _ syscall.Signal) error {
	return fmt.Errorf("signal process group %d: %w", pgid, errNoGroup)
}

// groupAlive reports no group alive here, where there are none.
func groupAlive(_ int) bool {
	return false
}

// runGroup reports whether pgid can be a run's process: a valid PID, not the init
// process.
func runGroup(pgid int) bool {
	return pgid > 1 && procs.ValidPID(pgid)
}
