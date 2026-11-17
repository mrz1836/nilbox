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
// keeps Debug records too. --data-dir=<absolute path> runs the app on that data directory,
// logs included, in place of the user's own: a sandbox, as magex app:fresh opens. A
// rehearsal build (magex app:rehearse) always runs on its own data directory, beside the
// user's, whatever its arguments say: the update it rehearses opens the new version with
// none.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/updater"

	"github.com/mrz1836/nilbox/frontend"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/core"
	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/ghthreads"
	"github.com/mrz1836/nilbox/internal/hooks"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/issues"
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
	"github.com/mrz1836/nilbox/internal/scripts"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/setup"
	"github.com/mrz1836/nilbox/internal/svcstatus"
	"github.com/mrz1836/nilbox/internal/update"
	"github.com/mrz1836/nilbox/internal/worktrees"
)

// Build metadata, injected at build time via -ldflags (see magefiles/bundle.go).
//
//nolint:gochecknoglobals // set by the linker
var (
	version     = "dev"
	commit      = "none"
	buildDate   = "unknown"
	release     = "0.0.0"
	buildNumber = "0"

	// flavor is the kind of build: "release" for one magex app:package made, "rehearsal"
	// for one magex app:rehearse made, and empty for one made from source (magex app:build,
	// app:run, app:dev), which stamps none.
	flavor string

	// updateFeed is the folder a rehearsal build reads its releases from, and updateKey the
	// public key, base64, it trusts them with: magex app:rehearse stamps both, and every
	// other flavor ignores them.
	updateFeed string
	updateKey  string
)

// errUnbound is returned for a core service no binder wraps.
var errUnbound = errors.New("the desktop bootstrap has no binder for a core service; add one to binders")

func main() {
	// An update's helper is this binary run again, to swap the bundles once the app it
	// replaces has quit: it does that here and exits, before anything reads the arguments
	// or opens the data that app may still hold.
	updater.HandleHelperMode()

	// A fenced agent run's hook is this binary run again, before each of the agent's tool
	// calls: it decides the call against the run's policy and exits, before anything reads
	// a setting, opens the data, starts logging, or touches the window.
	if code, ok := fence.RunHook(os.Args[1:], os.Stdin, os.Stderr); ok {
		os.Exit(code)
	}

	os.Exit(run(os.Args[1:], os.Stdout))
}

// run builds the app's logger, then starts the app and runs it until it quits. It returns
// the status the process exits with: 0, or 1 once the error that ended the run is logged.
// Without a data directory there is no log file, so that failure reaches stderr alone.
func run(args []string, stdout io.Writer) int {
	paths, err := dataPaths(args)

	logger, logs := logging.New(paths, logging.Level(args, os.Getenv))
	defer func() { _ = logs.Close() }()

	if err != nil {
		err = startFailed(args, logger, fmt.Errorf("find the data directory: %w", err), "")
	} else {
		err = start(args, stdout, paths, logger)
	}

	if err != nil {
		logger.Error("the app stopped with an error", "error", err)

		return 1
	}

	return 0
}

// dataPaths are the data locations the run uses: a rehearsal build's own, beside the
// user's, whatever args say; the data directory args name with --data-dir, a sandbox's; or
// else the user's own.
func dataPaths(args []string) (appdata.Paths, error) {
	if rehearsal() {
		return appdata.ResolveRehearsal()
	}

	dir, err := appdata.FromArgs(args)

	switch {
	case err != nil:
		return appdata.Paths{}, err
	case dir != "":
		return appdata.NewPaths(dir), nil
	default:
		return appdata.Resolve()
	}
}

// sandboxDir is the data directory args name with --data-dir, or "" for the user's own,
// or a rehearsal build's, which finds its own and so needs none passed when it opens
// again.
func sandboxDir(args []string) string {
	if rehearsal() {
		return ""
	}

	dir, _ := appdata.FromArgs(args)

	return dir
}

// rehearsal reports whether this is a rehearsal build, which runs on its own data.
func rehearsal() bool {
	return update.Flavor(flavor) == update.FlavorRehearsal
}

