package settings

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/branchrules"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/trust"
)

// newTestService returns a Service over a fresh database and a temp home directory.
func newTestService(t *testing.T) (*Service, *sql.DB, string) {
	t.Helper()

	home := t.TempDir()
	db := testutil.DB(t)

	return serviceOver(t, db, home), db, home
}

// serviceOver returns a Service over db, which also holds the profiles it looks up.
func serviceOver(t *testing.T, db *sql.DB, home string, opts ...Option) *Service {
	t.Helper()

	return NewService(db, profiles.NewStore(db, nil), home, opts...)
}

// closedDB returns a migrated database that is already closed.
func closedDB(t *testing.T) *sql.DB {
	t.Helper()

	db := testutil.DB(t)
	require.NoError(t, db.Close())

	return db
}

// setPref writes a raw preference value, bypassing validation.
func setPref(t *testing.T, db *sql.DB, key, value string) {
	t.Helper()

	require.NoError(t, prefs.New(db, nil).Set(t.Context(), key, value))
}

// storeProfiles stores profiles as discovery would.
func storeProfiles(t *testing.T, db *sql.DB, found ...profiles.Profile) {
	t.Helper()

	require.NoError(t, profiles.NewStore(db, nil).Sync(t.Context(), found))
}

func TestPrefDefaults(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, Settings{
		DefaultAgent:        agent.Claude,
		DefaultProfiles:     map[agent.ID]string{agent.Claude: "default", agent.Codex: "default"},
		WorktreeDefault:     true,
		WorktreeAutoCleanup: true,
		Terminal:            "iterm2",
		FollowSessions:      true,
		CloneRoot:           "~/projects",
		TrustedBots:         []string{"dependabot", "github-actions", "renovate"},
		DefaultTrustedBots:  []string{"dependabot", "github-actions", "renovate"},
		SnoozeDays:          3,
		SnoozeWake:          snooze.Rules{Commits: 1, Reviews: "decisions", Comments: 0},
		WindowSize:          WindowSize{Width: 1440, Height: 900},
		DefaultWindowSize:   WindowSize{Width: 1440, Height: 900},
		SensitiveFiles:      sensitive.Config{Scope: "collaborators", Off: []string{}, Patterns: []string{}},
		SensitiveCategories: sensitive.Categories(),
		Branches:            branchrules.Defaults(),
		MenuBar:             DefaultMenuBar,
		BannersOff:          []notifyprefs.Kind{notifyprefs.KindSessionNeedsYou, notifyprefs.KindSessionFinished},
		Theme:               ThemeSystem,
		TextSize:            TextSizeDefault,
		CollapsedSections:   []ListKey{},
		InboxOrder:          InboxOrder{StatusSorted: []ListKey{}, StatusOrder: []string{}},

		DefaultProtectedBranches: []string{"main", "master"},
	}, got)
}

func TestBranchRules(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	ctx := t.Context()

	rules := branchrules.Rules{Own: true, PullRequest: false, Merged: false, Protected: []string{"main", " develop ", "release/*"}}

	got, err := svc.SetBranchRules(ctx, rules)
	require.NoError(t, err)
	assert.Equal(t, branchrules.Rules{Own: true, PullRequest: false, Merged: false, Protected: []string{"main", "develop", "release/*"}},
		got.Branches, "the protected list normalized")

	again, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, got.Branches, again.Branches)

	_, err = svc.SetBranchRules(ctx, branchrules.Rules{Protected: []string{"[broken"}})
	require.ErrorIs(t, err, branchrules.ErrInvalidPattern)

	again, err = svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, got.Branches, again.Branches, "a refused save stores nothing")

	setPref(t, db, branchrules.PrefProtected, "{broken")

	again, err = svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, branchrules.DefaultProtected(), again.Branches.Protected, "an unreadable list falls back to the defaults")
}

