//go:build unix

package integrations

import (
	"errors"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// writable reports whether the user may create files in dir.
func writable(dir string) bool {
	return unix.Access(dir, unix.W_OK) == nil
}

// killed reports a program that ended on SIGKILL, as macOS ends one it refuses to run.
func killed(err error) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ProcessState == nil {
		return false
	}

	status, ok := exit.Sys().(syscall.WaitStatus)

	return ok && status.Signaled() && status.Signal() == syscall.SIGKILL
}
