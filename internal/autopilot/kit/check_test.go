package kit_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// checkedRoutine checks a proposal of routines against the fixtures' snapshot and returns
// its routines.
func checkedRoutines(t *testing.T, routines ...kit.RoutineChange) []kit.CheckedRoutine {
	t.Helper()

	c := kit.Check(t.Context(), checkSnapshot(), kit.Proposal{Summary: "Routines.", Routines: routines})
	require.Len(t, c.Routines, len(routines))

	return c.Routines
}

// problemPaths returns where each of r's problems is: its own field's name, or its spec's
// path.
func problemPaths(r kit.CheckedRoutine) []string {
	out := make([]string, 0, len(r.Problems))
	for _, p := range r.Problems {
		out = append(out, p.Path()+"="+p.Message)
	}

	return out
}

func TestANewRoutineChecksAsTheBuilderReadsADraft(t *testing.T) {
	t.Parallel()

	r := checkedRoutines(t, kit.RoutineChange{
		Name: "  Fix my checks  ", Description: "Fixes them.", Template: "fix-failing-checks-on-my-prs",
		Mode: autopilotprefs.ModeAuto,
		Spec: checksFailSpec(agentStep(spec.PromptRef{Starter: fixChecksKey}, catalog.WhereTerminal, "")),
	})[0]

	assert.Empty(t, r.Problems)
	assert.Equal(t, "Fix my checks", r.Name, "trimmed, as a save stores it")
	assert.False(t, r.NameTaken)
	assert.Nil(t, r.Before)
	assert.False(t, r.Gone)
	assert.False(t, r.Stale)
	assert.Contains(t, r.Sentence, `run "Fix checks" in your terminal`, "a starter is named by the library's nickname")
	assert.Empty(t, r.Warnings, "a new routine runs in the mode new routines start in, never the agent's suggestion")
	assert.Equal(t, spec.Version, r.Spec.Version, "its spec as a save stores it")
	assert.NotNil(t, r.DryRunOnly)

	snap := checkSnapshot()
	snap.Setup.Settings.DefaultMode = autopilotprefs.ModeAuto
	c := kit.Check(t.Context(), snap, kit.Proposal{Summary: "x", Routines: []kit.RoutineChange{{
		Name: "Fix my checks", Spec: checksFailSpec(agentStep(spec.PromptRef{Starter: fixChecksKey}, catalog.WhereTerminal, "")),
	}}})
	require.Len(t, c.Routines[0].Warnings, 1, "Settings' mode decides its warnings")
	assert.Equal(t, "steps[0].agent.where", c.Routines[0].Warnings[0].Path())
}

func TestARoutinesOwnFieldsAreCheckedAsSaveChecksThem(t *testing.T) {
	t.Parallel()

	good := checksFailSpec(notifyStep())

	for name, c := range map[string]struct {
		change kit.RoutineChange
		want   []string
	}{
		"no name": {kit.RoutineChange{Spec: good}, []string{".name=Give the routine a name."}},
		"a long name": {
			kit.RoutineChange{Name: strings.Repeat("a", 81), Spec: good}, []string{".name=Keep the name to 80 characters."},
		},
		"a name of two lines": {
			kit.RoutineChange{Name: "Fix\nit", Spec: good}, []string{".name=Keep the name to one line of plain text."},
		},
		"a long description": {
			kit.RoutineChange{Name: "R", Description: strings.Repeat("a", 501), Spec: good},
			[]string{".description=Keep the description to 500 characters."},
		},
		"a template that isn't a key": {
			kit.RoutineChange{Name: "R", Template: "Not a key", Spec: good},
			[]string{".template=That isn't one of the templates."},
		},
		"a template the gallery doesn't have": {
			kit.RoutineChange{Name: "R", Template: "fix-everything", Spec: good},
			[]string{`.template=no template has the key "fix-everything": name one of the gallery's, or leave template out`},
		},
		"a mode that isn't one": {
			kit.RoutineChange{Name: "R", Mode: "always", Spec: good},
			[]string{`.mode=say "dryRun", "ask", or "auto", the mode it suggests, which the card shows beside the one it ` +
				`starts in, or leave mode out`},
		},
		"a base of a new routine": {
			kit.RoutineChange{Name: "R", Base: "2026-11-20T05:31:03Z", Spec: good},
			[]string{".base=only a change to one of your routines names a base: leave base out of a new routine"},
		},
		"a spec another version wrote": {
			kit.RoutineChange{Name: "R", Spec: spec.Spec{Version: 3}},
			[]string{".spec=The routine can't be read: Made by a newer version: update to edit it (version 3)."},
		},
		"a spec with problems": {
			kit.RoutineChange{Name: "R", Spec: checksFailSpec(agentStep(spec.PromptRef{Nickname: "Nope"}, "", ""))},
			[]string{`steps[0].agent.prompt=no prompt in your library has the nickname "Nope": pick one that does`},
		},
	} {
		r := checkedRoutines(t, c.change)[0]
		assert.Equal(t, c.want, problemPaths(r), name)
	}

	r := checkedRoutines(t, kit.RoutineChange{Name: "R", Spec: checksFailSpec(agentStep(spec.PromptRef{Nickname: "Nope"}, "", ""))})[0]
	assert.Empty(t, r.Sentence, "a spec with problems has no sentence, as the builder keeps the last that read")
	assert.Equal(t, []kit.Risk{{Level: kit.RiskWarning, Text: "Its agent may push to the pull request's own branch: " +
		"one push, never by force."}}, r.Risks, "its risks are worked out all the same, from what it says")
}

