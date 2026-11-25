package inbox

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/mrz1836/nilbox/internal/periodic"
)

// DefaultInterval is how often the scheduler refreshes the inbox unless the settings say
// otherwise. At one point per query that is 12 points an hour, a fraction of a percent of
// the GraphQL budget.
const DefaultInterval = 5 * time.Minute

// schedulerJob names the scheduler's refreshes in log entries.
const schedulerJob = "inbox refresh"

// Refresher refreshes the inbox, and plans the next try of a refresh that failed for a
// reason a later try may well not meet (see retry.go): retryAt is when it is due, zero
// while none is planned, and onRetry has wake called as each is planned. The Service is
// the one Refresher outside the tests.
type Refresher interface {
	Refresh(ctx context.Context) (Snapshot, error)
	retryAt() time.Time
	onRetry(wake func())
}

// retryAt is when the next try of a failed refresh is due, zero while none is planned.
func (s *Service) retryAt() time.Time {
	return s.retries.next()
}

// onRetry has wake called each time a failed refresh plans another try.
func (s *Service) onRetry(wake func()) {
	s.retries.onPlanned(wake)
}

// Scheduler refreshes the inbox when it starts and then as often as its every says, the
// user's setting, DefaultInterval when nil, from the end of each refresh, so refreshes
// never overlap. A refresh that failed for a reason a later try may well not meet, whoever
// ran it, is tried again sooner, when the Refresher's plan says (see due), and the
// Refresher wakes it as each try is planned. Wake makes it read the setting again at once.
// Failures are logged; a panicking refresh is recovered, so the scheduler keeps going
// until its context is canceled.
type Scheduler struct {
	job   *periodic.Job
	every periodic.Every
	r     Refresher

	// ended is when the scheduler's own last refresh ended, in Unix nanoseconds, zero
	// before the first: the time periodic.RunEvery waits from.
	ended atomic.Int64
}

// NewScheduler returns a scheduler that refreshes through r as often as every says,
// logging failures to logger. A nil every refreshes every DefaultInterval, and a nil
// logger discards the failures. It has r wake it as each try of a failed refresh is
// planned.
func NewScheduler(r Refresher, every periodic.Every, logger *slog.Logger) *Scheduler {
	if every == nil {
		every = func(context.Context) time.Duration { return DefaultInterval }
	}

	s := &Scheduler{every: every, r: r}
	refresh := refreshThrough(r)
	s.job = periodic.NewJob(s.due, schedulerJob, func(ctx context.Context) error {
		defer s.ended.Store(time.Now().UnixNano())

		return refresh(ctx)
	}, logger)

	r.onRetry(s.job.Wake)

	return s
}

// Run refreshes now and then as often as every says until ctx is canceled, then waits for
// an in-flight refresh to return.
func (s *Scheduler) Run(ctx context.Context) {
	s.job.Run(ctx)
}

// Wake has the scheduler read how often to refresh again, as when the user changed it.
func (s *Scheduler) Wake() {
	s.job.Wake()
}

// due is how long periodic.RunEvery waits after the scheduler's last refresh: the user's
// interval, or less while a try of a failed refresh is planned, so the next refresh starts
// when that try is due, measured from the end of the scheduler's own last refresh, which
// may be older than the refresh that failed. A try already due waits no more. While the
// interval is zero or less, nothing runs, a planned try included.
func (s *Scheduler) due(ctx context.Context) time.Duration {
	d := s.every(ctx)

	at := s.r.retryAt()
	if d <= 0 || at.IsZero() {
		return d
	}

	ended := s.ended.Load()
	if ended == 0 {
		return d // the first refresh starts at once
	}

	// A wait of zero would turn the scheduler off, so a try already due waits a nanosecond.
	return min(d, max(at.Sub(time.Unix(0, ended)), time.Nanosecond))
}

// refreshThrough returns one refresh through r, as the scheduler's job.
func refreshThrough(r Refresher) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		_, err := r.Refresh(ctx)

		return err
	}
}
