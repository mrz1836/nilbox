package kit

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"text/template/parse"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/heldruns"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/trust"
)

// The prompt library's own caps, which kit can't import: a prompt's title, as
// internal/prompts caps it, and its text, in characters (MaxPromptBody caps its bytes).
const (
	promptTitleRunes = 80
	promptTextRunes  = 20_000
)

// checkPrompts checks a proposal's new prompts, adds, each as the prompt library checks
// one as it is added, its nickname beside the library's and those of the new prompts
// before it, and works out each one's risks from the routines that run it.
func checkPrompts(snap Snapshot, adds []NewPrompt, routines []spec.Spec) []CheckedPrompt {
	out := make([]CheckedPrompt, 0, len(adds))
	taken := make([]string, 0, len(snap.Setup.Prompts)+len(adds))

	for _, p := range snap.Setup.Prompts {
		taken = append(taken, p.Nickname)
	}

	for i, np := range adds {
		out = append(out, CheckedPrompt{
			Index: i, Problems: promptProblems(np, taken, snap.Vocabulary.Variables),
			Risks: PromptRisks(np, routines), Prompt: np,
		})
		taken = append(taken, strings.TrimSpace(np.Nickname))
	}

	return out
}

// promptProblems returns what keeps np from being added to a library whose nicknames are
// taken, each a sentence, in the prompt library's words; never nil.
func promptProblems(np NewPrompt, taken []string, variables []Variable) []string {
	out := []string{}

	for _, why := range []string{
		titleProblem(np.Name), nicknameProblem(np.Nickname, taken), purposeProblem(np), reviewProblem(np),
		kindsProblem(np),
	} {
		if why != "" {
			out = append(out, why)
		}
	}

	return append(out, bodyProblems(np, variables)...)
}

// titleProblem says what is wrong with a prompt's title, or "".
func titleProblem(name string) string {
	name = strings.TrimSpace(name)

	switch {
	case name == "":
		return "Give the prompt a title."
	case utf8.RuneCountInString(name) > promptTitleRunes:
		return fmt.Sprintf("Keep the title to %d characters.", promptTitleRunes)
	case strings.ContainsFunc(name, notInALine):
		return "Keep the title to one line of plain text."
	default:
		return ""
	}
}

// nicknameProblem says what is wrong with a prompt's nickname, among the taken ones, or
// "".
func nicknameProblem(nickname string, taken []string) string {
	nickname = strings.TrimSpace(nickname)

	switch {
	case nickname == "":
		return "Give the prompt a nickname for the launch menu."
	case utf8.RuneCountInString(nickname) > spec.MaxNicknameRunes:
		return fmt.Sprintf("Keep the nickname to %d characters so it fits the launch button.", spec.MaxNicknameRunes)
	case strings.ContainsFunc(nickname, notInALine):
		return "Keep the nickname to one line of plain text."
	case slices.ContainsFunc(taken, func(t string) bool { return strings.EqualFold(t, nickname) }):
		return fmt.Sprintf("Another prompt is already called %q: give this one a nickname of its own.", nickname)
	default:
		return ""
	}
}

// purposeProblem says np's purpose isn't one, or "".
func purposeProblem(np NewPrompt) string {
	if knownPurpose(np.Purpose) {
		return ""
	}

	return fmt.Sprintf("Say what the prompt is for: %s.", orChoices([]Purpose{PurposeAgent, PurposeReply}))
}

// reviewProblem says what is wrong with the review np, a reply, is written for, or that
// np, an agent's prompt, names one, or "".
func reviewProblem(np NewPrompt) string {
	switch {
	case np.Purpose == PurposeReply && !knownReview(np.ReplyAction):
		return fmt.Sprintf("Choose the review the reply is for: %s.",
			orChoices([]ReplyAction{ReplyApprove, ReplyRequestChanges, ReplyComment}))
	case np.Purpose != PurposeReply && np.ReplyAction != "":
		return "Only a reply names the review it is for: leave replyAction out of an agent's prompt."
	default:
		return ""
	}
}

// kindsProblem says what is wrong with the kinds np applies to, or "": a reply that
// approves or requests changes is a review of a pull request alone.
func kindsProblem(np NewPrompt) string {
	switch {
	case len(promptKinds(np.AppliesTo)) == 0:
		return fmt.Sprintf("Choose what the prompt is for: %s.",
			orChoices([]AppliesTo{AppliesToPR, AppliesToIssue, AppliesToAny}))
	case np.Purpose == PurposeReply && knownReview(np.ReplyAction) && np.ReplyAction != ReplyComment &&
		np.AppliesTo != AppliesToPR:
		return fmt.Sprintf("A reply that approves or requests changes is a review of a pull request: say %q.",
			string(AppliesToPR))
	default:
		return ""
	}
}

