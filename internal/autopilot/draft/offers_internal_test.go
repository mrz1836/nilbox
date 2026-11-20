package draft

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

func TestOnlyAStepThisBuildCantRunKeepsARoutineInDryRun(t *testing.T) {
	t.Parallel()

	var catalogs *Offers

	none := spec.Spec{Version: spec.Version}
	assert.Empty(t, catalogs.NotOffered(none))
	assert.Equal(t, autopilotprefs.ModeAuto, catalogs.ModeFor(autopilotprefs.ModeAuto, none), "a mode stands for what runs")

	later := spec.Spec{Steps: []spec.Step{
		{Notify: &spec.NotifyStep{}},
		{Pin: &spec.Empty{}},
		{MarkDone: &spec.Empty{}},
		{GitHub: []spec.GitHubAction{{Rebase: &spec.Empty{}}}},
		{MarkDone: &spec.Empty{}},
	}}
	assert.Empty(t, catalogs.NotOffered(later),
		"the notices, the triage steps, the notification's Done, and the GitHub step all run")

	verify := spec.Spec{Steps: []spec.Step{
		{Notify: &spec.NotifyStep{}},
		{WaitForChecks: &spec.WaitStep{}},
		{WaitForChecks: &spec.WaitStep{}},
	}}
	assert.Empty(t, catalogs.NotOffered(verify), "the engine follows an agent's checks itself")

	noticesOnly := &Offers{Steps: []catalog.StepKind{catalog.StepNotify}}
	assert.Equal(t, []catalog.StepKind{catalog.StepWaitForChecks}, noticesOnly.NotOffered(verify),
		"each kind once: a build that can't follow an agent's checks")
	assert.Equal(t, autopilotprefs.ModeDryRun, noticesOnly.ModeFor(autopilotprefs.ModeAuto, verify))

	notices := spec.Spec{Steps: []spec.Step{{Notify: &spec.NotifyStep{}}, {Pin: &spec.Empty{}}, {TellMe: &spec.TellStep{}}}}
	assert.Empty(t, catalogs.NotOffered(notices), "the notices and the triage steps run in this build")

	offered := &Offers{Steps: []catalog.StepKind{catalog.StepGitHub, catalog.StepPin}}
	github := spec.Step{GitHub: []spec.GitHubAction{{Rebase: &spec.Empty{}}}}
	assert.False(t, offered.runs(github), "a GitHub step runs only once each of its actions is offered")
	assert.True(t, offered.runs(spec.Step{Pin: &spec.Empty{}}))
	assert.Equal(t, autopilotprefs.ModeAuto, offered.ModeFor(autopilotprefs.ModeAuto, spec.Spec{Steps: []spec.Step{{Pin: &spec.Empty{}}}}),
		"a test's offers run what they name")
}

func TestAnActionIsNamedByTheBuildersLabel(t *testing.T) {
	t.Parallel()

	for _, a := range catalog.GitHubActions() {
		assert.Equal(t, a.Label, actionLabel(a.Kind), a.Kind)
	}

	assert.Empty(t, actionLabel("no-such-action"), "an action neither the catalog nor the spec spells")
}
