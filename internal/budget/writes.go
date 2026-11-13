package budget

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/textutil"
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
// The chore lane's notification writes (github.ChoreGate: marking a thread done or read)
// are paced apart, in a lane of their own, so chores never use up the edits the user asks
// for, nor edits the chores: at most ChoresPerMinute and ChoresPerHour, and never two
// within ChoreGap, as GitHub asks of a client that writes many times. They keep the same
// reserve, and the same hold: a wait GitHub asked for after an edit or a chore stops both.
//
// Every edit or chore allowed is counted, sent or not, so the pace errs on the side of
// fewer. A refusal says when the next may go, in words and as RetryIn: when several limits
// refuse at once, the one whose wait is longest.

const (
	// WritesPerMinute and WritesPerHour are the most edits the app sends GitHub in a
	// minute and in an hour.
	WritesPerMinute = 30
	WritesPerHour   = 200

	// ChoresPerMinute and ChoresPerHour are the most chores the app sends GitHub in a
	// minute and in an hour, and ChoreGap the least time between two.
	ChoresPerMinute = 50
	ChoresPerHour   = 300
	ChoreGap        = time.Second

	// reserveDivisor sets the reserve at a hundredth of a budget, and minReserve keeps it
	// at least that many points.
	reserveDivisor = 100
	minReserve     = 10

	// writeWindow is how long an edit or a chore counts toward its pace.
	writeWindow = time.Hour
)

var (
	// ErrBudgetReserved is returned for an edit or a chore while its budget is down to the
	// reserve.
	ErrBudgetReserved = errors.New("the API budget is down to the reserve edits leave")

	// ErrWritePace is returned for an edit or a chore past the app's pace.
	ErrWritePace = errors.New("the app made as many edits as it makes in a while")

	// ErrWritesHeld is returned for an edit or a chore while GitHub asked the app to wait.
	ErrWritesHeld = errors.New("GitHub asked the app to wait before its next edit")
)

// refusal is an edit or a chore the Tracker refused, and how long until the next may go.
type refusal struct {
	wait  time.Duration
	cause error
}

// Error returns the cause's text.
func (r *refusal) Error() string {
	return r.cause.Error()
}

// Unwrap returns the cause.
func (r *refusal) Unwrap() error {
	return r.cause
}

// refused is a refusal with msg, a sentence for the user, after which the next may go in
// wait.
func refused(msg string, wait time.Duration, cause error) error {
	return uierr.New(msg, &refusal{wait: max(wait, 0), cause: cause})
}

// RetryIn is how long after err, an edit or a chore AllowWrite or AllowChore refused, the
// next may go; 0 for any other error.
func RetryIn(err error) time.Duration {
	if r, ok := errors.AsType[*refusal](err); ok {
		return r.wait
	}

	return 0
}

// longest returns the refusal of errs whose wait (RetryIn) is longest, the first of those
// alike, or nil when none refused.
func longest(errs ...error) error {
	var out error

	for _, err := range errs {
		if err != nil && (out == nil || RetryIn(err) > RetryIn(out)) {
			out = err
		}
	}

	return out
}

// Reserve is how much of a budget of limit points edits leave for everything else signed
// in as the user.
func Reserve(limit int) int {
	return max(minReserve, limit/reserveDivisor)
}

// lane is one kind of change the Tracker paces, with counts of its own: the edits
// (writeLane) or the chores (choreLane). The reserve and GitHub's holds are the Tracker's,
// shared by both.
type lane struct {
	// sent are when the lane's changes of the last hour were sent, oldest first.
	sent []time.Time

	// perMinute and perHour are the most the lane sends in a minute and in an hour, and
	// gap the least time between two, 0 for none.
	perMinute int
	perHour   int
	gap       time.Duration

	// pace words a refusal for n sent in the last period ("minute" or "hour"), after which
	// the next may go in wait; tooSoon one for a change sent less than gap ago.
	pace    func(n int, period string, wait time.Duration) error
	tooSoon func(wait time.Duration) error
}

// writeLane is the lane of the edits the app makes for the user.
func writeLane() lane {
	return lane{perMinute: WritesPerMinute, perHour: WritesPerHour, pace: paceError}
}

// choreLane is the lane of the chores: slower, and never two within ChoreGap.
func choreLane() lane {
	return lane{
		perMinute: ChoresPerMinute, perHour: ChoresPerHour, gap: ChoreGap, pace: chorePaceError, tooSoon: choreGapError,
	}
}