// bodyProblems returns what is wrong with np's text, as the prompt library reads it before
// it parses it, then what its template names that the library doesn't have.
func bodyProblems(np NewPrompt, variables []Variable) []string {
	body := np.Body

	switch {
	case strings.TrimSpace(body) == "" && np.Purpose == PurposeReply:
		return []string{"Write the reply's text."}
	case strings.TrimSpace(body) == "":
		return []string{"Write what the agent should do."}
	case !utf8.ValidString(body) || strings.ContainsRune(body, 0):
		return []string{"The text holds characters that can't be sent to the agent."}
	case len(body) > MaxPromptBody:
		return []string{fmt.Sprintf("Keep the text to %d KB.", MaxPromptBody>>10)}
	case utf8.RuneCountInString(body) > promptTextRunes:
		return []string{fmt.Sprintf("Keep the text to %s characters.", digitGroups(promptTextRunes))}
	default:
		return templateProblems(body, variables)
	}
}

// templateProblems returns what the prompt library refuses in body as a template, as far
// as kit reads one: text Go's templates can't parse, {{range}}, {{template}}, {{define}},
// and {{block}}, and a variable variables doesn't name. With no variables to read, it
// reads none.
func templateProblems(body string, variables []Variable) []string {
	tree := parse.New("prompt")
	tree.Mode = parse.SkipFuncCheck
	trees := map[string]*parse.Tree{}

	if _, err := tree.Parse(body, "", "", trees); err != nil {
		return []string{"Its text isn't a template the prompt library can read: " +
			textutil.EndSentence(strings.TrimPrefix(err.Error(), "template: "))}
	}

	w := templateWalk{known: map[string]bool{}, out: []string{}}
	for _, v := range variables {
		w.known[v.Name] = true
	}

	if len(trees) > 1 {
		w.out = append(w.out, "{{define}} and {{block}} aren't available in prompts.")
	}

	w.list(tree.Root, true)

	return w.out
}

// templateWalk walks a prompt's template for what the prompt library refuses in it.
type templateWalk struct {
	known map[string]bool
	out   []string
}

// list walks every node of l, where the dot is the prompt's data when dotIsData, as it is
// everywhere but inside {{with}}.
func (w *templateWalk) list(l *parse.ListNode, dotIsData bool) {
	if l == nil {
		return
	}

	for _, n := range l.Nodes {
		switch n := n.(type) {
		case *parse.ActionNode:
			w.pipe(n.Pipe, dotIsData)
		case *parse.IfNode:
			w.pipe(n.Pipe, dotIsData)
			w.list(n.List, dotIsData)
			w.list(n.ElseList, dotIsData)
		case *parse.WithNode:
			w.pipe(n.Pipe, dotIsData)
			w.list(n.List, false)
			w.list(n.ElseList, dotIsData)
		case *parse.RangeNode:
			w.out = append(w.out, "{{range}} isn't available in prompts: there are no lists to go through.")
		case *parse.TemplateNode:
			w.out = append(w.out, "{{template}} isn't available in prompts.")
		}
	}
}

// pipe walks every argument of every command of p.
func (w *templateWalk) pipe(p *parse.PipeNode, dotIsData bool) {
	for _, cmd := range p.Cmds {
		for _, arg := range cmd.Args {
			w.arg(arg, dotIsData)
		}
	}
}

// arg walks one argument of a command: a variable, written .Name, or $.Name anywhere, or a
// pipeline in parentheses.
func (w *templateWalk) arg(arg parse.Node, dotIsData bool) {
	switch a := arg.(type) {
	case *parse.FieldNode:
		w.field(a.Ident, dotIsData)
	case *parse.VariableNode:
		if len(a.Ident) > 1 && a.Ident[0] == "$" {
			w.field(a.Ident[1:], true)
		}
	case *parse.PipeNode:
		w.pipe(a, dotIsData)
	}
}

// field checks a variable the template names: one of the variables, with nothing after
// it. One inside {{with}}, whose dot is another value, is the library's to check.
func (w *templateWalk) field(ident []string, dotIsData bool) {
	name := ident[0]

	switch {
	case !dotIsData || len(w.known) == 0:
	case !w.known[name]:
		w.out = append(w.out, w.unknown(name))
	case len(ident) > 1:
		w.out = append(w.out, fmt.Sprintf(".%s has no parts to pick: write .%s on its own.", name, name))
	}
}

