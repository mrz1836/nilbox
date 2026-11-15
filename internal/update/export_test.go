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
// does first, and returns how long to wait before settling again.
func (e *Engine) Settle(ctx context.Context) time.Duration {
	return e.settle(ctx)
}

// CleanUp removes what updates leave behind, as the start does.
func CleanUp(bundle, tempDir string, pid int, now time.Time) []error {
	return cleanUp(bundle, tempDir, pid, now)
}

// HelperReason words why an install failed from the helper's log.
func HelperReason(log []byte) string {
	reason, _ := helperOutcome(log)

	return reason
}

// HelperRetryable reports whether trying the install the helper's log tells of again may
// work.
func HelperRetryable(log []byte) bool {
	_, payload := helperOutcome(log)

	return !payload
}

// Locate works out where the bundle is, with writable standing in for the system's answer.
func Locate(bundle, home string, writable func(string) bool) Location {
	return locate(bundle, home, systemApplications, "", writable)
}