func TestTrustedBots(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	ctx := t.Context()

	require.NoError(t, testutil.ErrOf(svc.SetTrustedBots(ctx, []string{"Renovate[bot]", "app/my-release-bot", " ", "renovate"})))

	got, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"my-release-bot", "renovate"}, got.TrustedBots, "normalized, deduplicated, and sorted")

	require.NoError(t, testutil.ErrOf(svc.SetTrustedBots(ctx, nil)))

	got, err = svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{}, got.TrustedBots, "an emptied list trusts no bot")

	require.ErrorIs(t, testutil.ErrOf(svc.SetTrustedBots(ctx, []string{"not a login"})), trust.ErrInvalidBot)

	require.NoError(t, testutil.ErrOf(svc.ResetTrustedBots(ctx)))

	got, err = svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, trust.DefaultBots(), got.TrustedBots)

	setPref(t, db, KeyTrustedBots, "{broken")

	got, err = svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, trust.DefaultBots(), got.TrustedBots, "an unreadable list falls back to the defaults")
}

func TestTerminalPrefPersists(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	home := t.TempDir()

	db := testutil.DBAt(t, path)
	require.NoError(t, testutil.ErrOf(serviceOver(t, db, home).SetTerminal(t.Context(), TerminalApp)))
	require.NoError(t, db.Close())

	got, err := serviceOver(t, testutil.DBAt(t, path), home).Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, TerminalApp, got.Terminal)
}

func TestWithClockStampsTheStoredSettings(t *testing.T) {
	t.Parallel()

	pool := testutil.DB(t)
	at := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	svc := serviceOver(t, pool, t.TempDir(), WithClock(testutil.NewClock(at)))

	require.NoError(t, testutil.ErrOf(svc.SetTerminal(t.Context(), TerminalApp)))

	var stamp string
	require.NoError(t, pool.QueryRowContext(t.Context(),
		"SELECT CAST(updated_at AS TEXT) FROM prefs WHERE key = ?", KeyTerminal).Scan(&stamp))
	assert.Equal(t, "2026-01-10 12:00:00+00:00", stamp)
}

func TestSettersPersist(t *testing.T) {
	t.Parallel()

	svc, db, home := newTestService(t)
	storeProfiles(t, db, profiles.Profile{Agent: agent.Codex, Name: "work", ConfigDir: "/opt/profiles/work"})
	require.NoError(t, os.MkdirAll(filepath.Join(home, "code", "clones"), 0o700))

	require.NoError(t, testutil.ErrOf(svc.SetDefaultAgent(t.Context(), agent.Codex)))
	require.NoError(t, testutil.ErrOf(svc.SetDefaultProfile(t.Context(), agent.Codex, "work")))
	require.NoError(t, testutil.ErrOf(svc.SetWorktreeDefault(t.Context(), false)))
	require.NoError(t, testutil.ErrOf(svc.SetWorktreeAutoCleanup(t.Context(), false)))
	require.NoError(t, testutil.ErrOf(svc.SetTerminal(t.Context(), TerminalApp)))
	require.NoError(t, testutil.ErrOf(svc.SetTerminal(t.Context(), TerminalITerm2)))
	require.NoError(t, testutil.ErrOf(svc.SetCloneRoot(t.Context(), " ~/code/clones/ ")))
	require.NoError(t, testutil.ErrOf(svc.SetSnoozeDays(t.Context(), 7)))
	require.NoError(t, testutil.ErrOf(svc.SetSnoozeWake(t.Context(), snooze.Rules{Commits: 3, Reviews: snooze.ReviewsAny, Comments: 5})))
	require.NoError(t, testutil.ErrOf(svc.SetMenuBar(t.Context(), false)))
	require.NoError(t, testutil.ErrOf(svc.SetNotificationBanner(t.Context(), notifyprefs.KindWoke, false)))
	require.NoError(t, testutil.ErrOf(svc.SetNotificationBanner(t.Context(), notifyprefs.KindSessionNeedsYou, true)))
	require.NoError(t, testutil.ErrOf(svc.SetFollowSessions(t.Context(), false)))
	require.NoError(t, testutil.ErrOf(svc.SetBranchRules(t.Context(), branchrules.Rules{Own: true, Protected: []string{"develop"}})))
	require.NoError(t, testutil.ErrOf(svc.SetTextSize(t.Context(), TextSizeLarger)))

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, Settings{
		DefaultAgent:        agent.Codex,
		DefaultProfiles:     map[agent.ID]string{agent.Claude: "default", agent.Codex: "work"},
		WorktreeDefault:     false,
		WorktreeAutoCleanup: false,
		Terminal:            TerminalITerm2,
		FollowSessions:      false,
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
		BannersOff:          []notifyprefs.Kind{notifyprefs.KindWoke, notifyprefs.KindSessionFinished},
		Theme:               DefaultTheme,
		TextSize:            TextSizeLarger,
		CollapsedSections:   []ListKey{},
		InboxOrder:          InboxOrder{StatusSorted: []ListKey{}, StatusOrder: []string{}},
		Branches:            branchrules.Rules{Own: true, Protected: []string{"develop"}},

		DefaultProtectedBranches: branchrules.DefaultProtected(),
	}, got)

	abs := t.TempDir()
	require.NoError(t, testutil.ErrOf(svc.SetCloneRoot(t.Context(), abs)))
	require.NoError(t, testutil.ErrOf(svc.SetWorktreeDefault(t.Context(), true)))
	require.NoError(t, testutil.ErrOf(svc.SetWorktreeAutoCleanup(t.Context(), true)))

	got, err = svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, abs, got.CloneRoot)
	assert.True(t, got.WorktreeDefault)
	assert.True(t, got.WorktreeAutoCleanup)
}

