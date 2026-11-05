package profiles

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/profiles/model"
	"github.com/mrz1836/nilbox/internal/store"
)

// errRefused is what a test's database callback fails with.
var errRefused = errors.New("refused by the test")

// openDB opens a fresh, migrated database.
func openDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close(db) })

	return db
}

// mkdirs creates each directory under home.
func mkdirs(t *testing.T, home string, names ...string) {
	t.Helper()

	for _, name := range names {
		require.NoError(t, os.MkdirAll(filepath.Join(home, name), 0o700))
	}
}

// configs creates each directory under home as an agent config directory, holding the
// state file the agent writes on its first run with it.
func configs(t *testing.T, home string, names ...string) {
	t.Helper()

	for _, name := range names {
		mkdirs(t, home, name)
		touch(t, filepath.Join(home, name), ".claude.json")
	}
}

// touch creates each empty file under home.
func touch(t *testing.T, home string, names ...string) {
	t.Helper()

	for _, name := range names {
		require.NoError(t, os.WriteFile(filepath.Join(home, name), []byte("{}"), 0o600))
	}
}

// names returns the profile names in order.
func names(profiles []Profile) []string {
	out := make([]string, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, p.Name)
	}

	return out
}

func TestDiscoverProfiles(t *testing.T) {
	home := t.TempDir()
	mkdirs(t, home, ".claude", ".config")
	configs(t, home, ".claude_work", ".claude-alt", ".claude_", ".claude-", ".claude_-bad", ".claude_two words")
	touch(t, home, ".claude.json", ".claude.json.backup", ".claude_file")

	got, err := Discover(home)
	require.NoError(t, err)

	abs, err := filepath.Abs(home)
	require.NoError(t, err)

	assert.Equal(t, []Profile{
		newDiscovered(DefaultName, ""),
		newDiscovered("alt", filepath.Join(abs, ".claude-alt")),
		newDiscovered("work", filepath.Join(abs, ".claude_work")),
	}, got)

	for _, p := range got {
		assert.Equal(t, PermissionFull, p.PermissionMode)
		assert.Empty(t, p.Model)
		assert.Empty(t, p.Effort)
		assert.False(t, p.PlanMode)
		assert.NotNil(t, p.AddDirs)
		assert.Empty(t, p.AddDirs)
		assert.NotNil(t, p.ExtraArgs)
		assert.Empty(t, p.ExtraArgs)
		assert.NotNil(t, p.Env)
		assert.Empty(t, p.Env)
	}
}

func TestDiscoverDuplicateNames(t *testing.T) {
	home := t.TempDir()
	mkdirs(t, home, ".claude")
	configs(t, home, ".claude_default", ".claude-default", ".claude-dup", ".claude_dup")

	got, err := Discover(home)
	require.NoError(t, err)

	abs, err := filepath.Abs(home)
	require.NoError(t, err)

	assert.Equal(t, []Profile{
		newDiscovered(DefaultName, ""),
		newDiscovered("dup", filepath.Join(abs, ".claude_dup")),
	}, got)
}

func TestDiscoverFollowsSymlinks(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(t.TempDir(), "shared-config")
	require.NoError(t, os.MkdirAll(target, 0o700))
	touch(t, target, ".claude.json")
	require.NoError(t, os.Symlink(target, filepath.Join(home, ".claude_shared")))
	require.NoError(t, os.Symlink(filepath.Join(home, "missing"), filepath.Join(home, ".claude_gone")))

	got, err := Discover(home)
	require.NoError(t, err)
	assert.Equal(t, []string{"shared"}, names(got))
}

func TestDiscoverWithoutProfiles(t *testing.T) {
	got, err := Discover(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, got)

	_, err = Discover(filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)
}

