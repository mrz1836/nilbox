package desktop

import (
	"context"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/menubar"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/update"
)

// Updates is the app's update as the menu bar offers it: its status, and Check for
// Updates…. *update.Service implements it. A restart, and the download Just tell me's
// Update starts it with, are the window's (UpdateAction).
type Updates interface {
	Status(ctx context.Context) (update.Status, error)
	CheckNow(ctx context.Context) (update.Status, error)
}

// URLOpener opens a web page in the user's browser: the desktop runtime's.
type URLOpener interface {
	OpenURL(url string) error
}

// After a restart to update whose quit was held (event.UpdateQuitHeld), the shell asks to
// quit again every FinishQuitEvery, FinishQuitTries times, and, the app still running
// FinishWithin after the restart began starting the update's helper
// (UpdateQuitHeld.HelperStarted), shuts down in order, waiting for that at most
// FinishStopWithin, and exits. The helper, which can't be called back, waits 30 seconds
// from just after that moment for the app to exit, before it gives up, installs nothing,
// and opens this copy again; timing the exit from the event's arrival instead would lose
// however long the held quit took to come back.
const (
	FinishQuitEvery  = 5 * time.Second
	FinishQuitTries  = 4
	FinishWithin     = 25 * time.Second
	FinishStopWithin = 3 * time.Second
)

// UpdateAction does what a click on a row of the update's part of the menu bar asks. A
// check runs in the background, and how it went shows on the menu's status line for
// menubar.NoteFor, in place of the line an earlier check left. Restart to Update, Update to
// X… (the download Just tell me leaves to a click, then the restart), and Try Updating to X
// Again (either of them, after an install that may work if tried again) bring the window
// forward and ask it to restart (event.UpdateRestartRequested), so every restart runs the
// window's one flow: it downloads first when it must, asks first when restarting would stop
// a run or lose text a sheet holds, and says why when the restart doesn't go ahead. The
// window is only ever hidden, never gone, so its page hears the request. Download X
// Manually… opens the version's release page in the browser.
func (s *Shell) UpdateAction(action menubar.UpdateAction) {
	if s.updates == nil || s.stopping.Load() || action == menubar.UpdateNone {
		return
	}

	s.setUpdateNote(menubar.UpdateNote{})

	switch action {
	case menubar.UpdateCheck:
		s.goTracked(s.checkForUpdates)
	case menubar.UpdateRestart, menubar.UpdateDownload:
		s.window.Show()
		s.frontend.Emit(event.UpdateRestartRequested{})
	case menubar.UpdateManual:
		s.goTracked(s.openReleasePage)
	case menubar.UpdateNone:
	}
}

// openReleasePage opens, in the browser, the release page of the version found, or of the
// one the last restart didn't install when none is found (menubar.ManualURL), to download
// it by hand.
func (s *Shell) openReleasePage(ctx context.Context) {
	st, err := s.updates.Status(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.WarnContext(ctx, "could not read the update to open its release page", "error", err)
		}

		return
	}

	page := menubar.ManualURL(st)
	if s.browser == nil {
		s.logger.WarnContext(ctx, "this build can't open the release page", "url", page)

		return
	}

	if err = s.browser.OpenURL(page); err != nil {
		s.logger.WarnContext(ctx, "could not open the release page", "url", page, "error", err)
	}
}

// finishRestart sees the app out once a restart to update's quit was held, once: every quit
// stays let through, and seeOut, apart from the tracked work, asks to quit again, then
// shuts down in order and exits FinishWithin after helperStarted, when the restart began
// starting the update's helper (or after now, when that wasn't told).
func (s *Shell) finishRestart(helperStarted time.Time) {
	if !s.finishing.CompareAndSwap(false, true) {
		return
	}

	s.restarting.Store(true)

	left := FinishWithin
	if !helperStarted.IsZero() {
		left -= s.clock.Now().Sub(helperStarted)
	}

	s.logger.Warn("the restart to update's quit was held; quitting again to finish the update",
		"within", max(left, 0).String())

	go s.seeOut(left)
}

// seeOut asks the app to quit every FinishQuitEvery, FinishQuitTries times, each ask on a
// goroutine of its own, since a quit waits for the main thread whatever holds it; and, the
// process still running once left has passed, leaves (shuts down in order and exits), so
// the update's helper still finds the app gone within its wait. With no time left, it
// leaves at once. A quit that goes through ends the process before then.
func (s *Shell) seeOut(left time.Duration) {
	if left <= 0 {
		s.logger.Warn("the update's helper is nearly done waiting; shutting down in order to finish the update")
		s.leave()

		return
	}

	asks := time.NewTicker(FinishQuitEvery)
	defer asks.Stop()

	deadline := time.NewTimer(left)
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
			s.leave()

			return
		}
	}
}

// leave exits once the shell has stopped, which closes the data in order, or
// FinishStopWithin later, whichever comes first.
func (s *Shell) leave() {
	s.stopWithin(FinishStopWithin)
	s.logger.Info("exiting, so the update's helper installs the update")
	s.exit(0)
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
