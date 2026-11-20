package kit

import (
	"math"
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
