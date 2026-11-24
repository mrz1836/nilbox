package kit

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// topicEntry is one topic autopilot_reference answers: its name, the line the guide gives
// it, and what writes it from the vocabulary.
type topicEntry struct {
	name  string
	line  string
	write func(v Vocabulary) string
}

// topicTable is every topic, in the order the guide lists them.
func topicTable() []topicEntry {
	return []topicEntry{
		{"proposal", "what a proposal holds, part by part, with a worked example", proposalTopic},
		{"routine", "a routine's definition, every key with its type", routineTopic},
		{"watch", "what a routine watches: the inbox's lists, or a search of its own, field by field", watchTopic},
		{"triggers", "the triggers a routine fires on, with their options", triggersTopic},
		{"schedule", "a routine on a schedule: how often, at what time, and how it tells the user", scheduleTopic},
		{"conditions", "how conditions combine, and the operators each type of field takes", conditionsTopic},
		{"fields", "every field a condition reads, by group, with its type and its choices", fieldsTopic},
		{"steps", "every step and its settings, Only if, the modes, and the order rules", stepsTopic},
		{"agent", "the agent step: its prompt, where it runs, its launch, and its fence", agentTopic},
		{"github", "the GitHub step's actions, and what each sends", githubTopic},
		{"limits", "a routine's own limits: once per, tries, runs a day, active hours, a daily budget", limitsTopic},
		{"settings", "Autopilot's settings, with their ranges and defaults", settingsTopic},
		{"rules", "trusted bots, pinned and muted repositories and orgs, ignored workflows", rulesTopic},
		{"prompts", "how a step names a prompt, the starters, the variables, and a new prompt", promptsTopic},
		{"templates", "the gallery's templates: what each does, its blanks, and its definition", templatesTopic},
		{"schema", "the proposal's JSON Schema", schemaTopic},
	}
}

// Topics returns the names of the topics autopilot_reference answers, in the order the
// guide lists them.
func Topics() []string {
	table := topicTable()
	out := make([]string, 0, len(table))

	for _, t := range table {
		out = append(out, t.name)
	}

	return out
}

// Topic returns what autopilot_reference answers for the topic name, written from this
// build's catalog and spec and the vocabulary v, and false for a name that is no topic.
func Topic(name string, v Vocabulary) (string, bool) {
	for _, t := range topicTable() {
		if t.name == name {
			return t.write(v), true
		}
	}

	return "", false
}

// proposalTopic writes the proposal's parts, its shape, and a worked example.
func proposalTopic(v Vocabulary) string {
	var p page

	p.heading(1, "The proposal")
	p.para("A proposal is one JSON object, given as `proposal` to `" + ToolCheck + "` and `" + ToolPropose +
		"`. The user reviews it in the Autopilot tab as one card, whose parts apply in this order: `prompts`, " +
		"then `routines`, then `settings`, then `rules`, so a routine may run a prompt the same proposal " +
		"adds. The user applies or discards each part on its own.")
	p.block("", strings.Join(shapeLines(reflect.TypeFor[Proposal](), reflect.TypeFor[spec.Spec]()), "\n"))
	p.list(proposalKeyWords()...)
	p.para("A proposal holds at most " + strconv.Itoa(MaxRoutines) + " routines and " + strconv.Itoa(MaxPrompts) +
		" prompts, and one that holds nothing is refused. Every key is spelled exactly as above: a key the " +
		"format doesn't have, or one spelled in another case, is refused, and so is a key given twice.")

	p.heading(2, "A worked example")
	p.para("This example holds every part at once to show the format; a real proposal holds only what the " +
		"user asked for. It starts from a template, runs a new prompt in place of the template's, caps " +
		"Autopilot's daily spend, and mutes a repository.")

	if text, err := proposalJSON(exampleProposal(v)); err == nil {
		p.block("json", text)
	}

	return p.String()
}

