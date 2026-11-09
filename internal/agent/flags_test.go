package agent_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
)

func TestValidateEffort(t *testing.T) {
	t.Parallel()

	for _, id := range agent.IDs() {
		for _, effort := range append([]string{""}, agent.EffortLevels(id)...) {
			require.NoError(t, agent.ValidateEffort(id, effort), effort)
		}
	}

	err := agent.ValidateEffort(agent.Claude, "extreme")
	require.ErrorIs(t, err, agent.ErrInvalidEffort)
	assert.Contains(t, err.Error(), "low, medium, high, xhigh, max")

	require.ErrorIs(t, agent.ValidateEffort(agent.Claude, "ultra"), agent.ErrInvalidEffort)
	require.NoError(t, agent.ValidateEffort(agent.Codex, "ultra"))
	require.ErrorIs(t, agent.ValidateEffort(unknownAgent, "low"), agent.ErrInvalidEffort)
}

func TestValidateEffortAny(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"low", "medium", "high", "xhigh", "max", "ultra"}, agent.AnyEffortLevels())

	for _, effort := range append([]string{""}, agent.AnyEffortLevels()...) {
		require.NoError(t, agent.ValidateEffortAny(effort), effort)
	}

	err := agent.ValidateEffortAny("extreme")
	require.ErrorIs(t, err, agent.ErrInvalidEffort)
	assert.Contains(t, err.Error(), "low, medium, high, xhigh, max, ultra")
}

func TestValidateModel(t *testing.T) {
	t.Parallel()

	for _, model := range []string{"", "opus", "sonnet", "claude-fable-5-1", "claude-opus-5-5[1m]", "gpt-6-sol"} {
		require.NoError(t, agent.ValidateModel(model), model)
	}

	for name, model := range map[string]string{
		"a flag":           "--dangerously-skip-permissions",
		"a space":          "opus 5",
		"a newline":        "opus\n",
		"a control":        "opus\x00",
		"over 100 letters": strings.Repeat("a", 101),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.ErrorIs(t, agent.ValidateModel(model), agent.ErrInvalidModel)
		})
	}
}

func TestCheckExtraClaude(t *testing.T) {
	t.Parallel()

	require.NoError(t, agent.CheckExtra(agent.Claude, nil))
	require.NoError(t, agent.CheckExtra(agent.Claude, []string{flagAdvisor, "fable", "--add-dir=/srv/shared", "--agent", "reviewer"}))

	tests := []struct {
		name string
		args []string
		want error
		text string
	}{
		{name: "model", args: []string{"--model", "opus"}, want: agent.ErrReservedFlag, text: "--model is set by the Model setting"},
		{name: "model with =", args: []string{"--model=opus"}, want: agent.ErrReservedFlag, text: "--model is set"},
		{name: "effort", args: []string{"--effort=max"}, want: agent.ErrReservedFlag, text: "Effort setting"},
		{name: "worktree", args: []string{"--worktree"}, want: agent.ErrReservedFlag, text: "Worktree setting"},
		{name: "short worktree", args: []string{"-w"}, want: agent.ErrReservedFlag, text: "-w is set by the Worktree setting"},
		{name: "skip permissions", args: []string{"--dangerously-skip-permissions"}, want: agent.ErrReservedFlag, text: "permissions come from the profile"},
		{name: "allow skip", args: []string{"--allow-dangerously-skip-permissions"}, want: agent.ErrReservedFlag, text: "permissions"},
		{name: "permission mode", args: []string{"--permission-mode=plan"}, want: agent.ErrReservedFlag, text: "--permission-mode can't be added"},
		{name: "name", args: []string{"-n", "x"}, want: agent.ErrReservedFlag, text: "named for you"},
		{name: "print", args: []string{"--print"}, want: agent.ErrReservedFlag, text: "run in your terminal"},
		{name: "short print", args: []string{"-p"}, want: agent.ErrReservedFlag},
		{name: "background", args: []string{"--bg"}, want: agent.ErrReservedFlag},
		{name: "end of flags", args: []string{"--"}, want: agent.ErrEndOfFlags},
		{name: "empty", args: []string{flagAdvisor, ""}, want: agent.ErrEmptyFlag},
		{name: "NUL", args: []string{"a\x00b"}, want: agent.ErrNULByte},
		{name: "tilde", args: []string{"--add-dir", "~/shared"}, want: agent.ErrTilde, text: "write the full path"},
		{name: "tilde after =", args: []string{"--add-dir=~/shared"}, want: agent.ErrTilde, text: "write the full path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := agent.CheckExtra(agent.Claude, tt.args)
			require.ErrorIs(t, err, tt.want)
			assert.Contains(t, err.Error(), tt.text)
		})
	}
}

