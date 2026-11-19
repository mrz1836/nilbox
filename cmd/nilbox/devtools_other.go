//go:build desktop && devtools && !darwin

package main

import (
	"fmt"
	"runtime"
	"unsafe"
)

// snapshotWebView takes no picture outside macOS, where the inspector has no way to ask
// the web view for one.
func snapshotWebView(unsafe.Pointer) ([]byte, error) {
	return nil, fmt.Errorf("%w on %s", errNoSnapshot, runtime.GOOS)
}
