package desktop

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"path/filepath"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/update"
)

// Failure is what the error dialog says when the app cannot start.
type Failure struct {
	// Title is the dialog's title.
	Title string

	// Message is its text: what went wrong, and what to do about it where that is known.
	Message string

	// Restore is the backup the dialog offers to put back beside Quit (see Offering), or
	// nil for none.
	Restore *Restore

	// Actions are the buttons between the backup's and Quit: the dialog's way forward
	// (Update and Reopen, Try Again, or a command's Copy). The first takes Return.
	Actions []Button

	// Waiting marks the dialog shown while an update runs (Updating), whose one button
	// stops it.
	Waiting bool
}

// StartupFailure words err, which stopped the app from starting, for the error dialog
// shown before the app quits: nobody sees the standard error of an app opened from the
// Finder. A database saved by a newer build asks for an update, with newer's way forward:
// Update and Reopen, the install command, or, in a build from source, the build command.
// A database another process holds locked, and data the app may not read or write, say
// what to do. Each gives what the user is told about the error itself (see uierr.Message).
func StartupFailure(err error, newer Newer) Failure {
	couldNotStart := brand.Name + " couldn't start"

	switch {
	case errors.Is(err, store.ErrNewerSchema):
		return newerData(newer)
	case store.IsBusy(err):
		return Failure{
			Title: couldNotStart,
			Message: "Another copy of " + brand.Name + " may still be running with its data open. Quit it, then open " +
				brand.Name + " again.\n\n" + uierr.Message(err),
		}
	case errors.Is(err, fs.ErrPermission):
		return Failure{
			Title: couldNotStart,
			Message: brand.Name + " isn't allowed to read or write its data. Check the permissions of its data " +
				"folder, then open " + brand.Name + " again.\n\n" + uierr.Message(err),
		}
	default:
		return Failure{Title: couldNotStart, Message: uierr.Message(err)}
	}
}

// NewerUpdate is the update the dialog about data a newer build saved runs, without the
// database (update.Bare).
type NewerUpdate interface {
	// Latest returns the latest release, with found set when it is newer than this build.
	Latest(ctx context.Context) (m update.Manifest, found bool, err error)

	// Update installs m and restarts into it, through installer.
	Update(ctx context.Context, m update.Manifest, installer update.Installer) error
}

// StartupUI shows the startup dialogs: the desktop bootstrap's adapter over the runtime's
// alerts, on the bare app that runs only to show them.
type StartupUI interface {
	// Ask shows f with its buttons (Failure.Buttons) and returns the one clicked, once it
	// is. It is never called on the main thread, and may be called while another dialog
	// shows, which the new one shows over.
	Ask(f Failure) Button

	// Copy puts text on the clipboard, and reports whether it could.
	Copy(text string) bool

	// Quit quits the app, which didn't start.
	Quit()

	// Reopen quits the app and opens it again, once a backup is put back.
	Reopen()
}

// Startup runs the startup dialog of a failure that stopped the app from starting, and
// what its buttons start, until the app quits.
type Startup struct {
	// UI shows the dialogs.
	UI StartupUI

	// Update and Installer install the update the dialog about data a newer build saved
	// offers; with no Update, Update and Reopen offers nothing.
	Update    NewerUpdate
	Installer update.Installer

	// Commands are the commands the dialogs offer to copy.
	Commands Commands

	// DB is the database's file, where a backup offered goes back.
	DB string

	// Logger hears what the dialogs started.
	Logger *slog.Logger
}

// Run shows f and does what its buttons ask until the app quits: Quit quits; a Copy puts
// its command on the clipboard and shows the dialog again, saying so; the backup's button
// puts it back and opens the app again, or says why it couldn't; and Update and Reopen
// (Try Again) checks for a newer release and installs it, with Updating shown over the
// dialog until the app quits into it, and says plainly when no published release is newer
// or the update failed, which changed nothing. Nothing here touches the database but
// putting a backup back, on the user's click.
func (s Startup) Run(ctx context.Context, f Failure) {
	ctx, stop := context.WithCancel(ctx)
	defer stop()

	r := &startupRun{Startup: s, stop: stop, logger: logging.Or(s.Logger), failure: f, shown: f}

	for {
		if r.click(ctx, s.UI.Ask(r.shown)) {
			return
		}
	}
}

