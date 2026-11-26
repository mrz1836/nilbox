package terminal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// everyTerminal is a registry with a driver for each terminal, registered out of order.
func everyTerminal() Registry {
	return NewRegistry(&fakeDriver{id: TerminalApp}, &fakeDriver{id: Warp}, &fakeDriver{id: ITerm2})
}

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

func TestInstalledListsTheTerminalsInTheOrderAChoicePrefers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		bundles []string
		want    []Choice
	}{
		{
			name: "neither",
			want: []Choice{
				{ID: ITerm2, Name: "iTerm2"},
				{ID: Warp, Name: "Warp"},
				{ID: TerminalApp, Name: "Terminal", Installed: true},
			},
		},
		{
			name:    "Warp only",
			bundles: []string{warpBundle},
			want: []Choice{
				{ID: ITerm2, Name: "iTerm2"},
				{ID: Warp, Name: "Warp", Installed: true},
				{ID: TerminalApp, Name: "Terminal", Installed: true},
			},
		},
		{
			name:    "both",
			bundles: []string{itermBundle, warpBundle},
			want: []Choice{
				{ID: ITerm2, Name: "iTerm2", Installed: true},
				{ID: Warp, Name: "Warp", Installed: true},
				{ID: TerminalApp, Name: "Terminal", Installed: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			for _, b := range tt.bundles {
				require.NoError(t, os.Mkdir(filepath.Join(dir, b), 0o700))
			}

			assert.Equal(t, tt.want, everyTerminal().Installed(dir))
		})
	}
}

func TestInstalledFindsWarpInTheUsersOwnApplications(t *testing.T) {
	t.Parallel()

	system, user := t.TempDir(), t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(user, warpBundle), 0o700))

	choices := NewRegistry(&fakeDriver{id: Warp}).Installed(system, user)
	assert.Equal(t, []Choice{{ID: Warp, Name: "Warp", Installed: true}}, choices)
}

func TestInstalledWantsTheBundleNotAFile(t *testing.T) {
	t.Parallel()

	for id, bundle := range map[ID]string{ITerm2: itermBundle, Warp: warpBundle} {
		dir := t.TempDir()
		testutil.WriteFile(t, filepath.Join(dir, bundle), "not an app")

		choices := NewRegistry(&fakeDriver{id: id}).Installed(dir)
		assert.Equal(t, []Choice{{ID: id, Name: Name(id)}}, choices, id)
	}
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

func TestInstalledListsAnUnknownTerminalLast(t *testing.T) {
	t.Parallel()

	choices := NewRegistry(&fakeDriver{id: "alacritty"}, &fakeDriver{id: TerminalApp}, &fakeDriver{id: "kitty"},
		&fakeDriver{id: ITerm2}).Installed(t.TempDir())
	assert.Equal(t, []ID{ITerm2, TerminalApp, "alacritty", "kitty"}, choiceIDs(choices))
}

// choiceIDs returns the IDs of choices, in order.
func choiceIDs(choices []Choice) []ID {
	out := make([]ID, 0, len(choices))
	for _, c := range choices {
		out = append(out, c.ID)
	}

	return out
}
