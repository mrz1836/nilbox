package settings

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
)

// KeyMenuBar is whether the app shows its icon, with how many items need the user, in the
// menu bar; closing the window then keeps the app running there.
const KeyMenuBar = "ui.menuBar"

// DefaultMenuBar is the menu bar setting of a fresh database.
const DefaultMenuBar = true

// MenuBar returns whether the app shows its menu bar icon: the stored setting, or
// DefaultMenuBar when none is stored or the stored value cannot be read.
func (s *Service) MenuBar(ctx context.Context) (bool, error) {
	stored, _, err := s.prefs.Get(ctx, KeyMenuBar)
	if err != nil {
		return DefaultMenuBar, fmt.Errorf("read the menu bar setting: %w", err)
	}

	return decodeMenuBar(stored), nil
}

// SetMenuBar stores whether the app shows its menu bar icon. Once it is stored, an
// event.MenuBarChanged is emitted, so the desktop shell adds or removes the icon.
func (s *Service) SetMenuBar(ctx context.Context, on bool) error {
	if err := s.prefs.Set(ctx, KeyMenuBar, strconv.FormatBool(on)); err != nil {
		return err
	}

	s.events.Emit(event.MenuBarChanged{On: on})

	return nil
}

// SetNotificationBanner turns the banners of one kind of notification on or off (see the
// notify package for the kinds). The notification history keeps every kind either way.
func (s *Service) SetNotificationBanner(ctx context.Context, kind string, on bool) error {
	err := s.prefs.Update(ctx, notifyprefs.KeyBannersOff, func(stored string, _ bool) (string, error) {
		off, err := notifyprefs.SetBanner(notifyprefs.DecodeBannersOff(stored), kind, on)
		if err != nil {
			return "", err
		}

		return notifyprefs.EncodeBannersOff(off), nil
	})
	if err != nil {
		return fmt.Errorf("save the notification settings: %w", err)
	}

	return nil
}

// decodeMenuBar reads a stored menu bar setting, falling back to the default for one
// that is unset ("") or cannot be read.
func decodeMenuBar(stored string) bool {
	if on, err := strconv.ParseBool(stored); err == nil {
		return on
	}

	return DefaultMenuBar
}
