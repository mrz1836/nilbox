package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

	// ErrDamaged is returned by a restart whose downloaded update no longer matches its
	// manifest, or is gone.
	ErrDamaged = errors.New("the downloaded update is damaged")

	// errNoBackup is why a restart in a build that backs nothing up installs nothing.
	errNoBackup = errors.New("nothing backs up the database")

	// ErrFinishing is returned by a restart while an earlier one finishes: its quit was
	// held, and the app is quitting on its own so the update installs.
	ErrFinishing = errors.New("a restart to update is finishing")

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
//  1. It refuses where the app can't replace itself, an update that failed to install for
//     good, and, unless confirmed, while runs go in the app, with a RunsGoingError naming
//     them.
//  2. The zip is checked against its manifest again; a damaged or missing one is dropped,
//     to be downloaded again, and nothing else happens.
//  3. The database is backed up (a backup this engine took under BackupReuse ago serves
//     again); a backup that fails stops the restart.
//  4. The Installer stages the update beside the running bundle.
//  5. Unless confirmed, a run that started meanwhile refuses it as in step 1.
//  6. The install about to happen is recorded (KeyPendingInstall), for the next start to
//     read how it went: only now, so a quit that comes while the update is staged leaves
//     no record of an install that never began.
//  7. The quit is let through (event.QuitConfirmed, Restarting), whatever runs go, since a
//     held quit would leave the runtime's helper to reopen this copy beside the new one;
//     then the Installer restarts, and the app exits inside it.
//
// Everything the next start needs is written before step 7. Any failure before the app
// quits goes back to ready, with the failure as LastError worded with no path, the record
// cleared, and the staging folder removed; once the quit was let through, the shell holds a
// quit while runs go again (event.UpdateRestartFailed).
//
// A restart that returns with the app still running, with no error, is one whose helper
// stands by but whose quit something held (a modal panel, say). The helper can't be called
// back: it waits for this process to exit, then installs the staged update, and, should
// the process outlive its wait, opens this copy again beside it. So the restart is
// finishing (StateFinishing): the staging folder and the record stay, the quit stays let
// through, and the shell quits the app on its own (event.UpdateQuitHeld).
func (e *Engine) restart(ctx context.Context, confirmed bool) error {
	installer, entry, release, err := e.prepare(ctx, confirmed)
	if err != nil {
		return err
	}

	defer release()

	version := entry.Manifest.Version

	if err = e.checkReady(ctx, entry); err != nil {
		return err
	}

	e.change(func(st *state) { st.installing, st.lastErr = true, "" })

	if err = e.backUp(ctx); err != nil {
		return e.rollBack(ctx, version, false, err)
	}

	m := entry.Manifest
	if err = installer.Install(ctx, entry.Zip, version, m.Size, m.SHA512); err != nil {
		return e.rollBack(ctx, version, false, uierr.New("Couldn't get "+version+" ready to install.", err))
	}

	if !confirmed && e.runs != nil && e.runs.Active() {
		e.undo(ctx, false)

		return e.runsGoing(ctx)
	}

	if err = e.recordPending(ctx, m); err != nil {
		return e.rollBack(ctx, version, false, uierr.New("Couldn't get ready to install "+version+".", err))
	}

	e.logger.InfoContext(ctx, "restarting into the update", "from", e.current, "to", version)

	// The quit must never be held: Restart quits the app once the runtime's helper stands
	// by, and a quit held back would leave the helper to reopen this copy beside the new.
	e.events.Emit(event.QuitConfirmed{Restarting: true})

	err = installer.Restart(ctx)
	if err == nil {
		e.finish(ctx, version)

		return nil
	}

	return e.rollBack(ctx, version, true, uierr.New("Couldn't restart into "+version+".", err))
}

// finish handles a restart whose helper stands by while the app runs on, its quit held:
// the update stays staged and recorded for the helper, which installs it once this process
// exits, and the shell is told to see the app out (event.UpdateQuitHeld).
func (e *Engine) finish(ctx context.Context, version string) {
	e.logger.WarnContext(ctx, "the restart returned with the app still running; quitting to finish the update",
		"version", version)
	e.change(func(st *state) { st.finishing = true })
	e.events.Emit(event.UpdateQuitHeld{})
}

