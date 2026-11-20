// Package milestones keeps the local log of the inbox reaching nil, and says what each
// time is worth: the streak of days running it reached nil, the best streak, how many times
// the user emptied it while they watched, the achievements a time unlocks, and whether the
// window throws confetti for it, which it does at most once a local day.
//
// The log is one preference, milestones.log (KeyLog), which this package alone writes, read
// and written in one transaction each time the inbox reaches nil, so the once-a-day cap
// holds across restarts. It is history, like the triage, so a backup never holds it, and a
// reset clears it with the rest of the database. Days are calendar days where the user is
// (WithLocation).
//
// Every time counts toward the streak, an inbox found empty as well as one emptied, but
// only an inbox the user emptied while they watched counts as a nil of theirs and unlocks
// achievements, each once: a streak or a count reaching a tier, the first nil, a sweep of
// SweepAt or more items, and the badges for when it happened (the Kind constants). The
// tiers never run out, so neither does the confetti: a streak of 3, 7, 14, and 30 days, then
// every 30, and a 10th, 25th, 50th, and 100th nil, then every 100th. A streak tier reached
// on a day the inbox was only found empty unlocks with the next nil the user earns.
package milestones

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/store"
)

// KeyLog is the preference the log is stored under, as JSON.
const KeyLog = "milestones.log"

// SweepAt is how many items cleared in one sitting, on the way to nil, unlock KindSweep.
const SweepAt = 10

// dayLayout writes a local calendar day, which sorts as text in time order.
const dayLayout = time.DateOnly

// Achievement is one thing a time at nil unlocked: its kind, and its tier (the streak's
// days, the nil's number, SweepAt for a sweep) or 1 for a kind with none.
type Achievement struct {
	Kind Kind `json:"kind"`
	N    int  `json:"n"`
}

// key is how the log records the achievement as unlocked.
func (a Achievement) key() string {
	return string(a.Kind) + ":" + strconv.Itoa(a.N)
}

// Reach is a time the inbox reached nil, as the window saw it.
type Reach struct {
	// Earned is whether the inbox emptied while the user watched, rather than being found
	// empty.
	Earned bool `json:"earned"`

	// Cleared is the most the inbox held while the user watched, on the way there.
	Cleared int `json:"cleared"`

	// Loud is whether the window throws confetti for an achievement on its own (Settings →
	// Appearance's Milestones). Quiet, a time unlocks its achievements all the same, and
	// throws none.
	Loud bool `json:"loud"`
}

// Moment is what a time at nil was worth.
type Moment struct {
	// Earned and Cleared are the Reach's, Cleared never below 0.
	Earned  bool `json:"earned"`
	Cleared int  `json:"cleared"`

	// Streak is how many days running, today included, the inbox has reached nil, and Best
	// the longest streak there has been.
	Streak int `json:"streak"`
	Best   int `json:"best"`

	// Count is how many times the user has emptied the inbox while they watched, this time
	// included when it was one of them.
	Count int `json:"count"`

	// Unlocked are the achievements this time unlocked, never nil, the most significant
	// first: a streak, a count, the first nil, a sweep, then the badges, each kind's highest
	// tier first.
	Unlocked []Achievement `json:"unlocked"`

	// Confetti is whether the window throws confetti for it on its own: a loud time that
	// unlocked something, when none has flown yet today.
	Confetti bool `json:"confetti"`
}

// Service keeps the log, and is bound to the window.
type Service struct {
	prefs  *prefs.Store
	clock  clock.Clock
	loc    *time.Location
	logger *slog.Logger
}

// Option configures a Service.
type Option func(*Service)

// WithClock reads the time, and stamps the log, from c rather than from the system clock.
func WithClock(c clock.Clock) Option {
	return func(s *Service) { s.clock = c }
}

// WithLocation counts days, and reads the hour and the weekday, in loc rather than in
// time.Local.
func WithLocation(loc *time.Location) Option {
	return func(s *Service) { s.loc = loc }
}

// WithLogger logs to logger a log that could not be read, and starts afresh. Without it,
// or with nil, that is not logged.
func WithLogger(logger *slog.Logger) Option {
	return func(s *Service) { s.logger = logger }
}

// NewService returns a Service over the log in pool's preferences.
func NewService(pool *sql.DB, opts ...Option) *Service {
	s := &Service{}

	for _, opt := range opts {
		opt(s)
	}

	s.clock = clock.Or(s.clock)
	s.prefs = prefs.New(pool, s.clock)
	s.logger = logging.Or(s.logger)

	if s.loc == nil {
		s.loc = time.Local //nolint:gosmopolitan // a day at nil is a day where the user is
	}

	return s
}

// Reached records that the inbox reached nil, now, and returns what that was worth. The log
// is read and written in one transaction, so two windows reaching nil at once each count,
// and confetti flies for one of them at most. When it fails, nothing is recorded, and the
// moment is empty.
func (s *Service) Reached(ctx context.Context, reach Reach) (Moment, error) {
	now := s.clock.Now().In(s.loc)

	var out Moment

	err := s.prefs.Update(ctx, KeyLog, func(stored string, ok bool) (string, error) {
		rec := s.decode(ctx, stored, ok)
		out = rec.reach(reach, now)

		return store.EncodeJSON(rec)
	})
	if err != nil {
		return Moment{Unlocked: []Achievement{}}, fmt.Errorf("record the inbox at nil: %w", err)
	}

	return out, nil
}

