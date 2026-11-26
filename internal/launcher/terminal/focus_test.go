package terminal

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testTTY is a terminal device as macOS names a tab's: the tests' tty.
const testTTY = "/dev/ttys004"

func TestFocusScriptsFindTheTabByItsTTY(t *testing.T) {
	t.Parallel()

	for name, script := range map[string]string{
		"iTerm2":       itermFocusScript(testTTY),
		"Terminal.app": terminalAppFocusScript(testTTY),
	} {
		assert.Contains(t, script, `is "`+testTTY+`" then`, name)
		assert.Contains(t, script, "activate", name)
		assert.Contains(t, script, `return "found"`, name)
		assert.True(t, strings.HasSuffix(script, "return \"missing\"\n"), name)
		assert.NotContains(t, script, "write text", "%s only selects a tab: it types nothing", name)
		assert.NotContains(t, script, "do script", "%s only selects a tab: it runs nothing", name)
	}

	assert.Contains(t, itermFocusScript(testTTY), "tell s to select")
	assert.Contains(t, terminalAppFocusScript(testTTY), "set selected of t to true")
}

func TestValidateTTY(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{testTTY, "/dev/pts/3", "/dev/tty1"} {
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
	tab := Tab{TTY: testTTY, PID: 4100}

	require.ErrorIs(t, r.Focus(t.Context(), "plain", tab), ErrNoFocus)
	require.ErrorIs(t, r.Focus(t.Context(), "other", tab), ErrUnknownTerminal)
	require.ErrorIs(t, NewRegistry().Focus(t.Context(), ITerm2, tab), ErrUnsupportedOS)
}

// tabDriver is a Focuser that records the tab it was asked to bring forward.
type tabDriver struct {
	plainDriver

	asked *Tab
}

func (d tabDriver) Focus(_ context.Context, tab Tab) error {
	*d.asked = tab

	return nil
}

func TestRegistryFocusHandsTheDriverTheTab(t *testing.T) {
	t.Parallel()

	var asked Tab

	tab := Tab{TTY: testTTY, PID: 4100}
	require.NoError(t, NewRegistry(tabDriver{asked: &asked}).Focus(t.Context(), "plain", tab))
	assert.Equal(t, tab, asked, "its tty and its process both")
}
