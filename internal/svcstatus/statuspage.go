package svcstatus

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/mrz1836/nilbox/internal/statusprefs"
)

// spSummary is an Atlassian Statuspage summary.json, as much of it as the app reads.
type spSummary struct {
	Status struct {
		Indicator string `json:"indicator"`
	} `json:"status"`
	Components  []spComponent `json:"components"`
	Incidents   []spIncident  `json:"incidents"`
	Maintenance []spIncident  `json:"scheduled_maintenances"`
}

// spComponent is a component in the summary, or a group of them.
type spComponent struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Group   bool   `json:"group"`
	GroupID string `json:"group_id"`
}

// spIncident is an incident or a scheduled maintenance in the summary, with the
// components it names.
type spIncident struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Status     string        `json:"status"`
	Impact     string        `json:"impact"`
	Components []spComponent `json:"components"`
}

// parseStatuspage reads a Statuspage summary: every component but the groups, which only
// repeat their members' states, each with its group's name; the incidents not yet
// resolved; and the maintenance under way, which counts only while it is in progress or
// being verified. Each incident links to its page on the provider's host.
func parseStatuspage(p statusprefs.Provider, body []byte) (reading, error) {
	var s spSummary
	if err := json.Unmarshal(body, &s); err != nil {
		return reading{}, fmt.Errorf("decode %s's status page: %w: %w", p.Name, ErrNotASummary, err)
	}

	if s.Status.Indicator == "" {
		return reading{}, fmt.Errorf("decode %s's status page: %w: it names no state", p.Name, ErrNotASummary)
	}

	groups := groupNames(s.Components)
	out := reading{components: []component{}, incidents: []incident{}}

	for _, c := range s.Components {
		if !c.Group {
			out.components = append(out.components, component{
				ID: c.ID, Name: c.Name, Group: groups[c.GroupID], State: stateOf(c.Status),
			})
		}
	}

	for _, i := range s.Incidents {
		out.incidents = append(out.incidents, i.incident(p, groups, stateOf(i.Impact), false))
	}

	for _, m := range s.Maintenance {
		if m.Status == "in_progress" || m.Status == "verifying" {
			out.incidents = append(out.incidents, m.incident(p, groups, StateMaintenance, true))
		}
	}

	return out, nil
}

// groupNames maps each group among components to its name.
func groupNames(components []spComponent) map[string]string {
	groups := map[string]string{}

	for _, c := range components {
		if c.Group {
			groups[c.ID] = c.Name
		}
	}

	return groups
}

// incident converts the incident, linking it to its page on the provider's host when its
// id makes one path segment, and to the provider's page otherwise.
func (i spIncident) incident(p statusprefs.Provider, groups map[string]string, state State, maintenance bool) incident {
	link := p.PageURL
	if i.ID != "" && !strings.Contains(i.ID, "/") {
		link = (&url.URL{Scheme: "https", Host: p.Host, Path: "/incidents/" + i.ID}).String()
	}

	refs := []ref{}

	for _, c := range i.Components {
		if !c.Group {
			refs = append(refs, ref{ID: c.ID, Name: c.Name, Group: groups[c.GroupID]})
		}
	}

	return incident{
		ID: i.ID, Name: i.Name, Stage: stageOf(i.Status), State: state, URL: link, Refs: refs, Maintenance: maintenance,
	}
}
