package github

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// issueID is the node ID of the issue the project tests ask about.
const issueID = "I_fakeNodeIdIssue"

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

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{issueID},
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
	}, details.Items[issueID].Projects,
		"a closed project, one the token cannot see, and a null item are left out; an unknown color is none")
}

func TestFetchDetailsLeavesTheProjectsOutUnlessAsked(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.False(t, asksForProjects(t, r))

		_, _ = io.WriteString(w, plainIssue)
	})

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{issueID},
		DetailOptions{})
	require.NoError(t, err)

	assert.False(t, details.ProjectsRefused)
	assert.Nil(t, details.Items[issueID].Projects)
	assert.Equal(t, 3, details.Items[issueID].Comments)
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

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{issueID},
		DetailOptions{Projects: true})
	require.NoError(t, err, "a missing read:project scope never fails the details")

	assert.True(t, details.ProjectsRefused)
	assert.Equal(t, int32(1), withProjects.Load())
	assert.Equal(t, int32(1), without.Load(), "asked once more, without the projects")
	assert.Equal(t, 3, details.Items[issueID].Comments)
	assert.Nil(t, details.Items[issueID].Projects)
}

// repoIssueNode is the details answer's node for the issue with the node ID it is formatted
// with, read with its repository whole.
const repoIssueNode = `{"__typename":"Issue","id":%q,"comments":{"totalCount":3},"labels":{"nodes":[]},
  "repository":{"id":"R_a","name":"example-repo","owner":{"login":"acme-org"},
    "defaultBranchRef":{"name":"main"},"viewerPermission":"WRITE"}}`

// repoIssues is the details answer for the issues with the given node IDs, each read with
// its repository whole.
func repoIssues(ids ...string) string {
	nodes := make([]string, 0, len(ids))
	for _, id := range ids {
		nodes = append(nodes, fmt.Sprintf(repoIssueNode, id))
	}

	return `{"data":{"nodes":[` + strings.Join(nodes, ",") +
		`],"rateLimit":{"cost":1,"limit":5000,"remaining":4990,"used":10,"resetAt":"2026-01-10T13:00:00Z"}}}`
}

// detailsQueryName names which of the details queries query is, so a failure reads well.
func detailsQueryName(query string) string {
	switch query {
	case detailsQuery:
		return "plain"
	case detailsProjectsQuery:
		return "projects"
	case detailsRepoQuery:
		return "repository"
	case detailsRepoProjectsQuery:
		return "repository and projects"
	default:
		return "unknown"
	}
}

// askedIDs returns the node IDs a details request asks about.
func askedIDs(req graphQLRequest) []string {
	asked, _ := req.Variables["ids"].([]any)

	ids := make([]string, 0, len(asked))
	for _, id := range asked {
		s, _ := id.(string)
		ids = append(ids, s)
	}

	return ids
}

func TestFetchDetailsAsksAgainForTheRepositoryWithoutTheProjects(t *testing.T) {
	t.Parallel()

	heard := &heardRequests{}
	client := newTestClient(t, refusingProjects(t, heard, func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, repoIssues(issueID))
	}))

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{issueID},
		DetailOptions{Projects: true, Repository: true})
	require.NoError(t, err, "a missing read:project scope never fails the details")

	assert.True(t, details.ProjectsRefused)
	assert.Equal(t, 1, details.Requests)

	requests := heard.all()
	require.Len(t, requests, 2, "asked once more")
	assert.Equal(t, "repository and projects", detailsQueryName(requests[0].Query))
	assert.Equal(t, "repository", detailsQueryName(requests[1].Query),
		"the second request leaves the projects out, and still reads the repository")

	got := details.Items[issueID]
	assert.Equal(t, 3, got.Comments)
	assert.Nil(t, got.Projects)
	require.NotNil(t, got.Repo, "the repository asked for comes back")
	assert.Equal(t, "acme-org", got.Repo.Owner)
	assert.Equal(t, "example-repo", got.Repo.Name)
	assert.Equal(t, "main", got.Repo.Repo.DefaultBranch)
}

