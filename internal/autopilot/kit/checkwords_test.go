package kit_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/item"
)

func TestTheWordsOfACheckAreWhatTheAgentReads(t *testing.T) {
	t.Parallel()

	snap := checkSnapshot()

	problems := kit.Check(t.Context(), snap, kit.Proposal{
		Summary: "Fix flaky checks, and loosen the day's limits.",
		Prompts: []kit.NewPrompt{
			agentPrompt("Fix flaky", "Fix {{.Body}} on {{.URL}}."),
			agentPrompt("Triage", "Look."),
		},
		Routines: []kit.RoutineChange{
			newRoutine("Nightly", searchOnly(checksFailSpec(
				agentStep(spec.PromptRef{Nickname: "Fix flaky"}, catalog.WhereBackground, ""),
				spec.Step{Pin: &spec.Empty{}},
			))),
			{ID: 7, Name: "Nightly", Spec: checksFailSpec(agentStep(spec.PromptRef{Nickname: "Nope"}, "", ""))},
			changeOf(404, "Gone", checksFailSpec(notifyStep())),
			{Name: "", Spec: spec.Spec{Version: 2}},
		},
		Settings: &kit.SettingsChange{AgentsAtOnce: someNumber(3), HistoryDays: someNumber(5)},
		Rules:    &kit.RulesChange{Trust: []string{renovateLogin, "no such bot"}, Mute: []string{"acme-org/legacy"}},
	})
	checkGolden(t, "checked/problems.golden", problems.Words())

	// Each part is numbered by its place in the proposal's own lists, whatever its problems,
	// which the window reads a part of the proposal by.
	require.Len(t, problems.Routines, 4)
	require.Len(t, problems.Prompts, 2)

	for i, r := range problems.Routines {
		assert.Equal(t, i, r.Index)
	}

	for i, p := range problems.Prompts {
		assert.Equal(t, i, p.Index)
	}

	clean := kit.Check(t.Context(), snap, kit.Proposal{
		Summary: "Tell me when my checks fail, and cap the day.",
		Routines: []kit.RoutineChange{
			newRoutine("Tell me about checks", checksFailSpec(notifyStep())),
			changeOf(9, "Fix checks", checksFailSpec(
				agentStep(spec.PromptRef{Starter: fixChecksKey}, catalog.WhereBackground, catalog.PresetEditTest),
			)),
		},
		Settings: &kit.SettingsChange{DailyCap: &autopilotprefs.DailyCap{Amount: 5, Unit: autopilotprefs.UnitUSD}},
	})
	checkGolden(t, "checked/clean.golden", clean.Words())

	one := kit.Check(t.Context(), snap, kit.Proposal{
		Summary:  "Tells you when checks fail.",
		Routines: []kit.RoutineChange{newRoutine(strings.Repeat("n", 81), checksFailSpec(notifyStep()))},
	})
	assert.Equal(t, "Checked 1 routine: 1 problem.", strings.SplitN(one.Words(), "\n", 2)[0],
		"the first sentence, which the window shows as the check's line")

	assert.Equal(t, "Checked nothing: the proposal holds no part.\n\nIt has no problems: it can be proposed with "+
		"autopilot_propose, and the user reviews it in the Autopilot tab.", kit.Check(t.Context(), snap, kit.Proposal{}).Words())
}

// searchOnly returns sp watching only a search, of the user's account's pull requests.
func searchOnly(sp spec.Spec) spec.Spec {
	sp.Watch = spec.Watch{Sources: []spec.Source{{Search: &watch.Search{Kind: item.KindPR, Where: []string{"@me"}}}}}

	return sp
}

func TestTheWordsOfACheckSayAPartThatChangesNothing(t *testing.T) {
	t.Parallel()

	words := kit.Check(t.Context(), checkSnapshot(), kit.Proposal{
		Summary:  "Nothing new.",
		Prompts:  []kit.NewPrompt{agentPrompt("", "Look.")},
		Settings: &kit.SettingsChange{AgentsAtOnce: someNumber(2)},
		Rules:    &kit.RulesChange{Trust: []string{dependabotLogin}},
	}).Words()

	assert.Contains(t, words, "\nPrompt 1, (no name):\n")
	assert.Contains(t, words, "\nSettings: no change.\n- Problem at settings: It changes nothing: each setting it names is already so.\n")
	assert.Contains(t, words, "\nRules: no change.\n- Problem: It changes nothing: each rule it names is already so.\n")
}

func TestTheWordsOfACheckStayWithinAToolsResult(t *testing.T) {
	t.Parallel()

	many := make([]kit.RoutineChange, 0, kit.MaxRoutines)
	for range kit.MaxRoutines {
		sp := checksFailSpec(notifyStep())
		sp.Conditions = spec.Conditions{Match: spec.MatchAll}

		for j := range spec.MaxConditions {
			sp.Conditions.Of = append(sp.Conditions.Of, spec.Clause{
				Field: catalog.FieldID(fmt.Sprintf("no.such.field%d.%s", j, strings.Repeat("x", 100))), Op: catalog.OpIs, Value: "v",
			})
		}

		many = append(many, newRoutine(strings.Repeat("n", 200), sp))
	}

	words := kit.Check(t.Context(), checkSnapshot(), kit.Proposal{Summary: "Many.", Routines: many}).Words()

	assert.LessOrEqual(t, len(words), 48<<10, "well under what a tool's result may be")
	assert.Contains(t, words, "… (the rest is cut: fix what is above, then check it again)")
	assert.True(t, strings.HasSuffix(words, "A proposal with a problem can't be proposed."), "the closing line is kept")
}
