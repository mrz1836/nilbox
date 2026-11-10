package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// executable writes an empty file at path and makes it executable.
func executable(t *testing.T, path string) {
	t.Helper()

	testutil.WriteFile(t, path, "")
	require.NoError(t, os.Chmod(path, 0o700)) //nolint:gosec // a fake tool must be executable
}

func TestSetIntegrationEnabled(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	db := testutil.DB(t)
	events := &testutil.Events{}
	svc := serviceOver(t, db, home, WithEvents(events))

	got, err := svc.SetIntegrationEnabled(t.Context(), integrationprefs.Scripts, integrationprefs.SwitchOn)
	require.NoError(t, err)
	assert.Equal(t, integrationprefs.SwitchOn, got.Integrations.Switches[integrationprefs.Scripts])
	assert.Equal(t, []event.Event{event.IntegrationsChanged{}}, events.All(), "the tools' list changed")

	got, err = svc.SetIntegrationEnabled(t.Context(), integrationprefs.Scripts, integrationprefs.SwitchAuto)
	require.NoError(t, err)
	assert.Equal(t, integrationprefs.SwitchAuto, got.Integrations.Switches[integrationprefs.Scripts], "back to automatic")

	stored, ok, err := svc.prefs.Get(t.Context(), integrationprefs.KeyEnabled(integrationprefs.Scripts))
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Empty(t, stored, "automatic is stored as nothing")

	_, err = svc.SetIntegrationEnabled(t.Context(), "go-broadcast", integrationprefs.SwitchOn)
	require.ErrorIs(t, err, integrationprefs.ErrUnknownIntegration)

	_, err = svc.SetIntegrationEnabled(t.Context(), integrationprefs.MageX, "")
	require.ErrorIs(t, err, integrationprefs.ErrInvalidSwitch)
	assert.Equal(t, 2, events.Count(), "a refused change says nothing")
}

func TestSetRunsIn(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	svc := serviceOver(t, testutil.DB(t), t.TempDir(), WithEvents(events))

	got, err := svc.SetRunsIn(t.Context(), integrationprefs.RunsInTerminal)
	require.NoError(t, err)
	assert.Equal(t, integrationprefs.RunsInTerminal, got.Integrations.RunsIn)

	got, err = svc.SetRunsIn(t.Context(), integrationprefs.RunsInApp)
	require.NoError(t, err)
	assert.Equal(t, integrationprefs.RunsInApp, got.Integrations.RunsIn)

	_, err = svc.SetRunsIn(t.Context(), "cloud")
	require.ErrorIs(t, err, integrationprefs.ErrInvalidRunsIn)

	reread, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, integrationprefs.RunsInApp, reread.Integrations.RunsIn, "a refused place stored nothing")
	assert.Zero(t, events.Count(), "each run reads the setting when it starts")
}

func TestSetIntegrationPath(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	db := testutil.DB(t)
	events := &testutil.Events{}
	svc := serviceOver(t, db, home, WithEvents(events))

	magex := filepath.Join(home, "tools", "magex")
	executable(t, magex)

	got, err := svc.SetIntegrationPath(t.Context(), integrationprefs.MageX, " ~/tools/../tools/magex ")
	require.NoError(t, err)
	assert.Equal(t, magex, got.Integrations.Paths[integrationprefs.MageX], "expanded, cleaned, and absolute")
	assert.Equal(t, []event.Event{event.IntegrationsChanged{CopyChosen: true}}, events.All(), "the tools are looked for again")

	got, err = svc.SetIntegrationPath(t.Context(), integrationprefs.MageX, "")
	require.NoError(t, err)
	assert.Empty(t, got.Integrations.Paths[integrationprefs.MageX], "cleared: the copy the shell finds")

	plain := filepath.Join(home, "notes.txt")
	testutil.WriteFile(t, plain, "notes")

	refused := []struct {
		name, path, msg string
	}{
		{"missing", filepath.Join(home, "gone"), "There's nothing at ~/gone."},
		{"a folder", home, "~ isn't a program."},
		{"not executable", plain, "~/notes.txt can't be run: it isn't executable."},
	}

	for _, tt := range refused {
		_, err = svc.SetIntegrationPath(t.Context(), integrationprefs.MageX, tt.path)
		require.ErrorIs(t, err, ErrInvalidToolPath, tt.name)
		assert.Equal(t, tt.msg, uierr.Message(err), tt.name)
	}

	_, err = svc.SetIntegrationPath(t.Context(), integrationprefs.MageX, "tools/magex")
	require.ErrorIs(t, err, ErrInvalidToolPath, "a relative path")
	assert.Equal(t, "Enter the program's full path, such as ~/bin/magex.", uierr.Message(err))

	_, err = svc.SetIntegrationPath(t.Context(), integrationprefs.Scripts, magex)
	require.ErrorIs(t, err, integrationprefs.ErrNotATool)

	_, err = svc.SetIntegrationPath(t.Context(), "go-broadcast", magex)
	require.ErrorIs(t, err, integrationprefs.ErrUnknownIntegration)

	assert.Equal(t, 2, events.Count(), "a refused path says nothing")
}
