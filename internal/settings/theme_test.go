package settings

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// themeEvents returns the theme changes for themes, in order.
func themeEvents(themes ...string) []event.Event {
	out := make([]event.Event, 0, len(themes))
	for _, theme := range themes {
		out = append(out, event.ThemeChanged{Theme: theme})
	}

	return out
}

func TestThemeIsSystemOnAFreshDatabase(t *testing.T) {
	svc, _, _ := newTestService(t)

	got, err := svc.Theme(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ThemeSystem, got)
	assert.Equal(t, ThemeSystem, DefaultTheme)
}

func TestThemePersists(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := t.Context()

	for _, theme := range []string{ThemeDark, ThemeLight, ThemeSystem, ThemeDark} {
		require.NoError(t, svc.SetTheme(ctx, theme))

		got, err := svc.Theme(ctx)
		require.NoError(t, err)
		assert.Equal(t, theme, got)
	}
}

func TestThemeSurvivesARestart(t *testing.T) {
	ctx := t.Context()
	home := t.TempDir()
	path := filepath.Join(t.TempDir(), "app.db")

	db := testutil.DBAt(t, path)
	require.NoError(t, serviceOver(t, db, home).SetTheme(ctx, ThemeLight))
	require.NoError(t, db.Close())

	got, err := serviceOver(t, testutil.DBAt(t, path), home).Theme(ctx)
	require.NoError(t, err)
	assert.Equal(t, ThemeLight, got)
}

func TestSetThemeRejectsAnUnknownTheme(t *testing.T) {
	events := &testutil.Events{}
	home := t.TempDir()
	svc := serviceOver(t, testutil.DB(t), home, WithEvents(events))
	ctx := t.Context()

	require.NoError(t, svc.SetTheme(ctx, ThemeDark))

	for _, theme := range []string{"", "Dark", " dark", "auto", "sepia", "ui.theme"} {
		require.ErrorIs(t, svc.SetTheme(ctx, theme), ErrInvalidTheme, theme)
	}

	err := svc.SetTheme(ctx, "dim")
	require.ErrorContains(t, err, `"dim": choose one of system, light, dark`)

	got, err := svc.Theme(ctx)
	require.NoError(t, err)
	assert.Equal(t, ThemeDark, got, "a rejected theme must not be stored")
	assert.Equal(t, themeEvents(ThemeDark), events.All(), "a rejected theme must not be announced")
}

func TestThemeIgnoresABadStoredValue(t *testing.T) {
	svc, db, _ := newTestService(t)

	for _, stored := range []string{"", "auto", "DARK", "light "} {
		setPref(t, db, KeyTheme, stored)

		got, err := svc.Theme(t.Context())
		require.NoError(t, err)
		assert.Equal(t, DefaultTheme, got, "%q falls back to the default", stored)
	}
}

func TestThemeReportsAClosedDatabase(t *testing.T) {
	svc := serviceOver(t, closedDB(t), t.TempDir())

	_, err := svc.Theme(t.Context())
	require.ErrorContains(t, err, "read the theme")

	require.ErrorContains(t, svc.SetTheme(t.Context(), ThemeDark), "save setting "+KeyTheme)
}

func TestSetThemeEmitsOnceStored(t *testing.T) {
	events := &testutil.Events{}
	db := testutil.DB(t)
	svc := serviceOver(t, db, t.TempDir(), WithEvents(events))
	ctx := t.Context()

	require.NoError(t, svc.SetTheme(ctx, ThemeLight))
	require.NoError(t, svc.SetTheme(ctx, ThemeLight))
	require.NoError(t, svc.SetTheme(ctx, ThemeSystem))

	assert.Equal(t, themeEvents(ThemeLight, ThemeLight, ThemeSystem), events.All(), "every stored choice, in order")
}

func TestSetThemeWithoutAnEventSink(t *testing.T) {
	svc, _, _ := newTestService(t)

	require.NoError(t, svc.SetTheme(t.Context(), ThemeDark))
}

func TestThemesAreTheChoicesInOrder(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"system", "light", "dark"}, themeNames())

	for _, theme := range themeNames() {
		assert.True(t, validTheme(theme), theme)
	}

	assert.False(t, validTheme("auto"))
}

func TestIsDark(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		theme      string
		systemDark bool
		want       bool
	}{
		{ThemeSystem, true, true},
		{ThemeSystem, false, false},
		{ThemeDark, false, true},
		{ThemeDark, true, true},
		{ThemeLight, true, false},
		{ThemeLight, false, false},
		{"unknown", true, true},
		{"", false, false},
	} {
		assert.Equal(t, tc.want, IsDark(tc.theme, tc.systemDark), "%q with the system dark: %t", tc.theme, tc.systemDark)
	}
}
