package setup

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// ResetWord is what the user types to confirm a reset.
const ResetWord = "reset"

var (
	// ErrNotConfirmed is returned for a reset the user didn't confirm by typing ResetWord.
	ErrNotConfirmed = errors.New("the reset was not confirmed")

	// ErrRunsGoing is returned for a reset asked for while runs are going in the app.
	ErrRunsGoing = errors.New("runs are going in the app")

	// ErrNoResetRequest is returned when the Service has nowhere to write a reset request.
	ErrNoResetRequest = errors.New("the reset request's path is not set")
)

// ResetStarted says what Reset did before it asked for the reset.
type ResetStarted struct {
	// SavedTo is the copy of the user's setup saved first, with ~ for home, or "" when
	// there was nowhere to save it.
	SavedTo string `json:"savedTo"`
}

// Reset asks for a fresh start, as Settings → About → Reset asks once the user typed
// ResetWord: it saves the user's whole setup into the backups folder first, refusing the
// reset when that fails, writes the request that the next start carries out, and tells the
// desktop shell (event.ResetRequested), which quits and opens the app again. The next start
// sets the database aside and starts a fresh one, bringing back the prompts, the launch
// defaults, and the user's own scripts. It refuses, in words the window shows, a
// confirmation that isn't ResetWord, and a reset while runs are going in the app, which
// the quit would stop.
func (s *Service) Reset(ctx context.Context, confirmation string) (ResetStarted, error) {
	if strings.TrimSpace(confirmation) != ResetWord {
		return ResetStarted{}, uierr.New("Type "+ResetWord+" to confirm.", ErrNotConfirmed)
	}

	if s.runs != nil && s.runs.Active() {
		return ResetStarted{}, uierr.New("Runs are going in the app. Wait for them to end, or stop them, "+
			"then reset.", ErrRunsGoing)
	}

	if s.request == "" {
		return ResetStarted{}, uierr.New("The reset couldn't be asked for, so nothing changed.", ErrNoResetRequest)
	}

	var started ResetStarted

	if s.saver != nil {
		saved, err := s.saver.SaveBeforeReset(ctx)
		if err != nil {
			return ResetStarted{}, uierr.New("Couldn't save a backup of your setup first, so nothing was reset.", err)
		}

		started.SavedTo = saved
	}

	if err := appdata.RequestReset(s.request, appdata.ResetBySettings, s.clock.Now()); err != nil {
		return ResetStarted{}, uierr.New("The reset couldn't be asked for, so nothing changed.",
			fmt.Errorf("ask for the reset: %w", err))
	}

	s.logger.InfoContext(ctx, "the user asked to reset the app; it restarts to carry it out", "saved", started.SavedTo)
	s.events.Emit(event.ResetRequested{})

	return started, nil
}