func TestCheckExtraCodex(t *testing.T) {
	t.Parallel()

	require.NoError(t, agent.CheckExtra(agent.Codex, nil))
	require.NoError(t, agent.CheckExtra(agent.Codex, []string{
		"--search", "--add-dir=/srv/shared", "-c", "shell_environment_policy.inherit=all", "--config=model_verbosity=low",
		"-p", "work", "--enable", "web_search", "--no-alt-screen",
	}), "Codex's -p is a config profile, and -c may set what the launch does not")

	tests := []struct {
		name   string
		args   []string
		want   error
		text   string
		access bool
	}{
		{name: "model", args: []string{"--model", "o3"}, want: agent.ErrReservedFlag, text: "--model is set by the Model setting"},
		{name: "short model", args: []string{"-m", "o3"}, want: agent.ErrReservedFlag, text: "-m is set by the Model setting"},
		{name: "attached short model", args: []string{"-mo3"}, want: agent.ErrReservedFlag, text: "Model setting"},
		{name: "sandbox", args: []string{"--sandbox=danger-full-access"}, want: agent.ErrReservedFlag, text: "permissions", access: true},
		{name: "short sandbox", args: []string{"-s", "read-only"}, want: agent.ErrReservedFlag, access: true},
		{name: "attached sandbox", args: []string{"-sdanger-full-access"}, want: agent.ErrReservedFlag, access: true},
		{name: "approvals", args: []string{"-a", "never"}, want: agent.ErrReservedFlag, access: true},
		{name: "bypass", args: []string{"--dangerously-bypass-approvals-and-sandbox"}, want: agent.ErrReservedFlag, access: true},
		{name: "hook trust", args: []string{"--dangerously-bypass-hook-trust"}, want: agent.ErrReservedFlag, access: true},
		{name: "approve for me", args: []string{"--approve-for-me"}, want: agent.ErrReservedFlag, access: true},
		{name: "full auto", args: []string{"--full-auto"}, want: agent.ErrReservedFlag, access: true},
		{name: "yolo", args: []string{"--yolo"}, want: agent.ErrReservedFlag, access: true},
		{name: "worktree", args: []string{"--worktree"}, want: agent.ErrReservedFlag, text: "Worktree setting"},
		{name: "cd", args: []string{"-C", "/srv"}, want: agent.ErrReservedFlag, text: "item's folder"},
		{name: "long cd", args: []string{"--cd=/srv"}, want: agent.ErrReservedFlag},
		{name: "remote", args: []string{"--remote", "ws://host:1"}, want: agent.ErrReservedFlag, text: "terminal"},
		{name: "config model", args: []string{"-c", "model=o3"}, want: agent.ErrReservedFlag, text: "-c model is set by the Model setting"},
		{name: "config effort", args: []string{"--config", "model_reasoning_effort=high"}, want: agent.ErrReservedFlag, text: "--config model_reasoning_effort is set by the Effort setting"},
		{name: "config sandbox", args: []string{"--config=sandbox_mode=danger-full-access"}, want: agent.ErrReservedFlag, text: "--config sandbox_mode can't be added", access: true},
		{name: "config approvals", args: []string{"-capproval_policy=never"}, want: agent.ErrReservedFlag, text: "-c approval_policy", access: true},
		{name: "config approvals with =", args: []string{"-c=approval_policy=never"}, want: agent.ErrReservedFlag, access: true},
		{name: "config spaced key", args: []string{"-c", " sandbox_mode = 'danger-full-access'"}, want: agent.ErrReservedFlag, access: true},
		{name: "config trust", args: []string{"-c", `projects."/srv".trust_level="trusted"`}, want: agent.ErrReservedFlag, text: "trusts the folder"},
		{name: "config projects", args: []string{"-c", "projects={}"}, want: agent.ErrReservedFlag},
		{name: "config sandbox permissions", args: []string{"-c", `sandbox_permissions=["disk-full-read-access"]`}, want: agent.ErrReservedFlag, access: true},
		{name: "config workspace write", args: []string{"-c", "sandbox_workspace_write.network_access=true"}, want: agent.ErrReservedFlag, access: true},
		{name: "a command", args: []string{"exec"}, want: agent.ErrReservedFlag, text: "exec can't be added: it runs another command instead of a session", access: true},
		{name: "a command's alias after a flag", args: []string{"--search", "e"}, want: agent.ErrReservedFlag, access: true},
		{name: "a command after an option's value", args: []string{"--enable", "web_search", "review"}, want: agent.ErrReservedFlag, access: true},
		{name: "end of flags", args: []string{"--search", "--"}, want: agent.ErrEndOfFlags},
		{name: "tilde", args: []string{"--add-dir", "~/shared"}, want: agent.ErrTilde},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := agent.CheckExtra(agent.Codex, tt.args)
			require.ErrorIs(t, err, tt.want)
			assert.Contains(t, err.Error(), tt.text)

			if access := agent.CheckAccess(agent.Codex, tt.args); tt.access {
				require.ErrorIs(t, access, agent.ErrReservedFlag)
			} else {
				require.NoError(t, access)
			}
		})
	}
}

