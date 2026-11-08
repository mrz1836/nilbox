package agent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/agent"
)

func TestPerAgentMaps(t *testing.T) {
	t.Parallel()

	assert.Equal(t, map[agent.ID]string{}, agent.CloneModels(nil))
	assert.Equal(t, map[agent.ID]string{agent.Codex: "o3"}, agent.CloneModels(map[agent.ID]string{agent.Claude: "", agent.Codex: "o3"}))
	assert.True(t, agent.EqualModels(nil, map[agent.ID]string{agent.Claude: ""}))
	assert.False(t, agent.EqualModels(map[agent.ID]string{agent.Claude: "opus"}, map[agent.ID]string{agent.Codex: "opus"}))

	args := map[agent.ID][]string{agent.Claude: {"--a"}, agent.Codex: {}}
	clone := agent.CloneArgs(args)
	assert.Equal(t, map[agent.ID][]string{agent.Claude: {"--a"}}, clone)

	clone[agent.Claude][0] = "changed"
	assert.Equal(t, "--a", args[agent.Claude][0], "a deep copy")

	assert.Equal(t, map[agent.ID][]string{}, agent.CloneArgs(nil))
	assert.True(t, agent.EqualArgs(nil, map[agent.ID][]string{agent.Codex: nil}))
	assert.True(t, agent.EqualArgs(args, map[agent.ID][]string{agent.Claude: {"--a"}}))
	assert.False(t, agent.EqualArgs(args, map[agent.ID][]string{agent.Claude: {"--b"}}))

	assert.Equal(t, map[agent.ID]string{agent.Claude: "--note 'two words'"},
		agent.JoinArgs(map[agent.ID][]string{agent.Claude: {"--note", "two words"}, agent.Codex: nil}))
	assert.Equal(t, map[agent.ID]string{}, agent.JoinArgs(nil))
}