func TestSetRejectsInvalid(t *testing.T) {
	t.Parallel()

	svc, db, home := newTestService(t)
	storeProfiles(t, db, profiles.Profile{Agent: agent.Claude, Name: "default"}, profiles.Profile{Agent: agent.Claude, Name: "work"})

	file := filepath.Join(home, "notes.txt")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))

	tests := []struct {
		name    string
		set     func() error
		wantErr error
		wantMsg string
	}{
		{name: "unknown terminal", set: func() error { return testutil.ErrOf(svc.SetTerminal(t.Context(), "warp")) }, wantErr: ErrInvalidTerminal, wantMsg: `"warp"`},
		{name: "empty terminal", set: func() error { return testutil.ErrOf(svc.SetTerminal(t.Context(), "")) }, wantErr: ErrInvalidTerminal},
		{name: "terminal in the wrong case", set: func() error { return testutil.ErrOf(svc.SetTerminal(t.Context(), "ITERM2")) }, wantErr: ErrInvalidTerminal},
		{name: "unknown profile", set: func() error { return testutil.ErrOf(svc.SetDefaultProfile(t.Context(), agent.Claude, "missing")) }, wantErr: ErrUnknownProfile, wantMsg: `"missing"`},
		{name: "empty profile", set: func() error { return testutil.ErrOf(svc.SetDefaultProfile(t.Context(), agent.Claude, "")) }, wantErr: ErrUnknownProfile},
		{name: "another agent's profile", set: func() error { return testutil.ErrOf(svc.SetDefaultProfile(t.Context(), agent.Codex, "work")) }, wantErr: ErrUnknownProfile},
		{name: "profile of an unknown agent", set: func() error { return testutil.ErrOf(svc.SetDefaultProfile(t.Context(), "example-agent", "default")) }, wantErr: ErrUnknownAgent},
		{name: "unknown agent", set: func() error { return testutil.ErrOf(svc.SetDefaultAgent(t.Context(), "example-agent")) }, wantErr: ErrUnknownAgent, wantMsg: `"example-agent"`},
		{name: "no agent", set: func() error { return testutil.ErrOf(svc.SetDefaultAgent(t.Context(), agent.Inherit)) }, wantErr: ErrUnknownAgent},
		{name: "empty clone root", set: func() error { return testutil.ErrOf(svc.SetCloneRoot(t.Context(), "  ")) }, wantErr: paths.ErrEmpty},
		{name: "relative clone root", set: func() error { return testutil.ErrOf(svc.SetCloneRoot(t.Context(), "projects")) }, wantErr: paths.ErrRelative},
		{name: "missing clone root", set: func() error { return testutil.ErrOf(svc.SetCloneRoot(t.Context(), "~/missing")) }, wantErr: fs.ErrNotExist},
		{name: "clone root is a file", set: func() error { return testutil.ErrOf(svc.SetCloneRoot(t.Context(), file)) }, wantErr: ErrInvalidCloneRoot, wantMsg: "~/notes.txt is not a directory"},
		{name: "no snooze", set: func() error { return testutil.ErrOf(svc.SetSnoozeDays(t.Context(), 0)) }, wantErr: snooze.ErrInvalidDays, wantMsg: "0 days; choose 1 to 365"},
		{name: "snooze past a year", set: func() error { return testutil.ErrOf(svc.SetSnoozeDays(t.Context(), 366)) }, wantErr: snooze.ErrInvalidDays},
		{name: "unknown review rule", set: func() error {
			return testutil.ErrOf(svc.SetSnoozeWake(t.Context(), snooze.Rules{Reviews: "sometimes"}))
		}, wantErr: snooze.ErrInvalidRules, wantMsg: `"sometimes"`},
		{name: "negative comment rule", set: func() error {
			return testutil.ErrOf(svc.SetSnoozeWake(t.Context(), snooze.Rules{Reviews: snooze.ReviewsAny, Comments: -1}))
		}, wantErr: snooze.ErrInvalidRules},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.set()
			require.ErrorIs(t, err, tt.wantErr)

			if tt.wantMsg != "" {
				assert.Contains(t, err.Error(), tt.wantMsg)
			}

			// Every case is rejected, so nothing has been stored by any of them.
			got, err := svc.Get(t.Context())
			require.NoError(t, err)
			assert.Equal(t, fromStored(nil), got, "rejected values must not be stored")
		})
	}
}

