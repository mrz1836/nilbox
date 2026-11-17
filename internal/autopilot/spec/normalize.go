package spec

import (
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
)

// Normalize returns s cleaned as it is stored: every text trimmed, every list that is a
// set sorted without repeats (text compared ignoring case, the first spelling kept), a
// choice written as the catalog spells it, a condition given twice in one list kept
// once, the conditions' match "all" when it has no conditions, and an agent step's
// agent "inherit" when it names none. The order of the sources, the conditions, the
// steps, a GitHub step's actions, and the fallbacks is the user's and is kept. Normalize
// never changes a value Validate refuses into one it takes, and normalizing twice
// changes nothing.
func Normalize(s Spec) Spec {
	out := Spec{
		Version:    s.Version,
		Watch:      normalizeWatch(s.Watch),
		Trigger:    normalizeTrigger(s.Trigger),
		Conditions: normalizeConditions(s.Conditions),
		Limits:     normalizeLimits(s.Limits),
		Notify:     normalizeNotices(s.Notify),
		Requires:   textSet(s.Requires),
	}

	out.Steps = make([]Step, 0, len(s.Steps))
	for _, st := range s.Steps {
		out.Steps = append(out.Steps, normalizeStep(st))
	}

	return out
}

// normalizeWatch cleans each source.
func normalizeWatch(w Watch) Watch {
	out := Watch{Sources: make([]Source, 0, len(w.Sources))}

	for _, src := range w.Sources {
		n := Source{Lists: kindSet(src.Lists)}

		if src.Search != nil {
			search := watch.Normalize(*src.Search)
			n.Search = &search
		}

		out.Sources = append(out.Sources, n)
	}

	return out
}

// normalizeTrigger trims the trigger's text.
func normalizeTrigger(t Trigger) Trigger {
	t.Event = catalog.Event(strings.TrimSpace(string(t.Event)))
	t.Label = strings.TrimSpace(t.Label)

	if t.Schedule != nil {
		sched := *t.Schedule
		sched.At = strings.TrimSpace(sched.At)

		// Each match is the default, written as none, so a schedule has one form.
		if sched.Style = catalog.ScheduleStyle(strings.TrimSpace(string(sched.Style))); sched.Style == catalog.ScheduleEach {
			sched.Style = ""
		}

		t.Schedule = &sched
	}

	return t
}

// normalizeConditions cleans a list of conditions and its groups.
func normalizeConditions(c Conditions) Conditions {
	out := Conditions{Match: c.Match, Of: make([]Clause, 0, len(c.Of))}

	if out.Match == "" && len(c.Of) == 0 {
		out.Match = MatchAll
	}

	for _, cl := range c.Of {
		n := normalizeClause(cl)
		if !slices.ContainsFunc(out.Of, func(o Clause) bool { return sameClause(o, n) }) {
			out.Of = append(out.Of, n)
		}
	}

	return out
}

// normalizeClause cleans one condition or group.
func normalizeClause(c Clause) Clause {
	out := Clause{
		Field: catalog.FieldID(strings.TrimSpace(string(c.Field))),
		Op:    catalog.Op(strings.TrimSpace(string(c.Op))),
		Value: strings.TrimSpace(c.Value),
	}

	if c.Group != nil {
		g := normalizeConditions(*c.Group)
		out.Group = &g
	}

	field, ok := catalog.FieldByID(out.Field)

	values := textSet(c.Values)
	if ok && len(field.Choices) > 0 {
		values = choiceSet(field, values)
	}

	out.Values = values

	return out
}

// choiceSet writes each value as the field's choice spells it, sorted in the catalog's
// order; a value that is no choice is kept, for Validate to refuse.
func choiceSet(field catalog.Field, values []string) []string {
	for i, v := range values {
		if c, ok := field.Choice(v); ok {
			values[i] = c.Value
		}
	}

	rank := func(v string) int {
		return slices.IndexFunc(field.Choices, func(c catalog.Choice) bool { return c.Value == v })
	}

	slices.SortStableFunc(values, func(a, b string) int { return rank(a) - rank(b) })

	return slices.Compact(values)
}

// sameClause reports two conditions that say the same. Their values compare exactly: a
// regex's case is part of what it says (\d and \D are opposites), so only a condition
// written the same way twice is a repeat.
func sameClause(a, b Clause) bool {
	if a.Group != nil || b.Group != nil {
		return false
	}

	return a.Field == b.Field && a.Op == b.Op && a.Value == b.Value && slices.Equal(a.Values, b.Values)
}

// normalizeStep cleans one step.
func normalizeStep(s Step) Step {
	out := s
	out.OnlyIf = catalog.Outcome(strings.TrimSpace(string(s.OnlyIf)))

	if s.Notify != nil {
		n := *s.Notify
		n.Note = strings.TrimSpace(n.Note)
		out.Notify = &n
	}

	// A GitHub step with no action is written as none, as encoding leaves it out.
	out.GitHub = nil

	for _, a := range s.GitHub {
		out.GitHub = append(out.GitHub, normalizeAction(a))
	}

	if s.Agent != nil {
		a := normalizeAgent(*s.Agent)
		out.Agent = &a
	}

	if s.Script != nil {
		out.Script = &ScriptStep{ID: s.Script.ID, Name: strings.TrimSpace(s.Script.Name)}
	}

	return out
}