// proposalKeyWords says what each of a proposal's parts holds.
func proposalKeyWords() []string {
	return []string{
		"`summary`: one or two sentences, in plain words, that the card shows first: what the proposal does.",
		"`prompts`: new prompts for the library (topic `prompts`).",
		"`routines`: new routines, and changes to the user's routines. `name` is one line of at most " +
			strconv.Itoa(draft.MaxNameRunes) + " characters, and `description` at most " +
			strconv.Itoa(draft.MaxDescriptionRunes) + "; a name another routine has takes a free one, as a " +
			"save would. `spec` is its whole definition (topic `routine`), and `template` the gallery " +
			"template it starts from, if any (topic `templates`).",
		"A new routine has no `id`. Its `mode` is the mode you suggest: the card shows it as a hint beside the " +
			"mode the routine starts in, which Settings gives new routines, and never applies it.",
		"A change to one of the user's routines gives its `id`, and its `updatedAt` from `" + ToolMySetup +
			"` as `base`, exactly as written there, so the card can say if it changed since you read it. It " +
			"gives the routine's `name` (its own, unless the user asks for another), its `description`, and " +
			"its whole `spec` as it should be once changed. It has no `mode`: a routine keeps its own mode and " +
			"switch.",
		"`settings`: only the settings that change (topic `settings`).",
		"`rules`: the rules to change (topic `rules`).",
	}
}

// exampleProposal builds the worked example from the vocabulary: a routine from one of its
// templates, run with a new prompt, a cap on the daily spend, and a muted repository, so
// the example is always the format this build reads.
func exampleProposal(v Vocabulary) Proposal {
	prompt := NewPrompt{
		Name:      "Fix failing checks, starting with the tests",
		Nickname:  "Tests first",
		Purpose:   PurposeAgent,
		AppliesTo: AppliesToPR,
		Body:      exampleBody(v.Variables),
	}

	routine := RoutineChange{Name: "Fix my failing checks", Mode: autopilotprefs.ModeAsk, Spec: exampleSpec()}

	if t, ok := exampleTemplate(v.Templates); ok {
		routine.Name, routine.Template, routine.Mode, routine.Spec = t.Name, t.Key, t.Suggests, t.Spec
	}

	routine.Description = "Fixes the failing checks on my pull requests, starting with the tests."
	routine.Spec = withPrompt(routine.Spec, spec.PromptRef{Nickname: prompt.Nickname})
	capUSD := 5

	return Proposal{
		Summary: "Fixes failing checks on your pull requests in the background with a new prompt that " +
			"starts with the tests, caps Autopilot at $" + strconv.Itoa(capUSD) + " a day, and mutes " +
			"acme-org/legacy.",
		Prompts:  []NewPrompt{prompt},
		Routines: []RoutineChange{routine},
		Settings: &SettingsChange{DailyCap: &autopilotprefs.DailyCap{Amount: capUSD, Unit: autopilotprefs.UnitUSD}},
		Rules:    &RulesChange{Mute: []string{"acme-org/legacy"}},
	}
}

// exampleBody writes the example prompt's text, naming only the variables the vocabulary
// has.
func exampleBody(variables []Variable) string {
	has := func(name string) bool {
		return slices.ContainsFunc(variables, func(v Variable) bool { return v.Name == name })
	}

	body := "Fix the failing checks on this pull request"
	if has("Repo") && has("Number") {
		body = "Fix the failing checks on {{.Repo}} #{{.Number}}"
	}

	if has("FailedChecks") {
		body += " ({{.FailedChecks}})"
	}

	return body + ". Start with the tests: run them, read what fails, fix the cause, and push once."
}

// exampleTemplate picks the template the example starts from: one that loads, runs an
// agent in the background on a starter prompt, watches only the inbox's lists, and waits
// for the checks on what it pushed; else the one that comes closest, the gallery's order
// breaking ties.
func exampleTemplate(templates []Template) (Template, bool) {
	best, bestScore := -1, -1

	for i, t := range templates {
		if score := exampleScore(t.Spec); score > bestScore {
			best, bestScore = i, score
		}
	}

	if best < 0 || bestScore == 0 {
		return Template{}, false
	}

	return templates[best], true
}

