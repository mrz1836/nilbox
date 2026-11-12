package integrationprefs_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/integrationprefs"
)

func TestDecodeDefaultsToAutomatic(t *testing.T) {
	t.Parallel()

	got := integrationprefs.Decode(nil)

	assert.Equal(t, integrationprefs.Config{
		Switches: map[integrationprefs.ID]integrationprefs.Switch{
			integrationprefs.MageX:       integrationprefs.SwitchAuto,
			integrationprefs.GoPreCommit: integrationprefs.SwitchAuto,
			integrationprefs.Scripts:     integrationprefs.SwitchAuto,
		},
		Paths:           map[integrationprefs.ID]string{integrationprefs.MageX: "", integrationprefs.GoPreCommit: ""},
		RunsIn:          integrationprefs.RunsInApp,
		OfferAfterMerge: true,
		ScriptSources:   integrationprefs.ScriptSources{MagexTargets: true, RepoFiles: true},
		Sections:        map[integrationprefs.ScriptsSection]bool{},
	}, got, "a fresh database: every switch automatic, no copy chosen, runs in the app, a merge offers a release, "+
		"Repo scripts lists every source and shows no run at once, every section as its page starts it, and no nil "+
		"map")
}

func TestDecodeReadsTheStoredValues(t *testing.T) {
	t.Parallel()

	got := integrationprefs.Decode(map[string]string{
		"integrations.mage-x.enabled":        "on",
		"integrations.go-pre-commit.enabled": "off",
		"integrations.scripts.enabled":       "sometimes",
		"integrations.mage-x.path":           "/opt/tools/bin/../bin/magex",
		"integrations.go-pre-commit.path":    "bin/go-pre-commit",
		"integrations.runsIn":                "terminal",
		"integrations.offerAfterMerge":       "false",
		"scripts.magexTargets":               "false",
		"scripts.repoFiles":                  "yes",
	})

	assert.Equal(t, integrationprefs.SwitchOn, got.Switches[integrationprefs.MageX])
	assert.Equal(t, integrationprefs.SwitchOff, got.Switches[integrationprefs.GoPreCommit])
	assert.Equal(t, integrationprefs.SwitchAuto, got.Switches[integrationprefs.Scripts], "an unknown value is automatic")
	assert.Equal(t, "/opt/tools/bin/magex", got.Paths[integrationprefs.MageX], "cleaned")
	assert.Empty(t, got.Paths[integrationprefs.GoPreCommit], "a relative path is none")
	assert.Equal(t, integrationprefs.RunsInTerminal, got.RunsIn)
	assert.False(t, got.OfferAfterMerge)
	assert.Equal(t, integrationprefs.ScriptSources{MagexTargets: false, RepoFiles: true}, got.ScriptSources,
		"a value that doesn't read as a boolean is the default, on")
}

func TestOfferAfterMerge(t *testing.T) {
	t.Parallel()

	for _, on := range []bool{true, false} {
		assert.Equal(t, on, integrationprefs.DecodeOfferAfterMerge(integrationprefs.EncodeOfferAfterMerge(on)),
			"a stored choice reads back as itself")
	}

	for _, stored := range []string{"", "maybe", "on"} {
		assert.True(t, integrationprefs.DecodeOfferAfterMerge(stored), "%q is the default: on", stored)
	}

	assert.False(t, integrationprefs.DecodeOfferAfterMerge("0"), "anything that reads as false is off")
}

func TestEncodeRunsIn(t *testing.T) {
	t.Parallel()

	for _, place := range []integrationprefs.RunsIn{integrationprefs.RunsInApp, integrationprefs.RunsInTerminal} {
		got, err := integrationprefs.EncodeRunsIn(place)
		require.NoError(t, err)
		assert.Equal(t, place, integrationprefs.DecodeRunsIn(got), "a stored place reads back as itself")
	}

	for _, refused := range []integrationprefs.RunsIn{"", "cloud"} {
		_, err := integrationprefs.EncodeRunsIn(refused)
		require.ErrorIs(t, err, integrationprefs.ErrInvalidRunsIn, refused)
	}

	assert.Equal(t, integrationprefs.RunsInApp, integrationprefs.DecodeRunsIn("Terminal"), "anything else is the app")
}

