package inbox

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitTimeout bounds every wait in these tests.
const waitTimeout = 5 * time.Second

// fakeTicker is a ticker driven by the test.
type fakeTicker struct {
	ch       chan time.Time
	interval time.Duration
	stopped  atomic.Bool
}

func (f *fakeTicker) C() <-chan time.Time { return f.ch }
func (f *fakeTicker) Stop()               { f.stopped.Store(true) }

// tick delivers one tick; it returns once the scheduler has received it.
func (f *fakeTicker) tick(t *testing.T) {
	t.Helper()

	select {
	case f.ch <- time.Time{}:
	case <-time.After(waitTimeout):
		t.Fatal("the scheduler did not take the tick")
	}
}

// tickUntil keeps ticking until r has run at least want refreshes. A tick that lands
// while the previous refresh is finishing is skipped, so one tick is not always enough.
func tickUntil(t *testing.T, ticker *fakeTicker, r *fakeRefresher, want int32) {
	t.Helper()

	require.Eventually(t, func() bool {
		select {
		case ticker.ch <- time.Time{}:
		default:
		}

		return r.count.Load() >= want
	}, waitTimeout, time.Millisecond)
}

// fakeRefresher reports each call on calls. Each call runs the next behavior, if any.
type fakeRefresher struct {
	calls    chan struct{}
	count    atomic.Int32
	mu       sync.Mutex
	behavior []func(ctx context.Context) error
}

func newFakeRefresher(behavior ...func(ctx context.Context) error) *fakeRefresher {
	return &fakeRefresher{calls: make(chan struct{}, 16), behavior: behavior}
}

func (f *fakeRefresher) Refresh(ctx context.Context) (Snapshot, error) {
	f.count.Add(1)

	f.calls <- struct{}{}

	f.mu.Lock()

	var next func(context.Context) error
	if len(f.behavior) > 0 {
		next, f.behavior = f.behavior[0], f.behavior[1:]
	}

	f.mu.Unlock()

	if next == nil {
		return Snapshot{}, nil
	}

	return Snapshot{}, next(ctx)
}

// waitCall waits for the next refresh to start.
func (f *fakeRefresher) waitCall(t *testing.T) {
	t.Helper()

	select {
	case <-f.calls:
	case <-time.After(waitTimeout):
		t.Fatal("no refresh started")
	}
}

// syncBuffer is a log sink safe for concurrent writers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// startScheduler runs a scheduler over r with a fake ticker until the test ends or stop
// is called. stop waits for Run to return.
func startScheduler(t *testing.T, r Refresher, logs *syncBuffer) (ticker *fakeTicker, stop func()) {
	t.Helper()

	ticker = &fakeTicker{ch: make(chan time.Time)}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	s := NewScheduler(r,
		WithInterval(time.Minute),
		WithLogger(logger),
		WithTicker(func(d time.Duration) Ticker {
			ticker.interval = d

			return ticker
		}),
	)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		s.Run(ctx)
	}()

	stop = func() {
		cancel()

		select {
		case <-done:
		case <-time.After(waitTimeout):
			t.Fatal("the scheduler did not stop")
		}
	}

	t.Cleanup(stop)

	return ticker, stop
}

func TestSchedulerTicks(t *testing.T) {
	r := newFakeRefresher()
	ticker, stop := startScheduler(t, r, &syncBuffer{})

	r.waitCall(t) // on start
	assert.Equal(t, time.Minute, ticker.interval)

	tickUntil(t, ticker, r, 2)
	tickUntil(t, ticker, r, 3)

	stop()
	assert.True(t, ticker.stopped.Load())
	assert.GreaterOrEqual(t, r.count.Load(), int32(3))
}

func TestSchedulerSkipsWhileRunning(t *testing.T) {
	release := make(chan struct{})
	r := newFakeRefresher(func(context.Context) error {
		<-release

		return nil
	})

	logs := &syncBuffer{}
	ticker, stop := startScheduler(t, r, logs)

	r.waitCall(t) // the first refresh is now blocked

	// After the second tick is taken, the first tick has been fully handled.
	ticker.tick(t)
	ticker.tick(t)
	assert.Equal(t, int32(1), r.count.Load(), "ticks during a refresh are skipped")
	assert.Contains(t, logs.String(), "inbox refresh skipped")

	close(release)
	tickUntil(t, ticker, r, 2)
	stop()
}

func TestSchedulerStopsOnCancel(t *testing.T) {
	sawCancel := make(chan struct{})
	r := newFakeRefresher(func(ctx context.Context) error {
		<-ctx.Done()
		close(sawCancel)

		return ctx.Err()
	})

	logs := &syncBuffer{}
	ticker, stop := startScheduler(t, r, logs)

	r.waitCall(t)
	stop() // returns only after the in-flight refresh saw the cancel

	select {
	case <-sawCancel:
	default:
		t.Fatal("Run returned before the in-flight refresh finished")
	}

	assert.True(t, ticker.stopped.Load())
	assert.NotContains(t, logs.String(), "inbox refresh failed", "a canceled refresh is not a failure")
}

func TestSchedulerSurvivesErrorsAndPanics(t *testing.T) {
	r := newFakeRefresher(
		func(context.Context) error { return errFakeBroken },
		func(context.Context) error { panic("refresh exploded") },
	)

	logs := &syncBuffer{}
	ticker, stop := startScheduler(t, r, logs)

	r.waitCall(t)

	tickUntil(t, ticker, r, 2)
	tickUntil(t, ticker, r, 3)

	// The third refresh only starts once the panicking one has returned and logged.
	stop()

	out := logs.String()
	assert.Contains(t, out, "inbox refresh failed")
	assert.Contains(t, out, errFakeBroken.Error())
	assert.Contains(t, out, "inbox refresh panicked")
	assert.GreaterOrEqual(t, r.count.Load(), int32(3), "the scheduler kept going after both")
}

func TestNewSchedulerDefaults(t *testing.T) {
	s := NewScheduler(newFakeRefresher(), WithInterval(0))

	assert.Equal(t, DefaultInterval, s.interval, "a non-positive interval is ignored")
	assert.Equal(t, 5*time.Minute, DefaultInterval)
	assert.NotNil(t, s.logger)

	ticker := s.newTicker(time.Hour)
	assert.NotNil(t, ticker.C())
	ticker.Stop()
}
