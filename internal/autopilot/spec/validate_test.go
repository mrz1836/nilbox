package spec_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/item"
)

// problemPaths validates s and returns the path of each problem found.
func problemPaths(t *testing.T, s spec.Spec) []string {
	t.Helper()

	err := spec.Validate(s)
	if err == nil {
		return nil
	}

	var verr *spec.ValidationError

	require.ErrorAs(t, err, &verr)
	require.ErrorIs(t, err, spec.ErrInvalid)

	paths := make([]string, 0, len(verr.Problems))
	for _, p := range verr.Problems {
		assert.NotEmpty(t, p.Message, p.Path())

		paths = append(paths, p.Path())
	}

	return paths
}

// agentStep returns an agent step in the background under preset.
func agentStep(preset catalog.Preset) spec.Step {
	return spec.Step{Agent: &spec.AgentStep{
		Prompt: spec.PromptRef{Starter: "fix-checks"}, Fence: &spec.Fence{Tools: preset},
	}}
}

// withSteps returns the flagship with its steps replaced.
func withSteps(t *testing.T, steps ...spec.Step) spec.Spec {
	t.Helper()

	s := loadTemplate(t, flagship)
	s.Steps = steps

	return s
}

// TestValidateNamesThePartAndTheField checks that each problem names where it is, by
// breaking the flagship one field at a time.
func TestValidateNamesThePartAndTheField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*spec.Spec)
		want   []string
	}{
		{name: "an older version", mutate: func(s *spec.Spec) { s.Version = 0 }, want: []string{"version"}},
		{
			name: "no source", mutate: func(s *spec.Spec) { s.Watch.Sources = nil }, want: []string{"watch.sources"},
		},
		{
			name: "a search with no scope",
			mutate: func(s *spec.Spec) {
				s.Watch.Sources[0].Search.Where = nil
				s.Watch.Sources[0].Search.Authors = []string{"app/dependabot"}
			},
			want: []string{"watch.sources[0].search.where"},
		},
		{
			name:   "a label that would close its quote",
			mutate: func(s *spec.Spec) { s.Watch.Sources[0].Search.Labels = []string{`bug" author:x`} },
			want:   []string{"watch.sources[0].search.labels[0]"},
		},
		{
			name: "a source that is lists and a search",
			mutate: func(s *spec.Spec) {
				s.Watch.Sources[0].Lists = []catalog.ListSource{catalog.SourceAuthored}
			},
			want: []string{"watch.sources[0]"},
		},
		{
			name:   "an unknown inbox list",
			mutate: func(s *spec.Spec) { s.Watch.Sources = []spec.Source{{Lists: []catalog.ListSource{"starred"}}} },
			want:   []string{"watch.sources[0].lists[0]"},
		},
		{name: "an unknown trigger", mutate: func(s *spec.Spec) { s.Trigger.Event = "closed" }, want: []string{"trigger.event"}},
		{
			name:   "an option the trigger doesn't take",
			mutate: func(s *spec.Spec) { s.Trigger.Label = "bug" },
			want:   []string{"trigger.label"},
		},
		{
			name:   "a settle time out of range",
			mutate: func(s *spec.Spec) { s.Trigger.SettleMinutes = ptr(61) },
			want:   []string{"trigger.settleMinutes"},
		},
		{
			name: "a pull request's trigger on issues",
			mutate: func(s *spec.Spec) {
				s.Watch.Sources[0].Search.Kind = item.KindIssue
				s.Conditions = spec.Conditions{Match: spec.MatchAll}
				s.Steps = []spec.Step{{Notify: &spec.NotifyStep{}}}
			},
			want: []string{"trigger.event"},
		},
		{
			name:   "an unknown field",
			mutate: func(s *spec.Spec) { s.Conditions.Of[0].Field = "checks.flaky" },
			want:   []string{"conditions.of[0].field"},
		},
		{
			name:   "an operator the field doesn't take",
			mutate: func(s *spec.Spec) { s.Conditions.Of[0].Op = catalog.OpGt },
			want:   []string{"conditions.of[0].op"},
		},
		{
			name: "one value for an operator that takes several",
			mutate: func(s *spec.Spec) {
				s.Conditions.Of[0].Values = nil
				s.Conditions.Of[0].Value = "security"
			},
			want: []string{"conditions.of[0].value", "conditions.of[0].values"},
		},
		{
			name: "a regex that doesn't compile",
			mutate: func(s *spec.Spec) {
				s.Conditions.Of = []spec.Clause{{Field: catalog.FieldTitle, Op: catalog.OpMatchesRegex, Value: "(bump"}}
			},
			want: []string{"conditions.of[0].value"},
		},
		{
			name: "a number that isn't one",
			mutate: func(s *spec.Spec) {
				s.Conditions.Of = []spec.Clause{{Field: catalog.FieldLines, Op: catalog.OpLt, Value: "four hundred"}}
			},
			want: []string{"conditions.of[0].value"},
		},
		{
			name: "a length without a unit",
			mutate: func(s *spec.Spec) {
				s.Conditions.Of = []spec.Clause{{Field: catalog.FieldIdle, Op: catalog.OpMoreThan, Value: "14"}}
			},
			want: []string{"conditions.of[0].value"},
		},
		{
			name: "a value that is no choice",
			mutate: func(s *spec.Spec) {
				s.Conditions.Of = []spec.Clause{{Field: catalog.FieldTrust, Op: catalog.OpOneOf, Values: []string{"friend"}}}
			},
			want: []string{"conditions.of[0].values[0]"},
		},
		{
			name: "a path pattern that matches every file",
			mutate: func(s *spec.Spec) {
				s.Conditions.Of = []spec.Clause{{Field: catalog.FieldPaths, Op: catalog.OpAnyMatches, Values: []string{"*"}}}
			},
			want: []string{"conditions.of[0].values[0]"},
		},
		{
			name: "a group in a group",
			mutate: func(s *spec.Spec) {
				inner := spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{s.Conditions.Of[0]}}
				s.Conditions.Of = []spec.Clause{{Group: &spec.Conditions{
					Match: spec.MatchAny, Of: []spec.Clause{{Group: &inner}},
				}}}
			},
			want: []string{"conditions.of[0].group.of[0].group"},
		},
		{
			name:   "an empty group",
			mutate: func(s *spec.Spec) { s.Conditions.Of = []spec.Clause{{Group: &spec.Conditions{Match: spec.MatchAny}}} },
			want:   []string{"conditions.of[0].group.of"},
		},
		{name: "no steps", mutate: func(s *spec.Spec) { s.Steps = nil }, want: []string{"steps"}},
		{
			name:   "a step of two kinds",
			mutate: func(s *spec.Spec) { s.Steps[2].Pin = &spec.Empty{} },
			want:   []string{"steps[2]"},
		},
		{
			name: "a budget out of range",
			mutate: func(s *spec.Spec) {
				s.Steps[0].Agent.Fence.Budget = &spec.Budget{USD: ptr(80.0)}
			},
			want: []string{"steps[0].agent.fence.budget.usd"},
		},
		{
			name: "a budget in both units",
			mutate: func(s *spec.Spec) {
				s.Steps[0].Agent.Fence.Budget = &spec.Budget{USD: ptr(3.0), Tokens: ptr(int64(50_000))}
			},
			want: []string{"steps[0].agent.fence.budget"},
		},
		{
			name:   "an unknown tools preset",
			mutate: func(s *spec.Spec) { s.Steps[0].Agent.Fence.Tools = "everything" },
			want: []string{
				"steps[0].agent.fence.tools", "steps[1].onlyIf", "steps[1]",
			},
		},
		{
			name:   "no prompt",
			mutate: func(s *spec.Spec) { s.Steps[0].Agent.Prompt = spec.PromptRef{} },
			want:   []string{"steps[0].agent.prompt"},
		},
		{
			name:   "tries out of range",
			mutate: func(s *spec.Spec) { s.Limits.TriesPerItem = ptr(11) },
			want:   []string{"limits.triesPerItem"},
		},
		{
			name:   "an unknown once-per",
			mutate: func(s *spec.Spec) { s.Limits.OncePer = "week" },
			want:   []string{"limits.oncePer"},
		},
		{
			name: "active hours that start when they end",
			mutate: func(s *spec.Spec) {
				s.Limits.ActiveHours = &spec.ActiveHours{Days: []catalog.Weekday{catalog.Monday}, From: "08:00", To: "08:00"}
			},
			want: []string{"limits.activeHours.to"},
		},
		{
			name:   "a tool that isn't a program's name",
			mutate: func(s *spec.Spec) { s.Requires = []string{"rm -rf"} },
			want:   []string{"requires[0]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := loadTemplate(t, flagship)
			tt.mutate(&s)

			assert.Equal(t, tt.want, problemPaths(t, s))
		})
	}
}