// start assembles the core and the shell, then runs the window until the app quits. On
// macOS the app exits from inside Run once it terminates, so the shell stops in the
// runtime's shutdown hook. An error that stops the app from starting is shown in a
// native dialog first, except in a measurement run.
func start(args []string, stdout io.Writer, paths appdata.Paths, logger *slog.Logger) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return startFailed(args, logger, fmt.Errorf("find the home directory: %w", err), "")
	}

	if slices.Contains(args, measure.RefreshFlag) {
		return measureRefresh(stdout, home, measure.SearchesArg(args), logger)
	}

	app, shell, err := assemble(launch{
		paths: paths, home: home, sandbox: sandboxDir(args), stdout: stdout, logger: logger,
		measureStartup: slices.Contains(args, measure.StartupFlag),
	})
	if err != nil {
		return startFailed(args, logger, err, paths.DB)
	}

	runErr := app.Run()

	return errors.Join(runErr, shell.Stop())
}

// launch is what one run of the app starts from: its data locations, the home directory,
// a sandbox's data directory ("" for the user's own), where a startup measurement prints,
// the logger, and whether this run is a startup measurement.
type launch struct {
	paths          appdata.Paths
	home           string
	sandbox        string
	stdout         io.Writer
	logger         *slog.Logger
	measureStartup bool
}

// assemble bootstraps the core, builds the shell over it, and creates the app, which
// starts and stops the shell with its own lifecycle.
func assemble(l launch) (*application.App, *desktop.Shell, error) {
	// The services are built before the shell that hears their events, which needs them;
	// the relay carries the events once the shell is connected.
	relay := &event.Relay{}
	banners := newBanners(l.logger, relay)
	logger := l.logger

	c, err := core.Bootstrap(context.Background(), core.Options{
		Paths:   l.paths,
		Home:    l.home,
		Version: version,
		Release: release,
		Build:   buildNumber,
		Events:  relay,
		Banners: banners.poster(),
		Logger:  logger,
		// A rehearsal build runs on test data of its own, which the window says, as a
		// sandbox's.
		Sandbox:    l.sandbox != "" || rehearsal(),
		Flavor:     update.Flavor(buildFlavor()),
		UpdateFeed: updateFeed,
		UpdateKey:  updateKey,
		Bundle:     update.BundleOf(executable()),
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
	workers := []desktop.Worker{
		c.WatchPasses, c.Scheduler, c.GHThreadsSyncer, c.ChecksFollower, c.Sweeper, c.BudgetSaver, c.Watcher, c.Rereads,
		c.ReleaseRefresher, c.ReleaseChecksFollower, c.HooksScans,
	}
	shell := desktop.New(desktop.Config{
		Frontend: ui,
		Window:   ui,
		Tray:     &tray{ui: ui},
		App:      ui,
		Inbox:    c.Inbox,
		Settings: c.ShellSettings,
		Sessions: c.ShellSessions,
		Workers:  workers,
		Deferred: []desktop.Worker{
			c.Deferred, c.ToolFinder, c.Runs, c.ReleaseFollower, c.ServiceStatusChecker, c.UpdateWorker,
			c.AutopilotEngine,
		},
		Updates:        c.Updates,
		Autopilot:      c.ShellAutopilot,
		Browser:        ui,
		Data:           c,
		Banners:        banners.poster(),
		QuitGuard:      c.Runs,
		Stdout:         l.stdout,
		Logger:         logger,
		MeasureStartup: l.measureStartup,
	})
	relay.Connect(shell)

	registerEvents()

	app := application.New(application.Options{
		Name:        brand.Name,
		Description: brand.Tagline,
		Icon:        appIcon(),
		Services:    append(bound, bindService(&lifecycle{of: shell})),
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(frontend.Assets)},
		Mac:         macAppOptions(),
		// The application's error marshaler too, for any error the runtime encodes without
		// a service's; a bound method's goes through its service's (boundOptions).
		MarshalError: desktop.MarshalError,
		// Every quit asks first, while runs are going in the app.
		ShouldQuit: shell.ShouldQuit,
		// A timed-out measurement run exits with a failure status. On macOS the app exits
		// from inside Run, with a success status unless this hook exits first, so run never
		// logs how it ended: the quit is logged here, which tells it from a crash in the log.
		// Once the shell has stopped and closed the database, the app opens again here: the
		// copy a move to Applications made or found, or, after a reset the user asked for,
		// this bundle, so the next start carries the reset out. Either opens once.
		PostShutdown: func() {
			if shell.TimedOut() {
				os.Exit(1)
			}

			logQuit(logger, shell.Stop())

			argv, ok := shell.RelaunchCommand(executable(), l.sandbox)

			switch {
			case ok:
				openAgain(argv, "the app quit to open again", logger)
			case shell.Relaunching():
				logger.Info("the app runs outside a bundle, so it only quits; open it again to finish the reset")
			}
		},
	})
	ui.attach(app, shell)
	attachInstaller(app, c.UpdateEngine, logger)
	attachMover(c.UpdateEngine, shell)

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

// logQuit logs that the app quit the normal way, once the shell has stopped, and whether
// the database closed in order (stopErr, what the shell's Stop returned, which it returns
// again on every later call).
func logQuit(logger *slog.Logger, stopErr error) {
	if stopErr != nil {
		logger.Warn("the app quit, but its data didn't close in order", "error", stopErr)

		return
	}

	logger.Info("the app quit")
}

// attachInstaller hands the update engine the installer it restarts with, over the
// runtime's updater, in a build that updates; a build that doesn't sets nothing up. An
// installer that can't be set up is logged, and a restart to update then says this build
// can't install updates.
func attachInstaller(app *application.App, engine *update.Engine, logger *slog.Logger) {
	if !engine.Enabled() {
		return
	}

	i, err := newInstaller(app.Updater, release)
	if err != nil {
		logger.Error("could not set up the installer; updates can't be installed", "error", err)

		return
	}

	engine.Attach(i)
}

// attachMover hands the update engine the mover it moves the app into Applications with,
// in a build that updates: ditto and codesign run by their absolute paths through execpath,
// and the copy opens through the shell once the app has quit the normal way.
func attachMover(engine *update.Engine, shell *desktop.Shell) {
	if !engine.Enabled() {
		return
	}

	engine.AttachMover(desktop.NewMover(execpath.Exec{}, shell))
}

// executable is the running executable, its links resolved, or "" when it can't be told.
func executable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}

	if resolved, resolveErr := filepath.EvalSymlinks(exe); resolveErr == nil {
		exe = resolved
	}

	return exe
}

