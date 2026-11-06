//go:build !darwin

package appinfo

import "runtime"

// detectSystem returns the operating system's name; only macOS reports its version.
func detectSystem() string {
	return runtime.GOOS
}