// prune forgets the times that no longer count as of now: those an hour old, and any
// after now, which a clock set back, or a run whose clock was ahead, left behind, and
// which would otherwise hold the lane shut until then.
func (l *lane) prune(now time.Time) {
	cutoff := now.Add(-writeWindow)

	first := 0
	for first < len(l.sent) && !l.sent[first].After(cutoff) {
		first++
	}

	last := len(l.sent)
	for last > first && l.sent[last-1].After(now) {
		last--
	}

	l.sent = l.sent[first:last]
}

// countSince counts the lane's times after since.
func (l *lane) countSince(since time.Time) int {
	n := 0

	for i := len(l.sent) - 1; i >= 0 && l.sent[i].After(since); i-- {
		n++
	}

	return n
}

// allow decides on one change sent now, given shared, the Tracker's own refusal (a hold,
// the reserve) or nil, and counts it when it may go. A refusal is the limit whose wait is
// longest, so RetryIn says when the next may really go.
func (l *lane) allow(now time.Time, shared error) error {
	l.prune(now)

	if err := longest(shared, l.tooSoonError(now), l.minuteError(now), l.hourError(now)); err != nil {
		return err
	}

	l.sent = append(l.sent, now)

	return nil
}

// tooSoonError refuses a change less than gap after the last; nil otherwise.
func (l *lane) tooSoonError(now time.Time) error {
	n := len(l.sent)
	if l.gap <= 0 || n == 0 || now.Sub(l.sent[n-1]) >= l.gap {
		return nil
	}

	return l.tooSoon(l.sent[n-1].Add(l.gap).Sub(now))
}

// minuteError refuses a change past the minute's pace; nil otherwise. The next may go
// once the perMinute-th newest of the last minute leaves it.
func (l *lane) minuteError(now time.Time) error {
	n := l.countSince(now.Add(-time.Minute))
	if n < l.perMinute {
		return nil
	}

	return l.pace(n, "minute", l.sent[len(l.sent)-l.perMinute].Add(time.Minute).Sub(now))
}

// hourError refuses a change past the hour's pace; nil otherwise.
func (l *lane) hourError(now time.Time) error {
	n := len(l.sent)
	if n < l.perHour {
		return nil
	}

	return l.pace(n, "hour", l.sent[n-l.perHour].Add(writeWindow).Sub(now))
}

// writes reads the lane as of now, with the Tracker's reserve and hold.
func (l *lane) writes(now time.Time, reserve int, heldUntil time.Time) Writes {
	l.prune(now)

	out := Writes{
		LastHour: len(l.sent), LastMinute: l.countSince(now.Add(-time.Minute)),
		PerHour: l.perHour, PerMinute: l.perMinute, Reserve: reserve,
	}

	if now.Before(heldUntil) {
		out.HeldUntil = clock.Format(heldUntil)
	}

	return out
}

// restore takes back the times an earlier run kept, oldest first and each after the
// last, unless the lane sent any in this run. Any after now are pruned as the lane is
// next read.
func (l *lane) restore(kept []time.Time) {
	if len(l.sent) != 0 {
		return
	}

	for _, w := range kept {
		if len(l.sent) == 0 || w.After(l.sent[len(l.sent)-1]) {
			l.sent = append(l.sent, w.UTC())
		}
	}
}

// AllowWrite asks to send one edit now, spending from resource. It counts the edit and
// returns nil when it may go, or returns an error, with a sentence for the user, that
// says why not and when to try again (RetryIn).
func (t *Tracker) AllowWrite(resource string) error {
	return t.allow(resource, &t.writes)
}

// AllowChore asks to send one chore now, spending from resource. It counts the chore and
// returns nil when it may go, or returns an error as AllowWrite does.
func (t *Tracker) AllowChore(resource string) error {
	return t.allow(resource, &t.chores)
}

// allow decides on one change in l, and tells the window of one allowed. The clock is
// read under mu, so each lane's times are appended in order.
func (t *Tracker) allow(resource string, l *lane) error {
	t.mu.Lock()
	now := t.clock.Now().UTC()
	err := l.allow(now, t.sharedRefusalLocked(resource, now))
	t.mu.Unlock()

	if err == nil {
		t.changed()
	}

	return err
}

// HoldWrites refuses edits and chores for wait from now, as GitHub asked. A hold already
// running past then stays as it is.
func (t *Tracker) HoldWrites(wait time.Duration) {
	if wait <= 0 {
		return
	}

	t.mu.Lock()
	until := t.clock.Now().UTC().Add(wait)
	later := until.After(t.heldUntil)

	if later {
		t.heldUntil = until
	}
	t.mu.Unlock()

	if later {
		t.changed()
	}
}

