package inbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/mrz1836/nilbox/internal/periodic"
)

// DefaultInterval is how often the scheduler refreshes the inbox unless the settings say
// otherwise. At one point per query that is 12 points an hour, a fraction of a percent of
// the GraphQL budget.
const DefaultInterval = 5 * time.Minute

// schedulerJob names the scheduler's refreshes in log entries.
const schedulerJob = "inbox refresh"

// Refresher refreshes the inbox.
type Refresher interface {
	Refresh(ctx context.Context) (Snapshot, error)
}

// Scheduler refreshes the inbox when it starts and then as often as its every says, the
// user's setting, DefaultInterval when nil, from the end of each refresh, so refreshes
// never overlap. Wake makes it read the setting again at once. Failures are logged; a
// panicking refresh is recovered, so the scheduler keeps going until its context is
// canceled.
type Scheduler struct {
	refresher Refresher
	every     periodic.Every
	wake      chan struct{}
	logger    *slog.Logger
}

// NewScheduler returns a scheduler that refreshes through r as often as every says,
// logging failures to logger. A nil every refreshes every DefaultInterval, and a nil
// logger discards the failures.
func NewScheduler(r Refresher, every periodic.Every, logger *slog.Logger) *Scheduler {
	if every == nil {
		every = func(context.Context) time.Duration { return DefaultInterval }
	}

	return &Scheduler{refresher: r, every: every, wake: make(chan struct{}, 1), logger: logger}
}

// Run refreshes now and then as often as every says until ctx is canceled, then waits for
// an in-flight refresh to return.
func (s *Scheduler) Run(ctx context.Context) {
	periodic.RunEvery(ctx, s.every, s.wake, schedulerJob, s.refresh, s.logger)
}

// Wake has the scheduler read how often to refresh again, as when the user changed it.
func (s *Scheduler) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// refresh runs one refresh.
func (s *Scheduler) refresh(ctx context.Context) error {
	_, err := s.refresher.Refresh(ctx)

	return err
}
