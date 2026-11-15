package svcstatus

import (
	"context"
	"time"

	"github.com/mrz1836/nilbox/internal/statusprefs"
)

// panicWait is how long the Checker waits after a pass that panicked, so a panic that
// recurs never spins.
const panicWait = time.Minute

// Checker checks the pages of the providers the user watches as often as the settings
// say. It is a desktop worker, never bound.
//
// A provider is due once it is watched and has never been checked since it was turned
// on, or its last check ended Every ago. A pass checks every provider due, and, when one
// is due by its age, takes along any other watched provider whose last check ended Every/2
// ago or more, so the providers settle onto one timer and a pass costs one wake. Between
// passes it waits for the next provider to come due, a Wake (the interval changed), a
// RulesChanged (what is watched changed), or the end of its context.
type Checker struct {
	s     *Service
	wake  chan struct{}
	rules chan struct{}
}

// NewChecker returns a Checker over s.
func NewChecker(s *Service) *Checker {
	return &Checker{s: s, wake: make(chan struct{}, 1), rules: make(chan struct{}, 1)}
}

// Run checks every provider watched now, then each as it comes due, until ctx is
// canceled, and returns once the pass running has returned. While the check is off it
// reads nothing.
func (c *Checker) Run(ctx context.Context) {
	for ctx.Err() == nil {
		wait, timed := c.pass(ctx)
		if !c.sleep(ctx, wait, timed) {
			return
		}
	}
}

// Wake has the Checker read how often to check again, as when the user changed it.
func (c *Checker) Wake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// RulesChanged has the Checker follow a change of what the user watches: a provider no
// longer watched is forgotten, the baselines of the alerts are set quietly, the window is
// told, and a provider turned on is checked at once.
func (c *Checker) RulesChanged() {
	select {
	case c.rules <- struct{}{}:
	default:
	}
}

// sleep waits for wait, when timed, for a wake, or for a change of the rules, which it
// applies, and reports false once ctx is canceled.
func (c *Checker) sleep(ctx context.Context, wait time.Duration, timed bool) bool {
	var due <-chan time.Time

	if timed {
		timer := time.NewTimer(wait)
		defer timer.Stop()

		due = timer.C
	}

	select {
	case <-ctx.Done():
		return false
	case <-due:
	case <-c.wake:
	case <-c.rules:
		c.s.applyRules(ctx)
	}

	return true
}

// pass checks the providers due, and says how long until the next comes due; timed is
// false while the check is off or nothing is watched, when nothing comes due until a
// wake. A panic is logged, and the next pass waits panicWait.
func (c *Checker) pass(ctx context.Context) (wait time.Duration, timed bool) {
	defer func() {
		if r := recover(); r != nil {
			c.s.logger.ErrorContext(ctx, "the service status check panicked", "panic", r)

			wait, timed = panicWait, true
		}
	}()

	rules := c.s.rules(ctx)
	if rules.Every <= 0 {
		return 0, false
	}

	c.s.checkAll(ctx, c.s.due(rules), rules)

	return c.s.untilDue(rules)
}

// due returns the providers a pass checks now, in catalog order: those never checked
// since they were turned on, and those whose last check ended rules.Every ago, with,
// when there is one of those, any whose last check ended rules.Every/2 ago.
func (s *Service) due(rules Rules) []statusprefs.Provider {
	now := s.clock.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	aged := len(s.watched(rules, func(k *kept) bool {
		return !k.triedAt.IsZero() && now.Sub(k.triedAt) >= rules.Every
	})) > 0

	return s.watched(rules, func(k *kept) bool {
		return k.triedAt.IsZero() || aged && now.Sub(k.triedAt) >= rules.Every/2
	})
}

// untilDue says how long until the next watched provider comes due, or false when none is
// watched.
func (s *Service) untilDue(rules Rules) (time.Duration, bool) {
	now := s.clock.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	var next time.Time

	for _, p := range s.watched(rules, func(*kept) bool { return true }) {
		at := now
		if k := s.kept[p.ID]; !k.triedAt.IsZero() {
			at = k.triedAt.Add(rules.Every)
		}

		if next.IsZero() || at.Before(next) {
			next = at
		}
	}

	if next.IsZero() {
		return 0, false
	}

	return max(0, next.Sub(now)), true
}
