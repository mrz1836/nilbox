package github

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plannedIssue is the details answer for one issue with a type, a milestone, a parent,
// issues it is blocked by and blocking, and a value in each kind of issue field, besides a
// null value, one without a field, and an empty one.
const plannedIssue = `{"data":{"nodes":[{
  "__typename":"Issue","id":"I_fakeNodeIdIssue","comments":{"totalCount":0},"labels":{"nodes":[]},
  "issueType":{"name":"Task","color":"YELLOW"},
  "milestone":{"title":"v1.2","url":"https://github.com/acme-org/example-repo/milestone/3",
    "dueOn":"2026-06-30T07:00:00Z","state":"OPEN","progressPercentage":42.5},
  "parent":{"__typename":"Issue","number":12,"title":"Ship the widget",
    "url":"https://github.com/acme-org/example-repo/issues/12","repository":{"nameWithOwner":"acme-org/example-repo"}},
  "issueDependenciesSummary":{"blockedBy":2,"blocking":1},
  "issueFieldValues":{"nodes":[
    {"__typename":"IssueFieldSingleSelectValue","name":"Low","color":"GREEN","field":{"name":"Priority"}},
    {"__typename":"IssueFieldMultiSelectValue","options":[{"name":"Web"},null,{"name":"API"}],"field":{"name":"Areas"}},
    {"__typename":"IssueFieldNumberValue","number":0.5,"field":{"name":"Estimate"}},
    {"__typename":"IssueFieldTextValue","text":"  the  trigger \n lands ","field":{"name":"Waiting on"}},
    {"__typename":"IssueFieldDateValue","date":"2026-06-27","field":{"name":"Target"}},
    {"__typename":"IssueFieldNumberValue","number":null,"field":{"name":"Points"}},
    {"__typename":"IssueFieldTextValue","text":"Orphan","field":null},
    {"__typename":"IssueFieldSomethingNew","field":{"name":"Future"}},
    null
  ]}
}],"rateLimit":{"cost":1,"limit":5000,"remaining":4990,"used":10,"resetAt":"2026-01-10T13:00:00Z"}}}`

func TestFetchDetailsReadsHowAnIssueIsPlanned(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query := readDetailsRequest(t, r).Query
		for _, field := range []string{"issueType {", "milestone {", "parent {", "issueDependenciesSummary {", "issueFieldValues("} {
			assert.Contains(t, query, field)
		}

		_, _ = io.WriteString(w, plannedIssue)
	})

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{issueID}, DetailOptions{})
	require.NoError(t, err)

	assert.Equal(t, IssueInfo{
		Type: IssueType{Name: "Task", Color: "#bf8700"},
		Fields: []IssueField{
			{Name: "Priority", Value: "Low", Color: "#2da44e"},
			{Name: "Areas", Value: "Web, API"},
			{Name: "Estimate", Value: "0.5"},
			{Name: "Waiting on", Value: "the trigger lands"},
			{Name: "Target", Value: "Jun 27, 2026"},
		},
		Milestone: &Milestone{
			Title: "v1.2", URL: "https://github.com/acme-org/example-repo/milestone/3",
			DueOn: time.Date(2026, 6, 30, 7, 0, 0, 0, time.UTC), State: "OPEN", Progress: 42.5,
		},
		Parent: &ItemRef{
			Repo: "acme-org/example-repo", Number: 12, Title: "Ship the widget",
			URL: "https://github.com/acme-org/example-repo/issues/12",
		},
		BlockedBy: 2,
		Blocking:  1,
	}, details.Items[issueID].Issue,
		"a value without a field or a value, and one of a kind this build doesn't read, are left out")
}

func TestAnIssueWithNoPlanningReadsAsNone(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, plainIssue)
	})

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{issueID}, DetailOptions{})
	require.NoError(t, err)

	assert.Equal(t, IssueInfo{}, details.Items[issueID].Issue)
}

func TestIssuePlanningKeepsInBounds(t *testing.T) {
	t.Parallel()

	n := &detailsNode{
		IssueType:                &graphQLIssueType{Name: "", Color: "RED"},
		IssueDependenciesSummary: graphQLDependencies{BlockedBy: -1, Blocking: -3},
		Parent:                   &graphQLRef{TypeName: typeIssue, Number: 4},
	}

	assert.Equal(t, IssueInfo{}, n.issueInfo(),
		"an unnamed type, negative counts, and a parent without its repository read as none")
}

func TestAnIssueFieldsTextIsCut(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("word ", maxFieldRunes)
	f := graphQLIssueFields{Nodes: []*graphQLIssueFieldValue{
		{TypeName: "IssueFieldTextValue", Text: long, Field: &struct {
			Name string `json:"name"`
		}{Name: "Notes"}},
	}}

	fields := f.fields()
	require.Len(t, fields, 1)
	assert.LessOrEqual(t, len([]rune(fields[0].Value)), maxFieldRunes)
}