// unknown says .name is no variable, suggesting the one spelled the same in another case.
func (w *templateWalk) unknown(name string) string {
	for known := range w.known {
		if strings.EqualFold(known, name) {
			return fmt.Sprintf(".%s isn't a variable a prompt can use: did you mean .%s?", name, known)
		}
	}

	return fmt.Sprintf(".%s isn't a variable a prompt can use: the prompts topic lists the ones it can.", name)
}

// settingRow is one of Autopilot's settings a proposal may change, by the key a proposal
// names it with, with its label in Settings → Autopilot and its value in the window's
// words.
type settingRow struct {
	key   SettingKey
	label string
	words func(autopilotprefs.Config) string
}

// settingRows are the settings a proposal may change, in the window's order.
func settingRows() []settingRow {
	count := func(n func(autopilotprefs.Config) int) func(autopilotprefs.Config) string {
		return func(c autopilotprefs.Config) string { return strconv.Itoa(n(c)) }
	}

	return []settingRow{
		{SettingDefaultMode, "New routines start in", func(c autopilotprefs.Config) string { return c.DefaultMode.Label() }},
		{SettingAgentsAtOnce, "Agents at once", count(func(c autopilotprefs.Config) int { return c.AgentsAtOnce })},
		{SettingRunsPerDay, "Agent runs a day", count(func(c autopilotprefs.Config) int { return c.RunsPerDay })},
		{SettingActionsPerDay, "GitHub actions a day", count(func(c autopilotprefs.Config) int { return c.ActionsPerDay })},
		{SettingDailyCap, "Daily cap", func(c autopilotprefs.Config) string { return capAmount(c.DailyCap) }},
		{
			SettingKeepAwake, "Keep the Mac awake while agents work",
			func(c autopilotprefs.Config) string { return settingSwitch(c.KeepAwake) },
		},
		{
			SettingHoldOnOutage, "Hold agent starts while their provider reports an outage",
			func(c autopilotprefs.Config) string { return settingSwitch(c.HoldOnOutage) },
		},
		{
			SettingCatchUpHours, "Ask before catching up after",
			func(c autopilotprefs.Config) string { return spanHours(c.CatchUpHours) },
		},
		{SettingHistoryDays, "History kept", func(c autopilotprefs.Config) string { return spanDays(c.HistoryDays) }},
		{SettingLogDays, "Run logs kept", func(c autopilotprefs.Config) string { return spanDays(c.LogDays) }},
	}
}

// checkSettings checks a settings change over before, the settings now: the settings it
// leaves, as Settings checks them, each setting it changes in the window's words, and its
// risks.
func checkSettings(before autopilotprefs.Config, c SettingsChange) CheckedSettings {
	after := applySettings(before, c)
	out := CheckedSettings{Changes: []SettingChange{}, Problems: []string{}, Risks: SettingsRisks(before, after), After: after}

	for _, row := range settingRows() {
		if was, now := row.words(before), row.words(after); was != now {
			out.Changes = append(out.Changes, SettingChange{Key: row.key, Label: row.label, Before: was, After: now})
		}
	}

	if err := after.Check(); err != nil {
		out.Problems = append(out.Problems, settingsProblem(err))
	}

	if len(out.Changes) == 0 {
		out.Problems = append(out.Problems, "It changes nothing: each setting it names is already so.")
	}

	return out
}

// SettingsSet words each setting the change c names as the value it sets, with no value
// before it: what a settings change the user dealt with set, which its card shows, since
// checked again against the settings it changed itself it finds nothing left to change.
func SettingsSet(c SettingsChange) []SettingChange {
	set, named := applySettings(autopilotprefs.Defaults(), c), c.named()
	out := []SettingChange{}

	for _, row := range settingRows() {
		if named[row.key] {
			out = append(out, SettingChange{Key: row.key, Label: row.label, After: row.words(set)})
		}
	}

	return out
}

// named reports each setting c names, by its key.
func (c SettingsChange) named() map[SettingKey]bool {
	return map[SettingKey]bool{
		SettingDefaultMode: c.DefaultMode != nil, SettingAgentsAtOnce: c.AgentsAtOnce != nil,
		SettingRunsPerDay: c.RunsPerDay != nil, SettingActionsPerDay: c.ActionsPerDay != nil,
		SettingDailyCap: c.DailyCap != nil, SettingKeepAwake: c.KeepAwake != nil,
		SettingHoldOnOutage: c.HoldOnOutage != nil, SettingCatchUpHours: c.CatchUpHours != nil,
		SettingHistoryDays: c.HistoryDays != nil, SettingLogDays: c.LogDays != nil,
	}
}

