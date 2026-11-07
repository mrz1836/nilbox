package core

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/settings"
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
	require.NoError(t, c.DB.Raw("PRAGMA journal_mode").Scan(&mode).Error)
	assert.Equal(t, "wal", mode)

	assert.Empty(t, programNames(runner), "startup runs no external program")
}

func TestBootstrapKeepsExistingProfiles(t *testing.T) {
	home := testutil.ClaudeHome(t, ".claude", ".claude_work")
	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

	first, err := Bootstrap(t.Context(), Options{Paths: paths, Home: home, Runner: noPrograms()})
	require.NoError(t, err)
	require.NoError(t, first.DB.Exec("UPDATE claude_profiles SET model = 'opus' WHERE name = 'work'").Error)
	require.NoError(t, first.Close())

	// The work profile's directory is gone and a new one appeared.
	require.NoError(t, os.RemoveAll(filepath.Join(home, ".claude_work")))
	testutil.MakeClaudeDirs(t, home, ".claude-alt")

	c := bootstrap(t, Options{Paths: paths, Home: home, Runner: noPrograms()})

	assert.ElementsMatch(t, []string{profiles.DefaultName, "work", "alt"}, profileNames(t, c))

	work, err := profiles.Lookup(t.Context(), c.DB, "work")
	require.NoError(t, err)
	assert.Equal(t, "opus", work.Model, "an edited row is never overwritten")
}

func TestBootstrapRemovesProfilesForOtherToolsFolders(t *testing.T) {
	home := testutil.ClaudeHome(t, ".claude", ".claude_work", ".claude-dup")
	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

	// Folders other tools name ~/.claude-<name>, and an empty twin of a real profile.
	for _, dir := range []string{".claude-squad", ".claude-monitor", ".claude_dup"} {
		require.NoError(t, os.Mkdir(filepath.Join(home, dir), 0o700))
	}

	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude-squad", "state.json"), []byte("{}"), 0o600))

	// An older version stored a profile for each of them.
	first := bootstrap(t, Options{Paths: paths, Home: home, Runner: noPrograms()})
	require.NoError(t, profiles.Sync(t.Context(), first.DB, []profiles.Profile{
		{Name: "squad", ConfigDir: filepath.Join(home, ".claude-squad"), PermissionMode: profiles.PermissionFull},
		{Name: "monitor", ConfigDir: filepath.Join(home, ".claude-monitor"), PermissionMode: profiles.PermissionFull},
	}))
	require.NoError(t, first.DB.Exec("UPDATE claude_profiles SET config_dir = ? WHERE name = 'dup'",
		filepath.Join(home, ".claude_dup")).Error)
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
	assert.Contains(t, logs.String(), "profiles=\"[dup monitor squad]\"")

	// The freed name went to the directory that holds the config.
	dup, err := profiles.Lookup(t.Context(), c.DB, "dup")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".claude-dup"), dup.ConfigDir)

	// The folders themselves are left alone.
	assert.DirExists(t, filepath.Join(home, ".claude-squad"))
	assert.DirExists(t, filepath.Join(home, ".claude-monitor"))
}

func TestBootstrapStopsWhenProfilesCannotBePruned(t *testing.T) {
	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))
	home := testutil.ClaudeHome(t, ".claude")

	first := bootstrap(t, Options{Paths: paths, Home: home, Runner: noPrograms()})
	require.NoError(t, first.DB.Exec("DROP TABLE claude_profiles").Error)
	require.NoError(t, first.Close())

	_, err := Bootstrap(t.Context(), Options{Paths: paths, Home: home, Runner: noPrograms()})
	require.ErrorContains(t, err, "prune profiles")
}

func TestBootstrapUnreadableHomeSkipsDiscovery(t *testing.T) {
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

func TestBootstrapRequiresDataDirAndHome(t *testing.T) {
	_, err := Bootstrap(t.Context(), Options{Home: t.TempDir()})
	require.ErrorIs(t, err, ErrNoDataDir)

	_, err = Bootstrap(t.Context(), Options{Paths: appdata.NewPaths(t.TempDir())})
	require.ErrorIs(t, err, ErrNoHomeDir)
}

func TestBootstrapDataDirError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	_, err := Bootstrap(t.Context(), Options{Paths: appdata.NewPaths(filepath.Join(file, "data")), Home: t.TempDir()})
	require.ErrorContains(t, err, "create data directory")
}

func TestBootstrapDatabaseError(t *testing.T) {
	paths := appdata.NewPaths(t.TempDir())
	paths.DB = filepath.Join(paths.Root, "bad?name.db")

	_, err := Bootstrap(t.Context(), Options{Paths: paths, Home: t.TempDir()})
	require.Error(t, err)
}

