package release

import (
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/event"
)

// progressEvery is how often, at most, a refresh of the list tells the window how far it
// has got: four times a second.
const progressEvery = 250 * time.Millisecond

// listProgress tells the window how far one refresh of the list has got, as
// event.ReleasesProgress: the phase under way and its counts. It tells at most once per
// every; a change that comes sooner waits out the rest of that time, and the changes that
// come meanwhile go with it, so the window always ends up with the latest counts. Once the
// refresh ends, it tells nothing more. It is safe for concurrent use.
type listProgress struct {
	events event.Sink
	every  time.Duration

	mu    sync.Mutex
	now   event.ReleasesProgress
	sent  time.Time
	timer *time.Timer
	ended bool
}

// newListProgress returns the progress of a refresh that tells events at most once per
// every.
func newListProgress(events event.Sink, every time.Duration) *listProgress {
	return &listProgress{events: events, every: every}
}

// start begins phase, with total repositories or clones to count off; 0 counts nothing.
func (p *listProgress) start(phase event.ReleasesPhase, total int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.now = event.ReleasesProgress{Phase: phase, Total: total}
	p.tell()
}

// add counts n more of the phase's total done, never past the total. It is a
// GitHub.ReleaseStatus's answered, so any goroutine may call it.
func (p *listProgress) add(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.now.Done = min(p.now.Done+n, p.now.Total)
	p.tell()
}

// end stops telling: the refresh landed, failed, or was cut short.
func (p *listProgress) end() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.ended = true

	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
}

// tell sends the counts now when every has passed since the last were sent, and otherwise
// once it has, unless that send is already due. The caller holds mu.
func (p *listProgress) tell() {
	if p.ended || p.timer != nil {
		return
	}

	wait := p.every - time.Since(p.sent)
	if p.sent.IsZero() || wait <= 0 {
		p.send()

		return
	}

	p.timer = time.AfterFunc(wait, p.flush)
}

// flush sends the counts held back, unless the refresh ended meanwhile.
func (p *listProgress) flush() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.timer = nil

	if !p.ended {
		p.send()
	}
}

// send tells the window the counts now. The caller holds mu; Emit returns promptly and
// never calls back.
func (p *listProgress) send() {
	p.sent = time.Now()
	p.events.Emit(p.now)
}
