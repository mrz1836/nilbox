package profiles

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// keys returns each profile as "<agent>/<name>", in order.
func keys(profiles []Profile) []string {
	out := make([]string, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, string(p.Agent)+"/"+p.Name)
	}

	return out
}

func TestDiscoverEveryAgent(t *testing.T) {
	t.Parallel()

	home := testutil.AgentHome(t, ".claude", ".claude_work", ".codex", ".codex_work", ".codex-alt", ".codexbar")
	mkdirs(t, home, ".codex_empty")
	touch(t, filepath.Join(home, ".codexbar"), "config.json")

	got, err := Discover(home, nil)
	require.NoError(t, err)

	abs, err := filepath.Abs(home)
	require.NoError(t, err)

	assert.Equal(t, []string{"claude/default", "claude/work", "codex/default", "codex/alt", "codex/work"}, keys(got),
		"each agent has its own default and profiles; another tool's folder and an empty one are left out")
	assert.Equal(t, newDiscovered(agent.Codex, "work", filepath.Join(abs, ".codex_work")), got[4])
	assert.Equal(t, newDiscovered(agent.Codex, DefaultName, ""), got[2])
}

func TestDiscoverCodexNeedsItsOwnMarkers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  bool
	}{
		{name: "sign-in", setup: func(t *testing.T, dir string) { touch(t, dir, "auth.json") }, want: true},
		{name: "settings", setup: func(t *testing.T, dir string) { touch(t, dir, "config.toml") }, want: true},
		{name: "sessions", setup: func(t *testing.T, dir string) { mkdirs(t, dir, "sessions") }, want: true},
		{name: "Claude's state file", setup: func(t *testing.T, dir string) { touch(t, dir, ".claude.json") }, want: false},
		{name: "a sessions file", setup: func(t *testing.T, dir string) { touch(t, dir, "sessions") }, want: false},
		{name: "empty", setup: func(*testing.T, string) {}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			mkdirs(t, home, ".codex_candidate")
			tt.setup(t, filepath.Join(home, ".codex_candidate"))

			got, err := Discover(home, nil)
			require.NoError(t, err)

			if tt.want {
				assert.Equal(t, []string{"codex/candidate"}, keys(got))
			} else {
				assert.Empty(t, got)
			}
		})
	}
}

func TestSyncKeepsEachAgentsProfilesApart(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	home := testutil.AgentHome(t, ".claude", ".claude_work", ".codex", ".codex_work")

	found, err := Discover(home, nil)
	require.NoError(t, err)
	require.NoError(t, s.Sync(t.Context(), found))
	require.NoError(t, s.Sync(t.Context(), found))

	all, err := s.All(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"claude/default", "claude/work", "codex/default", "codex/work"}, keys(all))

	claude, err := s.Lookup(t.Context(), agent.Claude, "work")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".claude_work"), claude.ConfigDir)

	codex, err := s.Lookup(t.Context(), agent.Codex, "work")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".codex_work"), codex.ConfigDir)
}

func TestPruneUsesEachAgentsMarkers(t *testing.T) {
	t.Parallel()

	s, db := openStore(t)
	home := t.TempDir()
	mkdirs(t, home, ".codex_bare", ".codex_kept")
	touch(t, filepath.Join(home, ".codex_kept"), "config.toml")

	require.NoError(t, s.Sync(t.Context(), []Profile{
		newDiscovered(agent.Codex, "bare", filepath.Join(home, ".codex_bare")),
		newDiscovered(agent.Codex, "kept", filepath.Join(home, ".codex_kept")),
		newDiscovered("example-agent", "other", filepath.Join(home, ".codex_bare")),
	}))
	exec(t, db, "UPDATE agent_profiles SET agent = 'example-agent' WHERE name = 'other'")

	removed, err := s.Prune(t.Context(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"codex/bare"}, removed, "an unknown agent's profile is never pruned")

	all, err := s.All(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"codex/kept", "example-agent/other"}, keys(all), "an unknown agent sorts last")
}

func TestListReportsSignIn(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	home := t.TempDir()
	mkdirs(t, home, ".claude", ".codex", ".codex_out", ".codex_key")
	touch(t, filepath.Join(home, ".codex"), "auth.json")
	touch(t, filepath.Join(home, ".codex_out"), "config.toml")
	touch(t, filepath.Join(home, ".codex_key"), "config.toml")

	key := newDiscovered(agent.Codex, "key", filepath.Join(home, ".codex_key"))
	key.Env = map[string]string{"OPENAI_API_KEY": "set"}

	require.NoError(t, s.Sync(t.Context(), []Profile{
		newDiscovered(agent.Claude, DefaultName, ""),
		newDiscovered(agent.Codex, DefaultName, ""),
		newDiscovered(agent.Codex, "out", filepath.Join(home, ".codex_out")),
		key,
		newDiscovered(agent.Codex, "gone", filepath.Join(home, ".codex_gone")),
	}))

	list, err := NewService(s, home, nil).List(t.Context())
	require.NoError(t, err)

	signed := map[string]bool{}
	folders := map[string]string{}

	for _, p := range list {
		signed[string(p.Agent)+"/"+p.Name] = p.SignedIn
		folders[string(p.Agent)+"/"+p.Name] = p.Folder
	}

	assert.Equal(t, map[string]bool{
		"claude/default": true, "codex/default": true, "codex/out": false, "codex/key": true, "codex/gone": false,
	}, signed)
	assert.Equal(t, "~/.codex", folders["codex/default"])
	assert.Equal(t, "~/.claude", folders["claude/default"])
}

