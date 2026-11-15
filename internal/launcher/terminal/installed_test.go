package terminal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestInstalledFindsITerm2InEitherFolder(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(&fakeDriver{id: TerminalApp}, &fakeDriver{id: ITerm2})

	system, user := t.TempDir(), t.TempDir()

	assert.Equal(t, []Choice{
		{ID: ITerm2, Name: "iTerm2", Installed: false},
		{ID: TerminalApp, Name: "Terminal", Installed: true},
	}, registry.Installed(system, user), "Terminal comes with the system; iTerm2 isn't there")

	require.NoError(t, os.Mkdir(filepath.Join(user, itermBundle), 0o700))

	assert.Equal(t, []Choice{
		{ID: ITerm2, Name: "iTerm2", Installed: true},
		{ID: TerminalApp, Name: "Terminal", Installed: true},
	}, registry.Installed("", system, user), "a bundle in the user's own Applications counts")
}

func TestInstalledWantsTheBundleNotAFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testutil.WriteFile(t, filepath.Join(dir, itermBundle), "not an app")

	choices := NewRegistry(&fakeDriver{id: ITerm2}).Installed(dir)
	assert.Equal(t, []Choice{{ID: ITerm2, Name: "iTerm2"}}, choices)
}

func TestInstalledWithNoDriversOffersNone(t *testing.T) {
	t.Parallel()

	choices := NewRegistry().Installed(t.TempDir())
	assert.NotNil(t, choices)
	assert.Empty(t, choices)
}

func TestInstalledKnowsNoOtherTerminal(t *testing.T) {
	t.Parallel()

	choices := NewRegistry(&fakeDriver{id: "kitty"}).Installed(t.TempDir())
	assert.Equal(t, []Choice{{ID: "kitty"}}, choices)
}
