// Package desktop is the desktop shell's logic, kept apart from the desktop runtime: what
// the app does with the services' events, how the menu bar icon is kept current, the
// background work that runs while the app is open, and the order the app shuts down in.
//
// It imports no desktop runtime. The desktop bootstrap (cmd/nilbox, built with the desktop
// tag) implements the small interfaces declared here over the runtime: the page that
// hears window events, the window, the menu bar icon, and the app itself. So everything
// here builds and is tested on any OS.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/menubar"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/timing"
)

const (
	// DefaultStopTimeout bounds how long Stop waits for the background work in flight,
	// such as a refresh, before it closes the data anyway.
	DefaultStopTimeout = 5 * time.Second

	// DefaultMeasureTimeout bounds a startup measurement whose window never reports its
	// first paint.
	DefaultMeasureTimeout = time.Minute

	// snoozeSlack is how long after the soonest snooze ends the menu bar is redrawn, so
	// the snooze has ended when the count is taken again.
	snoozeSlack = time.Second
)

// ErrNoFirstPaint is reported when a startup measurement timed out.
var ErrNoFirstPaint = errors.New("the window did not report its first paint in time")

// Frontend is the window's page. Emit passes a window event on to it, and must not wait
// for the page.
type Frontend interface {
	Emit(e event.WindowEvent)
}

// Window is the app's one window and its native appearance.
type Window interface {
	// Open opens the window at size, shrunk to fit the screen, in the native appearance
	// and on the background of theme.
	Open(theme settings.Theme, size settings.WindowSize)

	// Show brings the window forward: shown if it was closed into the menu bar, restored
	// if it was minimized, and the app made active.
	Show()

	// SetTheme pins the native appearance to theme and repaints the window's background.
	SetTheme(theme settings.Theme)
}

// Tray is the menu bar icon. Its methods must not wait for anything Stop waits for:
// Stop can run on the thread the icon is drawn on.
type Tray interface {
	// Show puts the icon in the menu bar, if it is not there.
	Show()

	// Hide takes the icon out of the menu bar.
	Hide()

	// Draw sets the icon's count and its menu.
	Draw(sum menubar.Summary)
}

// App is the running app.
type App interface {
	// Quit quits the app.
	Quit()
}

// Inbox is the stored inbox the menu bar shows, and the refresh its menu asks for.
// *inbox.Service implements it.
type Inbox interface {
	Snapshot(ctx context.Context) (inbox.Snapshot, error)
	Refresh(ctx context.Context) (inbox.Snapshot, error)
}

// Settings are the settings the shell reads when the app launches. core.ShellSettings
// implements it.
type Settings interface {
	Theme(ctx context.Context) (settings.Theme, error)
	MenuBar(ctx context.Context) (bool, error)
	WindowSize(ctx context.Context) (settings.WindowSize, error)
}

// Worker is background work that runs while the app is open, such as the inbox's
// scheduled refreshes. Run returns once ctx is canceled and the work in flight has
// returned. *inbox.Scheduler and worktrees.Sweeper implement it.
type Worker interface {
	Run(ctx context.Context)
}

// Authorizer asks the user, the first time, to allow notification banners. Every
// notify.Poster implements it.
type Authorizer interface {
	Authorize() (bool, error)
}

// Config holds a Shell's dependencies. Frontend, Window, Tray, App, Inbox, and Settings
// are required; the rest are optional.
type Config struct {
	Frontend Frontend
	Window   Window
	Tray     Tray
	App      App

	Inbox    Inbox
	Settings Settings

	// Workers run in the background from Start until Stop, started in this order.
	Workers []Worker

	// Data is what Stop closes last, once the background work is done: the database.
	Data io.Closer

	// Banners is asked at launch to allow notifications. Nil means this build cannot
	// show them, and nothing is asked.
	Banners Authorizer

	// Clock times the menu bar's snooze ends. It defaults to the system clock.
	Clock clock.Clock

	// Stdout gets a startup measurement's timing line. It defaults to discarding.
	Stdout io.Writer

	// Logger receives what the shell cannot report as an error: a setting it could not
	// read and fell back from, a failed refresh or menu bar redraw, and a startup
	// measurement that never painted. Without it they are not logged.
	Logger *slog.Logger

	// MeasureStartup makes this run a startup measurement: the first paint is printed to
	// Stdout and quits the app, a run that never paints quits failing after
	// MeasureTimeout, and nothing asks to allow banners.
	MeasureStartup bool

	// StopTimeout and MeasureTimeout default to DefaultStopTimeout and
	// DefaultMeasureTimeout.
	StopTimeout    time.Duration
	MeasureTimeout time.Duration
}

