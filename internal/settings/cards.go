package settings

import (
	"context"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
)

// KeyCardAvatars is whether the inbox's cards show each author's picture: a person's in a
// circle and a bot's in a rounded square, downloaded through the avatars service without
// the token. Off, the cards show initials and the bot mark, and the window asks for no
// picture. Only the window reads it; the sheets show their pictures either way.
const KeyCardAvatars = "cards.avatars"

// DefaultCardAvatars is whether a fresh database shows the authors' pictures on the cards.
const DefaultCardAvatars = true

// SetCardAvatars stores whether the inbox's cards show the authors' pictures, and returns
// the settings with it.
func (s *Service) SetCardAvatars(ctx context.Context, on bool) (Settings, error) {
	return s.save(ctx, setting(KeyCardAvatars, prefval.EncodeBool(on)))
}
