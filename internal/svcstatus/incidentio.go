package svcstatus

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/statusprefs"
)

// ioSummary is incident.io's widget summary (/api/v1/summary), its documented public API,
// as much of it as the app reads. It lists no component that isn't affected.
type ioSummary struct {
	Ongoing      []ioIncident `json:"ongoing_incidents"`
	Maintenances []ioIncident `json:"in_progress_maintenances"`
}

// ioIncident is an ongoing incident or a maintenance in progress, with the components it
// affects.
type ioIncident struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Status   string        `json:"status"`
	URL      string        `json:"url"`
	Impact   string        `json:"current_worst_impact"`
	Affected []ioComponent `json:"affected_components"`
}

// ioComponent is a component an incident or a maintenance affects.
type ioComponent struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Group  string `json:"group_name"`
	Status string `json:"current_status"`
}

// parseIncidentIO reads an incident.io widget summary: the ongoing incidents and the
// maintenance in progress, and the components they affect, each at the worst status any of
// them gives it. An incident's own link is kept only when it is https on the provider's
// host; otherwise it links to the provider's page.
func parseIncidentIO(p statusprefs.Provider, body []byte) (reading, error) {
	var s ioSummary
	if err := json.Unmarshal(body, &s); err != nil {
		return reading{}, fmt.Errorf("decode %s's status page: %w: %w", p.Name, ErrNotASummary, err)
	}

	if s.Ongoing == nil {
		return reading{}, fmt.Errorf("decode %s's status page: %w: it lists no incidents", p.Name, ErrNotASummary)
	}

	out := reading{components: []component{}, incidents: []incident{}}

	for _, i := range s.Ongoing {
		out.addIncident(p, i, stateOf(i.Impact), false)
	}

	for _, m := range s.Maintenances {
		out.addIncident(p, m, StateMaintenance, true)
	}

	return out, nil
}

// addIncident adds i, an incident at state or a maintenance, and the components it
// affects, each at the status it gives, or at state when it gives none.
func (r *reading) addIncident(p statusprefs.Provider, i ioIncident, state State, maintenance bool) {
	refs := make([]ref, 0, len(i.Affected))

	for _, c := range i.Affected {
		refs = append(refs, ref{ID: c.ID, Name: c.Name, Group: c.Group})

		cs := state
		if c.Status != "" {
			cs = stateOf(c.Status)
		}

		r.addComponent(component{ID: c.ID, Name: c.Name, Group: c.Group, State: cs})
	}

	stage := stageOf(i.Status)
	if maintenance && stage == StageOther {
		stage = StageInProgress
	}

	r.incidents = append(r.incidents, incident{
		ID: i.ID, Name: i.Name, Stage: stage, State: state, URL: pageLink(p, i.URL), Refs: refs,
		Maintenance: maintenance,
	})
}

// addComponent adds c, or, when a component with its id is already listed, gives that one
// the worse of the two states.
func (r *reading) addComponent(c component) {
	at := slices.IndexFunc(r.components, func(have component) bool { return have.ID == c.ID })
	if at < 0 {
		r.components = append(r.components, c)

		return
	}

	r.components[at].State = worst(r.components[at].State, c.State)
}
