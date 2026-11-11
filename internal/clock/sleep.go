package clock

import (
	"context"
	"time"
)

// Sleep waits d on the real clock, or returns ctx's error once ctx ends first. A Clock
// can't make timers, so work that waits, as a merge's guard or a release's follow does,
// waits here and reads the time from the real clock too: in tests, from a synctest
// bubble's.
func Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