// RulesSet words each change the rules change c names as it sets it, whatever the rules
// are now: what a rules change the user dealt with set, which its card shows, since checked
// again against the rules it changed itself it finds nothing left to change. A name that
// isn't a bot's login, an org or a repository, or a workflow's is left out, and so is one
// named twice.
func RulesSet(c RulesChange) []RuleChange {
	b := rulesCheck{changes: []RuleChange{}, problems: []string{}}
	named := func(kind RuleKind, list []string) {
		for _, target := range list {
			b.changes = append(b.changes, ruleChange(kind, target))
		}
	}

	named(RuleTrust, b.botList(RuleTrust, c.Trust))
	named(RuleUntrust, b.botList(RuleUntrust, c.Untrust))

	seen := map[string]bool{}

	for _, step := range []struct {
		kind  RuleKind
		list  []string
		words string
	}{{RulePin, c.Pin, "Pin"}, {RuleMute, c.Mute, "Mute"}, {RuleClear, c.Clear, "Unpin or unmute"}} {
		for _, raw := range step.list {
			if name, err := reporules.ParseTarget(raw); err == nil && !seen[name.Key()] {
				seen[name.Key()] = true
				text := targetText(name)
				b.changes = append(b.changes, RuleChange{Kind: step.kind, Target: text, Words: step.words + " " + text})
			}
		}
	}

	named(RuleIgnoreWorkflows, b.workflowList(RuleIgnoreWorkflows, c.IgnoreWorkflows))
	named(RuleUnignoreWorkflows, b.workflowList(RuleUnignoreWorkflows, c.UnignoreWorkflows))

	return b.changes
}

// ruleChange is a change of kind to target, a bot or a workflow, in the window's words:
// "Trust renovate[bot]", "Stop ignoring Deploy preview".
func ruleChange(kind RuleKind, target string) RuleChange {
	words := map[RuleKind]string{
		RuleTrust: "Trust " + target + "[bot]", RuleUntrust: "Stop trusting " + target + "[bot]",
		RuleIgnoreWorkflows: "Ignore " + target, RuleUnignoreWorkflows: "Stop ignoring " + target,
	}[kind]

	return RuleChange{Kind: kind, Target: target, Words: words}
}

// applySettings returns c with s applied, as Settings' setters apply each: a shorter
// history shortens the run logs with it, and the run logs it names come after.
func applySettings(c autopilotprefs.Config, s SettingsChange) autopilotprefs.Config {
	applySetting(&c.DefaultMode, s.DefaultMode)
	applySetting(&c.AgentsAtOnce, s.AgentsAtOnce)
	applySetting(&c.RunsPerDay, s.RunsPerDay)
	applySetting(&c.ActionsPerDay, s.ActionsPerDay)
	applySetting(&c.DailyCap, s.DailyCap)
	applySetting(&c.KeepAwake, s.KeepAwake)
	applySetting(&c.HoldOnOutage, s.HoldOnOutage)
	applySetting(&c.CatchUpHours, s.CatchUpHours)

	if s.HistoryDays != nil {
		c.HistoryDays, c.LogDays = *s.HistoryDays, min(c.LogDays, *s.HistoryDays)
	}

	applySetting(&c.LogDays, s.LogDays)

	return c
}

// applySetting sets *setting to *to, when a change names it.
func applySetting[T any](setting, to *T) {
	if to != nil {
		*setting = *to
	}
}

// settingsProblem words a refusal of Settings' check.
func settingsProblem(err error) string {
	switch {
	case errors.Is(err, autopilotprefs.ErrUnknownMode):
		return fmt.Sprintf("Say %s for defaultMode.", orChoices(autopilotprefs.Modes()))
	case errors.Is(err, autopilotprefs.ErrUnknownUnit):
		return fmt.Sprintf("Say %s for dailyCap's unit.", orChoices(autopilotprefs.Units()))
	default:
		return textutil.Sentence(err.Error())
	}
}

// rulesCheck gathers a rules change's changes and problems as it is checked.
type rulesCheck struct {
	changes  []RuleChange
	problems []string
}

