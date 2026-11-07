package settings

import (
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// themeLog records the themes a ThemeNotifier was told about, in order.
type themeLog struct {
	mu     sync.Mutex
	themes []string
}

func (l *themeLog) ThemeChanged(theme string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.themes = append(l.themes, theme)
}

func (l *themeLog) got() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Clone(l.themes)
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
	require.NoError(t, NewService(db, home).SetTheme(ctx, ThemeLight))
	require.NoError(t, store.Close(db))

	got, err := NewService(testutil.DBAt(t, path), home).Theme(ctx)
	require.NoError(t, err)
	assert.Equal(t, ThemeLight, got)
}

func TestSetThemeRejectsAnUnknownTheme(t *testing.T) {
	log := &themeLog{}
	home := t.TempDir()
	svc := NewService(testutil.DB(t), home, WithThemeNotifier(log))
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
	assert.Equal(t, []string{ThemeDark}, log.got(), "a rejected theme must not be announced")
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
	svc := NewService(testutil.ClosedDB(t), t.TempDir())

	_, err := svc.Theme(t.Context())
	require.ErrorContains(t, err, "read the theme")

	require.ErrorContains(t, svc.SetTheme(t.Context(), ThemeDark), "save setting "+KeyTheme)
}

func TestSetThemeTellsTheNotifierOnceStored(t *testing.T) {
	log := &themeLog{}
	db := testutil.DB(t)
	svc := NewService(db, t.TempDir(), WithThemeNotifier(log))
	ctx := t.Context()

	require.NoError(t, svc.SetTheme(ctx, ThemeLight))
	require.NoError(t, svc.SetTheme(ctx, ThemeLight))
	require.NoError(t, svc.SetTheme(ctx, ThemeSystem))

	assert.Equal(t, []string{ThemeLight, ThemeLight, ThemeSystem}, log.got(), "every stored choice, in order")
}

func TestSetThemeWithoutANotifier(t *testing.T) {
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
