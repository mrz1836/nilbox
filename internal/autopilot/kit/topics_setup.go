package kit

import (
	"reflect"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/heldruns"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/trust"
)

// settingWords are one of Autopilot's settings in words: its key in the settings, its
// label in Settings → Autopilot, what it takes and its default, and whether it is the
// user's alone, which no proposal changes.
type settingWords struct {
	key, label, words string
	usersAlone        bool
}

// settingTable is every one of Autopilot's settings, in Settings' order, with its range
// and its default from autopilotprefs.
func settingTable() []settingWords {
	d := autopilotprefs.Defaults()
	numbers := func(low, high, def int, unit string) string {
		return "a whole number, " + strconv.Itoa(low) + " to " + strconv.Itoa(high) + " " + unit + ", default " +
			strconv.Itoa(def)
	}

	return []settingWords{
		{key: "enabled", label: "Autopilot (beta) in Labs", usersAlone: true, words: "shows and runs Autopilot"},
		{key: "paused", label: "Pause Autopilot", usersAlone: true, words: "while on, no routine starts anything new"},
		{
			key: "mergePastRules", label: "Let routines merge your own pull requests past the rules", usersAlone: true,
			words: "`true` or `false`, default `false`: while on, a `" + string(catalog.StepMerge) + "` whose " +
				"`allowBypass` the user ticked as they added it may merge their own signed pull request that lacks only " +
				"an approval from someone else; never in a backup, and only the user turns it on, in Settings",
		},
		{
			key: "defaultMode", label: "New routines start in",
			words: enumWords[autopilotprefs.Mode]() + ", default `" + string(d.DefaultMode) + "`: the mode a " +
				"routine the user makes starts in; a routine keeps its own once made",
		},
		{
			key: "agentsAtOnce", label: "Agents at once",
			words: numbers(autopilotprefs.MinAgentsAtOnce, autopilotprefs.MaxAgentsAtOnce, d.AgentsAtOnce, "agents") +
				": the most coding agents the routines run at the same time",
		},
		{
			key: "runsPerDay", label: "Agent runs a day",
			words: numbers(autopilotprefs.MinRunsPerDay, autopilotprefs.MaxRunsPerDay, d.RunsPerDay, "runs") +
				": the most coding agent runs the routines start in a day, together",
		},
		{
			key: "actionsPerDay", label: "GitHub actions a day",
			words: numbers(autopilotprefs.MinActionsPerDay, autopilotprefs.MaxActionsPerDay, d.ActionsPerDay,
				"actions") + ": the most actions on GitHub the routines send in a day, together",
		},
		{
			key: "actionsPerHour", label: "GitHub actions an hour",
			words: numbers(autopilotprefs.MinActionsPerHour, autopilotprefs.MaxActionsPerHour, d.ActionsPerHour,
				"actions") + ": the most actions on GitHub the routines send in an hour, together, kept under " +
				"GitHub's own limits",
		},
		{
			key: "dailyCap", label: "Daily cap",
			words: "`{\"amount\": 5, \"unit\": \"" + string(autopilotprefs.UnitUSD) + "\"}`: the most the routines' " +
				"agents use in a day, in whole dollars (" + strconv.Itoa(autopilotprefs.MinCapUSD) + " to " +
				strconv.Itoa(autopilotprefs.MaxCapUSD) + ") or in tokens (" + strconv.Itoa(autopilotprefs.MinCapTokens) +
				" to " + strconv.Itoa(autopilotprefs.MaxCapTokens) + "), as `unit` says, " +
				tickedOr(autopilotprefs.Units()) + "; an `amount` of 0 is none, the default. A cap counts only the " +
				"runs budgeted in its own unit",
		},
		{
			key: "keepAwake", label: "Keep the Mac awake while agents work",
			words: "`true` or `false`, default `" + strconv.FormatBool(d.KeepAwake) + "`",
		},
		{
			key: "holdOnOutage", label: "Hold agent starts while their provider reports an outage",
			words: "`true` or `false`, default `" + strconv.FormatBool(d.HoldOnOutage) + "`: holds a routine's " +
				"agent while its provider's status page reports a major outage",
		},
		{
			key: "catchUpHours", label: "Ask before catching up after",
			words: numbers(autopilotprefs.MinCatchUpHours, autopilotprefs.MaxCatchUpHours, d.CatchUpHours, "hours") +
				": after " + brand.Name + " was closed this long, a routine's agent and GitHub steps wait for the " +
				"user's OK",
		},
		{
			key: "historyDays", label: "History kept",
			words: numbers(autopilotprefs.MinHistoryDays, autopilotprefs.MaxHistoryDays, d.HistoryDays, "days") +
				": lowering it deletes the older history, and lowers the run logs with it",
		},
		{
			key: "logDays", label: "Run logs kept",
			words: numbers(autopilotprefs.MinLogDays, autopilotprefs.MaxLogDays, d.LogDays, "days") +
				": never more than `historyDays`",
		},
	}
}

