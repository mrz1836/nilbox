package profiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// mkdirs creates each directory under home.
func mkdirs(t *testing.T, home string, names ...string) {
	t.Helper()

	for _, name := range names {
		require.NoError(t, os.MkdirAll(filepath.Join(home, name), 0o700))
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
	t.Parallel()

	home := testutil.ClaudeHome(t, ".claude", ".config",
		".claude_work", ".claude-alt", ".claude_", ".claude-", ".claude_-bad", ".claude_two words")
	touch(t, home, ".claude.json", ".claude.json.backup", ".claude_file")

	got, err := Discover(home, nil)
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
	t.Parallel()

	home := testutil.ClaudeHome(t, ".claude", ".claude_default", ".claude-default", ".claude-dup", ".claude_dup")

	var logs testutil.Logs

	got, err := Discover(home, logs.Logger())
	require.NoError(t, err)

	abs, err := filepath.Abs(home)
	require.NoError(t, err)

	assert.Equal(t, []Profile{
		newDiscovered(DefaultName, ""),
		newDiscovered("dup", filepath.Join(abs, ".claude_dup")),
	}, got)

	// Each directory that lost its name is logged once, naming the one that kept it.
	warned := strings.Count(logs.String(), `level=WARN msg="skipping a duplicate profile directory"`)
	assert.Equal(t, 3, warned, logs.String())
	assert.Contains(t, logs.String(), "skipped="+filepath.Join(abs, ".claude-dup"))
}

func TestDiscoverFollowsSymlinks(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	target := filepath.Join(t.TempDir(), "shared-config")
	require.NoError(t, os.MkdirAll(target, 0o700))
	touch(t, target, ".claude.json")
	require.NoError(t, os.Symlink(target, filepath.Join(home, ".claude_shared")))
	require.NoError(t, os.Symlink(filepath.Join(home, "missing"), filepath.Join(home, ".claude_gone")))

	got, err := Discover(home, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"shared"}, names(got))
}

func TestDiscoverWithoutProfiles(t *testing.T) {
	t.Parallel()

	got, err := Discover(t.TempDir(), nil)
	require.NoError(t, err)
	assert.Empty(t, got)

	_, err = Discover(filepath.Join(t.TempDir(), "missing"), nil)
	require.Error(t, err)
}

func TestSyncDoesNotOverwriteOrDelete(t *testing.T) {
	t.Parallel()

	s, db := openStore(t)
	home := testutil.ClaudeHome(t, ".claude", ".claude_work", ".claude-alt")

	found, err := Discover(home, nil)
	require.NoError(t, err)
	require.NoError(t, s.Sync(t.Context(), found))

	// The user edits a profile by hand.
	exec(t, db, `UPDATE claude_profiles SET model = 'opus', effort = 'high', permission_mode = 'acceptEdits',
		plan_mode = 1, add_dirs = '["~/projects/shared"]' WHERE name = 'work'`)

	// A directory disappears and a new one appears; discovery runs again.
	require.NoError(t, os.RemoveAll(filepath.Join(home, ".claude-alt")))
	testutil.MakeClaudeDirs(t, home, ".claude_new")

	found, err = Discover(home, nil)
	require.NoError(t, err)
	require.NoError(t, s.Sync(t.Context(), found))
	require.NoError(t, s.Sync(t.Context(), found))

	list, err := NewService(s, home).List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{DefaultName, "alt", "new", "work"}, names(list))

	work, err := s.Lookup(t.Context(), "work")
	require.NoError(t, err)
	assert.Equal(t, "opus", work.Model)
	assert.Equal(t, "high", work.Effort)
	assert.Equal(t, "acceptEdits", work.PermissionMode)
	assert.True(t, work.PlanMode)
	assert.Equal(t, []string{"~/projects/shared"}, work.AddDirs)

	alt, err := s.Lookup(t.Context(), "alt")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".claude-alt"), alt.ConfigDir)
}

func TestSyncNothingFound(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	require.NoError(t, s.Sync(t.Context(), nil))

	list, err := NewService(s, t.TempDir()).List(t.Context())
	require.NoError(t, err)
	assert.NotNil(t, list, "no profiles is an empty list")
	assert.Empty(t, list)
}

func TestSyncError(t *testing.T) {
	t.Parallel()

	s, db := openStore(t)
	require.NoError(t, db.Close())

	err := s.Sync(t.Context(), []Profile{newDiscovered(DefaultName, "")})
	require.ErrorContains(t, err, "store discovered profiles")
}

