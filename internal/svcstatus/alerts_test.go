package svcstatus

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// judged is Claude as a reading judges it: state, with the parts affected and incidents.
func judged(state State, affected []Part, incidents ...Incident) Provider {
	return Provider{
		ID: "claude", Name: "Claude", URL: "https://status.claude.com/", State: state, Affected: affected,
		Incidents: incidents,
	}
}

func TestTheFirstReadingOnlySetsTheBaseline(t *testing.T) {
	t.Parallel()

	var a alertState

	assert.Nil(t, a.next(judged(StateMajor, nil)), "a restart during an incident shows no banner")
	assert.Nil(t, a.next(judged(StateMajor, nil)))
	assert.Nil(t, a.next(judged(StateMinor, nil)), "better, within trouble")

	got := a.next(judged(StateCritical, nil))
	require.NotNil(t, got, "worse than the baseline")
	assert.Equal(t, "Claude: Major outage", got.Title)

	got = a.next(judged(StateOperational, nil))
	require.NotNil(t, got, "a recovery from a baseline in trouble is told")
	assert.Equal(t, "Claude: Operational again", got.Title)
}

func TestABannerWhenAPartWatchedGoesDownAndComesBack(t *testing.T) {
	t.Parallel()

	a := alertState{based: true, level: StateOperational}

	api := []Part{{ID: "claude.api", Name: "Claude API", State: StateMajor}, {ID: "claude.code", Name: "Claude Code"}}
	incident := Incident{Name: "Elevated errors on Claude API", URL: "https://status.claude.com/incidents/x1"}

	got := a.next(judged(StateMajor, api, incident))
	assert.Equal(t, &Alert{
		Provider: "claude", State: StateMajor, Title: "Claude: Partial outage",
		Body: "Claude API, Claude Code · Elevated errors on Claude API", URL: "https://status.claude.com/incidents/x1",
	}, got)

	assert.Nil(t, a.next(judged(StateMajor, api, incident)), "told once")
	assert.Nil(t, a.next(judged(StateMinor, api, incident)), "better, within trouble")
	assert.Nil(t, a.next(judged(StateMajor, api, incident)), "back up to the level already told")

	got = a.next(judged(StateCritical, api, incident))
	require.NotNil(t, got, "worse than told")
	assert.Equal(t, "Claude: Major outage", got.Title)

	got = a.next(judged(StateOperational, nil))
	assert.Equal(t, &Alert{
		Provider: "claude", State: StateOperational, Title: "Claude: Operational again", URL: "https://status.claude.com/",
	}, got)

	assert.Nil(t, a.next(judged(StateOperational, nil)))

	got = a.next(judged(StateMinor, nil))
	require.NotNil(t, got, "down again after a recovery")
	assert.Equal(t, "Claude: Degraded", got.Title)
}

func TestMaintenanceAloneShowsNoBanner(t *testing.T) {
	t.Parallel()

	a := alertState{based: true, level: StateOperational}

	assert.Nil(t, a.next(judged(StateMaintenance, nil)))
	assert.Nil(t, a.next(judged(StateOperational, nil)))

	require.NotNil(t, a.next(judged(StateMinor, nil)), "trouble during maintenance")

	got := a.next(judged(StateMaintenance, nil))
	require.NotNil(t, got, "back to maintenance counts as back")
	assert.Equal(t, "Claude: Operational again", got.Title)
}

func TestTheBannersBody(t *testing.T) {
	t.Parallel()

	unscoped := Incident{Name: "Elevated errors", URL: "https://status.claude.com/incidents/x2", Parts: []string{}}
	named := Incident{Name: "Slow responses", URL: "u", Parts: []string{"Claude Code"}}

	for name, tc := range map[string]struct {
		provider Provider
		body     string
		url      string
	}{
		"an incident alone, naming nothing": {judged(StateMajor, nil, unscoped), "Elevated errors", unscoped.URL},
		"an incident alone, naming a part":  {judged(StateMajor, nil, named), "Claude Code · Slow responses", "u"},
		"parts alone": {
			judged(StateMinor, []Part{{Name: "Claude API"}}), "Claude API", "https://status.claude.com/",
		},
	} {
		got := troubleAlert(tc.provider)
		assert.Equal(t, tc.body, got.Body, name)
		assert.Equal(t, tc.url, got.URL, name)
	}
}

func TestStateWordsMatchTheWindows(t *testing.T) {
	t.Parallel()

	for state, want := range map[State]string{
		StateOperational: "Operational", StateMaintenance: "Maintenance", StateMinor: "Degraded",
		StateMajor: "Partial outage", StateCritical: "Major outage", StateUnknown: "Unknown",
	} {
		assert.Equal(t, want, stateWords(state), state)
	}
}
