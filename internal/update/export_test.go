package update

import (
	"context"
	"time"
)

// Tick runs one of the Worker's looks at once.
func (e *Engine) Tick(ctx context.Context) error {
	return e.tick(ctx)
}

// Settle reads the last restart's outcome, cleans up, and loads the cache, as the Worker
// does first.
func (e *Engine) Settle(ctx context.Context) {
	e.settle(ctx)
}

// CleanUp removes what updates leave behind, as the start does.
func CleanUp(bundle, tempDir string, pid int, now time.Time) []error {
	return cleanUp(bundle, tempDir, pid, now)
}

// HelperReason words why an install failed from the helper's log.
func HelperReason(log []byte) string {
	return helperReason(log)
}

// Locate works out where the bundle is, with writable standing in for the system's answer.
func Locate(bundle, home string, writable func(string) bool) Location {
	return locate(bundle, home, writable)
}
