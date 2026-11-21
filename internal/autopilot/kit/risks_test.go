package kit_test

import (
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

// riskLines writes risks as a golden file holds them: one a line, its level first.
func riskLines(risks []kit.Risk) string {
	var b strings.Builder

	for _, r := range risks {
		b.WriteString(string(r.Level) + ": " + r.Text + "\n")
	}

	return b.String()
}

// routineRisksOf checks one routine against the fixtures' snapshot, beside the new
// prompts adds, and returns its risks.
func routineRisksOf(t *testing.T, rc kit.RoutineChange, adds ...kit.NewPrompt) []kit.Risk {
	t.Helper()

	c := kit.Check(t.Context(), checkSnapshot(), kit.Proposal{Summary: "x", Prompts: adds, Routines: []kit.RoutineChange{rc}})
	require.Empty(t, c.Routines[0].Problems, "a routine with no problem")

	return c.Routines[0].Risks
}

// backgroundAgent is a background agent step that runs Fix checks behind fence.
func backgroundAgent(fence *spec.Fence) spec.Step {
	return spec.Step{Agent: &spec.AgentStep{Prompt: spec.PromptRef{Starter: fixChecksKey}, Fence: fence}}
}

// healThenMerge is a routine whose agent pushes a fix, waits for the checks on it, and
// then runs then.
func healThenMerge(then spec.Step) spec.Spec {
	return checksFailSpec(backgroundAgent(nil),
		spec.Step{WaitForChecks: &spec.WaitStep{}, OnlyIf: catalog.OutcomePushed},
		then)
}

// passedMerge is a routine with no agent that, when the checks pass, merges as m says.
func passedMerge(m spec.MergeStep) spec.Spec {
	sp := checksFailSpec(spec.Step{Merge: &m})
	sp.Trigger = spec.Trigger{Event: catalog.EventChecksPassed}

	return sp
}

// reviewSpec is a routine whose read-only agent reviews a member's pull request, then
// posts its review as review says.
func reviewSpec(review *spec.ReviewStep) spec.Spec {
	sp := checksFailSpec(spec.Step{Agent: &spec.AgentStep{
		Prompt: spec.PromptRef{Starter: "review-pr"}, Fence: &spec.Fence{Tools: catalog.PresetReadOnly},
	}}, spec.Step{PostReview: review})
	sp.Trigger = spec.Trigger{Event: catalog.EventReadyForReview}

	return sp
}

// githubSpec is a routine on the user's own pull requests that sends actions.
func githubSpec(actions ...spec.GitHubAction) spec.Spec {
	sp := checksFailSpec(spec.Step{GitHub: actions})
	sp.Trigger = spec.Trigger{Event: catalog.EventAppears}

	return sp
}

// withLimits is a notifying routine with limits.
func withLimits(l spec.Limits) spec.Spec {
	sp := checksFailSpec(notifyStep())
	sp.Limits = l

	return sp
}

// dollars and tokens are budgets.
func dollars(usd float64) *spec.Budget { return &spec.Budget{USD: &usd} }

func tokens(n int64) *spec.Budget { return &spec.Budget{Tokens: &n} }

func TestRiskLinesArePinned(t *testing.T) {
	t.Parallel()

	snap := checkSnapshot()
	defaults := autopilotprefs.Defaults()

	for name, risks := range map[string]func(t *testing.T) []kit.Risk{
		"routine-on-changes-what-it-acts-on": func(t *testing.T) []kit.Risk {
			sp := checksFailSpec(notifyStep())
			sp.Watch = spec.Watch{Sources: []spec.Source{{Search: &watch.Search{
				Kind: item.KindPR, Authors: []string{"app/dependabot"}, Where: []string{"@me"},
			}}}}
			sp.Trigger = spec.Trigger{Event: catalog.EventAppears}
			sp.Conditions = spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{{
				Field: catalog.FieldFailedNames, Op: catalog.OpAnyContains, Values: []string{"security"},
			}}}
			sp.Limits.OncePer = catalog.OncePerDay

			return routineRisksOf(t, changeOf(7, "Nightly", sp))
		},
		"routine-on-changes-its-conditions": func(t *testing.T) []kit.Risk {
			sp := checksFailSpec(notifyStep())
			sp.Conditions = spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{{
				Field: catalog.FieldFailedNames, Op: catalog.OpAnyContains, Values: []string{"security"},
			}}}

			return routineRisksOf(t, changeOf(7, "Nightly", sp))
		},
		"agent-may-push": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Fix", checksFailSpec(backgroundAgent(nil))))
		},
		"agent-profiles-own-permissions": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Fix", checksFailSpec(backgroundAgent(&spec.Fence{Tools: catalog.PresetProfile}))))
		},
		"agent-in-the-terminal": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Fix", checksFailSpec(agentStep(spec.PromptRef{Starter: fixChecksKey},
				catalog.WhereTerminal, ""))))
		},
		"agent-github-tasks": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Re-run", checksFailSpec(spec.Step{Agent: &spec.AgentStep{
				Prompt: spec.PromptRef{Nickname: "Triage"}, Fence: &spec.Fence{Tools: catalog.PresetGitHubTasks},
			}})))
		},
		"agent-budget-and-time-over-the-defaults": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Fix", checksFailSpec(backgroundAgent(&spec.Fence{
				Budget: dollars(10), TimeoutMinutes: new(120), Tools: catalog.PresetEditTest,
				Allow: []string{"docker", "make", "a", "b", "c", "d", "e"},
			}))))
		},
		"agent-budget-raised-from-its-own": func(t *testing.T) []kit.Risk {
			before := checksFailSpec(backgroundAgent(&spec.Fence{Budget: dollars(5), TimeoutMinutes: new(45)}))
			after := checksFailSpec(backgroundAgent(&spec.Fence{Budget: dollars(8), TimeoutMinutes: new(90)}))

			return kit.RoutineRisks(&kit.Routine{Spec: before}, after, nil, nil)
		},
		"agent-budget-in-tokens": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Fix", checksFailSpec(backgroundAgent(&spec.Fence{Budget: tokens(1_000_000)}))))
		},
		"agent-runs-a-prompt-the-proposal-adds": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Triage", checksFailSpec(agentStep(spec.PromptRef{Nickname: "Look closer"},
				catalog.WhereBackground, catalog.PresetReadOnly))), agentPrompt("Look closer", "Look at {{.URL}}."))
		},
		"merge-it": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Heal", healThenMerge(spec.Step{
				Merge: &spec.MergeStep{How: catalog.MergeWhenMergeable, Method: catalog.MergeSquash}, OnlyIf: catalog.OutcomePassedChecks,
			})))
		},
		"merge-it-the-repositorys-own-way": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Heal", healThenMerge(spec.Step{
				Merge: &spec.MergeStep{How: catalog.MergeAutoIfItWasOn}, OnlyIf: catalog.OutcomePassedChecks,
			})))
		},
		"merge-it-past-the-rules": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Sync", passedMerge(spec.MergeStep{
				How: catalog.MergeWhenMergeable, Method: catalog.MergeSquash, AllowBypass: true,
			})))
		},
		"merge-it-past-the-rules-sensitive-files-included": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Sync", passedMerge(spec.MergeStep{
				How: catalog.MergeWhenMergeable, Method: catalog.MergeSquash, AllowBypass: true, AllowSensitive: true,
			})))
		},
		"merge-it-past-the-rules-added-to-a-merge": func(t *testing.T) []kit.Risk {
			before := passedMerge(spec.MergeStep{How: catalog.MergeWhenMergeable, Method: catalog.MergeSquash})
			after := passedMerge(spec.MergeStep{How: catalog.MergeWhenMergeable, Method: catalog.MergeSquash, AllowBypass: true})

			return kit.RoutineRisks(&kit.Routine{Spec: before}, after, nil, nil)
		},
		"post-review-as-a-comment": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Review", reviewSpec(&spec.ReviewStep{As: catalog.ReviewAsComment})))
		},
		"post-review-as-its-verdict": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Review", reviewSpec(&spec.ReviewStep{As: catalog.ReviewAsVerdict})))
		},
		"post-review-allowing-approval": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Review", reviewSpec(&spec.ReviewStep{
				As: catalog.ReviewAsVerdict, AllowApprove: true,
			})))
		},
		"github-comment-a-reply": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Ask", githubSpec(spec.GitHubAction{
				Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Starter: "reply-question"}},
			})))
		},
		"github-comment-a-reply-the-proposal-adds": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Ask", githubSpec(spec.GitHubAction{
				Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Nickname: "Any news"}},
			})), kit.NewPrompt{
				Name: "Any news", Nickname: "Any news", Purpose: kit.PurposeReply, ReplyAction: kit.ReplyComment,
				AppliesTo: kit.AppliesToAny, Body: "Any news?",
			})
		},
		"github-labels": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Label", githubSpec(spec.GitHubAction{
				Labels: &spec.LabelsAction{Add: []string{"needs-repro", "stale"}, Remove: []string{"triage"}},
			})))
		},
		"github-rebase-and-update-branch": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Rebase", githubSpec(
				spec.GitHubAction{Rebase: &spec.Empty{}}, spec.GitHubAction{UpdateBranch: &spec.Empty{}},
			)))
		},
		"script": func(t *testing.T) []kit.Risk {
			sp := checksFailSpec(spec.Step{Script: &spec.ScriptStep{ID: 3, Name: "Lint"}})
			sp.Trigger = spec.Trigger{Event: catalog.EventAppears}

			return routineRisksOf(t, newRoutine("Lint it", sp))
		},
		"limits-raised-over-the-defaults": func(t *testing.T) []kit.Risk {
			return routineRisksOf(t, newRoutine("Busy", withLimits(spec.Limits{
				TriesPerItem: new(5), RunsPerDay: new(50), WaitMinutes: new(0),
			})))
		},
		"limits-loosened-from-its-own": func(t *testing.T) []kit.Risk {
			before := withLimits(spec.Limits{
				TriesPerItem: new(3), RunsPerDay: new(20), WaitMinutes: new(60),
				ActiveHours:  &spec.ActiveHours{Days: []catalog.Weekday{"mon", "tue", "wed", "thu", "fri"}, From: "09:00", To: "17:00"},
				BudgetPerDay: dollars(5),
			})
			after := withLimits(spec.Limits{
				TriesPerItem: new(4), RunsPerDay: new(40), WaitMinutes: new(15),
				ActiveHours: &spec.ActiveHours{From: "08:00", To: "20:00"}, BudgetPerDay: dollars(20),
			})

			return kit.RoutineRisks(&kit.Routine{Spec: before}, after, nil, nil)
		},
		"limits-taken-away": func(t *testing.T) []kit.Risk {
			before := withLimits(spec.Limits{
				ActiveHours:  &spec.ActiveHours{Days: []catalog.Weekday{"sat", "sun"}, From: "22:00", To: "06:00"},
				BudgetPerDay: tokens(500_000),
			})

			return kit.RoutineRisks(&kit.Routine{Spec: before}, withLimits(spec.Limits{}), nil, nil)
		},
		"limits-daily-budget-in-another-unit": func(t *testing.T) []kit.Risk {
			before := withLimits(spec.Limits{BudgetPerDay: dollars(5)})

			return kit.RoutineRisks(&kit.Routine{Spec: before}, withLimits(spec.Limits{BudgetPerDay: tokens(2_000_000)}), nil, nil)
		},
		"stale-base-and-a-taken-name": func(t *testing.T) []kit.Risk {
			change := changeOf(9, "Nightly", checksFailSpec(notifyStep()))
			change.Base = "2026-11-19T00:00:00Z"
			c := kit.Check(t.Context(), snap, kit.Proposal{Summary: "x", Routines: []kit.RoutineChange{change}})

			return c.Routines[0].Risks
		},
		"settings-loosened": func(t *testing.T) []kit.Risk {
			after := defaults
			after.DefaultMode, after.HoldOnOutage = autopilotprefs.ModeAuto, false
			after.HistoryDays, after.LogDays = 14, 7
			after.AgentsAtOnce, after.RunsPerDay, after.ActionsPerDay, after.CatchUpHours = 3, 100, 200, 168

			return kit.SettingsRisks(defaults, after)
		},
		"settings-daily-cap-taken-away": func(t *testing.T) []kit.Risk {
			before := defaults
			before.DailyCap = autopilotprefs.DailyCap{Amount: 20, Unit: autopilotprefs.UnitUSD}

			return kit.SettingsRisks(before, defaults)
		},
		"settings-daily-cap-raised-and-in-another-unit": func(t *testing.T) []kit.Risk {
			before, raised, other := defaults, defaults, defaults
			before.DailyCap = autopilotprefs.DailyCap{Amount: 20, Unit: autopilotprefs.UnitUSD}
			raised.DailyCap = autopilotprefs.DailyCap{Amount: 50, Unit: autopilotprefs.UnitUSD}
			other.DailyCap = autopilotprefs.DailyCap{Amount: 2_000_000, Unit: autopilotprefs.UnitTokens}

			return append(kit.SettingsRisks(before, raised), kit.SettingsRisks(before, other)...)
		},
		"rules-trust-mute-ignore": func(t *testing.T) []kit.Risk {
			before := kit.Rules{TrustedBots: []string{dependabotLogin}, Muted: []string{"acme-org/old"}}
			after := kit.Rules{
				TrustedBots: []string{dependabotLogin, renovateLogin}, Muted: []string{"Acme-Org/Old", "acme-org/legacy", "acme-labs"},
				IgnoredWorkflows: []string{"CI / flaky"},
			}

			return kit.RulesRisks(before, after)
		},
		"prompt-an-agent-runs-and-a-reply-commented": func(t *testing.T) []kit.Risk {
			both := spec.Spec{Steps: []spec.Step{
				{Agent: &spec.AgentStep{Prompt: spec.PromptRef{Nickname: "look closer"}}},
				{GitHub: []spec.GitHubAction{{Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Nickname: "LOOK CLOSER"}}}}},
			}}

			return kit.PromptRisks(agentPrompt(" Look closer ", "x"), []spec.Spec{both})
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := risks(t)
			require.NotEmpty(t, got, "a case of the Safety list has a risk")
			checkGolden(t, "risks/"+name+".golden", riskLines(got))
		})
	}
}

