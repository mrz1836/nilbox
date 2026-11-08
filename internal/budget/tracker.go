// Package budget keeps the app's view of the user's GitHub API budgets current.
//
// GitHub reports a budget with every API response, so the Tracker, which the GitHub client
// tells about each one (github.RateObserver), knows what is left as of the app's latest
// call, whichever service made it: an inbox refresh, a single card's refresh, a file check,
// or a worktree sweep. It also counts what the app itself spent from each budget since the
// budget last refilled, so the window can tell the app's share from everything else signed
// in as the same account (gh, coding agents), and it keeps what the last whole inbox
// refresh cost and how many items it tracked, from which the window works out the app's
// hourly pace. None of this makes a request of its own: Service.Check reads GitHub's free
// rate limit endpoint, and only when the window asks.
//
// Each change emits an event.BudgetUpdated, and the window reads the budgets again.
package budget

import (
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
)

// windowLength is how long each budget GitHub keeps lasts, from one refill to the next:
// an hour for GraphQL and REST, a minute for search. A budget not listed is an hour.
func windowLength(resource string) time.Duration {
	if resource == github.ResourceSearch {
		return time.Minute
	}

	return time.Hour
}

// Stored is a reading kept from an earlier run, which the Tracker starts from until a
// response says otherwise.
type Stored struct {
	Reading github.RateReading

	// TakenAt is when it was read.
	TakenAt time.Time
}

// held is the newest reading of one budget, and what the app spent from it.
type held struct {
	reading github.RateReading
	takenAt time.Time

	// spent and requests count what the app spent from the budget, and the requests that
	// did, since countedFrom, in the window that ends at reading.ResetAt. countedFrom is
	// the window's start when the app was running by then.
	spent       int
	requests    int
	countedFrom time.Time
}

// windowStart is when the window a reading belongs to began.
func windowStart(r github.RateReading) time.Time {
	return r.ResetAt.Add(-windowLength(r.Resource))
}

// later returns the later of a and b.
func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}

	return b
}

// refresh is the last whole inbox refresh.
type refresh struct {
	at     time.Time
	points int
	items  int
}

// Tracker holds the newest reading of each budget, what the app spent from each since it
// last refilled, and the last refresh's cost. It is safe for concurrent use.
type Tracker struct {
	clock   clock.Clock
	events  event.Sink
	started time.Time

	mu      sync.Mutex
	budgets map[string]*held
	last    refresh

	// dirty holds a signal while a change waits to be saved (see Saver).
	dirty chan struct{}
}

// NewTracker returns a Tracker that stamps readings from c, the system clock when nil, and
// emits an event.BudgetUpdated into events, if any, on every change.
func NewTracker(c clock.Clock, events event.Sink) *Tracker {
	c = clock.Or(c)

	return &Tracker{
		clock: c, events: event.Or(events), started: c.Now().UTC(), budgets: make(map[string]*held),
		dirty: make(chan struct{}, 1),
	}
}

// ObserveRate records a budget reading a response carried, and what its request spent.
// The newest reading wins: one from a later window, or from the same window with more
// used, since used only grows within a window. A reading from a window that is already
// over is a late answer and is dropped; one from an earlier window still running belongs
// to another account, as after gh auth switch, and replaces the one held. A reading older
// than the one held still counts what its request spent, if it belongs to the window held.
func (t *Tracker) ObserveRate(reading github.RateReading, spent int) {
	if reading.Resource == "" || reading.Limit <= 0 {
		return
	}

	now := t.clock.Now().UTC()

	t.mu.Lock()
	changed := t.observeLocked(reading, spent, now)
	t.mu.Unlock()

	if changed {
		t.changed()
	}
}

// counted is how many requests a spend stands for: one, unless it spent nothing.
func counted(spent int) int {
	if spent > 0 {
		return 1
	}

	return 0
}

// Seed starts the Tracker from readings kept from an earlier run, for each budget it has
// no reading of yet, so the window shows them until the first response. A seeded budget
// counts nothing the app spent: that was spent in another run.
func (t *Tracker) Seed(stored []Stored) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, s := range stored {
		r := s.Reading
		if r.Resource == "" || r.Limit <= 0 || t.budgets[r.Resource] != nil {
			continue
		}

		// What this run spends from the window counts from its start; what an earlier run
		// spent is not known.
		t.budgets[r.Resource] = &held{reading: r, takenAt: s.TakenAt.UTC(), countedFrom: later(t.started, windowStart(r))}
	}
}

// Refreshed records what a whole inbox refresh spent of the GraphQL budget, in points,
// and how many items it tracked. The inbox calls it after each refresh it stores.
func (t *Tracker) Refreshed(points, items int) {
	now := t.clock.Now().UTC()

	t.mu.Lock()
	t.last = refresh{at: now, points: max(points, 0), items: max(items, 0)}
	t.mu.Unlock()

	t.changed()
}

// changed tells the window, and the Saver, that something changed.
func (t *Tracker) changed() {
	select {
	case t.dirty <- struct{}{}:
	default:
	}

	t.events.Emit(event.BudgetUpdated{})
}

// observeLocked records a reading and its spend, and reports whether anything changed.
// The caller holds mu.
func (t *Tracker) observeLocked(reading github.RateReading, spent int, now time.Time) bool {
	h := t.budgets[reading.Resource]

	switch {
	case h == nil || reading.ResetAt.After(h.reading.ResetAt) ||
		(reading.ResetAt.Before(h.reading.ResetAt) && reading.ResetAt.After(now)):
		// The first reading, the first of a new window, or another account's: counting
		// starts again.
		t.budgets[reading.Resource] = &held{
			reading: reading, takenAt: now, spent: max(spent, 0), requests: counted(spent),
			countedFrom: later(t.started, windowStart(reading)),
		}

		return true
	case reading.ResetAt.Before(h.reading.ResetAt):
		// A late answer from a window already over.
		return false
	}

	changed := spent > 0
	h.spent += max(spent, 0)
	h.requests += counted(spent)

	// A reading as new as the one held is the budget as of now, even when nothing moved.
	if reading.Used >= h.reading.Used {
		changed = changed || reading != h.reading
		h.reading, h.takenAt = reading, now
	}

	return changed
}