func TestATakenNameTakesTheFreeOneASaveGivesIt(t *testing.T) {
	t.Parallel()

	good := checksFailSpec(notifyStep())
	r := checkedRoutines(t,
		newRoutine("NIGHTLY", good), newRoutine("Nightly", good), newRoutine("Other", good),
		changeOf(9, "Other", good), changeOf(7, "Nightly", good), newRoutine("Fix checks", good),
	)

	assert.Equal(t, "NIGHTLY 2", r[0].Name, "another routine has it, ignoring case")
	assert.True(t, r[0].NameTaken)
	assert.Equal(t, kit.Risk{Level: kit.RiskCaution, Text: "Another routine is named “NIGHTLY”, so this one is saved as " +
		"“NIGHTLY 2”."}, r[0].Risks[0])
	assert.Equal(t, "Nightly 3", r[1].Name, "a new routine before it took the next")
	assert.Equal(t, "Other", r[2].Name)
	assert.False(t, r[2].NameTaken)
	assert.Equal(t, "Other 2", r[3].Name, "a change renamed to a name a routine before it took")
	assert.Equal(t, "Nightly", r[4].Name, "a change keeps its own name")
	assert.False(t, r[4].NameTaken)
	assert.Equal(t, "Fix checks", r[5].Name, "the name a change gave up is free for a routine after it")
	assert.False(t, r[5].NameTaken)

	gone := checkedRoutines(t, changeOf(404, "Nightly", good), newRoutine("Gone", good), changeOf(404, "Gone", good))
	assert.Equal(t, "Nightly 2", gone[0].Name, "a change to a routine that is gone is named as a save would")
	assert.Equal(t, "Gone", gone[1].Name, "and takes no name from the routines after it")
}