func TestWhatARoutineDidBeforeIsNoNewRisk(t *testing.T) {
	t.Parallel()

	same := checksFailSpec(backgroundAgent(&spec.Fence{Budget: dollars(5)}),
		spec.Step{GitHub: []spec.GitHubAction{{Labels: &spec.LabelsAction{Add: []string{"Bug"}}}}})
	again := checksFailSpec(backgroundAgent(&spec.Fence{Budget: dollars(4)}),
		spec.Step{GitHub: []spec.GitHubAction{{Labels: &spec.LabelsAction{Add: []string{"bug"}}}}})
	assert.Empty(t, kit.RoutineRisks(&kit.Routine{Spec: same}, again, nil, nil), "a lower budget, a label it added")

	wider := checksFailSpec(agentStep(spec.PromptRef{Starter: fixChecksKey}, catalog.WhereBackground, catalog.PresetProfile))
	narrower := checksFailSpec(agentStep(spec.PromptRef{Starter: fixChecksKey}, catalog.WhereBackground, catalog.PresetReadOnly))
	assert.Empty(t, kit.RoutineRisks(&kit.Routine{Spec: wider}, narrower, nil, nil), "a narrower preset")

	terminal := checksFailSpec(agentStep(spec.PromptRef{Starter: fixChecksKey}, catalog.WhereTerminal, ""))
	assert.Empty(t, kit.RoutineRisks(&kit.Routine{Spec: terminal}, terminal, nil, nil))

	hours := withLimits(spec.Limits{ActiveHours: &spec.ActiveHours{From: "09:00", To: "17:00"}})
	inside := withLimits(spec.Limits{ActiveHours: &spec.ActiveHours{Days: []catalog.Weekday{"mon"}, From: "10:00", To: "12:00"}})
	assert.Empty(t, kit.RoutineRisks(&kit.Routine{Spec: hours}, inside, nil, nil), "narrower active hours")
	assert.Empty(t, kit.RoutineRisks(nil, hours, nil, nil), "active hours on a new routine hold it in")

	for name, before := range map[string]kit.Routine{
		"off":        {Enabled: false, Mode: autopilotprefs.ModeAuto, Spec: checksFailSpec(notifyStep())},
		"in Dry run": {Enabled: true, Mode: autopilotprefs.ModeDryRun, Spec: checksFailSpec(notifyStep())},
	} {
		changed := checksFailSpec(notifyStep())
		changed.Trigger = spec.Trigger{Event: catalog.EventAppears}
		assert.Empty(t, kit.RoutineRisks(&before, changed, nil, nil), "a routine %s acts on nothing as it is saved", name)
	}

	assert.Empty(t, kit.SettingsRisks(autopilotprefs.Defaults(), autopilotprefs.Defaults()))
	assert.Empty(t, kit.RulesRisks(kit.Rules{Muted: []string{"acme-org"}}, kit.Rules{Pinned: []string{"acme-org/api"}}),
		"a pin, and a mute taken off")
	assert.Empty(t, kit.PromptRisks(agentPrompt("Unused", "x"), nil), "a prompt nothing runs")
	assert.Empty(t, kit.PromptRisks(agentPrompt(" ", "x"), []spec.Spec{checksFailSpec(notifyStep())}))
}

