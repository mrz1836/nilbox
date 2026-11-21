package spec_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/item"
)

// TestSentenceGolden compares each template's sentence with its golden file under
// testdata/sentences. After a deliberate change to the wording, rewrite them with
// go test ./internal/autopilot/spec -run TestSentenceGolden -update, and review the diff.
func TestSentenceGolden(t *testing.T) {
	t.Parallel()

	for _, name := range templateNames(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := spec.Sentence(loadTemplate(t, name), starterNames)

			golden(t, filepath.Join("testdata", "sentences", name+".golden"),
				"# The sentence spec.Sentence writes for testdata/templates/"+name+".json. Rewrite with\n"+
					"# go test ./internal/autopilot/spec -run TestSentenceGolden -update\n\n",
				got+"\n")
		})
	}
}

// TestSentenceSaysEveryTrigger checks how the sentence opens for each trigger, on a
// search for Dependabot's pull requests in acme-org.
func TestSentenceSaysEveryTrigger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		trigger spec.Trigger
		want    string
	}{
		{spec.Trigger{Event: catalog.EventAppears}, "When a pull request by Dependabot in acme-org appears, pin it."},
		{
			spec.Trigger{Event: catalog.EventNewCommits, FirstToo: new(false), OwnPushesToo: new(true)},
			"When a pull request by Dependabot in acme-org gets new commits, after the first one, a routine's own " +
				"included, pin it.",
		},
		{
			spec.Trigger{Event: catalog.EventChecksFailed, WaitForChecks: new(false)},
			"When checks fail on pull requests by Dependabot in acme-org, pin it.",
		},
		{
			spec.Trigger{Event: catalog.EventChecksFailed, SettleMinutes: new(0)},
			"When checks fail on pull requests by Dependabot in acme-org, once every check has finished, pin it.",
		},
		{spec.Trigger{Event: catalog.EventChecksPassed}, "When checks pass on pull requests by Dependabot in acme-org, pin it."},
		{
			spec.Trigger{Event: catalog.EventChangesRequested},
			"When changes are requested on pull requests by Dependabot in acme-org, pin it.",
		},
		{spec.Trigger{Event: catalog.EventApproved}, "When a pull request by Dependabot in acme-org is approved, pin it."},
		{
			spec.Trigger{Event: catalog.EventMergeConflicts},
			"When a pull request by Dependabot in acme-org has merge conflicts, pin it.",
		},
		{
			spec.Trigger{Event: catalog.EventFallsBehind},
			"When a pull request by Dependabot in acme-org falls behind its base branch, pin it.",
		},
		{
			spec.Trigger{Event: catalog.EventLabeled, Label: "needs review"},
			`When a pull request by Dependabot in acme-org is labeled "needs review", pin it.`,
		},
		{
			spec.Trigger{Event: catalog.EventReadyForReview},
			"When a pull request by Dependabot in acme-org is ready for review, pin it.",
		},
		{
			spec.Trigger{Event: catalog.EventReviewRequested},
			"When a pull request by Dependabot in acme-org asks for your review, pin it.",
		},
		{spec.Trigger{Event: catalog.EventAssigned}, "When a pull request by Dependabot in acme-org is assigned to you, pin it."},
		{spec.Trigger{Event: catalog.EventNewComments}, "When a pull request by Dependabot in acme-org gets a new comment, pin it."},
		{
			spec.Trigger{Event: catalog.EventNewComments, Count: new(5)},
			"When a pull request by Dependabot in acme-org gets 5 new comments, pin it.",
		},
		{
			spec.Trigger{Event: catalog.EventGoesQuiet},
			"When a pull request by Dependabot in acme-org goes quiet for 7 days, pin it.",
		},
		{
			spec.Trigger{Event: catalog.EventSchedule, Schedule: &spec.Schedule{Every: catalog.EveryWeek, On: catalog.Monday, At: "09:00"}},
			"Every Monday at 09:00, for pull requests by Dependabot in acme-org, pin it.",
		},
		{
			spec.Trigger{Event: catalog.EventSchedule, Schedule: &spec.Schedule{Every: catalog.EveryDay, At: "18:15"}},
			"Every day at 18:15, for pull requests by Dependabot in acme-org, pin it.",
		},
		{spec.Trigger{Event: catalog.EventManual}, "When you run it on a pull request by Dependabot in acme-org, pin it."},
	}

	for _, tt := range tests {
		t.Run(string(tt.trigger.Event), func(t *testing.T) {
			t.Parallel()

			s := spec.Spec{
				Version: spec.Version,
				Watch: spec.Watch{Sources: []spec.Source{{Search: &watch.Search{
					Kind: item.KindPR, Authors: []string{"app/dependabot"}, Where: []string{"org:acme-org"},
				}}}},
				Trigger: tt.trigger, Conditions: spec.Conditions{Match: spec.MatchAll},
				Steps: []spec.Step{{Pin: &spec.Empty{}}},
			}

			assert.Equal(t, tt.want, spec.Sentence(s, nil))
		})
	}
}