// relaunchWithin is how long the relaunch's open may take: it runs as the app shuts down,
// on the main thread, and open returns as soon as Launch Services has the app.
const relaunchWithin = 5 * time.Second

// relaunch opens the app again once it has quit, on the sandbox's data directory when it
// ran on one: after a reset the user asked for, and after putting a backup back in place
// of data a newer build saved; after names which, for the log. A run outside a .app
// bundle, as `magex app:dev`'s, only quits: its next start carries the change out. open
// gets relaunchWithin, so it never holds the shutdown it runs in, on the main thread.
func relaunch(sandbox, after string, logger *slog.Logger) {
	argv, ok := desktop.RelaunchArgs(executable(), sandbox)
	if !ok {
		logger.Info("the app runs outside a bundle, so it only quits; open it again to finish", "after", after)

		return
	}

	openAgain(argv, after, logger)
}

// openAgain runs argv, the command that opens the app again once it has quit (open -n),
// within relaunchWithin, and logs a failure; after names why, for the log. It runs as the
// app shuts down, on the main thread, so the bound keeps it from holding that shutdown.
func openAgain(argv []string, after string, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), relaunchWithin)
	defer cancel()

	if _, _, err := (execpath.Exec{}).Run(ctx, argv[0], argv[1:]...); err != nil {
		logger.Warn("could not open the app again; open it to finish", "after", after, "error", err)
	}
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
	application.RegisterEvent[event.IssueProgress](event.NameIssueProgress)
	application.RegisterEvent[event.ToolsUpdated](event.NameToolsUpdated)
	application.RegisterEvent[event.ScriptRunsUpdated](event.NameScriptRunsUpdated)
	application.RegisterEvent[event.ScriptRunProgress](event.NameScriptRunProgress)
	application.RegisterEvent[event.QuitRequested](event.NameQuitRequested)
	application.RegisterEvent[event.ReleasesUpdated](event.NameReleasesUpdated)
	application.RegisterEvent[event.ReleasesProgress](event.NameReleasesProgress)
	application.RegisterEvent[event.HooksUpdated](event.NameHooksUpdated)
	application.RegisterEvent[event.WorktreesProgress](event.NameWorktreesProgress)
	application.RegisterEvent[event.GHThreadsUpdated](event.NameGHThreadsUpdated)
	application.RegisterEvent[event.ServiceStatusUpdated](event.NameServiceStatusUpdated)
	application.RegisterEvent[event.UpdatesUpdated](event.NameUpdatesUpdated)
	application.RegisterEvent[event.AutopilotUpdated](event.NameAutopilotUpdated)
	application.RegisterEvent[event.UpdateRestartRequested](event.NameUpdateRestartRequested)
	application.RegisterEvent[event.KeptRunning](event.NameKeptRunning)
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
// bound service by its call to application.NewServiceWithOptions, which needs the
// service's concrete type, so each type is named here once; which services are bound, and
// in what order, is the core's to say.
func binders() []func(svc any) (application.Service, bool) {
	return []func(svc any) (application.Service, bool){
		binder[settings.Service], binder[profiles.Service], binder[prompts.Service],
		binder[launchdefaults.Service], binder[inbox.Service], binder[launcher.Service],
		binder[worktrees.Service], binder[notify.Service], binder[appinfo.Service], binder[budget.Service],
		binder[sessions.Service], binder[review.Service], binder[issues.Service], binder[integrations.Service],
		binder[scriptrun.Service], binder[releases.Service], binder[hooks.Service], binder[scripts.Service],
		binder[ghthreads.Service], binder[backup.Service], binder[svcstatus.Service], binder[setup.Service],
		binder[update.Service], binder[autopilot.Service],
	}
}