func TestSyncDoesNotOverwriteOrDelete(t *testing.T) {
	db := openDB(t)
	home := t.TempDir()
	mkdirs(t, home, ".claude")
	configs(t, home, ".claude_work", ".claude-alt")

	found, err := Discover(home)
	require.NoError(t, err)
	require.NoError(t, Sync(t.Context(), db, found))

	// The user edits a profile by hand.
	require.NoError(t, db.Model(&model.ClaudeProfile{}).Where("name = ?", "work").Updates(map[string]any{
		"model":           "opus",
		"effort":          "high",
		"permission_mode": "acceptEdits",
		"plan_mode":       true,
		"add_dirs":        `["~/projects/shared"]`,
	}).Error)

	// A directory disappears and a new one appears; discovery runs again.
	require.NoError(t, os.RemoveAll(filepath.Join(home, ".claude-alt")))
	configs(t, home, ".claude_new")

	found, err = Discover(home)
	require.NoError(t, err)
	require.NoError(t, Sync(t.Context(), db, found))
	require.NoError(t, Sync(t.Context(), db, found))

	list, err := NewService(db, home).List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{DefaultName, "alt", "new", "work"}, names(list))

	work, err := Lookup(t.Context(), db, "work")
	require.NoError(t, err)
	assert.Equal(t, "opus", work.Model)
	assert.Equal(t, "high", work.Effort)
	assert.Equal(t, "acceptEdits", work.PermissionMode)
	assert.True(t, work.PlanMode)
	assert.Equal(t, []string{"~/projects/shared"}, work.AddDirs)

	alt, err := Lookup(t.Context(), db, "alt")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".claude-alt"), alt.ConfigDir)
}

func TestSyncNothingFound(t *testing.T) {
	db := openDB(t)
	require.NoError(t, Sync(t.Context(), db, nil))

	list, err := NewService(db, t.TempDir()).List(t.Context())
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestSyncError(t *testing.T) {
	db := openDB(t)
	require.NoError(t, store.Close(db))

	err := Sync(t.Context(), db, []Profile{newDiscovered(DefaultName, "")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store discovered profiles")
}

func TestListOrdersDefaultFirst(t *testing.T) {
	db := openDB(t)

	require.NoError(t, Sync(t.Context(), db, []Profile{
		newDiscovered("zeta", "/opt/profiles/zeta"),
		newDiscovered("alpha", "/opt/profiles/alpha"),
		newDiscovered(DefaultName, ""),
		newDiscovered("Beta", "/opt/profiles/beta"),
	}))

	list, err := NewService(db, t.TempDir()).List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{DefaultName, "Beta", "alpha", "zeta"}, names(list))
	assert.Equal(t, DefaultName, list[0].Name)
	assert.Empty(t, list[0].ConfigDir)
}

func TestListError(t *testing.T) {
	db := openDB(t)
	require.NoError(t, store.Close(db))

	_, err := NewService(db, t.TempDir()).List(t.Context())
	require.Error(t, err)
}

func TestLookup(t *testing.T) {
	db := openDB(t)
	require.NoError(t, Sync(t.Context(), db, []Profile{{
		Name:      "work",
		ConfigDir: "/opt/profiles/work",
		Env:       map[string]string{"EXAMPLE_FLAG": "1"},
		ExtraArgs: []string{"--verbose"},
	}}))

	got, err := Lookup(t.Context(), db, "work")
	require.NoError(t, err)
	assert.Equal(t, Profile{
		Name:      "work",
		ConfigDir: "/opt/profiles/work",
		AddDirs:   []string{},
		ExtraArgs: []string{"--verbose"},
		Env:       map[string]string{"EXAMPLE_FLAG": "1"},
	}, got)

	_, err = Lookup(t.Context(), db, "missing")
	require.ErrorIs(t, err, ErrProfileNotFound)

	require.NoError(t, store.Close(db))

	_, err = Lookup(t.Context(), db, "work")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrProfileNotFound)
}

// skipAsRoot skips a test that needs file permissions enforced, which root ignores.
func skipAsRoot(t *testing.T) {
	t.Helper()

	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
}

// lockDir makes dir unreadable until the test ends.
func lockDir(t *testing.T, dir string) {
	t.Helper()

	require.NoError(t, os.Chmod(dir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // restoring the test's own directory
}

func TestDiscoverNeedsAnAgentConfig(t *testing.T) {
	elsewhere := t.TempDir()
	touch(t, elsewhere, "settings.json")

	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  bool
	}{
		{name: "state file", setup: func(t *testing.T, dir string) { touch(t, dir, ".claude.json") }, want: true},
		{name: "settings", setup: func(t *testing.T, dir string) { touch(t, dir, "settings.json") }, want: true},
		{name: "session history", setup: func(t *testing.T, dir string) { mkdirs(t, dir, "projects") }, want: true},
		{
			name: "settings linked in from elsewhere",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.Symlink(filepath.Join(elsewhere, "settings.json"), filepath.Join(dir, "settings.json")))
			},
			want: true,
		},
		{name: "empty", setup: func(*testing.T, string) {}, want: false},
		{
			name: "a session manager's own config",
			setup: func(t *testing.T, dir string) {
				touch(t, dir, "config.json", "state.json")
				mkdirs(t, dir, "worktrees")
			},
			want: false,
		},
		{
			name: "a usage monitor's data",
			setup: func(t *testing.T, dir string) {
				touch(t, dir, "last_used.json")
				mkdirs(t, dir, "cache", "logs", "reports")
			},
			want: false,
		},
		{name: "a projects file", setup: func(t *testing.T, dir string) { touch(t, dir, "projects") }, want: false},
		{name: "a settings directory", setup: func(t *testing.T, dir string) { mkdirs(t, dir, "settings.json") }, want: false},
		{
			name: "a broken settings link",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "settings.json")))
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			mkdirs(t, home, ".claude", ".claude-candidate")
			tt.setup(t, filepath.Join(home, ".claude-candidate"))

			got, err := Discover(home)
			require.NoError(t, err)

			if tt.want {
				assert.Equal(t, []string{DefaultName, "candidate"}, names(got))
			} else {
				assert.Equal(t, []string{DefaultName}, names(got), "the default profile needs no config")
			}
		})
	}
}