// startupRun is one run of the startup dialog.
type startupRun struct {
	Startup

	// stop ends the update, once Stop and Quit is clicked.
	stop   context.CancelFunc
	logger *slog.Logger

	// failure is the dialog as it stands, and shown the same as it shows, saying a command
	// was copied.
	failure Failure
	shown   Failure

	// waiting is set once Updating shows.
	waiting bool
}

// click does what b asks, and reports whether the app is quitting.
func (r *startupRun) click(ctx context.Context, b Button) bool {
	switch b.Choice {
	case ChoiceCopy:
		r.shown = r.failure.Copied(b, r.UI.Copy(b.Copies))

		return false
	case ChoicePutBack:
		if r.failure.Restore != nil {
			next, done := r.putBack(ctx, *r.failure.Restore)

			return done || r.show(next)
		}
	case ChoiceUpdate:
		r.showUpdating()

		next, done := r.update(ctx)
		if done {
			return true
		}

		if r.failure.Restore != nil {
			next = next.Offering(*r.failure.Restore)
		}

		return r.show(next)
	case ChoiceQuit:
	}

	r.UI.Quit()

	return true
}

// show makes f the dialog shown next. It reports that the app isn't quitting.
func (r *startupRun) show(f Failure) bool {
	r.failure, r.shown = f, f

	return false
}

// showUpdating shows Updating over the dialog, once, until the app quits: Stop and Quit
// quits, which ends the update with the app, and stops it should the quit return.
func (r *startupRun) showUpdating() {
	if r.waiting {
		return
	}

	r.waiting = true

	go func() {
		r.UI.Ask(Updating())
		r.UI.Quit()
		r.stop()
	}()
}

// update checks for a newer release and installs it, and returns the dialog that says how
// it went: none newer, or a failure. done is set once the app is quitting: the update was
// stopped, or it restarts into the new version.
func (r *startupRun) update(ctx context.Context) (Failure, bool) {
	if r.Update == nil {
		return UpdateFailed(uierr.New("This build can't update itself.", errNoUpdate), r.Commands), false
	}

	m, found, err := r.Update.Latest(ctx)

	switch {
	case ctx.Err() != nil:
		return Failure{}, true
	case err != nil:
		return UpdateFailed(err, r.Commands), false
	case !found:
		r.logger.InfoContext(ctx, "no published release is newer than this build; it can't open the data")

		return NoNewerRelease(r.Commands), false
	}

	err = r.Update.Update(ctx, m, r.Installer)

	switch {
	case ctx.Err() != nil:
		return Failure{}, true
	case err != nil:
		return UpdateFailed(err, r.Commands), false
	}

	// The update's helper stands by, waiting for this process to exit, but the quit was
	// held: quit again, so it installs.
	r.logger.InfoContext(ctx, "the update the data needs is installing; quitting so it can", "version", m.Version)
	r.UI.Quit()

	return Failure{}, true
}

// putBack puts the backup b back in place of the newer data and opens the app again; done
// is set then. A backup that couldn't be put back returns the dialog that says so.
func (r *startupRun) putBack(ctx context.Context, b Restore) (Failure, bool) {
	aside, err := PutBack(ctx, r.DB, b)
	if err != nil {
		r.logger.ErrorContext(ctx, "could not put the backup back", "backup", filepath.Base(b.Backup), "error", err)

		return PutBackFailure(r.DB, err), false
	}

	r.logger.InfoContext(ctx, "put the backup back in place of the newer data; opening the app again",
		"backup", filepath.Base(b.Backup), "newer", filepath.Base(aside))
	r.UI.Reopen()

	return Failure{}, true
}
