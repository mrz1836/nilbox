package kit

import (
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// watchTopic writes what a routine watches: the inbox's lists, or a search, field by
// field.
func watchTopic(_ Vocabulary) string {
	var p page

	p.heading(1, "What a routine watches")
	p.para("`watch.sources` says where a routine's items come from. Each source is either `lists`, the user's " +
		"inbox, or `search`, a GitHub search of its own, never both. A routine watches at most " +
		strconv.Itoa(spec.MaxSources) + " sources, at most " + strconv.Itoa(spec.MaxSearches) + " of them " +
		"searches; one whose trigger is `" + string(catalog.EventManual) + "` may watch none.")
	p.block("json", `{"sources": [{"lists": ["`+string(catalog.SourceAuthored)+`"]}, {"search": {"kind": "`+
		string(item.KindPR)+`", "authors": ["app/dependabot"], "where": ["`+watch.Me+`"]}}]}`)

	p.heading(2, "The inbox's lists")
	p.para("`lists` names the reasons an item is in the user's inbox, any of them. Every refresh reads the " +
		"inbox already, so watching it costs GitHub nothing more: prefer it whenever it holds the items.")

	lists := catalog.ListSources()
	lines := make([]string, 0, len(lists))

	for _, l := range lists {
		lines = append(lines, tick(string(l.ID))+" ("+l.Label+"): items "+l.Phrase+"; "+kindsWords(l.Kinds)+".")
	}

	p.list(lines...)

	p.heading(2, "A search")
	p.para("`search` is a GitHub search the routine reads on each refresh while it is on, which costs GitHub " +
		"requests: keep it narrow, and watch the inbox's lists instead when they hold the items. A search " +
		"returns the most recently updated open items it matches. Its fields, each of them optional:")
	p.list(searchKeyWords()...)
	p.para("Every search names a user, an org, or a repository in `where`, or asks for something of the " +
		"user's (`" + watch.Me + "` among the `authors`, or an `involving`), so it never scans all of GitHub. " +
		"Each list holds at most " + strconv.Itoa(watch.MaxValues) + " values. Every search adds " +
		"`is:open archived:false sort:updated-desc`, and its whole query is at most " +
		strconv.Itoa(watch.MaxQueryRunes) + " characters: split a longer one into two searches. No value may " +
		"hold a quote, a backslash, or a control character.")

	return p.String()
}

// searchKeyWords says what each of a search's fields takes, in the format's order.
func searchKeyWords() []string {
	words := map[string]string{
		"kind": tickedOr(item.Kinds()) + "; left out, both.",
		"authors": "who opened the items, any of them: `" + watch.Me + "` (the user), a login, or " +
			"`app/<bot>` for a bot, such as `app/dependabot`: a bot's bare name matches nothing.",
		"where": "the places the items are in, any of them: `" + watch.Me + "` (the user's account, " +
			"whichever is signed in), `user:<login>`, `org:<org>`, or `repo:<owner>/<name>`.",
		"involving": "how the items involve the user, all of them: " +
			offerWords(tickLabeled(watch.Involvings(), valueLabels(reflect.TypeFor[watch.Involving]()))) +
			"; `" + string(watch.InvolvingReviewRequested) + "` only for pull requests.",
		"labels": "labels the items carry, any of them, each at most " + strconv.Itoa(watch.MaxLabelRunes) +
			" characters and holding no comma.",
		"notLabels":  "labels the items carry none of.",
		"base":       "the branch pull requests merge into; pull requests only.",
		"headPrefix": "the start of the branch pull requests come from, such as `dependabot/`; pull requests only.",
		"state": "the items' states, all of them; pull requests only: " +
			offerWords(tickLabeled(watch.States(), valueLabels(reflect.TypeFor[watch.State]()))) +
			". Two of one kind, such as `" + string(watch.StatePassing) + "` and `" + string(watch.StateFailing) +
			"`, match nothing and are refused.",
		"more": "qualifiers typed by hand, such as `language:go` or `label:\"help wanted\"`, at most " +
			strconv.Itoa(watch.MaxMoreRunes) + " characters. One a routine's search can't take is refused, and " +
			"so are free text and the search's own: `is:` (but `is:public`, `is:private`, `is:locked`, and " +
			"`is:unlocked`), `sort:`, `archived:`, and `in:`.",
	}

	return keyWords(reflect.TypeFor[watch.Search](), words)
}

// keyWords writes each key of the struct type t that words has words for, in the format's
// order, as "`key`: words".
func keyWords(t reflect.Type, words map[string]string) []string {
	out := make([]string, 0, t.NumField())

	for field := range t.Fields() {
		key, _, ok := visibleKeyOf(t, field)
		if ok && words[key] != "" {
			out = append(out, tick(key)+": "+words[key])
		}
	}

	return out
}

// involvingWords says how an item involves the user, as a search asks for it.
func involvingWords(i watch.Involving) string {
	switch i {
	case watch.InvolvingAssigned:
		return "assigned to the user"
	case watch.InvolvingReviewRequested:
		return "asking for the user's review"
	case watch.InvolvingMentioned:
		return "mentioning the user"
	}

	return ""
}

// stateWords says what a search's state asks of an item.
func stateWords(s watch.State) string {
	switch s {
	case watch.StateReady:
		return "not a draft"
	case watch.StateDraft:
		return "a draft"
	case watch.StateFailing:
		return "its checks fail"
	case watch.StatePassing:
		return "its checks pass"
	case watch.StatePending:
		return "its checks are running"
	case watch.StateChangesRequested:
		return "changes requested"
	case watch.StateApproved:
		return "approved"
	case watch.StateReviewRequired:
		return "a review required"
	case watch.StateUnreviewed:
		return "no review yet"
	}

	return ""
}

// kindsWords says which kinds of item something is for: "pull requests only", "pull
// requests and issues".
func kindsWords(kinds []item.Kind) string {
	if len(kinds) == 1 {
		return kinds[0].Plural() + " only"
	}

	words := make([]string, 0, len(kinds))
	for _, k := range kinds {
		words = append(words, k.Plural())
	}

	return textutil.Series(words)
}

// triggersTopic writes each trigger, with its options.
func triggersTopic(_ Vocabulary) string {
	var p page

	p.heading(1, "Triggers")
	p.para("A routine has one trigger, `trigger.event`, with that trigger's options beside it in `trigger`: " +
		"`{\"event\": \"" + string(catalog.EventChecksFailed) + "\", \"settleMinutes\": 5}`. " +
		"Autopilot checks it on each refresh of the inbox and fires once per occurrence, such as once per " +
		"head commit, across refreshes and restarts; a routine's own `limits.oncePer` replaces the " +
		"trigger's. An option the trigger doesn't take is refused, and one left out takes its default. A " +
		"trigger for pull requests only needs a watch that holds pull requests.")

	for _, t := range catalog.Triggers() {
		p.b.WriteString("- " + triggerLine(t) + "\n")

		for _, o := range t.Options {
			p.b.WriteString("  - " + optionLine(o) + "\n")
		}
	}

	p.b.WriteString("\n")

	return p.String()
}

// triggerLine says what a trigger is: its event, when it fires, what it fires for, and
// what one occurrence is.
func triggerLine(t catalog.Trigger) string {
	once := "once per " + t.OncePer
	if rest, ok := strings.CutPrefix(t.OncePer, "each "); ok {
		once = "once for each " + rest
	}

	line := tick(string(t.Event)) + labelWords(string(t.Event), t.Label) + ": fires when an item " + t.Help +
		"; " + kindsWords(t.Kinds) + "; " + once + "."

	if note := triggerNotes()[t.Event]; note != "" {
		line += " " + note
	}

	return line
}

// triggerNotes say what a trigger's line in the catalog leaves out.
func triggerNotes() map[catalog.Event]string {
	return map[catalog.Event]string{
		catalog.EventNewCommits: "It never fires on a commit a routine's own run pushed, unless `" +
			string(catalog.OptionOwnPushesToo) + "` is true.",
		catalog.EventChecksFailed: "With `" + string(catalog.OptionWaitForChecks) + "`, it waits until every " +
			"check has finished and settled, so it fires once with every failure.",
		catalog.EventSchedule: "Its `" + string(catalog.OptionSchedule) + "` says when (topic `schedule`).",
		catalog.EventManual: "The user runs it from an item's menu, on that item, so its watch may be " +
			"empty.",
	}
}

// optionLine says what one of a trigger's options takes: its type, its range or that it
// is needed, and its default.
func optionLine(o catalog.OptionInfo) string {
	line := tick(string(o.ID)) + " (" + o.Label + "): "

	switch kind := triggerOptionKind(o.ID); {
	case o.Range != nil:
		line += "a whole number, " + rangeWords(*o.Range)
	case kind == reflect.Bool:
		line += "`true` or `false`, default `" + strconv.FormatBool(o.Default) + "`"
	case kind == reflect.String:
		line += "text, needed"
	default:
		line += "an object, needed"
	}

	return line + "."
}

// triggerOptionKind returns the kind of value a trigger's option holds, from the format's
// trigger: its key's type, through a pointer.
func triggerOptionKind(o catalog.Option) reflect.Kind {
	t := reflect.TypeFor[spec.Trigger]()

	for field := range t.Fields() {
		if key, _, ok := jsonKeyOf(field); ok && key == string(o) {
			return elemOf(field.Type).Kind()
		}
	}

	return reflect.Invalid
}

// rangeWords says a whole number's range and default: "1 to 10, default 2 tries".
func rangeWords(r catalog.Range) string {
	return strconv.Itoa(r.Min) + " to " + strconv.Itoa(r.Max) + " " + r.Unit + ", default " +
		strconv.Itoa(r.Default)
}

// scheduleTopic writes a trigger on a schedule: how often, at what time, and how it tells
// the user.
func scheduleTopic(_ Vocabulary) string {
	var p page

	p.heading(1, "A routine on a schedule")
	p.para("A routine whose trigger is `" + string(catalog.EventSchedule) + "` fires at a time of day by the " +
		"Mac's own clock, once a day, on the items its watch holds that meet its conditions then. A time " +
		"missed while the Mac slept runs once it wakes, the same day, never for a day gone by.")
	p.block("json", `{"event": "`+string(catalog.EventSchedule)+`", "schedule": {"every": "`+
		string(catalog.EveryWeek)+`", "on": "`+string(catalog.Monday)+`", "at": "09:00", "style": "`+
		string(catalog.ScheduleDigest)+`"}}`)

	everies := catalog.Everies()
	every := make([]string, 0, len(everies))

	for _, e := range everies {
		every = append(every, tick(string(e.ID))+" ("+e.Label+"): "+e.Help)
	}

	styles := catalog.ScheduleStyles()
	style := make([]string, 0, len(styles))

	for i, s := range styles {
		words := tick(string(s.ID)) + " (" + s.Label + "): " + s.Help
		if i == 0 {
			words += " The default: leave `style` out for it."
		}

		style = append(style, words)
	}

	p.list(
		"`every`, how often, needed: "+strings.Join(every, " "),
		"`at`, the time of day, needed: `\"HH:MM\"` on a 24-hour clock, such as `\"08:00\"`.",
		"`on`, the day of the week, for `"+string(catalog.EveryWeek)+"` alone, and needed there: "+
			tickedOr(catalog.Weekdays())+".",
		"`style`, how it tells the user of what it matched: "+strings.Join(style, " "),
	)
	p.para("A `" + string(catalog.ScheduleDigest) + "` needs `" + string(catalog.StepNotify) + "` as the " +
		"routine's first step: the matches made at one time tell the user once, together.")

	return p.String()
}

// conditionsTopic writes how conditions combine, and the operators each type of field
// takes.
func conditionsTopic(_ Vocabulary) string {
	var p page

	p.heading(1, "Conditions")
	p.para("`conditions` says which of the items the trigger fires on count. `match` is " +
		enumWords[spec.Match]() + "; `of` lists the conditions, and groups of them one level deep (a group " +
		"holds no group), at most " + strconv.Itoa(spec.MaxConditions) + " in all. With none, write `{\"match\": \"" +
		string(spec.MatchAll) + "\", \"of\": []}`.")
	p.block("json", `{"match": "`+string(spec.MatchAll)+`", "of": [{"field": "`+string(catalog.FieldDraft)+
		`", "op": "`+string(catalog.OpIsNo)+`"}, {"group": {"match": "`+string(spec.MatchAny)+`", "of": [{"field": "`+
		string(catalog.FieldLabels)+`", "op": "`+string(catalog.OpHasAny)+`", "values": ["bug"]}, {"field": "`+
		string(catalog.FieldLines)+`", "op": "`+string(catalog.OpLt)+`", "value": "400"}]}}]}`)
	p.para("A condition names a `field` (topic `fields`), an `op` its type takes, and what the op compares " +
		"against: one value in `value`, several in `values`, or none. Each value is one line of at most " +
		strconv.Itoa(spec.MaxTextRunes) + " characters, at most " + strconv.Itoa(spec.MaxValues) + " of them. A " +
		"condition on a field that doesn't apply to an item, such as a pull request's field on an issue, is " +
		"no; one on a value not read yet waits until it is read.")

	for _, typ := range catalog.Types() {
		p.heading(2, "`"+string(typ)+"`")
		p.para(typeWords(typ))
		p.list(opLines(typ)...)
	}

	return p.String()
}

// typeWords says how a type of field compares, and how its values are written.
func typeWords(t catalog.Type) string {
	switch t {
	case catalog.TypeText:
		return "One piece of text, compared ignoring case. A pattern takes `*` for any run of characters and " +
			"`?` for one; a regex is Go's (RE2), ignoring case."
	case catalog.TypeList:
		return "A list of text, such as the labels. A list whose values are a field's choices takes only " +
			tickedAnd(choiceListOps()) + "; one of paths matches its patterns as `.gitignore` does, the " +
			"others as globs. An \"all\" operator over an empty list is no, never yes."
	case catalog.TypeNumber:
		return "A whole number, written as its digits in a string: `\"400\"`."
	case catalog.TypeBool:
		return "Yes or no; its operators take no value."
	case catalog.TypeChoice:
		return "One of the field's choices (topic `fields`), compared ignoring case."
	case catalog.TypeDuration:
		return "How long ago something happened, against a length: a whole number of minutes, hours, or days, " +
			"such as `\"30m\"`, `\"2h\"`, or `\"14d\"`."
	}

	return ""
}

// choiceListOps are the operators a list field of choices takes, as the catalog says of
// the first such field.
func choiceListOps() []catalog.Op {
	for _, f := range catalog.Fields() {
		if f.Type == catalog.TypeList && len(f.Choices) > 0 {
			return catalog.OpsFor(f)
		}
	}

	return []catalog.Op{}
}

// opLines writes each operator of a type of field: what it takes, and its opposite.
func opLines(t catalog.Type) []string {
	var out []string

	for _, o := range catalog.Ops() {
		if o.Type != t {
			continue
		}

		line := tick(string(o.ID)) + labelWords(string(o.ID), o.Label) + ": " + arityWords(o.Arity) + "."
		if o.Help != "" {
			line += " " + o.Help
		}

		if o.Negation != "" {
			line += " Its opposite is `" + string(o.Negation) + "`."
		}

		out = append(out, line)
	}

	return out
}

// arityWords says what an operator compares against.
func arityWords(a catalog.Arity) string {
	switch a {
	case catalog.ArityNone:
		return "no value"
	case catalog.ArityOne:
		return "one value, in `value`"
	case catalog.ArityMany:
		return "one or more, in `values`"
	}

	return ""
}

// fieldsTopic writes every field a condition reads, by group.
func fieldsTopic(_ Vocabulary) string {
	var p page

	p.heading(1, "Fields")
	p.para("A condition reads one field, by its `id`. A field of pull requests alone says so, and a condition " +
		"on it is no for an issue. Each field's type decides the operators it takes (topic `conditions`); a " +
		"choice field, and a list of choices, takes only its choices, as written here.")

	fields := catalog.Fields()

	for _, g := range catalog.Groups() {
		p.heading(2, g.Label+" (`"+string(g.ID)+"`)")

		var lines []string

		for _, f := range fields {
			if f.Group == g.ID {
				lines = append(lines, fieldLine(f))
			}
		}

		p.list(lines...)
	}

	return p.String()
}

// fieldLine says what a field is: its id, label, type, kinds, choices, and patterns.
func fieldLine(f catalog.Field) string {
	line := tick(string(f.ID)) + " (" + f.Label + "): `" + string(f.Type) + "`"

	if !slices.Contains(f.Kinds, item.KindIssue) {
		line += ", pull requests only"
	}

	if len(f.Choices) > 0 {
		choices := make([]string, 0, len(f.Choices))
		for _, c := range f.Choices {
			choices = append(choices, tick(c.Value)+labelWords(c.Value, c.Label))
		}

		line += "; its choices: " + strings.Join(choices, ", ")
	}

	switch f.Patterns {
	case catalog.PatternsGlob:
		line += "; its patterns are globs"
	case catalog.PatternsGitignore:
		line += "; its patterns match as `.gitignore`'s do"
	}

	return line + "."
}

// limitsTopic writes a routine's own limits.
func limitsTopic(_ Vocabulary) string {
	var p page

	p.heading(1, "Limits")
	p.para("`limits` are the routine's own, each optional: one left out takes its default. Settings → " +
		"Autopilot's `agentsAtOnce`, `runsPerDay`, `actionsPerDay`, and `dailyCap` still hold over every " +
		"routine together (topic `settings`). A limit delays a match and never loses it: one over today's " +
		"limit starts the next day.")

	limits := catalog.Limits()
	lines := make([]string, 0, len(limits))

	for _, l := range limits {
		lines = append(lines, limitLine(l))
	}

	p.list(lines...)
	p.para("A routine's runs of its agent, GitHub, and script steps on one item count as its tries, across " +
		"occurrences: a fix that still fails puts a new commit at the head, which the routine fires on again " +
		"after `waitMinutes`, until its last try.")

	return p.String()
}

// limitLine says what one of the limits takes, by its key in the format.
func limitLine(l catalog.Setting) string {
	line := tick(l.ID) + " (" + l.Label + "): "

	switch l.ID {
	case "oncePer":
		line += offerWords(tickLabeled(valuesOfChoices(l.Choices), valueLabels(reflect.TypeFor[catalog.OncePer]()))) +
			"; left out, the trigger's own"
	case "activeHours":
		line += "`{\"days\": [...], \"from\": \"HH:MM\", \"to\": \"HH:MM\"}`, the days (" +
			tickedAnd(catalog.Weekdays()) + ") and the local hours it may start in; `from` after `to` runs " +
			"past midnight, and the two differ; left out, always"
	case "budgetPerDay":
		line += "`{\"usd\": 5}`, $" + dollarWords(catalog.MinDayCents) + " to $" + dollarWords(catalog.MaxDayCents) +
			" to the cent, or `{\"tokens\": 1000000}`, " + strconv.Itoa(catalog.MinDayTokens) + " to " +
			strconv.Itoa(catalog.MaxDayTokens) + "; left out, none. Settings' `dailyCap` still holds beside it"
	default:
		if l.Range != nil {
			line += "a whole number, " + rangeWords(*l.Range)
		}
	}

	return strings.TrimSuffix(line, ".") + "."
}

// valuesOfChoices returns the values of a setting's choices.
func valuesOfChoices(choices []catalog.Choice) []string {
	out := make([]string, 0, len(choices))
	for _, c := range choices {
		out = append(out, c.Value)
	}

	return out
}

// dollarWords writes cents as dollars: "0.50", "5".
func dollarWords(cents int) string {
	if cents%100 == 0 {
		return strconv.Itoa(cents / 100)
	}

	return strconv.Itoa(cents/100) + "." + strconv.Itoa(cents%100/10) + strconv.Itoa(cents%10)
}
