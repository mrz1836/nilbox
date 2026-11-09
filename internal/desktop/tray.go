package desktop

import (
	"context"
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

// askRedraw asks for the menu bar to be redrawn. It never waits: an ask that comes while
// another is pending folds into it.
func (s *Shell) askRedraw() {
	select {
	case s.redraw <- struct{}{}:
	default:
	}
}

// redrawTray redraws the menu bar each time it is asked to, one redraw at a time, until
// ctx ends.
func (s *Shell) redrawTray(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.redraw:
			s.drawTray(ctx)
		}
	}
}

// drawTray draws the icon's count and menu from the stored inbox and the sessions waiting
// for the user, while the icon shows,
// and sets the timer for the soonest snooze to end, which changes the count with nothing
// stored.
func (s *Shell) drawTray(ctx context.Context) {
	if !s.showsTray() {
		return
	}

	snap, err := s.inbox.Snapshot(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.WarnContext(ctx, "could not read the inbox for the menu bar", "error", err)
		}

		return
	}

	now := s.clock.Now()
	sum := menubar.Summarize(snap, s.waiting(ctx), now)

	s.tray.Draw(sum)
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
