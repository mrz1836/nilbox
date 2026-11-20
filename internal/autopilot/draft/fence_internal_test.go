package draft

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// agentSpec is a routine whose one step is a as an agent step.
func agentSpec(a spec.AgentStep) spec.Spec {
	return spec.Spec{Steps: []spec.Step{{Agent: &a}}}
}

func TestAPresetItsOwnAgentCantKeepIsAProblem(t *testing.T) {
	t.Parallel()

	tasks := &spec.Fence{Tools: catalog.PresetGitHubTasks}

	problems := fenceProblems(agentSpec(spec.AgentStep{Agent: "codex", Fence: tasks}))
	require.Len(t, problems, 1)
	assert.Equal(t, "[0].agent.fence.tools", problems[0].Field)
	assert.Contains(t, problems[0].Message, "Codex can't keep GitHub tasks only", "the builder says why")

	assert.Empty(t, fenceProblems(agentSpec(spec.AgentStep{Agent: "codex"})), "Codex keeps the flagship's preset")
	assert.Empty(t, fenceProblems(agentSpec(spec.AgentStep{Agent: "claude", Fence: tasks})))
	assert.Empty(t, fenceProblems(agentSpec(spec.AgentStep{Fence: tasks})), "Inherit is checked as it starts, per repository")
	assert.Empty(t, fenceProblems(agentSpec(spec.AgentStep{Agent: "codex", Fence: tasks, Where: catalog.WhereTerminal})),
		"a terminal run is a click's launch, with no fence")
}

func TestAFallbackThatCantKeepThePresetIsWarnedOf(t *testing.T) {
	t.Parallel()

	step := spec.AgentStep{
		Agent: "claude", Fence: &spec.Fence{Tools: catalog.PresetGitHubTasks},
		Fallbacks: []spec.ProfileRef{{Agent: "codex", Profile: "default"}, {Agent: "claude", Profile: "work"}},
		LaunchFor: []spec.LaunchRow{{Target: "acme-org", Agent: "codex"}},
	}

	warnings := fenceWarnings(agentSpec(step))
	require.Len(t, warnings, 2)
	assert.Equal(t, "[0].agent.launchFor", warnings[0].Field)
	assert.Equal(t, "[0].agent.fallbacks", warnings[1].Field)
	assert.Contains(t, warnings[1].Message, "A fallback to it is passed over.")
}

func TestEachAgentTheChainNamesIsWarnedOfOnce(t *testing.T) {
	t.Parallel()

	codex := string(agent.Codex)
	step := spec.AgentStep{
		Agent: string(agent.Claude), Fence: &spec.Fence{Tools: catalog.PresetGitHubTasks},
		Fallbacks: []spec.ProfileRef{
			{Agent: codex, Profile: "default"}, {Agent: codex, Profile: "work"}, {Agent: catalog.Inherit}, {Profile: "work"},
		},
		LaunchFor: []spec.LaunchRow{{Target: "acme-org", Agent: catalog.Inherit}, {Target: "acme-labs"}},
	}

	warnings := fenceWarnings(agentSpec(step))
	require.Len(t, warnings, 1, "Codex once, and neither the launch defaults' agent nor none")
	assert.Equal(t, "[0].agent.fallbacks", warnings[0].Field)
}

func TestABudgetCodexCantKeepIsWarnedOf(t *testing.T) {
	t.Parallel()

	usd, tokens := 3.0, int64(500_000)

	warnings := fenceWarnings(agentSpec(spec.AgentStep{Agent: "codex"}))
	require.Len(t, warnings, 1)
	assert.Equal(t, "[0].agent.fence.budget", warnings[0].Field)
	assert.Contains(t, warnings[0].Message, "Budget: not available for Codex", "the time limit still applies")

	warnings = fenceWarnings(agentSpec(spec.AgentStep{Agent: "codex", Fence: &spec.Fence{Budget: &spec.Budget{USD: &usd}}}))
	require.Len(t, warnings, 1)

	warnings = fenceWarnings(agentSpec(spec.AgentStep{Agent: "codex", Fence: &spec.Fence{Budget: &spec.Budget{Tokens: &tokens}}}))
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].Message, "only as a run ends")

	assert.Empty(t, fenceWarnings(agentSpec(spec.AgentStep{Agent: "claude"})), "Claude Code keeps its own budget")
	assert.Empty(t, fenceWarnings(agentSpec(spec.AgentStep{})), "Inherit's agent is the item's repository's")
}

func TestABudgetIsSaidForWhatEachAgentCanKeep(t *testing.T) {
	t.Parallel()

	keeps := agent.Headless{Background: true, Budget: true, Usage: true, UsageLive: true}

	assert.Empty(t, budgetWords("Agent", agent.Headless{}, false), "an agent that can't run in the background")
	assert.Empty(t, budgetWords("Agent", keeps, false), "one that keeps a budget in dollars of its own")
	assert.Empty(t, budgetWords("Agent", keeps, true), "one that reports its tokens as it goes")

	noDollars := keeps
	noDollars.Budget = false
	assert.Equal(t, "Budget: not available for Agent in dollars, which it keeps no budget of; the time limit still "+
		"applies, and its runs stop when the app quits.", budgetWords("Agent", noDollars, false))

	noTokens := keeps
	noTokens.Usage = false
	assert.Equal(t, "Budget: Agent reports no tokens, so a budget in tokens can't stop it; the time limit still applies.",
		budgetWords("Agent", noTokens, true))

	atTheEnd := keeps
	atTheEnd.UsageLive = false
	assert.Equal(t, "Budget: Agent reports its tokens only as a run ends, so a budget in tokens is checked then; the "+
		"time limit bounds the run.", budgetWords("Agent", atTheEnd, true))

	_, ok := budgetWarning(spec.AgentStep{Agent: "no-such-agent"}, 0)
	assert.False(t, ok, "an agent this build doesn't know has no budget to warn of")
}
