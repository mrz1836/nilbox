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
	for _, effort := range append([]string{""}, agent.EffortLevels()...) {
		require.NoError(t, agent.ValidateEffort(effort), effort)
	}

	err := agent.ValidateEffort("extreme")
	require.ErrorIs(t, err, agent.ErrInvalidEffort)
	assert.Contains(t, err.Error(), "low, medium, high, xhigh, max")
}

func TestValidateModel(t *testing.T) {
	for _, model := range []string{"", "opus", "sonnet", "claude-fable-5-1", "claude-opus-5-5[1m]"} {
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
			require.ErrorIs(t, agent.ValidateModel(model), agent.ErrInvalidModel)
		})
	}
}

func TestCheckExtra(t *testing.T) {
	require.NoError(t, agent.CheckExtra(nil))
	require.NoError(t, agent.CheckExtra([]string{"--advisor", "fable", "--add-dir=/srv/shared", "--agent", "reviewer"}))

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
		{name: "empty", args: []string{"--advisor", ""}, want: agent.ErrEmptyFlag},
		{name: "NUL", args: []string{"a\x00b"}, want: agent.ErrNULByte},
		{name: "tilde", args: []string{"--add-dir", "~/shared"}, want: agent.ErrTilde, text: "write the full path"},
		{name: "tilde after =", args: []string{"--add-dir=~/shared"}, want: agent.ErrTilde, text: "write the full path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := agent.CheckExtra(tt.args)
			require.ErrorIs(t, err, tt.want)
			assert.Contains(t, err.Error(), tt.text)
		})
	}
}

func TestReservedFlagErrorAs(t *testing.T) {
	var reserved *agent.ReservedFlagError

	require.ErrorAs(t, agent.CheckExtra([]string{"--model=opus"}), &reserved)
	assert.Equal(t, "--model", reserved.Flag)
}

func TestSplit(t *testing.T) {
	tests := []struct {
		line string
		want []string
	}{
		{line: "", want: []string{}},
		{line: "   ", want: []string{}},
		{line: "--advisor fable", want: []string{"--advisor", "fable"}},
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
			got, err := agent.Split(tt.line)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSplitUnterminated(t *testing.T) {
	for _, line := range []string{`'open`, `"open`, `a "b\"`} {
		_, err := agent.Split(line)
		require.ErrorIs(t, err, agent.ErrUnterminatedQuote, line)
	}
}

func TestJoin(t *testing.T) {
	assert.Empty(t, agent.Join(nil))
	assert.Equal(t, "--advisor fable --add-dir=/srv/x", agent.Join([]string{"--advisor", "fable", "--add-dir=/srv/x"}))
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
