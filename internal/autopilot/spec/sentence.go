package spec

import (
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// PromptNames names the prompts a spec refers to, for its sentence: the library's
// nickname for a starter, say. Nil, or "" from it, writes the reference's own words: a
// nickname as it is, a starter's key as words.
type PromptNames func(PromptRef) string

// Sentence writes a spec as the one sentence the builder, the routine list, the menu bar,
// and an export all show: when something happens to which items, if some conditions
// hold, what it does, and, in a second sentence, its limits. "When checks fail on pull
// requests by Dependabot in your account and acme-org, once every check has settled, if
// a failed check's name contains codeql or security, run "Fix dependency security" in
// the background; if it pushed, wait for the checks, then tell me how it went. Once per
// commit, at most 2 tries per pull request."
func Sentence(s Spec, names PromptNames) string {
	items := newItems(s)

	var b strings.Builder

	b.WriteString(textutil.Capitalize(triggerPhrase(s.Trigger, items)))

	if cond := conditionsPhrase(s.Conditions); cond != "" {
		b.WriteString(", if " + cond)
	}

	if steps := stepsPhrase(s.Steps, names); steps != "" {
		b.WriteString(", " + steps)
	}

	b.WriteString(".")

	if limits := limitsPhrase(s, items); limits != "" {
		b.WriteString(" " + textutil.Capitalize(limits) + ".")
	}

	return b.String()
}

// items is how the sentence names the routine's items.
type items struct {
	// one is the noun for one item, "pull request", and many for several.
	one, many string

	// where is what the watch says about them: "by Dependabot in acme-org".
	where string
}

// newItems names the items the routine acts on: those of the kinds both its watch and
// its trigger take.
func newItems(s Spec) items {
	kinds := s.Watch.Kinds()

	if t, ok := catalog.TriggerByEvent(s.Trigger.Event); ok {
		kinds = intersect(kinds, t.Kinds)
	}

	it := items{one: "item", many: "items", where: watchPhrase(s.Watch)}

	if len(kinds) == 1 {
		it.one, it.many = kinds[0].Noun(), kinds[0].Plural()
	}

	return it
}

// a names one item, with the watch's words: "a pull request by Dependabot".
func (it items) a() string {
	article := "a "
	if strings.HasPrefix(it.one, "i") {
		article = "an "
	}

	return strings.TrimSpace(article + it.one + " " + it.where)
}

// all names the items, with the watch's words: "pull requests by Dependabot".
func (it items) all() string {
	return strings.TrimSpace(it.many + " " + it.where)
}

// intersect returns the kinds in both a and b, in a's order.
func intersect(a, b []item.Kind) []item.Kind {
	out := make([]item.Kind, 0, len(a))

	for _, k := range a {
		for _, o := range b {
			if k == o {
				out = append(out, k)
			}
		}
	}

	return out
}

// triggerPhrase says when the routine fires.
func triggerPhrase(t Trigger, it items) string {
	phrase, ok := triggerPhrases()[t.Event]
	if !ok {
		return "when " + it.a() + " " + string(t.Event)
	}

	return phrase(t, it)
}

// triggerPhrases are how each trigger is said.
func triggerPhrases() map[catalog.Event]func(Trigger, items) string {
	return map[catalog.Event]func(Trigger, items) string{
		catalog.EventAppears:          func(_ Trigger, it items) string { return "when " + it.a() + " appears" },
		catalog.EventNewCommits:       newCommitsPhrase,
		catalog.EventChecksFailed:     checksFailedPhrase,
		catalog.EventChecksPassed:     func(_ Trigger, it items) string { return "when checks pass on " + it.all() },
		catalog.EventChangesRequested: func(_ Trigger, it items) string { return "when changes are requested on " + it.all() },
		catalog.EventApproved:         func(_ Trigger, it items) string { return "when " + it.a() + " is approved" },
		catalog.EventMergeConflicts:   func(_ Trigger, it items) string { return "when " + it.a() + " has merge conflicts" },
		catalog.EventFallsBehind: func(_ Trigger, it items) string {
			return "when " + it.a() + " falls behind its base branch"
		},
		catalog.EventLabeled: func(t Trigger, it items) string {
			return "when " + it.a() + " is labeled " + quote(t.Label)
		},
		catalog.EventReadyForReview: func(_ Trigger, it items) string {
			return "when " + it.a() + " is ready for review"
		},
		catalog.EventReviewRequested: func(_ Trigger, it items) string { return "when " + it.a() + " asks for your review" },
		catalog.EventAssigned:        func(_ Trigger, it items) string { return "when " + it.a() + " is assigned to you" },
		catalog.EventNewComments:     newCommentsPhrase,
		catalog.EventGoesQuiet: func(t Trigger, it items) string {
			return "when " + it.a() + " goes quiet for " + textutil.Counted(t.QuietDays(), "day", "days")
		},
		catalog.EventSchedule: schedulePhrase,
		catalog.EventManual:   func(_ Trigger, it items) string { return "when you run it on " + it.a() },
	}
}

func newCommitsPhrase(t Trigger, it items) string {
	phrase := "when " + it.a() + " gets new commits"

	if t.FirstCommitToo() {
		phrase += ", the first one included"
	} else {
		phrase += ", after the first one"
	}

	if t.OwnPushes() {
		phrase += ", a routine's own included"
	}

	return phrase
}

func checksFailedPhrase(t Trigger, it items) string {
	phrase := "when checks fail on " + it.all()

	switch {
	case !t.WaitsForChecks():
	case t.Settle() > 0:
		phrase += ", once every check has settled"
	default:
		phrase += ", once every check has finished"
	}

	return phrase
}

func newCommentsPhrase(t Trigger, it items) string {
	if n := t.CommentCount(); n > 1 {
		return "when " + it.a() + " gets " + strconv.Itoa(n) + " new comments"
	}

	return "when " + it.a() + " gets a new comment"
}

func schedulePhrase(t Trigger, it items) string {
	if t.Schedule == nil {
		return "on a schedule, for " + it.all()
	}

	every := map[catalog.Every]string{
		catalog.EveryDay: "every day", catalog.EveryWeekday: "every weekday",
		catalog.EveryWeek: "every " + weekdayName(t.Schedule.On),
	}[t.Schedule.Every]

	return every + " at " + t.Schedule.At + ", for " + it.all()
}

// weekdayName names a day of the week.
func weekdayName(d catalog.Weekday) string {
	return map[catalog.Weekday]string{
		catalog.Monday: "Monday", catalog.Tuesday: "Tuesday", catalog.Wednesday: "Wednesday",
		catalog.Thursday: "Thursday", catalog.Friday: "Friday", catalog.Saturday: "Saturday", catalog.Sunday: "Sunday",
	}[d]
}

// conditionsPhrase says the conditions, "" for none.
func conditionsPhrase(c Conditions) string {
	phrases := make([]string, 0, len(c.Of))

	for _, cl := range c.Of {
		if cl.Group != nil {
			if g := conditionsPhrase(*cl.Group); g != "" {
				phrases = append(phrases, "("+g+")")
			}

			continue
		}

		phrases = append(phrases, Phrase(cl))
	}

	if c.Match == MatchAny {
		return series(phrases, "or")
	}

	return series(phrases, "and")
}

// series joins phrases as a sentence lists them, with conj before the last.
func series(phrases []string, conj string) string {
	if len(phrases) < 3 {
		return strings.Join(phrases, " "+conj+" ")
	}

	return strings.Join(phrases[:len(phrases)-1], ", ") + ", " + conj + " " + phrases[len(phrases)-1]
}

// Phrase says one condition as the sentence and the reasons do: "a failed check's name
// contains codeql or security", "the lines changed are under 400".
func Phrase(c Clause) string {
	f, ok := catalog.FieldByID(c.Field)
	if !ok {
		return string(c.Field) + " " + string(c.Op) + " " + c.Value
	}

	switch f.Type {
	case catalog.TypeText:
		return textPhrase(f, c)
	case catalog.TypeList:
		return listPhrase(f, c)
	case catalog.TypeNumber:
		return numberPhrase(f, c)
	case catalog.TypeBool:
		return map[bool]string{true: f.Words.No, false: f.Words.Yes}[c.Op == catalog.OpIsNo]
	case catalog.TypeChoice:
		return choicePhrase(f, c)
	case catalog.TypeDuration:
		return durationPhrase(f, c)
	}

	return f.Words.Noun
}

func choicePhrase(f catalog.Field, c Clause) string {
	subject := f.Words.Noun + " is "

	switch {
	case c.Op == catalog.OpNoneOf:
		subject = f.Words.Noun + " isn't "
	case f.Words.Noun == "it":
		subject = "it's "
	}

	return subject + series(choiceLabels(f, c.Values), "or")
}

func textPhrase(f catalog.Field, c Clause) string {
	verb := map[catalog.Op]string{
		catalog.OpIs: "is", catalog.OpIsNot: "isn't", catalog.OpContains: "contains",
		catalog.OpNotContains: "doesn't contain", catalog.OpStartsWith: "starts with",
		catalog.OpMatches: "matches", catalog.OpMatchesRegex: "matches the regex",
	}[c.Op]

	return f.Words.Noun + " " + verb + " " + quote(c.Value)
}

func listPhrase(f catalog.Field, c Clause) string {
	values := quoted(c.Values)
	if len(f.Choices) > 0 {
		values = choiceLabels(f, c.Values)
	}

	every := strings.TrimPrefix(strings.TrimPrefix(f.Words.One, "a "), "an ")

	phrases := map[catalog.Op]func() string{
		catalog.OpHasAny:       func() string { return f.Words.Noun + " include " + series(values, "or") },
		catalog.OpHasAll:       func() string { return f.Words.Noun + " include " + series(values, "and") },
		catalog.OpHasNone:      func() string { return f.Words.Noun + " don't include " + series(values, "or") },
		catalog.OpAnyContains:  func() string { return f.Words.One + " contains " + series(values, "or") },
		catalog.OpNoneContains: func() string { return "no " + every + " contains " + series(values, "or") },
		catalog.OpAnyMatches:   func() string { return f.Words.One + " matches " + series(values, "or") },
		catalog.OpAllMatch:     func() string { return "every " + every + " matches " + series(values, "or") },
		catalog.OpIsEmpty:      func() string { return f.Words.Yes },
		catalog.OpNotEmpty:     func() string { return f.Words.No },
	}

	if phrase, ok := phrases[c.Op]; ok {
		return phrase()
	}

	return f.Words.Noun
}

func numberPhrase(f catalog.Field, c Clause) string {
	be, beNot := "is", "isn't"
	if f.Words.Plural {
		be, beNot = "are", "aren't"
	}

	verb := map[catalog.Op]string{
		catalog.OpEq: be, catalog.OpNe: beNot, catalog.OpLt: be + " under", catalog.OpLte: be + " at most",
		catalog.OpGt: be + " over", catalog.OpGte: be + " at least",
	}[c.Op]

	return f.Words.Noun + " " + verb + " " + c.Value
}

func durationPhrase(f catalog.Field, c Clause) string {
	than := " for more than "
	if c.Op == catalog.OpLessThan {
		than = " for less than "
	}

	return f.Words.Noun + than + lengthWords(c.Value)
}

// lengthWords says a length such as "14d" in words: "14 days".
func lengthWords(s string) string {
	if len(s) < 2 {
		return s
	}

	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil {
		return s
	}

	switch s[len(s)-1] {
	case 'm':
		return textutil.Counted(n, "minute", "minutes")
	case 'h':
		return textutil.Counted(n, "hour", "hours")
	case 'd':
		return textutil.Counted(n, "day", "days")
	default:
		return s
	}
}

// choiceLabels writes each value as its choice's label, when the field has choices.
func choiceLabels(f catalog.Field, values []string) []string {
	out := make([]string, len(values))

	for i, v := range values {
		out[i] = v

		if c, ok := f.Choice(v); ok {
			out[i] = c.Label
		}
	}

	return out
}

// quote writes a value as the sentence shows it: in quotes when it holds a space, so
// "help wanted" reads as one value.
func quote(s string) string {
	if strings.ContainsAny(s, " \t") {
		return `"` + s + `"`
	}

	return s
}

// quoted writes each value as quote does.
func quoted(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = quote(v)
	}

	return out
}
