package core

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/timing"
)

var errNoPrograms = errors.New("tests run no external programs")

// noPrograms returns a runner that records every program it is asked to run and runs
// none.
func noPrograms() *testutil.Runner {
	return &testutil.Runner{Stderr: "not run", Err: errNoPrograms}
}

// programNames returns the base name of every program r was asked to run.
func programNames(r *testutil.Runner) []string {
	calls := r.Calls()
	names := make([]string, 0, len(calls))

	for _, argv := range calls {
		names = append(names, filepath.Base(argv[0]))
	}

	return names
}

// bootstrap starts a Core over a fresh data directory and closes it when the test ends.
func bootstrap(t *testing.T, opts Options) *Core {
	t.Helper()

	c, err := Bootstrap(t.Context(), opts)
	require.NoError(t, err)

	t.Cleanup(func() { _ = c.Close() })

	return c
}

func profileNames(t *testing.T, c *Core) []string {
	t.Helper()

	list, err := c.Profiles.List(t.Context())
	require.NoError(t, err)

	names := make([]string, 0, len(list))
	for _, p := range list {
		names = append(names, p.Name)
	}

	return names
}

func TestBootstrapTempHome(t *testing.T) {
	t.Parallel()

	home := testutil.ClaudeHome(t, ".claude", ".claude_work")
	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))
	runner := noPrograms()

	c := bootstrap(t, Options{Paths: paths, Home: home, Version: "test", Runner: runner})

	assert.Equal(t, []string{profiles.DefaultName, "work"}, profileNames(t, c))

	for _, dir := range []string{paths.Root, paths.Launch, paths.Logs} {
		info, err := os.Stat(dir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	}

	_, err := os.Stat(paths.DB)
	require.NoError(t, err, "the database file exists")

	var mode string
	require.NoError(t, c.DB.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&mode))
	assert.Equal(t, "wal", mode)

	assert.Empty(t, programNames(runner), "startup runs no external program")
}

func TestBootstrapKeepsExistingProfiles(t *testing.T) {
	t.Parallel()

	home := testutil.ClaudeHome(t, ".claude", ".claude_work")
	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

	first, err := Bootstrap(t.Context(), Options{Paths: paths, Home: home, Runner: noPrograms()})
	require.NoError(t, err)
	_, err = first.DB.ExecContext(t.Context(), "UPDATE agent_profiles SET model = 'opus' WHERE name = 'work'")
	require.NoError(t, err)
	require.NoError(t, first.Close())

	// The work profile's directory is gone and a new one appeared.
	require.NoError(t, os.RemoveAll(filepath.Join(home, ".claude_work")))
	testutil.MakeClaudeDirs(t, home, ".claude-alt")

	c := bootstrap(t, Options{Paths: paths, Home: home, Runner: noPrograms()})

	assert.ElementsMatch(t, []string{profiles.DefaultName, "work", "alt"}, profileNames(t, c))

	work, err := c.ProfileStore.Lookup(t.Context(), agent.Claude, "work")
	require.NoError(t, err)
	assert.Equal(t, "opus", work.Model, "an edited row is never overwritten")
}

func TestBootstrapRemovesProfilesForOtherToolsFolders(t *testing.T) {
	t.Parallel()

	home := testutil.ClaudeHome(t, ".claude", ".claude_work", ".claude-dup")
	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

	// Folders other tools name ~/.claude-<name>, and an empty twin of a real profile.
	for _, dir := range []string{".claude-squad", ".claude-monitor", ".claude_dup"} {
		require.NoError(t, os.Mkdir(filepath.Join(home, dir), 0o700))
	}

	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude-squad", "state.json"), []byte("{}"), 0o600))

	// An older version stored a profile for each of them.
	first := bootstrap(t, Options{Paths: paths, Home: home, Runner: noPrograms()})
	require.NoError(t, first.ProfileStore.Sync(t.Context(), []profiles.Profile{
		{Agent: agent.Claude, Name: "squad", ConfigDir: filepath.Join(home, ".claude-squad"), PermissionMode: profiles.PermissionFull},
		{Agent: agent.Claude, Name: "monitor", ConfigDir: filepath.Join(home, ".claude-monitor"), PermissionMode: profiles.PermissionFull},
	}))
	_, err := first.DB.ExecContext(t.Context(), "UPDATE agent_profiles SET config_dir = ? WHERE name = 'dup'",
		filepath.Join(home, ".claude_dup"))
	require.NoError(t, err)
	require.NoError(t, first.Close())

	var logs bytes.Buffer

	c := bootstrap(t, Options{
		Paths:  paths,
		Home:   home,
		Runner: noPrograms(),
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	})

	assert.Equal(t, []string{profiles.DefaultName, "dup", "work"}, profileNames(t, c))
	assert.Contains(t, logs.String(), "removed profiles whose directory holds no agent config")
	assert.Contains(t, logs.String(), "profiles=\"[claude/dup claude/monitor claude/squad]\"")

	// The freed name went to the directory that holds the config.
	dup, err := c.ProfileStore.Lookup(t.Context(), agent.Claude, "dup")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".claude-dup"), dup.ConfigDir)

	// The folders themselves are left alone.
	assert.DirExists(t, filepath.Join(home, ".claude-squad"))
	assert.DirExists(t, filepath.Join(home, ".claude-monitor"))
}

