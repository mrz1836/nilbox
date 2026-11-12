package scriptrun

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/uierr"
)

// errTerminalRefused is a terminal that refused to open.
var errTerminalRefused = errors.New("terminal refused")

func TestScriptNames(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "run-42", ScriptName(42))
	assert.Equal(t, FolderScript, ScriptName(0), "no run owns a terminal opened in a clone")
}

func TestOpenFolder(t *testing.T) {
	t.Parallel()

	term := newFakeTerminal(t)
	require.NoError(t, OpenFolder(t.Context(), term, "/work/sdk"))
	assert.Equal(t, []string{FolderScript + " /work/sdk"}, term.opensIn(), "the clone, with nothing run there")

	term.err = errTerminalRefused
	err := OpenFolder(t.Context(), term, "/work/sdk")
	require.ErrorIs(t, err, errTerminalRefused)
	require.EqualError(t, err, "open the terminal in the clone: terminal refused")

	err = OpenFolder(t.Context(), nil, "/work/sdk")
	require.ErrorIs(t, err, ErrNoFolderTerminal)
	assert.Equal(t, "Your terminal can't be opened here.", uierr.Message(err))
}
