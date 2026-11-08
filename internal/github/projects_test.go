package github

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refusedProjects is GitHub's answer to a query that asks for projects with a token
// that lacks the read:project scope: errors, and no data.
const refusedProjects = `{"errors":[{"type":"INSUFFICIENT_SCOPES","locations":[{"line":44,"column":5}],` +
	`"message":"Your token has not been granted the required scopes to execute this query. The 'projectItems' ` +
	`field requires one of the following scopes: ['read:project'], but your token has only been granted the: ` +
	`['gist', 'read:org', 'repo', 'workflow'] scopes."}]}`

// issueWithProjects is the details answer for one issue in several projects.
const issueWithProjects = `{"data":{"nodes":[{
  "__typename":"Issue","id":"I_fakeNodeIdIssue","comments":{"totalCount":3},"labels":{"nodes":[]},
  "projectItems":{"nodes":[
    {"project":{"title":"Roadmap","url":"https://github.com/orgs/acme-org/projects/1","closed":false},
     "status":{"name":"In progress","color":"YELLOW"}},
    {"project":{"title":"Old plans","url":"https://github.com/orgs/acme-org/projects/2","closed":true},
     "status":{"name":"Done","color":"PURPLE"}},
    {"project":null,"status":null},
    null,
    {"project":{"title":"Sprint","url":"https://github.com/users/octo-user/projects/3","closed":false},
     "status":{}},
    {"project":{"title":"Triage","url":"https://github.com/orgs/acme-org/projects/4","closed":false},
     "status":{"name":"New","color":"CHARTREUSE"}}
  ]}
}],"rateLimit":{"cost":1,"limit":5000,"remaining":4990,"used":10,"resetAt":"2026-01-10T13:00:00Z"}}}`

// plainIssue is the details answer for one issue, read without its projects.
const plainIssue = `{"data":{"nodes":[{
  "__typename":"Issue","id":"I_fakeNodeIdIssue","comments":{"totalCount":3},"labels":{"nodes":[]}
}],"rateLimit":{"cost":1,"limit":5000,"remaining":4990,"used":10,"resetAt":"2026-01-10T13:00:00Z"}}}`

// asksForProjects reports whether a request's query reads the projects.
func asksForProjects(t *testing.T, r *http.Request) bool {
	t.Helper()

	return strings.Contains(readDetailsRequest(t, r).Query, "projectItems(")
}

func TestFetchDetailsReadsAnIssuesProjectsWhenAsked(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.True(t, asksForProjects(t, r))

		_, _ = io.WriteString(w, issueWithProjects)
	})

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{"I_fakeNodeIdIssue"},
		DetailOptions{Projects: true})
	require.NoError(t, err)

	assert.False(t, details.ProjectsRefused)
	assert.Equal(t, []ProjectItem{
		{
			Title: "Roadmap", URL: "https://github.com/orgs/acme-org/projects/1",
			Status: "In progress", StatusColor: "#bf8700",
		},
		{Title: "Sprint", URL: "https://github.com/users/octo-user/projects/3"},
		{Title: "Triage", URL: "https://github.com/orgs/acme-org/projects/4", Status: "New"},
	}, details.Items["I_fakeNodeIdIssue"].Projects,
		"a closed project, one the token cannot see, and a null item are left out; an unknown color is none")
}

func TestFetchDetailsLeavesTheProjectsOutUnlessAsked(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.False(t, asksForProjects(t, r))

		_, _ = io.WriteString(w, plainIssue)
	})

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{"I_fakeNodeIdIssue"},
		DetailOptions{})
	require.NoError(t, err)

	assert.False(t, details.ProjectsRefused)
	assert.Nil(t, details.Items["I_fakeNodeIdIssue"].Projects)
	assert.Equal(t, 3, details.Items["I_fakeNodeIdIssue"].Comments)
}

func TestFetchDetailsAsksAgainWithoutTheProjectsWhenTheScopeIsMissing(t *testing.T) {
	t.Parallel()

	var withProjects, without atomic.Int32

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if asksForProjects(t, r) {
			withProjects.Add(1)

			_, _ = io.WriteString(w, refusedProjects)

			return
		}

		without.Add(1)

		_, _ = io.WriteString(w, plainIssue)
	})

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{"I_fakeNodeIdIssue"},
		DetailOptions{Projects: true})
	require.NoError(t, err, "a missing read:project scope never fails the details")

	assert.True(t, details.ProjectsRefused)
	assert.Equal(t, int32(1), withProjects.Load())
	assert.Equal(t, int32(1), without.Load(), "asked once more, without the projects")
	assert.Equal(t, 3, details.Items["I_fakeNodeIdIssue"].Comments)
	assert.Nil(t, details.Items["I_fakeNodeIdIssue"].Projects)
}

