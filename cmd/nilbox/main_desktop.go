//go:build desktop

// Command nilbox is the application entry point.
//
// This file is the desktop build: one window whose frontend is embedded from
// frontend/dist and talks to the core services through generated bindings. Everything
// except the window itself is assembled by internal/core, which builds and is tested on
// any OS; this file only adds the desktop runtime.
//
// Two flags measure the app for docs/performance.md: --measure-startup quits right after
// the first paint and prints the startup timing line, and --measure-refresh opens no
// window, times live inbox refreshes against a scratch database, and prints the report.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/mrz1836/nilbox/frontend"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/core"
	"github.com/mrz1836/nilbox/internal/measure"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/timing"
	"github.com/mrz1836/nilbox/internal/worktrees"
)

// Build metadata, injected at build time via -ldflags (see magefiles/app.go).
//
//nolint:gochecknoglobals // set by the linker
var (
	version     = "dev"
	commit      = "none"
	buildDate   = "unknown"
	release     = "0.0.0"
	buildNumber = "0"
)

const (
	// inboxUpdatedEvent tells the frontend the stored inbox changed.
	inboxUpdatedEvent = "inbox:updated"

	// worktreesUpdatedEvent tells the frontend an automatic worktree sweep removed a
	// worktree or changed how many need attention.
	worktreesUpdatedEvent = "worktrees:updated"

	// measureTimeout bounds a measurement run whose window never reports a first paint.
	measureTimeout = time.Minute

	// schedulerStopTimeout bounds how long shutdown waits for an in-flight refresh.
	schedulerStopTimeout = 5 * time.Second
)

var (
	// errNoFirstPaint is reported when a measurement run timed out.
	errNoFirstPaint = errors.New("the window did not report its first paint in time")

	// errServicesUnbound guards against a core service that is not bound to the window.
	errServicesUnbound = errors.New("every core service must be bound to the window")
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)

		os.Exit(1)
	}
}

// run assembles the core, then runs the window until the app quits. On macOS the app
// exits from inside Run once it terminates, so cleanup happens in the shutdown hook.
func run(args []string, stdout, stderr io.Writer) error {
	paths, err := appdata.Resolve()
	if err != nil {
		return fmt.Errorf("find the data directory: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find the home directory: %w", err)
	}

	if slices.Contains(args, measure.RefreshFlag) {
		return measureRefresh(stdout, home)
	}

	s := &shell{stdout: stdout, stderr: stderr, measureStartup: slices.Contains(args, measure.StartupFlag)}

	c, err := core.Bootstrap(context.Background(), core.Options{
		Paths:            paths,
		Home:             home,
		Version:          version,
		Release:          release,
		Build:            buildNumber,
		Notifier:         s,
		WorktreeNotifier: s,
		ThemeNotifier:    s,
		OnFirstPaint:     s.firstPaint,
	})
	if err != nil {
		return fmt.Errorf("start the app (%s, commit %s, built %s): %w", version, commit, buildDate, err)
	}

	s.core = c

	bound := services(c)
	if len(bound) != len(c.Services()) {
		_ = c.Close()

		return errServicesUnbound
	}

	app := application.New(application.Options{
		Name:         brand.Name,
		Description:  brand.Tagline,
		Icon:         appIcon(),
		Services:     bound,
		Assets:       application.AssetOptions{Handler: application.AssetFileServerFS(frontend.Assets)},
		Mac:          macAppOptions(),
		OnShutdown:   s.shutdown,
		PostShutdown: s.postShutdown,
	})
	s.app.Store(app)

	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		s.started()
	})

	app.Event.OnApplicationEvent(events.Common.ThemeChanged, func(*application.ApplicationEvent) {
		s.systemThemeChanged()
	})

	runErr := app.Run()

	s.shutdown()

	return errors.Join(runErr, s.closeErr)
}

// measureRefresh times live inbox refreshes the way the app makes them, against a scratch
// database that is deleted afterwards, and prints the report. It opens no window.
func measureRefresh(stdout io.Writer, home string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	report, err := measure.RunRefresh(ctx, measure.RefreshOptions{Home: home, Version: version})
	if err != nil {
		return fmt.Errorf("measure the inbox refresh: %w", err)
	}

	for _, line := range report.Lines() {
		if _, err = fmt.Fprintln(stdout, line); err != nil {
			return fmt.Errorf("write the report: %w", err)
		}
	}

	return nil
}

// services wraps each core service for binding. The bindings generator reads these
// calls, so every service is named here by its concrete type.
func services(c *core.Core) []application.Service {
	return []application.Service{
		application.NewService(c.Settings),
		application.NewService(c.Profiles),
		application.NewService(c.Prompts),
		application.NewService(c.LaunchDefaults),
		application.NewService(c.Inbox),
		application.NewService(c.Launcher),
		application.NewService(c.Worktrees),
		application.NewService(c.AppInfo),
	}
}

// baseWindowOptions are the window settings shared by every OS, for a window of the given
// size in points, on the native background of the dark or light palette, so the frame
// never flashes a different color before the page paints.
func baseWindowOptions(dark bool, size settings.WindowSize) application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Title:            brand.Name,
		Width:            size.Width,
		Height:           size.Height,
		MinWidth:         settings.MinWindowWidth,
		MinHeight:        settings.MinWindowHeight,
		URL:              "/",
		BackgroundColour: backgroundColour(dark),
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

