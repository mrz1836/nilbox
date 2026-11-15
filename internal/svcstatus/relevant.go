package svcstatus

import (
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/statusprefs"
)

// judge works out where a provider stands for what the user watches on it (picked, its
// parts the user ticked), from what its page said. It decides by component, never by the
// page's overall state, which also turns for parts the user doesn't watch:
//
//   - A component on the page matches a picked part by the page's id, or, when the id
//     doesn't match (a provider that issued new ids), by its name and, where the part
//     names one, its group, which tells apart two components of one name in two groups.
//   - Affected are the picked parts whose components aren't operational, each at the
//     worst state of its components.
//   - An incident or a maintenance that names a picked part counts, and so does one that
//     names no component at all when it is major or critical (Unscoped); every other
//     incident is only counted in Others.
//   - The provider's State is the worst over Affected and the incidents that count:
//     operational < maintenance < minor < major < critical.
//   - Unlisted are the picked parts a Statuspage page no longer lists. An incident.io
//     page lists only the components affected, so nothing there is unlisted.
//
// The Provider it returns carries no CheckedAt or Error, which are the reading's.
func judge(p statusprefs.Provider, read reading, picked []statusprefs.PartID) Provider {
	out := Provider{ID: p.ID, Name: p.Name, URL: p.PageURL, State: StateOperational}
	parts := pickedParts(p, picked)

	out.Affected, out.Unlisted = affectedParts(p, read, parts)
	out.Incidents, out.Others = relevantIncidents(read, parts)

	for _, part := range out.Affected {
		out.State = worst(out.State, part.State)
	}

	for _, i := range out.Incidents {
		out.State = worst(out.State, i.State)
	}

	return out
}

// affectedParts returns the parts among parts whose components on the page aren't
// operational, and, for a Statuspage page, those it doesn't list.
func affectedParts(p statusprefs.Provider, read reading, parts []statusprefs.Part) ([]Part, []statusprefs.PartID) {
	affected, unlisted := []Part{}, []statusprefs.PartID{}

	for _, part := range parts {
		state, listed := partState(part, read.components)

		if !listed && p.Format == statusprefs.FormatStatuspage {
			unlisted = append(unlisted, part.ID)
		}

		if state != StateOperational {
			affected = append(affected, Part{ID: part.ID, Name: part.Name, State: state})
		}
	}

	return affected, unlisted
}

// partState returns the worst state of part's components among components, and whether
// any is listed.
func partState(part statusprefs.Part, components []component) (State, bool) {
	state, listed := StateOperational, false

	for _, c := range components {
		if matches(part, c.ID, c.Name, c.Group) {
			listed = true
			state = worst(state, c.State)
		}
	}

	return state, listed
}

// relevantIncidents returns the page's incidents that touch parts, in the page's order,
// and counts the others.
func relevantIncidents(read reading, parts []statusprefs.Part) ([]Incident, int) {
	out, others := []Incident{}, 0

	for _, i := range read.incidents {
		named := namedParts(i, parts)
		unscoped := len(i.Refs) == 0

		if len(named) == 0 && (!unscoped || i.State.severity() < StateMajor.severity()) {
			others++

			continue
		}

		out = append(out, Incident{
			Name: i.Name, Stage: i.Stage, State: i.State, URL: i.URL, Parts: named, Unscoped: unscoped,
			Maintenance: i.Maintenance,
		})
	}

	return out, others
}

// namedParts returns the names of the parts among parts that i names, in catalog order.
func namedParts(i incident, parts []statusprefs.Part) []string {
	named := []string{}

	for _, part := range parts {
		if slices.ContainsFunc(i.Refs, func(r ref) bool { return matches(part, r.ID, r.Name, r.Group) }) {
			named = append(named, part.Name)
		}
	}

	return named
}

// pickedParts returns the provider's parts among picked, in catalog order.
func pickedParts(p statusprefs.Provider, picked []statusprefs.PartID) []statusprefs.Part {
	out := []statusprefs.Part{}

	for _, part := range p.Parts {
		if slices.Contains(picked, part.ID) {
			out = append(out, part)
		}
	}

	return out
}

// matches reports whether a component the page names (by id, name, and group) is part.
func matches(part statusprefs.Part, id, name, group string) bool {
	if id != "" && slices.Contains(part.PageIDs, id) {
		return true
	}

	return strings.EqualFold(strings.TrimSpace(name), part.MatchName()) &&
		(part.Group == "" || strings.EqualFold(strings.TrimSpace(group), part.Group))
}
