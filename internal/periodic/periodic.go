package periodic

import (
	"context"
	"log/slog"
	"time"

	"github.com/mrz1836/nilbox/internal/logging"
)

// Run calls fn now and then every interval until ctx is canceled, and returns once the
// run in flight has returned. Runs never overlap: a tick that comes while fn is still
// running is skipped, so a run longer than the interval is followed by the next tick,
// not by a run straight away.
//
// name labels the job in log entries: a failure (an error returned while ctx is still
// live) is logged as "<name> failed", and a panic is recovered and logged as "<name>
// panicked". A nil logger discards them. A non-positive interval panics, as
// time.NewTicker does.
func Run(ctx context.Context, interval time.Duration, name string, fn func(context.Context) error, log *slog.Logger) {
	log = logging.Or(log)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		once(ctx, name, fn, log)

		select {
		case <-ticker.C:
			log.DebugContext(ctx, name+" skipped: the previous run was still going")
		default:
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// once runs fn, logging a failure or a panic.
func once(ctx context.Context, name string, fn func(context.Context) error, log *slog.Logger) {
	defer func() {
		if r := recover(); r != nil {
			log.ErrorContext(ctx, name+" panicked", "panic", r)
		}
	}()

	if err := fn(ctx); err != nil && ctx.Err() == nil {
		log.WarnContext(ctx, name+" failed", "error", err)
	}
}

// Every says how long to wait after a run before the next, as set now: Run asks again
// before each wait, and again whenever it is woken, so a change applies at once. Zero or
// less means not to run until woken with a positive interval.
type Every func(ctx context.Context) time.Duration

// RunEvery calls fn whenever every says one is due, until ctx is canceled, and returns
// once the run in flight has returned: at once, then every(ctx) after each run ends, so
// runs never overlap and a long run is never followed by another straight away. A send on
// wake (a setting changed) makes it ask every again: a run already due under the new
// interval starts at once, and otherwise the wait is the new interval from the last run's
// end. While every says zero or less, nothing runs, the first run included.
//
// name labels the job in log entries, as Run's does. A nil logger discards them.
func RunEvery(ctx context.Context, every Every, wake <-chan struct{}, name string, fn func(context.Context) error,
	log *slog.Logger,
) {
	log = logging.Or(log)

	var last time.Time // when the last run ended; zero before the first

	for {
		d := every(ctx)

		if d > 0 && (last.IsZero() || time.Since(last) >= d) {
			once(ctx, name, fn, log)

			if ctx.Err() != nil {
				return
			}

			last = time.Now()

			continue
		}

		if !wait(ctx, d, last, wake) {
			return
		}
	}
}

// wait waits until a run is due under d, from last, or wake fires, and reports false once
// ctx is canceled. With d zero or less it waits for wake or ctx alone.
func wait(ctx context.Context, d time.Duration, last time.Time, wake <-chan struct{}) bool {
	var due <-chan time.Time

	if d > 0 {
		timer := time.NewTimer(d - time.Since(last))
		defer timer.Stop()

		due = timer.C
	}

	select {
	case <-ctx.Done():
		return false
	case <-due:
	case <-wake:
	}

	return true
}
