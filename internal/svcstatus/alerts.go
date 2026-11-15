package svcstatus

import (
	"context"
	"strings"

	"github.com/mrz1836/nilbox/internal/statusprefs"
)

// Notifier hears the changes worth a banner: a provider the user watches going down, or
// coming back. Core adapts the notifications to it, so this package never imports them.
type Notifier interface {
	Alert(ctx context.Context, alert Alert)
}

// Alert is a change worth a banner.
type Alert struct {
	// Provider is the provider that changed, and State where it stands now.
	Provider statusprefs.ProviderID
	State    State

	// Title is its sentence ("Claude: Partial outage"), Body what follows it, which may be
	// "", and URL the page a click on it opens: the incident's, or else the provider's
	// status page.
	Title string
	Body  string
	URL   string
}

// alertState is what one provider's alerts remember, in memory alone.
//
// The first reading after a start, or after the provider was turned on or what is watched
// on it changed, only sets the baseline, so a restart during an incident never shows its
// banner again. After that, a state that rises into minor, major, or critical, above the
// level already known, shows a banner, and a state that falls back to operational or
// maintenance after one shows another, that it works again. A state that improves within
// trouble shows none, and keeps the level, so a flapping incident shows its banner once; a
// check that fails changes nothing.
type alertState struct {
	based bool
	level State
}

// next takes the provider as a new reading judges it, and returns the alert it is worth,
// or nil.
func (a *alertState) next(judged Provider) *Alert {
	now := judged.State

	if !a.based {
		a.based, a.level = true, now

		return nil
	}

	switch {
	case now.trouble() && now.severity() > a.level.severity():
		a.level = now

		return troubleAlert(judged)
	case !now.trouble() && a.level.trouble():
		a.level = now

		return &Alert{
			Provider: judged.ID, State: now, Title: judged.Name + ": " + stateWords(StateOperational) + " again",
			URL: judged.URL,
		}
	case !now.trouble():
		a.level = now
	}

	return nil
}

// troubleAlert is the banner of a provider gone down: its state in the window's words,
// the parts affected, and the first incident that touches them, which a click opens.
func troubleAlert(judged Provider) *Alert {
	parts := make([]string, 0, len(judged.Affected))
	for _, part := range judged.Affected {
		parts = append(parts, part.Name)
	}

	url := judged.URL

	var first string

	if len(judged.Incidents) > 0 {
		first, url = judged.Incidents[0].Name, judged.Incidents[0].URL

		if len(parts) == 0 {
			parts = judged.Incidents[0].Parts
		}
	}

	body := strings.Join(parts, ", ")

	switch {
	case body == "":
		body = first
	case first != "":
		body += " · " + first
	}

	return &Alert{
		Provider: judged.ID, State: judged.State, Title: judged.Name + ": " + stateWords(judged.State), Body: body,
		URL: url,
	}
}

// stateWords is a state in the window's words, the same the window shows
// (frontend/src/lib/serviceStatus.ts).
func stateWords(s State) string {
	switch s {
	case StateOperational:
		return "Operational"
	case StateMaintenance:
		return "Maintenance"
	case StateMinor:
		return "Degraded"
	case StateMajor:
		return "Partial outage"
	case StateCritical:
		return "Major outage"
	case StateOff, StateUnknown:
		return "Unknown"
	default:
		return "Unknown"
	}
}
