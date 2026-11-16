package autopilot

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
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

func TestOnlyAStepThisBuildCantRunKeepsARoutineInDryRun(t *testing.T) {
	t.Parallel()

	none := spec.Spec{Version: spec.Version}
	assert.Empty(t, notOffered(none))
	assert.Equal(t, autopilotprefs.ModeAuto, modeFor(autopilotprefs.ModeAuto, none), "a mode stands for what runs")

	notify := spec.Spec{Steps: []spec.Step{{Notify: &spec.NotifyStep{}}, {Pin: &spec.Empty{}}, {Notify: &spec.NotifyStep{}}}}
	assert.Equal(t, []catalog.StepKind{catalog.StepNotify, catalog.StepPin}, notOffered(notify), "each kind once")
	assert.Equal(t, autopilotprefs.ModeDryRun, modeFor(autopilotprefs.ModeAuto, notify))

	offered := catalog.Step{Kind: catalog.StepGitHub, Offered: true}
	github := spec.Step{GitHub: []spec.GitHubAction{{Rebase: &spec.Empty{}}}}
	assert.False(t, runnable(offered, github), "a GitHub step runs only once each of its actions is offered")
	assert.True(t, runnable(catalog.Step{Kind: catalog.StepPin, Offered: true}, spec.Step{Pin: &spec.Empty{}}))
}
