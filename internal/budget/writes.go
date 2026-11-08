package budget

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The edits the app makes on GitHub for the user (github.WriteGate) are paced by the same
// Tracker that keeps the budgets, so an edit is refused before it is sent when:
//
//   - its budget is down to the reserve edits leave for everything else signed in as the
//     user (gh, coding agents), until the budget refills;
//   - the app sent WritesPerMinute edits in the last minute, or WritesPerHour in the last
//     hour. Both are well under GitHub's own limits on writes (80 a minute and 500 an hour
//     for requests that create content), which the user's other tools share;
//   - GitHub asked the app to slow down, until the time it gave.
//
// Every edit allowed is counted, sent or not, so the pace errs on the side of fewer.

const (
	// WritesPerMinute and WritesPerHour are the most edits the app sends GitHub in a
	// minute and in an hour.
	WritesPerMinute = 30
	WritesPerHour   = 200

	// reserveDivisor sets the reserve at a hundredth of a budget, and minReserve keeps it
	// at least that many points.
	reserveDivisor = 100
	minReserve     = 10

	// writeWindow is how long an edit counts toward the pace.
	writeWindow = time.Hour
)

var (
	// ErrBudgetReserved is returned for an edit while its budget is down to the reserve.
	ErrBudgetReserved = errors.New("the API budget is down to the reserve edits leave")

	// ErrWritePace is returned for an edit past the app's pace.
	ErrWritePace = errors.New("the app made as many edits as it makes in a while")

	// ErrWritesHeld is returned for an edit while GitHub asked the app to wait.
	ErrWritesHeld = errors.New("GitHub asked the app to wait before its next edit")
)

// Reserve is how much of a budget of limit points edits leave for everything else signed
// in as the user.
func Reserve(limit int) int {
	return max(minReserve, limit/reserveDivisor)
}

// AllowWrite asks to send one edit now, spending from resource. It counts the edit and
// returns nil when it may go, or returns an error, with a sentence for the user, that
// says why not and when to try again.
func (t *Tracker) AllowWrite(resource string) error {
	now := t.clock.Now().UTC()

	t.mu.Lock()
	err := t.allowWriteLocked(resource, now)
	t.mu.Unlock()

	if err == nil {
		t.changed()
	}

	return err
}

// HoldWrites refuses edits for wait from now, as GitHub asked. A hold already running
// past then stays as it is.
func (t *Tracker) HoldWrites(wait time.Duration) {
	if wait <= 0 {
		return
	}

	until := t.clock.Now().UTC().Add(wait)

	t.mu.Lock()
	later := until.After(t.heldUntil)

	if later {
		t.heldUntil = until
	}
	t.mu.Unlock()

	if later {
		t.changed()
	}
}

// allowWriteLocked decides on one edit and counts it when it may go. The caller holds mu.
func (t *Tracker) allowWriteLocked(resource string, now time.Time) error {
	t.pruneWritesLocked(now)

	if now.Before(t.heldUntil) {
		return uierr.New(
			fmt.Sprintf("GitHub asked %s to slow down its changes. Try again %s.", brand.Name, waitText(t.heldUntil.Sub(now))),
			ErrWritesHeld,
		)
	}

	if h := t.budgets[resource]; h != nil && h.reading.ResetAt.After(now) {
		if keep := Reserve(h.reading.Limit); h.reading.Remaining <= keep {
			return uierr.New(
				fmt.Sprintf("Your GitHub API budget is down to its last %d points, which %s leaves for gh and your agents. "+
					"Try again once it refills %s.", max(h.reading.Remaining, 0), brand.Name, waitText(h.reading.ResetAt.Sub(now))),
				fmt.Errorf("%w: %d of %d %s points left", ErrBudgetReserved, h.reading.Remaining, h.reading.Limit, resource),
			)
		}
	}

	if n := t.writesSinceLocked(now.Add(-time.Minute)); n >= WritesPerMinute {
		// The oldest edit of the last minute is the first to leave it.
		return paceError(n, "minute", t.writes[len(t.writes)-n].Add(time.Minute).Sub(now))
	}

	if len(t.writes) >= WritesPerHour {
		return paceError(len(t.writes), "hour", t.writes[0].Add(writeWindow).Sub(now))
	}

	t.writes = append(t.writes, now)

	return nil
}

