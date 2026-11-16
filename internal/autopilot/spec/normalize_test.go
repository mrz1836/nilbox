package spec_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
)

func TestNormalize(t *testing.T) {
	t.Parallel()

	in := spec.Spec{
		Version: spec.Version,
		Watch: spec.Watch{Sources: []spec.Source{
			{Lists: []catalog.ListSource{catalog.SourceAssigned, " reviewRequested ", catalog.SourceAssigned}},
			{Search: &watch.Search{
				Kind: " PR ", Authors: []string{"Dependabot[bot]", " @me "}, Where: []string{"org:Acme-Org", "@me", "org:acme-org"},
				Labels: []string{" Bug ", "bug", "help wanted"}, More: "  Language:go  label:x ",
			}},
		}},
		Trigger: spec.Trigger{Event: " labeled ", Label: " bug "},
		Conditions: spec.Conditions{Of: []spec.Clause{
			{Field: catalog.FieldUpdateType, Op: catalog.OpOneOf, Values: []string{"PATCH", "major", "patch"}},
			{Field: " item.title ", Op: catalog.OpContains, Value: " fix "},
			{Field: catalog.FieldUpdateType, Op: catalog.OpOneOf, Values: []string{"major", "patch"}},
		}},
		Steps: []spec.Step{
			{Agent: &spec.AgentStep{
				Prompt: spec.PromptRef{Starter: " Fix-Checks "},
				LaunchFor: []spec.LaunchRow{
					{Target: "Acme-Org/API", Model: "opus"}, {Target: "acme-labs", Model: " sonnet "},
				},
				Fallbacks: []spec.ProfileRef{{Agent: " codex ", Profile: " work "}, {Agent: "claude", Profile: "home"}},
				Fence:     &spec.Fence{Allow: []string{"pnpm test", " pnpm test ", "go test ./..."}},
			}},
			{Notify: &spec.NotifyStep{Note: "  hello  "}, OnlyIf: " notPushed "},
		},
		Limits: spec.Limits{ActiveHours: &spec.ActiveHours{
			Days: []catalog.Weekday{catalog.Friday, catalog.Monday, catalog.Friday}, From: " 08:00", To: "18:00 ",
		}},
		Notify:   &spec.Notices{},
		Requires: []string{"pnpm", "go", " pnpm "},
	}

	got := spec.Normalize(in)

	assert.Equal(t, []catalog.ListSource{catalog.SourceReviewRequested, catalog.SourceAssigned}, got.Watch.Sources[0].Lists,
		"lists in the catalog's order, once each")

	search := got.Watch.Sources[1].Search
	require.NotNil(t, search)
	assert.Equal(t, watch.Search{
		Kind: "pr", Authors: []string{"@me", "app/dependabot"}, Where: []string{"@me", "org:acme-org"},
		Labels: []string{"Bug", "help wanted"}, More: "label:x language:go",
	}, *search)

	assert.Equal(t, catalog.EventLabeled, got.Trigger.Event)
	assert.Equal(t, "bug", got.Trigger.Label)

	assert.Empty(t, got.Conditions.Match, "a list of conditions says how it combines; Normalize doesn't guess")
	assert.Equal(t, []spec.Clause{
		{Field: catalog.FieldUpdateType, Op: catalog.OpOneOf, Values: []string{"major", "patch"}},
		{Field: catalog.FieldTitle, Op: catalog.OpContains, Value: "fix"},
	}, got.Conditions.Of, "choices in the catalog's order and spelling, and a repeated condition kept once")

	agent := got.Steps[0].Agent
	require.NotNil(t, agent)
	assert.Equal(t, catalog.Inherit, agent.Agent)
	assert.Equal(t, "fix-checks", agent.Prompt.Starter)
	assert.Equal(t, []spec.LaunchRow{
		{Target: "acme-labs", Model: "sonnet"}, {Target: "acme-org/api", Model: "opus"},
	}, agent.LaunchFor, "rows by target")
	assert.Equal(t, []spec.ProfileRef{{Agent: "codex", Profile: "work"}, {Agent: "claude", Profile: "home"}},
		agent.Fallbacks, "the fallbacks' order is the user's")
	assert.Equal(t, []string{"go test ./...", "pnpm test"}, agent.Fence.Allow)

	assert.Equal(t, "hello", got.Steps[1].Notify.Note)
	assert.Equal(t, catalog.OutcomeNotPushed, got.Steps[1].OnlyIf)

	assert.Equal(t, &spec.ActiveHours{
		Days: []catalog.Weekday{catalog.Monday, catalog.Friday}, From: "08:00", To: "18:00",
	}, got.Limits.ActiveHours)
	assert.Nil(t, got.Notify, "notices that set nothing are left out")
	assert.Equal(t, []string{"go", "pnpm"}, got.Requires)

	assert.Equal(t, got, spec.Normalize(got), "normalizing twice changes nothing")
	assert.Equal(t, catalog.Event(" labeled "), in.Trigger.Event, "the input is left as it was")
}

func TestNormalizeKeepsTheUsersOrder(t *testing.T) {
	t.Parallel()

	s := loadTemplate(t, "weekday-nudge")

	assert.Equal(t, catalog.FieldIdle, s.Conditions.Of[0].Field, "conditions stay where the user put them")
	assert.NotNil(t, s.Conditions.Of[1].Group)
	assert.Equal(t, catalog.StepNotify, s.Steps[0].Kind(), "steps stay in the order they run")
	assert.Equal(t, catalog.StepPin, s.Steps[1].Kind())
}

// TestNormalizeKeepsRegexesApartByCase checks that two regexes that differ only in case
// stay two conditions: \d and \D say opposite things.
func TestNormalizeKeepsRegexesApartByCase(t *testing.T) {
	t.Parallel()

	digit := spec.Clause{Field: catalog.FieldTitle, Op: catalog.OpMatchesRegex, Value: `\d`}
	other := spec.Clause{Field: catalog.FieldTitle, Op: catalog.OpMatchesRegex, Value: `\D`}

	got := spec.Normalize(spec.Spec{Conditions: spec.Conditions{Match: spec.MatchAny, Of: []spec.Clause{digit, other}}})
	assert.Equal(t, []spec.Clause{digit, other}, got.Conditions.Of)

	same := spec.Clause{Field: catalog.FieldTitle, Op: catalog.OpMatchesRegex, Value: `\d`}
	got = spec.Normalize(spec.Spec{Conditions: spec.Conditions{Match: spec.MatchAny, Of: []spec.Clause{digit, same}}})
	assert.Equal(t, []spec.Clause{digit}, got.Conditions.Of, "the same regex twice is kept once")
}

func TestNormalizeGivesEmptyConditionsAMatch(t *testing.T) {
	t.Parallel()

	got := spec.Normalize(spec.Spec{})
	assert.Equal(t, spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{}}, got.Conditions)
	assert.Equal(t, []spec.Step{}, got.Steps)
}
