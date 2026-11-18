//go:build desktop

package main

import (
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/update"
)

// errNewerSchemaForTest is the error data a newer build saved stops the start with.
var errNewerSchemaForTest = fmt.Errorf("open the database: %w", store.ErrNewerSchema)

// alert is one button of a native alert, as the runtime draws it: its label, and the key it
// takes.
type alert struct {
	label  string
	key    string
	choice desktop.Choice
}

// drawn lists f's alert buttons, clicking each to read what it chooses.
func drawn(t *testing.T, f desktop.Failure) []alert {
	t.Helper()

	var clicked desktop.Button

	buttons := alertButtons(f, func(b desktop.Button) { clicked = b })
	out := make([]alert, 0, len(buttons))

	for _, b := range buttons {
		require.False(t, b.IsDefault && b.IsCancel, "a button takes Return or Escape, never both")

		key := ""

		switch {
		case b.IsDefault:
			key = "Return"
		case b.IsCancel:
			key = "Escape"
		}

		b.Callback()
		require.Equal(t, b.Label, clicked.Label, "the click reports its own button")
		out = append(out, alert{label: b.Label, key: key, choice: clicked.Choice})
	}

	return out
}

func TestTheStartupAlertsKeysAndClicks(t *testing.T) {
	t.Parallel()

	newerData := desktop.StartupFailure(errNewerSchemaForTest, desktop.Newer{Way: update.WayUpdate})
	backup := desktop.Restore{Backup: "/data/app.db.backup-x", Button: "Use my data from Nov 14, 3:02 PM"}

	assert.Equal(t, []alert{
		{label: "Use my data from Nov 14, 3:02 PM", choice: desktop.ChoicePutBack},
		{label: "Update and Reopen", key: "Return", choice: desktop.ChoiceUpdate},
		{label: "Quit", key: "Escape", choice: desktop.ChoiceQuit},
	}, drawn(t, newerData.Offering(backup)), "no key puts the backup back, and Quit stays rightmost")

	assert.Equal(t, []alert{
		{label: "Stop and Quit", key: "Escape", choice: desktop.ChoiceQuit},
	}, drawn(t, desktop.Updating()), "a Return pressed again never stops the update")

	assert.Equal(t, []alert{
		{label: "Quit", key: "Return", choice: desktop.ChoiceQuit},
	}, drawn(t, desktop.Failure{Title: "x", Message: "y"}), "alone, Quit takes Return")
}

// A build that can't open the data a newer build saved works out its way forward without
// reading anything: a build from source builds again, and a release that runs outside a
// bundle, as this test does, installs again by hand.
//
//nolint:paralleltest // sets the package's flavor, which the linker stamps
func TestNewerDataOffersTheBuildsWayForward(t *testing.T) {
	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	stamp(t, "")

	bare, newer := newerData(paths, t.TempDir(), logger)
	require.NotNil(t, bare)
	assert.Equal(t, update.WaySource, newer.Way)
	assert.Equal(t, "git pull && magex app:run", newer.Commands.Build)

	stamp(t, "release")

	_, newer = newerData(paths, t.TempDir(), logger)
	assert.Equal(t, update.WayInstall, newer.Way, "a release outside Applications can't replace itself")
	assert.Contains(t, []string{desktop.InstallCommand(false), desktop.InstallCommand(true)}, newer.Commands.Install,
		"the README's install command, for the Applications folder a move would take it to")
	assert.NoDirExists(t, paths.Updates, "nothing was read or kept")
}
