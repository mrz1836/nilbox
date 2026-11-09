package agent_test

import (
	"strings"
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

func TestArgsStartAndResumeSessions(t *testing.T) {
	t.Parallel()

	const id = "0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70"

	got, err := agent.Args(agent.Claude, agent.Options{Model: "haiku", SessionName: "Review · repo#7", SessionID: id})
	require.NoError(t, err)
	assert.Equal(t, []string{"--model", "haiku", "--name=Review · repo#7", "--session-id=" + id}, got,
		"a new Claude session is named and carries the app's ID last")

	got, err = agent.Args(agent.Claude, agent.Options{Model: "haiku", SessionName: "Review · repo#7", ResumeID: id})
	require.NoError(t, err)
	assert.Equal(t, []string{"--model", "haiku", "--resume=" + id}, got, "a resumed session keeps its name and ID")

	got, err = agent.Args(agent.Codex, agent.Options{Model: "gpt-6-sol", SessionID: id})
	require.NoError(t, err)
	assert.Equal(t, []string{"--model", "gpt-6-sol", "--"}, got, "Codex takes no session ID")

	got, err = agent.Args(agent.Codex, agent.Options{
		Model: "gpt-6-sol", Access: agent.Access{Kind: agent.AccessPlan}, Extra: []string{"--search"}, ResumeID: id,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"resume", "--model", "gpt-6-sol", "--sandbox", "read-only", "--ask-for-approval", "on-request", "--search", "--", id,
	}, got, "Codex resumes with its command first and the session's ID where the prompt would go")
}

func TestArgsRefusesSessionIDsThatAreNot(t *testing.T) {
	t.Parallel()

	for name, o := range map[string]agent.Options{
		"a flag":                 {SessionID: "--print"},
		"a space":                {ResumeID: "abc def"},
		"a leading dash":         {ResumeID: "-abc"},
		"too long":               {SessionID: strings.Repeat("a", 129)},
		"both started and kept":  {SessionID: "abc", ResumeID: "def"},
		"a control character":    {SessionID: "abc\n"},
		"a character outside ID": {ResumeID: "abc/def"},
	} {
		for _, id := range agent.IDs() {
			_, err := agent.Args(id, o)
			require.ErrorIs(t, err, agent.ErrInvalidSessionID, "%s for %s", name, id)
		}
	}

	assert.True(t, agent.ValidSessionID("019a1bc2-0000-7000-8000-00000000abcd"))
	assert.True(t, agent.ValidSessionID("a_b-c"))
	assert.False(t, agent.ValidSessionID(""))
}

func TestArgsFastMode(t *testing.T) {
	t.Parallel()

	got, err := agent.Args(agent.Codex, agent.Options{Effort: "high", Fast: true})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"--config", `model_reasoning_effort="high"`, "--config", `service_tier="fast"`, "--",
	}, got, "Codex starts in Fast mode through a config override")

	got, err = agent.Args(agent.Claude, agent.Options{Fast: true})
	require.NoError(t, err)
	assert.Empty(t, got, "Claude has no Fast mode to start in")

	codex, err := agent.Lookup(agent.Codex)
	require.NoError(t, err)
	assert.True(t, codex.FastMode)
	assert.True(t, codex.ConfigOverrides)

	claude, err := agent.Lookup(agent.Claude)
	require.NoError(t, err)
	assert.False(t, claude.FastMode)
	assert.False(t, claude.ConfigOverrides)
}
