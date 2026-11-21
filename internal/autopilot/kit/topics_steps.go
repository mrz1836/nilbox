package kit

import (
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/item"
)

// stepsTopic writes every step and its settings, Only if, how the modes run them, and the
// order rules the checks hold the steps to.
func stepsTopic(_ Vocabulary) string {
	var p page

	p.heading(1, "Steps")
	p.para("A routine's `steps` run in order, one to " + strconv.Itoa(spec.MaxSteps) + " of them. Each step " +
		"is an object holding exactly one kind of step's key, with its settings, and maybe `onlyIf`: " +
		"`{\"" + string(catalog.StepSnooze) + "\": {\"days\": 3}}`, `{\"" + string(catalog.StepPin) + "\": {}}`.")

	p.heading(2, "The kinds of step")

	steps := catalog.Steps()
	lines := make([]string, 0, len(steps))

	for _, s := range steps {
		lines = append(lines, stepLine(s))
	}

	p.list(lines...)
	stepModes(&p)
	stepOnlyIf(&p)

	p.heading(2, "The order rules")
	p.para("`" + ToolCheck + "` holds the steps to these, as the builder does:")
	p.list(orderRules()...)

	return p.String()
}

// stepLine says what a step does and what it acts on, then, a line each beneath it, its
// settings.
func stepLine(s catalog.Step) string {
	line := tick(string(s.Kind)) + labelWords(string(s.Kind), s.Label) + ": " + s.Help

	if !slices.Contains(s.Kinds, item.KindIssue) {
		line += " Pull requests only."
	}

	switch s.Kind {
	case catalog.StepGitHub:
		return line + " A list of actions: topic `github`."
	case catalog.StepAgent:
		return line + " Topic `agent`."
	case catalog.StepNotify, catalog.StepPin, catalog.StepSnooze, catalog.StepDismiss, catalog.StepRestore,
		catalog.StepMarkDone, catalog.StepWaitForChecks, catalog.StepMerge, catalog.StepPostReview,
		catalog.StepTellMe, catalog.StepScript:
	}

	settings := stepSettingLines(s.Kind)
	if len(settings) == 0 {
		return line + " It takes no settings: `{}`."
	}

	return line + "\n  - " + strings.Join(settings, "\n  - ")
}

// stepSettingLines writes each setting of a kind of step: its key, whether it is needed,
// and what it takes, from the format's step and the catalog's ranges and choices.
func stepSettingLines(kind catalog.StepKind) []string {
	t, ok := stepStruct(kind)
	if !ok {
		return nil
	}

	var out []string

	for f := range t.Fields() {

		key, optional, ok := jsonKeyOf(f)
		if !ok {
			continue
		}

		need := ""
		if !optional {
			need = ", needed"
		}

		out = append(out, tick(key)+need+": "+stepSettingWords(string(kind)+"."+key, f.Type)+".")
	}

	return out
}

// stepStruct returns the struct type a kind of step's key holds in the format's step.
func stepStruct(kind catalog.StepKind) (reflect.Type, bool) {
	t := reflect.TypeFor[spec.Step]()

	for field := range t.Fields() {
		if key, _, ok := jsonKeyOf(field); ok && key == string(kind) {
			inner := elemOf(field.Type)

			return inner, inner.Kind() == reflect.Struct
		}
	}

	return nil, false
}

// stepSettingWords says what a step's setting, named "<step>.<key>", takes: its choices
// with their labels, its range and default from the catalog, or the words kit gives it.
func stepSettingWords(id string, t reflect.Type) string {
	elem := elemOf(t)

	if set, ok := closedSetOf(elem); ok {
		words := offerWords(tickLabeled(set.Values, valueLabels(elem)))
		if set.Blank {
			words += "; left out, " + leftOutWords(elem)
		}

		return words
	}

	for _, s := range catalog.StepSettings() {
		if s.ID == id && s.Range != nil {
			return s.Label + ", a whole number, " + rangeWords(*s.Range)
		}
	}

	return map[string]string{
		"notify.note": "a line of the user's own the notice carries beside the reason, at most " +
			strconv.Itoa(spec.MaxTextRunes) + " characters",
		"notify.banner": "`true` for a banner, `false` for an entry in the Notifications page alone; left out, " +
			"the routine's notices decide",
		"postReview.allowApprove": "`true` lets a verdict approve, with `as` `" + string(catalog.ReviewAsVerdict) +
			"` alone; left out, `false`, and an approval posts as a comment",
		"script.id":   "the script's id, exactly as `" + ToolMySetup + "` lists it",
		"script.name": "the script's name, exactly as `" + ToolMySetup + "` lists it",
	}[id]
}

