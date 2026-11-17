package desktop_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/menubar"
)

var errFakeAutopilot = errors.New("fake: Autopilot cannot be read")

// fakeAutopilot serves a state the test sets, and records each change of Pause Autopilot,
// storing it, as the settings' setter does.
type fakeAutopilot struct {
	mu       sync.Mutex
	st       desktop.AutopilotState
	err      error
	pauseErr error
	reads    int
	paused   []bool
}

func (a *fakeAutopilot) State(context.Context) (desktop.AutopilotState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.reads++

	return a.st, a.err
}

func (a *fakeAutopilot) SetPaused(_ context.Context, paused bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.paused = append(a.paused, paused)

	if a.pauseErr != nil {
		return a.pauseErr
	}

	a.st.Menu.Paused = paused

	return nil
}

func (a *fakeAutopilot) set(st desktop.AutopilotState, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.st, a.err = st, err
}

func (a *fakeAutopilot) readCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.reads
}

func (a *fakeAutopilot) pauses() []bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	return append([]bool(nil), a.paused...)
}

// withAutopilot hands the shell a.
func withAutopilot(a *fakeAutopilot) func(*desktop.Config) {
	return func(c *desktop.Config) { c.Autopilot = a }
}

// oneRoutine is Autopilot with one routine, switched on.
func oneRoutine() desktop.AutopilotState {
	return desktop.AutopilotState{Menu: menubar.AutopilotState{Routines: 1}, KeepsRunning: true}
}

// lastGroup returns the Autopilot group of the latest drawing.
func lastGroup(t *testing.T, r *rig) menubar.Autopilot {
	t.Helper()

	draws := r.tray.drawn()
	require.NotEmpty(t, draws)

	return draws[len(draws)-1].Autopilot
}

func TestTheMenuBarDrawsAutopilotsGroup(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.inbox.set(snoozedFor(time.Hour), nil)

		ap := &fakeAutopilot{st: desktop.AutopilotState{
			Menu: menubar.AutopilotState{Routines: 2, NeedsYou: 1, Working: []menubar.AgentRun{{
				RunID: 9, RoutineID: 2, MatchID: 4, Repo: "acme-org/example-repo", Number: 88, Agent: agent.Claude,
				StartedAt: time.Now().Add(-6 * time.Minute), CreatedAt: time.Now().Add(-6 * time.Minute),
			}}},
			KeepsRunning: true,
		}}
		s := r.shell(withAutopilot(ap))

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		group := lastGroup(t, r)
		assert.True(t, group.Shown)
		assert.Equal(t, "1 needs your OK", group.NeedsYou)
		require.Len(t, group.Working, 1)
		assert.Equal(t, "example-repo #88 · Claude · 6m", group.Working[0].Label)
		assert.Equal(t, []string{"1"}, labels(r), "the count stays the inbox's")

		draws := len(r.tray.drawn())

		s.Emit(event.AutopilotUpdated{})
		synctest.Wait()
		assert.Len(t, r.tray.drawn(), draws, "a change the group doesn't show redraws nothing")

		time.Sleep(time.Minute + 2*time.Second) // past the minute and the redraw's slack
		synctest.Wait()
		assert.Equal(t, "example-repo #88 · Claude · 7m", lastGroup(t, r).Working[0].Label,
			"the run's minutes tick on their own")

		ap.set(desktop.AutopilotState{Menu: menubar.AutopilotState{Routines: 2, Paused: true}, KeepsRunning: true}, nil)
		s.Emit(event.AutopilotUpdated{})
		synctest.Wait()

		group = lastGroup(t, r)
		assert.True(t, group.Paused, "the Pause row is ticked")
		assert.Empty(t, group.Working)
		assert.Empty(t, group.NeedsYou)
		assert.Contains(t, r.page.events(), event.WindowEvent(event.AutopilotUpdated{}), "the window hears it too")

		require.NoError(t, s.Stop())
	})
}

func TestAutopilotIsReadWhileTheIconIsOffForTheClose(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.settings.menuBar = false
		ap := &fakeAutopilot{}
		s := r.shell(withAutopilot(ap))

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		assert.Equal(t, 1, ap.readCount(), "read once at the start")
		assert.Empty(t, r.lines("tray"), "and nothing is drawn")
		assert.Zero(t, r.inbox.readCount())
		assert.False(t, s.HidesOnClose(), "no routine on and no agent working: closing the window quits")

		ap.set(oneRoutine(), nil)
		s.Emit(event.AutopilotUpdated{})
		synctest.Wait()

		assert.Equal(t, 2, ap.readCount())
		assert.True(t, s.HidesOnClose(), "a routine on: closing the window hides it, the icon off")

		ap.set(desktop.AutopilotState{}, errFakeAutopilot)
		s.Emit(event.AutopilotUpdated{})
		synctest.Wait()

		assert.True(t, s.HidesOnClose(), "a state that can't be read keeps the last one read")
		assert.Contains(t, r.logs.String(), "could not read Autopilot for the menu bar")

		ap.set(desktop.AutopilotState{Menu: menubar.AutopilotState{Routines: 1}}, nil)
		s.Emit(event.AutopilotUpdated{})
		synctest.Wait()

		assert.False(t, s.HidesOnClose(), "every routine off and no agent working: it quits again")

		require.NoError(t, s.Stop())
		assert.False(t, s.HidesOnClose(), "a quitting app closes its window")
	})
}

