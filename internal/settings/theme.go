package settings

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/event"
)

// KeyTheme is the theme the app shows, stored as ThemeSystem, ThemeLight, or ThemeDark.
const KeyTheme = "ui.theme"

// Theme choices.
const (
	// ThemeSystem follows the system's light or dark appearance as it changes.
	ThemeSystem = "system"

	// ThemeLight always shows the light palette.
	ThemeLight = "light"

	// ThemeDark always shows the dark palette.
	ThemeDark = "dark"

	// DefaultTheme is the theme of a fresh database.
	DefaultTheme = ThemeSystem
)

// ErrInvalidTheme is returned for a theme that is not one of the choices.
var ErrInvalidTheme = errors.New("unknown theme")

// themeNames returns the theme choices in the order Settings offers them.
func themeNames() []string {
	return []string{ThemeSystem, ThemeLight, ThemeDark}
}

// validTheme reports whether theme is one of the choices.
func validTheme(theme string) bool {
	return slices.Contains(themeNames(), theme)
}

// IsDark reports whether a theme shows the dark palette while the system appearance is
// dark (systemDark) or light. A theme that is not one of the choices counts as
// DefaultTheme.
func IsDark(theme string, systemDark bool) bool {
	switch theme {
	case ThemeDark:
		return true
	case ThemeLight:
		return false
	default:
		return systemDark
	}
}

// Theme returns the stored theme, or DefaultTheme when none is stored or the stored
// value is no longer a choice.
func (s *Service) Theme(ctx context.Context) (string, error) {
	stored, _, err := s.prefs.Get(ctx, KeyTheme)
	if err != nil {
		return "", fmt.Errorf("read the theme: %w", err)
	}

	if validTheme(stored) {
		return stored, nil
	}

	return DefaultTheme, nil
}

// SetTheme stores the theme the app shows: ThemeSystem, ThemeLight, or ThemeDark. Once
// it is stored, an event.ThemeChanged is emitted, so the desktop shell brings the
// window's native appearance along with it.
func (s *Service) SetTheme(ctx context.Context, theme string) error {
	if !validTheme(theme) {
		return fmt.Errorf("%w %q: choose one of %s", ErrInvalidTheme, theme, strings.Join(themeNames(), ", "))
	}

	if err := s.prefs.Set(ctx, KeyTheme, theme); err != nil {
		return err
	}

	s.events.Emit(event.ThemeChanged{Theme: theme})

	return nil
}
