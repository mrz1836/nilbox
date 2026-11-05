package agents_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agents"
)

func TestLookup(t *testing.T) {
	got, err := agents.Lookup("")
	require.NoError(t, err)
	assert.Equal(t, agents.Default(), got)

	got, err = agents.Lookup(agents.Claude)
	require.NoError(t, err)
	assert.Equal(t, agents.Agent{ID: "claude", Name: "Claude", Command: "claude"}, got)

	_, err = agents.Lookup("codex")
	require.ErrorIs(t, err, agents.ErrUnknownAgent)
}

func TestAllHasUniqueIDs(t *testing.T) {
	seen := map[string]bool{}

	for _, a := range agents.All() {
		assert.False(t, seen[a.ID], a.ID)
		assert.NotEmpty(t, a.Name)
		assert.NotEmpty(t, a.Command)

		seen[a.ID] = true
	}

	assert.Equal(t, agents.All()[0], agents.Default())
}