func TestDiscoverSkipsUnreadableFolders(t *testing.T) {
	skipAsRoot(t)

	home := t.TempDir()
	configs(t, home, ".claude_locked")
	lockDir(t, filepath.Join(home, ".claude_locked"))

	got, err := Discover(home)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestDiscoverPrefersAConfigOverAnEmptyTwin(t *testing.T) {
	home := t.TempDir()
	mkdirs(t, home, ".claude_dup")
	configs(t, home, ".claude-dup")

	got, err := Discover(home)
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, "dup", got[0].Name)
	assert.Equal(t, ".claude-dup", filepath.Base(got[0].ConfigDir))
}

func TestPruneRemovesUneditedProfilesWithNoAgentConfig(t *testing.T) {
	db := openDB(t)
	home := t.TempDir()
	dir := func(name string) string { return filepath.Join(home, name) }

	mkdirs(t, home, ".claude", ".claude-squad/worktrees", ".claude-monitor/cache", ".claude-edited", ".claude-empty")
	touch(t, dir(".claude-squad"), "config.json", "state.json")
	configs(t, home, ".claude_real")

	// Rows an older discovery stored for every folder, and one whose folder is gone.
	require.NoError(t, Sync(t.Context(), db, []Profile{
		newDiscovered(DefaultName, ""),
		newDiscovered("real", dir(".claude_real")),
		newDiscovered("squad", dir(".claude-squad")),
		newDiscovered("monitor", dir(".claude-monitor")),
		newDiscovered("edited", dir(".claude-edited")),
		newDiscovered("empty", dir(".claude-empty")),
		newDiscovered("gone", dir(".claude-gone")),
	}))
	require.NoError(t, db.Model(&model.ClaudeProfile{}).Where("name = ?", "edited").Update("model", "opus").Error)

	removed, err := Prune(t.Context(), db)
	require.NoError(t, err)
	assert.Equal(t, []string{"empty", "monitor", "squad"}, removed)

	list, err := NewService(db, home).List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{DefaultName, "edited", "gone", "real"}, names(list))

	// Only the rows go; every folder stays on disk.
	for _, name := range []string{".claude-squad", ".claude-monitor", ".claude-empty", ".claude-edited"} {
		assert.DirExists(t, dir(name))
	}

	removed, err = Prune(t.Context(), db)
	require.NoError(t, err)
	assert.Empty(t, removed, "pruning again finds nothing")
}