// settingsTopic writes Autopilot's settings, with their ranges and defaults.
func settingsTopic(_ Vocabulary) string {
	var p page

	p.heading(1, "Autopilot's settings")
	p.para("Settings → Autopilot holds limits over every routine together. A proposal's `settings` holds only " +
		"what changes, each key optional: `{\"agentsAtOnce\": 1, \"dailyCap\": {\"amount\": 5, \"unit\": \"" +
		string(autopilotprefs.UnitUSD) + "\"}}`. A limit delays a match and never loses it, and a day runs from " +
		"local midnight.")

	var mine, theirs []string

	for _, s := range settingTable() {
		line := tick(s.key) + ", " + s.label + ": " + s.words + "."
		if s.usersAlone {
			theirs = append(theirs, line)
		} else {
			mine = append(mine, line)
		}
	}

	p.list(mine...)
	p.para("These are the user's alone, and a proposal has no place for them:")
	p.list(theirs...)

	return p.String()
}

// rulesTopic writes the rules routines lean on, and what a change to each does.
func rulesTopic(_ Vocabulary) string {
	var p page

	p.heading(1, "The rules routines lean on")
	p.para("Beside the routines, Settings keeps rules that change what a routine sees and may do. A " +
		"proposal's `rules` changes them, each list optional: `{\"trust\": [\"renovate\"], \"mute\": [\"acme-org/legacy\"]}`.")
	p.list(
		"`trust`, `untrust`: bots to trust, or to trust no longer, each by its login, such as `renovate`, "+
			"`renovate[bot]`, or `app/renovate`, all written as `renovate`. An item a trusted bot opened counts "+
			"as `"+string(trust.ClassBot)+"` in `"+string(catalog.FieldTrust)+"`, not `"+
			string(trust.ClassOutside)+"`: an agent works on it as the routine says rather than only planning, its "+
			"GitHub actions don't wait for the user's OK as an outside contributor's do, and `"+
			string(catalog.StepPostReview)+"` may approve it. Until the user changes the list, "+
			tickedAnd(trust.DefaultBots())+" are trusted.",
		"`pin`, `mute`, `clear`: repositories (`owner/name`) or orgs (`owner`) to pin, to mute, or to clear of "+
			"either. A pinned one's items count as pinned in the inbox (`"+string(catalog.FieldRepoPinned)+"`); "+
			"a muted one's are set apart in each list's Muted tab, counted nowhere, told in no banner, and "+
			"followed by nothing (`"+string(catalog.FieldRepoMuted)+"`). A repository's own rule wins over its "+
			"org's, a target is in one list at most, and each list holds at most "+
			strconv.Itoa(reporules.MaxTargets)+".",
		"`ignoreWorkflows`, `unignoreWorkflows`: workflows, by their names as GitHub shows them, matched whole "+
			"and case-sensitively, whose runs GitHub holds for a maintainer's approval never count toward where "+
			"a pull request stands, on the inbox's cards and in the review sheet; at most "+
			strconv.Itoa(heldruns.MaxIgnored)+", each at most "+strconv.Itoa(heldruns.MaxNameLength)+" characters.",
	)
	p.para("`" + ToolMySetup + "` lists the rules as they are now, as `trustedBots`, `pinned`, `muted`, and " +
		"`ignoredWorkflows`.")

	return p.String()
}

