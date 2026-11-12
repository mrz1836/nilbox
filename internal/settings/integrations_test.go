package settings

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

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

func TestSetOfferAfterMerge(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	svc := serviceOver(t, testutil.DB(t), t.TempDir(), WithEvents(events))

	fresh, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.True(t, fresh.Integrations.OfferAfterMerge, "a merge offers a release until the user turns it off")

	got, err := svc.SetOfferAfterMerge(t.Context(), false)
	require.NoError(t, err)
	assert.False(t, got.Integrations.OfferAfterMerge)

	stored, ok, err := svc.prefs.Get(t.Context(), integrationprefs.KeyOfferAfterMerge)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "false", stored)

	reread, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.False(t, reread.Integrations.OfferAfterMerge, "it is kept")

	got, err = svc.SetOfferAfterMerge(t.Context(), true)
	require.NoError(t, err)
	assert.True(t, got.Integrations.OfferAfterMerge)
	assert.Zero(t, events.Count(), "the review sheet asks as each merge lands")
}

func TestSetScriptSources(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	svc := serviceOver(t, testutil.DB(t), t.TempDir(), WithEvents(events))

	fresh, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, integrationprefs.ScriptSources{MagexTargets: true, RepoFiles: true}, fresh.Integrations.ScriptSources,
		"Repo scripts lists every source until the user turns one off")

	got, err := svc.SetScriptSources(t.Context(), false, true)
	require.NoError(t, err)
	assert.Equal(t, integrationprefs.ScriptSources{MagexTargets: false, RepoFiles: true}, got.Integrations.ScriptSources)

	for key, want := range map[string]string{
		integrationprefs.KeyMagexTargets: "false", integrationprefs.KeyRepoFiles: "true",
	} {
		stored, ok, getErr := svc.prefs.Get(t.Context(), key)
		require.NoError(t, getErr)
		assert.True(t, ok, key)
		assert.Equal(t, want, stored, key)
	}

	got, err = svc.SetScriptSources(t.Context(), true, false)
	require.NoError(t, err)
	assert.Equal(t, integrationprefs.ScriptSources{MagexTargets: true, RepoFiles: false}, got.Integrations.ScriptSources)

	reread, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, got.Integrations.ScriptSources, reread.Integrations.ScriptSources, "they are kept")
	assert.Zero(t, events.Count(), "Repo scripts reads the setting as it lists")
}

func TestSetScriptSourcesStoresBothOrNeither(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	svc := serviceOver(t, db, t.TempDir())

	_, err := db.ExecContext(t.Context(), `CREATE TRIGGER refuse_files BEFORE INSERT ON prefs
		WHEN NEW.key = 'scripts.repoFiles' BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)

	_, err = svc.SetScriptSources(t.Context(), false, false)
	require.Error(t, err)

	reread, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.True(t, reread.Integrations.ScriptSources.MagexTargets, "the first write rolled back with the second")
}

func TestSetIntegrationPath(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	db := testutil.DB(t)
	events := &testutil.Events{}
	svc := serviceOver(t, db, home, WithEvents(events))

	magex := filepath.Join(home, "tools", "magex")
	testutil.WriteFileMode(t, magex, "", 0o700)

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

func TestTheScriptsTabsMemoryIsKept(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	svc := serviceOver(t, testutil.DB(t), t.TempDir(), WithEvents(events))

	fresh, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.False(t, fresh.Integrations.Announced)
	assert.Equal(t, integrationprefs.PageNone, fresh.Integrations.LastPage)
	assert.Empty(t, fresh.Integrations.LastRepo)

	got, err := svc.SetIntegrationsAnnounced(t.Context(), true)
	require.NoError(t, err)
	assert.True(t, got.Integrations.Announced)

	got, err = svc.SetScriptsLastPage(t.Context(), integrationprefs.PageScripts)
	require.NoError(t, err)
	assert.Equal(t, integrationprefs.PageScripts, got.Integrations.LastPage)

	got, err = svc.SetScriptsLastRepo(t.Context(), "~/projects/example-repo")
	require.NoError(t, err)
	assert.Equal(t, "~/projects/example-repo", got.Integrations.LastRepo)

	_, err = svc.SetScriptsLastPage(t.Context(), "settings")
	require.ErrorIs(t, err, integrationprefs.ErrInvalidPage)

	_, err = svc.SetScriptsLastRepo(t.Context(), "projects/example-repo")
	require.ErrorIs(t, err, integrationprefs.ErrInvalidRepo)

	reread, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.True(t, reread.Integrations.Announced, "each is kept")
	assert.Equal(t, integrationprefs.PageScripts, reread.Integrations.LastPage, "a refused page stored nothing")
	assert.Equal(t, "~/projects/example-repo", reread.Integrations.LastRepo, "nor did a refused folder")

	got, err = svc.SetScriptsLastRepo(t.Context(), "")
	require.NoError(t, err)
	assert.Empty(t, got.Integrations.LastRepo, "an empty folder forgets it")
	assert.Zero(t, events.Count(), "only the window reads them")
}

func TestTheScriptsTabsSectionsAndRunsAreKept(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	svc := serviceOver(t, testutil.DB(t), t.TempDir(), WithEvents(events))

	fresh, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Empty(t, fresh.Integrations.Sections)
	assert.False(t, fresh.Integrations.ShowRuns)

	got, err := svc.SetScriptsSectionOpen(t.Context(), integrationprefs.SectionHooksReady, false)
	require.NoError(t, err)
	assert.Equal(t, map[integrationprefs.ScriptsSection]bool{integrationprefs.SectionHooksReady: false},
		got.Integrations.Sections)

	got, err = svc.SetScriptsSectionOpen(t.Context(), integrationprefs.SectionScriptsMagex, true)
	require.NoError(t, err)
	assert.Equal(t, map[integrationprefs.ScriptsSection]bool{
		integrationprefs.SectionHooksReady: false, integrationprefs.SectionScriptsMagex: true,
	}, got.Integrations.Sections, "the others stay as they were")

	_, err = svc.SetScriptsSectionOpen(t.Context(), "hooks.someday", true)
	require.ErrorIs(t, err, integrationprefs.ErrInvalidSection)

	got, err = svc.SetShowRuns(t.Context(), true)
	require.NoError(t, err)
	assert.True(t, got.Integrations.ShowRuns)

	reread, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Len(t, reread.Integrations.Sections, 2, "a refused section stored nothing")
	assert.True(t, reread.Integrations.ShowRuns)
	assert.Zero(t, events.Count(), "only the window reads them")
}
