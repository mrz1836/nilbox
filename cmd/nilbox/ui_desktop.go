//go:build desktop

package main

import (
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/settings"
)

// desktopUI is the shell's way to the desktop runtime: the page it passes window events
// to, the window and the native appearance, and the app itself. It is attached to the app
// once the app exists, which is before the app runs and so before the shell calls it.
type desktopUI struct {
	app   atomic.Pointer[application.App]
	shell *desktop.Shell

	// appearance guards theme and window, so a theme the user chose and a change of the
	// system appearance are applied one at a time, in order.
	appearance sync.Mutex
	theme      settings.Theme
	window     *application.WebviewWindow
}

// Emit passes a window event on to the page, with the event as its data: the type it was
// registered with.
func (u *desktopUI) Emit(e event.WindowEvent) {
	if app := u.app.Load(); app != nil {
		app.Event.Emit(e.Name(), e)
	}
}

// Open opens the window in theme and at size, with its page showing text at textSize. The
// window is never smaller than the minimum for the text size, and both fit the primary
// screen's work area.
func (u *desktopUI) Open(theme settings.Theme, size settings.WindowSize, textSize settings.TextSize) {
	app := u.app.Load()
	if app == nil {
		return
	}

	width, height := workArea(app.Screen.GetPrimary())
	size, least := settings.WindowBounds(size, textSize, width, height)

	u.appearance.Lock()
	u.theme = theme
	// Pinning the appearance before the window exists is what keeps its first frame from
	// flashing the other palette: the page's prefers-color-scheme already matches.
	applyAppearance(theme)
	u.window = app.Window.NewWithOptions(windowOptions(windowSpec{
		dark:     settings.IsDark(theme, app.Env.IsDarkMode()),
		size:     size,
		least:    least,
		textSize: textSize,
	}))
	window := u.window
	u.appearance.Unlock()

	window.RegisterHook(events.Common.WindowClosing, u.closing)
}

// SetTextSize sets the smallest the window can be to the minimum for a text size the user
// chose, fitted to the work area of the screen the window is on (or the primary screen,
// when that cannot be told). A window smaller than that grows to it, unless it is
// maximized or full screen, and moves back inside the work area if it grew past an edge.
// The page has already shown the size itself.
func (u *desktopUI) SetTextSize(textSize settings.TextSize) {
	u.appearance.Lock()
	window := u.window
	u.appearance.Unlock()

	if window == nil {
		return
	}

	screen := u.screenOf(window)
	width, height := window.Size()
	least := settings.MinWindowSizeFor(textSize).Fit(workArea(screen))
	window.SetMinSize(least.Width, least.Height)

	if grownWidth, grownHeight := window.Size(); grownWidth != width || grownHeight != height {
		keepInside(window, screen)
	}
}

// Show brings the window forward: shown if it was closed into the menu bar, restored if
// it was minimized, and the app made active.
func (u *desktopUI) Show() {
	u.appearance.Lock()
	window := u.window
	u.appearance.Unlock()

	if window == nil {
		return
	}

	if window.IsMinimised() {
		window.UnMinimise()
	}

	window.Show()
	window.Focus()
	activateApp()
}

// SetTheme brings the native appearance and the window background along with a theme
// the user chose; the page has already switched itself.
func (u *desktopUI) SetTheme(theme settings.Theme) {
	u.appearance.Lock()
	defer u.appearance.Unlock()

	u.theme = theme
	applyAppearance(theme)
	u.paintBackground()
}

// Quit quits the app.
func (u *desktopUI) Quit() {
	if app := u.app.Load(); app != nil {
		app.Quit()
	}
}

// screenOf is the screen the window is on, or the primary screen when that cannot be told,
// or nil when there is neither.
func (u *desktopUI) screenOf(window *application.WebviewWindow) *application.Screen {
	if screen, err := window.GetScreen(); err == nil && screen != nil {
		return screen
	}

	if app := u.app.Load(); app != nil {
		return app.Screen.GetPrimary()
	}

	return nil
}

// attach connects the UI to the app and to the shell that decides what it does.
func (u *desktopUI) attach(app *application.App, shell *desktop.Shell) {
	u.shell = shell
	u.app.Store(app)
}

// systemThemeChanged repaints the window background when the system appearance changes,
// which the System theme follows. AppKit and the page follow the system on their own.
func (u *desktopUI) systemThemeChanged() {
	if u.shell.Stopping() {
		return
	}

	u.appearance.Lock()
	defer u.appearance.Unlock()

	u.paintBackground()
}

// paintBackground sets the window's native background, which shows while the page has
// not painted, such as at the edges of a live resize, to the palette the theme shows now.
// The caller holds u.appearance.
func (u *desktopUI) paintBackground() {
	app := u.app.Load()
	if app == nil || u.window == nil {
		return
	}

	u.window.SetBackgroundColour(backgroundColour(settings.IsDark(u.theme, app.Env.IsDarkMode())))
}

// closing keeps the app running in the menu bar when its window is closed while the menu
// bar icon is shown: the window hides, and the icon or the Dock brings it back. Otherwise,
// and when the app is quitting, the window closes, which quits the app.
func (u *desktopUI) closing(e *application.WindowEvent) {
	if !u.shell.HidesOnClose() {
		return
	}

	u.appearance.Lock()
	window := u.window
	u.appearance.Unlock()

	if window != nil {
		window.Hide()
		e.Cancel()
	}
}

// keepInside moves the window back inside the screen's work area where it reaches past an
// edge (see desktop.WithinArea). Without a screen, it leaves the window where it is.
func keepInside(window *application.WebviewWindow, screen *application.Screen) {
	if screen == nil {
		return
	}

	bounds := window.Bounds()
	area := screen.WorkArea
	x, y := desktop.WithinArea(
		desktop.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: bounds.Height},
		desktop.Rect{X: area.X, Y: area.Y, Width: area.Width, Height: area.Height},
	)

	if x != bounds.X || y != bounds.Y {
		window.SetPosition(x, y)
	}
}

// workArea is the width and height of a screen's work area, or zero for no screen, which
// fits any size.
func workArea(screen *application.Screen) (width, height int) {
	if screen == nil {
		return 0, 0
	}

	return screen.WorkArea.Width, screen.WorkArea.Height
}

// windowSpec is what the window opens with: the palette its background is in, its size
// and the smallest it can be resized to, in points, and the text size its page shows.
type windowSpec struct {
	dark     bool
	size     settings.WindowSize
	least    settings.WindowSize
	textSize settings.TextSize
}

// baseWindowOptions are the window settings shared by every OS, for the window spec: on
// the native background of the dark or light palette, so the frame never flashes a
// different color before the page paints, and with the text size in the page's URL, so
// its first frame already shows text at that size.
func baseWindowOptions(spec windowSpec) application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Title:            brand.Name,
		Width:            spec.size.Width,
		Height:           spec.size.Height,
		MinWidth:         spec.least.Width,
		MinHeight:        spec.least.Height,
		URL:              desktop.PageURL(spec.textSize),
		BackgroundColour: backgroundColour(spec.dark),
	}
}

// backgroundColour is the palette's window background for the appearance.
func backgroundColour(dark bool) application.RGBA {
	palette := brand.Light
	if dark {
		palette = brand.Dark
	}

	red, green, blue, err := brand.RGB(palette.Background)
	if err != nil {
		// Unreachable: the brand tests validate every palette color.
		return application.NewRGB(0, 0, 0)
	}

	return application.NewRGB(red, green, blue)
}
