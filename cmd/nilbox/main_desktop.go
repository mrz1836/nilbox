//go:build desktop

// Command nilbox is the application entry point.
//
// This file is the desktop build: one window whose frontend is embedded from
// frontend/dist and talks to the core services through generated bindings. Everything
// except the window itself is assembled by internal/core, and everything the shell
// decides is in internal/desktop, both of which build and are tested on any OS; the
// files here only connect them to the desktop runtime.
//
// Two flags measure the app for docs/performance.md: --measure-startup quits right after
// the first paint and prints the startup timing line, and --measure-refresh opens no
// window, times live inbox refreshes against a scratch database, and prints the report.
// Those lines are the only output on stdout. Everything else the app has to say goes to
// its one logger (internal/logging): text on stderr, and JSON in the log file. --debug
// keeps Debug records too.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/mrz1836/nilbox/frontend"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/core"
	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/measure"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	releases "github.com/mrz1836/nilbox/internal/release"
	"github.com/mrz1836/nilbox/internal/review"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/settings"
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

// errUnbound is returned for a core service no binder wraps.
var errUnbound = errors.New("the desktop bootstrap has no binder for a core service; add one to binders")

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

// run builds the app's logger, then starts the app and runs it until it quits. It returns
// the status the process exits with: 0, or 1 once the error that ended the run is logged.
// Without a data directory there is no log file, so that failure reaches stderr alone.
func run(args []string, stdout io.Writer) int {
	paths, err := appdata.Resolve()

	logger, logs := logging.New(paths, logging.Level(args, os.Getenv))
	defer func() { _ = logs.Close() }()

	if err != nil {
		err = startFailed(args, logger, fmt.Errorf("find the data directory: %w", err))
	} else {
		err = start(args, stdout, paths, logger)
	}

	if err != nil {
		logger.Error("the app stopped with an error", "error", err)

		return 1
	}

	return 0
}

// start assembles the core and the shell, then runs the window until the app quits. On
// macOS the app exits from inside Run once it terminates, so the shell stops in the
// runtime's shutdown hook. An error that stops the app from starting is shown in a
// native dialog first, except in a measurement run.
func start(args []string, stdout io.Writer, paths appdata.Paths, logger *slog.Logger) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return startFailed(args, logger, fmt.Errorf("find the home directory: %w", err))
	}

	if slices.Contains(args, measure.RefreshFlag) {
		return measureRefresh(stdout, home, logger)
	}

	app, shell, err := assemble(paths, home, stdout, logger, slices.Contains(args, measure.StartupFlag))
	if err != nil {
		return startFailed(args, logger, err)
	}

	runErr := app.Run()

	return errors.Join(runErr, shell.Stop())
}

// assemble bootstraps the core, builds the shell over it, and creates the app, which
// starts and stops the shell with its own lifecycle.
func assemble(
	paths appdata.Paths, home string, stdout io.Writer, logger *slog.Logger, measureStartup bool,
) (*application.App, *desktop.Shell, error) {
	// The services are built before the shell that hears their events, which needs them;
	// the relay carries the events once the shell is connected.
	relay := &event.Relay{}
	banners := newBanners(logger, relay)

	c, err := core.Bootstrap(context.Background(), core.Options{
		Paths:   paths,
		Home:    home,
		Version: version,
		Release: release,
		Build:   buildNumber,
		Events:  relay,
		Banners: banners.poster(),
		Logger:  logger,
	})
	if err != nil {
		return nil, nil, err
	}

	bound, err := services(c)
	if err != nil {
		_ = c.Close()

		return nil, nil, err
	}

	ui := &desktopUI{}
	shell := desktop.New(desktop.Config{
		Frontend:       ui,
		Window:         ui,
		Tray:           &tray{ui: ui},
		App:            ui,
		Inbox:          c.Inbox,
		Settings:       c.ShellSettings,
		Sessions:       c.ShellSessions,
		Workers:        []desktop.Worker{c.Scheduler, c.Sweeper, c.BudgetSaver, c.Watcher, c.ReleaseRefresher},
		Deferred:       []desktop.Worker{c.Deferred, c.ToolFinder, c.Runs},
		Data:           c,
		Banners:        banners.poster(),
		QuitGuard:      c.Runs,
		Stdout:         stdout,
		Logger:         logger,
		MeasureStartup: measureStartup,
	})
	relay.Connect(shell)

	registerEvents()

	app := application.New(application.Options{
		Name:        brand.Name,
		Description: brand.Tagline,
		Icon:        appIcon(),
		Services:    append(bound, application.NewService(&lifecycle{of: shell})),
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(frontend.Assets)},
		Mac:         macAppOptions(),
		// A bound method's error reaches the page with what the user is told about it.
		MarshalError: desktop.MarshalError,
		// Every quit asks first, while runs are going in the app.
		ShouldQuit: shell.ShouldQuit,
		// A timed-out measurement run exits with a failure status. On macOS the app exits
		// from inside Run, with a success status unless this hook exits first.
		PostShutdown: func() {
			if shell.TimedOut() {
				os.Exit(1)
			}
		},
	})
	ui.attach(app, shell)

	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		shell.Launched()
	})

	// A click on the Dock icon brings back a window closed into the menu bar.
	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		shell.ShowWindow()
	})

	app.Event.OnApplicationEvent(events.Common.ThemeChanged, func(*application.ApplicationEvent) {
		ui.systemThemeChanged()
	})

	return app, shell, nil
}

