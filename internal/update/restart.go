package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/uierr"
)

var (
	// ErrRunsGoing is returned by a restart the user hasn't confirmed while runs go in the
	// app, which it would stop; a RunsGoingError names them.
	ErrRunsGoing = errors.New("runs are going in the app")

	// ErrNotReady is returned by a restart with no update downloaded and checked.
	ErrNotReady = errors.New("no update is ready to install")

	// ErrBusy is returned by a restart while the update downloads.
	ErrBusy = errors.New("the update is still downloading")

	// ErrNoInstaller is returned by a restart in a build that can't install updates: no
	// desktop runtime attached an Installer.
	ErrNoInstaller = errors.New("this build can't install updates")

	// errNoBackup is why a restart in a build that backs nothing up installs nothing.
	errNoBackup = errors.New("nothing backs up the database")

	// ErrOff is returned by what a build that can't update refuses.
	ErrOff = errors.New("updates are off in this build")
)

// RunsGoingError refuses a restart the user hasn't confirmed while runs go in the app. Its
// message names them, and Detail hands them to the desktop runtime, which sends them to the
// window as {"runsGoing": [...]}, so it can ask first in the quit dialog's words; a run
// that couldn't be listed leaves the list empty, and the window asks about the runs as a
// whole.
type RunsGoingError struct {
	// Runs are the runs going, as Runs.Going listed them.
	Runs []Run

	err error
}

// RunsGoingDetail is what the window gets beside a RunsGoingError's message.
type RunsGoingDetail struct {
	RunsGoing []Run `json:"runsGoing"`
}

// newRunsGoing refuses a restart while runs go, naming them.
func newRunsGoing(runs []Run) *RunsGoingError {
	if runs == nil {
		runs = []Run{}
	}

	return &RunsGoingError{Runs: runs, err: uierr.New(runsWords(runs), ErrRunsGoing)}
}

// Error returns the refusal's text.
func (e *RunsGoingError) Error() string {
	return e.err.Error()
}

// Unwrap returns the refusal: ErrRunsGoing, with the words for the user.
func (e *RunsGoingError) Unwrap() error {
	return e.err
}

// Detail returns the runs, for the window.
func (e *RunsGoingError) Detail() any {
	return RunsGoingDetail{RunsGoing: e.Runs}
}

// runsWords says what restarting would stop: one run by its title and where it runs,
// several by their titles, or the runs as a whole when none could be listed.
func runsWords(runs []Run) string {
	switch len(runs) {
	case 0:
		return "Restarting stops the runs going in " + brand.Name + "."
	case 1:
		return "Restarting stops " + runs[0].Title + where(runs[0]) + "."
	default:
		titles := make([]string, 0, len(runs))
		for _, r := range runs {
			titles = append(titles, r.Title)
		}

		return "Restarting stops the " + strconv.Itoa(len(runs)) + " runs going in " + brand.Name + ": " +
			strings.Join(titles, ", ") + "."
	}
}

// where says where a run runs, as " in owner/name" or " in ~/folder", or "".
func where(r Run) string {
	switch {
	case r.Repo != "":
		return " in " + r.Repo
	case r.Dir != "":
		return " in " + r.Dir
	default:
		return ""
	}
}

