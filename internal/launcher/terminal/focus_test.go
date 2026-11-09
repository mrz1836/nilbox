package terminal

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFocusScriptsFindTheTabByItsTTY(t *testing.T) {
	t.Parallel()

	for name, script := range map[string]string{
		"iTerm2":       itermFocusScript("/dev/ttys004"),
		"Terminal.app": terminalAppFocusScript("/dev/ttys004"),
	} {
		assert.Contains(t, script, `is "/dev/ttys004" then`, name)
		assert.Contains(t, script, "activate", name)
		assert.Contains(t, script, `return "found"`, name)
		assert.True(t, strings.HasSuffix(script, "return \"missing\"\n"), name)
		assert.NotContains(t, script, "write text", "%s only selects a tab: it types nothing", name)
		assert.NotContains(t, script, "do script", "%s only selects a tab: it runs nothing", name)
	}

	assert.Contains(t, itermFocusScript("/dev/ttys004"), "tell s to select")
	assert.Contains(t, terminalAppFocusScript("/dev/ttys004"), "set selected of t to true")
}

func TestValidateTTY(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"/dev/ttys004", "/dev/pts/3", "/dev/tty1"} {
		require.NoError(t, validateTTY(ok), ok)
	}

	for _, bad := range []string{"", "ttys004", "/dev/", "/dev/ttys004\"", "/dev/../etc/passwd", "/dev/tty s", "/tmp/x"} {
		require.ErrorIs(t, validateTTY(bad), ErrInvalidTTY, bad)
	}
}

func TestFocusResult(t *testing.T) {
	t.Parallel()

	require.NoError(t, focusResult("iTerm2", []byte("found\n")), "the tab was found")

	for _, out := range []string{"missing\n", "", "found it"} {
		err := focusResult("iTerm2", []byte(out))
		require.ErrorIs(t, err, ErrNoSuchTab, "%q", out)
		assert.Contains(t, err.Error(), "iTerm2", "the error names the terminal")
	}
}

// plainDriver is a Driver that cannot focus.
type plainDriver struct{}

func (plainDriver) ID() ID                             { return "plain" }
func (plainDriver) Open(context.Context, string) error { return nil }

func TestRegistryFocusNeedsAFocuser(t *testing.T) {
	t.Parallel()

	r := NewRegistry(plainDriver{})

	require.ErrorIs(t, r.Focus(t.Context(), "plain", "/dev/ttys004"), ErrNoFocus)
	require.ErrorIs(t, r.Focus(t.Context(), "other", "/dev/ttys004"), ErrUnknownTerminal)
	require.ErrorIs(t, NewRegistry().Focus(t.Context(), ITerm2, "/dev/ttys004"), ErrUnsupportedOS)
}
