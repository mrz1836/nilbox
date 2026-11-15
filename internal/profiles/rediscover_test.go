package profiles

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestRediscoverFindsAProfileMadeSinceTheStart(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	home := testutil.AgentHome(t, ".claude")
	logs := &testutil.Logs{}
	svc := NewService(s, home, nil, WithLogger(logs.Logger()))

	require.NoError(t, SyncHome(t.Context(), s, home, nil))

	list, err := svc.List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"claude/default"}, keys(list))

	// The user installs Codex and runs it, and makes a second Claude profile.
	mkdirs(t, home, ".codex", ".claude_work")
	touch(t, filepath.Join(home, ".codex"), "auth.json")
	touch(t, filepath.Join(home, ".claude_work"), ".claude.json")

	list, err = svc.Rediscover(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"claude/default", "claude/work", "codex/default"}, keys(list))
	assert.True(t, list[2].SignedIn, "listed as List lists it")
}

func TestRediscoverKeepsAProfileWhoseFolderIsGone(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	home := testutil.AgentHome(t, ".claude", ".claude_work")
	require.NoError(t, SyncHome(t.Context(), s, home, nil))
	require.NoError(t, os.RemoveAll(filepath.Join(home, ".claude_work")))

	list, err := NewService(s, home, nil).Rediscover(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"claude/default", "claude/work"}, keys(list))
	assert.True(t, list[1].Missing, "its row stays, marked missing")
}

func TestSyncHomeSkipsAHomeItCannotRead(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	logs := &testutil.Logs{}

	require.NoError(t, SyncHome(t.Context(), s, filepath.Join(t.TempDir(), "missing"), logs.Logger()))
	assert.Contains(t, logs.String(), "profile discovery skipped")
}
