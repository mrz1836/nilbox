package github

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchCommitAuthorsReadsEachPullRequestsNewestCommits(t *testing.T) {
	t.Parallel()

	var asked [][]string

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		var req struct {
			Query     string `json:"query"`
			Variables struct {
				IDs []string `json:"ids"`
			} `json:"variables"`
		}

		assert.NoError(t, json.Unmarshal(raw, &req))
		assert.Contains(t, req.Query, "recentCommits: commits(last: 20) { nodes { commit { oid author { user { login } } } } }")
		assert.NotContains(t, req.Query, "authors(", "a commit's one author, never the list, which costs points")
		asked = append(asked, req.Variables.IDs)

		_, _ = io.WriteString(w, `{"data":{"nodes":[
  {"id":"PR_1","recentCommits":{"nodes":[
    {"commit":{"oid":"`+sha1s("1")+`","author":{"user":{"login":"octo-author"}}}},
    null,
    {"commit":{"oid":""}},
    {"commit":{"oid":"`+sha1s("2")+`","author":{"user":null}}},
    {"commit":{"oid":"`+sha1s("3")+`","author":{"user":{"login":"`+octoUser+`"}}}}
  ]}},
  null,
  {"id":"I_not_a_pull_request"}
],"rateLimit":{"cost":1,"remaining":4999,"resetAt":"2026-11-17T23:00:00Z","limit":5000,"used":1}},
"errors":[{"type":"NOT_FOUND","path":["nodes",1],"message":"Could not resolve to a node"}]}`)
	})

	got, err := client.FetchCommitAuthors(t.Context(), NewToken(fakeToken), []string{"PR_1", "PR_gone", "I_not_a_pull_request"})
	require.NoError(t, err)

	assert.Equal(t, map[string][]CommitAuthor{
		"PR_1": {{OID: sha1s("1"), Login: "octo-author"}, {OID: sha1s("2")}, {OID: sha1s("3"), Login: octoUser}},
	}, got, "oldest first, nulls and commits without a SHA skipped, an author matching no account with no login")
	assert.Equal(t, [][]string{{"PR_1", "PR_gone", "I_not_a_pull_request"}}, asked)
}

func TestFetchCommitAuthorsAsksInBatches(t *testing.T) {
	t.Parallel()

	var sizes []int

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		var req struct {
			Variables struct {
				IDs []string `json:"ids"`
			} `json:"variables"`
		}

		assert.NoError(t, json.Unmarshal(raw, &req))
		sizes = append(sizes, len(req.Variables.IDs))

		_, _ = io.WriteString(w, `{"data":{"nodes":[],"rateLimit":{"cost":1}}}`)
	})

	ids := make([]string, 0, commitAuthorsBatch+1)
	for i := range commitAuthorsBatch + 1 {
		ids = append(ids, "PR_"+strconv.Itoa(i))
	}

	got, err := client.FetchCommitAuthors(t.Context(), NewToken(fakeToken), ids)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.NotNil(t, got)
	assert.Equal(t, []int{commitAuthorsBatch, 1}, sizes)
}

func TestFetchCommitAuthorsFailsWithoutData(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, serveItem(`{"errors":[{"type":"FORBIDDEN","message":"no"}]}`))

	_, err := client.FetchCommitAuthors(t.Context(), NewToken(fakeToken), []string{"PR_1"})
	require.ErrorIs(t, err, ErrGraphQL)
}