func TestCloneRootErrorsAreInvalidCloneRoot(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	for _, root := range []string{"", "projects", "~/missing"} {
		require.ErrorIs(t, testutil.ErrOf(svc.SetCloneRoot(t.Context(), root)), ErrInvalidCloneRoot, root)
	}
}

func TestGetFallsBackOnBadStoredValues(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	setPref(t, db, KeyDefaultProfile(agent.Claude), "")
	setPref(t, db, KeyDefaultAgent, "example-agent")
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
	t.Parallel()

	svc, db, _ := newTestService(t)
	storeProfiles(t, db, profiles.Profile{Agent: agent.Claude, Name: "work", ConfigDir: "/opt/profiles/work"},
		profiles.Profile{Agent: agent.Codex, Name: "work", ConfigDir: "/opt/profiles/codex-work"})
	require.NoError(t, testutil.ErrOf(svc.SetDefaultProfile(t.Context(), agent.Claude, "work")))
	require.NoError(t, testutil.ErrOf(svc.SetDefaultProfile(t.Context(), agent.Codex, "work")))

	// Claude's profile is removed, say because its folder turned out to be another tool's.
	_, err := db.ExecContext(t.Context(), "DELETE FROM agent_profiles WHERE agent = 'claude' AND name = 'work'")
	require.NoError(t, err)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, map[agent.ID]string{agent.Claude: DefaultProfile, agent.Codex: "work"}, got.DefaultProfiles)
}

