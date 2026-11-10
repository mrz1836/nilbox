//go:build !unix

package execpath

import (
	"context"
	"syscall"
)

// Start returns ErrUnsupported: this OS has no process groups to run a program in.
func Start(_ context.Context, _ Cmd) (*Proc, error) {
	return nil, ErrUnsupported
}

// signalGroup returns ErrUnsupported.
func signalGroup(_ int, _ syscall.Signal) error {
	return ErrUnsupported
}
