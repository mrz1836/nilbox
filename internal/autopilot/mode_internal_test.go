package autopilot

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// errUnreadablePrefs is settings that can't be read.
var errUnreadablePrefs = errors.New("the prefs can't be read")

// modePrefs is fake Autopilot settings for the mode tests.
type modePrefs struct {
	mode autopilotprefs.Mode
	err  error
}

// Autopilot returns settings whose new routines start in p's mode.
func (p modePrefs) Autopilot(context.Context) (autopilotprefs.Config, error) {
	c := autopilotprefs.Defaults()
	c.DefaultMode = p.mode

	return c, p.err
}

func TestTheModeANewRoutineStartsIn(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		prefs Prefs
		want  autopilotprefs.Mode
	}{
		{nil, autopilotprefs.ModeAsk},
		{modePrefs{mode: autopilotprefs.ModeAuto}, autopilotprefs.ModeAuto},
		{modePrefs{mode: autopilotprefs.ModeDryRun}, autopilotprefs.ModeDryRun},
		{modePrefs{err: errUnreadablePrefs}, autopilotprefs.ModeAsk},
	} {
		logs := &testutil.Logs{}
		s := NewService(Config{Prefs: c.prefs, Logger: logs.Logger()})
		assert.Equal(t, c.want, s.defaultMode(t.Context()))
	}
}

// TestOnlyAStepThisBuildCantRunKeepsARoutineInDryRun holds a restore and an import to the
// mode the catalog's offers keep a routine in (catalogMode). The offers' own cases, which
// kinds of step a build runs and the mode it keeps a routine in for them, are the draft
// leaf's, beside Offers.
func TestOnlyAStepThisBuildCantRunKeepsARoutineInDryRun(t *testing.T) {
	t.Parallel()

	later := spec.Spec{Steps: []spec.Step{
		{Notify: &spec.NotifyStep{}},
		{Pin: &spec.Empty{}},
		{MarkDone: &spec.Empty{}},
		{GitHub: []spec.GitHubAction{{Rebase: &spec.Empty{}}}},
		{MarkDone: &spec.Empty{}},
	}}
	assert.Equal(t, autopilotprefs.ModeAuto, catalogMode(autopilotprefs.ModeAuto, later))

	notices := spec.Spec{Steps: []spec.Step{{Notify: &spec.NotifyStep{}}, {Pin: &spec.Empty{}}, {TellMe: &spec.TellStep{}}}}
	assert.Equal(t, autopilotprefs.ModeAuto, catalogMode(autopilotprefs.ModeAuto, notices))

	pin := spec.Spec{Steps: []spec.Step{{Pin: &spec.Empty{}}, {Snooze: &spec.SnoozeStep{}}, {Restore: &spec.Empty{}}}}
	assert.Equal(t, autopilotprefs.ModeAuto, catalogMode(autopilotprefs.ModeAuto, pin), "the triage steps take any mode")
}