// leftOutWords says what a closed set's field left out means.
func leftOutWords(t reflect.Type) string {
	return map[reflect.Type]string{
		reflect.TypeFor[catalog.MergeMethod](): "the repository's own method",
		reflect.TypeFor[catalog.TellWhen]():    "`" + string(catalog.TellReached) + "`",
	}[t]
}

// stepModes says how the modes run each kind of step.
func stepModes(p *page) {
	var local, asks, rest []catalog.StepKind

	for _, s := range catalog.Steps() {
		switch {
		case s.Local:
			local = append(local, s.Kind)
		case s.AsksFirst:
			asks = append(asks, s.Kind)
		default:
			rest = append(rest, s.Kind)
		}
	}

	p.heading(2, "How the modes run them")
	p.list(
		tickedAnd(local)+" run at once, wherever they sit, needing no OK: they change only what this Mac shows "+
			"and tells, and the user's own GitHub notifications.",
		tickedAnd(asks)+" follow the routine's mode: in Ask first, the first of them waits for the user's OK, "+
			"and the rest go on their own; on Autopilot, they run at once. `"+string(catalog.StepMerge)+"` and `"+
			string(catalog.StepPostReview)+"` ask again in Ask first once they are reached.",
		tickedAnd(rest)+" follows the agent's push: it waits for the checks on the agent's own commit.",
		"In Dry run nothing runs: the routine records what it would have done.",
	)
}

// stepOnlyIf says what Only if names.
func stepOnlyIf(p *page) {
	p.heading(2, "Only if")
	p.para("`onlyIf` runs a step only when an earlier step ended so. A step whose `onlyIf` doesn't hold is " +
		"skipped, and the routine goes on, so \"if the fix passes, merge; if it still fails, tell me\" is two " +
		"steps, `{\"" + string(catalog.StepMerge) + "\": {\"how\": \"" + string(catalog.MergeWhenMergeable) +
		"\"}, \"onlyIf\": \"" + string(catalog.OutcomePassedChecks) + "\"}` and `{\"" + string(catalog.StepTellMe) +
		"\": {}, \"onlyIf\": \"" + string(catalog.OutcomeFailingChecks) + "\"}`. An unknown or overtaken " +
		"verdict holds neither. The outcomes:")

	outcomes := catalog.Outcomes()
	lines := make([]string, 0, len(outcomes))

	for _, o := range outcomes {
		lines = append(lines, tick(string(o.ID))+" ("+o.Label+"): from the latest `"+string(o.From)+"` step before it.")
	}

	p.list(lines...)
}

// orderRules says, in words, each rule the format's checks hold the steps to.
func orderRules() []string {
	mayPush, readOnly := presetsWhere(func(p catalog.PresetInfo) bool { return p.MayPush }),
		presetsWhere(func(p catalog.PresetInfo) bool { return p.ReadOnly })
	background, terminal := tick(string(catalog.WhereBackground)), tick(string(catalog.WhereTerminal))
	agent, wait := tick(string(catalog.StepAgent)), tick(string(catalog.StepWaitForChecks))

	return []string{
		"Each step holds exactly one kind of step.",
		"One " + agent + " step at most: two agents on one item take two routines.",
		wait + " only after an " + agent + " step in the background (`where` " + background + ") whose tools " +
			"preset may push (" + tickedOr(mayPush) + "), and never with `onlyIf` `" +
			string(catalog.OutcomeNotPushed) + "`.",
		tick(string(catalog.StepMerge)) + " only after a " + wait + " whose `onlyIf` is `" +
			string(catalog.OutcomePushed) + "`, so the checks it follows are on the agent's own commit, and only " +
			"with its own `onlyIf` `" + string(catalog.OutcomePassedChecks) + "`.",
		tick(string(catalog.StepPostReview)) + " only after an " + agent + " step in the background whose tools " +
			"preset is " + tickedOr(readOnly) + ", and `allowApprove` only with `as` `" +
			string(catalog.ReviewAsVerdict) + "`.",
		"After an " + agent + " step in the terminal (`where` " + terminal + "), only " +
			tickedOr(terminalFollowers()) + ": the app can't see a terminal run end.",
		"`onlyIf` is never on the first step. " + tickedAnd(outcomesFrom(catalog.StepWaitForChecks)) +
			" need a " + wait + " before it; " + tickedAnd(pushOutcomes()) + " need an " + agent + " step before " +
			"it that may push; " + tickedAnd(verdictOutcomes()) + " need an " + agent + " step before it in the " +
			"background whose preset is " + tickedOr(readOnly) + ".",
		tickedAnd(prOnlySteps()) + " act on pull requests alone, so the routine's watch must hold pull requests.",
		"A schedule's `" + string(catalog.ScheduleDigest) + "` needs `" + string(catalog.StepNotify) + "` as the " +
			"first step.",
	}
}