func TestAChangeNamesARoutineAndTheMomentItWasRead(t *testing.T) {
	t.Parallel()

	good := checksFailSpec(notifyStep())

	r := checkedRoutines(t, changeOf(7, "Nightly", good))[0]
	require.NotNil(t, r.Before)
	assert.Equal(t, int64(7), r.Before.ID, "the routine it changes, as the snapshot holds it")
	assert.Equal(t, "Nightly", r.Before.Name)
	assert.False(t, r.Stale)
	assert.False(t, r.Gone)
	assert.Empty(t, r.Problems)

	gone := checkedRoutines(t, changeOf(404, "Nightly", good))[0]
	assert.True(t, gone.Gone)
	assert.Nil(t, gone.Before)
	assert.Equal(t, []string{".id=no routine has the ID 404: leave id out for a new routine, or take a routine's " +
		"from autopilot_my_setup"}, problemPaths(gone))

	for base, stale := range map[string]bool{
		checkedAt.Format(time.RFC3339Nano):                                  false,
		checkedAt.In(time.FixedZone("here", 3600)).Format(time.RFC3339Nano): false,
		checkedAt.Format(time.RFC3339):                                      false,
		checkedAt.Add(time.Second).Format(time.RFC3339):                     true,
		checkedAt.Add(-time.Nanosecond).Format(time.RFC3339Nano):            true,
	} {
		change := changeOf(7, "Nightly", good)
		change.Base = base
		got := checkedRoutines(t, change)[0]
		assert.Equal(t, stale, got.Stale, base)

		if stale {
			assert.Equal(t, kit.Risk{
				Level: kit.RiskCaution, Text: "It changed after this proposal was written: saving it replaces those changes.",
			}, got.Risks[0], base)
		}
	}

	for base, want := range map[string]string{
		"": ".base=set base to the routine's updatedAt from autopilot_my_setup, so the card can tell when it changed since",
		"yesterday": `.base="yesterday" isn't a time: set base to the routine's updatedAt from autopilot_my_setup, as ` +
			"it is written there",
	} {
		change := changeOf(7, "Nightly", good)
		change.Base = base
		assert.Equal(t, []string{want}, problemPaths(checkedRoutines(t, change)[0]), base)
	}

	change := changeOf(7, "Nightly", good)
	change.Mode, change.Template = autopilotprefs.ModeAuto, "fix-failing-checks-on-my-prs"
	assert.Equal(t, []string{
		".mode=a change keeps the routine's own mode: leave mode out",
		".template=a change keeps the template the routine was made from: leave template out",
	}, problemPaths(checkedRoutines(t, change)[0]), "a change never touches its mode, nor its template")
}

func TestARoutineIsChangedOnceInAProposal(t *testing.T) {
	t.Parallel()

	good := checksFailSpec(notifyStep())
	r := checkedRoutines(t, changeOf(7, "Nightly", good), changeOf(9, "Fix checks", good), changeOf(7, "Nightly", good))

	assert.Empty(t, r[0].Problems)
	assert.Empty(t, r[1].Problems)
	assert.Equal(t, []string{".id=routines[0] changes this routine already: make every change to it in one"},
		problemPaths(r[2]), "a second change would undo the first as they are applied in turn")
}

func TestAChangeIsReadInTheRoutinesOwnMode(t *testing.T) {
	t.Parallel()

	snap := checkSnapshot()
	snap.Setup.Routines[0].Mode = autopilotprefs.ModeAuto

	terminal := checksFailSpec(agentStep(spec.PromptRef{Starter: fixChecksKey}, catalog.WhereTerminal, ""))
	c := kit.Check(t.Context(), snap, kit.Proposal{Summary: "x", Routines: []kit.RoutineChange{
		changeOf(7, "Nightly", terminal), newRoutine("New", terminal),
	}})

	assert.Len(t, c.Routines[0].Warnings, 1, "Nightly is on Autopilot, where a terminal run is warned of")
	assert.Empty(t, c.Routines[1].Warnings, "a new routine starts in Settings' Ask first")
}

func TestARoutineMayRunAPromptTheProposalAdds(t *testing.T) {
	t.Parallel()

	adds := []kit.NewPrompt{
		{Name: "Fix flaky", Nickname: " Fix flaky ", Purpose: kit.PurposeAgent, AppliesTo: kit.AppliesToPR, Body: "Fix it."},
		{Name: "Issues", Nickname: "Issues only", Purpose: kit.PurposeAgent, AppliesTo: kit.AppliesToIssue, Body: "Do it."},
		{
			Name: "Thanks", Nickname: "Thanks", Purpose: kit.PurposeReply, ReplyAction: kit.ReplyApprove, AppliesTo: kit.AppliesToAny,
			Body: "Thanks!",
		},
		{Name: "Broken", Nickname: "Broken", Purpose: "wizard", AppliesTo: kit.AppliesToPR, Body: "x"},
	}

	c := kit.Check(t.Context(), checkSnapshot(), kit.Proposal{Summary: "x", Prompts: adds, Routines: []kit.RoutineChange{
		newRoutine("A", checksFailSpec(agentStep(spec.PromptRef{Nickname: "fix FLAKY"}, catalog.WhereBackground, ""))),
		newRoutine("B", checksFailSpec(agentStep(spec.PromptRef{Nickname: "Issues only"}, catalog.WhereBackground, ""))),
		newRoutine("C", checksFailSpec(spec.Step{GitHub: []spec.GitHubAction{{
			Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Nickname: "Thanks"}},
		}}})),
		newRoutine("D", checksFailSpec(agentStep(spec.PromptRef{Nickname: "Broken"}, catalog.WhereBackground, ""))),
	}})

	assert.Empty(t, c.Routines[0].Problems, "a prompt the proposal adds is in the library it is checked against")
	assert.Equal(t, []string{`steps[0].agent.prompt="Issues only" is written for issues, and this routine acts only ` +
		"on pull requests: pick a prompt for pull requests"}, problemPaths(c.Routines[1]), "and checked as one there")
	assert.Equal(t, []string{`steps[0].github[0].reply.prompt="Thanks" is a reply that approves: a GitHub step only ` +
		"comments, so it never posts an approval's words; pick a comment reply"}, problemPaths(c.Routines[2]))
	assert.Equal(t, []string{`steps[0].agent.prompt=no prompt in your library has the nickname "Broken": pick one ` +
		"that does"}, problemPaths(c.Routines[3]), "a prompt whose purpose isn't one joins no library")
}

