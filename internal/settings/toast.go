package settings

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

// KeyToastSeconds is how long, in seconds, a message at the bottom of the window (a toast,
// such as "Dismissed" with its Undo) stays before it closes itself.
const KeyToastSeconds = "ui.toastSeconds"

// The lengths a toast can be set to stay, in seconds, and the length of a fresh database.
// The frontend keeps the same three (frontend/src/components/Toast.tsx), and a test holds
// the two in step.
const (
	MinToastSeconds     = 5
	MaxToastSeconds     = 60
	DefaultToastSeconds = 12
)

// ErrInvalidToastSeconds is returned for a toast length outside MinToastSeconds to
// MaxToastSeconds.
var ErrInvalidToastSeconds = errors.New("unknown toast length")

// validToastSeconds reports whether seconds is a length a toast can be set to stay.
func validToastSeconds(seconds int) bool {
	return seconds >= MinToastSeconds && seconds <= MaxToastSeconds
}

// decodeToastSeconds reads the stored toast length: as stored, when it is one a toast can
// be set to stay, else DefaultToastSeconds.
func decodeToastSeconds(stored string) int {
	if v, err := strconv.Atoi(stored); err == nil && validToastSeconds(v) {
		return v
	}

	return DefaultToastSeconds
}

// SetToastSeconds stores how long, in seconds, a toast stays before it closes itself,
// MinToastSeconds to MaxToastSeconds, and returns the settings with it. The window reads
// it from the settings, so the next toast stays that long.
func (s *Service) SetToastSeconds(ctx context.Context, seconds int) (Settings, error) {
	values, err := checkToastSeconds(seconds)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setMany(values, ""))
}

// checkToastSeconds refuses a toast length outside MinToastSeconds to MaxToastSeconds,
// and returns its write.
func checkToastSeconds(seconds int) (map[string]string, error) {
	if !validToastSeconds(seconds) {
		return nil, fmt.Errorf("%w: %d seconds; choose %d to %d", ErrInvalidToastSeconds, seconds,
			MinToastSeconds, MaxToastSeconds)
	}

	return map[string]string{KeyToastSeconds: strconv.Itoa(seconds)}, nil
}