func TestListOrdersDefaultFirst(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)

	require.NoError(t, s.Sync(t.Context(), []Profile{
		newDiscovered("zeta", "/opt/profiles/zeta"),
		newDiscovered("alpha", "/opt/profiles/alpha"),
		newDiscovered(DefaultName, ""),
		newDiscovered("Beta", "/opt/profiles/beta"),
	}))

	list, err := NewService(s, t.TempDir()).List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{DefaultName, "Beta", "alpha", "zeta"}, names(list))
	assert.Equal(t, DefaultName, list[0].Name)
	assert.Empty(t, list[0].ConfigDir)
}

func TestListError(t *testing.T) {
	t.Parallel()

	s, db := openStore(t)
	require.NoError(t, db.Close())

	_, err := NewService(s, t.TempDir()).List(t.Context())
	require.ErrorContains(t, err, "list profiles")
}

func TestLookup(t *testing.T) {
	t.Parallel()

	s, db := openStore(t)
	require.NoError(t, s.Sync(t.Context(), []Profile{{
		Name:      "work",
		ConfigDir: "/opt/profiles/work",
		Env:       map[string]string{"EXAMPLE_FLAG": "1"},
		ExtraArgs: []string{"--verbose"},
	}}))

	got, err := s.Lookup(t.Context(), "work")
	require.NoError(t, err)
	assert.Equal(t, Profile{
		Name:      "work",
		ConfigDir: "/opt/profiles/work",
		AddDirs:   []string{},
		ExtraArgs: []string{"--verbose"},
		Env:       map[string]string{"EXAMPLE_FLAG": "1"},
	}, got)

	_, err = s.Lookup(t.Context(), "missing")
	require.ErrorIs(t, err, ErrProfileNotFound)
	require.ErrorContains(t, err, `"missing"`)

	_, err = s.Lookup(t.Context(), "Work")
	require.ErrorIs(t, err, ErrProfileNotFound, "names match exactly, case included")

	require.NoError(t, db.Close())

	_, err = s.Lookup(t.Context(), "work")
	require.ErrorContains(t, err, "look up profile")
	require.NotErrorIs(t, err, ErrProfileNotFound)
}

