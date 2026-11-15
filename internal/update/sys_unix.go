//go:build unix

package update

import "golang.org/x/sys/unix"

// canWrite reports whether the user may create files in dir.
func canWrite(dir string) bool {
	return unix.Access(dir, unix.W_OK) == nil
}
