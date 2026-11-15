package desktop

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/menubar"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/update"
)

// Updates is the app's update as the menu bar offers it: its status, Check for Updates…,
// the download of a version Just tell me left found, and the restart into the version
// ready. *update.Service implements it.
type Updates interface {
	Status(ctx context.Context) (update.Status, error)
	CheckNow(ctx context.Context) (update.Status, error)
	Download(ctx context.Context) (update.Status, error)
	Restart(ctx context.Context, confirmed bool) (update.Status, error)
}

// URLOpener opens a web page in the user's browser: the desktop runtime's.
type URLOpener interface {
	OpenURL(url string) error
}

// After a restart to update whose quit was held (event.UpdateQuitHeld), the shell asks to
// quit again every FinishQuitEvery, FinishQuitTries times, and, the app still running
// FinishWithin after the hold, shuts down in order, waiting for that at most
// FinishStopWithin, and exits: the update's helper, which can't be called back, waits 30
// seconds for the app to exit before it gives up and installs nothing.
const (
	FinishQuitEvery  = 5 * time.Second
	FinishQuitTries  = 4
	FinishWithin     = 25 * time.Second
	FinishStopWithin = 3 * time.Second
)

// UpdateAction does what a click on a row of the update's part of the menu bar asks, in
// the background: a check, the restart into the version ready, or the download of the
// version found and then the restart. How a check or a refused restart went shows on the
// menu's status line for menubar.NoteFor, in place of the line an earlier action left. The
// restart is never confirmed here: while runs go in the app it is refused, and the refusal
// shows on that line.
func (s *Shell) UpdateAction(action menubar.UpdateAction) {
	if s.updates == nil || s.stopping.Load() || action == menubar.UpdateNone {
		return
	}

	s.setUpdateNote(menubar.UpdateNote{})

	switch action {
	case menubar.UpdateCheck:
		s.goTracked(s.checkForUpdates)
	case menubar.UpdateRestart:
		s.restartUntracked()
	case menubar.UpdateDownload:
		s.goTracked(s.downloadAndRestart)
	case menubar.UpdateManual:
		s.goTracked(s.openReleasePage)
	case menubar.UpdateNone:
	}
}

// openReleasePage opens the release page of the version found in the browser, to download
// it by hand.
func (s *Shell) openReleasePage(ctx context.Context) {
	st, err := s.updates.Status(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.WarnContext(ctx, "could not read the update to open its release page", "error", err)
		}

		return
	}

	if s.browser == nil {
		s.logger.WarnContext(ctx, "this build can't open the release page", "url", st.ReleaseURL)

		return
	}

	if err = s.browser.OpenURL(st.ReleaseURL); err != nil {
		s.logger.WarnContext(ctx, "could not open the release page", "url", st.ReleaseURL, "error", err)
	}
}

// finishRestart sees the app out once a restart to update's quit was held, once: every quit
// stays let through, and seeOut, apart from the tracked work, asks to quit again, then
// shuts down in order and exits.
func (s *Shell) finishRestart() {
	if !s.finishing.CompareAndSwap(false, true) {
		return
	}

	s.restarting.Store(true)
	s.logger.Warn("the restart to update's quit was held; quitting again to finish the update",
		"within", FinishWithin.String())

	go s.seeOut()
}

// seeOut asks the app to quit every FinishQuitEvery, FinishQuitTries times, each ask on a
// goroutine of its own, since a quit waits for the main thread whatever holds it; and, the
// process still running FinishWithin on, exits once the shell has stopped, which closes the
// data in order, or FinishStopWithin later, whichever comes first, so the update's helper
// still finds the app gone within its wait. A quit that goes through ends the process
// before then.
func (s *Shell) seeOut() {
	asks := time.NewTicker(FinishQuitEvery)
	defer asks.Stop()

	deadline := time.NewTimer(FinishWithin)
	defer deadline.Stop()

	for tries := 0; ; {
		select {
		case <-asks.C:
			if tries < FinishQuitTries {
				tries++
				s.logger.Info("asking to quit again to finish the update", "try", tries)

				go s.app.Quit()
			}
		case <-deadline.C:
			s.logger.Warn("the app still runs; shutting down in order to finish the update")
			s.stopWithin(FinishStopWithin)
			s.logger.Info("exiting, so the update's helper installs the update")
			s.exit(0)

			return
		}
	}
}

// stopWithin stops the shell, which closes the data in order, waiting for it at most
// limit; a stop that takes longer goes on, and the caller exits anyway.
func (s *Shell) stopWithin(limit time.Duration) {
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		if err := s.Stop(); err != nil {
			s.logger.Warn("could not close the data in order", "error", err)
		}
	}()

	timeout := time.NewTimer(limit)
	defer timeout.Stop()

	select {
	case <-stopped:
	case <-timeout.C:
		s.logger.Warn("the shutdown is taking too long; exiting before the update's helper gives up",
			"waited", limit.String())
	}
}

