//go:build unix

package integrations

import (
	"os"
	"syscall"
)

// inodeOf returns a file's inode.
func inodeOf(info os.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino) //nolint:unconvert // Ino is narrower on some platforms
	}

	return 0
}