func TestGetReportsAProfileLookupError(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	setPref(t, db, KeyDefaultProfile(agent.Codex), "work")

	// The preferences still read; only the profile lookup fails.
	_, err := db.ExecContext(t.Context(), "DROP TABLE agent_profiles")
	require.NoError(t, err)

	_, err = svc.Get(t.Context())
	require.ErrorContains(t, err, "read settings: look up profile")
	require.NotErrorIs(t, err, profiles.ErrProfileNotFound)
}

func TestDatabaseErrors(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	require.NoError(t, db.Close())

	_, err := svc.Get(t.Context())
	require.ErrorContains(t, err, "read settings")

	require.ErrorContains(t, testutil.ErrOf(svc.SetTerminal(t.Context(), TerminalApp)), "begin transaction")

	_, err = svc.SetDefaultProfile(t.Context(), agent.Claude, "default")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrUnknownProfile)

	require.Error(t, testutil.ErrOf(svc.SetSnoozeWake(t.Context(), snooze.DefaultRules())))
}

// refusePrefs makes every write to the prefs table fail, the way a full disk would.
const refusePrefs = `CREATE TRIGGER refuse_prefs BEFORE INSERT ON prefs
BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`

func TestASetterThatCannotWriteStoresNothing(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	ctx := t.Context()

	_, err := db.ExecContext(ctx, refusePrefs)
	require.NoError(t, err)

	got, err := svc.SetTerminal(ctx, TerminalApp)
	require.ErrorContains(t, err, "save setting launch.terminal")
	assert.Equal(t, Settings{}, got)

	_, err = svc.SetSnoozeWake(ctx, snooze.Rules{Commits: 5, Reviews: snooze.ReviewsAny, Comments: 5})
	require.ErrorContains(t, err, "save the snooze rules")

	stored, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, DefaultTerminal, stored.Terminal)
	assert.Equal(t, snooze.DefaultRules(), stored.SnoozeWake, "no rule of the refused save is stored")
}

func TestASaveThatFailsPartWayStoresNothing(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)
	ctx := t.Context()

	_, err := svc.save(ctx, func(ctx context.Context, p *prefs.Store) error {
		require.NoError(t, p.Set(ctx, KeyTheme, string(ThemeDark)))

		return errLookup
	})
	require.ErrorIs(t, err, errLookup)

	got, err := svc.Theme(ctx)
	require.NoError(t, err)
	assert.Equal(t, DefaultTheme, got, "the write before the failure is rolled back")
}

func TestSensitiveFiles(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	ctx := t.Context()

	require.NoError(t, testutil.ErrOf(svc.SetSensitiveScope(ctx, sensitive.ScopeOutside)))
	require.NoError(t, testutil.ErrOf(svc.SetSensitiveCategory(ctx, sensitive.CategoryDependencies, false)))
	require.NoError(t, testutil.ErrOf(svc.SetSensitiveCategory(ctx, sensitive.CategoryReview, false)))
	require.NoError(t, testutil.ErrOf(svc.SetSensitiveCategory(ctx, sensitive.CategoryReview, true)))
	require.NoError(t, testutil.ErrOf(svc.SetSensitivePatterns(ctx, []string{" infra/ ", "*.tf", "", "INFRA/"})))

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
	require.ErrorIs(t, testutil.ErrOf(svc.SetSensitiveScope(ctx, "everyone")), sensitive.ErrInvalidScope)
	require.ErrorIs(t, testutil.ErrOf(svc.SetSensitiveCategory(ctx, sensitive.CategoryAgent, false)), sensitive.ErrLockedCategory)
	require.ErrorIs(t, testutil.ErrOf(svc.SetSensitiveCategory(ctx, "nope", false)), sensitive.ErrUnknownCategory)
	require.ErrorIs(t, testutil.ErrOf(svc.SetSensitivePatterns(ctx, []string{"ok/", "*"})), sensitive.ErrInvalidPattern)

	again, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, got.SensitiveFiles, again.SensitiveFiles)

	// Clearing the patterns stores an empty list.
	require.NoError(t, testutil.ErrOf(svc.SetSensitivePatterns(ctx, nil)))

	var value string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT value FROM prefs WHERE key = ?", sensitive.PrefPatterns).Scan(&value))
	assert.Equal(t, "[]", value)
}