// Shell connects the services to the desktop runtime. It is the services' event.Sink:
// window events go on to the page, an inbox change redraws the menu bar, a theme or menu
// bar change reaches the window and the icon, and the first paint ends a startup
// measurement. It runs the background work, tracked so Stop can wait for it, and keeps
// the menu bar icon current with one redraw at a time.
//
// The desktop runtime calls Start once the app starts, Launched once it has launched,
// and Stop when it shuts down.
type Shell struct {
	frontend Frontend
	window   Window
	tray     Tray
	app      App
	inbox    Inbox
	settings Settings
	workers  []Worker
	data     io.Closer
	banners  Authorizer
	clock    clock.Clock
	stdout   io.Writer
	logger   *slog.Logger
	measure  bool

	stopTimeout    time.Duration
	measureTimeout time.Duration

	// lifetime is canceled when the shell stops, which ends the background work. The
	// shell lives as long as the app, so its work's context is part of it.
	lifetime context.Context //nolint:containedctx // the app's lifetime, which outlives every call
	cancel   context.CancelFunc
	unlink   func() bool

	// stopping is set once Stop begins, under mu; it is read without the lock too.
	stopping atomic.Bool

	// mu guards the fields below, and adds work only before Stop waits for it.
	mu       sync.Mutex
	started  bool
	work     sync.WaitGroup
	trayOn   bool
	snooze   *time.Timer
	measured *time.Timer

	// redraw holds one pending ask to redraw the menu bar: asks that come while one is
	// pending fold into it.
	redraw chan struct{}

	timedOut atomic.Bool
	stopOnce sync.Once
	closeErr error
}

// New returns a Shell over cfg that has not started.
func New(cfg Config) *Shell {
	lifetime, cancel := context.WithCancel(context.Background())

	s := &Shell{
		frontend: cfg.Frontend, window: cfg.Window, tray: cfg.Tray, app: cfg.App,
		inbox: cfg.Inbox, settings: cfg.Settings, workers: cfg.Workers, data: cfg.Data, banners: cfg.Banners,
		clock: clock.Or(cfg.Clock), stdout: orDiscard(cfg.Stdout), logger: logging.Or(cfg.Logger),
		measure: cfg.MeasureStartup, stopTimeout: cfg.StopTimeout, measureTimeout: cfg.MeasureTimeout,
		lifetime: lifetime, cancel: cancel, redraw: make(chan struct{}, 1),
	}

	if s.stopTimeout <= 0 {
		s.stopTimeout = DefaultStopTimeout
	}

	if s.measureTimeout <= 0 {
		s.measureTimeout = DefaultMeasureTimeout
	}

	return s
}

// Start starts the background work: the menu bar's redraws, then each worker in order.
// It runs once; a later call, or one after Stop, does nothing. When ctx ends, the work
// ends with it, as it does when the shell stops.
func (s *Shell) Start(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started || s.stopping.Load() {
		return
	}

	s.started = true
	s.unlink = context.AfterFunc(ctx, s.cancel)

	s.goLocked(s.redrawTray)

	for _, w := range s.workers {
		s.goLocked(w.Run)
	}
}

// Stop shuts the shell down, once, in this order: it drops every later event, stops the
// snooze and measurement timers, ends the background work and waits for it, up to the
// stop timeout, and then closes the data. It returns the error closing the data
// returned, on this and every later call. It is safe to call without Start.
func (s *Shell) Stop() error {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.stopping.Store(true)
		stopTimer(&s.snooze)
		stopTimer(&s.measured)
		unlink := s.unlink
		s.mu.Unlock()

		if unlink != nil {
			unlink()
		}

		s.cancel()
		s.waitForWork()

		if s.data != nil {
			s.closeErr = s.data.Close()
		}
	})

	return s.closeErr
}

// Emit acts on e, until the shell stops: a window event goes on to the page, an inbox
// change also redraws the menu bar, a request to reveal an item brings the window
// forward, a theme or menu bar change reaches the window or the icon, and the first paint
// ends a startup measurement. It never waits for a redraw.
func (s *Shell) Emit(e event.Event) {
	if s.stopping.Load() {
		return
	}

	switch e := e.(type) {
	case event.InboxUpdated:
		s.frontend.Emit(e)
		s.askRedraw()
	case event.RevealItem:
		s.reveal(e)
	case event.ThemeChanged:
		s.window.SetTheme(settings.Theme(e.Theme))
	case event.MenuBarChanged:
		s.setMenuBar(e.On)
	case event.FirstPaint:
		s.firstPaint(e.Measurement)
	case event.WindowEvent:
		s.frontend.Emit(e)
	}
}

// Launched opens the window in the stored theme and at the stored size, shows the menu
// bar icon when it is on, asks once to allow banners, and starts a startup measurement's
// timeout. The desktop runtime calls it once the app has launched, when the system
// appearance the System theme follows is known. A setting that cannot be read falls back
// to its default, with a note on Stderr.
func (s *Shell) Launched() {
	if s.stopping.Load() {
		return
	}

	s.window.Open(s.storedTheme(), s.storedWindowSize())

	if s.storedMenuBar() {
		s.setMenuBar(true)
	}

	// The question waits for the user, for minutes if need be, and touches nothing Stop
	// closes, so it runs apart from the tracked work: quitting never waits for it.
	if s.banners != nil && !s.measure {
		go s.askForBanners()
	}

	if s.measure {
		s.startMeasureTimeout()
	}
}

