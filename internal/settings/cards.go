package settings

import (
	"context"
	"errors"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
)

// KeyCardAvatars is whether the inbox's cards show each author's picture: a person's in a
// circle and a bot's in a rounded square, downloaded through the avatars service without
// the token. Off, the cards show initials and the bot mark, and the window asks for no
// picture. Only the window reads it; the sheets show their pictures either way.
const KeyCardAvatars = "cards.avatars"

// DefaultCardAvatars is whether a fresh database shows the authors' pictures on the cards.
const DefaultCardAvatars = true

// KeyCardDensity is how much room each of the inbox's cards takes, in both lists, stored
// as one of the CardDensity choices. Only the window reads it.
const KeyCardDensity = "cards.density"

// CardDensity is how much room each of the inbox's cards takes. Both densities show the
// same next step, from the same rules.
type CardDensity string

// The cards' densities.
const (
	// CardComfortable shows each card on two lines, with the state track, and the agent's
	// slot beside the next step.
	CardComfortable CardDensity = "comfortable"

	// CardCompact shows each card on one line, a column per question, with the triage and
	// the agent's slot on hover.
	CardCompact CardDensity = "compact"
)

// DefaultCardDensity is the density of a fresh database.
const DefaultCardDensity = CardComfortable

// ErrInvalidCardDensity is returned for a card density that is not one of the choices.
var ErrInvalidCardDensity = errors.New("unknown card density")

// cardDensities is the card density choice: the densities in the order Settings offers
// them, falling back to DefaultCardDensity for a stored density that is unset or no longer
// a choice.
func cardDensities() choice[CardDensity] {
	return choice[CardDensity]{
		values:   []CardDensity{CardComfortable, CardCompact},
		fallback: DefaultCardDensity, invalid: ErrInvalidCardDensity,
	}
}

// SetCardAvatars stores whether the inbox's cards show the authors' pictures, and returns
// the settings with it.
func (s *Service) SetCardAvatars(ctx context.Context, on bool) (Settings, error) {
	return s.save(ctx, setting(KeyCardAvatars, prefval.EncodeBool(on)))
}

// SetCardDensity stores how much room the inbox's cards take, CardComfortable or
// CardCompact, and returns the settings with it. A density that is not one of the choices
// is refused, and nothing is stored.
func (s *Service) SetCardDensity(ctx context.Context, density CardDensity) (Settings, error) {
	values, err := checkCardDensity(density)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setMany(values, ""))
}

// checkCardDensity refuses a card density that is not one of the choices, and returns its
// write.
func checkCardDensity(density CardDensity) (map[string]string, error) {
	if err := cardDensities().check(density); err != nil {
		return nil, err
	}

	return map[string]string{KeyCardDensity: string(density)}, nil
}