// checkRules checks a rules change over before, the rules now: the rules it leaves, each
// through its own package, each change in the window's words, and its risks.
func checkRules(before Rules, c RulesChange) CheckedRules {
	before = rulesLists(before)
	b := rulesCheck{changes: []RuleChange{}, problems: []string{}}

	// Each list is checked in the proposal's order, so its changes and problems are listed
	// in it.
	bots := b.bots(before.TrustedBots, c.Trust, c.Untrust)
	pinned, muted := b.targets(before, c)
	after := Rules{
		TrustedBots: bots, Pinned: pinned, Muted: muted,
		IgnoredWorkflows: b.workflows(before.IgnoredWorkflows, c.IgnoreWorkflows, c.UnignoreWorkflows),
	}

	if len(b.problems) == 0 && len(b.changes) == 0 {
		b.problems = append(b.problems, "It changes nothing: each rule it names is already so.")
	}

	return CheckedRules{Changes: b.changes, Problems: b.problems, Risks: RulesRisks(before, after), After: after}
}

// bots returns the trusted bots once trust and untrust apply over before, as
// trust.NormalizeBots stores them.
func (b *rulesCheck) bots(before, trusting, untrusting []string) []string {
	current := botsOf(before)
	trusts, untrusts := b.botList(RuleTrust, trusting), b.botList(RuleUntrust, untrusting)

	for _, bot := range trusts {
		switch {
		case slices.Contains(untrusts, bot):
			b.problems = append(b.problems, fmt.Sprintf("rules: %s is in both trust and untrust: keep it in one.", bot))
		case !slices.Contains(current, bot):
			current = append(current, bot)
			b.changes = append(b.changes, ruleChange(RuleTrust, bot))
		}
	}

	for _, bot := range untrusts {
		if i := slices.Index(current, bot); i >= 0 && !slices.Contains(trusts, bot) {
			current = slices.Delete(current, i, i+1)

			b.changes = append(b.changes, ruleChange(RuleUntrust, bot))
		}
	}

	after, err := trust.NormalizeBots(current)
	if err != nil {
		b.problems = append(b.problems, "rules.trust: "+textutil.Sentence(err.Error()))

		return textutil.NonNil(current)
	}

	return after
}

// botsOf returns the bots list names, each as trust.NormalizeBots stores it, once,
// leaving out one that isn't a bot's login: never nil.
func botsOf(list []string) []string {
	out := []string{}

	for _, raw := range list {
		if bots, err := trust.NormalizeBots([]string{raw}); err == nil && len(bots) == 1 && !slices.Contains(out, bots[0]) {
			out = append(out, bots[0])
		}
	}

	return out
}

// botList returns the bots a rules change's list names, each as trust.NormalizeBots stores
// it, once; each that isn't one is a problem.
func (b *rulesCheck) botList(key RuleKind, list []string) []string {
	out := []string{}

	for i, raw := range list {
		bots, err := trust.NormalizeBots([]string{raw})

		switch {
		case err != nil:
			b.problems = append(b.problems, fmt.Sprintf("rules.%s[%d]: %q isn't a bot's login GitHub could give: "+
				"write its login, as renovate or renovate[bot].", key, i, textutil.Truncate(raw, 60)))
		case len(bots) == 0:
			b.problems = append(b.problems, fmt.Sprintf("rules.%s[%d] is empty: name a bot, or leave it out.", key, i))
		case !slices.Contains(out, bots[0]):
			out = append(out, bots[0])
		}
	}

	return out
}

// targets returns the pinned and muted repositories and orgs once c's pins, mutes, and
// clears apply over before's, in turn, through reporules.
func (b *rulesCheck) targets(before Rules, c RulesChange) (pinned, muted []string) {
	rules := reporules.Rules{Pinned: slices.Clone(before.Pinned), Muted: slices.Clone(before.Muted)}
	seen := map[string]bool{}

	for _, step := range []struct {
		key  RuleKind
		list []string
		rule reporules.Rule
	}{
		{RulePin, c.Pin, reporules.RulePinned}, {RuleMute, c.Mute, reporules.RuleMuted}, {RuleClear, c.Clear, reporules.RuleNone},
	} {
		for i, raw := range step.list {
			rules = b.target(rules, seen, fmt.Sprintf("rules.%s[%d]", step.key, i), step.key, raw, step.rule)
		}
	}

	return textutil.NonNil(rules.Pinned), textutil.NonNil(rules.Muted)
}