// sharedRefusalLocked refuses an edit or a chore that spends from resource while GitHub
// asked the app to wait, or while the budget is down to the reserve, with the longer wait
// when both do; nil otherwise. The caller holds mu.
func (t *Tracker) sharedRefusalLocked(resource string, now time.Time) error {
	var held, reserved error

	if now.Before(t.heldUntil) {
		wait := t.heldUntil.Sub(now)
		held = refused(
			fmt.Sprintf("GitHub asked %s to slow down its changes. Try again %s.", brand.Name, waitText(wait)),
			wait, ErrWritesHeld,
		)
	}

	if h := t.budgets[resource]; h != nil && h.reading.ResetAt.After(now) {
		if keep := Reserve(h.reading.Limit); h.reading.Remaining <= keep {
			wait := h.reading.ResetAt.Sub(now)
			reserved = refused(
				fmt.Sprintf("Your GitHub API budget is down to its last %d points, which %s leaves for gh and your agents. "+
					"Try again once it refills %s.", max(h.reading.Remaining, 0), brand.Name, waitText(wait)),
				wait, fmt.Errorf("%w: %d of %d %s points left", ErrBudgetReserved, h.reading.Remaining, h.reading.Limit, resource),
			)
		}
	}

	return longest(held, reserved)
}

// reserveLocked is the reserve of the REST budget, which edits and chores spend, as far
// as the Tracker knows it. The caller holds mu.
func (t *Tracker) reserveLocked() int {
	if h := t.budgets[github.ResourceCore]; h != nil {
		return Reserve(h.reading.Limit)
	}

	return Reserve(0)
}

// paceError says the app made n edits in the last period, and when the next may go.
func paceError(n int, period string, wait time.Duration) error {
	return refused(
		fmt.Sprintf("%s made %d changes on GitHub in the last %s, the most it makes to stay clear of GitHub's limits. "+
			"Try again %s.", brand.Name, n, period, waitText(wait)),
		wait, fmt.Errorf("%w: %d in the last %s", ErrWritePace, n, period),
	)
}

// chorePaceError says the app sent n chores in the last period, and when the next may go.
func chorePaceError(n int, period string, wait time.Duration) error {
	return refused(
		fmt.Sprintf("%s cleared %d notifications on GitHub in the last %s, the most it clears in that time. Try again %s.",
			brand.Name, n, period, waitText(wait)),
		wait, fmt.Errorf("%w: %d chores in the last %s", ErrWritePace, n, period),
	)
}

// choreGapError says the app sent a chore less than ChoreGap ago, and when the next may
// go.
func choreGapError(wait time.Duration) error {
	return refused(
		fmt.Sprintf("%s clears at most one notification on GitHub a second. Try again %s.", brand.Name, waitText(wait)),
		wait, fmt.Errorf("%w: a chore less than %s ago", ErrWritePace, ChoreGap),
	)
}

// waitText says when a wait of d ends, in words: "in 40 seconds", "in 12 minutes".
func waitText(d time.Duration) string {
	if seconds := int(math.Ceil(d.Seconds())); seconds < 60 {
		return "in " + textutil.Counted(max(seconds, 1), "second", "seconds")
	}

	return "in " + textutil.Counted(int(math.Ceil(d.Minutes())), "minute", "minutes")
}

// Writes is what the app changed on GitHub lately in one lane, and how it paces that lane:
// the edits it made for the user (Gauge.Writes), or its chores (Gauge.Chores). Every field
// is a plain value.
type Writes struct {
	// LastHour and LastMinute count the changes the app sent in the last hour and minute.
	LastHour   int `json:"lastHour"`
	LastMinute int `json:"lastMinute"`

	// PerHour and PerMinute are the most changes the app sends in an hour and in a minute.
	PerHour   int `json:"perHour"`
	PerMinute int `json:"perMinute"`

	// Reserve is how much of the REST budget, which the changes spend, they leave for
	// everything else signed in as the user.
	Reserve int `json:"reserve"`

	// HeldUntil is when GitHub's request to wait before the next change, which holds both
	// lanes, ends, as RFC 3339, or "" when there is none.
	HeldUntil string `json:"heldUntil"`
}

// restoreWritesLocked takes back the edits, the chores, and the hold an earlier run kept,
// each lane unless this run already sent on it. The caller holds mu.
func (t *Tracker) restoreWritesLocked(writes, chores []time.Time, heldUntil *time.Time) {
	t.writes.restore(writes)
	t.chores.restore(chores)

	if heldUntil != nil && heldUntil.After(t.heldUntil) {
		t.heldUntil = heldUntil.UTC()
	}
}