// decode reads the stored log, or starts a fresh one when none is stored or it can't be
// read.
func (s *Service) decode(ctx context.Context, stored string, ok bool) record {
	if !ok {
		return fresh()
	}

	rec, err := store.DecodeJSON[record](stored)
	if err != nil {
		s.logger.WarnContext(ctx, "the inbox's milestones could not be read, so they start afresh", "error", err)

		return fresh()
	}

	rec.sane()

	return rec
}

// record is the log as stored.
type record struct {
	// Count is how many times the user emptied the inbox while they watched.
	Count int `json:"count"`

	// Streak is the days running, up to LastDay, that the inbox reached nil, and Best the
	// longest streak.
	Streak int `json:"streak"`
	Best   int `json:"best"`

	// LastDay is the last day the inbox reached nil, and ConfettiDay the last day confetti
	// flew on its own, each a local calendar day (dayLayout), or "" for never.
	LastDay     string `json:"lastDay"`
	ConfettiDay string `json:"confettiDay"`

	// Unlocked are the achievements unlocked so far, by key, in the order they were.
	Unlocked []string `json:"unlocked"`
}

// fresh is the log before the inbox first reaches nil.
func fresh() record {
	return record{Unlocked: []string{}}
}

// sane reads what no build stores in r as never having happened: a negative number as 0,
// and a day that isn't one as never, with its streak.
func (r *record) sane() {
	r.Count, r.Streak, r.Best = max(r.Count, 0), max(r.Streak, 0), max(r.Best, 0)

	if !isDay(r.LastDay) {
		r.LastDay, r.Streak = "", 0
	}

	if !isDay(r.ConfettiDay) {
		r.ConfettiDay = ""
	}

	r.Best = max(r.Best, r.Streak)

	if r.Unlocked == nil {
		r.Unlocked = []string{}
	}
}

// isDay reports whether s is a calendar day as the log writes one.
func isDay(s string) bool {
	_, err := time.Parse(dayLayout, s)

	return err == nil
}

// reach records a time at nil, now, in r, and returns what it was worth.
func (r *record) reach(reach Reach, now time.Time) Moment {
	r.extend(now)

	out := Moment{Earned: reach.Earned, Cleared: max(reach.Cleared, 0), Unlocked: []Achievement{}}

	if reach.Earned {
		r.Count++
		out.Unlocked = r.unlock(r.earned(out.Cleared, now))
	}

	if today := now.Format(dayLayout); reach.Loud && len(out.Unlocked) > 0 && r.ConfettiDay != today {
		r.ConfettiDay = today
		out.Confetti = true
	}

	out.Streak, out.Best, out.Count = r.Streak, r.Best, r.Count

	return out
}

// extend carries the streak to now: unchanged on the day it was last extended, one longer
// the day after, and started again after a day without. A day before the last one, after
// the clock or the zone moved back, leaves it as it is.
func (r *record) extend(now time.Time) {
	today := now.Format(dayLayout)

	switch {
	case r.LastDay == "" || r.Streak < 1:
		r.Streak = 1
	case today <= r.LastDay:
	case r.LastDay == yesterday(now):
		r.Streak++
	default:
		r.Streak = 1
	}

	r.LastDay = max(r.LastDay, today)
	r.Best = max(r.Best, r.Streak)
}

// yesterday is the calendar day before now's, where now is. Noon keeps a day that a
// clock change makes 23 or 25 hours long from landing on the wrong side of midnight.
func yesterday(now time.Time) string {
	return time.Date(now.Year(), now.Month(), now.Day()-1, 12, 0, 0, 0, now.Location()).Format(dayLayout)
}

// earned are the achievements a nil the user earned now, after clearing cleared items,
// may unlock, the most significant first, unlocked before or not.
func (r *record) earned(cleared int, now time.Time) []Achievement {
	var out []Achievement

	for _, n := range slices.Backward(tiers(r.Streak, []int{3, 7, 14, 30}, 30)) {
		out = append(out, Achievement{Kind: KindStreak, N: n})
	}

	for _, n := range slices.Backward(tiers(r.Count, []int{10, 25, 50, 100}, 100)) {
		out = append(out, Achievement{Kind: KindCount, N: n})
	}

	out = append(out, Achievement{Kind: KindFirst, N: 1})

	if cleared >= SweepAt {
		out = append(out, Achievement{Kind: KindSweep, N: SweepAt})
	}

	hour, weekday := now.Hour(), now.Weekday()

	for _, badge := range []struct {
		kind Kind
		won  bool
	}{
		{KindEarly, hour >= 5 && hour < 12},
		{KindFriday, weekday == time.Friday},
		{KindWeekend, weekday == time.Saturday || weekday == time.Sunday},
		{KindOwl, hour >= 22 || hour < 5},
	} {
		if badge.won {
			out = append(out, Achievement{Kind: badge.kind, N: 1})
		}
	}

	return out
}

// unlock records each of candidates not unlocked before, and returns them in their order.
func (r *record) unlock(candidates []Achievement) []Achievement {
	out := []Achievement{}

	for _, a := range candidates {
		if key := a.key(); !slices.Contains(r.Unlocked, key) {
			r.Unlocked = append(r.Unlocked, key)
			out = append(out, a)
		}
	}

	return out
}

// tiers returns the tiers up to n, smallest first: each of first, then every step past
// the last of them.
func tiers(n int, first []int, step int) []int {
	out := []int{}

	for _, t := range first {
		if t <= n {
			out = append(out, t)
		}
	}

	for t := first[len(first)-1] + step; t <= n; t += step {
		out = append(out, t)
	}

	return out
}
