package core

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/timing"
)

var errNoPrograms = errors.New("tests run no external programs")

// recordingRunner records every program it is asked to run and runs none.
type recordingRunner struct {
	mu    sync.Mutex
	calls []string
}

func (r *recordingRunner) Run(_ context.Context, name string, _ ...string) (stdout, stderr []byte, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, filepath.Base(name))

	return nil, []byte("not run"), errNoPrograms
}

func (r *recordingRunner) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.calls...)
}

// tempHome returns a home directory holding the given entries as directories. Each
// ~/.claude_<name> or ~/.claude-<name> one is an agent config directory, holding the
// state file the agent writes on its first run with it.
func tempHome(t *testing.T, dirs ...string) string {
	t.Helper()

	home := t.TempDir()
	for _, d := range dirs {
		makeDir(t, home, d)
	}

	return home
}

// makeDir creates dir under home, as an agent config directory when its name is a
// named profile's.
func makeDir(t *testing.T, home, dir string) {
	t.Helper()

	path := filepath.Join(home, dir)
	require.NoError(t, os.Mkdir(path, 0o700))

	if strings.HasPrefix(dir, ".claude_") || strings.HasPrefix(dir, ".claude-") {
		require.NoError(t, os.WriteFile(filepath.Join(path, ".claude.json"), []byte("{}"), 0o600))
	}
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
	home := tempHome(t, ".claude", ".claude_work")
	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))
	runner := &recordingRunner{}

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

	assert.Empty(t, runner.names(), "startup runs no external program")
}

func TestBootstrapKeepsExistingProfiles(t *testing.T) {
	home := tempHome(t, ".claude", ".claude_work")
	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

	first, err := Bootstrap(t.Context(), Options{Paths: paths, Home: home, Runner: &recordingRunner{}})
	require.NoError(t, err)
	require.NoError(t, first.DB.Exec("UPDATE claude_profiles SET model = 'opus' WHERE name = 'work'").Error)
	require.NoError(t, first.Close())

	// The work profile's directory is gone and a new one appeared.
	require.NoError(t, os.RemoveAll(filepath.Join(home, ".claude_work")))
	makeDir(t, home, ".claude-alt")

	c := bootstrap(t, Options{Paths: paths, Home: home, Runner: &recordingRunner{}})

	assert.ElementsMatch(t, []string{profiles.DefaultName, "work", "alt"}, profileNames(t, c))

	work, err := profiles.Lookup(t.Context(), c.DB, "work")
	require.NoError(t, err)
	assert.Equal(t, "opus", work.Model, "an edited row is never overwritten")
}

func TestBootstrapRemovesProfilesForOtherToolsFolders(t *testing.T) {
	home := tempHome(t, ".claude", ".claude_work", ".claude-dup")
	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

	// Folders other tools name ~/.claude-<name>, and an empty twin of a real profile.
	for _, dir := range []string{".claude-squad", ".claude-monitor", ".claude_dup"} {
		require.NoError(t, os.Mkdir(filepath.Join(home, dir), 0o700))
	}

	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude-squad", "state.json"), []byte("{}"), 0o600))

	// An older version stored a profile for each of them.
	first := bootstrap(t, Options{Paths: paths, Home: home, Runner: &recordingRunner{}})
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
		Runner: &recordingRunner{},
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
	home := tempHome(t, ".claude")

	first := bootstrap(t, Options{Paths: paths, Home: home, Runner: &recordingRunner{}})
	require.NoError(t, first.DB.Exec("DROP TABLE claude_profiles").Error)
	require.NoError(t, first.Close())

	_, err := Bootstrap(t.Context(), Options{Paths: paths, Home: home, Runner: &recordingRunner{}})
	require.ErrorContains(t, err, "prune profiles")
}

func TestBootstrapUnreadableHomeSkipsDiscovery(t *testing.T) {
	var logs bytes.Buffer

	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   filepath.Join(t.TempDir(), "missing-home"),
		Runner: &recordingRunner{},
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
		Home:   tempHome(t, ".claude"),
		Runner: &recordingRunner{},
	})

	services := c.Services()

	assert.Equal(t, []any{c.Settings, c.Profiles, c.Inbox, c.Launcher, c.Worktrees, c.AppInfo}, services)

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
		Home:         tempHome(t),
		Runner:       &recordingRunner{},
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

func TestAppInfoReportsVersion(t *testing.T) {
	c := bootstrap(t, Options{
		Paths:   appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:    tempHome(t),
		Version: "v1.2.3",
		Runner:  &recordingRunner{},
	})

	assert.Equal(t, "v1.2.3", c.AppInfo.Info().Version)
}

func TestInboxRefreshUsesInjectedRunner(t *testing.T) {
	runner := &recordingRunner{}

	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   tempHome(t),
		Runner: runner,
	})

	// The token is borrowed through the injected runner, which runs nothing, so the
	// refresh fails before any request is made.
	_, err := c.Inbox.Refresh(t.Context())
	require.Error(t, err)

	switch {
	case errors.Is(err, github.ErrNotLoggedIn):
		assert.Equal(t, []string{"gh"}, runner.names())
	case errors.Is(err, github.ErrGHNotFound):
		assert.Empty(t, runner.names())
	default:
		t.Fatalf("unexpected refresh error: %v", err)
	}

	snap, err := c.Inbox.Snapshot(t.Context())
	require.NoError(t, err)
	assert.NotEmpty(t, snap.LastError)
}
