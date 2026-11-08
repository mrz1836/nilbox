package settings

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// KeyTheme is the theme the app shows, stored as ThemeSystem, ThemeLight, or ThemeDark.
const KeyTheme = "ui.theme"

// Theme is the palette the app shows.
type Theme string

// Theme choices.
const (
	// ThemeSystem follows the system's light or dark appearance as it changes.
	ThemeSystem Theme = "system"

	// ThemeLight always shows the light palette.
	ThemeLight Theme = "light"

	// ThemeDark always shows the dark palette.
	ThemeDark Theme = "dark"
)

// DefaultTheme is the theme of a fresh database.
const DefaultTheme = ThemeSystem

// ErrInvalidTheme is returned for a theme that is not one of the choices.
var ErrInvalidTheme = errors.New("unknown theme")

// themes returns the theme choices in the order Settings offers them.
func themes() []Theme {
	return []Theme{ThemeSystem, ThemeLight, ThemeDark}
}

// validTheme reports whether theme is one of the choices.
func validTheme(theme Theme) bool {
	return slices.Contains(themes(), theme)
}

// decodeTheme reads a stored theme, falling back to DefaultTheme for one that is unset or
// no longer a choice.
func decodeTheme(stored string) Theme {
	if theme := Theme(stored); validTheme(theme) {
		return theme
	}

	return DefaultTheme
}

// IsDark reports whether a theme shows the dark palette while the system appearance is
// dark (systemDark) or light. A theme that is not one of the choices counts as
// DefaultTheme.
func IsDark(theme Theme, systemDark bool) bool {
	switch theme {
	case ThemeDark:
		return true
	case ThemeLight:
		return false
	case ThemeSystem:
		return systemDark
	default:
		return IsDark(DefaultTheme, systemDark)
	}
}

// Theme returns the stored theme, or DefaultTheme when none is stored or the stored
// value is no longer a choice.
func (s *Service) Theme(ctx context.Context) (Theme, error) {
	stored, _, err := s.prefs.Get(ctx, KeyTheme)
	if err != nil {
		return "", fmt.Errorf("read the theme: %w", err)
	}

	return decodeTheme(stored), nil
}

// SetTheme stores the theme the app shows: ThemeSystem, ThemeLight, or ThemeDark, and
// returns the settings with it. Once it is stored, an event.ThemeChanged is emitted, so
// the desktop shell brings the window's native appearance along with it.
func (s *Service) SetTheme(ctx context.Context, theme Theme) (Settings, error) {
	if !validTheme(theme) {
		return Settings{}, fmt.Errorf("%w %q: choose one of %s", ErrInvalidTheme, theme, textutil.Join(themes(), ", "))
	}

	out, err := s.save(ctx, setting(KeyTheme, string(theme)))
	if err != nil {
		return Settings{}, err
	}

	s.events.Emit(event.ThemeChanged{Theme: string(theme)})

	return out, nil
}