func TestBootstrapUnreadableHomeSkipsDiscovery(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   filepath.Join(t.TempDir(), "missing-home"),
		Runner: noPrograms(),
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	})

	assert.Empty(t, profileNames(t, c))
	assert.Contains(t, logs.String(), "profile discovery skipped")
}

func TestServicesListsBoundServices(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t, ".claude"),
		Runner: noPrograms(),
	})

	services := c.Services()

	assert.Equal(t, []any{
		c.Settings, c.Profiles, c.Prompts, c.LaunchDefaults, c.Inbox, c.Launcher, c.Worktrees, c.Notifications,
		c.AppInfo, c.Budget, c.Sessions, c.Review,
	}, services)

	for _, s := range services {
		assert.NotNil(t, s)
	}

	assert.NotNil(t, c.Scheduler)
	assert.NotNil(t, c.Watcher)
	assert.NotNil(t, c.Timing)
}

func TestTheSweeperSweepsTheWorktreeService(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t),
		Runner: noPrograms(),
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// A canceled context still gets the first sweep, which Run makes before it waits.
	c.Sweeper.Run(ctx)
	assert.Equal(t, 1, c.Worktrees.LastSweep().Seq)
}

func TestFirstPaintEventAndTimingLog(t *testing.T) {
	t.Parallel()

	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))
	events := &testutil.Events{}

	c := bootstrap(t, Options{
		Paths:  paths,
		Home:   testutil.ClaudeHome(t),
		Runner: noPrograms(),
		Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		Events: events,
	})

	require.NoError(t, c.AppInfo.ReportFirstPaint())
	require.NoError(t, c.AppInfo.ReportFirstPaint())

	got := events.All()
	require.Len(t, got, 1)
	assert.IsType(t, event.FirstPaint{}, got[0])

	data, err := os.ReadFile(filepath.Join(paths.Logs, timing.LogFileName))
	require.NoError(t, err)
	assert.Contains(t, string(data), timing.FirstPaintKey+"=")
}

func TestThemeChangesReachTheEvents(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}

	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t),
		Runner: noPrograms(),
		Events: events,
	})

	require.NoError(t, testutil.ErrOf(c.Settings.SetTheme(t.Context(), settings.ThemeDark)))
	require.Error(t, testutil.ErrOf(c.Settings.SetTheme(t.Context(), "sepia")))

	assert.Equal(t, []event.Event{event.ThemeChanged{Theme: string(settings.ThemeDark)}}, events.All())
}

// posterStub is a notification center that shows every banner.
type posterStub struct {
	banners []notify.Banner
}

func (p *posterStub) Post(b notify.Banner) error {
	p.banners = append(p.banners, b)

	return nil
}

func (p *posterStub) Status() notify.BannerStatus { return notify.StatusOn }

func (p *posterStub) Authorize() (bool, error) { return true, nil }

