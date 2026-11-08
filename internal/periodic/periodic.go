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
