package settings

import (
	"context"
	"strconv"
)

// KeyProjectsTipDismissed is whether the user put away the inbox's tip about showing the
// GitHub projects an issue is in, which the inbox offers while the token lacks the
// read:project scope.
const KeyProjectsTipDismissed = "ui.projectsTipDismissed"

// SetProjectsTipDismissed stores whether the inbox's tip about showing issue projects is
// put away, and returns the settings with it.
func (s *Service) SetProjectsTipDismissed(ctx context.Context, dismissed bool) (Settings, error) {
	return s.save(ctx, setting(KeyProjectsTipDismissed, strconv.FormatBool(dismissed)))
}

// decodeProjectsTipDismissed reads the stored setting; anything but true leaves the tip
// out.
func decodeProjectsTipDismissed(stored string) bool {
	dismissed, err := strconv.ParseBool(stored)

	return err == nil && dismissed
}
