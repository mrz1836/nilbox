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
// GitHub sometimes counts one budget in two hourly windows at once, each with its own
// reset and its own count, and its responses report one or the other by turns, the rate
// limit endpoint included. So the Tracker holds each window a response reported, with the
// app's own spend in it, and shows the one with the least left (see shownLocked); a
// response never takes another window's place, nor clears what the app spent in it.
//
// The same Tracker paces the edits the app makes on GitHub for the user (github.WriteGate,
// see writes.go): an edit is refused before it is sent when its budget is down to the
// reserve left for everything else, when the app has made as many as it makes in a minute
// or an hour, or while GitHub has asked it to wait. It paces the chore lane's notification
// writes (github.ChoreGate) the same way, more slowly, and with counts of their own, and a
// bulk mark's writes in a lane of their own, slower still.
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

// unseenFor is how long a window of a budget can go unreported, while another of the same
// budget is reported, before the Tracker stops showing it. Two windows GitHub counts at once
// are each reported every few responses; a window no response reported for this long is
// an account's the token no longer belongs to, as after gh auth switch.
const unseenFor = 15 * time.Minute

// maxWindows is the most windows of one budget the Tracker holds: the two GitHub has been
// seen counting at once, and the next hour's.
const maxWindows = 3

// Stored is a reading kept from an earlier run, which the Tracker starts from until a
// response says otherwise.
type Stored struct {
	Reading github.RateReading

	// TakenAt is when it was read.
	TakenAt time.Time
}

// held is the newest reading of one window of a budget, and what the app spent from it.
type held struct {
	reading github.RateReading
	takenAt time.Time

	// spent and requests count what the app spent from the budget, and the requests that
	// did, since countedFrom, in the window that ends at reading.ResetAt. countedFrom is
	// the window's start when the app was running by then.
	spent       int
	requests    int
	countedFrom time.Time

	// purposes splits spent by what each request was sent for (github.Purpose), so the
	// shares add up to spent.
	purposes map[github.Purpose]int
}