func TestSensitiveFilesDatabaseErrors(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	require.NoError(t, db.Close())

	require.Error(t, testutil.ErrOf(svc.SetSensitiveCategory(t.Context(), sensitive.CategoryReview, false)))

	_, err := svc.MatchSensitivePath(t.Context(), "go.mod")
	require.Error(t, err)
}

func TestSettersReturnTheStoredSettings(t *testing.T) {
	t.Parallel()

	svc, db, home := newTestService(t)
	storeProfiles(t, db, profiles.Profile{Agent: agent.Claude, Name: "work", ConfigDir: "/opt/profiles/work"})
	require.NoError(t, os.MkdirAll(filepath.Join(home, "code"), 0o700))

	ctx := t.Context()
	wake := snooze.Rules{Commits: 2, Reviews: snooze.ReviewsAny, Comments: 4}
	size := WindowSize{Width: 1024, Height: 700}

	for _, tc := range []struct {
		name  string
		set   func() (Settings, error)
		field func(Settings) any
		want  any
	}{
		{
			"terminal", func() (Settings, error) { return svc.SetTerminal(ctx, TerminalApp) },
			func(s Settings) any { return s.Terminal }, TerminalApp,
		},
		{
			"default profile", func() (Settings, error) { return svc.SetDefaultProfile(ctx, agent.Claude, "work") },
			func(s Settings) any { return s.DefaultProfiles[agent.Claude] }, "work",
		},
		{
			"default agent", func() (Settings, error) { return svc.SetDefaultAgent(ctx, agent.Codex) },
			func(s Settings) any { return s.DefaultAgent }, agent.Codex,
		},
		{
			"worktree default", func() (Settings, error) { return svc.SetWorktreeDefault(ctx, false) },
			func(s Settings) any { return s.WorktreeDefault }, false,
		},
		{
			"worktree cleanup", func() (Settings, error) { return svc.SetWorktreeAutoCleanup(ctx, false) },
			func(s Settings) any { return s.WorktreeAutoCleanup }, false,
		},
		{
			"clone root", func() (Settings, error) { return svc.SetCloneRoot(ctx, "~/code") },
			func(s Settings) any { return s.CloneRoot }, "~/code",
		},
		{
			"trusted bots", func() (Settings, error) { return svc.SetTrustedBots(ctx, []string{"renovate"}) },
			func(s Settings) any { return s.TrustedBots },
			[]string{"renovate"},
		},
		{
			"reset trusted bots", func() (Settings, error) { return svc.ResetTrustedBots(ctx) },
			func(s Settings) any { return s.TrustedBots }, trust.DefaultBots(),
		},
		{
			"snooze days", func() (Settings, error) { return svc.SetSnoozeDays(ctx, 9) },
			func(s Settings) any { return s.SnoozeDays }, 9,
		},
		{
			"snooze wake", func() (Settings, error) { return svc.SetSnoozeWake(ctx, wake) },
			func(s Settings) any { return s.SnoozeWake }, wake,
		},
		{
			"sensitive scope", func() (Settings, error) { return svc.SetSensitiveScope(ctx, sensitive.ScopeOutside) },
			func(s Settings) any { return s.SensitiveFiles.Scope }, sensitive.ScopeOutside,
		},
		{
			"sensitive category",
			func() (Settings, error) { return svc.SetSensitiveCategory(ctx, sensitive.CategoryReview, false) },
			func(s Settings) any { return s.SensitiveFiles.Off },
			[]string{sensitive.CategoryReview},
		},
		{
			"sensitive patterns", func() (Settings, error) { return svc.SetSensitivePatterns(ctx, []string{"*.pem"}) },
			func(s Settings) any { return s.SensitiveFiles.Patterns },
			[]string{"*.pem"},
		},
		{
			"menu bar", func() (Settings, error) { return svc.SetMenuBar(ctx, false) },
			func(s Settings) any { return s.MenuBar }, false,
		},
		{
			"notification banner",
			func() (Settings, error) { return svc.SetNotificationBanner(ctx, notifyprefs.KindAssigned, false) },
			func(s Settings) any { return s.BannersOff },
			[]notifyprefs.Kind{notifyprefs.KindAssigned, notifyprefs.KindSessionNeedsYou, notifyprefs.KindSessionFinished},
		},
		{
			"follow sessions", func() (Settings, error) { return svc.SetFollowSessions(ctx, false) },
			func(s Settings) any { return s.FollowSessions }, false,
		},
		{
			"codex effort", func() (Settings, error) { return svc.SetCodexEffort(ctx, true) },
			func(s Settings) any { return s.CodexEffort }, true,
		},
		{
			"codex fast", func() (Settings, error) { return svc.SetCodexFast(ctx, true) },
			func(s Settings) any { return s.CodexFast }, true,
		},
		{
			"collapsed section", func() (Settings, error) { return svc.SetSectionCollapsed(ctx, ListAuthored, true) },
			func(s Settings) any { return s.CollapsedSections },
			[]ListKey{ListAuthored},
		},
		{
			"theme", func() (Settings, error) { return svc.SetTheme(ctx, ThemeDark) },
			func(s Settings) any { return s.Theme }, ThemeDark,
		},
		{
			"projects tip", func() (Settings, error) { return svc.SetProjectsTipDismissed(ctx, true) },
			func(s Settings) any { return s.ProjectsTipDismissed }, true,
		},
		{
			"window size", func() (Settings, error) { return svc.SetWindowSize(ctx, size) },
			func(s Settings) any { return s.WindowSize }, size,
		},
		{
			"text size", func() (Settings, error) { return svc.SetTextSize(ctx, TextSizeLarge) },
			func(s Settings) any { return s.TextSize }, TextSizeLarge,
		},
	} {
		got, err := tc.set()
		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.want, tc.field(got), tc.name)

		stored, err := svc.Get(ctx)
		require.NoError(t, err, tc.name)
		assert.Equal(t, stored, got, "%s returns the settings as stored", tc.name)
	}
}

