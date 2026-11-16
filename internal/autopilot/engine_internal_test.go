package autopilot

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestOneCopyHoldsTheLockUntilItLetsGo(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "data", "autopilot.lock")
	first, second := NewLock(path), NewLock(path)

	held, err := first.TryLock()
	require.NoError(t, err)
	assert.True(t, held)

	held, err = second.TryLock()
	require.NoError(t, err)
	assert.False(t, held, "a second open of the file is another copy")
	assert.False(t, second.Held())

	first.Unlock()

	held, err = second.TryLock()
	require.NoError(t, err)
	assert.True(t, held, "it takes the lock once the first lets go")

	second.Unlock()
	second.Unlock()

	held, err = NewLock("").TryLock()
	require.NoError(t, err)
	assert.True(t, held, "a lock with no file always holds")
}

func TestActiveHours(t *testing.T) {
	t.Parallel()

	// 2026-11-16 is a Monday.
	at := func(day, hh, mm int) time.Time { return time.Date(2026, time.November, 16+day, hh, mm, 0, 0, time.UTC) }
	weekdays := []catalog.Weekday{catalog.Monday, catalog.Tuesday, catalog.Wednesday, catalog.Thursday, catalog.Friday}
	day := &spec.ActiveHours{Days: weekdays, From: "08:00", To: "20:00"}
	night := &spec.ActiveHours{Days: []catalog.Weekday{catalog.Monday}, From: "22:00", To: "06:00"}

	for _, c := range []struct {
		name  string
		hours *spec.ActiveHours
		now   time.Time
		want  bool
	}{
		{"always", nil, at(0, 3, 0), true},
		{"within", day, at(0, 9, 30), true},
		{"before", day, at(0, 7, 59), false},
		{"at the end", day, at(0, 20, 0), false},
		{"a weekend", day, at(5, 9, 30), false},
		{"past midnight, the evening", night, at(0, 23, 0), true},
		{"past midnight, the morning after", night, at(1, 5, 0), true},
		{"past midnight, another day's morning", night, at(0, 5, 0), false},
	} {
		assert.Equal(t, c.want, inActiveHours(c.hours, c.now), c.name)
	}
}

func TestOnlyIfReadsTheLatestStepOfItsKind(t *testing.T) {
	t.Parallel()

	agent := spec.Step{Agent: &spec.AgentStep{}}
	pushed := []stepRun{{Step: agent, Outcome: string(catalog.OutcomePushed)}}
	notPushed := []stepRun{{Step: agent, Outcome: string(catalog.OutcomeNotPushed)}}

	assert.True(t, onlyIfHolds(nil, ""))
	assert.True(t, onlyIfHolds(pushed, catalog.OutcomePushed))
	assert.False(t, onlyIfHolds(notPushed, catalog.OutcomePushed))
	assert.False(t, onlyIfHolds(nil, catalog.OutcomePushed), "no step of its kind ran")
}

func TestADayRunsFromLocalMidnight(t *testing.T) {
	t.Parallel()

	east := time.FixedZone("east", 5*3600)
	now := time.Date(2026, time.November, 16, 1, 30, 0, 0, east)
	assert.Equal(t, time.Date(2026, time.November, 16, 0, 0, 0, 0, east), dayStart(now))
}

// panicky is the inbox, as read by an engine whose every cycle panics.
type panicky struct {
	EngineItems

	mu     sync.Mutex
	cycles int
}

func (p *panicky) InboxStatus(context.Context) (InboxStatus, error) {
	p.mu.Lock()
	p.cycles++
	p.mu.Unlock()

	panic("a cycle that panics")
}

func (p *panicky) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.cycles
}

func TestACycleThatPanicsIsRecoveredAndHoldsOffTheNext(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		logs := &testutil.Logs{}
		svc := NewService(Config{Pool: testutil.DB(t), Logger: logs.Logger()})
		items := &panicky{}
		e := NewEngine(svc, EngineConfig{Items: items})

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			e.Run(ctx)
		}()

		synctest.Wait()
		assert.Equal(t, 1, items.count())
		assert.Contains(t, logs.String(), "an Autopilot cycle panicked")

		// Commands are still answered, and a wake within the minute runs no cycle.
		_, err := svc.Skip(ctx, 99)
		require.ErrorIs(t, err, ErrNoMatch)

		e.Wake()
		synctest.Wait()
		assert.Equal(t, 1, items.count(), "it holds off a minute")

		time.Sleep(panicWait + time.Second)
		synctest.Wait()
		assert.Equal(t, 2, items.count(), "then tries again")

		cancel()
		<-done
	})
}

func TestACommandBeforeTheEngineRunsIsRefused(t *testing.T) {
	t.Parallel()

	svc := NewService(Config{Pool: testutil.DB(t)})
	NewEngine(svc, EngineConfig{Items: &panicky{}})

	_, err := svc.Approve(t.Context(), []int64{1})
	require.ErrorIs(t, err, ErrNotRunning)

	_, err = NewService(Config{Pool: testutil.DB(t)}).Approve(t.Context(), []int64{1})
	require.ErrorIs(t, err, ErrNoEngine, "with no engine at all")
}

// events counts the autopilot:updated events.
type events struct {
	mu sync.Mutex
	n  int
}

func (e *events) Emit(ev event.Event) {
	if _, ok := ev.(event.AutopilotUpdated); ok {
		e.mu.Lock()
		e.n++
		e.mu.Unlock()
	}
}

func (e *events) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.n
}

func TestTheWindowHearsOfChangesAtMostFourTimesASecond(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		heard := &events{}
		e := NewEngine(NewService(Config{Pool: testutil.DB(t)}), EngineConfig{Items: &panicky{}, Events: heard})

		for range 100 {
			e.notify()
			time.Sleep(10 * time.Millisecond)
		}

		time.Sleep(time.Second)
		synctest.Wait()

		assert.Equal(t, 5, heard.count(), "a second of changes: one at once, then one every quarter second")
	})
}
