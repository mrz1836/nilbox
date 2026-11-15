package svcstatus

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/statusprefs"
)

// githubPage is a reading of GitHub's page listing every part the catalog knows,
// operational, with changed applied over them by id.
func githubPage(t testing.TB, changed ...component) reading {
	t.Helper()

	out := reading{incidents: []incident{}}

	for _, part := range provider(t, statusprefs.ProviderGitHub).Parts {
		c := component{ID: part.PageIDs[0], Name: part.Name, State: StateOperational}

		for _, ch := range changed {
			if ch.ID == c.ID {
				c = ch
			}
		}

		out.components = append(out.components, c)
	}

	return out
}

// The page ids of a few of GitHub's parts.
const (
	pullsID    = "hhtssxt0f5v2"
	actionsID  = "br0l2tvcx85d"
	copilotID  = "pjmpxvq2cmr2"
	pagesID    = "vg70hn9s2tyj"
	gitID      = "8l4ygp009s5s"
	issuesID   = "kr09ddfgbfsf"
	apiID      = "brv1bkgrwx7q"
	webhooksID = "4230lsnqdsld"
)

func TestJudge(t *testing.T) {
	t.Parallel()

	defaults := []statusprefs.PartID{"github.git", "github.api", "github.issues", "github.pulls", "github.actions"}

	withIncidents := func(r reading, incidents ...incident) reading {
		r.incidents = incidents

		return r
	}

	tests := []struct {
		name     string
		provider statusprefs.ProviderID
		read     reading
		picked   []statusprefs.PartID
		want     Provider
	}{
		{
			name: "everything operational", provider: statusprefs.ProviderGitHub, read: githubPage(t), picked: defaults,
			want: Provider{State: StateOperational},
		},
		{
			name: "a part watched is degraded", provider: statusprefs.ProviderGitHub,
			read:   githubPage(t, component{ID: pullsID, Name: "Pull Requests", State: StateMinor}),
			picked: defaults,
			want: Provider{
				State: StateMinor, Affected: []Part{{ID: "github.pulls", Name: "Pull Requests", State: StateMinor}},
			},
		},
		{
			name: "a part not watched is down", provider: statusprefs.ProviderGitHub,
			read:   githubPage(t, component{ID: copilotID, Name: "Copilot", State: StateCritical}),
			picked: defaults,
			want:   Provider{State: StateOperational},
		},
		{
			name: "an incident that names a part watched counts", provider: statusprefs.ProviderGitHub,
			read: withIncidents(githubPage(t), incident{
				Name: "Incident with Actions", Stage: StageInvestigating, State: StateMajor, URL: "u1",
				Refs: []ref{{ID: actionsID, Name: "Actions"}, {ID: copilotID, Name: "Copilot"}},
			}),
			picked: defaults,
			want: Provider{State: StateMajor, Incidents: []Incident{{
				Name: "Incident with Actions", Stage: StageInvestigating, State: StateMajor, URL: "u1",
				Parts: []string{"Actions"},
			}}},
		},
		{
			name: "an incident that names only parts not watched is another", provider: statusprefs.ProviderGitHub,
			read: withIncidents(githubPage(t), incident{
				Name: "Incident with Copilot", State: StateCritical, Refs: []ref{{ID: copilotID, Name: "Copilot"}},
			}),
			picked: defaults,
			want:   Provider{State: StateOperational, Others: 1},
		},
		{
			name: "an incident that names no part counts only when major or critical", provider: statusprefs.ProviderGitHub,
			read: withIncidents(githubPage(t),
				incident{Name: "Minor and unscoped", State: StateMinor, Refs: []ref{}},
				incident{Name: "Major and unscoped", Stage: StageIdentified, State: StateMajor, URL: "u2", Refs: []ref{}},
				incident{Name: "Critical and unscoped", State: StateCritical, URL: "u3"},
				incident{Name: "Maintenance, unscoped", State: StateMaintenance, Maintenance: true},
			),
			picked: defaults,
			want: Provider{State: StateCritical, Others: 2, Incidents: []Incident{
				{Name: "Major and unscoped", Stage: StageIdentified, State: StateMajor, URL: "u2", Parts: []string{}, Unscoped: true},
				{Name: "Critical and unscoped", State: StateCritical, URL: "u3", Parts: []string{}, Unscoped: true},
			}},
		},
		{
			name: "impact none counts as minor", provider: statusprefs.ProviderGitHub,
			read: withIncidents(githubPage(t), incident{
				Name: "Investigating reports", State: stateOf("none"), Refs: []ref{{ID: issuesID, Name: "Issues"}},
			}),
			picked: defaults,
			want: Provider{State: StateMinor, Incidents: []Incident{{
				Name: "Investigating reports", State: StateMinor, Parts: []string{"Issues"},
			}}},
		},
		{
			name: "maintenance under way on a part watched", provider: statusprefs.ProviderGitHub,
			read: withIncidents(githubPage(t, component{ID: gitID, Name: "Git Operations", State: StateMaintenance}),
				incident{
					Name: "Upgrade", Stage: StageInProgress, State: StateMaintenance, Maintenance: true,
					Refs: []ref{{ID: gitID, Name: "Git Operations"}},
				}),
			picked: defaults,
			want: Provider{
				State:    StateMaintenance,
				Affected: []Part{{ID: "github.git", Name: "Git Operations", State: StateMaintenance}},
				Incidents: []Incident{{
					Name: "Upgrade", Stage: StageInProgress, State: StateMaintenance, Parts: []string{"Git Operations"},
					Maintenance: true,
				}},
			},
		},
		{
			name: "the worst over the parts and the incidents", provider: statusprefs.ProviderGitHub,
			read: withIncidents(githubPage(t, component{ID: apiID, Name: "API Requests", State: StateMinor}),
				incident{Name: "Outage", State: StateCritical, Refs: []ref{{ID: apiID, Name: "API Requests"}}}),
			picked: defaults,
			want: Provider{
				State:     StateCritical,
				Affected:  []Part{{ID: "github.api", Name: "API Requests", State: StateMinor}},
				Incidents: []Incident{{Name: "Outage", State: StateCritical, Parts: []string{"API Requests"}}},
			},
		},
		{
			name: "a component that changed its id is matched by name", provider: statusprefs.ProviderGitHub,
			read: reading{components: []component{
				{ID: "new-id", Name: "pull requests", State: StateMajor},
				{ID: gitID, Name: "Git Operations", State: StateOperational},
			}, incidents: []incident{{Name: "Slow PRs", State: StateMajor, Refs: []ref{{ID: "new-id", Name: "Pull Requests"}}}}},
			picked: []statusprefs.PartID{"github.pulls", "github.git"},
			want: Provider{
				State:     StateMajor,
				Affected:  []Part{{ID: "github.pulls", Name: "Pull Requests", State: StateMajor}},
				Incidents: []Incident{{Name: "Slow PRs", State: StateMajor, Parts: []string{"Pull Requests"}}},
			},
		},
		{
			name: "parts watched that the page no longer lists", provider: statusprefs.ProviderGitHub,
			read:   reading{components: []component{{ID: gitID, Name: "Git Operations", State: StateOperational}}},
			picked: []statusprefs.PartID{"github.git", "github.actions", "github.webhooks"},
			want:   Provider{State: StateOperational, Unlisted: []statusprefs.PartID{"github.actions", "github.webhooks"}},
		},
		{
			name: "a part takes the worst of its components", provider: statusprefs.ProviderGitHub,
			read: reading{components: []component{
				{ID: pagesID, Name: "Pages", State: StateMinor},
				{ID: "other", Name: "Pages", State: StateCritical},
			}},
			picked: []statusprefs.PartID{"github.pages"},
			want:   Provider{State: StateCritical, Affected: []Part{{ID: "github.pages", Name: "Pages", State: StateCritical}}},
		},
		{
			name: "nothing picked", provider: statusprefs.ProviderGitHub,
			read:   githubPage(t, component{ID: webhooksID, Name: "Webhooks", State: StateCritical}),
			picked: []statusprefs.PartID{},
			want:   Provider{State: StateOperational},
		},
		{
			name: "the two Logins are told apart by group", provider: statusprefs.ProviderOpenAI,
			read: reading{
				components: []component{{ID: "01JSM5RTJWHRWDTS6Q604VEW3B", Name: "Login", Group: "APIs", State: StateCritical}},
				incidents: []incident{{
					Name: "API login errors", State: StateCritical,
					Refs: []ref{{ID: "01JSM5RTJWHRWDTS6Q604VEW3B", Name: "Login", Group: "APIs"}},
				}},
			},
			picked: []statusprefs.PartID{"openai.login"},
			want:   Provider{State: StateOperational, Others: 1},
		},
		{
			name: "ChatGPT's Login under a new id is matched by name and group", provider: statusprefs.ProviderOpenAI,
			read: reading{
				components: []component{{ID: "new", Name: "Login", Group: "ChatGPT", State: StateMajor}},
				incidents: []incident{{
					Name: "Sign-in errors", State: StateMajor, Refs: []ref{{ID: "new", Name: "Login", Group: "ChatGPT"}},
				}},
			},
			picked: []statusprefs.PartID{"openai.login", "openai.codex-cli"},
			want: Provider{
				State:     StateMajor,
				Affected:  []Part{{ID: "openai.login", Name: "Login", State: StateMajor}},
				Incidents: []Incident{{Name: "Sign-in errors", State: StateMajor, Parts: []string{"Login"}}},
			},
		},
		{
			name: "an incident.io page lists only what is affected, so nothing is unlisted", provider: statusprefs.ProviderOpenAI,
			read:   reading{components: []component{}, incidents: []incident{}},
			picked: []statusprefs.PartID{"openai.codex-api", "openai.codex-cli", "openai.responses"},
			want:   Provider{State: StateOperational},
		},
		{
			name: "Codex CLI is matched by the page's name for it", provider: statusprefs.ProviderOpenAI,
			read:   reading{components: []component{{ID: "renamed", Name: "CLI", Group: "Codex", State: StateMinor}}},
			picked: []statusprefs.PartID{"openai.codex-cli"},
			want: Provider{
				State: StateMinor, Affected: []Part{{ID: "openai.codex-cli", Name: "Codex CLI", State: StateMinor}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := provider(t, tc.provider)

			want := tc.want
			want.ID, want.Name, want.URL = p.ID, p.Name, p.PageURL

			if want.Affected == nil {
				want.Affected = []Part{}
			}

			if want.Incidents == nil {
				want.Incidents = []Incident{}
			}

			if want.Unlisted == nil {
				want.Unlisted = []statusprefs.PartID{}
			}

			assert.Equal(t, want, judge(p, tc.read, tc.picked))
		})
	}
}

func TestJudgeOnRecordedPages(t *testing.T) {
	t.Parallel()

	claude := provider(t, statusprefs.ProviderClaude)

	read, err := parseStatuspage(claude, fixture(t, "claude-incident.json"))
	require.NoError(t, err)

	got := judge(claude, read, statusprefs.Defaults().Watch[1].Parts)
	assert.Equal(t, StateOperational, got.State, "claude.ai's incident touches neither the API nor Claude Code")
	assert.Equal(t, 1, got.Others)
	assert.Empty(t, got.Unlisted)

	got = judge(claude, read, []statusprefs.PartID{"claude.api", "claude.code", "claude.ai"})
	assert.Equal(t, StateMajor, got.State, "ticked, claude.ai's incident counts at its impact")
	assert.Equal(t, []Part{{ID: "claude.ai", Name: "claude.ai", State: StateMinor}}, got.Affected)
	require.Len(t, got.Incidents, 1)
	assert.Equal(t, []string{"claude.ai"}, got.Incidents[0].Parts)

	openai := provider(t, statusprefs.ProviderOpenAI)

	read, err = parseIncidentIO(openai, fixture(t, "openai-incident.json"))
	require.NoError(t, err)

	got = judge(openai, read, statusprefs.Defaults().Watch[2].Parts)
	assert.Equal(t, StateOperational, got.State, "the Compliance API isn't Codex")
	assert.Equal(t, 1, got.Others)

	github := provider(t, statusprefs.ProviderGitHub)

	read, err = parseStatuspage(github, fixture(t, "github-operational.json"))
	require.NoError(t, err)

	got = judge(github, read, statusprefs.Defaults().Watch[0].Parts)
	assert.Equal(t, StateOperational, got.State)
	assert.Empty(t, got.Unlisted, "the catalog's ids are the page's")

	every := make([]statusprefs.PartID, 0, len(github.Parts))
	for _, part := range github.Parts {
		every = append(every, part.ID)
	}

	assert.Empty(t, judge(github, read, every).Unlisted, "every part in the catalog is on the page")

	read, err = parseStatuspage(claude, fixture(t, "claude-incident.json"))
	require.NoError(t, err)

	every = make([]statusprefs.PartID, 0, len(claude.Parts))
	for _, part := range claude.Parts {
		every = append(every, part.ID)
	}

	assert.Empty(t, judge(claude, read, every).Unlisted, "every part of Claude's in the catalog is on the page")
}
