package integrationprefs_test

import (
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
	}, got, "a fresh database: every switch automatic, no copy chosen, runs in the app, a merge offers a release, "+
		"Repo scripts lists every source, and no nil map")
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
		"integrations.offerAfterMerge", "scripts.magexTargets", "scripts.repoFiles",
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
