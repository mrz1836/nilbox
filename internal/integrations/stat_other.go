//go:build !unix

package integrations

import "os"

// inodeOf returns 0: this platform has no inode to read.
func inodeOf(os.FileInfo) uint64 {
	return 0
}
