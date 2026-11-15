package svcstatus

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/statusprefs"
)

// provider returns the catalog's provider id.
func provider(t testing.TB, id statusprefs.ProviderID) statusprefs.Provider {
	t.Helper()

	p, ok := statusprefs.Lookup(id)
	require.True(t, ok, id)

	return p
}

// fixture reads a page recorded in testdata.
func fixture(t testing.TB, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // the test's own testdata
	require.NoError(t, err)

	return data
}

func TestParseStatuspageReadsGitHubsRecordedPage(t *testing.T) {
	t.Parallel()

	got, err := parseStatuspage(provider(t, statusprefs.ProviderGitHub), fixture(t, "github-operational.json"))
	require.NoError(t, err)

	assert.Len(t, got.components, 12, "every component, the placeholder too: the catalog leaves it out")
	assert.Equal(t, component{ID: "8l4ygp009s5s", Name: "Git Operations", State: StateOperational}, got.components[0])
	assert.Empty(t, got.incidents)
	assert.NotNil(t, got.incidents)
}

func TestParseStatuspageReadsClaudesRecordedIncident(t *testing.T) {
	t.Parallel()

	got, err := parseStatuspage(provider(t, statusprefs.ProviderClaude), fixture(t, "claude-incident.json"))
	require.NoError(t, err)

	assert.Len(t, got.components, 9, "the group of cloud platforms is left out")
	assert.Contains(t, got.components, component{
		ID: "rwppv331jlwc", Name: "claude.ai", State: StateMinor,
	})
	assert.Contains(t, got.components, component{
		ID: "w14q5j51spbm", Name: "Claude Platform on AWS", Group: "Claude on Cloud Platforms", State: StateOperational,
	}, "a member carries its group's name")

	assert.Equal(t, []incident{{
		ID: "ct4c2pwf2xpj", Name: "Some users unable to sign up with Apple on the Claude iOS app",
		Stage: StageInvestigating, State: StateMajor, URL: "https://status.claude.com/incidents/ct4c2pwf2xpj",
		Refs: []ref{{ID: "rwppv331jlwc", Name: "claude.ai"}},
	}}, got.incidents)
}

func TestParseStatuspageCountsOnlyMaintenanceUnderWay(t *testing.T) {
	t.Parallel()

	got, err := parseStatuspage(provider(t, statusprefs.ProviderGitHub), []byte(`{
		"status": {"indicator": "maintenance"},
		"components": [{"id": "a", "name": "Actions", "status": "under_maintenance"}],
		"incidents": [{"id": "../x", "name": "A bad id", "status": "identified", "impact": "none", "components": []}],
		"scheduled_maintenances": [
			{"id": "m1", "name": "Database upgrade", "status": "in_progress", "impact": "maintenance",
			 "components": [{"id": "a", "name": "Actions"}]},
			{"id": "m2", "name": "Checking", "status": "verifying", "impact": "maintenance", "components": []},
			{"id": "m3", "name": "Next week", "status": "scheduled", "impact": "maintenance", "components": []},
			{"id": "m4", "name": "Done", "status": "completed", "impact": "maintenance", "components": []}
		]
	}`))
	require.NoError(t, err)

	assert.Equal(t, []component{{ID: "a", Name: "Actions", State: StateMaintenance}}, got.components)
	assert.Equal(t, []incident{
		{
			ID: "../x", Name: "A bad id", Stage: StageIdentified, State: StateMinor,
			URL: "https://www.githubstatus.com/", Refs: []ref{},
		},
		{
			ID: "m1", Name: "Database upgrade", Stage: StageInProgress, State: StateMaintenance,
			URL: "https://www.githubstatus.com/incidents/m1", Refs: []ref{{ID: "a", Name: "Actions"}}, Maintenance: true,
		},
		{
			ID: "m2", Name: "Checking", Stage: StageVerifying, State: StateMaintenance,
			URL: "https://www.githubstatus.com/incidents/m2", Refs: []ref{}, Maintenance: true,
		},
	}, got.incidents, "an id that isn't one path segment links to the page, and impact none counts as minor")
}

func TestParseStatuspageRefusesWhatIsntASummary(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"not JSON":       `<html>down</html>`,
		"no state":       `{"status": {}, "components": []}`,
		"another format": `{"ongoing_incidents": []}`,
	} {
		_, err := parseStatuspage(provider(t, statusprefs.ProviderGitHub), []byte(body))
		require.ErrorIs(t, err, ErrNotASummary, name)
		assert.Contains(t, err.Error(), "GitHub's status page", name)
	}
}

func TestStatesShareOneScale(t *testing.T) {
	t.Parallel()

	for word, want := range map[string]State{
		"operational": StateOperational, "degraded_performance": StateMinor, "partial_outage": StateMajor,
		"major_outage": StateCritical, "full_outage": StateCritical, "under_maintenance": StateMaintenance,
		"none": StateMinor, "minor": StateMinor, "major": StateMajor, "critical": StateCritical,
		"maintenance": StateMaintenance, "on fire": StateMinor, "": StateMinor,
	} {
		assert.Equal(t, want, stateOf(word), word)
	}

	for word, want := range map[string]Stage{
		"investigating": StageInvestigating, "identified": StageIdentified, "monitoring": StageMonitoring,
		"in_progress": StageInProgress, "verifying": StageVerifying, "resolved": StageOther, "": StageOther,
	} {
		assert.Equal(t, want, stageOf(word), word)
	}

	assert.Equal(t, StateCritical, worst(StateMajor, StateCritical))
	assert.Equal(t, StateMinor, worst(StateMinor, StateMaintenance), "maintenance is below an incident")
	assert.Equal(t, StateOperational, worst(StateUnknown, StateOperational))
}
