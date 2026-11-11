package hooks

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestToolFactsOfReadsTheFeaturesOfEachVersion(t *testing.T) {
	t.Parallel()

	for version, want := range map[string][]integrations.Feature{
		"1.9.0":  {},
		"1.13.1": {integrations.FeatureSelfUpdate},
		"1.14.0": {integrations.FeatureInstallExitCode, integrations.FeatureSelfUpdate, integrations.FeatureVerifiedTools},
		"1.14.1": {
			integrations.FeatureInstallExitCode, integrations.FeatureRunJSON, integrations.FeatureSelfUpdate,
			integrations.FeatureStatusJSON, integrations.FeatureVerifiedTools,
		},
		"dev": {},
	} {
		got := factsAt("/opt/tools/go-pre-commit", version)
		assert.Equal(t, want, got.Features, version)
		assert.True(t, got.Usable(), version)
	}
}

func TestToolFactsOfListsEveryCopy(t *testing.T) {
	t.Parallel()

	def := integrations.GoPreCommit()

	got := ToolFactsOf(def, integrations.Tool{
		ID: def.ID, On: true, State: integrations.StateFound,
		Copy:   integrations.Copy{Path: "/home/me/.local/bin/go-pre-commit"},
		Others: []integrations.Copy{{Path: "/home/me/go/bin/go-pre-commit", OnPath: true}},
	}, false)

	assert.Equal(t, []string{"/home/me/.local/bin/go-pre-commit", "/home/me/go/bin/go-pre-commit"}, got.Copies)
	assert.True(t, got.OnPath, "the shell finds another copy")
	assert.False(t, got.ShellRead)

	none := ToolFactsOf(def, integrations.Tool{ID: def.ID, State: integrations.StateMissing}, true)
	assert.Empty(t, none.Copies)
	assert.NotNil(t, none.Copies)
	assert.False(t, none.Usable())
	assert.False(t, none.Off, "a switch left on automatic is never off, though nothing is found")
}

func TestToolFactsOfIsOffOnlyWhenTheUserSaysSo(t *testing.T) {
	t.Parallel()

	def := integrations.GoPreCommit()
	tool := func(sw integrationprefs.Switch, state integrations.State, on bool) ToolFacts {
		return ToolFactsOf(def, integrations.Tool{
			ID: def.ID, Switch: sw, On: on, State: state,
			Copy: integrations.Copy{Path: "/opt/go-pre-commit", Version: integrations.Version{Raw: "1.10.0"}},
		}, true)
	}

	tooOld := tool(integrationprefs.SwitchAuto, integrations.StateTooOld, false)
	assert.False(t, tooOld.Off, "automatic with a copy too old to use isn't turned off")
	assert.False(t, tooOld.Usable())
	require.ErrorIs(t, noToolError(tooOld), ErrNoTool)
	assert.Contains(t, uierr.Message(noToolError(tooOld)), "too old")

	missing := tool(integrationprefs.SwitchOn, integrations.StateMissing, true)
	assert.False(t, missing.Off)
	assert.Contains(t, uierr.Message(noToolError(missing)), "isn't installed")

	off := tool(integrationprefs.SwitchOff, integrations.StateFound, false)
	assert.True(t, off.Off)
	assert.Contains(t, uierr.Message(noToolError(off)), "turned off")
}

func TestToolFactsUsable(t *testing.T) {
	t.Parallel()

	on := factsAt("/opt/tools/go-pre-commit", "1.14.1")
	assert.True(t, on.Has(integrations.FeatureRunJSON))

	off := on
	off.Off = true
	assert.False(t, off.Usable())
	assert.False(t, off.Has(integrations.FeatureRunJSON), "a copy turned off has nothing")

	old := on
	old.State = integrations.StateTooOld
	assert.False(t, old.Usable())

	assert.Equal(t, ToolStatus{
		Version: "1.14.1", Path: "~/bin/go-pre-commit", State: integrations.StateFound, Usable: true,
	}, factsAt("/srv/home/bin/go-pre-commit", "1.14.1").status("/srv/home"))
	assert.Equal(t, ToolStatus{State: integrations.StateMissing, Off: true}, ToolFacts{
		State: integrations.StateMissing, Off: true,
	}.status("/srv/home"))
}

func TestPathWarning(t *testing.T) {
	t.Parallel()

	home := "/srv/home"
	local := filepath.Join(home, ".local", "bin", "go-pre-commit")
	gopath := filepath.Join(home, "go", "bin", "go-pre-commit")

	offPath := func(copies ...string) ToolFacts {
		return ToolFacts{Path: copies[0], Copies: copies, ShellRead: true}
	}

	assert.Equal(t, PathWarning{Show: true, Dir: "~/.local/bin"}, pathWarning(offPath(local), nil, home))

	assert.Equal(t, PathWarning{}, pathWarning(ToolFacts{}, nil, home), "no copy, no warning")
	assert.Equal(t, PathWarning{}, pathWarning(ToolFacts{Path: local, Copies: []string{local}}, nil, home),
		"a shell not read can't tell")
	assert.Equal(t, PathWarning{}, pathWarning(ToolFacts{Path: local, Copies: []string{local}, OnPath: true, ShellRead: true},
		nil, home))
	assert.Equal(t, PathWarning{}, pathWarning(offPath(local, gopath), nil, home), "the hook finds the copy in GOPATH's bin")
	assert.Equal(t, PathWarning{}, pathWarning(offPath(local, "/opt/go/bin/go-pre-commit"),
		[]string{"GOPATH=/opt/go:/srv/second"}, home))
	assert.Equal(t, PathWarning{Show: true, Dir: "~/.local/bin"}, pathWarning(offPath(local, gopath),
		[]string{"GOPATH=/opt/go"}, home), "GOPATH says where the hook looks")
}

func TestGoPathBin(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/opt/go/bin", goPathBin([]string{"GOPATH=/opt/go:/srv/other"}, "/srv/home"))
	assert.Equal(t, "/srv/home/go/bin", goPathBin([]string{"GOPATH=relative"}, "/srv/home"))
	assert.Equal(t, "/srv/home/go/bin", goPathBin(nil, "/srv/home"))
	assert.Empty(t, goPathBin(nil, ""))
}