// exampleScore scores a template's spec for the example: how many of the example's
// wishes it meets, 0 for one that doesn't load or has no agent step.
func exampleScore(sp spec.Spec) int {
	agent := exampleAgent(sp)
	if agent == nil || spec.Validate(spec.Normalize(sp)) != nil {
		return 0
	}

	score := 1

	for _, ok := range []bool{
		agent.Place() == catalog.WhereBackground && agent.MayPush(),
		agent.Prompt.Starter != "",
		!slices.ContainsFunc(sp.Watch.Sources, func(s spec.Source) bool { return s.Search != nil }),
		draft.HasStep(sp, catalog.StepWaitForChecks),
	} {
		if ok {
			score++
		}
	}

	return score
}

// exampleAgent returns a spec's agent step, nil for none.
func exampleAgent(sp spec.Spec) *spec.AgentStep {
	for _, st := range sp.Steps {
		if st.Agent != nil {
			return st.Agent
		}
	}

	return nil
}

// exampleSpec is the example's routine when the vocabulary has no template to start from:
// the user's own pull requests, an agent in the background when their checks fail, then a
// wait for the checks on what it pushed and a notice of how they came out.
func exampleSpec() spec.Spec {
	return spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{{Lists: []catalog.ListSource{catalog.SourceAuthored}}}},
		Trigger: spec.Trigger{Event: catalog.EventChecksFailed},
		Conditions: spec.Conditions{
			Match: spec.MatchAll, Of: []spec.Clause{{Field: catalog.FieldDraft, Op: catalog.OpIsNo}},
		},
		Steps: []spec.Step{
			{Agent: &spec.AgentStep{Where: catalog.WhereBackground, Fence: &spec.Fence{Tools: catalog.PresetEditTestPush}}},
			{WaitForChecks: &spec.WaitStep{}, OnlyIf: catalog.OutcomePushed},
			{TellMe: &spec.TellStep{When: catalog.TellVerdict}},
		},
	}
}

// withPrompt returns sp with its agent step running ref, its lists copied so the spec it
// came from is left as it was.
func withPrompt(sp spec.Spec, ref spec.PromptRef) spec.Spec {
	sp.Steps = slices.Clone(sp.Steps)

	for i, st := range sp.Steps {
		if st.Agent != nil {
			a := *st.Agent
			a.Prompt = ref
			sp.Steps[i].Agent = &a

			break
		}
	}

	return sp
}

// proposalJSON writes a proposal as the example shows it: indented, as encoding/json
// writes it, its HTML characters left as they are.
func proposalJSON(p Proposal) (string, error) {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	if err := enc.Encode(p); err != nil {
		return "", err
	}

	return strings.TrimRight(buf.String(), "\n"), nil
}

// compactJSON writes v as one line of JSON, as encoding/json writes it, its HTML characters
// left as they are; "" when it can't be written.
func compactJSON(v any) string {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(v); err != nil {
		return ""
	}

	return strings.TrimRight(buf.String(), "\n")
}

// routineTopic writes a routine's definition, every key with its type.
func routineTopic(_ Vocabulary) string {
	var p page

	p.heading(1, "A routine's definition")
	p.para("A routine's `spec` is one JSON object. Every key is spelled exactly as below, and one marked `?` " +
		"may be left out, to take its default. A key the format doesn't have, or one spelled in another case, " +
		"is refused. Each part has a topic of its own: `watch`, `triggers` and `schedule`, `conditions` and " +
		"`fields`, `steps`, `agent`, `github`, and `limits`.")
	p.block("", strings.Join(shapeLines(reflect.TypeFor[spec.Spec]()), "\n"))
	p.list(
		"`version` is always "+strconv.Itoa(spec.Version)+", the format this build reads and writes.",
		"`notify` says which of the routine's notices show a banner (`true`) or only an entry in the "+
			"Notifications page (`false`); a key left out follows Settings → Notifications. `alert` is its "+
			"Notify me and Tell me, `needsYou` a match that waits for the user's OK, `finished` its agent's run "+
			"ending, `checks` Wait for the checks' verdict, and `problem` the routine failing to run.",
		"`requires` names the programs the routine needs beyond what "+brand.Name+" works out on its own, "+
			"such as `pnpm` or `govulncheck`, at most "+strconv.Itoa(spec.MaxRequires)+".",
		"A routine's name and description sit beside its definition, in the proposal's routine; its mode and "+
			"its switch are the user's.",
	)

	return p.String()
}