func TestTheWindowComesBackFromAHideForAutopilotWithNothingSaid(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.settings.menuBar = false
		ap := &fakeAutopilot{st: oneRoutine()}
		s := r.shell(withAutopilot(ap))

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		require.True(t, s.HidesOnClose(), "a routine on, the icon off")
		s.ShowWindow()
		assert.Empty(t, r.page.events(), "the Dock brings the window back, and the page hears nothing of it")

		require.True(t, s.HidesOnClose())
		s.Emit(event.RevealItem{ItemID: 7})

		assert.Equal(t, []event.WindowEvent{event.RevealItem{ItemID: 7}}, r.page.events(),
			"a reveal tells the page only what to show")
		assert.Equal(t, 2, r.window.shown())

		require.NoError(t, s.Stop())
	})
}

func TestThePauseRowFlipsPauseAutopilot(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		ap := &fakeAutopilot{st: oneRoutine()}
		s := r.shell(withAutopilot(ap))

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		require.False(t, lastGroup(t, r).Paused)

		s.AutopilotAction(menubar.AutopilotPause, menubar.Working{})
		synctest.Wait()

		assert.Equal(t, []bool{true}, ap.pauses(), "unticked, a click pauses")
		assert.True(t, lastGroup(t, r).Paused, "and the menu reads it again, ticked")

		s.AutopilotAction(menubar.AutopilotPause, menubar.Working{})
		synctest.Wait()

		assert.Equal(t, []bool{true, false}, ap.pauses(), "ticked, a click resumes")
		assert.False(t, lastGroup(t, r).Paused)

		require.NoError(t, s.Stop())

		s.AutopilotAction(menubar.AutopilotPause, menubar.Working{})
		assert.Len(t, ap.pauses(), 2, "a stopping shell changes nothing")
	})
}

func TestAPauseThatFailsPutsTheTickBack(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		ap := &fakeAutopilot{st: oneRoutine(), pauseErr: errFakeAutopilot}
		s := r.shell(withAutopilot(ap))

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		draws := len(r.tray.drawn())

		// The menu ticks the row itself on the click; the write then fails.
		s.AutopilotAction(menubar.AutopilotPause, menubar.Working{})
		synctest.Wait()

		assert.Equal(t, []bool{true}, ap.pauses())
		assert.Contains(t, r.logs.String(), "could not change Pause Autopilot from the menu bar")
		require.Greater(t, len(r.tray.drawn()), draws, "the menu is drawn again, though the group reads as before")
		assert.False(t, lastGroup(t, r).Paused, "so the tick the click put there is taken back")

		require.NoError(t, s.Stop())
	})
}

func TestAPauseChangedElsewhereRedrawsTheTick(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		ap := &fakeAutopilot{st: oneRoutine()}
		s := r.shell(withAutopilot(ap))

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()
		require.False(t, lastGroup(t, r).Paused)

		// Settings paused it; core's relay tells the shell Autopilot changed.
		st := oneRoutine()
		st.Menu.Paused = true
		ap.set(st, nil)
		s.Emit(event.AutopilotUpdated{})
		synctest.Wait()

		assert.True(t, lastGroup(t, r).Paused)

		s.AutopilotAction(menubar.AutopilotPause, menubar.Working{})
		synctest.Wait()
		assert.Equal(t, []bool{false}, ap.pauses(), "a click flips what the menu shows now")

		require.NoError(t, s.Stop())
	})
}

func TestTheGroupsRowsOpenTheirTarget(t *testing.T) {
	t.Parallel()

	r := newRig()
	s := r.shell()

	s.AutopilotAction(menubar.AutopilotWatch, menubar.Working{
		RunID: 9, RoutineID: 2, MatchID: 4, Created: "2026-11-16T09:00:00Z", Label: "example-repo #88 · Claude · 6m",
	})
	s.AutopilotAction(menubar.AutopilotActivity, menubar.Working{})
	s.AutopilotAction(menubar.AutopilotPause, menubar.Working{})

	assert.Equal(t, 2, r.window.shown(), "a run and Activity bring the window forward")
	assert.Equal(t, []event.WindowEvent{
		event.RevealItem{Autopilot: event.AutopilotTarget{RoutineID: 2, MatchID: 4, RunID: 9, CreatedAt: "2026-11-16T09:00:00Z"}},
		event.RevealItem{Autopilot: event.AutopilotTarget{Activity: true}},
	}, r.page.events(), "each names what it opens; without Autopilot, Pause does nothing")
}
