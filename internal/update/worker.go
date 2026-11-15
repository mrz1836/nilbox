package update

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/periodic"
)

// Worker runs the engine's timed checks. It is a desktop worker, run as deferred work, so
// never on the cold start, and never bound.
//
// It first reads how the last restart went, cleans up after it, and loads the update kept
// in the cache (so one waiting is ready at once, with no download), waiting first while the
// last install's helper may still be at work (HelperWindow); FirstCheckAfter later it
// looks whether a check is due, and again every TickEvery: a check is due once CheckEvery
// has passed on the wall clock since the last, since timers stand still while a Mac sleeps.
// A Wake (the setting changed, or a manual check found a version to download) has it look
// at once. Its checks and the window's never overlap.
type Worker struct {
	e      *Engine
	wake   chan struct{}
	urgent atomic.Bool
}

// NewWorker returns the Worker of e, which the engine wakes when a manual check finds a
// version to download.
func NewWorker(e *Engine) *Worker {
	w := &Worker{e: e, wake: make(chan struct{}, 1)}
	e.setWake(w.Wake)

	return w
}

// Run settles the start, waits FirstCheckAfter, and then looks every TickEvery, or when
// woken, until ctx ends, returning once the look in flight has. A build that doesn't
// update returns at once.
func (w *Worker) Run(ctx context.Context) {
	if !w.e.Enabled() {
		return
	}

	// While the last install's helper may still be at work, the start settles once it
	// can't be, before any check.
	for wait := w.e.settle(ctx); wait > 0; wait = w.e.settle(ctx) {
		if clock.Sleep(ctx, wait) != nil {
			return
		}
	}

	if clock.Sleep(ctx, FirstCheckAfter) != nil {
		return
	}

	periodic.RunEvery(ctx, w.every, w.wake, "the update check", w.tick, w.e.logger)
}

// Wake has the Worker look at once, as when the user changed the setting.
func (w *Worker) Wake() {
	w.urgent.Store(true)

	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// every is TickEvery, or no wait at all once woken, so the look runs at once.
func (w *Worker) every(context.Context) time.Duration {
	if w.urgent.Load() {
		return time.Nanosecond
	}

	return TickEvery
}

// tick looks whether something is due.
func (w *Worker) tick(ctx context.Context) error {
	w.urgent.Store(false)

	return w.e.tick(ctx)
}
