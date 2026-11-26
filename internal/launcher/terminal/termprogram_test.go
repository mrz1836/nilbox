package terminal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// termPrograms are the values each terminal sets TERM_PROGRAM to in its shells.
func termPrograms() map[string]ID {
	return map[string]ID{
		"WarpTerminal":   Warp,
		"iTerm.app":      ITerm2,
		"Apple_Terminal": TerminalApp,
	}
}

func TestFromTermProgram(t *testing.T) {
	t.Parallel()

	for value, want := range termPrograms() {
		got, ok := FromTermProgram(value)
		assert.True(t, ok, value)
		assert.Equal(t, want, got, value)
		assert.True(t, Valid(got), "%s names one of the terminals", value)
	}

	for name, value := range map[string]string{
		"empty":                      "",
		"tmux's own":                 "tmux",
		"another terminal":           "vscode",
		"a lowercase name":           "warpterminal",
		"an uppercase name":          "ITERM.APP",
		"a trailing space":           "WarpTerminal ",
		"a leading space":            " Apple_Terminal",
		"a trailing newline":         "iTerm.app\n",
		"a settings ID, not a name":  "iterm2",
		"Warp's name, not its value": "Warp",
	} {
		got, ok := FromTermProgram(value)
		assert.False(t, ok, name)
		assert.Empty(t, got, name)
	}
}

func TestEveryTerminalHasATermProgram(t *testing.T) {
	t.Parallel()

	named := map[ID]bool{}
	for _, id := range termPrograms() {
		named[id] = true
	}

	for _, id := range All() {
		assert.True(t, named[id], "%s's TERM_PROGRAM is known, so a session in it is found there", id)
	}
}