func TestARoutinesScriptIsOneOfTheSnapshots(t *testing.T) {
	t.Parallel()

	script := func(id int64, name string) spec.Spec {
		sp := checksFailSpec(spec.Step{Script: &spec.ScriptStep{ID: id, Name: name}})
		sp.Trigger = spec.Trigger{Event: catalog.EventAppears}

		return sp
	}

	r := checkedRoutines(t, newRoutine("A", script(3, "Lint")), newRoutine("B", script(3, "Old lint")),
		newRoutine("C", script(4, "Gone")))

	assert.Empty(t, r[0].Problems)
	assert.Equal(t, []string{`steps[0].script.id=The script picked as "Old lint" is now "Lint": pick it again`},
		problemPaths(r[1]))
	assert.Equal(t, []string{`steps[0].script.id="Gone" is no longer one of your scripts: pick another`},
		problemPaths(r[2]))
}

func TestEveryListOfACheckIsThere(t *testing.T) {
	t.Parallel()

	c := kit.Check(t.Context(), kit.Snapshot{}, kit.Proposal{
		Summary:  "Everything.",
		Prompts:  []kit.NewPrompt{{}},
		Routines: []kit.RoutineChange{{}},
		Settings: &kit.SettingsChange{AgentsAtOnce: new(1)},
		Rules:    &kit.RulesChange{Trust: []string{renovateLogin}},
	})

	data, err := json.Marshal(c)
	require.NoError(t, err)
	assert.NotContains(t, strings.ReplaceAll(string(data), `"before":null`, ""), "null",
		"a zero snapshot and a zero proposal give lists, never nulls: %s", data)

	empty := kit.Check(t.Context(), kit.Snapshot{}, kit.Proposal{})
	assert.NotNil(t, empty.Prompts)
	assert.NotNil(t, empty.Routines)
	assert.Nil(t, empty.Settings, "no settings change, no settings")
	assert.Nil(t, empty.Rules)
	assert.Zero(t, empty.Problems())
}

func TestProblemsCountsEveryPartsProblems(t *testing.T) {
	t.Parallel()

	c := kit.Check(t.Context(), checkSnapshot(), kit.Proposal{
		Summary:  "Problems.",
		Prompts:  []kit.NewPrompt{{Name: "P", Nickname: "Triage", Purpose: kit.PurposeAgent, AppliesTo: kit.AppliesToPR, Body: "x"}},
		Routines: []kit.RoutineChange{{Spec: checksFailSpec(notifyStep())}},
		Settings: &kit.SettingsChange{AgentsAtOnce: new(9)},
		Rules:    &kit.RulesChange{Trust: []string{"not a bot"}},
	})

	assert.Len(t, c.Prompts[0].Problems, 1)
	assert.Len(t, c.Routines[0].Problems, 1)
	assert.Len(t, c.Settings.Problems, 1)
	assert.Len(t, c.Rules.Problems, 1, "the bot alone: a change with a problem isn't also said to change nothing")
	assert.Equal(t, 4, c.Problems())
}
