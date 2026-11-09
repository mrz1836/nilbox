package agent

import (
	"maps"
	"slices"
)

// CloneModels returns each agent's model with the blank ones left out; never nil.
func CloneModels(m map[ID]string) map[ID]string {
	out := make(map[ID]string, len(m))

	for id, model := range m {
		if model != "" {
			out[id] = model
		}
	}

	return out
}

// CloneArgs returns a deep copy of each agent's extra arguments with the empty lists left
// out; never nil.
func CloneArgs(m map[ID][]string) map[ID][]string {
	out := make(map[ID][]string, len(m))

	for id, args := range m {
		if len(args) > 0 {
			out[id] = slices.Clone(args)
		}
	}

	return out
}

// EqualModels reports whether two agents' models are the same, a blank model and no
// entry being the same.
func EqualModels(a, b map[ID]string) bool {
	return maps.Equal(CloneModels(a), CloneModels(b))
}

// EqualArgs reports whether two agents' extra arguments are the same, an empty list and no
// entry being the same.
func EqualArgs(a, b map[ID][]string) bool {
	return maps.EqualFunc(CloneArgs(a), CloneArgs(b), slices.Equal[[]string])
}

// JoinArgs writes each agent's extra arguments on one line, as the editors show them; never
// nil.
func JoinArgs(m map[ID][]string) map[ID]string {
	out := make(map[ID]string, len(m))

	for id, args := range CloneArgs(m) {
		out[id] = Join(args)
	}

	return out
}