// ShowWindow brings the window forward, unless the shell is stopping.
func (s *Shell) ShowWindow() {
	if s.stopping.Load() {
		return
	}

	s.window.Show()
}

// RefreshNow refreshes the inbox from GitHub in the background, as the menu bar's Refresh
// Now asks; Stop waits for it. The refresh says when it is stored, and records a failure
// for the window and the menu bar to show, which is also logged.
func (s *Shell) RefreshNow() {
	s.goTracked(func(ctx context.Context) {
		if _, err := s.inbox.Refresh(ctx); err != nil && ctx.Err() == nil {
			s.logger.WarnContext(ctx, "the menu bar's inbox refresh failed", "error", err)
		}
	})
}

// Quit quits the app.
func (s *Shell) Quit() {
	s.app.Quit()
}

// HidesOnClose reports whether closing the window only hides it, keeping the app running
// in the menu bar: while the icon shows and the app is not quitting.
func (s *Shell) HidesOnClose() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.trayOn && !s.stopping.Load()
}

// Stopping reports whether Stop has begun. Work the desktop runtime queued before then
// checks it, so nothing is drawn once the app is shutting down.
func (s *Shell) Stopping() bool {
	return s.stopping.Load()
}

// TimedOut reports whether a startup measurement gave up waiting for the first paint;
// the run then exits with a failure status.
func (s *Shell) TimedOut() bool {
	return s.timedOut.Load()
}

// goTracked runs fn in the background under the shell's lifetime, tracked so Stop waits
// for it. Once the shell is stopping it runs nothing.
func (s *Shell) goTracked(fn func(ctx context.Context)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.goLocked(fn)
}

// goLocked is goTracked for a caller that holds mu.
func (s *Shell) goLocked(fn func(ctx context.Context)) {
	if s.stopping.Load() {
		return
	}

	s.work.Go(func() { fn(s.lifetime) })
}

// waitForWork waits for the tracked work to return, up to the stop timeout.
func (s *Shell) waitForWork() {
	done := make(chan struct{})

	go func() {
		s.work.Wait()
		close(done)
	}()

	timeout := time.NewTimer(s.stopTimeout)
	defer timeout.Stop()

	select {
	case <-done:
	case <-timeout.C:
	}
}

// reveal brings the window forward and, when the request names an item or its URL, asks
// the page to show it.
func (s *Shell) reveal(r event.RevealItem) {
	s.window.Show()

	if r.ItemID == 0 && r.URL == "" {
		return
	}

	s.frontend.Emit(r)
}

// storedTheme is the stored theme, or the default when it cannot be read.
func (s *Shell) storedTheme() settings.Theme {
	theme, err := s.settings.Theme(s.lifetime)
	if err != nil {
		s.logger.Warn("could not read the theme; following the system", "error", err)

		return settings.DefaultTheme
	}

	return theme
}

// storedWindowSize is the stored window size, or the default when it cannot be read.
func (s *Shell) storedWindowSize() settings.WindowSize {
	size, err := s.settings.WindowSize(s.lifetime)
	if err != nil {
		s.logger.Warn("could not read the window size; opening at the default", "error", err)

		return settings.DefaultWindowSize()
	}

	return size
}

// storedMenuBar is the stored menu bar setting, or the default when it cannot be read.
func (s *Shell) storedMenuBar() bool {
	on, err := s.settings.MenuBar(s.lifetime)
	if err != nil {
		s.logger.Warn("could not read the menu bar setting; showing the icon", "error", err)

		return settings.DefaultMenuBar
	}

	return on
}

// askForBanners asks the user, the first time, to allow notifications.
func (s *Shell) askForBanners() {
	if _, err := s.banners.Authorize(); err != nil {
		s.logger.Warn("could not ask to show notifications", "error", err)
	}
}

// startMeasureTimeout quits a startup measurement that has not painted in time.
func (s *Shell) startMeasureTimeout() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopping.Load() || s.measured != nil {
		return
	}

	s.measured = time.AfterFunc(s.measureTimeout, s.measureTimedOut)
}

// measureTimedOut quits a startup measurement that never painted, failing.
func (s *Shell) measureTimedOut() {
	if s.stopping.Load() {
		return
	}

	s.timedOut.Store(true)
	s.logger.Error("the startup measurement failed", "error", ErrNoFirstPaint)
	s.app.Quit()
}

// firstPaint ends a startup measurement: it prints the timing line and quits.
func (s *Shell) firstPaint(m timing.Measurement) {
	if !s.measure {
		return
	}

	_, _ = fmt.Fprintln(s.stdout, m)

	s.app.Quit()
}

// stopTimer stops the timer *t, if one is set, and clears it.
func stopTimer(t **time.Timer) {
	if *t != nil {
		(*t).Stop()
		*t = nil
	}
}

// orDiscard returns w, or io.Discard for nil.
func orDiscard(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}

	return w
}