// failingLookup is a ProfileLookup whose every lookup fails.
type failingLookup struct{}

func (failingLookup) Lookup(context.Context, agent.ID, string) (profiles.Profile, error) {
	return profiles.Profile{}, errLookup
}

// errLookup is failingLookup's error.
var errLookup = errors.New("lookup failed")

func TestASaveStandsWhenTheDefaultProfileCannotBeChecked(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	setPref(t, db, KeyDefaultProfile(agent.Claude), "work")

	// The change is stored, but the stored default profile cannot be looked up once it is.
	var logs bytes.Buffer

	events := &testutil.Events{}
	svc := NewService(db, failingLookup{}, t.TempDir(), WithEvents(events),
		WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))

	got, err := svc.SetMenuBar(t.Context(), false)
	require.NoError(t, err, "the change happened, so the save is not reported as failed")
	assert.False(t, got.MenuBar)
	assert.Equal(t, "work", got.DefaultProfiles[agent.Claude], "the stored profile is named as it is")
	assert.Equal(t, []event.Event{event.MenuBarChanged{On: false}}, events.All())
	assert.Contains(t, logs.String(), "lookup failed")

	got, err = svc.SetTheme(t.Context(), ThemeDark)
	require.NoError(t, err)
	assert.Equal(t, ThemeDark, got.Theme)

	stored, err := serviceOver(t, db, t.TempDir()).Get(t.Context())
	require.NoError(t, err)
	assert.False(t, stored.MenuBar)
	assert.Equal(t, ThemeDark, stored.Theme)
}