// normalizeAction cleans one GitHub action.
func normalizeAction(a GitHubAction) GitHubAction {
	out := a

	if a.Reply != nil {
		out.Reply = &ReplyAction{Prompt: normalizePrompt(a.Reply.Prompt)}
	}

	if a.Labels != nil {
		out.Labels = &LabelsAction{Add: textSet(a.Labels.Add), Remove: textSet(a.Labels.Remove)}
	}

	return out
}

// normalizeAgent cleans an agent step.
func normalizeAgent(a AgentStep) AgentStep {
	out := a
	out.Agent = strings.TrimSpace(a.Agent)

	if out.Agent == "" {
		out.Agent = catalog.Inherit
	}

	out.Prompt = normalizePrompt(a.Prompt)
	out.Profile = strings.TrimSpace(a.Profile)
	out.Model = strings.TrimSpace(a.Model)
	out.Effort = strings.TrimSpace(a.Effort)
	out.Fallbacks = nil

	for _, f := range a.Fallbacks {
		out.Fallbacks = append(out.Fallbacks, ProfileRef{
			Agent: strings.TrimSpace(f.Agent), Profile: strings.TrimSpace(f.Profile),
		})
	}

	out.LaunchFor = normalizeLaunchRows(a.LaunchFor)

	if a.Fence != nil {
		f := *a.Fence
		f.Allow = textSet(f.Allow)
		f.Deny = textSet(f.Deny)
		out.Fence = &f
	}

	return out
}

// normalizeLaunchRows trims each row and sorts them by target, ignoring case: a
// repository's row wins over its org's whatever their order.
func normalizeLaunchRows(rows []LaunchRow) []LaunchRow {
	if len(rows) == 0 {
		return nil
	}

	out := make([]LaunchRow, 0, len(rows))

	for _, r := range rows {
		out = append(out, LaunchRow{
			Target: strings.ToLower(strings.TrimSpace(r.Target)), Agent: strings.TrimSpace(r.Agent),
			Profile: strings.TrimSpace(r.Profile), Model: strings.TrimSpace(r.Model), Effort: strings.TrimSpace(r.Effort),
		})
	}

	slices.SortStableFunc(out, func(a, b LaunchRow) int { return strings.Compare(a.Target, b.Target) })

	return out
}

// normalizePrompt trims a prompt's name.
func normalizePrompt(p PromptRef) PromptRef {
	return PromptRef{
		Starter: strings.ToLower(strings.TrimSpace(p.Starter)), Nickname: strings.TrimSpace(p.Nickname),
		ItemDefault: p.ItemDefault,
	}
}

// normalizeLimits cleans the limits.
func normalizeLimits(l Limits) Limits {
	if l.ActiveHours != nil {
		h := *l.ActiveHours
		h.Days = weekdaySet(h.Days)
		h.From = strings.TrimSpace(h.From)
		h.To = strings.TrimSpace(h.To)
		l.ActiveHours = &h
	}

	return l
}

// normalizeNotices drops notices that set nothing.
func normalizeNotices(n *Notices) *Notices {
	if n == nil || *n == (Notices{}) {
		return nil
	}

	c := *n

	return &c
}

// textSet returns values trimmed, without empty ones and repeats ignoring case (the
// first spelling kept), sorted ignoring case; nil for none.
func textSet(values []string) []string {
	out := make([]string, 0, len(values))

	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" && !slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, v) }) {
			out = append(out, v)
		}
	}

	if len(out) == 0 {
		return nil
	}

	slices.SortStableFunc(out, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})

	return out
}

// kindSet returns inbox lists without repeats, in the catalog's order; nil for none.
func kindSet(lists []catalog.ListSource) []catalog.ListSource {
	out := make([]catalog.ListSource, 0, len(lists))

	for _, l := range lists {
		l = catalog.ListSource(strings.TrimSpace(string(l)))
		if l != "" && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}

	if len(out) == 0 {
		return nil
	}

	rank := func(l catalog.ListSource) int {
		i := slices.IndexFunc(catalog.ListSources(), func(s catalog.ListSourceInfo) bool { return s.ID == l })
		if i < 0 {
			return len(catalog.ListSources())
		}

		return i
	}

	slices.SortStableFunc(out, func(a, b catalog.ListSource) int { return rank(a) - rank(b) })

	return out
}

// weekdaySet returns days without repeats, from Monday; nil for none.
func weekdaySet(days []catalog.Weekday) []catalog.Weekday {
	out := make([]catalog.Weekday, 0, len(days))

	for _, d := range catalog.Weekdays() {
		if slices.Contains(days, d) {
			out = append(out, d)
		}
	}

	for _, d := range days {
		if !slices.Contains(catalog.Weekdays(), d) && !slices.Contains(out, d) {
			out = append(out, d)
		}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}