// promptsTopic writes how a step names a prompt, the starters and the variables the
// vocabulary has, and what a new prompt holds.
func promptsTopic(v Vocabulary) string {
	var p page

	p.heading(1, "Prompts")
	p.para("An `" + string(catalog.StepAgent) + "` step runs a prompt from the user's prompt library, and a " +
		"GitHub step's `" + string(catalog.GitHubReply) + "` comments a reply from it. A step names one in " +
		"`prompt`, exactly one way:")
	p.list(
		"`{\"starter\": \"<key>\"}`: the user's copy of a starter prompt, below, whatever they renamed it.",
		"`{\"nickname\": \"<nickname>\"}`: the library's prompt with that nickname, ignoring case: one "+
			"`"+ToolMySetup+"` lists, or a new one the same proposal adds.",
		"`{\"itemDefault\": true}`: the item's own default prompt, as a click picks it; an agent step's alone.",
	)
	p.para("A prompt's `purpose` is " + enumWords[Purpose]() + ": an agent step runs an `" + string(PurposeAgent) +
		"` prompt, and a GitHub step comments a `" + string(PurposeReply) + "`. A reply's `replyAction` is the " +
		"review it is written for, " + enumWords[ReplyAction]() + "; a GitHub step only comments, so it takes a `" +
		string(ReplyComment) + "` reply alone. A prompt's `appliesTo` is " + enumWords[AppliesTo]() + ", and it " +
		"must apply to every kind of item the routine acts on.")

	p.heading(2, "The starters")
	p.para("Prompts " + brand.Name + " ships, which the user's library holds a copy of unless they deleted it; " +
		"`" + ToolMySetup + "` says which copies it holds, and their nicknames there.")
	p.list(starterLines(v.Starters)...)

	promptNew(&p)

	p.heading(2, "The variables")
	p.para("A prompt's text names the item's facts as `{{.Name}}`, filled in for each item; `{{if .IsPR}}...{{end}}` " +
		"works, and nothing that loops. It never holds the item's text or comments: the agent reads those " +
		"itself. A reply is filled in the same way.")
	p.list(variableLines(v.Variables)...)

	return p.String()
}

// starterLines says what each starter is.
func starterLines(starters []Starter) []string {
	if len(starters) == 0 {
		return []string{"This build ships none."}
	}

	out := make([]string, 0, len(starters))

	for _, s := range starters {
		line := tick(s.Key) + ": " + quotedData(s.Nickname) + " (" + vocabLine(s.Name) + "), `" +
			string(starterPurpose(s.Purpose)) + "`"
		if s.ReplyAction != "" {
			line += " for `" + string(s.ReplyAction) + "`"
		}

		out = append(out, line+", applies to `"+string(s.AppliesTo)+"`.")
	}

	return out
}

// starterPurpose returns a purpose, an agent's prompt's for none.
func starterPurpose(p Purpose) Purpose {
	if p == "" {
		return PurposeAgent
	}

	return p
}

// promptNew says what a new prompt holds.
func promptNew(p *page) {
	p.heading(2, "A new prompt")
	p.para("Add one in the proposal's `prompts` only when none in the library does what the user wants. It " +
		"takes the library's defaults for its model, effort, plan mode, and worktree.")
	p.list(keyWords(reflect.TypeFor[NewPrompt](), map[string]string{
		"name":        "its title: one line, short.",
		"nickname":    "what the launch menu shows, and a step names it by: one line of at most " + strconv.Itoa(spec.MaxNicknameRunes) + " characters, unlike any other prompt's, ignoring case.",
		"purpose":     enumWords[Purpose]() + ".",
		"replyAction": "a reply's review, " + enumWords[ReplyAction]() + "; left out for an agent's prompt.",
		"appliesTo":   enumWords[AppliesTo]() + ".",
		"body":        "its text, a Go template filled in for each item, at most " + strconv.Itoa(MaxPromptBody) + " bytes. Say what to do and how to finish; an agent in the background is told it runs unattended.",
	})...)
}

// variableLines says what each variable a prompt's text can name holds.
func variableLines(variables []Variable) []string {
	if len(variables) == 0 {
		return []string{"This build has none."}
	}

	out := make([]string, 0, len(variables))

	for _, v := range variables {
		line := "`{{." + v.Name + "}}` (" + vocabLine(v.Group) + ", " + vocabLine(v.Type)
		if v.PROnly {
			line += ", pull requests only"
		}

		out = append(out, line+"): "+strings.TrimSuffix(vocabLine(v.Description), ".")+".")
	}

	return out
}