func TestServicesListsBoundServices(t *testing.T) {
	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t, ".claude"),
		Runner: noPrograms(),
	})

	services := c.Services()

	assert.Equal(t, []any{
		c.Settings, c.Profiles, c.Prompts, c.LaunchDefaults, c.Inbox, c.Launcher, c.Worktrees, c.Notifications,
		c.AppInfo,
	}, services)

	for _, s := range services {
		assert.NotNil(t, s)
	}

	assert.NotNil(t, c.Scheduler)
	assert.NotNil(t, c.Timing)
}

func TestFirstPaintHookAndTimingLog(t *testing.T) {
	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

	var got []timing.Measurement

	c := bootstrap(t, Options{
		Paths:        paths,
		Home:         testutil.ClaudeHome(t),
		Runner:       noPrograms(),
		Logger:       slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		OnFirstPaint: func(m timing.Measurement) { got = append(got, m) },
	})

	require.NoError(t, c.AppInfo.ReportFirstPaint())
	require.NoError(t, c.AppInfo.ReportFirstPaint())

	require.Len(t, got, 1)

	data, err := os.ReadFile(filepath.Join(paths.Logs, timing.LogFileName))
	require.NoError(t, err)
	assert.Contains(t, string(data), timing.FirstPaintKey+"=")
}

// themeRecorder records the themes the settings service announces.
type themeRecorder struct {
	themes []string
}

func (r *themeRecorder) ThemeChanged(theme string) {
	r.themes = append(r.themes, theme)
}

func TestThemeChangesReachTheThemeNotifier(t *testing.T) {
	recorder := &themeRecorder{}

	c := bootstrap(t, Options{
		Paths:         appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:          testutil.ClaudeHome(t),
		Runner:        noPrograms(),
		ThemeNotifier: recorder,
	})

	require.NoError(t, c.Settings.SetTheme(t.Context(), settings.ThemeDark))
	require.Error(t, c.Settings.SetTheme(t.Context(), "sepia"))

	assert.Equal(t, []string{settings.ThemeDark}, recorder.themes)
}

// menuBarRecorder records the menu bar settings the settings service announces.
type menuBarRecorder struct {
	changes []bool
}

func (r *menuBarRecorder) MenuBarChanged(on bool) {
	r.changes = append(r.changes, on)
}

// posterStub is a notification center that shows every banner.
type posterStub struct {
	banners []notify.Banner
}

func (p *posterStub) Post(b notify.Banner) error {
	p.banners = append(p.banners, b)

	return nil
}

func (p *posterStub) Status() string { return notify.StatusOn }

func (p *posterStub) Authorize() (bool, error) { return true, nil }

func TestMenuBarAndNotificationsReachTheShell(t *testing.T) {
	menuBar := &menuBarRecorder{}
	poster := &posterStub{}

	c := bootstrap(t, Options{
		Paths:           appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:            testutil.ClaudeHome(t),
		Runner:          noPrograms(),
		MenuBarNotifier: menuBar,
		Banners:         poster,
	})

	require.NoError(t, c.Settings.SetMenuBar(t.Context(), false))
	assert.Equal(t, []bool{false}, menuBar.changes)

	assert.Equal(t, notify.StatusOn, c.Notifications.BannerStatus())
	require.NoError(t, c.Notifications.SendTest())
	assert.Equal(t, []notify.Banner{notify.TestBanner()}, poster.banners)

	log, err := c.Notifications.List(t.Context())
	require.NoError(t, err)
	assert.Empty(t, log.Items)
}

func TestThemeWithoutAThemeNotifier(t *testing.T) {
	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t),
		Runner: noPrograms(),
	})

	require.NoError(t, c.Settings.SetTheme(t.Context(), settings.ThemeLight))

	got, err := c.Settings.Theme(t.Context())
	require.NoError(t, err)
	assert.Equal(t, settings.ThemeLight, got)
}

func TestAppInfoReportsVersion(t *testing.T) {
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
}

func TestInboxRefreshUsesInjectedRunner(t *testing.T) {
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
	runner := noPrograms()

	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t),
		Runner: runner,
	})

	states := itemStates{tokens: github.NewTokenSource(runner, execpath.Finder{Home: testutil.ClaudeHome(t)}), client: github.NewClient("test")}

	// The token is borrowed through the injected runner, which runs nothing, so no
	// request is made.
	_, err := states.ItemStates(t.Context(), []github.ItemRef{{Owner: "acme-org", Name: "example-repo", Number: 1}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, github.ErrNotLoggedIn) || errors.Is(err, github.ErrGHNotFound), err)

	assert.Zero(t, c.Worktrees.LastSweep().Seq, "nothing sweeps until the shell starts the sweeps")
}

func TestBootstrapAddsTheStarterPrompts(t *testing.T) {
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

	require.NoError(t, c.Prompts.Delete(t.Context(), quick))
	require.NoError(t, c.Close())

	c = bootstrap(t, opts)

	all, err = c.Prompts.List(t.Context())
	require.NoError(t, err)
	assert.Len(t, all, len(prompts.Starters())-1)
}