// prepare checks a restart can begin and takes the operation for it: the app where it can
// replace itself, an Installer attached, no restart finishing, nothing downloading, the
// update ready and installable, and, unless confirmed, no run going in the app. It returns
// what lets go of the operation.
func (e *Engine) prepare(ctx context.Context, confirmed bool) (Installer, Entry, func(), error) {
	installer, err := e.restartable()
	if err != nil {
		return nil, Entry{}, nil, err
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

	if e.blocked(ctx, st.ready.Manifest) {
		release()

		return nil, Entry{}, nil, uninstallableError(st.ready.Manifest.Version)
	}

	if !confirmed && e.runs != nil && e.runs.Active() {
		release()

		return nil, Entry{}, nil, e.runsGoing(ctx)
	}

	return installer, *st.ready, release, nil
}

// restartable returns the Installer a restart installs through, or why none can begin
// before the operation is taken: the app where it can't replace itself, no Installer, a
// restart finishing, or a download running.
func (e *Engine) restartable() (Installer, error) {
	e.mu.Lock()
	installer, downloading, finishing := e.installer, e.st.downloading != nil, e.st.finishing
	e.mu.Unlock()

	switch {
	case !e.location.OK:
		return nil, locationError()
	case installer == nil:
		return nil, uierr.New("This build can't install updates.", ErrNoInstaller)
	case finishing:
		return nil, uierr.New(brand.Name+" is quitting to finish the update.", ErrFinishing)
	case downloading:
		return nil, uierr.New("Wait for the download to finish, then restart.", ErrBusy)
	default:
		return installer, nil
	}
}

// runsGoing refuses a restart while runs go, naming them as they can be listed.
func (e *Engine) runsGoing(ctx context.Context) error {
	runs, err := e.runs.Going(ctx)
	if err != nil {
		e.logger.WarnContext(ctx, "could not list the runs a restart would stop", "error", err)
	}

	return newRunsGoing(runs)
}

// checkReady checks the zip of the update ready against its manifest once more. A zip
// damaged or gone since is dropped from the cache, the version is found but no longer
// downloaded (available), the Worker downloads it again when the update downloads on its
// own, and the restart is refused with words saying so.
func (e *Engine) checkReady(ctx context.Context, entry Entry) error {
	err := checkZip(entry)
	if err == nil {
		return nil
	}

	e.logger.WarnContext(ctx, "the update kept is damaged or gone; dropping it", "version", entry.Manifest.Version,
		"error", err)
	e.prune(ctx, "")

	again := "Click Update to download it again."
	if e.downloads(e.mode(ctx)) {
		again = "It's downloading again."
	}

	refusal := uierr.New("The downloaded "+entry.Manifest.Version+" was damaged, so it was thrown away. "+again,
		fmt.Errorf("%w: %w", ErrDamaged, err))

	e.change(func(st *state) { st.ready, st.lastErr = nil, uierr.Message(refusal) })

	if e.downloads(e.mode(ctx)) {
		e.wakeWorker()
	}

	return refusal
}

// checkZip checks entry's zip against its manifest.
func checkZip(entry Entry) error {
	f, err := os.Open(entry.Zip)
	if err != nil {
		return fmt.Errorf("open the downloaded update: %w", err)
	}

	defer func() { _ = f.Close() }()

	return CheckFile(entry.Manifest, f)
}

// backUp backs the database up before an install, unless a backup this engine took is
// still there and under BackupReuse old: a restart tried again soon after reuses it, so
// retries never push out the older backups (those before migrations among them) the
// restore needs. The schema can't change while the app runs, so the backup is of the same
// schema. A failure is worded with no path.
func (e *Engine) backUp(ctx context.Context) error {
	if e.backup == nil {
		return uierr.New("Couldn't back up your data first, so nothing was installed.", errNoBackup)
	}

	now := e.clock.Now()

	e.mu.Lock()
	last := e.lastBackup
	e.mu.Unlock()

	if last.path != "" && !now.Before(last.at) && now.Sub(last.at) < BackupReuse {
		if _, err := os.Stat(last.path); err == nil {
			e.logger.InfoContext(ctx, "a backup taken moments ago serves this install too", "backup", e.tilde(last.path))

			return nil
		}
	}

	path, err := e.backup.BackUp(ctx)
	if err != nil {
		return uierr.New("Couldn't back up your data first, so nothing was installed.", err)
	}

	e.logger.InfoContext(ctx, "backed up the data before installing the update", "backup", e.tilde(path))

	e.mu.Lock()
	e.lastBackup = backupTaken{path: path, at: now}
	e.mu.Unlock()

	return nil
}

// recordPending records the install of m about to happen, committed before the app quits.
func (e *Engine) recordPending(ctx context.Context, m Manifest) error {
	body, err := json.Marshal(pendingInstall{
		From: e.current, To: m.Version, At: e.clock.Now().UTC(), PID: e.pid, SHA512: m.SHA512,
	})
	if err != nil {
		return fmt.Errorf("record the install: %w", err)
	}

	if err = e.memory.SetMany(ctx, map[string]string{KeyPendingInstall: string(body)}); err != nil {
		return fmt.Errorf("record the install: %w", err)
	}

	return nil
}

// rollBack undoes a restart that failed before the app quit (undo), with err, worded, as
// LastError, and logs err whole. It returns err.
func (e *Engine) rollBack(ctx context.Context, version string, quitLet bool, err error) error {
	e.logger.WarnContext(ctx, "the update didn't install", "version", version, "error", err)
	e.undo(ctx, quitLet)
	e.change(func(st *state) { st.lastErr = uierr.Message(err) })

	return err
}

// undo takes back a restart that won't go on: the update is ready again, the install's
// record is cleared, even when ctx has ended, and the runtime's staging folder beside the
// bundle is removed. quitLet reports that the quit was let through already, which the
// shell is told to hold again.
func (e *Engine) undo(ctx context.Context, quitLet bool) {
	if quitLet {
		e.events.Emit(event.UpdateRestartFailed{})
	}

	if err := e.memory.Delete(context.WithoutCancel(ctx), KeyPendingInstall); err != nil {
		e.logger.WarnContext(ctx, "could not clear the install's record", "error", err)
	}

	for _, err := range removeStaging(e.bundle) {
		e.logger.WarnContext(ctx, "could not remove the update's staging folder", "error", err)
	}

	e.change(func(st *state) { st.installing = false })
}
