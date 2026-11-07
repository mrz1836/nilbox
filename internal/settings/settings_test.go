package settings

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/trust"
)

// errRefused is what a test's database callback fails with.
var errRefused = errors.New("refused by the test")

// newTestService returns a Service over a fresh database and a temp home directory.
func newTestService(t *testing.T) (*Service, *gorm.DB, string) {
	t.Helper()

	home := t.TempDir()
	db := testutil.DB(t)

	return NewService(db, home), db, home
}

// setPref writes a raw preference value, bypassing validation.
func setPref(t *testing.T, db *gorm.DB, key, value string) {
	t.Helper()

	require.NoError(t, db.Exec(upsertPrefSQL, key, value, "2026-01-10T12:00:00Z").Error)
}

func TestPrefDefaults(t *testing.T) {
	svc, _, _ := newTestService(t)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, Settings{
		DefaultProfile:      "default",
		WorktreeDefault:     true,
		WorktreeAutoCleanup: true,
		Terminal:            "iterm2",
		CloneRoot:           "~/projects",
		TrustedBots:         []string{"dependabot", "github-actions", "renovate"},
		DefaultTrustedBots:  []string{"dependabot", "github-actions", "renovate"},
		SnoozeDays:          3,
		SnoozeWake:          snooze.Rules{Commits: 1, Reviews: "decisions", Comments: 0},
		WindowSize:          WindowSize{Width: 1280, Height: 800},
		DefaultWindowSize:   WindowSize{Width: 1280, Height: 800},
		SensitiveFiles:      sensitive.Config{Scope: "collaborators", Off: []string{}, Patterns: []string{}},
		SensitiveCategories: sensitive.Categories(),
		MenuBar:             DefaultMenuBar,
		BannersOff:          []string{},
	}, got)
}

func TestTrustedBots(t *testing.T) {
	svc, db, _ := newTestService(t)
	ctx := t.Context()

	require.NoError(t, svc.SetTrustedBots(ctx, []string{"Renovate[bot]", "app/my-release-bot", " ", "renovate"}))

	got, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"my-release-bot", "renovate"}, got.TrustedBots, "normalized, deduplicated, and sorted")

	require.NoError(t, svc.SetTrustedBots(ctx, nil))

	got, err = svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{}, got.TrustedBots, "an emptied list trusts no bot")

	require.ErrorIs(t, svc.SetTrustedBots(ctx, []string{"not a login"}), trust.ErrInvalidBot)

	require.NoError(t, svc.ResetTrustedBots(ctx))

	got, err = svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, trust.DefaultBots(), got.TrustedBots)

	setPref(t, db, KeyTrustedBots, "{broken")

	got, err = svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, trust.DefaultBots(), got.TrustedBots, "an unreadable list falls back to the defaults")
}

func TestTerminalPrefPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	home := t.TempDir()

	db := testutil.DBAt(t, path)
	require.NoError(t, NewService(db, home).SetTerminal(t.Context(), TerminalApp))
	require.NoError(t, store.Close(db))

	got, err := NewService(testutil.DBAt(t, path), home).Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, TerminalApp, got.Terminal)
}

func TestSettersPersist(t *testing.T) {
	svc, db, home := newTestService(t)
	require.NoError(t, profiles.Sync(t.Context(), db, []profiles.Profile{{Name: "work", ConfigDir: "/opt/profiles/work"}}))
	require.NoError(t, os.MkdirAll(filepath.Join(home, "code", "clones"), 0o700))

	require.NoError(t, svc.SetDefaultProfile(t.Context(), "work"))
	require.NoError(t, svc.SetWorktreeDefault(t.Context(), false))
	require.NoError(t, svc.SetWorktreeAutoCleanup(t.Context(), false))
	require.NoError(t, svc.SetTerminal(t.Context(), TerminalApp))
	require.NoError(t, svc.SetTerminal(t.Context(), TerminalITerm2))
	require.NoError(t, svc.SetCloneRoot(t.Context(), " ~/code/clones/ "))
	require.NoError(t, svc.SetSnoozeDays(t.Context(), 7))
	require.NoError(t, svc.SetSnoozeWake(t.Context(), snooze.Rules{Commits: 3, Reviews: snooze.ReviewsAny, Comments: 5}))
	require.NoError(t, svc.SetMenuBar(t.Context(), false))
	require.NoError(t, svc.SetNotificationBanner(t.Context(), notify.KindWoke, false))

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, Settings{
		DefaultProfile:      "work",
		WorktreeDefault:     false,
		WorktreeAutoCleanup: false,
		Terminal:            TerminalITerm2,
		CloneRoot:           "~/code/clones",
		TrustedBots:         trust.DefaultBots(),
		DefaultTrustedBots:  trust.DefaultBots(),
		SnoozeDays:          7,
		SnoozeWake:          snooze.Rules{Commits: 3, Reviews: snooze.ReviewsAny, Comments: 5},
		WindowSize:          DefaultWindowSize(),
		DefaultWindowSize:   DefaultWindowSize(),
		SensitiveFiles:      sensitive.DefaultConfig(),
		SensitiveCategories: sensitive.Categories(),
		MenuBar:             false,
		BannersOff:          []string{notify.KindWoke},
	}, got)

	abs := t.TempDir()
	require.NoError(t, svc.SetCloneRoot(t.Context(), abs))
	require.NoError(t, svc.SetWorktreeDefault(t.Context(), true))
	require.NoError(t, svc.SetWorktreeAutoCleanup(t.Context(), true))

	got, err = svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, abs, got.CloneRoot)
	assert.True(t, got.WorktreeDefault)
	assert.True(t, got.WorktreeAutoCleanup)
}

