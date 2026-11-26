package launcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// writeRun writes a followed run's script into the harness's launch folder.
func writeRun(t *testing.T, h *launchHarness) string {
	t.Helper()

	path, err := terminal.WriteScript(h.paths.Launch, "run-42", terminal.Script{
		Dir: h.clone, Binary: filepath.Join(h.home, "bin", "magex"), Args: []string{"version:bump"},
		Record: &terminal.Recording{
			Log:      filepath.Join(h.paths.Root, "script-runs", "42.log"),
			PIDFile:  filepath.Join(h.paths.Root, "script-runs", "42.pid"),
			ExitFile: filepath.Join(h.paths.Root, "script-runs", "42.exit"),
		},
	}, fixedNow(), nil, nil)
	require.NoError(t, err)

	return path
}

func TestCommandOpenerOpensTheScriptInTheChosenTerminal(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	path := writeRun(t, h)

	require.NoError(t, NewCommandOpener(h.service()).Open(t.Context(), path))
	assert.Equal(t, []string{path}, h.iterm.opened, "iTerm2 is the default terminal")
	assert.Empty(t, h.terminal.opened)

	require.NoError(t, testutil.ErrOf(h.settings.SetTerminal(t.Context(), settings.TerminalApp)))

	require.NoError(t, NewCommandOpener(h.service()).Open(t.Context(), path))
	assert.Equal(t, []string{path}, h.terminal.opened, "the terminal chosen in Settings")
	assert.Len(t, h.iterm.opened, 1)

	res, _ := h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	require.NoError(t, NewCommandOpener(h.service()).Open(t.Context(), res.ScriptPath), "a launch's own script opens too")
	assert.Equal(t, []string{path, res.ScriptPath, res.ScriptPath}, h.terminal.opened)
}

func TestCommandOpenerFailures(t *testing.T) {
	t.Parallel()

	denied := fmt.Errorf("%w (iTerm2)", terminal.ErrAutomationDenied)

	tests := []struct {
		name    string
		mutate  func(h *launchHarness)
		wantErr error
		wantMsg string
	}{
		{
			name:    "macOS refuses Automation",
			mutate:  func(h *launchHarness) { h.iterm.err = denied },
			wantErr: terminal.ErrAutomationDenied,
			wantMsg: denied.Error(),
		},
		{
			name:    "the terminal fails to open",
			mutate:  func(h *launchHarness) { h.iterm.err = errFakeOpen },
			wantErr: errFakeOpen,
		},
		{
			name:    "no terminal support on this OS",
			mutate:  func(h *launchHarness) { h.cfg.Registry = terminal.NewRegistry() },
			wantErr: terminal.ErrUnsupportedOS,
		},
		{
			name:    "unreadable settings",
			mutate:  func(h *launchHarness) { h.cfg.Settings = failingSettings{} },
			wantErr: errFakeSettings,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newLaunchHarness(t)
			path := writeRun(t, h)
			tt.mutate(h)

			err := NewCommandOpener(h.service()).Open(t.Context(), path)
			require.ErrorIs(t, err, tt.wantErr)

			if tt.wantMsg != "" {
				require.EqualError(t, err, tt.wantMsg, "the refusal keeps the driver's words, as a launch's does")
			}
		})
	}
}

func TestCommandOpenerOpensOnlyTheAppsScripts(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	path := writeRun(t, h)

	elsewhere := filepath.Join(t.TempDir(), filepath.Base(path))
	require.NoError(t, os.WriteFile(elsewhere, nil, 0o600))

	res, _ := h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	h.iterm.opened = nil

	for name, refused := range map[string]string{
		"a script elsewhere": elsewhere,
		"a prompt file":      strings.TrimSuffix(res.ScriptPath, ".command") + terminal.PromptSuffix,
		"a relative path":    filepath.Base(path),
		"a missing script":   filepath.Join(h.paths.Launch, "run-9-20260314T150926Z-00000000.command"),
		"a program":          "/bin/sh",
	} {
		err := NewCommandOpener(h.service()).Open(t.Context(), refused)
		require.ErrorIs(t, err, terminal.ErrInvalidScriptPath, name)
	}

	assert.Empty(t, h.iterm.opened, "nothing is opened")
}

// folderDriver is a terminal driver that opens a folder itself, as Warp's does, and
// records the folders and the scripts it was asked to open.
type folderDriver struct {
	fakeDriver

	folderErr error
	folders   []string
}

func (d *folderDriver) OpenFolder(_ context.Context, dir string) error {
	d.folders = append(d.folders, dir)

	return d.folderErr
}