func TestFetchDetailsAsksAgainBatchByBatchWithoutTheProjects(t *testing.T) {
	t.Parallel()

	// failing is the ID whose batch's second try fails.
	const failing = "I_fakeNodeId3"

	ids := []string{"I_fakeNodeId1", "I_fakeNodeId2", failing}
	heard := &heardRequests{}

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		req := heard.hear(t, r)
		asked := askedIDs(req)

		switch {
		case readsProjects(req):
			_, _ = io.WriteString(w, refusedProjects)
		case slices.Contains(asked, failing):
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{"message":"bad gateway"}`)
		default:
			_, _ = io.WriteString(w, repoIssues(asked...))
		}
	})

	var told atomic.Int64

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), ids, DetailOptions{
		Projects: true, Repository: true, Batch: 2, Partial: true,
		Progress: func(n int) { told.Add(int64(n)) },
	})
	require.ErrorIs(t, err, ErrUnexpectedStatus, "the batch whose second try failed")
	require.NotNil(t, details, "with Partial, what the other batch read is kept")

	assert.True(t, details.ProjectsRefused)
	assert.Equal(t, 1, details.Requests)
	assert.Equal(t, int64(2), told.Load(), "progress is told of the batch read, once it is")
	require.Len(t, details.Items, 2)

	for _, id := range ids[:2] {
		require.Contains(t, details.Items, id)
		assert.NotNil(t, details.Items[id].Repo, "%s keeps its repository", id)
	}

	// ask is one request: which query, and the IDs it asked about.
	type ask struct {
		query string
		ids   string
	}

	requests := heard.all()
	asks := make([]ask, 0, len(requests))

	for _, req := range requests {
		asks = append(asks, ask{query: detailsQueryName(req.Query), ids: strings.Join(askedIDs(req), " ")})
	}

	assert.ElementsMatch(t, []ask{
		{"repository and projects", "I_fakeNodeId1 I_fakeNodeId2"},
		{"repository", "I_fakeNodeId1 I_fakeNodeId2"},
		{"repository and projects", failing},
		{"repository", failing},
	}, asks, "each batch, of the size asked for, is asked again without the projects and with the repository")
}

func TestFetchDetailsKeepsTheDataGitHubSendsWithARefusal(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)

		_, _ = io.WriteString(w, strings.Replace(plainIssue, `"data"`,
			`"errors":[{"type":"INSUFFICIENT_SCOPES","message":"read:project"}],"data"`, 1))
	})

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{issueID},
		DetailOptions{Projects: true})
	require.NoError(t, err)

	assert.True(t, details.ProjectsRefused)
	assert.Equal(t, int32(1), requests.Load(), "the data came with the refusal, so nothing is asked again")
	assert.Equal(t, 3, details.Items[issueID].Comments)
}

func TestFetchDetailsFailsOnAScopeRefusalItDidNotAskFor(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)

		_, _ = io.WriteString(w, refusedProjects)
	})

	_, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{issueID},
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

	_, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{issueID},
		DetailOptions{Projects: true})
	require.ErrorIs(t, err, ErrUnexpectedStatus)
}

func TestFetchItemReadsAnIssuesProjectsWhenAsked(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.True(t, asksForProjects(t, r))

		_, _ = io.WriteString(w, issueItem(projectsJSON))
	})

	update, err := client.FetchItem(t.Context(), NewToken(fakeToken), issueID, DetailOptions{Projects: true})
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

	update, err := client.FetchItem(t.Context(), NewToken(fakeToken), issueID, DetailOptions{Projects: true})
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

// issueWithFields is the details answer for one issue in a project with a value in each
// kind of field, GitHub's built-in fields, and sub-issues.
const issueWithFields = `{"data":{"nodes":[{
  "__typename":"Issue","id":"I_fakeNodeIdIssue","comments":{"totalCount":0},"labels":{"nodes":[]},
  "subIssuesSummary":{"total":8,"completed":6},
  "projectItems":{"nodes":[{
    "project":{"title":"Roadmap","url":"https://github.com/orgs/acme-org/projects/1","closed":false},
    "status":{"name":"In progress","color":"YELLOW"},
    "fieldValues":{"nodes":[
      {"__typename":"ProjectV2ItemFieldRepositoryValue"},
      {"__typename":"ProjectV2ItemFieldTextValue","text":"Fix it","field":{"name":"Title","dataType":"TITLE"}},
      {"__typename":"ProjectV2ItemFieldSingleSelectValue","name":"In progress","color":"YELLOW",
       "field":{"name":"Status","dataType":"SINGLE_SELECT"}},
      {"__typename":"ProjectV2ItemFieldSingleSelectValue","name":"High","color":"RED",
       "field":{"name":"Priority","dataType":"SINGLE_SELECT"}},
      {"__typename":"ProjectV2ItemFieldSingleSelectValue","name":"XL","color":"MAUVE",
       "field":{"name":"Size","dataType":"SINGLE_SELECT"}},
      {"__typename":"ProjectV2ItemFieldNumberValue","number":2.5,"field":{"name":"Estimate","dataType":"NUMBER"}},
      {"__typename":"ProjectV2ItemFieldNumberValue","number":null,"field":{"name":"Points","dataType":"NUMBER"}},
      {"__typename":"ProjectV2ItemFieldTextValue","text":"  Design\n  team ","field":{"name":"Waiting on","dataType":"TEXT"}},
      {"__typename":"ProjectV2ItemFieldDateValue","date":"2026-06-27","field":{"name":"Target","dataType":"DATE"}},
      {"__typename":"ProjectV2ItemFieldDateValue","date":"someday","field":{"name":"Start","dataType":"DATE"}},
      {"__typename":"ProjectV2ItemFieldIterationValue","title":"Sprint 4","field":{"name":"Iteration","dataType":"ITERATION"}},
      {"__typename":"ProjectV2ItemFieldSingleSelectValue","name":"Beta","color":"BLUE","field":null},
      {"__typename":"ProjectV2ItemFieldTextValue","text":"","field":{"name":"Notes","dataType":"TEXT"}},
      null
    ]}
  }]}
}],"rateLimit":{"cost":4,"limit":5000,"remaining":4990,"used":10,"resetAt":"2026-01-10T13:00:00Z"}}}`

func TestFetchDetailsReadsAnIssuesProjectFieldsAndSubIssues(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query := readDetailsRequest(t, r).Query
		assert.Contains(t, query, "fieldValues(first: 20)")
		assert.Contains(t, query, "fragment ProjectFieldName on ProjectV2FieldConfiguration")
		assert.Contains(t, query, "subIssuesSummary { total completed }")

		_, _ = io.WriteString(w, issueWithFields)
	})

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{issueID},
		DetailOptions{Projects: true})
	require.NoError(t, err)

	issue := details.Items[issueID]
	assert.Equal(t, SubIssues{Total: 8, Completed: 6}, issue.SubIssues)
	require.Len(t, issue.Projects, 1)
	assert.Equal(t, []ProjectField{
		{Name: "Priority", Value: "High", Color: "#fa4549"},
		{Name: "Size", Value: "XL"},
		{Name: "Estimate", Value: "2.5"},
		{Name: "Waiting on", Value: "Design team"},
		{Name: "Target", Value: "Jun 27, 2026"},
		{Name: "Start", Value: "someday"},
		{Name: "Iteration", Value: "Sprint 4"},
	}, issue.Projects[0].Fields, "the title, Status, built-in, empty, and fieldless values are left out")
}

func TestTheSubIssuesQueryNeedsNoProjects(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query := readDetailsRequest(t, r).Query
		assert.Contains(t, query, "subIssuesSummary { total completed }")
		assert.NotContains(t, query, "ProjectFieldName")

		_, _ = io.WriteString(w, strings.Replace(plainIssue, `"labels":{"nodes":[]}`,
			`"labels":{"nodes":[]},"subIssuesSummary":{"total":3,"completed":9}`, 1))
	})

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{issueID},
		DetailOptions{})
	require.NoError(t, err)
	assert.Equal(t, SubIssues{Total: 3, Completed: 3}, details.Items[issueID].SubIssues,
		"completed never passes the total")
}

func TestSubIssueCountsStayInBounds(t *testing.T) {
	t.Parallel()

	assert.Equal(t, SubIssues{}, graphQLSubIssues{Total: -1, Completed: -4}.subIssues())
	assert.Equal(t, SubIssues{Total: 2, Completed: 0}, graphQLSubIssues{Total: 2, Completed: -1}.subIssues())
}

func TestALongTextFieldIsCut(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("word ", 60)
	field, ok := (&graphQLFieldValue{
		TypeName: "ProjectV2ItemFieldTextValue", Text: long,
		Field: &struct {
			Name     string `json:"name"`
			DataType string `json:"dataType"`
		}{Name: "Notes", DataType: "TEXT"},
	}).field()

	require.True(t, ok)
	assert.LessOrEqual(t, len([]rune(field.Value)), maxFieldRunes)
}