func TestRisksWeighWhatAChangeAdds(t *testing.T) {
	t.Parallel()

	profile := checksFailSpec(agentStep(spec.PromptRef{Starter: fixChecksKey}, catalog.WhereBackground, catalog.PresetProfile))
	assert.Empty(t, kit.RoutineRisks(&kit.Routine{Spec: profile}, profile, nil, nil), "the profile's own, as before")

	wild := checksFailSpec(agentStep(spec.PromptRef{Starter: fixChecksKey}, catalog.WhereBackground, "wild"))
	assert.Empty(t, kit.RoutineRisks(nil, wild, nil, nil), "a preset the catalog doesn't list reaches nothing")

	replies := githubSpec(
		spec.GitHubAction{Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Starter: "reply-question"}}},
		spec.GitHubAction{Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Nickname: "Ping"}}},
	)
	again := githubSpec(
		spec.GitHubAction{Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Nickname: "PING"}}},
		spec.GitHubAction{Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Starter: "reply-question"}}},
	)
	assert.Empty(t, kit.RoutineRisks(&kit.Routine{Spec: replies}, again, nil, nil), "the replies it commented before")
	assert.Equal(t, []kit.Risk{{
		Level: kit.RiskWarning, Text: "It comments your reply “Reply question” on GitHub as you: it can be deleted, but " +
			"GitHub has told people by then.",
	}}, kit.RoutineRisks(nil, githubSpec(replies.Steps[0].GitHub[0]), nil, nil),
		"with no names to read, a starter is its key as words")

	assert.Contains(t, riskLines(kit.RoutineRisks(nil, githubSpec(replies.Steps[0].GitHub[1]), nil, nil)),
		"It comments your reply “Ping” on GitHub as you", "a reply by its nickname is named so")

	unnamed := func(spec.PromptRef) string { return "" }
	assert.Contains(t, riskLines(kit.RoutineRisks(nil, githubSpec(replies.Steps[0].GitHub[0]), nil, unnamed)),
		"“Reply question”", "and so is one the names don't know")

	off := githubSpec(spec.GitHubAction{Labels: &spec.LabelsAction{Remove: []string{"triage"}}})
	assert.Equal(t, "warning: It takes off “triage” on GitHub; a label can start a repository's label-triggered "+
		"workflows.\n", riskLines(kit.RoutineRisks(nil, off, nil, nil)))

	waited := withLimits(spec.Limits{WaitMinutes: new(30)})
	assert.Equal(t, "warning: It tries again on one item without waiting, where it waited 30 minutes.\n",
		riskLines(kit.RoutineRisks(&kit.Routine{Spec: waited}, withLimits(spec.Limits{WaitMinutes: new(0)}), nil, nil)))

	lower := kit.RoutineRisks(&kit.Routine{Spec: withLimits(spec.Limits{BudgetPerDay: dollars(20)})},
		withLimits(spec.Limits{BudgetPerDay: dollars(5)}), nil, nil)
	assert.Empty(t, lower, "a lower daily budget")

	script := checksFailSpec(spec.Step{Script: &spec.ScriptStep{ID: 3, Name: "Lint"}},
		spec.Step{Script: &spec.ScriptStep{ID: 3, Name: "Lint"}})
	assert.Empty(t, kit.RoutineRisks(&kit.Routine{Spec: script}, script, nil, nil), "a script it ran before")
	assert.Len(t, kit.RoutineRisks(nil, script, nil, nil), 1, "a script it runs twice is one risk")

	before, after := autopilotprefs.Defaults(), autopilotprefs.Defaults()
	before.DailyCap = autopilotprefs.DailyCap{Amount: 50, Unit: autopilotprefs.UnitUSD}
	after.DailyCap = autopilotprefs.DailyCap{Amount: 20, Unit: autopilotprefs.UnitUSD}
	assert.Empty(t, kit.SettingsRisks(before, after), "a lower daily cap")

	unread := kit.RulesRisks(kit.Rules{TrustedBots: []string{"Not A Bot", "Renovate[bot]"}}, kit.Rules{TrustedBots: []string{renovateLogin}})
	assert.Empty(t, unread, "a bot trusted before is one, however its login was written, whatever else the list holds")
}