// templatesTopic writes each of the gallery's templates: what it does, its blanks, and its
// definition.
func templatesTopic(v Vocabulary) string {
	var p page

	p.heading(1, "Templates")
	p.para("The gallery's templates are routines " + brand.Name + " ships ready to use. To start from one, copy " +
		"its definition into the routine's `spec`, fill in its blanks, change only what the user asked for, " +
		"set the routine's `template` to its key, and give its suggested mode as the routine's `mode`.")

	if len(v.Templates) == 0 {
		p.para("This build ships no templates.")
	}

	for _, t := range v.Templates {
		p.heading(2, tick(t.Key)+": "+vocabLine(t.Name))
		p.para(vocabLine(t.Summary))

		lines := make([]string, 0, 1+len(t.Blanks))
		lines = append(lines, "Suggests "+tickModes(t.Suggests)+".")

		for _, b := range t.Blanks {
			lines = append(lines, blankWords(b))
		}

		p.list(lines...)
		p.block("json", compactJSON(t.Spec))
	}

	return p.String()
}

// blankWords says what one of a template's blanks asks for, and where in its definition it
// goes.
func blankWords(b Blank) string {
	text := "Blank " + tick(b.Kind) + " (" + vocabLine(b.Label) + "): " + textutil.EndSentence(vocabLine(b.Help))

	switch b.Kind {
	case "where":
		text += " It fills `watch.sources[" + strconv.Itoa(b.Source) + "].search.where`."
	case "checks":
		text += " It fills `conditions.of[" + strconv.Itoa(b.Condition) + "].values`."
	}

	if b.Required {
		text += " Required: the routine can't run until it is filled."
	}

	return text
}

// schemaTopic writes the proposal's JSON Schema.
func schemaTopic(_ Vocabulary) string {
	return "# The proposal's JSON Schema\n\nDraft 2020-12, generated from this build's format: every key, every " +
		"closed set of values, and the ranges the catalog sets.\n\n```json\n" + string(Schema()) + "```\n"
}

// shapeLines writes the shape of each struct type reachable from root, in the order a walk
// reaches them, as TypeScript writes an interface: "Spec {", a line a key, "}". The walk
// stops at the types in stop, which another topic writes.
func shapeLines(root reflect.Type, stop ...reflect.Type) []string {
	var lines []string

	for _, t := range shapeStructs(root, stop) {
		head := t.Name() + " {"
		if note := shapeStructNotes()[t]; note != "" {
			head += "  // " + note
		}

		lines = append(lines, head)

		for field := range t.Fields() {
			if line, ok := shapeKey(t, field); ok {
				lines = append(lines, "  "+line)
			}
		}

		lines = append(lines, "}")
	}

	return lines
}

// shapeStructs returns each struct type reachable from root, through pointers and lists,
// in the order a walk reaches them, each once, never walking into the types in stop.
func shapeStructs(root reflect.Type, stop []reflect.Type) []reflect.Type {
	var (
		out   []reflect.Type
		seen  = map[reflect.Type]bool{}
		queue = []reflect.Type{root}
	)

	for _, t := range stop {
		seen[t] = true
	}

	for len(queue) > 0 {
		t := queue[0]
		queue = queue[1:]

		if seen[t] {
			continue
		}

		seen[t] = true
		out = append(out, t)

		for field := range t.Fields() {
			if inner := structIn(field.Type); inner != nil && !seen[inner] {
				queue = append(queue, inner)
			}
		}
	}

	return out
}

// structIn returns the struct a field of type t holds, through pointers and lists, or nil.
func structIn(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}

	if t.Kind() == reflect.Struct {
		return t
	}

	return nil
}

