package inbox

import (
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
)

// A whole refresh that fails for a reason a later try may well not meet
// (github.Retryable: GitHub answering a 500 for a minute, or not answering at all) is
// tried again soon rather than at the scheduler's next interval: retryFirst after it,
// then twice as long after each failure in a row, up to retryMost, and never sooner than
// GitHub asked (github.RetryAfter). The Scheduler, woken as each try is planned, starts
// it, whoever ran the refresh that failed (see Scheduler.due); the window says a try is
// planned and when (Snapshot.Retry) in place of the error, which needs nothing of the
// user. A refresh that succeeds ends the plan, and so does one that fails for any other
// reason, as a refused token: the window shows that error, and the scheduler goes back to
// its interval. The plan lives while the app runs, never stored: the app's start
// refreshes at once anyway.
const (
	// retryFirst is how long after the first failure the refresh is tried again.
	retryFirst = 15 * time.Second

	// retryMost is the longest the plan waits between tries, unless GitHub asked for
	// longer: the scheduler's default interval, so a refresh is tried at least as often as
	// it would be had it succeeded (the scheduler never waits past the user's own
	// interval either).
	retryMost = DefaultInterval
)

// RefreshRetry is the next try of a whole refresh that failed for a reason a later try
// may well not meet. It is the Service's own state, never kept with a snapshot.
type RefreshRetry struct {
	// At is the RFC 3339 time the inbox tries the refresh again, "" while no try is
	// planned: before any refresh failed so since the app started, and after one that
	// succeeded or failed for a reason only the user can change.
	At string `json:"at"`

	// Failures counts the refreshes that failed so in a row, the last included.
	Failures int `json:"failures"`
}

// retries keeps the plan: how many refreshes failed in a row for a reason a later try
// may well not meet, when the next try is due (zero while none is), and what the
// scheduler gave to be woken by as a try is planned.
type retries struct {
	mu       sync.Mutex
	failures int
	at       time.Time
	wake     func()
}

// ended records how a whole refresh ended at now. One that failed for a reason a later
// try may well not meet plans the next try and wakes the scheduler, outside the lock; one
// that succeeded (err nil), or failed for any other reason, ends the plan.
func (r *retries) ended(now time.Time, err error) {
	r.mu.Lock()

	if !github.Retryable(err) {
		r.failures, r.at = 0, time.Time{}
		r.mu.Unlock()

		return
	}

	r.failures++
	r.at = now.Add(retryWait(r.failures, github.RetryAfter(err)))
	wake := r.wake

	r.mu.Unlock()

	if wake != nil {
		wake()
	}
}

// next is when the next try is due, zero while none is planned.
func (r *retries) next() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.at
}

// state is the plan as a snapshot says it.
func (r *retries) state() RefreshRetry {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.at.IsZero() {
		return RefreshRetry{}
	}

	return RefreshRetry{At: clock.Format(r.at), Failures: r.failures}
}

// onPlanned has wake called each time a try is planned from then on.
func (r *retries) onPlanned(wake func()) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.wake = wake
}

// retryWait is how long to wait before trying again a refresh that failed failures times
// in a row: retryFirst after the first, twice as long after each one more, up to
// retryMost, and never less than GitHub asked for (after).
func retryWait(failures int, after time.Duration) time.Duration {
	wait := retryFirst

	for range max(failures-1, 0) {
		if wait >= retryMost {
			break
		}

		wait *= 2
	}

	return max(min(wait, retryMost), after)
}
