//go:build unix

package scriptrun

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/mrz1836/nilbox/internal/procs"
)

// signalGroup sends sig to every process in the group pgid: a run from before the app
// restarted, which the app holds no handle on. It refuses a group that can't be a run's.
func signalGroup(pgid int, sig syscall.Signal) error {
	if !runGroup(pgid) {
		return fmt.Errorf("signal process group %d: %w", pgid, errNoGroup)
	}

	if err := syscall.Kill(-pgid, sig); err != nil {
		return fmt.Errorf("signal process group %d: %w", pgid, err)
	}

	return nil
}

// groupAlive reports whether any process is left in the group pgid. It sends no signal.
func groupAlive(pgid int) bool {
	if !runGroup(pgid) {
		return false
	}

	err := syscall.Kill(-pgid, 0)

	return err == nil || errors.Is(err, syscall.EPERM)
}

// runGroup reports whether pgid can be a run's process group: a valid PID, not the init
// process, whose negative would reach every process, and not the app's own group.
func runGroup(pgid int) bool {
	return pgid > 1 && procs.ValidPID(pgid) && pgid != syscall.Getpgrp()
}