// jsonKeyOf returns a struct field's JSON key and whether it may be left out (omitempty),
// and false for a field JSON leaves alone.
func jsonKeyOf(f reflect.StructField) (key string, optional, ok bool) {
	tag := f.Tag.Get("json")
	if tag == "-" || !f.IsExported() {
		return "", false, false
	}

	name, opts, _ := strings.Cut(tag, ",")
	if name == "" {
		name = f.Name
	}

	return name, slices.Contains(strings.Split(opts, ","), "omitempty"), true
}

// shapeKey writes one key of struct t: its name, "?" when it may be left out, its type, and
// a note on where its values are listed.
func shapeKey(t reflect.Type, f reflect.StructField) (string, bool) {
	key, optional, ok := visibleKeyOf(t, f)
	if !ok {
		return "", false
	}

	if optional {
		key += "?"
	}

	typ := shapeType(f.Type)
	if fixed, ok := shapeFixed()[t.Name()+"."+f.Name]; ok {
		typ = fixed
	}

	line := key + ": " + typ
	if note := shapeTypeNotes()[elemOf(f.Type)]; note != "" {
		line += "  // " + note
	}

	return line, true
}

// elemOf returns the type a field of type t holds, through pointers and lists.
func elemOf(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}

	return t
}

// shapeType writes a type as TypeScript would: a closed set of values as their union, a
// list with [], a struct by its name.
func shapeType(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		return shapeType(t.Elem())
	}

	if t.Kind() == reflect.Slice {
		if inner := shapeType(t.Elem()); strings.Contains(inner, " | ") {
			return "(" + inner + ")[]"
		}

		return shapeType(t.Elem()) + "[]"
	}

	if t.Kind() == reflect.Struct {
		return t.Name()
	}

	if t.Kind() == reflect.String {
		return shapeString(t)
	}

	if t.Kind() == reflect.Bool {
		return "boolean"
	}

	return "number"
}

// shapeString writes a string type: its closed set's values as a union, when it has one
// short enough to list here, else string.
func shapeString(t reflect.Type) string {
	set, ok := closedSetOf(t)
	if !ok || shapeTypeNotes()[t] != "" {
		return "string"
	}

	quoted := make([]string, 0, len(set.Values))
	for _, v := range set.Values {
		quoted = append(quoted, strconv.Quote(v))
	}

	return strings.Join(quoted, " | ")
}

// shapeFixed are the keys whose type is a single value, by "<struct>.<field>".
func shapeFixed() map[string]string {
	return map[string]string{"Spec.Version": strconv.Itoa(spec.Version)}
}

// shapeStructNotes say what a struct's keys can't: which of them go together.
func shapeStructNotes() map[reflect.Type]string {
	return map[reflect.Type]string{
		reflect.TypeFor[spec.Source]():       "exactly one of lists and search",
		reflect.TypeFor[spec.Clause]():       "a condition (field, op, and value or values), or a group",
		reflect.TypeFor[spec.Step]():         "exactly one kind of step, and onlyIf",
		reflect.TypeFor[spec.GitHubAction](): "exactly one action",
		reflect.TypeFor[spec.PromptRef]():    "exactly one of starter, nickname, and itemDefault",
		reflect.TypeFor[spec.Budget]():       "exactly one of usd and tokens",
		reflect.TypeFor[spec.Empty]():        "no keys: {}",
	}
}

// shapeTypeNotes say where the values of a closed set too long to list in a shape are
// listed.
func shapeTypeNotes() map[reflect.Type]string {
	return map[reflect.Type]string{
		reflect.TypeFor[catalog.FieldID](): "a field's id: topic fields",
		reflect.TypeFor[catalog.Op]():      "an operator: topic conditions",
		reflect.TypeFor[catalog.Event]():   "a trigger's event: topic triggers",
		reflect.TypeFor[spec.Spec]():       "a routine's definition: topic routine",
	}
}

