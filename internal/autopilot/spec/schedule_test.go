package spec_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// scheduled returns the weekday nudge with its schedule's style set to style.
func scheduled(t *testing.T, style catalog.ScheduleStyle) spec.Spec {
	t.Helper()

	s := loadTemplate(t, "weekday-nudge")
	s.Trigger.Schedule.Style = style

	return s
}

func TestADigestTellsItsListFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		style  catalog.ScheduleStyle
		mutate func(*spec.Spec)
		want   []string
	}{
		{name: "each match, as the template is", style: ""},
		{name: "each match, spelled out", style: catalog.ScheduleEach},
		{name: "a digest whose Notify me comes first", style: catalog.ScheduleDigest},
		{
			name: "a digest whose Notify me comes second", style: catalog.ScheduleDigest,
			mutate: func(s *spec.Spec) { s.Steps[0], s.Steps[1] = s.Steps[1], s.Steps[0] },
			want:   []string{"trigger.schedule.style"},
		},
		{
			name: "a digest with no Notify me", style: catalog.ScheduleDigest,
			mutate: func(s *spec.Spec) { s.Steps = s.Steps[1:] },
			want:   []string{"trigger.schedule.style"},
		},
		{name: "a style there isn't", style: "weekly", want: []string{"trigger.schedule.style"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := scheduled(t, tt.style)
			if tt.mutate != nil {
				tt.mutate(&s)
			}

			assert.Equal(t, tt.want, problemPaths(t, s))
		})
	}
}

func TestEachMatchIsWrittenAsNoStyle(t *testing.T) {
	t.Parallel()

	each := spec.Normalize(scheduled(t, " each "))
	assert.Empty(t, each.Trigger.Schedule.Style, "the default has one form")
	assert.False(t, each.Trigger.Schedule.Digest())

	digest := spec.Normalize(scheduled(t, " digest"))
	assert.Equal(t, catalog.ScheduleDigest, digest.Trigger.Schedule.Style)
	assert.True(t, digest.Trigger.Schedule.Digest())

	var none *spec.Schedule
	assert.False(t, none.Digest(), "a routine with no schedule tells of each match")
}

func TestTheSentenceSaysADigest(t *testing.T) {
	t.Parallel()

	got := spec.Sentence(spec.Normalize(scheduled(t, catalog.ScheduleDigest)), nil)
	assert.Contains(t, got, "Every weekday at 08:30, once with the list, for pull requests")

	got = spec.Sentence(spec.Normalize(scheduled(t, "")), nil)
	assert.Contains(t, got, "Every weekday at 08:30, for pull requests")
	assert.NotContains(t, got, "once with the list")
}

func TestAScriptStepNamesItsScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		script spec.ScriptStep
		want   []string
	}{
		{name: "a script", script: spec.ScriptStep{ID: 3, Name: "Lint"}},
		{name: "no script", script: spec.ScriptStep{Name: "Lint"}, want: []string{"steps[1].script.id"}},
		{name: "no name", script: spec.ScriptStep{ID: 3}, want: []string{"steps[1].script.name"}},
		{
			name: "a name on two lines", script: spec.ScriptStep{ID: 3, Name: "Lint\nall"},
			want: []string{"steps[1].script.name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := scheduled(t, "")
			s.Steps[1] = spec.Step{Script: &tt.script}

			assert.Equal(t, tt.want, problemPaths(t, s))
		})
	}
}

func TestAScriptStepReadsAsOneStep(t *testing.T) {
	t.Parallel()

	s := scheduled(t, "")
	s.Steps[1] = spec.Step{Script: &spec.ScriptStep{ID: 3, Name: `  Lint "all"  `}}

	n := spec.Normalize(s)
	require.NotNil(t, n.Steps[1].Script)
	assert.Equal(t, spec.ScriptStep{ID: 3, Name: `Lint "all"`}, *n.Steps[1].Script)
	assert.Equal(t, catalog.StepScript, n.Steps[1].Kind())
	assert.Contains(t, spec.Sentence(n, nil), `notify me, then run my script "Lint \"all\""`)

	data, err := spec.Encode(n)
	require.NoError(t, err)

	back, err := spec.Load(data)
	require.NoError(t, err)
	assert.Equal(t, n, back, "a script step comes back as it was written")
}

func TestAScriptStepActsOnIssuesToo(t *testing.T) {
	t.Parallel()

	s := loadTemplate(t, "triage-busy-issues")
	s.Steps = append(s.Steps, spec.Step{Script: &spec.ScriptStep{ID: 1, Name: "Tidy"}})

	assert.Empty(t, problemPaths(t, s))
}
