package settings

import (
	"context"
	"fmt"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/prefs/prefval"
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

	return prefval.Bool(stored, DefaultMenuBar), nil
}

// SetMenuBar stores whether the app shows its menu bar icon, and returns the settings
// with it. Once it is stored, an event.MenuBarChanged is emitted, so the desktop shell
// adds or removes the icon.
func (s *Service) SetMenuBar(ctx context.Context, on bool) (Settings, error) {
	out, err := s.save(ctx, setting(KeyMenuBar, prefval.EncodeBool(on)))
	if err != nil {
		return Settings{}, err
	}

	s.events.Emit(event.MenuBarChanged{On: on})

	return out, nil
}

// checkBannersOff refuses a list of the kinds whose banners are off that names one this
// build doesn't know, and returns the writes of both banner keys, which together say of
// every kind whether its banners are off.
func checkBannersOff(kinds []notifyprefs.Kind) (map[string]string, error) {
	for _, kind := range kinds {
		if !kind.Valid() {
			return nil, fmt.Errorf("%w %q", notifyprefs.ErrUnknownKind, kind)
		}
	}

	storedOff, storedOn := notifyprefs.EncodeBanners(kinds)

	return map[string]string{notifyprefs.KeyBannersOff: storedOff, notifyprefs.KeyBannersOn: storedOn}, nil
}

// SetNotificationBanner turns the banners of one kind of notification on or off (see the
// notify package for the kinds), and returns the settings with it. The notification
// history keeps every kind either way.
//
// A kind that starts on is stored in notifyprefs.KeyBannersOff once turned off, and one
// that starts off, such as the agent sessions' kinds, in notifyprefs.KeyBannersOn once
// turned on; both keys are written together.
func (s *Service) SetNotificationBanner(ctx context.Context, kind notifyprefs.Kind, on bool) (Settings, error) {
	return s.save(ctx, func(ctx context.Context, p *prefs.Store) error {
		stored, err := p.GetMany(ctx, notifyprefs.KeyBannersOff, notifyprefs.KeyBannersOn)
		if err != nil {
			return fmt.Errorf("read the notification settings: %w", err)
		}

		off, err := notifyprefs.SetBanner(
			notifyprefs.DecodeBannersOff(stored[notifyprefs.KeyBannersOff], stored[notifyprefs.KeyBannersOn]), kind, on,
		)
		if err != nil {
			return err
		}

		storedOff, storedOn := notifyprefs.EncodeBanners(off)

		err = p.SetMany(ctx, map[string]string{notifyprefs.KeyBannersOff: storedOff, notifyprefs.KeyBannersOn: storedOn})
		if err != nil {
			return fmt.Errorf("save the notification settings: %w", err)
		}

		return nil
	})
}