// proposalClosedSets are the closed sets of values a proposal holds: the spec's
// (spec.ClosedSets), and those of a new prompt and of Autopilot's settings, each with the
// Go type that holds it.
func proposalClosedSets() []spec.ClosedSet {
	return append(spec.ClosedSets(),
		kitSet("prompt.purpose", false, []Purpose{PurposeAgent, PurposeReply}),
		kitSet("prompt.replyAction", true, []ReplyAction{ReplyApprove, ReplyRequestChanges, ReplyComment}),
		kitSet("prompt.appliesTo", false, []AppliesTo{AppliesToPR, AppliesToIssue, AppliesToAny}),
		kitSet("mode", true, autopilotprefs.Modes()),
		kitSet("dailyCap.unit", false, autopilotprefs.Units()),
	)
}

// kitSet returns the closed set named name, which T holds with values, blank when its
// field may be left out.
func kitSet[T ~string](name string, blank bool, values []T) spec.ClosedSet {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, string(v))
	}

	return spec.ClosedSet{Name: name, Type: reflect.TypeFor[T](), Values: out, Blank: blank}
}

// closedSetOf returns the closed set the type t holds.
func closedSetOf(t reflect.Type) (spec.ClosedSet, bool) {
	for _, s := range proposalClosedSets() {
		if s.Type == t {
			return s, true
		}
	}

	return spec.ClosedSet{}, false
}

// valueLabels returns the words the app gives each value of the closed set the type t
// holds, by value, from the catalog's lists where it has them: "Checks fail" for
// checks_failed.
func valueLabels(t reflect.Type) map[string]string {
	out := map[string]string{}

	for _, pair := range labelPairs(t) {
		out[pair[0]] = pair[1]
	}

	return out
}

// labelPairs returns the value and the words of each value of the closed set t holds.
func labelPairs(t reflect.Type) [][2]string {
	if pairs := catalogLabelPairs(t); pairs != nil {
		return pairs
	}

	switch t {
	case reflect.TypeFor[autopilotprefs.Mode]():
		return pairsOf(autopilotprefs.Modes(), func(m autopilotprefs.Mode) [2]string { return [2]string{string(m), m.Label()} })
	case reflect.TypeFor[item.Kind]():
		return pairsOf(item.Kinds(), func(k item.Kind) [2]string { return [2]string{string(k), k.Plural()} })
	case reflect.TypeFor[spec.Match]():
		return [][2]string{{string(spec.MatchAll), "every condition"}, {string(spec.MatchAny), "any condition"}}
	case reflect.TypeFor[watch.Involving]():
		return pairsOf(watch.Involvings(), func(i watch.Involving) [2]string { return [2]string{string(i), involvingWords(i)} })
	case reflect.TypeFor[watch.State]():
		return pairsOf(watch.States(), func(s watch.State) [2]string { return [2]string{string(s), stateWords(s)} })
	}

	return nil
}

// catalogLabelPairs returns the value and the label of each value of a closed set the
// catalog lists with labels, nil for another.
func catalogLabelPairs(t reflect.Type) [][2]string {
	lists := map[reflect.Type]func() [][2]string{
		reflect.TypeFor[catalog.ListSource](): func() [][2]string {
			return pairsOf(catalog.ListSources(), func(l catalog.ListSourceInfo) [2]string { return [2]string{string(l.ID), l.Label} })
		},
		reflect.TypeFor[catalog.Event](): func() [][2]string {
			return pairsOf(catalog.Triggers(), func(x catalog.Trigger) [2]string { return [2]string{string(x.Event), x.Label} })
		},
		reflect.TypeFor[catalog.Every](): func() [][2]string {
			return pairsOf(catalog.Everies(), func(e catalog.EveryInfo) [2]string { return [2]string{string(e.ID), e.Label} })
		},
		reflect.TypeFor[catalog.ScheduleStyle](): func() [][2]string {
			return pairsOf(catalog.ScheduleStyles(), func(s catalog.ScheduleStyleInfo) [2]string {
				return [2]string{string(s.ID), s.Label}
			})
		},
		reflect.TypeFor[catalog.Outcome](): func() [][2]string {
			return pairsOf(catalog.Outcomes(), func(o catalog.OutcomeInfo) [2]string { return [2]string{string(o.ID), o.Label} })
		},
		reflect.TypeFor[catalog.Where](): func() [][2]string {
			return pairsOf(catalog.Places(), func(x catalog.PlaceInfo) [2]string { return [2]string{string(x.ID), x.Label} })
		},
		reflect.TypeFor[catalog.Preset](): func() [][2]string {
			return pairsOf(catalog.Presets(), func(x catalog.PresetInfo) [2]string { return [2]string{string(x.ID), x.Label} })
		},
		reflect.TypeFor[catalog.MergeHow](): func() [][2]string {
			return pairsOf(catalog.MergeHows(), func(m catalog.MergeHowInfo) [2]string { return [2]string{string(m.ID), m.Label} })
		},
		reflect.TypeFor[catalog.MergeMethod](): func() [][2]string {
			return pairsOf(catalog.MergeMethods(), func(m catalog.MergeMethodInfo) [2]string {
				return [2]string{string(m.ID), m.Label}
			})
		},
		reflect.TypeFor[catalog.ReviewAs](): func() [][2]string {
			return pairsOf(catalog.ReviewAses(), func(r catalog.ReviewAsInfo) [2]string { return [2]string{string(r.ID), r.Label} })
		},
		reflect.TypeFor[catalog.TellWhen](): func() [][2]string {
			return pairsOf(catalog.TellWhens(), func(x catalog.TellWhenInfo) [2]string { return [2]string{string(x.ID), x.Label} })
		},
		reflect.TypeFor[catalog.OncePer](): oncePerPairs,
	}

	if list, ok := lists[t]; ok {
		return list()
	}

	return nil
}