// binder wraps svc for binding when it is a *T.
func binder[T any](svc any) (application.Service, bool) {
	s, ok := svc.(*T)
	if !ok {
		return application.Service{}, false
	}

	return bindService(s), true
}

// bindService binds s, as every service the window calls is bound: with boundOptions.
func bindService[T any](s *T) application.Service {
	return application.NewServiceWithOptions(s, boundOptions())
}

// boundOptions are the options every bound service is bound with: its methods' errors
// reach the window through desktop.MarshalError, with what the user is told about each
// and the detail the window needs to answer it. The runtime encodes a bound method's
// error with its own service's marshaler alone, never the application's MarshalError
// option, so each service carries it (internal/repoguard checks that every one does, and
// the desktop-tagged test that the window gets both).
func boundOptions() application.ServiceOptions {
	return application.ServiceOptions{MarshalError: desktop.MarshalError}
}

// startFailed returns err, which stopped the app from starting, with the build's details.
// Outside a measurement run it first logs the error, shows it in a native dialog, and
// exits with a failure status once the dialog is dismissed. db is the database's file, or
// "" when the data couldn't be found: for data a newer build saved, the dialog offers to
// put back the newest backup beside it this build can open.
func startFailed(args []string, logger *slog.Logger, err error, db string) error {
	wrapped := fmt.Errorf("start the app (%s, %s build, commit %s, built %s): %w",
		version, buildFlavor(), commit, buildDate, err)

	if slices.Contains(args, measure.StartupFlag) || slices.Contains(args, measure.RefreshFlag) {
		return wrapped
	}

	failure := desktop.StartupFailure(err)

	restore, ok, findErr := desktop.FindRestore(err, db, time.Now())
	if findErr != nil {
		logger.Warn("could not look for a backup to offer", "error", findErr)
	}

	if ok {
		failure = failure.Offering(restore)
		logger.Info("offering to put a backup back", "backup", filepath.Base(restore.Backup), "taken", restore.At)
	}

	failStartup(logger, wrapped, failure, putBack{db: db, sandbox: sandboxDir(args)})

	return wrapped
}