func TestCheckExtraUnknownAgent(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, agent.CheckExtra(unknownAgent, []string{"--x"}), agent.ErrUnknownAgent)
	require.ErrorIs(t, agent.CheckAccess(unknownAgent, []string{"--x"}), agent.ErrUnknownAgent)
}

func TestCheckAccess(t *testing.T) {
	t.Parallel()

	require.NoError(t, agent.CheckAccess(agent.Claude, []string{"--model", "opus", flagAdvisor, "fable"}),
		"only permission switches count")

	err := agent.CheckAccess(agent.Claude, []string{flagAdvisor, "--permission-mode=bypassPermissions"})
	require.ErrorIs(t, err, agent.ErrReservedFlag)

	var reserved *agent.ReservedFlagError

	require.ErrorAs(t, err, &reserved)
	assert.Equal(t, "--permission-mode", reserved.Flag)
	assert.True(t, reserved.Access)

	require.ErrorIs(t, agent.CheckAccess(agent.Claude, []string{"--dangerously-skip-permissions"}), agent.ErrReservedFlag)
	require.NoError(t, agent.CheckAccess(agent.Codex, []string{"-c", "model=o3"}), "a model override is no permission")
	require.ErrorIs(t, agent.CheckAccess(agent.Codex, []string{"-c", "sandbox_mode=danger-full-access"}), agent.ErrReservedFlag)
	require.ErrorIs(t, agent.CheckAccess(agent.Codex, []string{"exec"}), agent.ErrReservedFlag,
		"a command would run without the launch's sandbox")
	require.NoError(t, agent.CheckExtra(agent.Claude, []string{flagAdvisor, "review"}),
		"Claude Code reads its prompt first, so no later word is a command")
}

func TestReservedFlagErrorAs(t *testing.T) {
	t.Parallel()

	var reserved *agent.ReservedFlagError

	require.ErrorAs(t, agent.CheckExtra(agent.Claude, []string{"--model=opus"}), &reserved)
	assert.Equal(t, "--model", reserved.Flag)
	assert.False(t, reserved.Access)
}

func TestSplit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line string
		want []string
	}{
		{line: "", want: []string{}},
		{line: "   ", want: []string{}},
		{line: "--advisor fable", want: []string{flagAdvisor, "fable"}},
		{line: "  --a\t b \n c ", want: []string{"--a", "b", "c"}},
		{line: `--add-dir '/srv/my dir'`, want: []string{"--add-dir", "/srv/my dir"}},
		{line: `--note "say \"hi\" \\ \n"`, want: []string{"--note", `say "hi" \ \n`}},
		{line: `it\'s a\ b`, want: []string{"it's", "a b"}},
		{line: `'it'\''s'`, want: []string{"it's"}},
		{line: `'' x`, want: []string{"", "x"}},
		{line: `trailing\`, want: []string{`trailing\`}},
		{line: `a"b c"d`, want: []string{"ab cd"}},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()

			got, err := agent.Split(tt.line)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSplitUnterminated(t *testing.T) {
	t.Parallel()

	for _, line := range []string{`'open`, `"open`, `a "b\"`} {
		_, err := agent.Split(line)
		require.ErrorIs(t, err, agent.ErrUnterminatedQuote, line)
	}
}

func TestJoin(t *testing.T) {
	t.Parallel()

	assert.Empty(t, agent.Join(nil))
	assert.Equal(t, "--advisor fable --add-dir=/srv/x", agent.Join([]string{flagAdvisor, "fable", "--add-dir=/srv/x"}))
	assert.Equal(t, `--note 'two words' '' 'it'\''s' 'é'`, agent.Join([]string{"--note", "two words", "", "it's", "é"}))
}

func FuzzSplitJoin(f *testing.F) {
	for _, seed := range []string{"--advisor fable", "two words", "it's", "", `a"b`, "tab\there", "new\nline", `back\slash`} {
		f.Add(seed, "--flag")
	}

	f.Fuzz(func(t *testing.T, a, b string) {
		if !utf8.ValidString(a) || !utf8.ValidString(b) {
			t.Skip("flags arrive as JSON text, which is always valid UTF-8")
		}

		args := []string{a, b}

		got, err := agent.Split(agent.Join(args))
		require.NoError(t, err)
		assert.Equal(t, args, got)
	})
}
