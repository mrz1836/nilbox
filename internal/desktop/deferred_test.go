package desktop_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/event"
)

// fakeDeferred is deferred work: it records starting, works for work or until its
// context ends, takes linger more when the context ended it, and records returning.
type fakeDeferred struct {
	name   string
	log    *journal
	work   time.Duration
	linger time.Duration
}

func (d *fakeDeferred) Run(ctx context.Context) {
	d.log.add("%s started", d.name)

	select {
	case <-time.After(d.work):
	case <-ctx.Done():
		time.Sleep(d.linger)
	}

	d.log.add("%s returned", d.name)
}

// withDeferred adds deferred workers to a Config.
func withDeferred(ws ...desktop.Worker) func(*desktop.Config) {
	return func(c *desktop.Config) { c.Deferred = ws }
}

func TestDeferredStartsAtTheFirstPaint(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		first := &fakeDeferred{name: "shellenv", log: r.log, work: time.Second}
		second := &fakeDeferred{name: "detection", log: r.log, work: time.Second}
		s := r.shell(withDeferred(first, second))

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()
		assert.Empty(t, r.lines("shellenv"), "nothing deferred runs before the first paint")

		s.Emit(event.FirstPaint{})
		synctest.Wait()
		assert.Equal(t, []string{"shellenv started"}, r.lines("shellenv"))
		assert.Equal(t, []string{"detection started"}, r.lines("detection"))

		time.Sleep(time.Second)
		synctest.Wait()
		assert.Equal(t, []string{"shellenv started", "shellenv returned"}, r.lines("shellenv"))

		require.NoError(t, s.Stop())
	})
}

func TestDeferredStartsAfterTheFallbackWithoutAPaint(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell(withDeferred(&fakeDeferred{name: "shellenv", log: r.log, work: time.Second}))

		s.Start(t.Context())
		s.Launched()

		time.Sleep(desktop.DeferredFallback - time.Millisecond)
		synctest.Wait()
		assert.Empty(t, r.lines("shellenv"), "the fallback waits its full time for a first paint")

		time.Sleep(time.Millisecond)
		synctest.Wait()
		assert.Equal(t, []string{"shellenv started"}, r.lines("shellenv"))

		require.NoError(t, s.Stop())
	})
}

func TestDeferredStartsOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell(withDeferred(&fakeDeferred{name: "shellenv", log: r.log, work: time.Second}))

		s.Start(t.Context())
		s.Launched()
		s.Launched()
		s.Emit(event.FirstPaint{})
		s.Emit(event.FirstPaint{})

		time.Sleep(2 * desktop.DeferredFallback)
		synctest.Wait()

		assert.Equal(t, []string{"shellenv started", "shellenv returned"}, r.lines("shellenv"),
			"a second paint, a second launch, and the fallback start nothing more")

		require.NoError(t, s.Stop())
	})
}

func TestDeferredNeverRunsInAMeasurement(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell(withDeferred(&fakeDeferred{name: "shellenv", log: r.log, work: time.Second}),
			func(c *desktop.Config) { c.MeasureStartup = true })

		s.Start(t.Context())
		s.Launched()
		s.Emit(event.FirstPaint{})

		time.Sleep(2 * desktop.DeferredFallback)
		synctest.Wait()

		assert.Empty(t, r.lines("shellenv"), "a startup measurement runs nothing deferred")
		assert.Equal(t, 1, r.app.quitCount(), "the measurement still ends at the first paint")

		require.NoError(t, s.Stop())
	})
}

func TestStopWaitsForDeferredWork(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell(withDeferred(&fakeDeferred{name: "shellenv", log: r.log, work: time.Hour, linger: time.Second}))

		s.Start(t.Context())
		s.Launched()
		s.Emit(event.FirstPaint{})
		synctest.Wait()

		require.NoError(t, s.Stop())

		assert.Equal(t, []string{"shellenv started", "shellenv returned", "data closed"},
			append(r.lines("shellenv"), r.lines("data")...), "the data closes after the deferred work returned")
	})
}

func TestDeferredDoesNotStartOnceStopping(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell(withDeferred(&fakeDeferred{name: "shellenv", log: r.log, work: time.Second}))

		s.Start(t.Context())
		s.Launched()
		require.NoError(t, s.Stop())

		s.Emit(event.FirstPaint{})
		time.Sleep(2 * desktop.DeferredFallback)
		synctest.Wait()

		assert.Empty(t, r.lines("shellenv"), "neither a late paint nor the stopped fallback starts it")
	})
}

func TestWithoutDeferredWorkNothingIsArmed(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell()

		s.Start(t.Context())
		s.Launched()
		s.Emit(event.FirstPaint{})
		synctest.Wait()

		require.NoError(t, s.Stop())
	})
}
