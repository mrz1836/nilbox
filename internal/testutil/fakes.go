package testutil

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"
)

// ErrNotFound is what Finder fails with for a name it does not resolve.
var ErrNotFound = errors.New("testutil: program not found")

// Runner is a fake execpath.Runner: it records each argv it is asked to run, runs
// nothing, and answers with Stdout, Stderr, and Err, or with Script when that is set. It
// is safe for concurrent use.
type Runner struct {
	// Stdout, Stderr, and Err answer every call when Script is nil.
	Stdout string
	Stderr string
	Err    error

	// Script, when set, answers each call instead. It gets the call's context, so it can
	// check a deadline, and the argv: the program name followed by its arguments. It
	// runs outside the Runner's lock, so it guards any state of its own.
	Script func(ctx context.Context, argv []string) (stdout, stderr []byte, err error)

	mu    sync.Mutex
	calls [][]string
}

// Run records the argv and answers it.
func (r *Runner) Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	argv := append([]string{name}, args...)

	r.mu.Lock()
	r.calls = append(r.calls, argv)
	r.mu.Unlock()

	if r.Script != nil {
		return r.Script(ctx, slices.Clone(argv))
	}

	return []byte(r.Stdout), []byte(r.Stderr), r.Err
}

// Calls returns a copy of every argv run so far, in order.
func (r *Runner) Calls() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()

	calls := make([][]string, 0, len(r.calls))
	for _, argv := range r.calls {
		calls = append(calls, slices.Clone(argv))
	}

	return calls
}

// Finder is a fake program finder: it resolves names without looking at the disk.
type Finder struct {
	// Path is what every name resolves to when Paths is nil.
	Path string

	// Paths, when set, maps the only names Finder resolves to their paths; any other
	// name fails with ErrNotFound.
	Paths map[string]string

	// Err, when set, is what every lookup fails with.
	Err error
}

// Find resolves name.
func (f Finder) Find(name string) (string, error) {
	switch {
	case f.Err != nil:
		return "", f.Err
	case f.Paths == nil:
		return f.Path, nil
	}

	if path, ok := f.Paths[name]; ok {
		return path, nil
	}

	return "", ErrNotFound
}

// Clock is a fake clock that is safe for concurrent use. It stands still until the test
// moves it, unless it was made with a step, when every read moves it on by that step.
type Clock struct {
	mu   sync.Mutex
	now  time.Time
	step time.Duration
}

// NewClock returns a Clock that reads now until the test moves it.
func NewClock(now time.Time) *Clock {
	return &Clock{now: now}
}

// NewSteppingClock returns a Clock whose first read is start and whose every read after
// is step later than the one before.
func NewSteppingClock(start time.Time, step time.Duration) *Clock {
	return &Clock{now: start, step: step}
}

// Now returns the clock's time, then moves the clock on by its step.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now
	c.now = c.now.Add(c.step)

	return now
}

// Set moves the clock to now.
func (c *Clock) Set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = now
}

// Advance moves the clock on by d and returns the new time.
func (c *Clock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)

	return c.now
}

// Notifier counts the change notifications a service sends. It answers to the notifier
// interface of each service that has one, so one type serves them all. It is safe for
// concurrent use.
type Notifier struct {
	mu    sync.Mutex
	calls int
}

// InboxUpdated counts a notification from the inbox.
func (n *Notifier) InboxUpdated() { n.count() }

// NotificationsUpdated counts a notification from the notification log.
func (n *Notifier) NotificationsUpdated() { n.count() }

// WorktreesUpdated counts a notification from the worktree service.
func (n *Notifier) WorktreesUpdated() { n.count() }

// Calls returns the number of notifications so far.
func (n *Notifier) Calls() int {
	n.mu.Lock()
	defer n.mu.Unlock()

	return n.calls
}

// count records one notification.
func (n *Notifier) count() {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.calls++
}