// restart installs the update that is ready and restarts into it, as the window asks once
// the user clicks Restart to update:
//
//  1. While runs go in the app, a restart the user hasn't confirmed is refused with a
//     RunsGoingError naming them.
//  2. The database is backed up; a backup that fails stops the restart.
//  3. The install about to happen is recorded (KeyPendingInstall), for the next start to
//     read how it went.
//  4. The Installer stages the update beside the running bundle.
//  5. The quit is let through (event.QuitConfirmed, Restarting), whatever runs go, since a
//     held quit would leave the runtime's helper to reopen this copy beside the new one;
//     then the Installer restarts, and the app exits inside it.
//
// Everything the next start needs is written before step 5. Any failure before the app
// quits goes back to ready, with the failure as LastError, the record cleared, and the
// staging folder removed; once the quit was let through, the shell holds a quit while runs
// go again (event.UpdateRestartFailed).
func (e *Engine) restart(ctx context.Context, confirmed bool) error {
	installer, entry, release, err := e.prepare(ctx, confirmed)
	if err != nil {
		return err
	}

	defer release()

	version := entry.Manifest.Version

	e.change(func(st *state) { st.installing, st.lastErr = true, "" })

	if e.backup == nil {
		return e.rollBack(ctx, version, false, uierr.New("Couldn't back up your data first, so nothing was installed.", errNoBackup))
	}

	backup, err := e.backup.BackUp(ctx)
	if err != nil {
		return e.rollBack(ctx, version, false, uierr.New("Couldn't back up your data first: "+uierr.Message(err), err))
	}

	e.logger.InfoContext(ctx, "backed up the data before installing the update", "version", version,
		"backup", e.tilde(backup))

	if err = e.recordPending(ctx, version); err != nil {
		return e.rollBack(ctx, version, false, uierr.New("Couldn't get ready to install "+version+": "+uierr.Message(err), err))
	}

	m := entry.Manifest
	if err = installer.Install(ctx, entry.Zip, version, m.Size, m.SHA512); err != nil {
		return e.rollBack(ctx, version, false, uierr.New("Couldn't install "+version+": "+uierr.Message(err), err))
	}

	e.logger.InfoContext(ctx, "restarting into the update", "from", e.current, "to", version)

	// The quit must never be held: Restart quits the app once the runtime's helper stands
	// by, and a quit held back would leave the helper to reopen this copy beside the new.
	e.events.Emit(event.QuitConfirmed{Restarting: true})

	err = installer.Restart(ctx)
	if err == nil {
		e.logger.ErrorContext(ctx, "the restart returned without the app quitting; the update's helper may open "+
			"the app again", "version", version)

		return nil
	}

	return e.rollBack(ctx, version, true, uierr.New("Couldn't restart into "+version+": "+uierr.Message(err), err))
}

// prepare checks a restart can begin and takes the operation for it: the update ready, an
// Installer attached, nothing downloading, and, unless confirmed, no run going in the app.
// It returns what lets go of the operation.
func (e *Engine) prepare(ctx context.Context, confirmed bool) (Installer, Entry, func(), error) {
	e.mu.Lock()
	installer, downloading := e.installer, e.st.downloading != nil
	e.mu.Unlock()

	switch {
	case installer == nil:
		return nil, Entry{}, nil, uierr.New("This build can't install updates.", ErrNoInstaller)
	case downloading:
		return nil, Entry{}, nil, uierr.New("Wait for the download to finish, then restart.", ErrBusy)
	}

	release, err := e.acquire(ctx)
	if err != nil {
		return nil, Entry{}, nil, err
	}

	st := e.view()
	if state, _ := st.phase(); state != StateReady {
		release()

		return nil, Entry{}, nil, uierr.New("There's no update ready to install yet.", ErrNotReady)
	}

	if !confirmed && e.runs != nil && e.runs.Active() {
		release()

		runs, listErr := e.runs.Going(ctx)
		if listErr != nil {
			e.logger.WarnContext(ctx, "could not list the runs a restart would stop", "error", listErr)
		}

		return nil, Entry{}, nil, newRunsGoing(runs)
	}

	return installer, *st.ready, release, nil
}

// recordPending records the install about to happen, committed before the app quits.
func (e *Engine) recordPending(ctx context.Context, version string) error {
	body, err := json.Marshal(pendingInstall{From: e.current, To: version, At: e.clock.Now().UTC(), PID: e.pid})
	if err != nil {
		return fmt.Errorf("record the install: %w", err)
	}

	if err = e.memory.SetMany(ctx, map[string]string{KeyPendingInstall: string(body)}); err != nil {
		return fmt.Errorf("record the install: %w", err)
	}

	return nil
}

// rollBack undoes a restart that failed before the app quit: the update is ready again,
// with err as LastError, the install's record is cleared, and the runtime's staging folder
// beside the bundle is removed. quitLet reports that the quit was let through already,
// which the shell is told to hold again. It returns err.
func (e *Engine) rollBack(ctx context.Context, version string, quitLet bool, err error) error {
	if quitLet {
		e.events.Emit(event.UpdateRestartFailed{})
	}

	e.logger.WarnContext(ctx, "the update didn't install", "version", version, "error", err)

	if delErr := e.memory.Delete(ctx, KeyPendingInstall); delErr != nil {
		e.logger.WarnContext(ctx, "could not clear the install's record", "error", delErr)
	}

	for _, cleanErr := range removeStaging(e.bundle) {
		e.logger.WarnContext(ctx, "could not remove the update's staging folder", "error", cleanErr)
	}

	e.change(func(st *state) { st.installing, st.lastErr = false, uierr.Message(err) })

	return err
}