func TestEncodeSwitch(t *testing.T) {
	t.Parallel()

	for sw, want := range map[integrationprefs.Switch]string{
		integrationprefs.SwitchAuto: "", integrationprefs.SwitchOn: "on", integrationprefs.SwitchOff: "off",
	} {
		got, err := integrationprefs.EncodeSwitch(sw)
		require.NoError(t, err)
		assert.Equal(t, want, got)
		assert.Equal(t, sw, integrationprefs.DecodeSwitch(got), "a stored switch reads back as itself")
		assert.True(t, sw.Valid())
	}

	_, err := integrationprefs.EncodeSwitch("maybe")
	require.ErrorIs(t, err, integrationprefs.ErrInvalidSwitch)
	assert.False(t, integrationprefs.Switch("").Valid(), "the stored form of automatic isn't a state")
}

func TestEncodePath(t *testing.T) {
	t.Parallel()

	got, err := integrationprefs.EncodePath("/opt/tools//bin/magex")
	require.NoError(t, err)
	assert.Equal(t, "/opt/tools/bin/magex", got)

	got, err = integrationprefs.EncodePath("")
	require.NoError(t, err)
	assert.Empty(t, got, "none: the copy the shell finds")

	_, err = integrationprefs.EncodePath("bin/magex")
	require.ErrorIs(t, err, integrationprefs.ErrRelativePath)
}

func TestIDs(t *testing.T) {
	t.Parallel()

	assert.True(t, integrationprefs.MageX.Valid())
	assert.True(t, integrationprefs.Scripts.Valid())
	assert.False(t, integrationprefs.ID("go-broadcast").Valid())
	assert.True(t, integrationprefs.GoPreCommit.Tool())
	assert.False(t, integrationprefs.Scripts.Tool(), "the scripts have no copy to choose")
	assert.Equal(t, []string{
		"integrations.mage-x.enabled", "integrations.go-pre-commit.enabled", "integrations.scripts.enabled",
		"integrations.mage-x.path", "integrations.go-pre-commit.path", "integrations.runsIn",
		"integrations.offerAfterMerge", "scripts.magexTargets", "scripts.repoFiles", "integrations.announced",
		"scripts.lastPage", "scripts.lastRepo", "scripts.sections", "scripts.showRuns",
	}, integrationprefs.Keys())
}

func FuzzDecode(f *testing.F) {
	f.Add("on", "/usr/local/bin/magex")
	f.Add("", "")
	f.Add("off", "relative/magex")
	f.Add("\x00", "/\x00")

	f.Fuzz(func(t *testing.T, sw, path string) {
		got := integrationprefs.Decode(map[string]string{
			integrationprefs.KeyEnabled(integrationprefs.MageX): sw,
			integrationprefs.KeyPath(integrationprefs.MageX):    path,
		})

		if !got.Switches[integrationprefs.MageX].Valid() {
			t.Fatalf("decoded an invalid switch from %q", sw)
		}

		if p := got.Paths[integrationprefs.MageX]; p != "" {
			if _, err := integrationprefs.EncodePath(p); err != nil {
				t.Fatalf("decoded a path EncodePath refuses: %q", p)
			}
		}
	})
}

func TestTheScriptsTabsMemory(t *testing.T) {
	t.Parallel()

	fresh := integrationprefs.Decode(nil)
	assert.False(t, fresh.Announced, "a fresh database hasn't announced the tab")
	assert.Equal(t, integrationprefs.PageNone, fresh.LastPage)
	assert.Empty(t, fresh.LastRepo)

	got := integrationprefs.Decode(map[string]string{
		"integrations.announced": "true", "scripts.lastPage": "hooks", "scripts.lastRepo": "~/projects/example-repo",
	})
	assert.True(t, got.Announced)
	assert.Equal(t, integrationprefs.PageHooks, got.LastPage)
	assert.Equal(t, "~/projects/example-repo", got.LastRepo)

	odd := integrationprefs.Decode(map[string]string{
		"integrations.announced": "yes", "scripts.lastPage": "settings", "scripts.lastRepo": "projects/example-repo",
	})
	assert.False(t, odd.Announced, "only true announces")
	assert.Equal(t, integrationprefs.PageNone, odd.LastPage, "a page the tab doesn't have is none")
	assert.Empty(t, odd.LastRepo, "a folder that isn't an absolute or ~ path is none")
}

