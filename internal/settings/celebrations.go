package settings

import (
	"context"
	"errors"
)

// KeyCelebrations is how the window celebrates, stored as one of the Celebrations choices.
// Only the window reads it: the inbox's party at nil, and the bursts of confetti that end
// the first-run setup and greet a release on GitHub.
const KeyCelebrations = "ui.celebrations"

// Celebrations is how the window celebrates: the inbox reaching nil, the first-run setup's
// end, and a release on GitHub.
type Celebrations string

// The celebrations' choices.
const (
	// CelebrationsMilestones throws the confetti for what it means: the inbox's party
	// throws it on its own for an achievement (the milestones package), at most once a
	// day, and the setup's end and a release each throw a burst. Every other party plays a
	// small flourish instead.
	CelebrationsMilestones Celebrations = "milestones"

	// CelebrationsQuiet keeps the party's flourishes and the achievements it shows, and
	// throws confetti only when the user runs the party again.
	CelebrationsQuiet Celebrations = "quiet"

	// CelebrationsOff shows the inbox at nil as a still strip, and throws nothing.
	CelebrationsOff Celebrations = "off"
)

// DefaultCelebrations is how a fresh database celebrates.
const DefaultCelebrations = CelebrationsMilestones

// ErrInvalidCelebrations is returned for a celebrations choice that is not one of them.
var ErrInvalidCelebrations = errors.New("unknown celebrations")

// celebrationChoices is the celebrations choice: the choices in the order Settings offers
// them, falling back to DefaultCelebrations for a stored choice that is unset or no longer
// one.
func celebrationChoices() choice[Celebrations] {
	return choice[Celebrations]{
		values:   []Celebrations{CelebrationsMilestones, CelebrationsQuiet, CelebrationsOff},
		fallback: DefaultCelebrations, invalid: ErrInvalidCelebrations,
	}
}

// SetCelebrations stores how the window celebrates, CelebrationsMilestones,
// CelebrationsQuiet, or CelebrationsOff, and returns the settings with it. A choice that is
// not one of them is refused, and nothing is stored.
func (s *Service) SetCelebrations(ctx context.Context, c Celebrations) (Settings, error) {
	values, err := checkCelebrations(c)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setMany(values, ""))
}

// checkCelebrations refuses a celebrations choice that is not one of them, and returns its
// write.
func checkCelebrations(c Celebrations) (map[string]string, error) {
	if err := celebrationChoices().check(c); err != nil {
		return nil, err
	}

	return map[string]string{KeyCelebrations: string(c)}, nil
}