// writeModels writes a Codex models cache into dir.
func writeModels(t *testing.T, dir, body string) string {
	t.Helper()

	path := filepath.Join(dir, "models_cache.json")
	testutil.WriteFile(t, path, body)

	return path
}

func TestAgentsReportsInstallsAndCachedModels(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	home := t.TempDir()
	mkdirs(t, home, ".codex")
	writeModels(t, filepath.Join(home, ".codex"), `{"fetched_at": "x", "models": [
		{"slug": "gpt-late", "display_name": "GPT Late", "visibility": "list", "priority": 3},
		{"slug": "gpt-hidden", "display_name": "Hidden", "visibility": "hide", "priority": 1},
		{"slug": "gpt-first", "display_name": "", "visibility": "list", "priority": 2},
		{"slug": "-bad", "display_name": "Bad", "visibility": "list", "priority": 0}
	]}`)

	finder := testutil.Finder{Paths: map[string]string{"codex": "/opt/bin/codex"}}

	got, err := NewService(s, home, finder).Agents(t.Context())
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, agent.Claude, got[0].ID)
	assert.False(t, got[0].Installed)
	assert.Empty(t, got[0].Path)
	assert.NotEmpty(t, got[0].Models, "Claude's suggestions are built in")

	assert.Equal(t, agent.Codex, got[1].ID)
	assert.True(t, got[1].Installed)
	assert.Equal(t, "/opt/bin/codex", got[1].Path)
	assert.Equal(t, []agent.Choice{{Value: "gpt-first", Label: "gpt-first"}, {Value: "gpt-late", Label: "GPT Late"}},
		got[1].Models, "listed models by priority; hidden ones and names the agent would misread are left out")
}

func TestAgentsFallsBackToAnotherProfilesCache(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	home := t.TempDir()
	mkdirs(t, home, ".codex_work")
	writeModels(t, filepath.Join(home, ".codex_work"), `{"models": [{"slug": "gpt-a", "visibility": "list"}]}`)
	require.NoError(t, s.Sync(t.Context(), []Profile{newDiscovered(agent.Codex, "work", filepath.Join(home, ".codex_work"))}))

	got, err := NewService(s, home, nil).Agents(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []agent.Choice{{Value: "gpt-a", Label: "gpt-a"}}, got[1].Models)
	assert.False(t, got[1].Installed, "no finder finds nothing")
}

func TestAgentsWithoutACacheOffersNoModels(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	home := t.TempDir()
	mkdirs(t, home, ".codex")
	writeModels(t, filepath.Join(home, ".codex"), `not json`)

	got, err := NewService(s, home, nil).Agents(t.Context())
	require.NoError(t, err)
	assert.NotNil(t, got[1].Models)
	assert.Empty(t, got[1].Models)

	got, err = NewService(s, t.TempDir(), nil).Agents(t.Context())
	require.NoError(t, err)
	assert.Empty(t, got[1].Models)
}

func TestModelsCacheRereadsOnlyAChangedFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeModels(t, dir, `{"models": [{"slug": "gpt-a", "visibility": "list"}]}`)
	cache := newModelsCache()

	got, ok := cache.read(path)
	require.True(t, ok)
	assert.Equal(t, "gpt-a", got[0].Value)

	got[0].Value = "changed by the caller"

	again, ok := cache.read(path)
	require.True(t, ok)
	assert.Equal(t, "gpt-a", again[0].Value, "callers get copies")

	writeModels(t, dir, `{"models": [{"slug": "gpt-b", "visibility": "list"}, {"slug": "gpt-c", "visibility": "list"}]}`)

	later := time.Now().Add(time.Minute)
	require.NoError(t, os.Chtimes(path, later, later))

	changed, ok := cache.read(path)
	require.True(t, ok)
	assert.Equal(t, []string{"gpt-b", "gpt-c"}, []string{changed[0].Value, changed[1].Value})

	_, ok = cache.read(filepath.Join(dir, "missing.json"))
	assert.False(t, ok)
}

func TestAgentsError(t *testing.T) {
	t.Parallel()

	s, db := openStore(t)
	require.NoError(t, db.Close())

	_, err := NewService(s, t.TempDir(), nil).Agents(t.Context())
	require.ErrorContains(t, err, "list profiles")
}
