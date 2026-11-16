//go:build desktop

package main

import (
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/menubar"
)

// quitCounter is the app, counting the requests to quit.
type quitCounter struct{ quits atomic.Int32 }

func (q *quitCounter) Quit() { q.quits.Add(1) }

// quietTray is a menu bar icon that draws nothing.
type quietTray struct{}

func (quietTray) Show()                {}
func (quietTray) Hide()                {}
func (quietTray) Draw(menubar.Summary) {}

// TestClosingTheLastWindowNeverQuitsByItself guards the app's macOS options: AppKit asks
// applicationShouldTerminateAfterLastWindowClosed as the last window hides too, so with it
// on, a close that hid the window to keep the app running ended the app a moment later.
func TestClosingTheLastWindowNeverQuitsByItself(t *testing.T) {
	t.Parallel()

	assert.False(t, macAppOptions().ApplicationShouldTerminateAfterLastWindowClosed,
		"the window's close decides whether the app quits, never the last window's")
}

// TestTheWindowsCloseHook runs the window's close hook over a shell with the runtime's own
// window event: it keeps the window for a hide and a quit, which the shell starts, and lets
// the close through once the shell is stopping.
func TestTheWindowsCloseHook(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		prepare  func(t *testing.T, s *desktop.Shell)
		canceled bool
		quits    int32
		why      string
	}{
		{
			name:    "the icon on hides",
			prepare: func(_ *testing.T, s *desktop.Shell) { s.Emit(event.MenuBarChanged{On: true}) },
			// The hook has no window to hide here, and keeps it from closing all the same.
			canceled: true, why: "the window hides instead of closing",
		},
		{
			name:     "nothing keeping the app running quits",
			prepare:  func(*testing.T, *desktop.Shell) {},
			canceled: true, quits: 1, why: "the window stays while the app quits",
		},
		{
			name:    "stopping goes through",
			prepare: func(t *testing.T, s *desktop.Shell) { require.NoError(t, s.Stop()) },
			why:     "the runtime's shutdown closes the window",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				app := &quitCounter{}
				shell := desktop.New(desktop.Config{App: app, Tray: quietTray{}})
				tt.prepare(t, shell)

				ui := &desktopUI{shell: shell}
				e := application.NewWindowEvent()
				ui.closing(e)
				synctest.Wait()

				assert.Equal(t, tt.canceled, e.IsCancelled(), tt.why)
				assert.Equal(t, tt.quits, app.quits.Load())
			})
		})
	}
}
