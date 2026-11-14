package settings

import (
	"context"
	"errors"
	"fmt"

	"github.com/mrz1836/nilbox/internal/event"
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

// themes is the theme choice: the themes in the order Settings offers them, falling back
// to DefaultTheme for a stored theme that is unset or no longer a choice.
func themes() choice[Theme] {
	return choice[Theme]{
		values: []Theme{ThemeSystem, ThemeLight, ThemeDark}, fallback: DefaultTheme, invalid: ErrInvalidTheme,
	}
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

	return themes().decode(stored), nil
}

// SetTheme stores the theme the app shows: ThemeSystem, ThemeLight, or ThemeDark, and
// returns the settings with it. Once it is stored, an event.ThemeChanged is emitted, so
// the desktop shell brings the window's native appearance along with it.
func (s *Service) SetTheme(ctx context.Context, theme Theme) (Settings, error) {
	values, err := checkTheme(theme)
	if err != nil {
		return Settings{}, err
	}

	out, err := s.save(ctx, setMany(values, ""))
	if err != nil {
		return Settings{}, err
	}

	s.events.Emit(event.ThemeChanged{Theme: string(theme)})

	return out, nil
}

// checkTheme refuses a theme that is not one of the choices, and returns its write.
func checkTheme(theme Theme) (map[string]string, error) {
	if err := themes().check(theme); err != nil {
		return nil, err
	}

	return map[string]string{KeyTheme: string(theme)}, nil
}