// presetsWhere returns the tools presets keep reports true of, from the narrowest.
func presetsWhere(keep func(catalog.PresetInfo) bool) []catalog.Preset {
	var out []catalog.Preset

	for _, p := range catalog.Presets() {
		if keep(p) {
			out = append(out, p.ID)
		}
	}

	return out
}

// terminalFollowers are the kinds of step that may follow an agent step in the terminal:
// the notices.
func terminalFollowers() []catalog.StepKind {
	return []catalog.StepKind{catalog.StepNotify, catalog.StepTellMe}
}

// outcomesFrom returns the outcomes a kind of step ends with.
func outcomesFrom(kind catalog.StepKind) []catalog.Outcome {
	var out []catalog.Outcome

	for _, o := range catalog.Outcomes() {
		if o.From == kind {
			out = append(out, o.ID)
		}
	}

	return out
}

// pushOutcomes and verdictOutcomes are the agent step's outcomes: whether it pushed, and
// a read-only agent's verdict.
func pushOutcomes() []catalog.Outcome {
	return slices.DeleteFunc(outcomesFrom(catalog.StepAgent), isVerdict)
}

func verdictOutcomes() []catalog.Outcome {
	return slices.DeleteFunc(outcomesFrom(catalog.StepAgent), func(o catalog.Outcome) bool { return !isVerdict(o) })
}

// isVerdict reports a read-only agent's verdict.
func isVerdict(o catalog.Outcome) bool {
	return o == catalog.OutcomeApprove || o == catalog.OutcomeRequestChanges
}

// prOnlySteps are the kinds of step that act on pull requests alone.
func prOnlySteps() []catalog.StepKind {
	var out []catalog.StepKind

	for _, s := range catalog.Steps() {
		if !slices.Contains(s.Kinds, item.KindIssue) {
			out = append(out, s.Kind)
		}
	}

	return out
}

// agentTopic writes the agent step: its prompt, where it runs, its launch, and its fence.
func agentTopic(v Vocabulary) string {
	var p page

	p.heading(1, "The agent step")
	p.para("`" + string(catalog.StepAgent) + "` starts a coding agent on the item with a prompt from the user's " +
		"library, as a click on the item's launch button would, with the routine's own choices in the launch " +
		"menu's place. Every setting left out inherits what a click would use.")
	p.block("json", agentExample(v))
	p.list(agentKeyLines(reflect.TypeFor[spec.AgentStep]())...)

	p.heading(2, "Where it runs")
	p.list(placeLines()...)

	p.heading(2, "The fence")
	p.para("`fence` holds a run in the background in, since nobody is there to answer the agent; a terminal " +
		"run takes none. Left out, it takes the defaults.")
	p.list(agentKeyLines(reflect.TypeFor[spec.Fence]())...)

	p.heading(2, "The tools presets")
	p.para("From the narrowest. Give a step the narrowest that does the job:")
	p.list(presetLines()...)
	p.para("Under every preset an agent never edits outside the item's worktree, nor the repository's agent, " +
		"workflow, and tooling files; never uses the network tools; never merges, reviews, or closes through " +
		"`gh`; never reads the user's credentials; and pushes at most once, to the pull request's own branch, " +
		"never with `--force`. An agent works on an outside contributor's item only where its org or " +
		"repository allows it, and then read only, whatever the preset. The step's own agent may not keep " +
		"every preset, which `" + ToolCheck + "` says.")

	return p.String()
}