func TestSetRejectsInvalid(t *testing.T) {
	svc, db, home := newTestService(t)
	require.NoError(t, profiles.Sync(t.Context(), db, []profiles.Profile{{Name: "default"}}))

	file := filepath.Join(home, "notes.txt")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))

	tests := []struct {
		name    string
		set     func() error
		wantErr error
		wantMsg string
	}{
		{name: "unknown terminal", set: func() error { return svc.SetTerminal(t.Context(), "warp") }, wantErr: ErrInvalidTerminal, wantMsg: `"warp"`},
		{name: "empty terminal", set: func() error { return svc.SetTerminal(t.Context(), "") }, wantErr: ErrInvalidTerminal},
		{name: "terminal in the wrong case", set: func() error { return svc.SetTerminal(t.Context(), "ITERM2") }, wantErr: ErrInvalidTerminal},
		{name: "unknown profile", set: func() error { return svc.SetDefaultProfile(t.Context(), "missing") }, wantErr: ErrUnknownProfile, wantMsg: `"missing"`},
		{name: "empty profile", set: func() error { return svc.SetDefaultProfile(t.Context(), "") }, wantErr: ErrUnknownProfile},
		{name: "empty clone root", set: func() error { return svc.SetCloneRoot(t.Context(), "  ") }, wantErr: paths.ErrEmpty},
		{name: "relative clone root", set: func() error { return svc.SetCloneRoot(t.Context(), "projects") }, wantErr: paths.ErrRelative},
		{name: "missing clone root", set: func() error { return svc.SetCloneRoot(t.Context(), "~/missing") }, wantErr: fs.ErrNotExist},
		{name: "clone root is a file", set: func() error { return svc.SetCloneRoot(t.Context(), file) }, wantErr: ErrInvalidCloneRoot, wantMsg: "~/notes.txt is not a directory"},
		{name: "no snooze", set: func() error { return svc.SetSnoozeDays(t.Context(), 0) }, wantErr: snooze.ErrInvalidDays, wantMsg: "0 days; choose 1 to 365"},
		{name: "snooze past a year", set: func() error { return svc.SetSnoozeDays(t.Context(), 366) }, wantErr: snooze.ErrInvalidDays},
		{name: "unknown review rule", set: func() error { return svc.SetSnoozeWake(t.Context(), snooze.Rules{Reviews: "sometimes"}) }, wantErr: snooze.ErrInvalidRules, wantMsg: `"sometimes"`},
		{name: "negative comment rule", set: func() error {
			return svc.SetSnoozeWake(t.Context(), snooze.Rules{Reviews: snooze.ReviewsAny, Comments: -1})
		}, wantErr: snooze.ErrInvalidRules},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.set()
			require.ErrorIs(t, err, tt.wantErr)

			if tt.wantMsg != "" {
				assert.Contains(t, err.Error(), tt.wantMsg)
			}
		})
	}

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, fromStored(nil), got, "rejected values must not be stored")
}

func TestCloneRootErrorsAreInvalidCloneRoot(t *testing.T) {
	svc, _, _ := newTestService(t)

	for _, root := range []string{"", "projects", "~/missing"} {
		require.ErrorIs(t, svc.SetCloneRoot(t.Context(), root), ErrInvalidCloneRoot, root)
	}
}

func TestGetFallsBackOnBadStoredValues(t *testing.T) {
	svc, db, _ := newTestService(t)
	setPref(t, db, KeyDefaultProfile, "")
	setPref(t, db, KeyWorktreeDefault, "maybe")
	setPref(t, db, KeyWorktreeAutoCleanup, "sometimes")
	setPref(t, db, KeyTerminal, "warp")
	setPref(t, db, KeyCloneRoot, "   ")
	setPref(t, db, KeySnoozeDays, "-2")
	setPref(t, db, snooze.PrefWakeReviews, "sometimes")
	setPref(t, db, snooze.PrefWakeCommits, "lots")

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, fromStored(nil), got)

	setPref(t, db, KeySnoozeDays, "soon")

	got, err = svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, fromStored(nil), got)
}

