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
		"resume", id, "--model", "gpt-6-sol", "--sandbox", "read-only", "--ask-for-approval", "on-request", "--search", "--",
	}, got, "Codex resumes with its command first and the session's ID right after it")
}

func TestArgsKeepACodexResumeOnItsSession(t *testing.T) {
	t.Parallel()

	const id = "01a1bec5-d48c-7092-9332-fb198342b55d"

	got, err := agent.Args(agent.Codex, agent.Options{Extra: []string{"--search", "stray"}, ResumeID: id})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(got), 2)
	assert.Equal(t, []string{"resume", id}, got[:2],
		"the session's ID is the first word after the command, so a word in the extra arguments can't take its place")
	assert.Equal(t, "--", got[len(got)-1], "and nothing follows the end of the flags to be read as a prompt")
	assert.Equal(t, 1, strings.Count(strings.Join(got, "\n"), id))
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
		"a session name":         {ResumeID: "review-pr-7"},
		"a word":                 {SessionID: "a_b-c"},
		"no dashes":              {ResumeID: "0f8c1e2a5b4d4c3e9a1f2b3c4d5e6f70"},
		"braces":                 {ResumeID: "{0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70}"},
		"a URN":                  {ResumeID: "urn:uuid:0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70"},
		"a trailing newline":     {ResumeID: "0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70\n"},
		"no version":             {ResumeID: "0f8c1e2a-5b4d-0c3e-9a1f-2b3c4d5e6f70"},
		"no variant":             {SessionID: "0f8c1e2a-5b4d-4c3e-7a1f-2b3c4d5e6f70"},
	} {
		for _, id := range agent.IDs() {
			_, err := agent.Args(id, o)
			require.ErrorIs(t, err, agent.ErrInvalidSessionID, "%s for %s", name, id)
		}
	}
}

func TestValidSessionID(t *testing.T) {
	t.Parallel()

	for _, id := range []string{
		"0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70", // a Claude session, a version 4 UUID
		"11111111-2222-4333-8444-555555555555",
		"01a1bec5-d48c-7092-9332-fb198342b55d", // a Codex thread, a version 7 UUID
		"019a1bc2-0000-7000-8000-00000000abcd",
		"0F8C1E2A-5B4D-4C3E-9A1F-2B3C4D5E6F70",
	} {
		assert.True(t, agent.ValidSessionID(id), id)
	}

	for _, id := range []string{"", "a_b-c", "review-pr-7", "00000000-0000-0000-0000-000000000000"} {
		assert.False(t, agent.ValidSessionID(id), id)
	}
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
