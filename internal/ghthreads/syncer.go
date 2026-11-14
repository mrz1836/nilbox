package ghthreads

import (
	"context"
	"log/slog"
	"time"

	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/periodic"
)

// DefaultInterval is how often the Syncer runs a pass when it is given no interval: the
// inbox's default refresh.
const DefaultInterval = 5 * time.Minute

// syncerJob names the Syncer's passes in log entries.
const syncerJob = "GitHub notifications pass"

// passer is what the Syncer runs: *Service.
type passer interface {
	runPass(ctx context.Context, req passRequest) error
	pollInterval() time.Duration
	lastEnded() time.Time

	// takeNudge, woken, and rulesChanged are the nudges: what the Syncer was asked to run
	// soon, what wakes it to look, and the rules changing.
	takeNudge() (passRequest, bool)
	woken() <-chan struct{}
	rulesChanged()

	// untilSend says how long until a queued clear may be sent.
	untilSend() (time.Duration, bool)
}

// Syncer runs a pass when it starts, then again as often as its every says (the inbox's
// refresh setting), but never sooner than GitHub's poll interval asks, from the end of the
// last pass, its own or CheckNow's. Wake makes it read the interval again at once. Between
// passes it runs what it is nudged to at once (what the inbox tells the Settler, and the
// rules changing, RulesChanged), and a run that sends the queued clears as soon as the next
// may go, so an Undo hold of seconds is never stretched into the interval's wait. A pass
// that fails is logged, and one that panics is recovered and logged, so the Syncer keeps
// going until its context is canceled.
type Syncer struct {
	svc    passer
	every  periodic.Every
	wake   chan struct{}
	logger *slog.Logger
}

// NewSyncer returns a Syncer that runs svc's passes as often as every says, logging
// failures to logger. A nil every runs one every DefaultInterval, and a nil logger
// discards the failures.
func NewSyncer(svc *Service, every periodic.Every, logger *slog.Logger) *Syncer {
	return newSyncer(svc, every, logger)
}

// newSyncer returns a Syncer over p.
func newSyncer(p passer, every periodic.Every, logger *slog.Logger) *Syncer {
	if every == nil {
		every = func(context.Context) time.Duration { return DefaultInterval }
	}

	return &Syncer{svc: p, every: every, wake: make(chan struct{}, 1), logger: logging.Or(logger)}
}

// Wake has the Syncer read how often to run a pass again, as when the user changed it.
func (s *Syncer) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// RulesChanged has the Syncer decide every thread again soon, under the rules as they are
// now, as when the user changed them: clearing turned off skips the queued clears, and
// turned on, or changed, clears what it now finds finished, FirstCap at most.
func (s *Syncer) RulesChanged() {
	s.svc.rulesChanged()
}

// Run runs a pass now, then whenever one is due, until ctx is canceled, and returns once
// the pass running has returned. While every says zero or less, no timed pass runs, the
// first included, until a wake finds it positive; what it is nudged to, and the sends, run
// all the same.
func (s *Syncer) Run(ctx context.Context) {
	first := true

	for ctx.Err() == nil {
		ended := s.svc.lastEnded()
		wait, timed := s.untilDue(ctx, first, ended)

		req, run := s.next(timed && wait <= 0, ended)
		if !run {
			if !s.sleep(ctx, wait, timed) {
				return
			}

			continue
		}

		s.once(ctx, req)
		first = first && !req.timed
	}
}

// next returns the run due now, if any: the timed pass when due (found due when the last
// pass had ended at ended), else what the Syncer was nudged to, else a run that sends the
// queued clears once the next may go.
func (s *Syncer) next(due bool, ended time.Time) (passRequest, bool) {
	if due {
		return passRequest{timed: true, lastEnded: ended}, true
	}

	if req, ok := s.svc.takeNudge(); ok {
		return req, true
	}

	if wait, ok := s.svc.untilSend(); ok && wait <= 0 {
		return passRequest{send: true}, true
	}

	return passRequest{}, false
}

// sleep waits for wait, when timed, for the next queued clear to come due, for a nudge, or
// for a wake, and reports false once ctx is canceled. Each only has Run work out what is
// due again.
func (s *Syncer) sleep(ctx context.Context, wait time.Duration, timed bool) bool {
	var due, send <-chan time.Time

	if timed {
		timer := time.NewTimer(wait)
		defer timer.Stop()

		due = timer.C
	}

	if until, ok := s.svc.untilSend(); ok {
		timer := time.NewTimer(until)
		defer timer.Stop()

		send = timer.C
	}

	select {
	case <-ctx.Done():
		return false
	case <-due:
	case <-send:
	case <-s.svc.woken():
	case <-s.wake:
	}

	return true
}

// untilDue says how long until the next pass is due, as set now, when the last pass ended
// at ended: at once for the first, and otherwise the longer of every and GitHub's poll
// interval after ended. timed is false while every says zero or less: nothing is due until
// a wake.
func (s *Syncer) untilDue(ctx context.Context, first bool, ended time.Time) (wait time.Duration, timed bool) {
	every := s.every(ctx)
	if every <= 0 {
		return 0, false
	}

	if first || ended.IsZero() {
		return 0, true
	}

	return time.Until(ended.Add(max(every, s.svc.pollInterval()))), true
}

// once runs req, logging a failure or a panic. A timed pass runs nothing when another
// (CheckNow's) ended while it waited for the one running, so a timer that fires during
// CheckNow never lists again right after it.
func (s *Syncer) once(ctx context.Context, req passRequest) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.ErrorContext(ctx, syncerJob+" panicked", "panic", r)
		}
	}()

	if err := s.svc.runPass(ctx, req); err != nil && ctx.Err() == nil {
		s.logger.WarnContext(ctx, syncerJob+" failed", slog.Any("error", err))
	}
}
