package agent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
)

func TestLookup(t *testing.T) {
	t.Parallel()

	got, err := agent.Lookup("")
	require.NoError(t, err)
	assert.Equal(t, agent.Default(), got)

	got, err = agent.Lookup(agent.Claude)
	require.NoError(t, err)
	assert.Equal(t, agent.Agent{ID: "claude", Name: "Claude", Command: "claude"}, got)

	_, err = agent.Lookup("codex")
	require.ErrorIs(t, err, agent.ErrUnknownAgent)
}

func TestAllHasUniqueIDs(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}

	for _, a := range agent.All() {
		assert.False(t, seen[a.ID], a.ID)
		assert.NotEmpty(t, a.Name)
		assert.NotEmpty(t, a.Command)

		seen[a.ID] = true
	}

	assert.Equal(t, agent.All()[0], agent.Default())
}
