//go:build !unix

package testutil

import "syscall"

// grandchildAttr can't start a session here, and starts the grandchild as it is.
func grandchildAttr(_ bool) *syscall.SysProcAttr {
	return nil
}

// openFDs can't list descriptors here.
func openFDs() string {
	return "unknown"
}
