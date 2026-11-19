//go:build unix

package execpath

import "syscall"

// KillGroup is what Start's canceled context kills a group with, for the test of a group
// that is gone.
func KillGroup(pgid int) error {
	return killGroup(pgid)
}

// SignalGroup is what Proc.Signal signals a group with, for the test of the IDs it
// refuses.
func SignalGroup(pgid int, sig syscall.Signal) error {
	return signalGroup(pgid, sig)
}
