package agent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
)

func TestArgsAccessMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		access agent.Access
		claude []string
		codex  []string
	}{
		{name: "default", access: agent.Access{}, claude: nil, codex: []string{"--"}},
		{
			name: "full", access: agent.Access{Kind: agent.AccessFull},
			claude: []string{"--dangerously-skip-permissions"},
			codex:  []string{"--dangerously-bypass-approvals-and-sandbox", "--"},
		},
		{
			name: "plan", access: agent.Access{Kind: agent.AccessPlan},
			claude: []string{"--permission-mode", "plan"},
			codex:  []string{"--sandbox", "read-only", "--ask-for-approval", "on-request", "--"},
		},
		{
			name: "plan allowing full", access: agent.Access{Kind: agent.AccessPlanAllowFull},
			claude: []string{"--permission-mode", "plan", "--allow-dangerously-skip-permissions"},
			codex:  []string{"--sandbox", "read-only", "--ask-for-approval", "on-request", "--"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := agent.Args(agent.Claude, agent.Options{Access: tt.access})
			require.NoError(t, err)
			assert.Equal(t, tt.claude, got)

			got, err = agent.Args(agent.Codex, agent.Options{Access: tt.access})
			require.NoError(t, err)
			assert.Equal(t, tt.codex, got)
		})
	}
}

func TestArgsModes(t *testing.T) {
	t.Parallel()

	got, err := agent.Args(agent.Claude, agent.Options{Access: agent.Access{Kind: agent.AccessMode, Mode: "acceptEdits"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"--permission-mode", "acceptEdits"}, got)

	got, err = agent.Args(agent.Codex, agent.Options{Access: agent.Access{Kind: agent.AccessMode, Mode: "workspace-write"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"--sandbox", "workspace-write", "--"}, got)

	_, err = agent.Args(agent.Codex, agent.Options{Access: agent.Access{Kind: agent.AccessMode, Mode: "acceptEdits"}})
	require.ErrorIs(t, err, agent.ErrInvalidPermissionMode)

	_, err = agent.Args(agent.Claude, agent.Options{Access: agent.Access{Kind: agent.AccessMode}})
	require.ErrorIs(t, err, agent.ErrInvalidPermissionMode)

	_, err = agent.Args(agent.Claude, agent.Options{Access: agent.Access{Kind: "everything"}})
	require.ErrorIs(t, err, agent.ErrInvalidAccess)
}

func TestArgsOrder(t *testing.T) {
	t.Parallel()

	o := agent.Options{
		Model: "opus", Effort: "high", Access: agent.Access{Kind: agent.AccessFull},
		AddDirs: []string{"/srv/a", "/srv/b c"}, Extra: []string{flagAdvisor, "fable"}, SessionName: "Review · repo#7",
	}

	got, err := agent.Args(agent.Claude, o)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"--model", "opus", "--effort", "high", "--dangerously-skip-permissions",
		"--add-dir=/srv/a", "--add-dir=/srv/b c", flagAdvisor, "fable", "--name=Review · repo#7",
	}, got)

	o.Model, o.Extra = "gpt-6-sol", []string{"--search"}

	got, err = agent.Args(agent.Codex, o)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"--model", "gpt-6-sol", "--config", `model_reasoning_effort="high"`,
		"--dangerously-bypass-approvals-and-sandbox", "--add-dir=/srv/a", "--add-dir=/srv/b c", "--search", "--",
	}, got, "Codex names no session and ends its flags with --")
}

func TestArgsRefusesWhatTheAgentWouldMisread(t *testing.T) {
	t.Parallel()

	_, err := agent.Args(agent.Claude, agent.Options{Model: "-x"})
	require.ErrorIs(t, err, agent.ErrInvalidModel)

	_, err = agent.Args(agent.Claude, agent.Options{Effort: "ultra"})
	require.ErrorIs(t, err, agent.ErrInvalidEffort, "Claude has no ultra effort")

	got, err := agent.Args(agent.Codex, agent.Options{Effort: "ultra"})
	require.NoError(t, err)
	assert.Equal(t, []string{"--config", `model_reasoning_effort="ultra"`, "--"}, got)

	_, err = agent.Args(unknownAgent, agent.Options{})
	require.ErrorIs(t, err, agent.ErrUnknownAgent)
}