// registerEvents registers each window event's data type under its name. The runtime
// then checks the data of every event the shell emits, and the bindings generator writes
// the frontend's event types from these calls, which it finds only as direct calls with
// constant names.
func registerEvents() {
	application.RegisterEvent[event.InboxUpdated](event.NameInboxUpdated)
	application.RegisterEvent[event.RevealItem](event.NameRevealItem)
	application.RegisterEvent[event.WorktreesUpdated](event.NameWorktreesUpdated)
	application.RegisterEvent[event.NotificationsUpdated](event.NameNotificationsUpdated)
	application.RegisterEvent[event.BudgetUpdated](event.NameBudgetUpdated)
	application.RegisterEvent[event.SessionsUpdated](event.NameSessionsUpdated)
	application.RegisterEvent[event.ReviewProgress](event.NameReviewProgress)
	application.RegisterEvent[event.ToolsUpdated](event.NameToolsUpdated)
	application.RegisterEvent[event.ScriptRunsUpdated](event.NameScriptRunsUpdated)
	application.RegisterEvent[event.ScriptRunProgress](event.NameScriptRunProgress)
	application.RegisterEvent[event.QuitRequested](event.NameQuitRequested)
	application.RegisterEvent[event.ReleasesUpdated](event.NameReleasesUpdated)
	application.RegisterEvent[event.ReleasesProgress](event.NameReleasesProgress)
}

// services wraps every service the core binds, in the core's order. A service no binder
// wraps stops the app from starting, naming its type, so a service added to the core is
// never left unbound.
func services(c *core.Core) ([]application.Service, error) {
	all := c.Services()
	out := make([]application.Service, 0, len(all)+1)

	for _, svc := range all {
		bound, err := bind(svc)
		if err != nil {
			return nil, err
		}

		out = append(out, bound)
	}

	return out, nil
}

// bind wraps svc with the binder for its type.
func bind(svc any) (application.Service, error) {
	for _, wrap := range binders() {
		if bound, ok := wrap(svc); ok {
			return bound, nil
		}
	}

	return application.Service{}, fmt.Errorf("%w: %T", errUnbound, svc)
}

// binders wraps each type of service the core binds. The bindings generator finds a
// bound service by its call to application.NewService, which needs the service's
// concrete type, so each type is named here once; which services are bound, and in what
// order, is the core's to say.
func binders() []func(svc any) (application.Service, bool) {
	return []func(svc any) (application.Service, bool){
		binder[settings.Service], binder[profiles.Service], binder[prompts.Service],
		binder[launchdefaults.Service], binder[inbox.Service], binder[launcher.Service],
		binder[worktrees.Service], binder[notify.Service], binder[appinfo.Service], binder[budget.Service],
		binder[sessions.Service], binder[review.Service], binder[integrations.Service], binder[scriptrun.Service],
		binder[releases.Service],
	}
}

// binder wraps svc for binding when it is a *T.
func binder[T any](svc any) (application.Service, bool) {
	s, ok := svc.(*T)
	if !ok {
		return application.Service{}, false
	}

	return application.NewService(s), true
}

// startFailed returns err, which stopped the app from starting, with the build's details.
// Outside a measurement run it first logs the error, shows it in a native dialog, and
// exits with a failure status once the dialog is dismissed.
func startFailed(args []string, logger *slog.Logger, err error) error {
	wrapped := fmt.Errorf("start the app (%s, commit %s, built %s): %w", version, commit, buildDate, err)

	if !slices.Contains(args, measure.StartupFlag) && !slices.Contains(args, measure.RefreshFlag) {
		failStartup(logger, wrapped, desktop.StartupFailure(err))
	}

	return wrapped
}

// failStartup logs err, shows failure in a native error dialog, since nobody sees stderr
// when the app is launched from Finder, and exits with a failure status once the dialog
// is dismissed. The log file is written unbuffered, so exiting without closing it loses
// nothing. The dialog needs a running app, so a bare one, with no window and no services,
// runs just to show it.
func failStartup(logger *slog.Logger, err error, failure desktop.Failure) {
	logger.Error("the app could not start", "error", err)

	app := application.New(application.Options{
		Name:        brand.Name,
		Description: brand.Tagline,
		Icon:        appIcon(),
		// On macOS the app exits from inside Run once it terminates, with a success
		// status unless this hook exits first.
		PostShutdown: func() { os.Exit(1) },
	})

	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		dialog := app.Dialog.Error().SetTitle(failure.Title).SetMessage(failure.Message)
		dialog.AddButton("Quit").SetAsDefault().OnClick(app.Quit)
		dialog.Show()
	})

	_ = app.Run()

	os.Exit(1)
}

// measureRefresh times live inbox refreshes the way the app makes them, against a scratch
// database that is deleted afterwards, and prints the report. It opens no window.
func measureRefresh(stdout io.Writer, home string, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	report, err := measure.RunRefresh(ctx, measure.RefreshOptions{Home: home, Version: version, Logger: logger})
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

// lifecycle hands the desktop runtime's service hooks to something with a plain Go
// lifecycle: Start when the app starts, and Stop when it shuts down. It binds no methods.
type lifecycle struct {
	of interface {
		Start(ctx context.Context)
		Stop() error
	}
}

// ServiceStartup starts it with the app's context, which ends when the app shuts down.
//
//nolint:unparam // the runtime's hook: its signature is not ours to choose
func (l *lifecycle) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	l.of.Start(ctx)

	return nil
}

// ServiceShutdown stops it, reporting what Stop returns.
func (l *lifecycle) ServiceShutdown() error {
	return l.of.Stop()
}