func TestPruneKeepsTheDefaultAndWhatItCannotRead(t *testing.T) {
	skipAsRoot(t)

	db := openDB(t)
	home := t.TempDir()
	mkdirs(t, home, ".claude", ".claude-locked", ".claude_default")
	lockDir(t, filepath.Join(home, ".claude-locked"))

	require.NoError(t, Sync(t.Context(), db, []Profile{
		newDiscovered(DefaultName, ""),
		newDiscovered("locked", filepath.Join(home, ".claude-locked")),
	}))

	removed, err := Prune(t.Context(), db)
	require.NoError(t, err)
	assert.Empty(t, removed)

	list, err := NewService(db, home).List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{DefaultName, "locked"}, names(list))
}

func TestPruneErrors(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		db := openDB(t)
		require.NoError(t, store.Close(db))

		_, err := Prune(t.Context(), db)
		require.ErrorContains(t, err, "read profiles to prune")
	})

	t.Run("remove", func(t *testing.T) {
		db := openDB(t)
		home := t.TempDir()
		mkdirs(t, home, ".claude-stray")
		require.NoError(t, Sync(t.Context(), db, []Profile{newDiscovered("stray", filepath.Join(home, ".claude-stray"))}))

		require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("test:refuse", func(tx *gorm.DB) {
			_ = tx.AddError(errRefused)
		}))

		_, err := Prune(t.Context(), db)
		require.ErrorIs(t, err, errRefused)
		require.ErrorContains(t, err, "remove profiles with no agent config")
	})
}

func TestUntouched(t *testing.T) {
	fresh := toRow(newDiscovered("work", "/opt/profiles/work"))
	assert.True(t, untouched(fresh))

	tests := []struct {
		name string
		edit func(row *model.ClaudeProfile)
	}{
		{name: "model", edit: func(row *model.ClaudeProfile) { row.Model = "opus" }},
		{name: "effort", edit: func(row *model.ClaudeProfile) { row.Effort = "high" }},
		{name: "permission mode", edit: func(row *model.ClaudeProfile) { row.PermissionMode = "acceptEdits" }},
		{name: "agent's default permissions", edit: func(row *model.ClaudeProfile) { row.PermissionMode = "" }},
		{name: "plan mode", edit: func(row *model.ClaudeProfile) { row.PlanMode = true }},
		{name: "extra directory", edit: func(row *model.ClaudeProfile) { row.AddDirs = model.StringList{"~/shared"} }},
		{name: "extra argument", edit: func(row *model.ClaudeProfile) { row.ExtraArgs = model.StringList{"--verbose"} }},
		{name: "environment", edit: func(row *model.ClaudeProfile) { row.Env = model.StringMap{"EXAMPLE_FLAG": "1"} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := toRow(newDiscovered("work", "/opt/profiles/work"))
			tt.edit(&row)
			assert.False(t, untouched(row))
		})
	}
}

func TestListDescribesEachFolder(t *testing.T) {
	db := openDB(t)
	home := t.TempDir()
	outside := t.TempDir()
	mkdirs(t, home, ".claude")
	configs(t, home, ".claude_work")

	require.NoError(t, Sync(t.Context(), db, []Profile{
		newDiscovered(DefaultName, ""),
		newDiscovered("work", filepath.Join(home, ".claude_work")),
		newDiscovered("gone", filepath.Join(home, ".claude-gone")),
		newDiscovered("shared", outside),
	}))

	list, err := NewService(db, home).List(t.Context())
	require.NoError(t, err)

	folders := make(map[string][2]any, len(list))
	for _, p := range list {
		folders[p.Name] = [2]any{p.Folder, p.Missing}
	}

	assert.Equal(t, map[string][2]any{
		DefaultName: {"~/.claude", false},
		"work":      {"~/.claude_work", false},
		"gone":      {"~/.claude-gone", true},
		"shared":    {outside, false},
	}, folders)

	// Without ~/.claude, the default profile's folder is missing too.
	list, err = NewService(db, t.TempDir()).List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "~/.claude", list[0].Folder)
	assert.True(t, list[0].Missing)
}
