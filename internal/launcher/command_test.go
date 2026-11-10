package launcher

import (
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
