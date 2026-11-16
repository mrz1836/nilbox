package catalog

import "sync"

// index is the catalog's entries by their IDs, built once, so the evaluator's lookups,
// made for every condition of every routine on every item, cost a map read. The entries
// it hands out share their lists with every caller's: read them, never change them.
type index struct {
	fields   map[FieldID]Field
	ops      map[Op]OpInfo
	triggers map[Event]Trigger
	steps    map[StepKind]Step
	outcomes map[Outcome]OutcomeInfo
	presets  map[Preset]PresetInfo
	lists    map[ListSource]ListSourceInfo
}

// byID is the one index, built the first time a lookup needs it.
//
//nolint:gochecknoglobals // a fixed table, built once from the catalog's own lists
var byID = sync.OnceValue(func() index {
	return index{
		fields:   keyed(Fields(), func(f Field) FieldID { return f.ID }),
		ops:      keyed(Ops(), func(o OpInfo) Op { return o.ID }),
		triggers: keyed(Triggers(), func(t Trigger) Event { return t.Event }),
		steps:    keyed(Steps(), func(s Step) StepKind { return s.Kind }),
		outcomes: keyed(Outcomes(), func(o OutcomeInfo) Outcome { return o.ID }),
		presets:  keyed(Presets(), func(p PresetInfo) Preset { return p.ID }),
		lists:    keyed(ListSources(), func(l ListSourceInfo) ListSource { return l.ID }),
	}
})

// keyed returns list's entries by the key each has.
func keyed[K comparable, V any](list []V, key func(V) K) map[K]V {
	out := make(map[K]V, len(list))
	for _, v := range list {
		out[key(v)] = v
	}

	return out
}

// FieldByID returns the field id names.
func FieldByID(id FieldID) (Field, bool) {
	f, ok := byID().fields[id]

	return f, ok
}

// OpByID returns the operator id names.
func OpByID(id Op) (OpInfo, bool) {
	o, ok := byID().ops[id]

	return o, ok
}

// TriggerByEvent returns the trigger e names.
func TriggerByEvent(e Event) (Trigger, bool) {
	t, ok := byID().triggers[e]

	return t, ok
}

// StepByKind returns the step kind names.
func StepByKind(kind StepKind) (Step, bool) {
	s, ok := byID().steps[kind]

	return s, ok
}

// OutcomeByID returns the outcome id names.
func OutcomeByID(id Outcome) (OutcomeInfo, bool) {
	o, ok := byID().outcomes[id]

	return o, ok
}

// PresetByID returns the preset id names.
func PresetByID(id Preset) (PresetInfo, bool) {
	p, ok := byID().presets[id]

	return p, ok
}

// ListSourceByID returns the inbox list id names.
func ListSourceByID(id ListSource) (ListSourceInfo, bool) {
	l, ok := byID().lists[id]

	return l, ok
}
