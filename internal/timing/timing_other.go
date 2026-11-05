//go:build !darwin

package timing

import (
	"errors"
	"time"
)

// errNoOSStart reports that this OS's process start time is not read yet, so the runtime
// fallback is used. A port can read it here (for example from /proc on Linux).
var errNoOSStart = errors.New("the process start time is not available on this OS")

// osProcessStart is not implemented on this OS.
func osProcessStart(_ int) (time.Time, error) {
	return time.Time{}, errNoOSStart
}
