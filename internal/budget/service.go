package budget

import (
	"context"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/flight"
	"github.com/mrz1836/nilbox/internal/github"
)

// FreshFor is how long a reading of every budget stands before Check reads them again.
// Checks asked for within it, as a window kept open asks, cost nothing.
const FreshFor = 10 * time.Second

// checkKey names the one read of GitHub's rate limits that runs at a time.
const checkKey = "check"

// Reader reads every budget from GitHub's rate limit endpoint, which costs no budget. The
// readings reach the Tracker through the GitHub client's RateObserver, so it returns
// only whether the read worked.
type Reader interface {
	ReadRates(ctx context.Context) error
}

// Config holds a Service's dependencies. Clock is optional.
type Config struct {
	Tracker *Tracker
	Reader  Reader
	Clock   clock.Clock

	// RefreshInterval is how often the inbox refreshes on its own, from which the window
	// works out the app's hourly pace.
	RefreshInterval time.Duration
}

// Service serves the budgets to the window: as the app's latest responses left them, and,
// when asked, as GitHub reports them now.
type Service struct {
	tracker  *Tracker
	reader   Reader
	clock    clock.Clock
	interval time.Duration
	checks   flight.Group[Gauge]
}

// NewService returns a Service. A nil Clock uses the system clock.
func NewService(cfg Config) *Service {
	return &Service{tracker: cfg.Tracker, reader: cfg.Reader, clock: clock.Or(cfg.Clock), interval: cfg.RefreshInterval}
}

// Gauge returns every budget as the app's latest responses left it, what the app spent
// from each since it last refilled, and what the last inbox refresh cost.
func (s *Service) Gauge() Gauge {
	return s.tracker.gauge(s.refreshesPerHour())
}

// Check reads every budget from GitHub now, unless the app read all of them within
// FreshFor, and returns the gauge. The read costs no budget, and calls that come while
// one runs share it. A failed read returns the gauge as it was, with the error.
func (s *Service) Check(ctx context.Context) (Gauge, error) {
	if s.tracker.freshSince(s.clock.Now().UTC().Add(-FreshFor)) {
		return s.Gauge(), nil
	}

	return s.checks.Do(ctx, checkKey, func(ctx context.Context) (Gauge, error) {
		if err := s.reader.ReadRates(ctx); err != nil {
			return s.Gauge(), fmt.Errorf("read the API budget: %w", err)
		}

		return s.Gauge(), nil
	})
}

// refreshesPerHour is how many times an hour the inbox refreshes on its own.
func (s *Service) refreshesPerHour() int {
	if s.interval <= 0 {
		return 0
	}

	return int(time.Hour / s.interval)
}

// Gauge is every budget the window shows, and the app's own spending. Every field is a
// plain value.
type Gauge struct {
	GraphQL Reading `json:"graphql"`
	Core    Reading `json:"core"`
	Search  Reading `json:"search"`

	// Refresh is what the last whole inbox refresh cost.
	Refresh Refresh `json:"refresh"`

	// RefreshesPerHour is how many times an hour the inbox refreshes on its own.
	RefreshesPerHour int `json:"refreshesPerHour"`

	// Writes is what the app changed on GitHub for the user lately, and how it paces
	// its edits.
	Writes Writes `json:"writes"`
}

// Reading is one budget as GitHub last reported it, and what the app spent from it since
// it last refilled.
type Reading struct {
	// Known is false until GitHub has reported the budget.
	Known bool `json:"known"`

	Limit     int `json:"limit"`
	Remaining int `json:"remaining"`
	Used      int `json:"used"`

	// ResetAt is when it refills in full, and TakenAt when it was read, as RFC 3339.
	ResetAt string `json:"resetAt"`
	TakenAt string `json:"takenAt"`

	// Spent is what the app spent from the budget since it last refilled, and Requests the
	// requests that did. Complete reports whether that counts all of it: false when the
	// app opened after the budget last refilled, so it missed what it spent before.
	Spent    int  `json:"spent"`
	Requests int  `json:"requests"`
	Complete bool `json:"complete"`
}

// Refresh is what one whole inbox refresh cost.
type Refresh struct {
	// Known is false until the inbox refreshed in this run.
	Known bool `json:"known"`

	// At is when it finished, as RFC 3339.
	At string `json:"at"`

	// Points is what it spent of the GraphQL budget, and Items how many pull requests
	// and issues it tracked.
	Points int `json:"points"`
	Items  int `json:"items"`
}

// gauge builds the Gauge from what the Tracker holds.
func (t *Tracker) gauge(perHour int) Gauge {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := Gauge{
		GraphQL:          t.readingLocked(github.ResourceGraphQL),
		Core:             t.readingLocked(github.ResourceCore),
		Search:           t.readingLocked(github.ResourceSearch),
		RefreshesPerHour: perHour,
		Writes:           t.writesLocked(t.clock.Now().UTC()),
	}

	if !t.last.at.IsZero() {
		out.Refresh = Refresh{Known: true, At: clock.Format(t.last.at), Points: t.last.points, Items: t.last.items}
	}

	return out
}

// readingLocked converts the reading held of one budget. The caller holds mu.
func (t *Tracker) readingLocked(resource string) Reading {
	h := t.budgets[resource]
	if h == nil {
		return Reading{}
	}

	r := h.reading

	return Reading{
		Known: true, Limit: r.Limit, Remaining: max(r.Remaining, 0), Used: max(r.Used, 0),
		ResetAt: clock.Format(r.ResetAt), TakenAt: clock.Format(h.takenAt),
		Spent: h.spent, Requests: h.requests, Complete: !h.countedFrom.After(windowStart(r)),
	}
}

// freshSince reports whether every budget the window shows was read at since or later.
func (t *Tracker) freshSince(since time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, resource := range []string{github.ResourceGraphQL, github.ResourceCore, github.ResourceSearch} {
		if h := t.budgets[resource]; h == nil || h.takenAt.Before(since) {
			return false
		}
	}

	return true
}
