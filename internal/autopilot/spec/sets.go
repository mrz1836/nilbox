package spec

import (
	"reflect"
	"slices"
	"strconv"
	"sync"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/item"
)

// ClosedSet is one closed set of values the format holds: every value a field of its type
// can take, as the spec spells it.
type ClosedSet struct {
	// Name is where the set is used: "merge.method", "schedule.on, activeHours.days", or,
	// for the keys of a step and of a GitHub step's action, "step kinds" and "github
	// actions".
	Name string

	// Type is the Go named type that holds it: catalog.MergeMethod, Match, item.Kind.
	Type reflect.Type

	// Values are every value, as the spec spells it, in the catalog's order. "" is never
	// one of them: Blank says whether a field may be left out.
	Values []string

	// Blank reports a set whose field may be left out, "", for its default: the trigger's
	// own once-per, the repository's own merge method, both kinds of item.
	Blank bool
}

// ClosedSets returns every closed set of values the format holds, in the order a spec
// reads them: the watch's, the trigger's, the conditions', the steps', and the limits'.
// Their values come from the catalog's lists, watch's, and item's, never a copy of them,
// so a value one of them adds is in its set, which Validate checks every value against.
func ClosedSets() []ClosedSet {
	var (
		lists    = ids(catalog.ListSources(), func(l catalog.ListSourceInfo) catalog.ListSource { return l.ID })
		events   = ids(catalog.Triggers(), func(t catalog.Trigger) catalog.Event { return t.Event })
		everies  = ids(catalog.Everies(), func(e catalog.EveryInfo) catalog.Every { return e.ID })
		styles   = ids(catalog.ScheduleStyles(), func(s catalog.ScheduleStyleInfo) catalog.ScheduleStyle { return s.ID })
		fields   = ids(catalog.Fields(), func(f catalog.Field) catalog.FieldID { return f.ID })
		ops      = ids(catalog.Ops(), func(o catalog.OpInfo) catalog.Op { return o.ID })
		steps    = ids(catalog.Steps(), func(s catalog.Step) catalog.StepKind { return s.Kind })
		outcomes = ids(catalog.Outcomes(), func(o catalog.OutcomeInfo) catalog.Outcome { return o.ID })
		actions  = ids(catalog.GitHubActions(), func(a catalog.GitHubAction) catalog.GitHubActionKind { return a.Kind })
		places   = ids(catalog.Places(), func(p catalog.PlaceInfo) catalog.Where { return p.ID })
		presets  = ids(catalog.Presets(), func(p catalog.PresetInfo) catalog.Preset { return p.ID })
		hows     = ids(catalog.MergeHows(), func(m catalog.MergeHowInfo) catalog.MergeHow { return m.ID })
		ases     = ids(catalog.ReviewAses(), func(r catalog.ReviewAsInfo) catalog.ReviewAs { return r.ID })
		whens    = ids(catalog.TellWhens(), func(w catalog.TellWhenInfo) catalog.TellWhen { return w.ID })
	)

	return []ClosedSet{
		closedSet("source.lists", false, lists),
		closedSet("search.kind", true, item.Kinds()),
		closedSet("search.involving", false, watch.Involvings()),
		closedSet("search.state", false, watch.States()),

		closedSet("trigger.event", false, events),
		closedSet("schedule.every", false, everies),
		closedSet("schedule.on, activeHours.days", false, catalog.Weekdays()),
		closedSet("schedule.style", true, styles),

		closedSet("conditions.match", false, []Match{MatchAll, MatchAny}),
		closedSet("condition.field", false, fields),
		closedSet("condition.op", false, ops),

		closedSet("step kinds", false, steps),
		closedSet("step.onlyIf", true, outcomes),
		closedSet("github actions", false, actions),
		closedSet("agent.where", true, places),
		closedSet("fence.tools", true, presets),
		closedSet("merge.how", false, hows),
		closedSet("merge.method", true, mergeMethods()),
		closedSet("postReview.as", false, ases),
		closedSet("tellMe.when", true, whens),

		closedSet("limits.oncePer", true, oncePers()),
	}
}

// closedSet returns the set named name, which T holds with values, blank when its field
// may be left out.
func closedSet[T ~string](name string, blank bool, values []T) ClosedSet {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, string(v))
	}

	return ClosedSet{Name: name, Type: reflect.TypeFor[T](), Values: out, Blank: blank}
}

// ids returns the ID id reads from every entry of list.
func ids[E any, T ~string](list []E, id func(E) T) []T {
	out := make([]T, 0, len(list))
	for _, e := range list {
		out = append(out, id(e))
	}

	return out
}

// mergeMethods are the merge methods a spec spells. The repository's own, which the
// catalog lists first, is the method left out: the set's blank, not one of its values.
func mergeMethods() []catalog.MergeMethod {
	methods := ids(catalog.MergeMethods(), func(m catalog.MergeMethodInfo) catalog.MergeMethod { return m.ID })

	return slices.DeleteFunc(methods, func(m catalog.MergeMethod) bool { return m == catalog.MergeDefault })
}

// oncePers are a routine's own once-pers, as the limits' Once per offers them.
func oncePers() []catalog.OncePer {
	for _, s := range catalog.Limits() {
		if s.ID == "oncePer" {
			return ids(s.Choices, func(c catalog.Choice) catalog.OncePer { return catalog.OncePer(c.Value) })
		}
	}

	return nil
}

// setsByType are the closed sets by the type that holds each, built once, for Validate's
// checks of a value against its set.
//
//nolint:gochecknoglobals // a fixed table, built once from the closed sets
var setsByType = sync.OnceValue(func() map[reflect.Type]ClosedSet {
	sets := ClosedSets()
	out := make(map[reflect.Type]ClosedSet, len(sets))

	for _, s := range sets {
		out[s.Type] = s
	}

	return out
})

// member reports whether v is a value of the closed set its type holds, or "" where that
// set's field may be left out.
func member[T ~string](v T) bool {
	set := setsByType()[reflect.TypeFor[T]()]

	return (v == "" && set.Blank) || slices.Contains(set.Values, string(v))
}

// valuesOf returns the values of the closed set T holds, shared with every caller's: read
// them, never change them.
func valuesOf[T ~string]() []string {
	return setsByType()[reflect.TypeFor[T]()].Values
}

// choices writes the values of the closed set T holds as a problem offers them, each in
// quotes, with or before the last: "day", "weekday", or "week".
func choices[T ~string]() string {
	values := valuesOf[T]()
	quoted := make([]string, 0, len(values))

	for _, v := range values {
		quoted = append(quoted, strconv.Quote(v))
	}

	return series(quoted, "or")
}