// spend counts what one request, sent for purpose, spent from the budget.
func (h *held) spend(spent int, purpose github.Purpose) {
	if spent <= 0 {
		return
	}

	h.spent += spent
	h.requests++

	if h.purposes == nil {
		h.purposes = make(map[github.Purpose]int)
	}

	h.purposes[purpose] += spent
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

// Tracker holds the newest reading of each budget's windows, what the app spent from each
// since it last refilled, and the last refresh's cost. It is safe for concurrent use.
type Tracker struct {
	clock   clock.Clock
	events  event.Sink
	started time.Time

	mu sync.Mutex
	// budgets holds each budget's windows, by resource; a budget never read has no entry,
	// never an empty list.
	budgets map[string][]*held
	last    refresh

	// writes, chores, and bulk are the lanes of the edits, the chores, and a bulk mark's
	// writes, each with the times it sent its changes of the last hour, and heldUntil when
	// GitHub's request to wait before the next of any ends (see writes.go).
	writes    lane
	chores    lane
	bulk      lane
	heldUntil time.Time

	// dirty holds a signal while a change waits to be saved (see Saver).
	dirty chan struct{}
}

// NewTracker returns a Tracker that stamps readings from c, the system clock when nil, and
// emits an event.BudgetUpdated into events, if any, on every change.
func NewTracker(c clock.Clock, events event.Sink) *Tracker {
	c = clock.Or(c)

	return &Tracker{
		clock: c, events: event.Or(events), started: c.Now().UTC(), budgets: make(map[string][]*held),
		writes: writeLane(), chores: choreLane(), bulk: bulkLane(), dirty: make(chan struct{}, 1),
	}
}

// ObserveRate records a budget reading a response carried, and what its request spent, in
// the window it reports, which its reset names. Within a window the newest reading wins,
// the one with the most used, since used only grows within a window, and a reading older
// than the one held still counts what its request spent. A reading of a window not held
// yet adds it, unless that window is already over, which makes it a late answer, dropped.
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

// Seed starts the Tracker from readings kept from an earlier run, for each budget it has
// no reading of yet, so the window shows them until the first response. A seeded budget
// counts nothing the app spent: that was spent in another run.
func (t *Tracker) Seed(stored []Stored) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, s := range stored {
		r := s.Reading
		if r.Resource == "" || r.Limit <= 0 || len(t.budgets[r.Resource]) > 0 {
			continue
		}

		// What this run spends from the window counts from its start; what an earlier run
		// spent is not known.
		t.budgets[r.Resource] = []*held{{reading: r, takenAt: s.TakenAt.UTC(), countedFrom: later(t.started, windowStart(r))}}
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
	// What the request was for counts its spend, and is no part of the budget's reading.
	purpose := reading.Purpose
	reading.Purpose = github.PurposeOther

	windows := t.budgets[reading.Resource]

	h := windowAt(windows, reading.ResetAt)
	if h == nil {
		if len(windows) > 0 && !reading.ResetAt.After(now) {
			// A late answer from a window already over.
			return false
		}

		// The first reading of a window: the budget's first, the next hour's, another
		// window GitHub counts at once, or another account's. What the app spends in it
		// counts from its start, or from when the app began counting.
		h = &held{reading: reading, takenAt: now, countedFrom: later(t.started, windowStart(reading))}
		h.spend(spent, purpose)
		t.budgets[reading.Resource] = append(roomFor(windows, now), h)

		return true
	}

	changed := spent > 0
	h.spend(spent, purpose)

	// A reading as new as the one held is the window as of now, even when nothing moved.
	if reading.Used >= h.reading.Used {
		changed = changed || reading != h.reading
		h.reading, h.takenAt = reading, now
	}

	return changed
}

// windowAt returns the window of windows that refills at resetAt, or nil.
func windowAt(windows []*held, resetAt time.Time) *held {
	for _, h := range windows {
		if h.reading.ResetAt.Equal(resetAt) {
			return h
		}
	}

	return nil
}

// roomFor makes room in windows for one more: it lets go of every window over at now, then,
// while maxWindows are still held, of the one reported longest ago. It returns a new list.
func roomFor(windows []*held, now time.Time) []*held {
	out := make([]*held, 0, len(windows)+1)

	for _, h := range windows {
		if h.reading.ResetAt.After(now) {
			out = append(out, h)
		}
	}

	for len(out) >= maxWindows {
		oldest := 0
		for i, h := range out {
			if h.takenAt.Before(out[oldest].takenAt) {
				oldest = i
			}
		}

		out = append(out[:oldest], out[oldest+1:]...)
	}

	return out
}

// shownLocked returns the window of resource the gauge shows and the edits' reserve
// checks, or nil when none was read. Of the windows reported within unseenFor of the
// budget's newest report, it is the one still running with the least left, since a
// request may count against any of them; when none is still running, the one that
// refilled last. The caller holds mu.
func (t *Tracker) shownLocked(resource string, now time.Time) *held {
	windows := t.budgets[resource]
	if len(windows) == 0 {
		return nil
	}

	since := newestTaken(windows).Add(-unseenFor)

	var shown *held

	for _, h := range windows {
		if !h.takenAt.Before(since) && showsOver(h, shown, now) {
			shown = h
		}
	}

	return shown
}

// showsOver reports whether the gauge shows h rather than shown, nil when none was chosen
// yet: a window still running over one that refilled, then the one with less left, then
// the one that refills later.
func showsOver(h, shown *held, now time.Time) bool {
	if shown == nil {
		return true
	}

	running, shownRunning := h.reading.ResetAt.After(now), shown.reading.ResetAt.After(now)

	switch {
	case running != shownRunning:
		return running
	case running && h.reading.Remaining != shown.reading.Remaining:
		return h.reading.Remaining < shown.reading.Remaining
	default:
		return h.reading.ResetAt.After(shown.reading.ResetAt)
	}
}

// newestTaken returns when the newest reading of windows was taken.
func newestTaken(windows []*held) time.Time {
	var newest time.Time

	for _, h := range windows {
		newest = later(newest, h.takenAt)
	}

	return newest
}