// withWarp registers a Warp that opens a folder itself beside the harness's iTerm2 and
// Terminal.app, and chooses the terminal chosen in the settings.
func withWarp(t *testing.T, h *launchHarness, chosen terminal.ID) *folderDriver {
	t.Helper()

	warp := &folderDriver{id: terminal.Warp}
	h.cfg.Registry = terminal.NewRegistry(h.iterm, warp, h.terminal)

	require.NoError(t, testutil.ErrOf(h.settings.SetTerminal(t.Context(), chosen)))

	return warp
}

func TestCommandOpenerOpensAFolderInATerminalThatCan(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	warp := withWarp(t, h, settings.TerminalWarp)

	opened, err := NewCommandOpener(h.service()).OpenFolder(t.Context(), h.clone)
	require.NoError(t, err)
	assert.True(t, opened)
	assert.Equal(t, []string{h.clone}, warp.folders, "the folder itself, in the terminal chosen")
	assert.Empty(t, warp.opened, "no script")
	assert.Empty(t, h.iterm.opened)
	assert.Empty(t, h.terminal.opened)
}

func TestCommandOpenerLeavesAFolderToAScriptWhereTheTerminalCant(t *testing.T) {
	t.Parallel()

	for _, id := range []terminal.ID{settings.TerminalITerm2, settings.TerminalApp} {
		t.Run(string(id), func(t *testing.T) {
			t.Parallel()

			h := newLaunchHarness(t)
			warp := withWarp(t, h, id)

			opened, err := NewCommandOpener(h.service()).OpenFolder(t.Context(), h.clone)
			require.NoError(t, err, "a terminal that can't is no failure")
			assert.False(t, opened, "the caller opens a script instead")
			assert.Empty(t, h.iterm.opened, "nothing is opened")
			assert.Empty(t, h.terminal.opened)
			assert.Empty(t, warp.folders, "only the terminal chosen is asked")
		})
	}
}

func TestCommandOpenerRefusesFoldersItCantOpen(t *testing.T) {
	t.Parallel()

	for _, id := range []terminal.ID{settings.TerminalWarp, settings.TerminalITerm2} {
		t.Run(string(id), func(t *testing.T) {
			t.Parallel()

			h := newLaunchHarness(t)
			warp := withWarp(t, h, id)

			file := filepath.Join(h.clone, "README.md")
			require.NoError(t, os.WriteFile(file, nil, 0o600))

			gone := filepath.Join(h.root, "gone")

			for name, dir := range map[string]string{
				"a relative folder":   filepath.Join("projects", exampleRepo),
				"a control character": h.clone + "\n/bin/sh",
				"a file":              file,
				"a missing folder":    gone,
			} {
				opened, err := NewCommandOpener(h.service()).OpenFolder(t.Context(), dir)
				require.ErrorIs(t, err, terminal.ErrInvalidFolder, name)
				assert.False(t, opened, name)
			}

			_, err := NewCommandOpener(h.service()).OpenFolder(t.Context(), gone)
			assert.Equal(t, "~/projects/gone isn't there anymore.", uierr.Message(err),
				"a folder gone since its page listed it is worded for the window")

			assert.Empty(t, warp.folders, "nothing is opened")
			assert.Empty(t, warp.opened)
			assert.Empty(t, h.iterm.opened)
		})
	}
}

func TestCommandOpenerOpenFolderFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(h *launchHarness, warp *folderDriver)
		wantErr error
	}{
		{
			name:    "the terminal fails to open",
			mutate:  func(_ *launchHarness, warp *folderDriver) { warp.folderErr = errFakeOpen },
			wantErr: errFakeOpen,
		},
		{
			name:    "the terminal chosen has no driver",
			mutate:  func(h *launchHarness, _ *folderDriver) { h.cfg.Registry = terminal.NewRegistry(h.iterm, h.terminal) },
			wantErr: terminal.ErrUnknownTerminal,
		},
		{
			name:    "no terminal support on this OS",
			mutate:  func(h *launchHarness, _ *folderDriver) { h.cfg.Registry = terminal.NewRegistry() },
			wantErr: terminal.ErrUnsupportedOS,
		},
		{
			name:    "unreadable settings",
			mutate:  func(h *launchHarness, _ *folderDriver) { h.cfg.Settings = failingSettings{} },
			wantErr: errFakeSettings,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newLaunchHarness(t)
			warp := withWarp(t, h, settings.TerminalWarp)
			tt.mutate(h, warp)

			opened, err := NewCommandOpener(h.service()).OpenFolder(t.Context(), h.clone)
			require.ErrorIs(t, err, tt.wantErr)
			assert.False(t, opened, "a failure is never an open, so the caller opens nothing more")
		})
	}
}