// putBack is where a startup dialog that offers a backup puts it back: the database's
// file, and the sandbox's data directory the app opens on again, or "".
type putBack struct {
	db      string
	sandbox string
}

// buildFlavor names the kind of build this is: the flavor the linker stamped, or
// "source" for a build made from source.
func buildFlavor() string {
	if flavor == "" {
		return "source"
	}

	return flavor
}

// failStartup logs err, shows failure in a native error dialog, since nobody sees stderr
// when the app is launched from Finder, and exits with a failure status once the dialog
// is dismissed. The log file is written unbuffered, so exiting without closing it loses
// nothing. The dialog needs a running app, so a bare one, with no window and no services,
// runs just to show it.
//
// A failure that offers a backup (failure.Restore) has a button beside Quit that puts it
// back in place of the database at to.db, setting the newer one aside, and then quits and
// opens the app again, as a reset does, once the bare app has shut down; the app exits
// with a success status then. A backup that can't be put back says so in a dialog of its
// own, and Quit quits.
func failStartup(logger *slog.Logger, err error, failure desktop.Failure, to putBack) {
	logger.Error("the app could not start", "error", err)

	var restored atomic.Bool

	app := application.New(application.Options{
		Name:        brand.Name,
		Description: brand.Tagline,
		Icon:        appIcon(),
		// On macOS the app exits from inside Run once it terminates, with a success
		// status unless this hook exits first.
		PostShutdown: func() {
			if restored.Load() {
				relaunch(to.sandbox, "putting a backup back", logger)
				os.Exit(0)
			}

			os.Exit(1)
		},
	})

	// The dialog's click runs on the main thread; the copy runs off it, and so does the
	// quit, which waits for the main thread.
	putBackNow := func() {
		go func() {
			aside, putErr := desktop.PutBack(context.Background(), to.db, *failure.Restore)
			if putErr != nil {
				logger.Error("could not put the backup back", "backup", filepath.Base(failure.Restore.Backup),
					"error", putErr)
				showFailure(app, desktop.PutBackFailure(to.db, putErr), nil)

				return
			}

			logger.Info("put the backup back in place of the newer data; opening the app again",
				"backup", filepath.Base(failure.Restore.Backup), "newer", filepath.Base(aside))
			restored.Store(true)
			app.Quit()
		}()
	}

	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		showFailure(app, failure, putBackNow)
	})

	_ = app.Run()

	os.Exit(1)
}

// showFailure shows failure in a native error dialog whose Quit quits app, with its
// backup's button beside it, which runs putBack, when it offers one and putBack is set.
//
// The runtime's macOS alert gives a button Return when it is the default and Escape only
// when it isn't, never both. With a backup offered, Quit takes Escape, the safe answer,
// and no button takes Return, so no key puts the backup back by accident; alone, Quit takes
// Return.
func showFailure(app *application.App, failure desktop.Failure, putBack func()) {
	dialog := app.Dialog.Error().SetTitle(failure.Title).SetMessage(failure.Message)

	// macOS shows the buttons right to left, the last added rightmost: Quit stays on the
	// right.
	if failure.Restore == nil || putBack == nil {
		dialog.AddButton("Quit").SetAsDefault().OnClick(app.Quit)
		dialog.Show()

		return
	}

	dialog.AddButton(failure.Restore.Button).OnClick(putBack)
	dialog.AddButton("Quit").SetAsCancel().OnClick(app.Quit)
	dialog.Show()
}

// measureRefresh times live inbox refreshes the way the app makes them, each with searches
// routine searches beside it (none at 0), against a scratch database that is deleted
// afterwards, and prints the report. It opens no window.
func measureRefresh(stdout io.Writer, home string, searches int, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	report, err := measure.RunRefresh(ctx, measure.RefreshOptions{
		Home: home, Version: version, Searches: searches, Logger: logger,
	})
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
