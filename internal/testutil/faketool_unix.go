//go:build unix

package testutil

import (
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// maxProbedFD is the highest descriptor openFDs probes.
const maxProbedFD = 256

// grandchildAttr starts a fake grandchild in its own session when setsid is true.
func grandchildAttr(setsid bool) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: setsid}
}

// openFDs lists the descriptors open in this process, probing each with fcntl rather
// than listing /dev/fd, which would open a descriptor of its own.
func openFDs() string {
	var open []string

	for fd := range maxProbedFD {
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err == nil {
			open = append(open, strconv.Itoa(fd))
		}
	}

	return strings.Join(open, ",")
}
