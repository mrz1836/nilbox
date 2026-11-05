package profiles

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/profiles/model"
	"github.com/mrz1836/nilbox/internal/store"
)

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
	mkdirs(t, home, ".claude", ".claude_work", ".claude-alt", ".claude_", ".claude-", ".claude_-bad", ".claude_two words", ".config")
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
	mkdirs(t, home, ".claude", ".claude_default", ".claude-default", ".claude-dup", ".claude_dup")

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
	mkdirs(t, home, ".claude", ".claude_work", ".claude-alt")

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
	mkdirs(t, home, ".claude_new")

	found, err = Discover(home)
	require.NoError(t, err)
	require.NoError(t, Sync(t.Context(), db, found))
	require.NoError(t, Sync(t.Context(), db, found))

	list, err := NewService(db).List(t.Context())
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

	list, err := NewService(db).List(t.Context())
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

	list, err := NewService(db).List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{DefaultName, "Beta", "alpha", "zeta"}, names(list))
	assert.Equal(t, newDiscovered(DefaultName, ""), list[0])
}

func TestListError(t *testing.T) {
	db := openDB(t)
	require.NoError(t, store.Close(db))

	_, err := NewService(db).List(t.Context())
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