// oncePerPairs are a routine's own once-pers, as the limits' Once per offers them.
func oncePerPairs() [][2]string {
	for _, s := range catalog.Limits() {
		if s.ID == "oncePer" {
			return pairsOf(s.Choices, func(c catalog.Choice) [2]string { return [2]string{c.Value, c.Label} })
		}
	}

	return [][2]string{}
}

// pairsOf returns what pair says of each entry of list.
func pairsOf[E any](list []E, pair func(E) [2]string) [][2]string {
	out := make([][2]string, 0, len(list))
	for _, e := range list {
		out = append(out, pair(e))
	}

	return out
}

// tickLabeled writes values in backticks, each with its label when it has one that says
// more than the value: "`checks_failed` (Checks fail)".
func tickLabeled[S ~string](values []S, labels map[string]string) []string {
	out := make([]string, 0, len(values))

	for _, v := range values {
		out = append(out, tick(string(v))+labelWords(string(v), labels[string(v)]))
	}

	return out
}

// labelWords writes a value's label in parentheses, after the value, when it says more
// than the value itself: " (Checks fail)" for checks_failed, nothing for is's "is".
func labelWords(value, label string) string {
	if label == "" || strings.EqualFold(label, value) {
		return ""
	}

	return " (" + label + ")"
}

// enumWords offers the values of the closed set the type T holds, each with its label:
// "`comment` (as a comment) or `verdict` (as its verdict)".
func enumWords[T ~string]() string {
	t := reflect.TypeFor[T]()
	set, _ := closedSetOf(t)

	return offerWords(tickLabeled(set.Values, valueLabels(t)))
}

// HiddenKeys are the keys of a routine's spec that Describe it never reads nor writes, by
// the struct that holds each: the UUID a routine names a prompt or a script by beside its
// name, which the app keeps for itself. The snapshot and an attachment carry none, the
// schema and the guide name none, a proposal that holds one has it dropped, and Save stamps
// each again from the name.
func HiddenKeys() map[reflect.Type][]string {
	return map[reflect.Type][]string{
		reflect.TypeFor[spec.PromptRef]():  {"uuid"},
		reflect.TypeFor[spec.ScriptStep](): {"uuid"},
	}
}

// visibleKeyOf is jsonKeyOf for a field of struct t, false for one of HiddenKeys.
func visibleKeyOf(t reflect.Type, f reflect.StructField) (key string, optional, ok bool) {
	key, optional, ok = jsonKeyOf(f)
	if !ok || slices.Contains(HiddenKeys()[t], key) {
		return "", false, false
	}

	return key, optional, true
}
