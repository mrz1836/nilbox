package settings

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
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

// ThemeNotifier is told when the theme was changed and stored, so the desktop shell can
// bring the window's native appearance along with it.
type ThemeNotifier interface {
	ThemeChanged(theme string)
}

// Option configures a Service.
type Option func(*Service)

// WithThemeNotifier tells n about every theme change once it is stored.
func WithThemeNotifier(n ThemeNotifier) Option {
	return func(s *Service) { s.themes = n }
}

// Themes returns the theme choices in the order Settings offers them.
func Themes() []string {
	return []string{ThemeSystem, ThemeLight, ThemeDark}
}

// ValidTheme reports whether theme is one of the choices.
func ValidTheme(theme string) bool {
	return slices.Contains(Themes(), theme)
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
	var stored []string

	err := s.db.WithContext(ctx).Raw("SELECT value FROM prefs WHERE key = ?", KeyTheme).Scan(&stored).Error
	if err != nil {
		return "", fmt.Errorf("read the theme: %w", err)
	}

	if len(stored) == 1 && ValidTheme(stored[0]) {
		return stored[0], nil
	}

	return DefaultTheme, nil
}

// SetTheme stores the theme the app shows: ThemeSystem, ThemeLight, or ThemeDark. Once
// it is stored, the theme notifier, if any, is told.
func (s *Service) SetTheme(ctx context.Context, theme string) error {
	if !ValidTheme(theme) {
		return fmt.Errorf("%w %q: choose one of %s", ErrInvalidTheme, theme, strings.Join(Themes(), ", "))
	}

	if err := s.set(ctx, KeyTheme, theme); err != nil {
		return err
	}

	if s.themes != nil {
		s.themes.ThemeChanged(theme)
	}

	return nil
}
