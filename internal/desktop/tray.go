package desktop

import (
	"context"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/menubar"
	"github.com/mrz1836/nilbox/internal/sessions"
)

// setMenuBar shows or hides the menu bar icon, as the user turned it on or off. A shown
// icon is drawn at once; a hidden one is no longer redrawn when a snooze ends.
func (s *Shell) setMenuBar(on bool) {
	s.mu.Lock()

	if s.stopping.Load() {
		s.mu.Unlock()

		return
	}

	s.trayOn = on

	if !on {
		stopTimer(&s.snooze)
	}

	s.mu.Unlock()

	if !on {
		s.tray.Hide()

		return
	}

	s.tray.Show()
	s.askRedraw()
}

// askRedraw asks for the menu bar to be redrawn in full. It never waits: an ask that
// comes while another is pending folds into it.
func (s *Shell) askRedraw() {
	s.full.Store(true)
	s.signalRedraw()
}

// askSessionsRedraw asks for the menu bar to be redrawn when the sessions waiting for the
// user read otherwise than when it was last drawn. A session runs through many changes
// the menu doesn't show, such as each thing its agent says, and each would otherwise read
// the inbox and rebuild the menu. It never waits, and folds into a pending ask.
func (s *Shell) askSessionsRedraw() {
	s.signalRedraw()
}

// signalRedraw wakes the redraw loop, unless an ask is already pending.
func (s *Shell) signalRedraw() {
	select {
	case s.redraw <- struct{}{}:
	default:
	}
}

// redrawTray redraws the menu bar each time it is asked to, one redraw at a time, until
// ctx ends: in full when any ask since the last redraw was for that, else only when the
// sessions waiting for the user changed.
func (s *Shell) redrawTray(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.redraw:
			if s.full.Swap(false) {
				s.drawTray(ctx)
			} else {
				s.redrawSessions(ctx)
			}
		}
	}
}

// redrawSessions draws the menu bar as drawTray does, but only when the rows of the
// sessions waiting for the user differ from the ones it was last drawn with, or it has
// not been drawn.
func (s *Shell) redrawSessions(ctx context.Context) {
	if !s.showsTray() {
		return
	}

	waiting := s.waiting(ctx)
	if s.drawn && slices.Equal(menubar.WaitingEntries(waiting), s.drawnWaiting) {
		return
	}

	s.drawTrayWith(ctx, waiting)
}

// drawTray draws the icon's count and menu from the stored inbox and the sessions waiting
// for the user, while the icon shows, and sets the timer for the soonest snooze to end,
// which changes the count with nothing stored.
func (s *Shell) drawTray(ctx context.Context) {
	if !s.showsTray() {
		return
	}

	s.drawTrayWith(ctx, s.waiting(ctx))
}

// drawTrayWith draws the icon's count and menu from the stored inbox and waiting, and
// keeps waiting's rows, for the next sessions change to compare with. A menu it could not
// draw is drawn in full on that change.
func (s *Shell) drawTrayWith(ctx context.Context, waiting []sessions.Session) {
	snap, err := s.inbox.Snapshot(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.WarnContext(ctx, "could not read the inbox for the menu bar", "error", err)
		}

		s.drawn = false

		return
	}

	now := s.clock.Now()
	sum := menubar.Summarize(snap, waiting, now)

	s.tray.Draw(sum)
	s.drawn, s.drawnWaiting = true, menubar.WaitingEntries(waiting)
	s.wakeAt(sum.NextChange, now)
}

// waiting returns the sessions waiting for the user, or none when they cannot be read,
// which is logged: the inbox's part of the menu still shows.
func (s *Shell) waiting(ctx context.Context) []sessions.Session {
	if s.sessions == nil {
		return nil
	}

	got, err := s.sessions.Waiting(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.WarnContext(ctx, "could not read the sessions for the menu bar", "error", err)
		}

		return nil
	}

	return got
}

// showsTray reports whether the icon is on and the shell is not stopping.
func (s *Shell) showsTray() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.trayOn && !s.stopping.Load()
}

// wakeAt sets the one snooze timer to redraw the menu bar just after next, replacing any
// timer set before. A zero next sets none, and so does a hidden icon or a stopping shell.
func (s *Shell) wakeAt(next, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stopTimer(&s.snooze)

	if next.IsZero() || !s.trayOn || s.stopping.Load() {
		return
	}

	s.snooze = time.AfterFunc(next.Sub(now)+snoozeSlack, s.askRedraw)
}
