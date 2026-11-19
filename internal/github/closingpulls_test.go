package github

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// linkedIssue is the details answer for one issue with closing pull requests: an open one
// from a member, a merged draft from a fork in another repository by a bot, one closed
// without merging, one with no repository, a null, and one whose author is gone.
const linkedIssue = `{"data":{"nodes":[{
  "__typename":"Issue","id":"I_fakeNodeIdIssue","comments":{"totalCount":0},"labels":{"nodes":[]},
  "linkedPulls":{"nodes":[
    {"number":14,"title":"Fix the widget","url":"https://github.com/acme-org/example-repo/pull/14",
      "prState":"OPEN","isDraft":false,"isCrossRepository":false,"authorAssociation":"MEMBER",
      "author":{"__typename":"User","login":"octo-member"},"repository":{"nameWithOwner":"acme-org/example-repo"}},
    {"number":7,"title":"Bump the widget","url":"https://github.com/acme-org/other-repo/pull/7",
      "prState":"MERGED","isDraft":true,"isCrossRepository":true,"authorAssociation":"NONE",
      "author":{"__typename":"Bot","login":"helper-bot"},"repository":{"nameWithOwner":"acme-org/other-repo"}},
    {"number":15,"title":"Abandoned","url":"https://github.com/acme-org/example-repo/pull/15",
      "prState":"CLOSED","repository":{"nameWithOwner":"acme-org/example-repo"}},
    {"number":16,"title":"Nowhere","url":"","prState":"OPEN","repository":null},
    null,
    {"number":17,"title":"Ghost's fix","url":"https://github.com/acme-org/example-repo/pull/17",
      "prState":"OPEN","authorAssociation":"NONE","author":null,"repository":{"nameWithOwner":"acme-org/example-repo"}}
  ]}
}],"rateLimit":{"cost":1,"limit":5000,"remaining":4990,"used":10,"resetAt":"2026-01-10T13:00:00Z"}}}`

// linkedRepo is the issue's own repository in linkedIssue.
const linkedRepo = filesOwner + "/" + exampleRepo

func TestFetchDetailsReadsAnIssuesClosingPulls(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, readDetailsRequest(t, r).Query, "linkedPulls: closedByPullRequestsReferences(first: 5) {")

		_, _ = io.WriteString(w, linkedIssue)
	})

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{issueID}, DetailOptions{})
	require.NoError(t, err)

	assert.Equal(t, []ClosingPull{
		{
			Repo: linkedRepo, Number: 14, Title: "Fix the widget",
			URL: "https://github.com/acme-org/example-repo/pull/14", AuthorLogin: "octo-member",
			AuthorType: "User", AuthorAssociation: "MEMBER",
		},
		{
			Repo: "acme-org/other-repo", Number: 7, Title: "Bump the widget",
			URL: "https://github.com/acme-org/other-repo/pull/7", Merged: true, IsDraft: true,
			AuthorLogin: "helper-bot", AuthorType: "Bot", AuthorAssociation: "NONE", IsCrossRepository: true,
		},
		{
			Repo: linkedRepo, Number: 17, Title: "Ghost's fix",
			URL: "https://github.com/acme-org/example-repo/pull/17", AuthorAssociation: "NONE",
		},
	}, details.Items[issueID].ClosingPulls,
		"one closed without merging, one without a repository, and a null are left out, in GitHub's order")
}

func TestAnIssueWithNoClosingPullsReadsAsNone(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, plainIssue)
	})

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{issueID}, DetailOptions{})
	require.NoError(t, err)
	assert.Nil(t, details.Items[issueID].ClosingPulls)
}

// The issue sheet spreads the card's details on the node it reads, beside its own closing
// pull requests, which take other arguments: GitHub refuses two fields under one name with
// different arguments, so the card's are aliased apart.
func TestTheCardsClosingPullsNeverMeetTheIssueSheets(t *testing.T) {
	t.Parallel()

	for _, query := range []string{issueViewQuery, issueViewProjectsQuery} {
		assert.Contains(t, query, "fixes: closedByPullRequestsReferences(")
		assert.Contains(t, query, "linkedPulls: closedByPullRequestsReferences(")
		assert.Equal(t, 1, strings.Count(query, "linkedPulls:"))
	}

	for _, query := range []string{detailsQuery, detailsProjectsQuery, detailsRepoQuery, detailsRepoProjectsQuery} {
		assert.Equal(t, 1, strings.Count(query, "closedByPullRequestsReferences("), "only an issue reads them")
		assert.NotContains(t, query, "closingIssuesReferences", "a pull request's side would cost a point a batch")
	}
}