// target applies rule to the target raw, at where in the change's list key, over rules,
// and returns the rules it leaves; seen are the targets named before it, by key.
func (b *rulesCheck) target(rules reporules.Rules, seen map[string]bool, where string, key RuleKind, raw string,
	rule reporules.Rule,
) reporules.Rules {
	name, err := reporules.ParseTarget(raw)
	if err != nil {
		b.problems = append(b.problems, fmt.Sprintf("%s: %q isn't an org or a repository: write one as acme-org or "+
			"acme-org/example-repo.", where, textutil.Truncate(raw, 60)))

		return rules
	}

	text := targetText(name)
	if seen[name.Key()] {
		b.problems = append(b.problems, fmt.Sprintf("%s: %s is named more than once: name it once.", where, text))

		return rules
	}

	seen[name.Key()] = true
	was := ownRule(rules, name.Key())

	next, err := rules.Set(raw, rule)
	if err != nil {
		b.problems = append(b.problems, where+": "+textutil.Sentence(err.Error()))

		return rules
	}

	if was != rule {
		b.changes = append(b.changes, RuleChange{Kind: key, Target: text, Words: ruleWords(was, rule) + " " + text})
	}

	return next
}

// workflows returns the ignored workflows once ignoring and unignoring apply over before,
// as heldruns stores them.
func (b *rulesCheck) workflows(before, ignoring, unignoring []string) []string {
	current := slices.Clone(before)
	ignores, unignores := b.workflowList(RuleIgnoreWorkflows, ignoring), b.workflowList(RuleUnignoreWorkflows, unignoring)

	for _, name := range ignores {
		switch {
		case slices.Contains(unignores, name):
			b.problems = append(b.problems, fmt.Sprintf("rules: %s is in both ignoreWorkflows and unignoreWorkflows: "+
				"keep it in one.", name))
		case !slices.Contains(current, name):
			current = append(current, name)
			b.changes = append(b.changes, ruleChange(RuleIgnoreWorkflows, name))
		}
	}

	for _, name := range unignores {
		if i := slices.Index(current, name); i >= 0 && !slices.Contains(ignores, name) {
			current = slices.Delete(current, i, i+1)

			b.changes = append(b.changes, ruleChange(RuleUnignoreWorkflows, name))
		}
	}

	if _, err := heldruns.Encode(current); err != nil {
		b.problems = append(b.problems, "rules.ignoreWorkflows: "+textutil.Sentence(err.Error()))
	}

	return textutil.NonNil(current)
}

// workflowList returns the workflows a rules change's list names, each trimmed, once; each
// that heldruns refuses, or that isn't one line of plain text, is a problem.
func (b *rulesCheck) workflowList(key RuleKind, list []string) []string {
	out := []string{}

	for i, raw := range list {
		name := strings.TrimSpace(raw)
		_, err := heldruns.Clean([]string{name})

		switch {
		case name == "":
			b.problems = append(b.problems, fmt.Sprintf("rules.%s[%d] is empty: name a workflow, or leave it out.", key, i))
		case err != nil:
			b.problems = append(b.problems, fmt.Sprintf("rules.%s[%d]: %s", key, i, textutil.Sentence(err.Error())))
		case strings.ContainsFunc(name, notInALine):
			b.problems = append(b.problems, fmt.Sprintf("rules.%s[%d]: keep a workflow's name to one line of plain "+
				"text, as GitHub shows it.", key, i))
		case !slices.Contains(out, name):
			out = append(out, name)
		}
	}

	return out
}

// targetText writes a rule's target as it is stored: "owner" or "owner/name".
func targetText(n repo.Name) string {
	if n.Repo == "" {
		return n.Owner
	}

	return n.String()
}

// ownRule returns the rule the target whose key is key has of its own in r, never its
// org's.
func ownRule(r reporules.Rules, key string) reporules.Rule {
	has := func(list []string) bool {
		return slices.ContainsFunc(list, func(t string) bool {
			n, err := reporules.ParseTarget(t)

			return err == nil && n.Key() == key
		})
	}

	switch {
	case has(r.Pinned):
		return reporules.RulePinned
	case has(r.Muted):
		return reporules.RuleMuted
	default:
		return reporules.RuleNone
	}
}

// ruleWords says a target's rule going from was to rule, as the line's verb: "Pin",
// "Mute", "Unpin", "Unmute".
func ruleWords(was, rule reporules.Rule) string {
	switch rule {
	case reporules.RulePinned:
		return "Pin"
	case reporules.RuleMuted:
		return "Mute"
	case reporules.RuleNone:
	}

	if was == reporules.RuleMuted {
		return "Unmute"
	}

	return "Unpin"
}