// TestValidateHoldsTheStepsToTheirOrder checks the steps' order rules: one agent step;
// Wait for the checks after an agent step that may push; Merge it after a Wait for the
// checks, only if they passed; Post the agent's review after a read-only agent step;
// Allow approving only with a verdict; and only a notice after a terminal run.
func TestValidateHoldsTheStepsToTheirOrder(t *testing.T) {
	t.Parallel()

	push, readOnly := agentStep(catalog.PresetEditTestPush), agentStep(catalog.PresetReadOnly)
	terminal := spec.Step{Agent: &spec.AgentStep{Prompt: spec.PromptRef{Starter: "fix-checks"}, Where: catalog.WhereTerminal}}
	wait := spec.Step{WaitForChecks: &spec.WaitStep{}, OnlyIf: catalog.OutcomePushed}
	merge := spec.Step{Merge: &spec.MergeStep{How: catalog.MergeWhenMergeable}, OnlyIf: catalog.OutcomePassedChecks}
	tell := spec.Step{TellMe: &spec.TellStep{}}

	tests := []struct {
		name  string
		steps []spec.Step
		want  []string
	}{
		{name: "fix, wait, merge, and tell", steps: []spec.Step{push, wait, merge, {TellMe: &spec.TellStep{}, OnlyIf: catalog.OutcomeFailingChecks}}},
		{name: "two agent steps", steps: []spec.Step{push, tell, readOnly}, want: []string{"steps[2]"}},
		{name: "a wait with no agent before it", steps: []spec.Step{{WaitForChecks: &spec.WaitStep{}}}, want: []string{"steps[0]"}},
		{name: "a wait after an agent that can't push", steps: []spec.Step{readOnly, {WaitForChecks: &spec.WaitStep{}}}, want: []string{"steps[1]"}},
		{name: "a merge with no wait before it", steps: []spec.Step{push, merge}, want: []string{"steps[1].onlyIf", "steps[1]"}},
		{
			name: "a merge that doesn't wait for a pass", steps: []spec.Step{push, wait, {Merge: &spec.MergeStep{How: catalog.MergeWhenMergeable}}},
			want: []string{"steps[2].onlyIf"},
		},
		{
			name:  "a merge after the checks still fail",
			steps: []spec.Step{push, wait, {Merge: &spec.MergeStep{How: catalog.MergeWhenMergeable}, OnlyIf: catalog.OutcomeFailingChecks}},
			want:  []string{"steps[2].onlyIf"},
		},
		{name: "a review after a read-only agent", steps: []spec.Step{readOnly, {PostReview: &spec.ReviewStep{As: catalog.ReviewAsVerdict, AllowApprove: true}}}},
		{
			name: "a review after an agent that may push", steps: []spec.Step{push, {PostReview: &spec.ReviewStep{As: catalog.ReviewAsComment}}},
			want: []string{"steps[1]"},
		},
		{
			name:  "approving with a review posted as a comment",
			steps: []spec.Step{readOnly, {PostReview: &spec.ReviewStep{As: catalog.ReviewAsComment, AllowApprove: true}}},
			want:  []string{"steps[1].postReview.allowApprove"},
		},
		{
			name:  "a verdict's only if after a read-only agent",
			steps: []spec.Step{readOnly, {Notify: &spec.NotifyStep{}, OnlyIf: catalog.OutcomeRequestChanges}},
		},
		{
			name:  "a verdict's only if after an agent that may push",
			steps: []spec.Step{push, {Notify: &spec.NotifyStep{}, OnlyIf: catalog.OutcomeApprove}},
			want:  []string{"steps[1].onlyIf"},
		},
		{name: "whether it pushed, after one that can't push", steps: []spec.Step{readOnly, {Pin: &spec.Empty{}, OnlyIf: catalog.OutcomeNotPushed}}, want: []string{"steps[1].onlyIf"}},
		{name: "an only if on the first step", steps: []spec.Step{{Pin: &spec.Empty{}, OnlyIf: catalog.OutcomePushed}}, want: []string{"steps[0].onlyIf"}},
		{name: "an unknown only if", steps: []spec.Step{push, {Pin: &spec.Empty{}, OnlyIf: "maybe"}}, want: []string{"steps[1].onlyIf"}},
		{name: "a notice after a terminal run", steps: []spec.Step{terminal, tell}},
		{name: "a wait after a terminal run", steps: []spec.Step{terminal, {WaitForChecks: &spec.WaitStep{}}}, want: []string{"steps[1]"}},
		{
			name: "a fence on a terminal run",
			steps: []spec.Step{{Agent: &spec.AgentStep{
				Prompt: spec.PromptRef{Starter: "fix-checks"}, Where: catalog.WhereTerminal, Fence: &spec.Fence{},
			}}},
			want: []string{"steps[0].agent.fence"},
		},
		{
			name:  "plan mode in the background",
			steps: []spec.Step{{Agent: &spec.AgentStep{Prompt: spec.PromptRef{Starter: "fix-checks"}, PlanMode: ptr(true)}}},
			want:  []string{"steps[0].agent"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, problemPaths(t, withSteps(t, tt.steps...)))
		})
	}
}