func TestMenuBarAndNotificationsReachTheShell(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	poster := &posterStub{}

	c := bootstrap(t, Options{
		Paths:   appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:    testutil.ClaudeHome(t),
		Runner:  noPrograms(),
		Events:  events,
		Banners: poster,
	})

	require.NoError(t, testutil.ErrOf(c.Settings.SetMenuBar(t.Context(), false)))
	assert.Equal(t, []event.Event{event.MenuBarChanged{On: false}}, events.All())

	assert.Equal(t, notify.StatusOn, c.Notifications.BannerStatus())
	require.NoError(t, c.Notifications.SendTest())
	assert.Equal(t, []notify.Banner{notify.TestBanner()}, poster.banners)

	log, err := c.Notifications.List(t.Context())
	require.NoError(t, err)
	assert.Empty(t, log.Items)
}

func TestTheShellReadsItsSettingsOffTheBoundService(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t),
		Runner: noPrograms(),
	})

	for _, s := range c.Services() {
		assert.NotEqual(t, c.ShellSettings, s, "the shell's settings are not bound")
	}

	ctx := t.Context()
	size := settings.WindowSize{Width: 1680, Height: 1050}

	require.NoError(t, testutil.ErrOf(c.Settings.SetTheme(ctx, settings.ThemeDark)))
	require.NoError(t, testutil.ErrOf(c.Settings.SetMenuBar(ctx, false)))
	require.NoError(t, testutil.ErrOf(c.Settings.SetWindowSize(ctx, size)))
	require.NoError(t, testutil.ErrOf(c.Settings.SetTextSize(ctx, settings.TextSizeLarge)))

	theme, err := c.ShellSettings.Theme(ctx)
	require.NoError(t, err)
	assert.Equal(t, settings.ThemeDark, theme)

	on, err := c.ShellSettings.MenuBar(ctx)
	require.NoError(t, err)
	assert.False(t, on)

	got, err := c.ShellSettings.WindowSize(ctx)
	require.NoError(t, err)
	assert.Equal(t, size, got)

	textSize, err := c.ShellSettings.TextSize(ctx)
	require.NoError(t, err)
	assert.Equal(t, settings.TextSizeLarge, textSize)
}

func TestThemeWithoutAnEventSink(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t),
		Runner: noPrograms(),
	})

	require.NoError(t, testutil.ErrOf(c.Settings.SetTheme(t.Context(), settings.ThemeLight)))

	got, err := c.Settings.Theme(t.Context())
	require.NoError(t, err)
	assert.Equal(t, settings.ThemeLight, got)
}

func TestAppInfoReportsVersion(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, Options{
		Paths:   appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:    testutil.ClaudeHome(t),
		Version: "v1.2.3",
		Release: "1.2.3",
		Build:   "42",
		Runner:  noPrograms(),
	})

	info := c.AppInfo.Info()
	assert.Equal(t, "v1.2.3", info.Version)
	assert.Equal(t, "1.2.3", info.Release)
	assert.Equal(t, "42", info.Build)
	assert.Nil(t, info.DataReset, "a database that opens is not reset")
}

func TestTheWindowShowsEveryReasonTheStoreSetsADatabaseAside(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	for stored, shown := range map[store.ResetReason]appinfo.ResetReason{
		store.ResetCorrupt:   appinfo.ResetCorrupt,
		store.ResetUnmanaged: appinfo.ResetUnmanaged,
		store.ResetHistory:   appinfo.ResetHistory,
	} {
		got := dataReset(t.Context(), &store.Reset{Path: "/data/app.db.broken", Reason: stored}, "/home", logger)
		require.NotNil(t, got)
		assert.Equal(t, shown, got.Reason, string(stored))
	}
}

func TestBootstrapStartsFreshWhenTheDatabaseCannotBeRead(t *testing.T) {
	t.Parallel()

	home := testutil.ClaudeHome(t, ".claude")
	paths := appdata.NewPaths(filepath.Join(home, "data"))
	testutil.WriteFile(t, paths.DB, "this is not a database file, just text long enough to have a header")

	var logs bytes.Buffer

	c := bootstrap(t, Options{
		Paths:  paths,
		Home:   home,
		Runner: noPrograms(),
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	})

	reset := c.AppInfo.Info().DataReset
	require.NotNil(t, reset, "the window is told the data was reset")
	assert.Equal(t, appinfo.ResetCorrupt, reset.Reason)
	assert.Regexp(t, `^~/data/`+regexp.QuoteMeta(filepath.Base(paths.DB))+`\.broken-\d{8}T\d{6}Z$`, reset.Path,
		"the path is shown with ~ for the home directory")
	assert.Contains(t, logs.String(), "set aside")

	// The fresh database is set up like any new one.
	all, err := c.Prompts.List(t.Context())
	require.NoError(t, err)
	assert.Len(t, all, len(prompts.Starters()))
	assert.Equal(t, []string{profiles.DefaultName}, profileNames(t, c))
}

