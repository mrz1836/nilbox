package settings

import (
	"context"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
)

// KeyProjectsTipDismissed is whether the user put away the inbox's tip about showing the
// GitHub projects an issue is in, which the inbox offers while the token lacks the
// read:project scope.
const KeyProjectsTipDismissed = "ui.projectsTipDismissed"

// SetProjectsTipDismissed stores whether the inbox's tip about showing issue projects is
// put away, and returns the settings with it.
func (s *Service) SetProjectsTipDismissed(ctx context.Context, dismissed bool) (Settings, error) {
	return s.save(ctx, setting(KeyProjectsTipDismissed, prefval.EncodeBool(dismissed)))
}

// KeyKeptRunningTold is whether the window said, once, that closing it only hid it because
// Autopilot has a routine on or an agent working, the menu bar icon off: the window's
// memory, which a backup never holds.
const KeyKeptRunningTold = "ui.keptRunningTold"

// SetKeptRunningTold stores whether the window said why the app kept running after its
// close, and returns the settings with it.
func (s *Service) SetKeptRunningTold(ctx context.Context, told bool) (Settings, error) {
	return s.save(ctx, setting(KeyKeptRunningTold, prefval.EncodeBool(told)))
}