func TestActiveHoursWidenAsTheEngineReadsThem(t *testing.T) {
	t.Parallel()

	night := &spec.ActiveHours{Days: []catalog.Weekday{"fri"}, From: "22:00", To: "02:00"}

	for name, c := range map[string]struct {
		after *spec.ActiveHours
		wider bool
	}{
		"the same":               {&spec.ActiveHours{Days: []catalog.Weekday{"fri"}, From: "22:00", To: "02:00"}, false},
		"saturday's small hours": {&spec.ActiveHours{Days: []catalog.Weekday{"sat"}, From: "00:00", To: "01:00"}, false},
		"saturday night":         {&spec.ActiveHours{Days: []catalog.Weekday{"sat"}, From: "23:00", To: "23:30"}, true},
		"an earlier start":       {&spec.ActiveHours{Days: []catalog.Weekday{"fri"}, From: "21:00", To: "02:00"}, true},
		"no hour at all":         {&spec.ActiveHours{Days: []catalog.Weekday{"fri"}, From: "10:00", To: "10:00"}, false},
		"hours it can't read":    {&spec.ActiveHours{From: "soon", To: "later"}, true},
	} {
		risks := kit.RoutineRisks(&kit.Routine{Spec: withLimits(spec.Limits{ActiveHours: night})},
			withLimits(spec.Limits{ActiveHours: c.after}), nil, nil)
		assert.Equal(t, c.wider, len(risks) == 1, name)
	}
}