// paceError says the app made n edits in the last period, and when the next may go.
func paceError(n int, period string, wait time.Duration) error {
	return uierr.New(
		fmt.Sprintf("%s made %d changes on GitHub in the last %s, the most it makes to stay clear of GitHub's limits. "+
			"Try again %s.", brand.Name, n, period, waitText(wait)),
		fmt.Errorf("%w: %d in the last %s", ErrWritePace, n, period),
	)
}

// waitText says when a wait of d ends, in words: "in 40 seconds", "in 12 minutes".
func waitText(d time.Duration) string {
	if seconds := int(math.Ceil(d.Seconds())); seconds < 60 {
		return "in " + plural(max(seconds, 1), "second")
	}

	return "in " + plural(int(math.Ceil(d.Minutes())), "minute")
}

// plural writes n of a unit, such as "1 minute" or "5 minutes".
func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}

	return fmt.Sprintf("%d %ss", n, unit)
}

// pruneWritesLocked forgets the edits that no longer count toward the pace. The caller
// holds mu.
func (t *Tracker) pruneWritesLocked(now time.Time) {
	cutoff := now.Add(-writeWindow)

	i := 0
	for i < len(t.writes) && !t.writes[i].After(cutoff) {
		i++
	}

	t.writes = t.writes[i:]
}

// writesSinceLocked counts the edits sent after since. The caller holds mu.
func (t *Tracker) writesSinceLocked(since time.Time) int {
	n := 0

	for i := len(t.writes) - 1; i >= 0 && t.writes[i].After(since); i-- {
		n++
	}

	return n
}

// Writes is what the app changed on GitHub for the user lately, and how it paces its
// edits. Every field is a plain value.
type Writes struct {
	// LastHour and LastMinute count the edits the app sent in the last hour and minute.
	LastHour   int `json:"lastHour"`
	LastMinute int `json:"lastMinute"`

	// PerHour and PerMinute are the most edits the app sends in an hour and in a minute.
	PerHour   int `json:"perHour"`
	PerMinute int `json:"perMinute"`

	// Reserve is how much of the REST budget, which edits spend, they leave for
	// everything else signed in as the user.
	Reserve int `json:"reserve"`

	// HeldUntil is when GitHub's request to wait before the next edit ends, as RFC 3339,
	// or "" when there is none.
	HeldUntil string `json:"heldUntil"`
}

// writesLocked reads the app's edits as of now. The caller holds mu.
func (t *Tracker) writesLocked(now time.Time) Writes {
	t.pruneWritesLocked(now)

	out := Writes{
		LastHour: len(t.writes), LastMinute: t.writesSinceLocked(now.Add(-time.Minute)),
		PerHour: WritesPerHour, PerMinute: WritesPerMinute, Reserve: Reserve(0),
	}

	if h := t.budgets[github.ResourceCore]; h != nil {
		out.Reserve = Reserve(h.reading.Limit)
	}

	if now.Before(t.heldUntil) {
		out.HeldUntil = clock.Format(t.heldUntil)
	}

	return out
}

// restoreWritesLocked takes back the edits and the hold an earlier run kept, unless this
// run already sent one. The caller holds mu.
func (t *Tracker) restoreWritesLocked(writes []time.Time, heldUntil *time.Time) {
	if len(t.writes) == 0 {
		for _, w := range writes {
			if len(t.writes) == 0 || w.After(t.writes[len(t.writes)-1]) {
				t.writes = append(t.writes, w.UTC())
			}
		}
	}

	if heldUntil != nil && heldUntil.After(t.heldUntil) {
		t.heldUntil = heldUntil.UTC()
	}
}
