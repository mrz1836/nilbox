package svcstatus

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/statusprefs"
)

func TestParseIncidentIOReadsOpenAIsRecordedIncident(t *testing.T) {
	t.Parallel()

	got, err := parseIncidentIO(provider(t, statusprefs.ProviderOpenAI), fixture(t, "openai-incident.json"))
	require.NoError(t, err)

	assert.Equal(t, []component{{
		ID: "01JNKS9D9S72PMP1938PVFFQN4", Name: "Compliance API", Group: "ChatGPT", State: StateMinor,
	}}, got.components, "only what an incident affects is listed")
	assert.Equal(t, []incident{{
		ID: "01M7ATRS6BMZ61E4JDHJGM79Y0", Name: "Delayed Costs Data in the Compliance API", Stage: StageMonitoring,
		State: StateMinor, URL: "https://status.openai.com/incidents/01M7ATRS6BMZ61E4JDHJGM79Y0",
		Refs: []ref{{ID: "01JNKS9D9S72PMP1938PVFFQN4", Name: "Compliance API", Group: "ChatGPT"}},
	}}, got.incidents, "the page's own link, its doubled slash cleaned")
}

func TestParseIncidentIOReadsMaintenanceAndTheWorstState(t *testing.T) {
	t.Parallel()

	got, err := parseIncidentIO(provider(t, statusprefs.ProviderOpenAI), []byte(`{
		"ongoing_incidents": [
			{"id": "i1", "name": "Codex errors", "status": "identified", "url": "https://elsewhere.example.com/i1",
			 "current_worst_impact": "full_outage",
			 "affected_components": [
				{"id": "c1", "name": "CLI", "group_name": "Codex", "current_status": "degraded_performance"}
			 ]},
			{"id": "i2", "name": "More Codex errors", "status": "investigating", "url": "http://status.openai.com/i2",
			 "current_worst_impact": "partial_outage",
			 "affected_components": [
				{"id": "c1", "name": "CLI", "group_name": "Codex", "current_status": "full_outage"}
			 ]}
		],
		"in_progress_maintenances": [
			{"id": "m1", "name": "Upgrade", "status": "maintenance_in_progress", "url": "https://status.openai.com/m1",
			 "affected_components": [{"id": "c2", "name": "Responses", "group_name": "APIs"}]}
		]
	}`))
	require.NoError(t, err)

	assert.Equal(t, []component{
		{ID: "c1", Name: "CLI", Group: "Codex", State: StateCritical},
		{ID: "c2", Name: "Responses", Group: "APIs", State: StateMaintenance},
	}, got.components, "a component two incidents affect takes the worse status")

	require.Len(t, got.incidents, 3)
	assert.Equal(t, StateCritical, got.incidents[0].State)
	assert.Equal(t, "https://status.openai.com/", got.incidents[0].URL, "a link off the host goes to the page")
	assert.Equal(t, "https://status.openai.com/", got.incidents[1].URL, "and so does one that isn't https")
	assert.Equal(t, incident{
		ID: "m1", Name: "Upgrade", Stage: StageInProgress, State: StateMaintenance, URL: "https://status.openai.com/m1",
		Refs: []ref{{ID: "c2", Name: "Responses", Group: "APIs"}}, Maintenance: true,
	}, got.incidents[2])
}

func TestParseIncidentIORefusesWhatIsntASummary(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"not JSON":       `<html>down</html>`,
		"no incidents":   `{"page_title": "OpenAI"}`,
		"null incidents": `{"ongoing_incidents": null}`,
		"another format": `{"status": {"indicator": "none"}, "incidents": []}`,
	} {
		_, err := parseIncidentIO(provider(t, statusprefs.ProviderOpenAI), []byte(body))
		require.ErrorIs(t, err, ErrNotASummary, name)
		assert.Contains(t, err.Error(), "OpenAI's status page", name)
	}
}

func TestPageLinkStaysOnTheProvidersHost(t *testing.T) {
	t.Parallel()

	p := provider(t, statusprefs.ProviderOpenAI)

	for raw, want := range map[string]string{
		"https://status.openai.com//incidents/x": "https://status.openai.com/incidents/x",
		"https://STATUS.openai.com/incidents/x":  "https://STATUS.openai.com/incidents/x",
		"https://status.openai.com.example.com/": "https://status.openai.com/",
		"https://user@status.openai.com/x":       "https://status.openai.com/",
		"javascript:alert(1)":                    "https://status.openai.com/",
		"":                                       "https://status.openai.com/",
		"://":                                    "https://status.openai.com/",
	} {
		assert.Equal(t, want, pageLink(p, raw), raw)
	}
}
