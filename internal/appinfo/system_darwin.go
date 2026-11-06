//go:build darwin

package appinfo

import "syscall"

// detectSystem returns the macOS version and build, e.g. macOS 15.1 (24B83), or "macOS"
// when the kernel does not report them.
func detectSystem() string {
	version, err := syscall.Sysctl("kern.osproductversion")
	if err != nil || version == "" {
		return "macOS"
	}

	if build, buildErr := syscall.Sysctl("kern.osversion"); buildErr == nil && build != "" {
		return "macOS " + version + " (" + build + ")"
	}

	return "macOS " + version
}
