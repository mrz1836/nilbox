package update_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/update"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

// runWorker runs the rig's Worker inside the caller's synctest bubble and waits until it
// has settled; stop ends it and waits for it to return.
func runWorker(t *testing.T, r *rig) (w *update.Worker, stop func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	w = update.NewWorker(r.engine)
	done := make(chan struct{})

	go func() {
		defer close(done)

		w.Run(ctx)
	}()

	synctest.Wait()

	return w, func() {
		cancel()
		<-done
	}
}

// after moves the bubble's clock on by d and waits until everything has settled.
func after(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func TestTheFirstCheckIsAMinuteIn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		r.release(t, next)

		_, stop := runWorker(t, r)
		defer stop()

		after(update.FirstCheckAfter - time.Second)
		assert.Zero(t, r.feed.manifests.Load(), "never on the cold start")

		after(time.Second)
		assert.Equal(t, int32(1), r.feed.manifests.Load())
		assert.Equal(t, update.StateReady, r.status(t).State)
	})
}

func TestChecksComeEverySixHoursByTheWallClock(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)

		_, stop := runWorker(t, r)
		defer stop()

		after(update.FirstCheckAfter)
		require.Equal(t, int32(1), r.feed.manifests.Load())

		after(update.CheckEvery - update.TickEvery)
		assert.Equal(t, int32(1), r.feed.manifests.Load(), "no recheck before six hours")

		after(update.TickEvery)
		assert.Equal(t, int32(2), r.feed.manifests.Load(), "a recheck once six hours passed")

		// A Mac that slept: the wall clock jumps six hours while the timers stand still, and
		// the next tick checks.
		r.clock.sleep(update.CheckEvery)
		after(update.TickEvery)
		assert.Equal(t, int32(3), r.feed.manifests.Load(), "a recheck after a sleep, at the next tick")
	})
}

func TestDontCheckMakesNoTimedChecks(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		r.mode(updateprefs.ModeOff)
		r.release(t, next)

		_, stop := runWorker(t, r)
		defer stop()

		after(update.FirstCheckAfter + 2*update.CheckEvery)
		assert.Zero(t, r.feed.manifests.Load())

		s, err := r.service.CheckNow(t.Context())
		require.NoError(t, err)
		assert.Equal(t, update.StateAvailable, s.State, "Check for Updates still works")
	})
}

func TestChecksNeverOverlap(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		gate := make(chan struct{})
		r.feed.gate = gate

		_, stop := runWorker(t, r)
		defer stop()

		after(update.FirstCheckAfter)
		require.Equal(t, int32(1), r.feed.manifests.Load(), "the timed check is reading")

		manual := make(chan error, 1)

		go func() {
			_, err := r.service.CheckNow(t.Context())
			manual <- err
		}()

		synctest.Wait()
		assert.Equal(t, int32(1), r.feed.manifests.Load(), "the manual check waits for the timed one")

		close(gate)
		synctest.Wait()

		require.NoError(t, <-manual)
		assert.Equal(t, int32(2), r.feed.manifests.Load())
		assert.Equal(t, int32(1), r.feed.maxInFlight.Load(), "never two at once")
	})
}

func TestAManualCheckWithDownloadsOnDownloadsAtOnceAndTellsNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)

		_, stop := runWorker(t, r)
		defer stop()

		after(update.FirstCheckAfter)

		r.release(t, next)

		s, err := r.service.CheckNow(t.Context())
		require.NoError(t, err)
		assert.Equal(t, next, s.Latest)

		after(time.Millisecond) // a wake runs the look at once, a moment after the last
		assert.Equal(t, update.StateReady, r.status(t).State, "the Worker downloads it at once")
		assert.Empty(t, r.notifier.all(), "the user saw it found")
	})
}

func TestTurningDownloadsOnDownloadsWhatWasFound(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		r.mode(updateprefs.ModeNotify)
		r.release(t, next)

		w, stop := runWorker(t, r)
		defer stop()

		after(update.FirstCheckAfter)
		require.Equal(t, update.StateAvailable, r.status(t).State)

		r.mode(updateprefs.ModeDownload)
		w.Wake()
		after(time.Millisecond)

		assert.Equal(t, update.StateReady, r.status(t).State)
		assert.Equal(t, int32(1), r.feed.manifests.Load(), "no second check: the download alone")
		assert.Equal(t, []update.Notice{{Version: next}}, r.notifier.all(), "the version was told once")
	})
}

func TestAnUpdateKeptIsReadyAtOnceAfterARelaunch(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		first := newRig(t)
		first.release(t, next)
		require.NoError(t, first.engine.Tick(t.Context()))

		// The app quits with the update ready, and opens again on the same data.
		again := newRig(t, func(c *update.Config) {
			c.Cache, c.Key, c.Feed = first.cfg.Cache, first.cfg.Key, first.feed
		})

		_, stop := runWorker(t, again)
		defer stop()

		s := again.status(t)
		assert.Equal(t, update.StateReady, s.State, "before any check")
		assert.Equal(t, next, s.Latest)
		assert.Equal(t, int32(1), first.feed.downloads.Load(), "with no download")
	})
}

func TestTheWorkerOfABuildThatDoesntUpdateReturnsAtOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, func(c *update.Config) { c.Flavor = update.FlavorSource })
		w := update.NewWorker(r.engine)

		done := make(chan struct{})

		go func() {
			defer close(done)

			w.Run(t.Context())
		}()

		synctest.Wait()

		select {
		case <-done:
		default:
			require.FailNow(t, "the Worker of a source build kept running")
		}
	})
}