func TestValidateGitHubSteps(t *testing.T) {
	t.Parallel()

	reply := spec.GitHubAction{Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Nickname: "Question"}}}

	tests := []struct {
		name    string
		actions []spec.GitHubAction
		kind    item.Kind
		want    []string
	}{
		{name: "a rebase", actions: []spec.GitHubAction{{Rebase: &spec.Empty{}}}},
		{name: "a reply and labels", actions: []spec.GitHubAction{reply, {Labels: &spec.LabelsAction{Add: []string{"needs-repro"}}}}},
		{name: "no action", actions: []spec.GitHubAction{}, want: []string{"steps[0].github"}},
		{name: "an action of two kinds", actions: []spec.GitHubAction{{Rebase: &spec.Empty{}, UpdateBranch: &spec.Empty{}}}, want: []string{"steps[0].github[0]"}},
		{name: "one action twice", actions: []spec.GitHubAction{reply, reply}, want: []string{"steps[0].github[1]"}},
		{
			name:    "a reply by the item's default prompt",
			actions: []spec.GitHubAction{{Reply: &spec.ReplyAction{Prompt: spec.PromptRef{ItemDefault: true}}}},
			want:    []string{"steps[0].github[0].reply.prompt"},
		},
		{
			name:    "a label added and removed",
			actions: []spec.GitHubAction{{Labels: &spec.LabelsAction{Add: []string{"bug"}, Remove: []string{"Bug"}}}},
			want:    []string{"steps[0].github[0].labels.remove"},
		},
		{name: "no labels", actions: []spec.GitHubAction{{Labels: &spec.LabelsAction{}}}, want: []string{"steps[0].github[0].labels"}},
		{
			name: "updating an issue's branch", actions: []spec.GitHubAction{{UpdateBranch: &spec.Empty{}}}, kind: item.KindIssue,
			want: []string{"steps[0].github[0]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := withSteps(t, spec.Step{GitHub: tt.actions})
			s.Trigger = spec.Trigger{Event: catalog.EventAppears}
			s.Conditions = spec.Conditions{Match: spec.MatchAll}

			if tt.kind != "" {
				s.Watch.Sources[0].Search.Kind = tt.kind
			}

			assert.Equal(t, tt.want, problemPaths(t, s))
		})
	}
}

