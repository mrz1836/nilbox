//go:build unix

package autopilot

import (
	"errors"
	"os"
	"syscall"
)

// flock takes an exclusive lock on f without waiting, errLocked when it is held.
func flock(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return errLocked
	}

	return err
}

// funlock lets go of f's lock.
func funlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