// agentExample writes an agent step as a routine holds one: the worked example's
// template's, with a budget and a time limit of its own.
func agentExample(v Vocabulary) string {
	sp := exampleSpec()
	if t, ok := exampleTemplate(v.Templates); ok {
		sp = t.Spec
	}

	a := exampleAgent(sp)
	if a == nil {
		return ""
	}

	step := *a
	if step.Prompt == (spec.PromptRef{}) {
		step.Prompt = spec.PromptRef{ItemDefault: true}
	}

	step.Fence = &spec.Fence{Tools: step.Tools(), TimeoutMinutes: new(20), Budget: &spec.Budget{USD: new(2.0)}}

	return compactJSON(spec.Step{Agent: &step})
}

// agentKeyLines writes each key of the agent step's struct t: what it takes, from the
// catalog's agent settings by the key's name, and kit's words.
func agentKeyLines(t reflect.Type) []string {
	settings := map[string]catalog.Setting{}
	for _, s := range catalog.AgentSettings() {
		settings[s.ID] = s
	}

	out := make([]string, 0, t.NumField())

	for field := range t.Fields() {
		key, _, ok := jsonKeyOf(field)
		if !ok {
			continue
		}

		out = append(out, tick(key)+": "+agentKeyWords(key, settings[key]))
	}

	return out
}

// agentKeyWords says what one of the agent step's keys takes.
func agentKeyWords(key string, s catalog.Setting) string {
	words := map[string]string{
		"agent": "`" + catalog.Inherit + "`, or an agent's id, as `" + ToolMySetup + "`'s profiles name the " +
			"agents; left out, `" + catalog.Inherit + "`.",
		"prompt": "the prompt it runs, exactly one of `{\"starter\": \"<key>\"}`, `{\"nickname\": \"<nickname>\"}`, " +
			"and `{\"itemDefault\": true}` (topic `prompts`).",
		"where":   enumWords[catalog.Where]() + "; left out, `" + string(catalog.DefaultWhere) + "`.",
		"profile": "the agent's profile by its name, one of `" + ToolMySetup + "`'s; left out, inherited.",
		"fallbacks": "up to " + strconv.Itoa(catalog.MaxFallbacks) + " profiles, `{\"agent\": ..., \"profile\": " +
			"...}`, to use in order when one is out of usage, another agent's included.",
		"model":  "the agent's own model, as its launch takes it; left out, inherited. Leave it out unless the user asks.",
		"effort": "the agent's own effort; left out, inherited. Leave it out unless the user asks.",
		"planMode": "`true` or `false`, for a terminal run alone; in the background the tools preset decides. " +
			"Left out, inherited.",
		"worktree": "`true` or `false`, for a terminal run alone; in the background the item's own worktree is " +
			"always used. Left out, inherited.",
		"launchFor": "rows, `{\"target\": \"acme-org\" or \"acme-org/api\", \"agent\", \"profile\", \"model\", " +
			"\"effort\"}`, that set the launch for one org or repository, a repository's row winning over its " +
			"org's; at most " + strconv.Itoa(spec.MaxLaunchRows) + ".",
		"fence": "what holds a background run in, below.",
		"budget": "`{\"usd\": 3}`, $" + dollarWords(catalog.MinRunCents) + " to $" + dollarWords(catalog.MaxRunCents) +
			" to the cent, which the agent keeps to itself, or `{\"tokens\": 200000}`, " +
			strconv.Itoa(catalog.MinRunTokens) + " to " + strconv.Itoa(catalog.MaxRunTokens) + ", which " +
			brand.Name + " stops it at; left out, $" + dollarWords(catalog.DefaultRunCents) + ".",
		"timeoutMinutes": "its time limit, a whole number, " + rangeWords(catalogRange(s)) + ".",
		"tools":          "its tools preset, below; left out, `" + string(catalog.DefaultPreset) + "`.",
		"allow": "extra commands it may run, each one line of at most " + strconv.Itoa(spec.MaxTextRunes) +
			" characters, at most " + strconv.Itoa(spec.MaxCommands) + "; they never loosen the fence's hard lines.",
		"deny": "extra commands it may not run, as `allow`.",
	}

	return words[key]
}

// catalogRange returns a setting's range, or none.
func catalogRange(s catalog.Setting) catalog.Range {
	if s.Range == nil {
		return catalog.Range{}
	}

	return *s.Range
}