func TestValidateAllowsAManualRoutineWithNoWatch(t *testing.T) {
	t.Parallel()

	s := withSteps(t, spec.Step{Pin: &spec.Empty{}})
	s.Watch = spec.Watch{}
	s.Trigger = spec.Trigger{Event: catalog.EventManual}

	assert.Empty(t, problemPaths(t, s))
	assert.Equal(t, item.Kinds(), s.Watch.Kinds())
}

func TestValidationErrorSaysTheFirstProblem(t *testing.T) {
	t.Parallel()

	s := loadTemplate(t, flagship)
	s.Limits.TriesPerItem = ptr(0)
	s.Limits.RunsPerDay = ptr(0)

	err := spec.Validate(s)
	require.Error(t, err)
	assert.Equal(t, "limits.triesPerItem: pick 1 to 10 (and 1 more problem)", err.Error())
}

func TestValidateRefusesASearchTooLongForAnyAccount(t *testing.T) {
	t.Parallel()

	s := loadTemplate(t, flagship)
	s.Watch.Sources[0].Search.More = "language:go label:alpha label:bravo label:charlie label:delta label:echo " +
		"label:foxtrot label:golf label:hotel label:india label:juliet label:kilo"

	_, err := watch.Build(*s.Watch.Sources[0].Search, "me")
	require.NoError(t, err, "it fits with a short login")
	assert.Equal(t, []string{"watch.sources[0].search.more"}, problemPaths(t, s),
		"Validate holds it to the longest login, so it fits whichever account signs in")
}
