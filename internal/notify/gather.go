package notify

import (
	"context"
	"sync"
	"time"
)

// GatherWindow is how long Gather holds the banner of a notification for those of others
// that arrive meanwhile: a burst, as Install all's failed runs or the runs a restart
// settles, then shows as one batch.
const GatherWindow = 2 * time.Second

// gathering holds the banners of notifications recorded one at a time, from the first for
// window, and then shows them together, so more than MaxBanners at once sum up in one, as
// one refresh's arrivals do. It is safe for concurrent use.
type gathering struct {
	window time.Duration
	show   func(ctx context.Context, rows []Notification)

	// life is the gathering's lifetime, which every batch shows within, and end ends it:
	// stop does, so a batch still showing at quit is cut short.
	life context.Context //nolint:containedctx // the gathering's lifetime, which outlives every call
	end  context.CancelFunc

	// showing holds a token while a batch shows, so stop returns only once a showing the
	// timer began has ended.
	showing chan struct{}

	// mu guards rows, timer, and stopped.
	mu      sync.Mutex
	rows    []Notification
	timer   *time.Timer
	stopped bool
}

// newGathering returns a gathering that shows each batch with show, GatherWindow after the
// first banner it held.
func newGathering(show func(ctx context.Context, rows []Notification)) *gathering {
	life, end := context.WithCancel(context.Background())

	return &gathering{window: GatherWindow, show: show, life: life, end: end, showing: make(chan struct{}, 1)}
}

// add holds row's banner until the window that the first held banner began ends. Once
// stopped, it holds nothing: the banner isn't shown, as the history still has its entry.
func (g *gathering) add(row Notification) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.stopped {
		return
	}

	g.rows = append(g.rows, row)

	if g.timer == nil {
		g.timer = time.AfterFunc(g.window, g.flush)
	}
}

// flush shows the banners held, as one batch, and holds none after. Only the timer calls
// it, with no caller's context to carry: the batch shows within the gathering's lifetime.
func (g *gathering) flush() {
	g.showing <- struct{}{}
	defer func() { <-g.showing }()

	g.mu.Lock()
	rows := g.rows
	g.rows, g.timer = nil, nil
	g.mu.Unlock()

	if len(rows) > 0 {
		g.show(g.life, rows)
	}
}

// stop drops the banners held, shows none after, cuts short a batch showing by ending the
// gathering's lifetime, and returns once that showing has ended, so nothing reads the
// database or posts a banner after it.
func (g *gathering) stop() {
	g.mu.Lock()
	g.stopped = true
	g.rows = nil

	if g.timer != nil {
		g.timer.Stop()
		g.timer = nil
	}
	g.mu.Unlock()

	g.end()

	// Wait for a batch showing to end.
	g.showing <- struct{}{}

	<-g.showing
}
