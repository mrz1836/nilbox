package settings

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
)

// KeyNudgeAfterHours is how many hours a pull request the user requested changes on has to
// be quiet before its card offers to nudge its author, stored as a whole number; 0 offers
// none. KeyNudgeSnooze is whether the nudge dialog starts with "Then snooze" ticked. Only
// the window reads them: the inbox posts a nudge whatever they say.
const (
	KeyNudgeAfterHours = "review.nudgeAfterHours"
	KeyNudgeSnooze     = "review.nudgeSnooze"
)

// The hours a pull request can be set to be quiet before a nudge is offered, beyond 0 for
// never, the hours of a fresh database, and whether a fresh database snoozes after a nudge.
const (
	MaxNudgeAfterHours     = 720
	DefaultNudgeAfterHours = 24
	DefaultNudgeSnooze     = false
)

// ErrInvalidNudgeAfterHours is returned for a quiet time outside 0 to MaxNudgeAfterHours.
var ErrInvalidNudgeAfterHours = errors.New("unknown time before a nudge")

// validNudgeAfterHours reports whether hours is a quiet time a nudge can wait for: 0, for
// none, to MaxNudgeAfterHours.
func validNudgeAfterHours(hours int) bool {
	return hours >= 0 && hours <= MaxNudgeAfterHours
}

// decodeNudgeAfterHours reads the stored quiet time: as stored, when it is one a nudge can
// wait for, else DefaultNudgeAfterHours.
func decodeNudgeAfterHours(stored string) int {
	if v, err := strconv.Atoi(stored); err == nil && validNudgeAfterHours(v) {
		return v
	}

	return DefaultNudgeAfterHours
}

// SetNudgeAfterHours stores how many hours a pull request waiting on its author has to be
// quiet before its card offers a nudge, 0 for never to MaxNudgeAfterHours, and returns the
// settings with it.
func (s *Service) SetNudgeAfterHours(ctx context.Context, hours int) (Settings, error) {
	values, err := checkNudgeAfterHours(hours)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setMany(values, ""))
}

// checkNudgeAfterHours refuses a quiet time outside 0 to MaxNudgeAfterHours, and returns
// its write.
func checkNudgeAfterHours(hours int) (map[string]string, error) {
	if !validNudgeAfterHours(hours) {
		return nil, fmt.Errorf("%w: %d hours; choose 0 (never) to %d", ErrInvalidNudgeAfterHours, hours,
			MaxNudgeAfterHours)
	}

	return map[string]string{KeyNudgeAfterHours: strconv.Itoa(hours)}, nil
}

// SetNudgeSnooze stores whether the nudge dialog starts with "Then snooze" ticked, so a
// nudge snoozes its pull request until the author answers, and returns the settings with
// it.
func (s *Service) SetNudgeSnooze(ctx context.Context, on bool) (Settings, error) {
	return s.save(ctx, setting(KeyNudgeSnooze, prefval.EncodeBool(on)))
}
