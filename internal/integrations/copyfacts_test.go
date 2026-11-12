package integrations

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/integrationprefs"
)

func TestFeaturesOfListsWhatAVersionHasSorted(t *testing.T) {
	t.Parallel()

	def := GoPreCommit()

	for version, want := range map[string][]Feature{
		"1.9.0":  {},
		"1.13.1": {FeatureSelfUpdate},
		"1.14.0": {FeatureInstallExitCode, FeatureSelfUpdate, FeatureVerifiedTools},
		"1.14.1": {FeatureInstallExitCode, FeatureRunJSON, FeatureSelfUpdate, FeatureStatusJSON, FeatureVerifiedTools},
		"dev":    {},
		"":       {},
	} {
		got := def.FeaturesOf(Version{Raw: version})
		assert.NotNil(t, got, version)
		assert.Equal(t, want, got, version)
	}

	assert.Equal(t, []Feature{}, Definition{}.FeaturesOf(Version{Raw: "9.9.9"}), "a tool with no features")
}

func TestCopyFactsOfReadsTheCopyInUse(t *testing.T) {
	t.Parallel()

	def := MageX()
	tool := Tool{
		Switch: integrationprefs.SwitchAuto, State: StateFound, On: true,
		Copy:   Copy{Path: "/opt/tools/magex", Version: Version{Raw: "1.30.1"}},
		Others: []Copy{{Path: "/opt/other/magex", Version: Version{Raw: "1.31.0"}}},
	}

	got := CopyFactsOf(def, tool)
	assert.Equal(t, CopyFacts{
		Path: "/opt/tools/magex", Version: "1.30.1", State: StateFound,
		Features: []Feature{FeatureBumpJSON, FeatureExpect, FeaturePushFails, FeatureSelfUpdate},
	}, got, "the copy in use alone, never another")
	assert.True(t, got.Usable())
	assert.True(t, got.Has(FeatureExpect))
	assert.False(t, got.Has(FeatureFastForwardPull))
	assert.Equal(t, "/opt/tools/magex@1.30.1", got.Key())

	tool.Switch, tool.On = integrationprefs.SwitchOff, false
	off := CopyFactsOf(def, tool)
	assert.True(t, off.Off, "only the user's switch turns it off")
	assert.False(t, off.Usable())
	assert.False(t, off.Has(FeatureExpect), "a copy turned off has nothing")

	tool.Switch = integrationprefs.SwitchOn
	assert.False(t, CopyFactsOf(def, tool).Off)

	missing := CopyFactsOf(def, Tool{State: StateMissing, On: true})
	assert.Equal(t, CopyFacts{State: StateMissing, Features: []Feature{}}, missing)
	assert.False(t, missing.Usable())
	assert.Equal(t, "@", missing.Key())
}

func TestCopyFactsUsableNeedsACopyThatIsOnAndNewEnough(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		facts CopyFacts
		want  bool
	}{
		{name: "found", facts: CopyFacts{Path: "/x", State: StateFound}, want: true},
		{name: "update", facts: CopyFacts{Path: "/x", State: StateUpdate}, want: true},
		{name: "off", facts: CopyFacts{Path: "/x", State: StateFound, Off: true}},
		{name: "no path", facts: CopyFacts{State: StateFound}},
		{name: "too old", facts: CopyFacts{Path: "/x", State: StateTooOld}},
		{name: "won't run", facts: CopyFacts{Path: "/x", State: StateWontRun}},
		{name: "missing", facts: CopyFacts{Path: "/x", State: StateMissing}},
	} {
		assert.Equal(t, tc.want, tc.facts.Usable(), tc.name)

		tc.facts.Features = []Feature{FeatureExpect}
		assert.Equal(t, tc.want, tc.facts.Has(FeatureExpect), tc.name)
	}
}