func TestGetFallsBackWhenTheDefaultProfileIsGone(t *testing.T) {
	svc, db, _ := newTestService(t)
	require.NoError(t, profiles.Sync(t.Context(), db, []profiles.Profile{{Name: "work", ConfigDir: "/opt/profiles/work"}}))
	require.NoError(t, svc.SetDefaultProfile(t.Context(), "work"))

	// The profile is removed, say because its folder turned out to be another tool's.
	require.NoError(t, db.Exec("DELETE FROM claude_profiles WHERE name = 'work'").Error)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, DefaultProfile, got.DefaultProfile)
}

func TestGetReportsAProfileLookupError(t *testing.T) {
	svc, db, _ := newTestService(t)
	setPref(t, db, KeyDefaultProfile, "work")

	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:refuse", func(tx *gorm.DB) {
		_ = tx.AddError(errRefused)
	}))

	_, err := svc.Get(t.Context())
	require.ErrorIs(t, err, errRefused)
	require.ErrorContains(t, err, "read settings")
}

func TestDatabaseErrors(t *testing.T) {
	svc, db, _ := newTestService(t)
	require.NoError(t, store.Close(db))

	_, err := svc.Get(t.Context())
	require.ErrorContains(t, err, "read settings")

	require.ErrorContains(t, svc.SetTerminal(t.Context(), TerminalApp), "save setting launch.terminal")

	err = svc.SetDefaultProfile(t.Context(), "default")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrUnknownProfile)

	require.ErrorContains(t, svc.SetSnoozeWake(t.Context(), snooze.DefaultRules()), "save the snooze rules")
}

func TestSensitiveFiles(t *testing.T) {
	svc, db, _ := newTestService(t)
	ctx := t.Context()

	require.NoError(t, svc.SetSensitiveScope(ctx, sensitive.ScopeOutside))
	require.NoError(t, svc.SetSensitiveCategory(ctx, sensitive.CategoryDependencies, false))
	require.NoError(t, svc.SetSensitiveCategory(ctx, sensitive.CategoryReview, false))
	require.NoError(t, svc.SetSensitiveCategory(ctx, sensitive.CategoryReview, true))
	require.NoError(t, svc.SetSensitivePatterns(ctx, []string{" infra/ ", "*.tf", "", "INFRA/"}))

	got, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, sensitive.Config{
		Scope: sensitive.ScopeOutside, Off: []string{sensitive.CategoryDependencies}, Patterns: []string{"infra/", "*.tf"},
	}, got.SensitiveFiles)

	match, err := svc.MatchSensitivePath(ctx, "infra/prod/main.tf")
	require.NoError(t, err)
	assert.Equal(t, sensitive.CategoryCustom, match.Category)
	assert.Equal(t, "infra/", match.Pattern)

	match, err = svc.MatchSensitivePath(ctx, "go.mod")
	require.NoError(t, err)
	assert.False(t, match.Sensitive, "dependencies are off")

	// Refused changes leave the settings as they were.
	require.ErrorIs(t, svc.SetSensitiveScope(ctx, "everyone"), sensitive.ErrInvalidScope)
	require.ErrorIs(t, svc.SetSensitiveCategory(ctx, sensitive.CategoryAgent, false), sensitive.ErrLockedCategory)
	require.ErrorIs(t, svc.SetSensitiveCategory(ctx, "nope", false), sensitive.ErrUnknownCategory)
	require.ErrorIs(t, svc.SetSensitivePatterns(ctx, []string{"ok/", "*"}), sensitive.ErrInvalidPattern)

	again, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, got.SensitiveFiles, again.SensitiveFiles)

	// Clearing the patterns stores an empty list.
	require.NoError(t, svc.SetSensitivePatterns(ctx, nil))

	var value string
	require.NoError(t, db.Raw("SELECT value FROM prefs WHERE key = ?", sensitive.PrefPatterns).Scan(&value).Error)
	assert.Equal(t, "[]", value)
}

func TestSensitiveFilesDatabaseErrors(t *testing.T) {
	svc, db, _ := newTestService(t)
	require.NoError(t, store.Close(db))

	require.Error(t, svc.SetSensitiveCategory(t.Context(), sensitive.CategoryReview, false))

	_, err := svc.MatchSensitivePath(t.Context(), "go.mod")
	require.Error(t, err)
}