func TestBootstrapRefusesADatabaseFromANewerBuild(t *testing.T) {
	t.Parallel()

	opts := Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t),
		Runner: noPrograms(),
	}

	c := bootstrap(t, opts)
	_, err := c.DB.ExecContext(t.Context(), `INSERT INTO schema_migrations (version, name, checksum, applied_at)
VALUES (9999, 'from_the_future', 'abc', '2026-01-01T00:00:00Z')`)
	require.NoError(t, err)
	require.NoError(t, c.Close())

	_, err = Bootstrap(t.Context(), opts)
	require.ErrorIs(t, err, store.ErrNewerSchema)
}

func TestInboxRefreshUsesInjectedRunner(t *testing.T) {
	t.Parallel()

	runner := noPrograms()

	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t),
		Runner: runner,
	})

	// The token is borrowed through the injected runner, which runs nothing, so the
	// refresh fails before any request is made.
	_, err := c.Inbox.Refresh(t.Context())
	require.Error(t, err)

	switch {
	case errors.Is(err, github.ErrNotLoggedIn):
		assert.Equal(t, []string{"gh"}, programNames(runner))
	case errors.Is(err, github.ErrGHNotFound):
		assert.Empty(t, programNames(runner))
	default:
		t.Fatalf("unexpected refresh error: %v", err)
	}

	snap, err := c.Inbox.Snapshot(t.Context())
	require.NoError(t, err)
	assert.NotEmpty(t, snap.LastError)
}

func TestWorktreeItemStatesBorrowTheToken(t *testing.T) {
	t.Parallel()

	runner := noPrograms()

	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t),
		Runner: runner,
	})

	states := itemStates{tokens: github.NewTokenSource(runner, execpath.Search{Home: testutil.ClaudeHome(t)}), client: github.NewClient("test")}

	// The token is borrowed through the injected runner, which runs nothing, so no
	// request is made.
	_, err := states.ItemStates(t.Context(), []item.Ref{{Repo: repo.Name{Owner: "acme-org", Repo: "example-repo"}, Number: 1}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, github.ErrNotLoggedIn) || errors.Is(err, github.ErrGHNotFound), err)

	assert.Zero(t, c.Worktrees.LastSweep().Seq, "nothing sweeps until the shell starts the sweeps")
}

func TestBootstrapAddsTheStarterPrompts(t *testing.T) {
	t.Parallel()

	opts := Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t, ".claude"),
		Runner: noPrograms(),
	}

	c := bootstrap(t, opts)

	all, err := c.Prompts.List(t.Context())
	require.NoError(t, err)
	assert.Len(t, all, len(prompts.Starters()))

	defaults, err := c.Prompts.Defaults(t.Context())
	require.NoError(t, err)
	assert.NotZero(t, defaults.PR)
	assert.NotZero(t, defaults.Issue)

	// A starter the user deleted is not added back on the next start.
	var quick int64

	for _, p := range all {
		if p.StarterKey == prompts.StarterQuickLook {
			quick = p.ID
		}
	}

	// While a launch default names it, deleting it is refused, saying where it is used.
	_, err = c.LaunchDefaults.Save(t.Context(), launchdefaults.Input{Owner: "acme-org", PRPromptID: &quick})
	require.NoError(t, err)
	require.ErrorContains(t, c.Prompts.Delete(t.Context(), quick), "while it's a default for acme-org;")
	require.NoError(t, c.LaunchDefaults.Remove(t.Context(), "acme-org", ""))

	require.NoError(t, c.Prompts.Delete(t.Context(), quick))
	require.NoError(t, c.Close())

	c = bootstrap(t, opts)

	all, err = c.Prompts.List(t.Context())
	require.NoError(t, err)
	assert.Len(t, all, len(prompts.Starters())-1)
}
