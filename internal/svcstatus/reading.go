package svcstatus

import (
	"errors"
	"net/url"
	"path"
	"strings"

	"github.com/mrz1836/nilbox/internal/statusprefs"
)

// ErrNotASummary is returned for a page's answer that isn't the summary its format
// promises.
var ErrNotASummary = errors.New("not a status page summary")

// State is where a provider, a part of it, or an incident stands, on one scale for every
// status page, or why there is no reading.
type State string

// Where a provider stands.
const (
	// StateOff is the check turned off in Settings, or the provider not watched.
	StateOff State = "off"

	// StateUnknown is no reading yet: before the first check lands, or while every check
	// has failed.
	StateUnknown State = "unknown"

	// StateOperational is everything watched working.
	StateOperational State = "operational"

	// StateMaintenance is maintenance under way.
	StateMaintenance State = "maintenance"

	// StateMinor is degraded performance.
	StateMinor State = "minor"

	// StateMajor is a partial outage.
	StateMajor State = "major"

	// StateCritical is a major outage.
	StateCritical State = "critical"
)

// severity orders the states: off and unknown below operational, then maintenance,
// minor, major, and critical.
func (s State) severity() int {
	switch s {
	case StateOperational:
		return 1
	case StateMaintenance:
		return 2
	case StateMinor:
		return 3
	case StateMajor:
		return 4
	case StateCritical:
		return 5
	case StateOff, StateUnknown:
		return 0
	default:
		return 0
	}
}

// trouble reports whether the state is an incident's: minor, major, or critical.
func (s State) trouble() bool {
	return s.severity() >= StateMinor.severity()
}

// worst returns the more severe of a and b.
func worst(a, b State) State {
	if b.severity() > a.severity() {
		return b
	}

	return a
}

// stateOf turns a status page's word for a component's status, or an incident's impact,
// into a State. A word this build doesn't know counts as minor, so the window still says
// something is wrong.
func stateOf(word string) State {
	switch word {
	case "operational":
		return StateOperational
	case "none", "minor", "degraded_performance":
		return StateMinor
	case "major", "partial_outage":
		return StateMajor
	case "critical", "major_outage", "full_outage":
		return StateCritical
	case "maintenance", "under_maintenance":
		return StateMaintenance
	default:
		return StateMinor
	}
}

// Stage is where an incident or a maintenance stands.
type Stage string

// The stages.
const (
	StageInvestigating Stage = "investigating"
	StageIdentified    Stage = "identified"
	StageMonitoring    Stage = "monitoring"
	StageInProgress    Stage = "in_progress"
	StageVerifying     Stage = "verifying"
	StageOther         Stage = "other"
)

// stageOf turns a status page's word for an incident's status into a Stage.
func stageOf(word string) Stage {
	switch s := Stage(word); s {
	case StageInvestigating, StageIdentified, StageMonitoring, StageInProgress, StageVerifying, StageOther:
		return s
	default:
		return StageOther
	}
}

// reading is what one status page said, in one shape whatever its format, before it is
// judged against what the user watches.
type reading struct {
	// components are the page's components: every one a Statuspage page lists, or the
	// ones an incident.io page's incidents and maintenance affect.
	components []component

	// incidents are the incidents not yet resolved and the maintenance under way.
	incidents []incident
}

// component is one of a page's components.
type component struct {
	ID, Name, Group string
	State           State
}

// incident is an incident not yet resolved, or maintenance under way.
type incident struct {
	ID, Name    string
	Stage       Stage
	State       State
	URL         string
	Refs        []ref
	Maintenance bool
}

// ref is a component an incident names.
type ref struct {
	ID, Name, Group string
}

// parse reads a provider's summary in its format.
func parse(p statusprefs.Provider, body []byte) (reading, error) {
	switch p.Format {
	case statusprefs.FormatIncidentIO:
		return parseIncidentIO(p, body)
	case statusprefs.FormatStatuspage:
		return parseStatuspage(p, body)
	default:
		return parseStatuspage(p, body)
	}
}

// pageLink returns raw when it is an https link on the provider's own host, written with
// the catalog's host and its path cleaned ("/" when it has none), and the provider's page
// otherwise, so nothing a page says can send the window elsewhere.
func pageLink(p statusprefs.Provider, raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || !strings.EqualFold(u.Host, p.Host) {
		return p.PageURL
	}

	u.Host = p.Host
	if u.Path == "" {
		u.Path = "/"
	} else {
		u.Path = path.Clean(u.Path)
	}

	u.RawPath = ""

	return u.String()
}