// checkForUpdates checks for a new version now, and notes how it went.
func (s *Shell) checkForUpdates(ctx context.Context) {
	st, err := s.updates.CheckNow(ctx)
	if ctx.Err() != nil {
		return
	}

	note := menubar.UpdateNote{At: s.clock.Now(), Checked: true, CheckError: st.LastError}
	if err != nil {
		note.CheckError = uierr.Message(err)
		s.logger.WarnContext(ctx, "the menu bar's update check failed", "error", err)
	}

	s.setUpdateNote(note)
}

// downloadAndRestart downloads the version found, then restarts into it; a download that
// fails is noted.
func (s *Shell) downloadAndRestart(ctx context.Context) {
	if _, err := s.updates.Download(ctx); err != nil {
		if ctx.Err() == nil {
			s.logger.WarnContext(ctx, "the menu bar's update download failed", "error", err)
			s.setUpdateNote(refusal(err, "Download failed", s.clock.Now()))
		}

		return
	}

	s.restartUntracked()
}

// restartUntracked restarts into the version ready, apart from the tracked work: the
// restart quits the app once the update is staged, and the quit stops the shell, which
// waits for its tracked work, so the restart is not tracked work itself. Once the shell is
// stopping it starts nothing.
func (s *Shell) restartUntracked() {
	if s.stopping.Load() {
		return
	}

	go s.restart(s.lifetime)
}

// restart restarts into the version ready, unconfirmed, and notes why it didn't go ahead.
// It returns only when the restart didn't happen: otherwise the app quits inside it.
func (s *Shell) restart(ctx context.Context) {
	_, err := s.updates.Restart(ctx, false)
	if err == nil || ctx.Err() != nil {
		return
	}

	s.logger.InfoContext(ctx, "the menu bar's restart to update didn't go ahead", "error", err)
	s.setUpdateNote(restartRefusal(err, s.clock.Now()))
}

// restartRefusal is the status line of a restart that was refused at at: the runs going,
// counted, the app where it can't replace itself, or that it failed; its tooltip has the
// words.
func restartRefusal(err error, at time.Time) menubar.UpdateNote {
	note := refusal(err, "Restart failed", at)

	var going *update.RunsGoingError
	if !errors.As(err, &going) {
		return note
	}

	switch n := len(going.Runs); n {
	case 0:
		note.Refused, note.Why = "Not restarted: runs are going", note.Why+" Restart once they have ended."
	case 1:
		note.Refused, note.Why = "Not restarted: a run is going", note.Why+" Restart once it has ended."
	default:
		note.Refused = "Not restarted: " + strconv.Itoa(n) + " runs are going"
		note.Why += " Restart once they have ended."
	}

	return note
}

// refusal is the status line of a download or a restart that err stopped at at: failed,
// as label says; where the app can't replace itself, that it must move to Applications
// first; or, while the last update may still be finishing, that it is. Its tooltip has the
// words.
func refusal(err error, label string, at time.Time) menubar.UpdateNote {
	switch {
	case errors.Is(err, update.ErrLocation):
		label = "Move to Applications to update"
	case errors.Is(err, update.ErrSettling):
		label = "The last update is still finishing"
	}

	return menubar.UpdateNote{At: at, Refused: label, Why: uierr.Message(err)}
}

// setUpdateNote keeps note as what the menu last asked of the update, and redraws the
// menu with its status line.
func (s *Shell) setUpdateNote(note menubar.UpdateNote) {
	s.mu.Lock()
	s.updateNote = note
	s.mu.Unlock()

	s.askRedraw()
}

// updateRows works out the update's part of the menu at now, or none in a build with no
// update or when its status can't be read, which is logged.
func (s *Shell) updateRows(ctx context.Context, now time.Time) menubar.Updates {
	if s.updates == nil {
		return menubar.Updates{}
	}

	st, err := s.updates.Status(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.WarnContext(ctx, "could not read the update for the menu bar", "error", err)
		}

		return menubar.Updates{}
	}

	s.mu.Lock()
	note := s.updateNote
	s.mu.Unlock()

	return menubar.UpdateRows(st, note, now)
}

// sameUpdates reports whether two update parts of the menu read alike.
func sameUpdates(a, b menubar.Updates) bool {
	return slices.Equal(a.Rows, b.Rows) && a.Until.Equal(b.Until)
}

// soonest is the earlier of two times, the zero time counting as none.
func soonest(a, b time.Time) time.Time {
	switch {
	case a.IsZero():
		return b
	case b.IsZero() || a.Before(b):
		return a
	default:
		return b
	}
}
