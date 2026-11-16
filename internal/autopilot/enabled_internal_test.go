package autopilot

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// errUnreadable is settings that can't be read.
var errUnreadable = errors.New("the settings can't be read")

// switchPrefs are Autopilot's default settings with Autopilot (beta) on or off, as a test
// switches it, and failing to be read while broken is set.
type switchPrefs struct {
	on, broken atomic.Bool
}

// switchedOn returns settings with Autopilot on or off.
func switchedOn(on bool) *switchPrefs {
	p := &switchPrefs{}
	p.on.Store(on)

	return p
}

func (p *switchPrefs) Autopilot(context.Context) (autopilotprefs.Config, error) {
	if p.broken.Load() {
		return autopilotprefs.Config{}, errUnreadable
	}

	c := autopilotprefs.Defaults()
	c.Enabled = p.on.Load()

	return c, nil
}

// quiet is the inbox before any refresh, as an engine reads it: each cycle reads its
// status, which it counts, and waits for a refresh.
type quiet struct {
	EngineItems

	mu     sync.Mutex
	cycles int
}

func (q *quiet) InboxStatus(context.Context) (InboxStatus, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.cycles++

	return InboxStatus{}, nil
}

func (q *quiet) count() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.cycles
}

// lockFree reports whether another copy could take the lock at path now.
func lockFree(t *testing.T, path string) bool {
	t.Helper()

	other := NewLock(path)

	held, err := other.TryLock()
	require.NoError(t, err)
	other.Unlock()

	return held
}

// running runs e until the test ends.
func running(t *testing.T, e *Engine) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		e.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func TestAnEngineBuiltWhileAutopilotIsOffTakesNoLockAndRunsNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "autopilot.lock")
		svc := NewService(Config{Pool: testutil.DB(t), Prefs: switchedOn(false)})
		items := &quiet{}
		e := NewEngine(svc, EngineConfig{Items: items, Lock: NewLock(path)})
		searches := NewSearches(svc)

		assert.False(t, e.Enabled())
		assert.True(t, lockFree(t, path), "it takes no lock as it is built")
		assert.False(t, searches.HoldsWatches(), "so the inbox reads no routine search")

		running(t, e)
		synctest.Wait()
		time.Sleep(3 * DefaultTick)
		synctest.Wait()

		assert.Zero(t, items.count(), "no cycle runs, on a wake or a tick")
		assert.True(t, lockFree(t, path), "nor does a tick take the lock")

		_, err := svc.Skip(t.Context(), 99)
		require.ErrorIs(t, err, ErrOff)
	})
}

func TestAnEngineBuiltWhileAutopilotIsOnTakesTheLockAtOnce(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "autopilot.lock")
	svc := NewService(Config{Pool: testutil.DB(t), Prefs: switchedOn(true)})
	searches := NewSearches(svc)

	// Core builds the searches' snapshot first, which reads the switch.
	searches.Rebuild(t.Context())

	e := NewEngine(svc, EngineConfig{Items: &quiet{}, Lock: NewLock(path)})
	t.Cleanup(e.lock.Unlock)

	assert.True(t, e.Enabled())
	assert.False(t, lockFree(t, path), "before its loop runs, so the first refresh carries the searches")
	assert.True(t, searches.HoldsWatches())
}

func TestTurningAutopilotOnAndOffStartsAndStopsTheEngine(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "autopilot.lock")
		prefs := switchedOn(false)
		svc := NewService(Config{Pool: testutil.DB(t), Prefs: prefs})
		items := &quiet{}
		e := NewEngine(svc, EngineConfig{Items: items, Lock: NewLock(path)})
		searches := NewSearches(svc)

		running(t, e)
		synctest.Wait()
		require.Zero(t, items.count())

		// On: the engine takes the lock and runs its cycles.
		prefs.on.Store(true)
		e.Wake()
		synctest.Wait()

		assert.True(t, e.Enabled())
		assert.False(t, lockFree(t, path))
		assert.True(t, searches.HoldsWatches())
		assert.Equal(t, 1, items.count(), "a cycle runs")

		_, err := svc.Skip(t.Context(), 99)
		require.ErrorIs(t, err, ErrNoMatch, "commands are applied")

		// Off: it lets go of the lock, as nothing it started still goes, and runs nothing.
		prefs.on.Store(false)
		e.Wake()
		synctest.Wait()

		cycles := items.count()

		assert.False(t, e.Enabled())
		assert.True(t, lockFree(t, path))
		assert.False(t, searches.HoldsWatches())

		time.Sleep(3 * DefaultTick)
		synctest.Wait()
		assert.Equal(t, cycles, items.count(), "no cycle runs while it is off")

		_, err = svc.Skip(t.Context(), 99)
		require.ErrorIs(t, err, ErrOff)

		// On again, it starts again; a command reads the switch as it is, without waiting
		// for the loop's wake.
		prefs.on.Store(true)

		_, err = svc.Skip(t.Context(), 99)
		require.ErrorIs(t, err, ErrNoMatch)
		assert.False(t, lockFree(t, path))
	})
}

func TestSettingsThatCantBeReadLeaveTheEngineAsItWas(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "autopilot.lock")
		prefs := switchedOn(true)
		logs := &testutil.Logs{}
		svc := NewService(Config{Pool: testutil.DB(t), Prefs: prefs, Logger: logs.Logger()})
		e := NewEngine(svc, EngineConfig{Items: &quiet{}, Lock: NewLock(path)})

		running(t, e)
		synctest.Wait()
		require.False(t, lockFree(t, path))

		prefs.broken.Store(true)
		e.Wake()
		synctest.Wait()

		assert.True(t, e.Enabled(), "a passing problem never turns it off")
		assert.False(t, lockFree(t, path))
		assert.Contains(t, logs.String(), "could not read whether Autopilot is on")
	})
}

// counted is a dispatcher that counts what it is handed.
type counted struct {
	n atomic.Int32
}

func (c *counted) Dispatch(context.Context, Job) (Result, error) {
	c.n.Add(1)

	return Result{}, nil
}

func TestAStartOnItsWayAsAutopilotIsTurnedOffIsNeverHandedOver(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		svc := NewService(Config{Pool: testutil.DB(t), Prefs: switchedOn(true)})
		got := &counted{}
		e := NewEngine(svc, EngineConfig{Items: &quiet{}, Dispatchers: map[catalog.Dispatcher]Dispatcher{
			catalog.DispatchNotices: got,
		}})

		e.on.Store(false)
		e.dispatch(t.Context(), 1, 0, catalog.DispatchNotices, Job{MatchID: 1})
		e.work.Wait()

		assert.Zero(t, got.n.Load(), "Enabled is asked right before a step is handed over")

		e.on.Store(true)
		e.dispatch(t.Context(), 1, 0, catalog.DispatchNotices, Job{MatchID: 1})
		e.work.Wait()

		assert.Equal(t, int32(1), got.n.Load())
	})
}