// lockDir makes dir unreadable until the test ends.
func lockDir(t *testing.T, dir string) {
	t.Helper()

	require.NoError(t, os.Chmod(dir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // restoring the test's own directory
}

func TestDiscoverNeedsAnAgentConfig(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			home := t.TempDir()
			mkdirs(t, home, ".claude", ".claude-candidate")
			tt.setup(t, filepath.Join(home, ".claude-candidate"))

			got, err := Discover(home, nil)
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
	t.Parallel()
	testutil.SkipAsRoot(t)

	home := testutil.ClaudeHome(t, ".claude_locked")
	lockDir(t, filepath.Join(home, ".claude_locked"))

	got, err := Discover(home, nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestDiscoverPrefersAConfigOverAnEmptyTwin(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	mkdirs(t, home, ".claude_dup")
	testutil.MakeClaudeDirs(t, home, ".claude-dup")

	got, err := Discover(home, nil)
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, "dup", got[0].Name)
	assert.Equal(t, ".claude-dup", filepath.Base(got[0].ConfigDir))
}

func TestPruneRemovesUneditedProfilesWithNoAgentConfig(t *testing.T) {
	t.Parallel()

	s, db := openStore(t)
	home := t.TempDir()
	dir := func(name string) string { return filepath.Join(home, name) }

	mkdirs(t, home, ".claude", ".claude-squad/worktrees", ".claude-monitor/cache", ".claude-edited", ".claude-empty")
	touch(t, dir(".claude-squad"), "config.json", "state.json")
	testutil.MakeClaudeDirs(t, home, ".claude_real")

	// Rows an older discovery stored for every folder, and one whose folder is gone.
	require.NoError(t, s.Sync(t.Context(), []Profile{
		newDiscovered(DefaultName, ""),
		newDiscovered("real", dir(".claude_real")),
		newDiscovered("squad", dir(".claude-squad")),
		newDiscovered("monitor", dir(".claude-monitor")),
		newDiscovered("edited", dir(".claude-edited")),
		newDiscovered("empty", dir(".claude-empty")),
		newDiscovered("gone", dir(".claude-gone")),
	}))
	exec(t, db, "UPDATE claude_profiles SET model = 'opus' WHERE name = 'edited'")

	var logs testutil.Logs

	removed, err := s.Prune(t.Context(), logs.Logger())
	require.NoError(t, err)
	assert.Equal(t, []string{"empty", "monitor", "squad"}, removed)
	assert.Contains(t, logs.String(),
		`level=WARN msg="keeping an edited profile whose directory holds no agent config" profile=edited`)

	list, err := NewService(s, home).List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{DefaultName, "edited", "gone", "real"}, names(list))

	// Only the rows go; every folder stays on disk.
	for _, name := range []string{".claude-squad", ".claude-monitor", ".claude-empty", ".claude-edited"} {
		assert.DirExists(t, dir(name))
	}

	removed, err = s.Prune(t.Context(), nil)
	require.NoError(t, err)
	assert.Empty(t, removed, "pruning again finds nothing")
}

func TestPruneKeepsTheDefaultAndWhatItCannotRead(t *testing.T) {
	t.Parallel()
	testutil.SkipAsRoot(t)

	s, _ := openStore(t)
	home := t.TempDir()
	mkdirs(t, home, ".claude", ".claude-locked", ".claude_default")
	lockDir(t, filepath.Join(home, ".claude-locked"))

	require.NoError(t, s.Sync(t.Context(), []Profile{
		newDiscovered(DefaultName, ""),
		newDiscovered("locked", filepath.Join(home, ".claude-locked")),
	}))

	removed, err := s.Prune(t.Context(), nil)
	require.NoError(t, err)
	assert.Empty(t, removed)

	list, err := NewService(s, home).List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{DefaultName, "locked"}, names(list))
}

func TestPruneErrors(t *testing.T) {
	t.Parallel()

	t.Run("read", func(t *testing.T) {
		t.Parallel()

		s, db := openStore(t)
		require.NoError(t, db.Close())

		_, err := s.Prune(t.Context(), nil)
		require.ErrorContains(t, err, "read profiles to prune")
	})

	t.Run("remove", func(t *testing.T) {
		t.Parallel()

		s, db := openStore(t)
		home := t.TempDir()
		mkdirs(t, home, ".claude-stray")
		require.NoError(t, s.Sync(t.Context(), []Profile{newDiscovered("stray", filepath.Join(home, ".claude-stray"))}))
		exec(t, db, refuseRemoval)

		_, err := s.Prune(t.Context(), nil)
		require.ErrorContains(t, err, "refused by the test")
		require.ErrorContains(t, err, "remove profiles with no agent config")

		_, err = s.Lookup(t.Context(), "stray")
		require.NoError(t, err, "the row is kept")
	})
}

func TestUntouched(t *testing.T) {
	t.Parallel()

	assert.True(t, untouched(newDiscovered("work", "/opt/profiles/work")))

	tests := []struct {
		name string
		edit func(p *Profile)
	}{
		{name: "model", edit: func(p *Profile) { p.Model = "opus" }},
		{name: "effort", edit: func(p *Profile) { p.Effort = "high" }},
		{name: "permission mode", edit: func(p *Profile) { p.PermissionMode = "acceptEdits" }},
		{name: "agent's default permissions", edit: func(p *Profile) { p.PermissionMode = "" }},
		{name: "plan mode", edit: func(p *Profile) { p.PlanMode = true }},
		{name: "extra directory", edit: func(p *Profile) { p.AddDirs = []string{"~/shared"} }},
		{name: "extra argument", edit: func(p *Profile) { p.ExtraArgs = []string{"--verbose"} }},
		{name: "environment", edit: func(p *Profile) { p.Env = map[string]string{"EXAMPLE_FLAG": "1"} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := newDiscovered("work", "/opt/profiles/work")
			tt.edit(&p)
			assert.False(t, untouched(p))
		})
	}
}

func TestListDescribesEachFolder(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	home := t.TempDir()
	outside := t.TempDir()
	mkdirs(t, home, ".claude")
	testutil.MakeClaudeDirs(t, home, ".claude_work")

	require.NoError(t, s.Sync(t.Context(), []Profile{
		newDiscovered(DefaultName, ""),
		newDiscovered("work", filepath.Join(home, ".claude_work")),
		newDiscovered("gone", filepath.Join(home, ".claude-gone")),
		newDiscovered("shared", outside),
	}))

	list, err := NewService(s, home).List(t.Context())
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
	list, err = NewService(s, t.TempDir()).List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "~/.claude", list[0].Folder)
	assert.True(t, list[0].Missing)
}