// TestPhraseSaysEveryType checks a condition's words for each type of field.
func TestPhraseSaysEveryType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		clause spec.Clause
		want   string
	}{
		{spec.Clause{Field: catalog.FieldTitle, Op: catalog.OpStartsWith, Value: "chore(deps)"}, "the title starts with chore(deps)"},
		{spec.Clause{Field: catalog.FieldTitle, Op: catalog.OpNotContains, Value: "do not merge"}, `the title doesn't contain "do not merge"`},
		{
			spec.Clause{Field: catalog.FieldFailedNames, Op: catalog.OpNoneContains, Values: []string{"lint", "test"}},
			"no failed check's name contains lint or test",
		},
		{
			spec.Clause{Field: catalog.FieldPaths, Op: catalog.OpAllMatch, Values: []string{"docs/", "*.md"}},
			"every changed path matches docs/ or *.md",
		},
		{
			spec.Clause{Field: catalog.FieldLabels, Op: catalog.OpHasAll, Values: []string{"bug", "help wanted"}},
			`the labels include bug and "help wanted"`,
		},
		{
			spec.Clause{Field: catalog.FieldLists, Op: catalog.OpHasAny, Values: []string{"reviewRequested", "assigned"}},
			"the reasons it's in your inbox include review requested or assigned to you",
		},
		{spec.Clause{Field: catalog.FieldSensitive, Op: catalog.OpNotEmpty}, "a sensitive file changed"},
		{spec.Clause{Field: catalog.FieldApprovals, Op: catalog.OpGte, Value: "2"}, "the approvals are at least 2"},
		{spec.Clause{Field: catalog.FieldDraft, Op: catalog.OpIsYes}, "it's a draft"},
		{spec.Clause{Field: catalog.FieldRepoMuted, Op: catalog.OpIsNo}, "the repository isn't muted"},
		{
			spec.Clause{Field: catalog.FieldTrust, Op: catalog.OpNoneOf, Values: []string{"outside"}},
			"the author isn't an outside contributor",
		},
		{spec.Clause{Field: catalog.FieldKind, Op: catalog.OpOneOf, Values: []string{"pr"}}, "it's a pull request"},
		{spec.Clause{Field: catalog.FieldAge, Op: catalog.OpLessThan, Value: "2h"}, "it's been open for less than 2 hours"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, spec.Phrase(tt.clause))
		})
	}
}

func TestSentenceNamesAPromptWithoutTheLibrary(t *testing.T) {
	t.Parallel()

	s := loadTemplate(t, flagship)
	assert.Contains(t, spec.Sentence(s, nil), `run "Fix dependency security" in the background`,
		"a starter's key reads as words when nothing names it")
	assert.Contains(t, spec.Sentence(s, func(spec.PromptRef) string { return "Patch it" }), `run "Patch it"`)
}
