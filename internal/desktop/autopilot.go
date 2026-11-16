package desktop

import (
	"context"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/menubar"
)

// Autopilot is what the shell reads of Autopilot and asks of it: its state, for the menu
// bar's Autopilot group and for the window's close, and Pause Autopilot, which the menu's
// checkbox row flips. Core adapts Autopilot's Service and the settings' setter, which
// stays the one writer of Autopilot's settings.
type Autopilot interface {
	// State reads Autopilot as the shell shows it, from the database alone.
	State(ctx context.Context) (AutopilotState, error)

	// SetPaused turns Pause Autopilot on or off, as Settings does.
	SetPaused(ctx context.Context, paused bool) error
}

// AutopilotState is Autopilot as the shell needs it.
type AutopilotState struct {
	// Menu is what the menu bar's Autopilot group is drawn from.
	Menu menubar.AutopilotState

	// KeepsRunning reports a routine switched on, or an agent run going: closing the
	// window then only hides it, whatever the menu bar icon setting, so the app keeps
	// running them.
	KeepsRunning bool
}

// askAutopilot asks for Autopilot's state to be read again, and the menu bar redrawn if
// its group reads otherwise: Autopilot changed. It never waits, and folds into a pending
// ask.
func (s *Shell) askAutopilot() {
	s.autopilotAsked.Store(true)
	s.signalRedraw()
}

// readAutopilot reads Autopilot's state, for the menu bar's group and the window's close,
// whether or not the icon shows. A state that can't be read keeps the last one read,
// logged.
func (s *Shell) readAutopilot(ctx context.Context) {
	if s.autopilot == nil {
		return
	}

	st, err := s.autopilot.State(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.WarnContext(ctx, "could not read Autopilot for the menu bar", "error", err)
		}

		return
	}

	s.autopilotMenu = st.Menu
	s.autopilotKeeps.Store(st.KeepsRunning)
	s.autopilotPaused.Store(st.Menu.Paused)
}

// AutopilotAction does what a click on a row of the menu bar's Autopilot group asks: an
// agent run's row opens the run, the row of the matches waiting for the user opens
// Activity, and Pause Autopilot flips it, from what the menu showed, through the settings
// in the background, which Stop waits for. A run or Activity opens in the window, which
// comes forward; until its tab exists, the window says why it can't open it.
func (s *Shell) AutopilotAction(action menubar.AutopilotAction, row menubar.Working) {
	if s.stopping.Load() {
		return
	}

	switch action {
	case menubar.AutopilotWatch:
		s.reveal(event.RevealItem{Autopilot: event.AutopilotTarget{
			RoutineID: row.RoutineID, MatchID: row.MatchID, RunID: row.RunID, CreatedAt: row.Created,
		}})
	case menubar.AutopilotActivity:
		s.reveal(event.RevealItem{Autopilot: event.AutopilotTarget{Activity: true}})
	case menubar.AutopilotPause:
		s.flipPause()
	}
}

// flipPause turns Pause Autopilot the other way from what the menu last showed, then has
// the menu read Autopilot again, so its tick follows what was stored.
func (s *Shell) flipPause() {
	if s.autopilot == nil {
		return
	}

	paused := !s.autopilotPaused.Load()

	s.goTracked(func(ctx context.Context) {
		if err := s.autopilot.SetPaused(ctx, paused); err != nil && ctx.Err() == nil {
			s.logger.WarnContext(ctx, "could not change Pause Autopilot from the menu bar", "paused", paused, "error", err)
		}

		s.askAutopilot()
	})
}

// showWindow brings the window forward and, after a close that only hid it because of
// Autopilot, the menu bar icon off, tells the page once, so it can say why the app kept
// running.
func (s *Shell) showWindow() {
	s.window.Show()

	if s.hidForAutopilot.CompareAndSwap(true, false) {
		s.frontend.Emit(event.KeptRunning{})
	}
}
