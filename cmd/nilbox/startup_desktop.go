//go:build desktop

package main

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/core"
	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/update"
)

// startupDialog is what the startup dialog works with beside the failure it shows: the
// database's file, where a backup offered goes back; the sandbox's data directory the app
// opens on again, or ""; and, for data a newer build saved, this build's way forward and
// the update that runs without the database (nil for none).
type startupDialog struct {
	db      string
	sandbox string
	newer   desktop.Newer
	bare    *update.Bare
}

// newerData works out what this build can do about data a newer build saved, at paths,
// before anything opens it: the update core builds without the database (core.BareUpdate),
// over the feed and key this build's flavor trusts, and the way forward and the commands
// it offers, from where the app runs.
func newerData(paths appdata.Paths, home string, logger *slog.Logger) (*update.Bare, desktop.Newer) {
	bundle := update.BundleOf(executable())

	bare := core.BareUpdate(core.Options{
		Paths: paths, Home: home, Version: version, Release: release, Logger: logger,
		Flavor: update.Flavor(buildFlavor()), UpdateFeed: updateFeed, UpdateKey: updateKey, Bundle: bundle,
	})

	newer := desktop.Newer{Way: bare.Way(), Commands: desktop.NewerCommands(bundle, bare.Location())}
	logger.Info("a newer build saved the data", "way", newer.Way)

	return bare, newer
}

// failStartup logs err, shows failure in a native error dialog, since nobody sees stderr
// when the app is launched from Finder, and exits with a failure status once the user
// quits. The log file is written unbuffered, so exiting without closing it loses nothing.
// The dialog needs a running app, so a bare one, with no window and no services, runs just
// to show it, and desktop.Startup runs what its buttons ask, off the main thread.
//
// A failure that offers a backup (failure.Restore) has a button that puts it back in place
// of the database at to.db, setting the newer one aside, and then quits and opens the app
// again, as a reset does, once the bare app has shut down; the app exits with a success
// status then. For data a newer build saved, Update and Reopen installs the latest release
// through the runtime's updater, the installer a running app updates with, which quits
// the app and opens the new version; nothing opens the database.
func failStartup(logger *slog.Logger, err error, failure desktop.Failure, to startupDialog) {
	logger.Error("the app could not start", "error", err)

	var reopen atomic.Bool

	app := application.New(application.Options{
		Name:        brand.Name,
		Description: brand.Tagline,
		Icon:        appIcon(),
		// On macOS the app exits from inside Run once it terminates, with a success
		// status unless this hook exits first.
		PostShutdown: func() {
			if reopen.Load() {
				relaunch(to.sandbox, "putting a backup back", logger)
				os.Exit(0)
			}

			os.Exit(1)
		},
	})

	run := desktop.Startup{
		UI: &startupUI{app: app, reopen: &reopen}, Commands: to.newer.Commands, DB: to.db, Logger: logger,
	}

	if to.bare != nil && to.newer.Way == update.WayUpdate {
		installer, setupErr := newInstaller(app.Updater, release)
		if setupErr != nil {
			logger.Error("could not set up the installer; the update can't be installed", "error", setupErr)
		} else {
			run.Update, run.Installer = to.bare, installer
		}
	}

	// The dialogs wait for their clicks, which run on the main thread, so they run off it.
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go run.Run(context.Background(), failure)
	})

	_ = app.Run()

	os.Exit(1)
}

// startupUI shows the startup dialogs as the runtime's native alerts, on the bare app
// (desktop.StartupUI).
type startupUI struct {
	app    *application.App
	reopen *atomic.Bool
}

// Ask shows f as a native alert, over any shown already, and returns the button clicked.
// An alert's every button dismisses it.
func (u *startupUI) Ask(f desktop.Failure) desktop.Button {
	clicked := make(chan desktop.Button, 1)

	dialog := u.app.Dialog.Error()
	if f.Waiting {
		dialog = u.app.Dialog.Info().SetIcon(appIcon())
	}

	dialog.SetTitle(f.Title).SetMessage(f.Message).AddButtons(alertButtons(f, func(b desktop.Button) { clicked <- b }))
	dialog.Show()

	return <-clicked
}

// alertButtons are f's buttons as the runtime's alert takes them, in order, each telling
// click which was clicked. macOS shows them right to left, the last added rightmost, so
// Quit stays on the right. The runtime's macOS alert gives a button Return when it is the
// default and Escape only when it isn't, never both (desktop.Failure.Buttons).
func alertButtons(f desktop.Failure, click func(desktop.Button)) []*application.Button {
	buttons := f.Buttons()
	out := make([]*application.Button, 0, len(buttons))

	for _, b := range buttons {
		out = append(out, &application.Button{
			Label: b.Label, IsDefault: b.Default, IsCancel: b.Cancel, Callback: func() { click(b) },
		})
	}

	return out
}

// Copy puts text on the clipboard.
func (u *startupUI) Copy(text string) bool {
	return u.app.Clipboard.SetText(text)
}

// Quit quits the app, which exits inside it, from the shutdown hook. A quit something held
// (an alert that runs modal, say) returns, and then the process exits here: the bare app
// holds nothing open, and an update's helper waits for it to.
func (u *startupUI) Quit() {
	u.app.Quit()
	os.Exit(1)
}

// Reopen quits the app, which opens again from the shutdown hook.
func (u *startupUI) Reopen() {
	u.reopen.Store(true)
	u.app.Quit()
}
