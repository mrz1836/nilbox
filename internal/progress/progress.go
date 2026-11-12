// Package progress tells the window how far a long read has got while it runs: the phase
// under way and how many of the phase's things are done, as an event the caller builds,
// at most a few times a second. The Release page's list and the worktrees' listing tell
// theirs through it.
package progress

import (
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/event"
)

// Every is how often, at most, a Tracker tells the window: four times a second.
const Every = 250 * time.Millisecond

// Build makes the event that tells the window phase, with done of total counted off.
type Build[P ~string] func(phase P, done, total int) event.Event

// Tracker tells how far one read has got, as the event its Build makes of the phase under
// way and its counts. It tells at most once per every; a change that comes sooner waits
// out the rest of that time, and the changes that come meanwhile go with it, so the
// window always ends up with the latest counts. Once the read ends, it tells nothing more.
// A nil Tracker tells nothing, so a read nobody watches passes none. It is safe for
// concurrent use.
type Tracker[P ~string] struct {
	events event.Sink
	every  time.Duration
	build  Build[P]

	mu    sync.Mutex
	phase P
	done  int
	total int
	sent  time.Time
	timer *time.Timer
	ended bool
}

// New returns the Tracker of a read that tells events at most once per every.
func New[P ~string](events event.Sink, every time.Duration, build Build[P]) *Tracker[P] {
	return &Tracker[P]{events: event.Or(events), every: every, build: build}
}

// Start begins phase, with total things to count off; 0 counts nothing.
func (t *Tracker[P]) Start(phase P, total int) {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.phase, t.done, t.total = phase, 0, total
	t.tell()
}

// Add counts n more of the phase's total done, never past the total. Any goroutine may
// call it.
func (t *Tracker[P]) Add(n int) {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.done = min(t.done+n, t.total)
	t.tell()
}

// End stops telling: the read landed, failed, or was cut short.
func (t *Tracker[P]) End() {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.ended = true

	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
}

// tell sends the counts now when every has passed since the last were sent, and otherwise
// once it has, unless that send is already due. The caller holds mu.
func (t *Tracker[P]) tell() {
	if t.ended || t.timer != nil {
		return
	}

	wait := t.every - time.Since(t.sent)
	if t.sent.IsZero() || wait <= 0 {
		t.send()

		return
	}

	t.timer = time.AfterFunc(wait, t.flush)
}

// flush sends the counts held back, unless the read ended meanwhile.
func (t *Tracker[P]) flush() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.timer = nil

	if !t.ended {
		t.send()
	}
}

// send tells the window the counts now. The caller holds mu; Emit returns promptly and
// never calls back.
func (t *Tracker[P]) send() {
	t.sent = time.Now()
	t.events.Emit(t.build(t.phase, t.done, t.total))
}
