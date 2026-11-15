package settings

import (
	"context"
	"fmt"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

// SetUpdateMode stores what the app does when a new version is out (see the updateprefs
// package): download it and tell the user, just tell them, or make no timed checks. It
// returns the settings with it. Once it is stored, an event.UpdatesChanged is emitted, so
// the update check follows the new choice at once.
func (s *Service) SetUpdateMode(ctx context.Context, mode updateprefs.Mode) (Settings, error) {
	values, err := checkUpdateMode(mode)
	if err != nil {
		return Settings{}, err
	}

	saved, err := s.save(ctx, setMany(values, ""))
	if err != nil {
		return saved, err
	}

	s.events.Emit(event.UpdatesChanged{})

	return saved, nil
}

// checkUpdateMode refuses a mode that is not one of the choices, and returns its write.
func checkUpdateMode(mode updateprefs.Mode) (map[string]string, error) {
	if !mode.Valid() {
		return nil, uierr.New("That isn't one of the choices for new versions.",
			fmt.Errorf("%w %q: choose one of %s", updateprefs.ErrUnknownMode, mode,
				textutil.Join(updateprefs.Modes(), ", ")))
	}

	return map[string]string{updateprefs.KeyMode: string(mode)}, nil
}