// shell connects the core to the window: it opens the window and starts the inbox
// scheduler once the app has started, forwards inbox changes as events, keeps the native
// appearance in the chosen theme, ends a startup measurement, and closes the core on
// shutdown.
type shell struct {
	app            atomic.Pointer[application.App]
	core           *core.Core
	stdout         io.Writer
	stderr         io.Writer
	measureStartup bool

	// appearance guards theme and window, so a theme the user chose and a change of the
	// system appearance are applied one at a time, in order.
	appearance sync.Mutex
	theme      string
	window     *application.WebviewWindow

	scheduler sync.WaitGroup
	stopping  atomic.Bool
	timedOut  atomic.Bool
	stopOnce  sync.Once
	closeErr  error
}

// InboxUpdated tells the frontend to reload the inbox.
func (s *shell) InboxUpdated() {
	if s.stopping.Load() {
		return
	}

	if app := s.app.Load(); app != nil {
		app.Event.Emit(inboxUpdatedEvent)
	}
}

// WorktreesUpdated tells the frontend to read the latest worktree sweep.
func (s *shell) WorktreesUpdated() {
	if s.stopping.Load() {
		return
	}

	if app := s.app.Load(); app != nil {
		app.Event.Emit(worktreesUpdatedEvent)
	}
}

// ThemeChanged brings the native appearance and the window background along with a
// theme the user chose; the page has already switched itself.
func (s *shell) ThemeChanged(theme string) {
	if s.stopping.Load() {
		return
	}

	s.appearance.Lock()
	defer s.appearance.Unlock()

	s.theme = theme
	applyAppearance(theme)
	s.paintBackground()
}

// systemThemeChanged repaints the window background when the system appearance changes,
// which the System theme follows. AppKit and the page follow the system on their own.
func (s *shell) systemThemeChanged() {
	if s.stopping.Load() {
		return
	}

	s.appearance.Lock()
	defer s.appearance.Unlock()

	s.paintBackground()
}

// paintBackground sets the window's native background, which shows while the page has
// not painted, such as at the edges of a live resize, to the palette the theme shows now.
// The caller holds s.appearance.
func (s *shell) paintBackground() {
	app := s.app.Load()
	if app == nil || s.window == nil {
		return
	}

	s.window.SetBackgroundColour(backgroundColour(settings.IsDark(s.theme, app.Env.IsDarkMode())))
}

// started opens the window in the theme and at the size the settings hold, and starts
// refreshing the inbox. The system appearance, which the System theme follows, is known
// only once the app runs.
func (s *shell) started() {
	app := s.app.Load()
	size := s.windowSize(app)

	s.appearance.Lock()
	s.theme = s.storedTheme(app)
	// Pinning the appearance before the window exists is what keeps its first frame from
	// flashing the other palette: the page's prefers-color-scheme already matches.
	applyAppearance(s.theme)
	s.window = app.Window.NewWithOptions(windowOptions(settings.IsDark(s.theme, app.Env.IsDarkMode()), size))
	s.appearance.Unlock()

	ctx := app.Context()

	s.scheduler.Go(func() { s.core.Scheduler.Run(ctx) })
	s.scheduler.Go(func() { worktrees.NewSweeper(s.core.Worktrees).Run(ctx, worktrees.DefaultSweepInterval) })

	if s.measureStartup {
		time.AfterFunc(measureTimeout, s.measureTimedOut)
	}
}

// windowSize is the size the window opens at: the one the settings hold, or the default
// when they cannot be read, shrunk where needed to fit the primary screen's work area.
func (s *shell) windowSize(app *application.App) settings.WindowSize {
	size := settings.DefaultWindowSize()

	prefs, err := s.core.Settings.Get(app.Context())
	if err == nil {
		size = prefs.WindowSize
	} else {
		_, _ = fmt.Fprintln(s.stderr, "read the window size, opening at the default:", err)
	}

	if screen := app.Screen.GetPrimary(); screen != nil {
		size = size.Fit(screen.WorkArea.Width, screen.WorkArea.Height)
	}

	return size
}

// storedTheme is the theme the settings hold, or the default when they cannot be read.
func (s *shell) storedTheme(app *application.App) string {
	theme, err := s.core.Settings.Theme(app.Context())
	if err != nil {
		_, _ = fmt.Fprintln(s.stderr, "read the theme, following the system:", err)

		return settings.DefaultTheme
	}

	return theme
}

// firstPaint ends a measurement run: it prints the timing line and quits.
func (s *shell) firstPaint(m timing.Measurement) {
	if !s.measureStartup {
		return
	}

	_, _ = fmt.Fprintln(s.stdout, m)

	if app := s.app.Load(); app != nil {
		app.Quit()
	}
}

// measureTimedOut quits a measurement run that never painted.
func (s *shell) measureTimedOut() {
	s.timedOut.Store(true)

	_, _ = fmt.Fprintln(s.stderr, errNoFirstPaint)

	if app := s.app.Load(); app != nil {
		app.Quit()
	}
}

// shutdown stops the scheduler (the app's context is already canceled) and closes the
// database. It runs once, whichever of the shutdown hook and Run's return comes first.
func (s *shell) shutdown() {
	s.stopOnce.Do(func() {
		s.stopping.Store(true)

		done := make(chan struct{})

		go func() {
			s.scheduler.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(schedulerStopTimeout):
		}

		s.closeErr = s.core.Close()
	})
}

// postShutdown makes a timed-out measurement run exit with a failure status.
func (s *shell) postShutdown() {
	if s.timedOut.Load() {
		os.Exit(1)
	}
}
