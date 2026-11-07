package inbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/mrz1836/nilbox/internal/periodic"
)

// DefaultInterval is how often the scheduler refreshes the inbox. At one point per query
// that is 12 points an hour, a fraction of a percent of the GraphQL budget.
const DefaultInterval = 5 * time.Minute

// schedulerJob names the scheduler's refreshes in log entries.
const schedulerJob = "inbox refresh"

// Refresher refreshes the inbox.
type Refresher interface {
	Refresh(ctx context.Context) (Snapshot, error)
}

// Scheduler refreshes the inbox when it starts and then every DefaultInterval. A tick
// that arrives while a refresh is still running is skipped. Failures are logged; a
// panicking refresh is recovered, so the scheduler keeps going until its context is
// canceled.
type Scheduler struct {
	refresher Refresher
	interval  time.Duration
	logger    *slog.Logger
}

// NewScheduler returns a scheduler that refreshes through r, logging failures to logger.
// A nil logger discards them.
func NewScheduler(r Refresher, logger *slog.Logger) *Scheduler {
	return &Scheduler{refresher: r, interval: DefaultInterval, logger: logger}
}

// Run refreshes now and on every tick until ctx is canceled, then waits for an in-flight
// refresh to return.
func (s *Scheduler) Run(ctx context.Context) {
	periodic.Run(ctx, s.interval, schedulerJob, s.refresh, s.logger)
}

// refresh runs one refresh.
func (s *Scheduler) refresh(ctx context.Context) error {
	_, err := s.refresher.Refresh(ctx)

	return err
}