func TestFetchDetailsKeepsTheDataGitHubSendsWithARefusal(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)

		_, _ = io.WriteString(w, strings.Replace(plainIssue, `"data"`,
			`"errors":[{"type":"INSUFFICIENT_SCOPES","message":"read:project"}],"data"`, 1))
	})

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{"I_fakeNodeIdIssue"},
		DetailOptions{Projects: true})
	require.NoError(t, err)

	assert.True(t, details.ProjectsRefused)
	assert.Equal(t, int32(1), requests.Load(), "the data came with the refusal, so nothing is asked again")
	assert.Equal(t, 3, details.Items["I_fakeNodeIdIssue"].Comments)
}

func TestFetchDetailsFailsOnAScopeRefusalItDidNotAskFor(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)

		_, _ = io.WriteString(w, refusedProjects)
	})

	_, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{"I_fakeNodeIdIssue"},
		DetailOptions{})
	require.ErrorIs(t, err, ErrGraphQL)
	assert.Contains(t, err.Error(), "read:project")
	assert.Equal(t, int32(1), requests.Load(), "only a refusal of the projects is asked again")
}

func TestFetchDetailsFailsWhenTheSecondTryFails(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if asksForProjects(t, r) {
			_, _ = io.WriteString(w, refusedProjects)

			return
		}

		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"message":"bad gateway"}`)
	})

	_, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{"I_fakeNodeIdIssue"},
		DetailOptions{Projects: true})
	require.ErrorIs(t, err, ErrUnexpectedStatus)
}

func TestFetchItemReadsAnIssuesProjectsWhenAsked(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.True(t, asksForProjects(t, r))

		_, _ = io.WriteString(w, issueItem(projectsJSON))
	})

	update, err := client.FetchItem(t.Context(), NewToken(fakeToken), "I_fakeNodeIdIssue", DetailOptions{Projects: true})
	require.NoError(t, err)

	assert.False(t, update.ProjectsRefused)
	assert.Equal(t, []ProjectItem{{
		Title: "Roadmap", URL: "https://github.com/orgs/acme-org/projects/1", Status: "Todo", StatusColor: "#2da44e",
	}}, update.Details.Projects)
}

func TestFetchItemAsksAgainWithoutTheProjectsWhenTheScopeIsMissing(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)

		if asksForProjects(t, r) {
			_, _ = io.WriteString(w, refusedProjects)

			return
		}

		_, _ = io.WriteString(w, issueItem(""))
	})

	update, err := client.FetchItem(t.Context(), NewToken(fakeToken), "I_fakeNodeIdIssue", DetailOptions{Projects: true})
	require.NoError(t, err)

	assert.True(t, update.ProjectsRefused)
	assert.True(t, update.Found)
	assert.Nil(t, update.Details.Projects)
	assert.Equal(t, int32(2), requests.Load())
}

func TestProjectColors(t *testing.T) {
	t.Parallel()

	for color, want := range map[string]string{
		"GRAY": "#8c959f", "BLUE": "#218bff", "GREEN": "#2da44e", "YELLOW": "#bf8700",
		"ORANGE": "#e16f24", "RED": "#fa4549", "PINK": "#e85aad", "PURPLE": "#a475f9",
		"": "", "blue": "", "TEAL": "",
	} {
		assert.Equal(t, want, projectColor(color), color)
	}
}

// projectsJSON is an issue's projectItems in an item answer.
const projectsJSON = `,"projectItems":{"nodes":[{"project":{"title":"Roadmap",` +
	`"url":"https://github.com/orgs/acme-org/projects/1","closed":false},"status":{"name":"Todo","color":"GREEN"}}]}`

// issueItem is the item answer for one open issue, with projects spliced into its fields.
func issueItem(projects string) string {
	return fmt.Sprintf(`{"data":{"node":{
  "__typename":"Issue","id":"I_fakeNodeIdIssue","number":7,"title":"Fix it",
  "url":"https://github.com/acme-org/example-repo/issues/7",
  "createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z","issueState":"OPEN",
  "authorAssociation":"MEMBER","author":{"__typename":"User","login":"octo-user"},
  "repository":{"id":"R_fakeRepoId","name":"example-repo","owner":{"login":"acme-org"}},
  "comments":{"totalCount":2},"labels":{"nodes":[]}%s
},"rateLimit":{"cost":1,"limit":5000,"remaining":4990,"used":10,"resetAt":"2026-01-10T13:00:00Z"}}}`, projects)
}
