package agent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
)

// unknownAgent is an agent ID no build knows.
const unknownAgent agent.ID = "example-agent"

// flagAdvisor is an extra flag no agent reserves.
const flagAdvisor = "--advisor"

func TestLookup(t *testing.T) {
	t.Parallel()

	got, err := agent.Lookup(agent.Inherit)
	require.NoError(t, err)
	assert.Equal(t, agent.Default(), got)
	assert.Equal(t, agent.Claude, got.ID)

	got, err = agent.Lookup(agent.Claude)
	require.NoError(t, err)
	assert.Equal(t, "Claude", got.Name)
	assert.Equal(t, "claude", got.Command)
	assert.Equal(t, "CLAUDE_CONFIG_DIR", got.ConfigEnv)
	assert.Equal(t, ".claude", got.ConfigDir)
	assert.True(t, got.SessionNames)

	got, err = agent.Lookup(agent.Codex)
	require.NoError(t, err)
	assert.Equal(t, "Codex", got.Name)
	assert.Equal(t, "codex", got.Command)
	assert.Equal(t, "CODEX_HOME", got.ConfigEnv)
	assert.Equal(t, ".codex", got.ConfigDir)
	assert.Equal(t, "Read-only", got.PlanLabel)
	assert.False(t, got.SessionNames)

	_, err = agent.Lookup(unknownAgent)
	require.ErrorIs(t, err, agent.ErrUnknownAgent)
}

func TestAllHasUniqueIDsAndCompleteSpecs(t *testing.T) {
	t.Parallel()

	seen := map[agent.ID]bool{}
	dirs := map[string]bool{}

	for _, a := range agent.All() {
		assert.False(t, seen[a.ID], a.ID)
		assert.False(t, dirs[a.ConfigDir], a.ConfigDir)
		assert.NotEmpty(t, a.Name)
		assert.NotEmpty(t, a.Command)
		assert.NotEmpty(t, a.ConfigEnv)
		assert.NotEmpty(t, a.Markers)
		assert.NotEmpty(t, a.EffortLevels)
		assert.NotNil(t, a.Models)
		assert.NotEmpty(t, a.PermissionModes)
		assert.NotEmpty(t, a.PlanLabel)
		assert.True(t, agent.Known(a.ID))

		for _, level := range a.EffortLevels {
			assert.NotEmpty(t, level.Label, level.Value)
		}

		seen[a.ID] = true
		dirs[a.ConfigDir] = true
	}

	assert.Equal(t, agent.All()[0], agent.Default())
	assert.Equal(t, []agent.ID{agent.Claude, agent.Codex}, agent.IDs())
	assert.False(t, agent.Known(agent.Inherit))
	assert.False(t, agent.Known(unknownAgent))
}

func TestAllReturnsCopies(t *testing.T) {
	t.Parallel()

	first := agent.All()
	first[0].Markers[0].Name = "changed"
	first[0].EffortLevels = nil

	again := agent.All()
	assert.Equal(t, ".claude.json", again[0].Markers[0].Name)
	assert.NotEmpty(t, again[0].EffortLevels)
}

func TestPromptLast(t *testing.T) {
	t.Parallel()

	assert.False(t, agent.PromptLast(agent.Claude))
	assert.True(t, agent.PromptLast(agent.Codex))
	assert.False(t, agent.PromptLast(unknownAgent))
}

func TestValidPermissionMode(t *testing.T) {
	t.Parallel()

	assert.True(t, agent.ValidPermissionMode(agent.Claude, "acceptEdits"))
	assert.True(t, agent.ValidPermissionMode(agent.Claude, "plan"))
	assert.False(t, agent.ValidPermissionMode(agent.Claude, "read-only"))
	assert.True(t, agent.ValidPermissionMode(agent.Codex, "read-only"))
	assert.True(t, agent.ValidPermissionMode(agent.Codex, "workspace-write"))
	assert.True(t, agent.ValidPermissionMode(agent.Codex, "danger-full-access"))
	assert.False(t, agent.ValidPermissionMode(agent.Codex, "acceptEdits"))
	assert.False(t, agent.ValidPermissionMode(agent.Codex, ""))
	assert.False(t, agent.ValidPermissionMode(unknownAgent, "read-only"))
}
