package inbox

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultInterval is how often the scheduler refreshes the inbox. At one point per query
// that is 12 points an hour, a fraction of a percent of the GraphQL budget.
const DefaultInterval = 5 * time.Minute

// Refresher refreshes the inbox.
type Refresher interface {
	Refresh(ctx context.Context) (Snapshot, error)
}

// Ticker delivers ticks on C until stopped.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// SchedulerOption configures a Scheduler.
type SchedulerOption func(*Scheduler)

// WithInterval sets the refresh interval. A non-positive interval is ignored.
func WithInterval(d time.Duration) SchedulerOption {
	return func(s *Scheduler) {
		if d > 0 {
			s.interval = d
		}
	}
}

// WithTicker replaces the real ticker, for tests.
func WithTicker(newTicker func(time.Duration) Ticker) SchedulerOption {
	return func(s *Scheduler) { s.newTicker = newTicker }
}

// WithLogger sets the logger for refresh failures.
func WithLogger(logger *slog.Logger) SchedulerOption {
	return func(s *Scheduler) { s.logger = logger }
}

// Scheduler refreshes the inbox when it starts and then on every tick. A tick that
// arrives while a refresh is still running is skipped. Failures are logged; a panicking
// refresh is recovered, so the scheduler keeps going until its context is canceled.
type Scheduler struct {
	refresher Refresher
	interval  time.Duration
	newTicker func(time.Duration) Ticker
	logger    *slog.Logger
	running   atomic.Bool
}

// NewScheduler returns a scheduler that refreshes through r every DefaultInterval.
func NewScheduler(r Refresher, opts ...SchedulerOption) *Scheduler {
	s := &Scheduler{refresher: r, interval: DefaultInterval, newTicker: newTimeTicker, logger: slog.Default()}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Run refreshes now and on every tick until ctx is canceled, then waits for an in-flight
// refresh to return.
func (s *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	defer wg.Wait()

	ticker := s.newTicker(s.interval)
	defer ticker.Stop()

	s.trigger(ctx, &wg)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			s.trigger(ctx, &wg)
		}
	}
}

// trigger starts a refresh unless one is already running.
func (s *Scheduler) trigger(ctx context.Context, wg *sync.WaitGroup) {
	if !s.running.CompareAndSwap(false, true) {
		s.logger.DebugContext(ctx, "inbox refresh skipped: the previous one is still running")

		return
	}

	wg.Go(func() {
		defer s.running.Store(false)

		s.refresh(ctx)
	})
}

// refresh runs one refresh, logging a failure or a panic.
func (s *Scheduler) refresh(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.ErrorContext(ctx, "inbox refresh panicked", "panic", r)
		}
	}()

	if _, err := s.refresher.Refresh(ctx); err != nil && ctx.Err() == nil {
		s.logger.WarnContext(ctx, "inbox refresh failed", "error", err)
	}
}

// timeTicker adapts time.Ticker.
type timeTicker struct {
	t *time.Ticker
}

// newTimeTicker starts a real ticker.
func newTimeTicker(d time.Duration) Ticker {
	return timeTicker{t: time.NewTicker(d)}
}

// C returns the tick channel.
func (t timeTicker) C() <-chan time.Time { return t.t.C }

// Stop stops the ticker.
func (t timeTicker) Stop() { t.t.Stop() }
