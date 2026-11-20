package draft_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

func TestAScriptStepsScriptIsTheOneItPickedUnderItsName(t *testing.T) {
	t.Parallel()

	read := func(s *setup) []spec.Problem {
		got, err := draft.Read(t.Context(), lintSpec(), "", s, nil)
		require.NoError(t, err)

		return got.Problems
	}

	lint := draft.Script{ID: 7, Name: "Lint"}

	assert.Empty(t, read(&setup{scripts: []draft.Script{{ID: 3, Name: "Test"}, lint}}), "the script it picked, under its name")

	assert.Equal(t, []spec.Problem{{
		Part: spec.PartSteps, Field: "[1].script.id", Message: `"Lint" is no longer one of your scripts: pick another`,
	}}, read(&setup{scripts: []draft.Script{{ID: 3, Name: "Lint"}}}), "another script under its name is never it")

	assert.Equal(t, []spec.Problem{{
		Part: spec.PartSteps, Field: "[1].script.id", Message: `The script picked as "Lint" is now "Lint all": pick it again`,
	}}, read(&setup{scripts: []draft.Script{{ID: 7, Name: "Lint all"}}}), "renamed, it waits to be picked again")

	assert.Empty(t, read(&setup{noScripts: true}), "scripts that can't be read find none here")

	notify := &setup{}
	_, err := draft.Read(t.Context(), notifySpec(), "", notify, nil)
	require.NoError(t, err)
	assert.Zero(t, notify.scriptReads, "a routine that runs no script reads none")

	assert.Equal(t, `The script picked as "Lint" is now "Lint all": pick it again`, draft.RenamedWords("Lint", "Lint all"))
}

func TestHasStepFindsAStepOfAKind(t *testing.T) {
	t.Parallel()

	assert.True(t, draft.HasStep(lintSpec(), catalog.StepScript))
	assert.True(t, draft.HasStep(lintSpec(), catalog.StepNotify))
	assert.False(t, draft.HasStep(lintSpec(), catalog.StepAgent))
	assert.False(t, draft.HasStep(spec.Spec{}, catalog.StepScript), "a routine with no steps")
}