func TestEncodeLastPage(t *testing.T) {
	t.Parallel()

	for _, page := range []integrationprefs.ScriptsPage{
		integrationprefs.PageRelease, integrationprefs.PageHooks, integrationprefs.PageScripts, integrationprefs.PageRuns,
	} {
		stored, err := integrationprefs.EncodeLastPage(page)
		require.NoError(t, err)
		assert.Equal(t, page, integrationprefs.DecodeLastPage(stored), "%s round-trips", page)
	}

	for _, page := range []integrationprefs.ScriptsPage{integrationprefs.PageNone, "settings", "Release"} {
		_, err := integrationprefs.EncodeLastPage(page)
		require.ErrorIs(t, err, integrationprefs.ErrInvalidPage, "%q", page)
	}
}

func TestEncodeLastRepo(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{"", "~/projects/example-repo", "/Volumes/work/example-repo", "~/projects/naïve repo"} {
		stored, err := integrationprefs.EncodeLastRepo(dir)
		require.NoError(t, err, "%q", dir)
		assert.Equal(t, dir, integrationprefs.DecodeLastRepo(stored), "%q round-trips", dir)
	}

	for name, dir := range map[string]string{
		"relative":     "projects/example-repo",
		"another home": "~other/example-repo",
		"two lines":    "~/projects/a\n~/projects/b",
		"a control":    "~/projects/a\x1b[2J",
		"not UTF-8":    "~/projects/\xff",
		"too long":     "/" + strings.Repeat("a", 4096),
	} {
		_, err := integrationprefs.EncodeLastRepo(dir)
		require.ErrorIs(t, err, integrationprefs.ErrInvalidRepo, name)
		assert.Empty(t, integrationprefs.DecodeLastRepo(dir), "%s reads as none", name)
	}
}

func TestTheSectionsTheUserOpenedOrFolded(t *testing.T) {
	t.Parallel()

	got := integrationprefs.Decode(map[string]string{
		integrationprefs.KeySections: `{"hooks.ready":false,"scripts.magex":true,"hooks.someday":true,"scripts.t3":"x"}`,
	})
	assert.Equal(t, map[integrationprefs.ScriptsSection]bool{
		integrationprefs.SectionHooksReady: false, integrationprefs.SectionScriptsMagex: true,
	}, got.Sections, "a section the tab doesn't have is dropped")

	for _, odd := range []string{"", "[]", "not json", `{"hooks.ready":"no"}`} {
		assert.Empty(t, integrationprefs.DecodeSections(odd), odd)
	}

	stored, err := integrationprefs.EncodeSections(map[integrationprefs.ScriptsSection]bool{
		integrationprefs.SectionScriptsYours: false, integrationprefs.SectionHooksOff: true,
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"scripts.yours":false,"hooks.off":true}`, stored)

	stored, err = integrationprefs.EncodeSections(nil)
	require.NoError(t, err)
	assert.Empty(t, stored, "none is stored as nothing")

	_, err = integrationprefs.EncodeSections(map[integrationprefs.ScriptsSection]bool{"hooks.someday": true})
	require.ErrorIs(t, err, integrationprefs.ErrInvalidSection)

	for _, section := range integrationprefs.Sections() {
		assert.True(t, section.Valid(), section)
	}
}

func TestShowRunsStartsOff(t *testing.T) {
	t.Parallel()

	assert.False(t, integrationprefs.Decode(nil).ShowRuns, "a run shows beside the scripts only when asked")
	assert.True(t, integrationprefs.Decode(map[string]string{integrationprefs.KeyShowRuns: "true"}).ShowRuns)
	assert.False(t, integrationprefs.Decode(map[string]string{integrationprefs.KeyShowRuns: "yes"}).ShowRuns)
}