// placeLines says where an agent step can run.
func placeLines() []string {
	places := catalog.Places()
	out := make([]string, 0, len(places))

	for _, pl := range places {
		line := tick(string(pl.ID)) + " (" + pl.Label + "): " + pl.Help
		if pl.ID == catalog.WhereTerminal {
			line += " It suits Ask first: on Autopilot it opens a terminal tab while the user works."
		}

		out = append(out, line)
	}

	return out
}

// presetLines says what each tools preset lets an agent do.
func presetLines() []string {
	presets := catalog.Presets()
	out := make([]string, 0, len(presets))

	for _, pr := range presets {
		line := tick(string(pr.ID)) + " (" + pr.Label + "): " + pr.Help

		switch {
		case pr.ReadOnly:
			line += " A `" + string(catalog.StepPostReview) + "` step, and an `onlyIf` on its verdict, can follow it."
		case pr.MayPush:
			line += " A `" + string(catalog.StepWaitForChecks) + "` step can follow it."
		}

		out = append(out, line)
	}

	return out
}

// githubTopic writes the GitHub step's actions, and what each sends.
func githubTopic(v Vocabulary) string {
	var p page

	p.heading(1, "The GitHub step")
	p.para("`" + string(catalog.StepGitHub) + "` sends GitHub one or more of the app's own writes, as the review " +
		"and issue sheets send them, in the order listed: 1 to " + strconv.Itoa(spec.MaxActions) + " actions, " +
		"each kind once, each an object holding exactly one action's key.")
	p.block("json", githubExample(v))

	actions := catalog.GitHubActions()
	lines := make([]string, 0, len(actions))

	for _, a := range actions {
		line := tick(string(a.Kind)) + " (" + a.Label + "): " + a.Help
		if !slices.Contains(a.Kinds, item.KindIssue) {
			line += " Pull requests only."
		}

		lines = append(lines, line+" "+githubActionWords(a.Kind))
	}

	p.list(lines...)
	p.para("Each action follows the routine's mode, is read from GitHub right before it goes and sent only " +
		"while the sheet still offers it, and is pinned to the head commit that read found. On an outside " +
		"contributor's item it waits for the user's OK in every mode. Every action counts toward Settings' " +
		"`actionsPerDay`. A GitHub action never merges, approves, requests changes, closes, or approves " +
		"workflow runs: merging and posting a review are the steps `" + string(catalog.StepMerge) + "` and `" +
		string(catalog.StepPostReview) + "`, and marking the item's GitHub notifications done is the step `" +
		string(catalog.StepMarkDone) + "`.")

	return p.String()
}

// githubActionWords says what one of the GitHub step's actions takes.
func githubActionWords(kind catalog.GitHubActionKind) string {
	switch kind {
	case catalog.GitHubRebase, catalog.GitHubUpdateBranch:
		return "It takes no settings: `{}`."
	case catalog.GitHubReply:
		return "It takes `{\"prompt\": ...}`, one of the library's comment replies (`purpose` `" +
			string(PurposeReply) + "`, `replyAction` `" + string(ReplyComment) + "`) by its `starter` or its " +
			"`nickname`, never `itemDefault`: the step only comments, so a reply that approves or requests " +
			"changes is refused."
	case catalog.GitHubLabels:
		return "It takes `{\"add\": [...], \"remove\": [...]}`, at least one label, at most " +
			strconv.Itoa(spec.MaxLabels) + " in each, each at most " + strconv.Itoa(catalog.MaxLabelRunes) +
			" characters, none both added and removed; it changes only the labels the item has."
	}

	return ""
}

// githubExample writes a GitHub step as a routine holds one, commenting the first comment
// reply the vocabulary's starters have.
func githubExample(v Vocabulary) string {
	reply := `{"nickname": "Question"}`

	for _, s := range v.Starters {
		if s.Purpose == PurposeReply && s.ReplyAction == ReplyComment {
			reply = `{"starter": "` + s.Key + `"}`

			break
		}
	}

	return `{"` + string(catalog.StepGitHub) + `": [{"` + string(catalog.GitHubReply) + `": {"prompt": ` + reply +
		`}}, {"` + string(catalog.GitHubLabels) + `": {"add": ["needs-repro"]}}]}`
}
