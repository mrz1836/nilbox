package inbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
)

// Token scopes the project tests sign in with.
const (
	scopesWithProjects    = "repo, read:org, read:project"
	scopesWithoutProjects = "repo, read:org"
)

// roadmap is a project an issue is in, with its status there.
func roadmap() github.ProjectItem {
	return github.ProjectItem{
		Title: "Roadmap", URL: "https://github.com/orgs/acme-org/projects/1", Status: "In progress", StatusColor: "#bf8700",
	}
}

// projectsHarness is a harness whose inbox holds a pull request (1) and an issue (2)
// that is in the roadmap project, signed in with scopes.
func projectsHarness(t *testing.T, scopes string) *harness {
	t.Helper()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, []github.Item{issue("example-repo", 2, "MEMBER")})
	h.client.inbox.OAuthScopes = scopes
	h.client.details = map[string]github.ItemDetails{nodeID(1): prDetails(), nodeID(2): {Comments: 1}}
	h.client.projects = map[string][]github.ProjectItem{nodeID(2): {roadmap()}}

	return h
}

// issueProjects returns the projects the snapshot shows on the issue numbered 2.
func issueProjects(t *testing.T, snap Snapshot) []Project {
	t.Helper()

	details := itemByNumber(t, snap.Assigned, 2).Details
	require.NotNil(t, details)

	return details.Projects
}

// wantRoadmap is the roadmap project as a card shows it.
func wantRoadmap() []Project {
	return []Project{{
		Title: "Roadmap", URL: "https://github.com/orgs/acme-org/projects/1", Status: "In progress", StatusColor: "#bf8700",
	}}
}

func TestSnapshotProjectAccessBeforeTheFirstRefresh(t *testing.T) {
	t.Parallel()

	snap, err := newHarness(t).svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ProjectAccessUnknown, snap.ProjectAccess)
}

func TestRefreshReadsTheProjectsWithTheScope(t *testing.T) {
	t.Parallel()

	h := projectsHarness(t, scopesWithProjects)

	snap := h.refresh(t)
	assert.Equal(t, ProjectAccessOn, snap.ProjectAccess)
	assert.Equal(t, wantRoadmap(), issueProjects(t, snap))
	assert.Empty(t, itemByNumber(t, snap.ReviewRequested, 1).Details.Projects, "a pull request shows none")
	assert.Equal(t, []github.DetailOptions{{Projects: true}}, h.client.detailsOpts,
		"the first refresh reads every item once its scopes are known")

	// The next refresh knows already, and reads the listed items with their projects
	// alongside the inbox.
	h.client.detailsOpts = nil

	snap = h.refresh(t)
	assert.Equal(t, wantRoadmap(), issueProjects(t, snap))
	assert.Equal(t, []github.DetailOptions{{Projects: true}}, h.client.detailsOpts)

	// The projects survive a restart.
	snap, err := h.service().Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ProjectAccessOn, snap.ProjectAccess)
	assert.Equal(t, wantRoadmap(), issueProjects(t, snap))
}

func TestRefreshWithoutTheScopeReadsNoProjects(t *testing.T) {
	t.Parallel()

	h := projectsHarness(t, scopesWithoutProjects)

	snap := h.refresh(t)
	assert.Empty(t, snap.LastError, "a missing read:project scope never fails a refresh")
	assert.Equal(t, ProjectAccessNeedsScope, snap.ProjectAccess)
	assert.Empty(t, issueProjects(t, snap))
	assert.Equal(t, []github.DetailOptions{{}}, h.client.detailsOpts, "GitHub is never asked for them")

	h.client.detailsOpts = nil

	snap = h.refresh(t)
	assert.Equal(t, ProjectAccessNeedsScope, snap.ProjectAccess)
	assert.Equal(t, []github.DetailOptions{{}}, h.client.detailsOpts)
}

