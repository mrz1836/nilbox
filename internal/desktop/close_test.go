package desktop_test

import (
	"slices"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/event"
)

func TestCloseOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		stopping bool
		hides    bool
		want     desktop.CloseAction
	}{
		{name: "stopping goes through", stopping: true, want: desktop.CloseThrough},
		{
			name: "stopping goes through, though the app would keep running", stopping: true, hides: true,
			want: desktop.CloseThrough,
		},
		{name: "kept running hides", hides: true, want: desktop.CloseHide},
		{name: "nothing keeps the app running quits", want: desktop.CloseQuit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, desktop.CloseOf(tt.stopping, tt.hides))
		})
	}
}

// quitRuntime stands for the desktop runtime's side of a quit, which each request to quit
// starts: it asks the shell whether the app may quit (ShouldQuit), and a quit let through
// stops the shell and then closes the window, asking the shell about that close as the
// window's hook does (WindowClosing).
type quitRuntime struct {
	shell *desktop.Shell

	mu      sync.Mutex
	refused int
	closes  []desktop.CloseAction
}

// attach has every request r's app gets to quit run through q.
func (q *quitRuntime) attach(r *rig) {
	r.app.onQuit(q.quit)
}

// quit asks, then stops the shell and closes the window when the quit goes through.
func (q *quitRuntime) quit() {
	if !q.shell.ShouldQuit() {
		q.mu.Lock()
		q.refused++
		q.mu.Unlock()

		return
	}

	_ = q.shell.Stop()
	action := q.shell.WindowClosing()

	q.mu.Lock()
	q.closes = append(q.closes, action)
	q.mu.Unlock()
}

// refusals counts the quits the shell held back.
func (q *quitRuntime) refusals() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.refused
}

// closed is what the shell answered to each close the runtime's shutdown made.
func (q *quitRuntime) closed() []desktop.CloseAction {
	q.mu.Lock()
	defer q.mu.Unlock()

	return slices.Clone(q.closes)
}

// With the menu bar icon on, the close only hides the window: nothing quits.
func TestTheWindowsCloseHidesWhileTheIconShows(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell()

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		assert.Equal(t, desktop.CloseHide, s.WindowClosing())
		synctest.Wait()
		assert.Zero(t, r.app.quitCount(), "the app keeps running")

		s.ShowWindow()
		assert.NotContains(t, r.page.events(), event.WindowEvent(event.KeptRunning{}),
			"the icon shows where the app went")

		require.NoError(t, s.Stop())
	})
}

// With a routine on, the close only hides the window even with the icon off, and the
// window says why as it next shows.
func TestTheWindowsCloseHidesWhileAutopilotKeepsTheAppRunning(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.settings.menuBar = false
		s := r.shell(withAutopilot(&fakeAutopilot{st: oneRoutine()}))

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		assert.Equal(t, desktop.CloseHide, s.WindowClosing())
		synctest.Wait()
		assert.Zero(t, r.app.quitCount(), "the app keeps running for Autopilot")

		s.ShowWindow()
		assert.Equal(t, []event.WindowEvent{event.KeptRunning{}}, r.page.events())

		require.NoError(t, s.Stop())
	})
}

// With nothing keeping the app running, the close keeps the window and quits the normal
// way; the quit goes through, and the runtime's own close of the window, the shell stopped
// by then, goes through without quitting again.
func TestTheWindowsCloseQuitsWhenNothingKeepsTheAppRunning(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.settings.menuBar = false
		s := r.shell(func(c *desktop.Config) { c.QuitGuard = &fakeGuard{} })
		rt := &quitRuntime{shell: s}
		rt.attach(r)

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		assert.Equal(t, desktop.CloseQuit, s.WindowClosing(), "the window stays while the app quits")
		synctest.Wait()

		assert.Equal(t, 1, r.app.quitCount(), "one quit, the normal way")
		assert.Zero(t, rt.refusals())
		assert.Equal(t, []desktop.CloseAction{desktop.CloseThrough}, rt.closed(),
			"the shutdown's close goes through, and quits nothing again")
		assert.Equal(t, []string{dataClosed}, r.lines(dataClosed), "the shell shut down in order")
		assert.Zero(t, r.window.shown(), "nothing was asked")
	})
}

// While runs go in the app, the close's quit asks first, in the window it kept, and the
// app runs on until the user lets the quit through.
func TestTheWindowsCloseAsksFirstWhileRunsGo(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.settings.menuBar = false
		guard := &fakeGuard{}
		guard.active.Store(true)

		s := r.shell(func(c *desktop.Config) { c.QuitGuard = guard })
		rt := &quitRuntime{shell: s}
		rt.attach(r)

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		assert.Equal(t, desktop.CloseQuit, s.WindowClosing(), "the window stays for the question")
		synctest.Wait()

		assert.Equal(t, 1, rt.refusals(), "the quit waits for the runs")
		assert.Equal(t, 1, r.window.shown(), "the window comes forward to ask")
		assert.Equal(t, []event.WindowEvent{event.QuitRequested{}}, r.page.events())
		assert.Empty(t, rt.closed(), "the window was never closed")
		assert.False(t, s.Stopping(), "the app runs on")

		// Keep running keeps it so; a close asks again.
		s.Emit(event.QuitKept{})
		assert.Equal(t, desktop.CloseQuit, s.WindowClosing())
		synctest.Wait()
		assert.Equal(t, 2, rt.refusals())
		assert.Equal(t, []event.WindowEvent{event.QuitRequested{}, event.QuitRequested{}}, r.page.events())

		// Stop and quit lets the quit through, and the shutdown's close goes through.
		s.Emit(event.QuitConfirmed{})
		synctest.Wait()

		assert.Equal(t, 3, r.app.quitCount())
		assert.Equal(t, []desktop.CloseAction{desktop.CloseThrough}, rt.closed())
		assert.True(t, s.Stopping())
	})
}

// While a move to Applications runs, the close's quit waits for it, as every quit does,
// and goes ahead once the move lets go.
func TestTheWindowsCloseWaitsForAMove(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.settings.menuBar = false
		s := r.shell()
		rt := &quitRuntime{shell: s}
		rt.attach(r)

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		letGo := s.HoldQuit()

		assert.Equal(t, desktop.CloseQuit, s.WindowClosing())
		synctest.Wait()
		assert.Equal(t, 1, rt.refusals(), "the quit waits for the move")
		assert.Equal(t, 1, r.window.shown(), "where the move shows")
		assert.Empty(t, rt.closed())

		letGo()
		synctest.Wait()

		assert.Equal(t, 2, r.app.quitCount(), "the quit goes ahead once the move lets go")
		assert.Equal(t, []desktop.CloseAction{desktop.CloseThrough}, rt.closed())
	})
}

// Once the shell is stopping, as the runtime's shutdown closes the window, the close goes
// through, the icon on or not, and quits nothing.
func TestTheWindowsCloseGoesThroughOnceStopping(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		s := r.shell(withAutopilot(&fakeAutopilot{st: oneRoutine()}))

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()
		require.True(t, s.HidesOnClose(), "the icon on and a routine on")

		require.NoError(t, s.Stop())

		assert.Equal(t, desktop.CloseThrough, s.WindowClosing())
		synctest.Wait()
		assert.Zero(t, r.app.quitCount())
		assert.Zero(t, r.window.shown())
	})
}
