package settings

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/store"
)

// openDB opens the database at path, closing it when the test ends.
func openDB(t *testing.T, path string) *gorm.DB {
	t.Helper()

	db, err := store.Open(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close(db) })

	return db
}

// newTestService returns a Service over a fresh database and a temp home directory.
func newTestService(t *testing.T) (*Service, *gorm.DB, string) {
	t.Helper()

	home := t.TempDir()
	db := openDB(t, filepath.Join(t.TempDir(), "app.db"))

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
		DefaultProfile:  "default",
		WorktreeDefault: true,
		Terminal:        "iterm2",
		CloneRoot:       "~/projects",
	}, got)
}

func TestTerminalPrefPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	home := t.TempDir()

	db, err := store.Open(t.Context(), path)
	require.NoError(t, err)
	require.NoError(t, NewService(db, home).SetTerminal(t.Context(), TerminalApp))
	require.NoError(t, store.Close(db))

	got, err := NewService(openDB(t, path), home).Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, TerminalApp, got.Terminal)
}

func TestSettersPersist(t *testing.T) {
	svc, db, home := newTestService(t)
	require.NoError(t, profiles.Sync(t.Context(), db, []profiles.Profile{{Name: "work", ConfigDir: "/opt/profiles/work"}}))
	require.NoError(t, os.MkdirAll(filepath.Join(home, "code", "clones"), 0o700))

	require.NoError(t, svc.SetDefaultProfile(t.Context(), "work"))
	require.NoError(t, svc.SetWorktreeDefault(t.Context(), false))
	require.NoError(t, svc.SetTerminal(t.Context(), TerminalApp))
	require.NoError(t, svc.SetTerminal(t.Context(), TerminalITerm2))
	require.NoError(t, svc.SetCloneRoot(t.Context(), " ~/code/clones/ "))

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, Settings{
		DefaultProfile:  "work",
		WorktreeDefault: false,
		Terminal:        TerminalITerm2,
		CloneRoot:       "~/code/clones",
	}, got)

	abs := t.TempDir()
	require.NoError(t, svc.SetCloneRoot(t.Context(), abs))
	require.NoError(t, svc.SetWorktreeDefault(t.Context(), true))

	got, err = svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, abs, got.CloneRoot)
	assert.True(t, got.WorktreeDefault)
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
		{name: "empty clone root", set: func() error { return svc.SetCloneRoot(t.Context(), "  ") }, wantErr: localrepos.ErrEmptyRoot},
		{name: "relative clone root", set: func() error { return svc.SetCloneRoot(t.Context(), "projects") }, wantErr: localrepos.ErrRelativeRoot},
		{name: "missing clone root", set: func() error { return svc.SetCloneRoot(t.Context(), "~/missing") }, wantErr: fs.ErrNotExist},
		{name: "clone root is a file", set: func() error { return svc.SetCloneRoot(t.Context(), file) }, wantErr: ErrInvalidCloneRoot, wantMsg: "~/notes.txt is not a directory"},
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
	setPref(t, db, KeyTerminal, "warp")
	setPref(t, db, KeyCloneRoot, "   ")

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, fromStored(nil), got)
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
}