func TestRefreshReadsEveryItemAgainOnceTheScopeIsGranted(t *testing.T) {
	t.Parallel()

	h := projectsHarness(t, scopesWithoutProjects)
	h.refresh(t)

	// gh auth refresh -s read:project
	h.client.inbox.OAuthScopes = scopesWithProjects
	h.client.detailsIDs, h.client.detailsOpts = nil, nil

	snap := h.refresh(t)
	assert.Equal(t, ProjectAccessOn, snap.ProjectAccess)
	assert.Equal(t, wantRoadmap(), issueProjects(t, snap), "shown on the first refresh after the scope is granted")
	assert.Equal(t, []github.DetailOptions{{}, {Projects: true}}, h.client.detailsOpts)
	require.Len(t, h.client.detailsIDs, 2)

	for _, ids := range h.client.detailsIDs {
		assert.ElementsMatch(t, []string{nodeID(1), nodeID(2)}, ids, "the listed items are read again, with their projects")
	}
}

func TestRefreshStopsShowingTheProjectsOnceTheScopeIsGone(t *testing.T) {
	t.Parallel()

	h := projectsHarness(t, scopesWithProjects)
	h.refresh(t)

	// gh auth refresh -r read:project: the listed items are asked for with their
	// projects, and GitHub refuses them.
	h.client.inbox.OAuthScopes = scopesWithoutProjects
	h.client.refuseProjects = true
	h.client.detailsIDs, h.client.detailsOpts = nil, nil

	snap := h.refresh(t)
	assert.Empty(t, snap.LastError)
	assert.Equal(t, ProjectAccessNeedsScope, snap.ProjectAccess)
	assert.Empty(t, issueProjects(t, snap), "no project is left over from before")
	assert.Equal(t, []github.DetailOptions{{Projects: true}}, h.client.detailsOpts,
		"the details read without the projects stand; nothing is read again")
}

func TestRefreshAsksATokenThatReportsNoScopes(t *testing.T) {
	t.Parallel()

	h := projectsHarness(t, "")

	snap := h.refresh(t)
	assert.Equal(t, ProjectAccessOn, snap.ProjectAccess)
	assert.Equal(t, wantRoadmap(), issueProjects(t, snap))
}

func TestRefreshStopsAskingATokenThatReportsNoScopesOnceRefused(t *testing.T) {
	t.Parallel()

	h := projectsHarness(t, "")
	h.client.refuseProjects = true

	snap := h.refresh(t)
	assert.Equal(t, ProjectAccessNeedsScope, snap.ProjectAccess)
	assert.Equal(t, []github.DetailOptions{{Projects: true}}, h.client.detailsOpts)

	h.client.detailsOpts = nil

	snap = h.refresh(t)
	assert.Equal(t, ProjectAccessNeedsScope, snap.ProjectAccess)
	assert.Equal(t, []github.DetailOptions{{}}, h.client.detailsOpts, "it is not asked again")
}

func TestRefreshItemReadsTheProjectsTheLastRefreshCould(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		scopes string
		want   github.DetailOptions
	}{
		"with the scope":    {scopes: scopesWithProjects, want: github.DetailOptions{Projects: true}},
		"without the scope": {scopes: scopesWithoutProjects, want: github.DetailOptions{}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := projectsHarness(t, tt.scopes)
			snap := h.refresh(t)

			moved := issue("example-repo", 2, "MEMBER")
			moved.Title = "Item 2, retitled"
			h.client.items = map[string]*github.ItemUpdate{
				nodeID(2): {Found: true, Item: moved, Details: github.ItemDetails{Comments: 2}},
			}

			snap, err := h.svc.RefreshItem(t.Context(), itemByNumber(t, snap.Assigned, 2).ID)
			require.NoError(t, err)

			assert.Equal(t, []github.DetailOptions{tt.want}, h.client.itemOpts)
			assert.Equal(t, "Item 2, retitled", itemByNumber(t, snap.Assigned, 2).Title)

			if tt.want.Projects {
				assert.Equal(t, wantRoadmap(), issueProjects(t, snap))
			} else {
				assert.Empty(t, issueProjects(t, snap))
			}
		})
	}
}

func TestDecodeProjectAccess(t *testing.T) {
	t.Parallel()

	for stored, want := range map[string]ProjectAccess{
		"on": ProjectAccessOn, "needs_scope": ProjectAccessNeedsScope, "": ProjectAccessUnknown, "maybe": ProjectAccessUnknown,
	} {
		assert.Equal(t, want, decodeProjectAccess(stored), stored)
	}
}
