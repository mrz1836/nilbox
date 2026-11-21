package kit

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

func TestAProposalThatCantBeEncodedIsRefused(t *testing.T) {
	t.Parallel()

	// No decoded document holds a budget that isn't a number; one built by hand may, and
	// encoding refuses it.
	nan := math.NaN()
	p := Proposal{Summary: "x", Routines: []RoutineChange{{Name: "R", Spec: spec.Spec{Steps: []spec.Step{{
		Agent: &spec.AgentStep{Fence: &spec.Fence{Budget: &spec.Budget{USD: &nan}}},
	}}}}}}

	_, err := canonical(p)
	require.Error(t, err)

	_, err = readProposal(p)
	require.ErrorIs(t, err, ErrUnreadableProposal)

	assert.True(t, strings.HasPrefix(routinesCap(p), "routines[0].spec: "), routinesCap(p))
}

func TestDigitGroupsWriteThousandsApart(t *testing.T) {
	t.Parallel()

	for n, want := range map[int64]string{
		0: "0", 999: "999", 1000: "1,000", 200_000: "200,000", 2_000_000: "2,000,000", -1_234_567: "-1,234,567",
	} {
		assert.Equal(t, want, digitGroups(n), "%d", n)
	}
}

func TestNoActiveHoursAreAlways(t *testing.T) {
	t.Parallel()

	assert.True(t, startsAt(nil, 0, 0))
	assert.True(t, startsAt(nil, 6, 24*60-1))
}

func TestARoutineHeldInDryRunSaysForWhichSteps(t *testing.T) {
	t.Parallel()

	lines := routineLines(CheckedRoutine{DryRunOnly: []catalog.StepKind{catalog.StepMerge, "teleport"}})

	assert.Contains(t, lines, "- This build runs it only in Dry run, whatever its mode, for “Merge it” and “teleport”.")
}

// jsonNames returns the JSON names of the struct T's fields, in their order.
func jsonNames[T any]() []string {
	t := reflect.TypeFor[T]()
	names := make([]string, 0, t.NumField())

	for field := range t.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		names = append(names, name)
	}

	return names
}

func TestEverySettingAProposalChangesIsKeyedByItsJSONName(t *testing.T) {
	t.Parallel()

	keys := make([]string, 0, len(settingRows()))
	for _, row := range settingRows() {
		keys = append(keys, string(row.key))
	}

	assert.Equal(t, jsonNames[SettingsChange](), keys,
		"a SettingChange's key is the proposal's own name for the setting, which the window applies by")
}

func TestEveryRuleChangeIsKindedByTheListItCameFrom(t *testing.T) {
	t.Parallel()

	before := Rules{
		TrustedBots: []string{"dependabot"}, Pinned: []string{"acme-org/pinned"}, Muted: []string{},
		IgnoredWorkflows: []string{"Nightly"},
	}
	checked := checkRules(before, RulesChange{
		Trust: []string{"mergify"}, Untrust: []string{"dependabot"}, Pin: []string{"acme-org/new"},
		Mute: []string{"acme-org/loud"}, Clear: []string{"acme-org/pinned"}, IgnoreWorkflows: []string{"Deploy"},
		UnignoreWorkflows: []string{"Nightly"},
	})
	require.Empty(t, checked.Problems)

	kinds := make([]string, 0, len(checked.Changes))
	for _, c := range checked.Changes {
		kinds = append(kinds, string(c.Kind))
	}

	assert.ElementsMatch(t, jsonNames[RulesChange](), kinds,
		"a RuleChange's kind is the name of the proposal's list it came from, which the window applies by")
}
