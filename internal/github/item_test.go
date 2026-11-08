package github

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// itemRequest is the body FetchItem sends.
type itemRequest struct {
	Query     string `json:"query"`
	Variables struct {
		ID string `json:"id"`
	} `json:"variables"`
}

// serveItem answers every request with body.
func serveItem(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}
}

func TestFetchItemReadsTheItemAndItsDetails(t *testing.T) {
	t.Parallel()

	body := fixture(t, "item_response.json")

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/graphql", r.URL.Path)
		assert.Equal(t, "Bearer "+fakeToken, r.Header.Get("Authorization"))

		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		var req itemRequest
		assert.NoError(t, json.Unmarshal(raw, &req))
		assert.Contains(t, req.Query, "node(id: $id) { ...ItemFields ...DetailFields }")
		assert.Contains(t, req.Query, "fragment ItemFields on SearchResultItem")
		assert.Contains(t, req.Query, "fragment RepoFields on Repository")
		assert.Contains(t, req.Query, "fragment DetailFields on Node")
		assert.Equal(t, "PR_fakeNodeId12", req.Variables.ID)

		_, _ = w.Write(body)
	})

	update, err := client.FetchItem(t.Context(), NewToken(fakeToken), "PR_fakeNodeId12", DetailOptions{})
	require.NoError(t, err)
	require.True(t, update.Found)

	it := update.Item
	assert.True(t, it.IsPR)
	assert.Equal(t, "PR_fakeNodeId12", it.NodeID)
	assert.Equal(t, 12, it.Number)
	assert.Equal(t, "Add a dark mode toggle", it.Title)
	assert.Equal(t, StateMerged, it.State)
	assert.True(t, it.Closed())
	assert.Equal(t, time.Date(2026, 1, 10, 12, 30, 0, 0, time.UTC), it.UpdatedAt)
	assert.Equal(t, "patch-1", it.HeadRef)
	assert.Equal(t, "fork-contributor", it.AuthorLogin)
	assert.Equal(t, "User", it.AuthorType)
	assert.True(t, it.IsCrossRepository)
	assert.Equal(t, "acme-org", it.RepoOwner)
	assert.Equal(t, "example-repo", it.RepoName)
	assert.Equal(t, "R_fakeRepoExample", it.Repo.NodeID)
	assert.Equal(t, "#00add8", it.Repo.LanguageColor)

	assert.Equal(t, ItemDetails{
		Labels:         []Label{{Name: "enhancement", Color: "#a2eeef"}},
		Comments:       4,
		Additions:      120,
		Deletions:      30,
		ChangedFiles:   6,
		Commits:        3,
		HeadOID:        "1111111111111111111111111111111111111111",
		Mergeable:      "UNKNOWN",
		Checks:         Checks{State: "SUCCESS", Passed: 9},
		Reviews:        Reviews{Decision: "APPROVED", Reviewers: 1, Reviewed: 1, Approvals: 1},
		ReviewRequests: noRequests(),
	}, update.Details)

	assert.Equal(t, QueryRate{
		Cost: 1, Limit: 5000, Remaining: 4970, Used: 30, ResetAt: time.Date(2026, 1, 10, 13, 0, 0, 0, time.UTC),
	}, update.RateLimit)
}

func TestFetchItemReadsAnIssue(t *testing.T) {
	t.Parallel()

	body := `{"data":{"node":{"__typename":"Issue","id":"I_fakeNodeId7","number":7,"title":"Fix the docs",
		"issueState":"OPEN","author":{"__typename":"Bot","login":"helper-bot"},
		"repository":{"name":"docs-repo","owner":{"login":"acme-org"}},
		"comments":{"totalCount":2},"labels":{"nodes":[{"name":"bug","color":"d73a4a"}]}},
		"rateLimit":{"cost":1,"limit":5000,"remaining":4999,"used":1,"resetAt":"2026-01-10T13:00:00Z"}}}`

	update, err := newTestClient(t, serveItem(body)).FetchItem(t.Context(), NewToken(fakeToken), "I_fakeNodeId7", DetailOptions{})
	require.NoError(t, err)
	require.True(t, update.Found)

	assert.False(t, update.Item.IsPR)
	assert.Equal(t, StateOpen, update.Item.State)
	assert.False(t, update.Item.Closed())
	assert.Equal(t, "Bot", update.Item.AuthorType)
	assert.Equal(t, ItemDetails{Labels: []Label{{Name: "bug", Color: "#d73a4a"}}, Comments: 2}, update.Details)
}

func TestFetchItemThatIsGone(t *testing.T) {
	t.Parallel()

	// What GitHub answers for an ID it cannot resolve.
	body := `{"data":{"node":null,"rateLimit":{"cost":1,"limit":5000,"remaining":4946,"used":54,
		"resetAt":"2026-01-10T13:00:00Z"}},"errors":[{"type":"NOT_FOUND","path":["node"],
		"message":"Could not resolve to a node with the global id of 'PR_fakeNodeIdGone'"}]}`

	update, err := newTestClient(t, serveItem(body)).FetchItem(t.Context(), NewToken(fakeToken), "PR_fakeNodeIdGone", DetailOptions{})
	require.NoError(t, err)

	assert.False(t, update.Found)
	assert.Equal(t, Item{}, update.Item)
	assert.Equal(t, 4946, update.RateLimit.Remaining, "the budget reading still counts")
}

func TestFetchItemFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{name: "server error", status: http.StatusBadGateway, body: `{"message":"bad gateway"}`, wantErr: ErrUnexpectedStatus},
		{name: "errors without data", status: http.StatusOK, body: `{"errors":[{"message":"timeout"}]}`, wantErr: ErrGraphQL},
		{
			name:    "a null node for another reason",
			status:  http.StatusOK,
			body:    `{"data":{"node":null},"errors":[{"type":"FORBIDDEN","message":"not now"}]}`,
			wantErr: ErrGraphQL,
		},
		{name: "neither data nor errors", status: http.StatusOK, body: `{}`, wantErr: ErrMalformedResponse},
		{name: "not json", status: http.StatusOK, body: `<html>`, wantErr: ErrMalformedResponse},
		{
			name:    "not an item",
			status:  http.StatusOK,
			body:    `{"data":{"node":{"__typename":"Repository","id":"R_fakeRepoExample"}}}`,
			wantErr: ErrMalformedResponse,
		},
		{
			name:    "an item without an ID",
			status:  http.StatusOK,
			body:    `{"data":{"node":{"__typename":"Issue","number":7,"repository":{"name":"docs-repo"}}}}`,
			wantErr: ErrMalformedResponse,
		},
		{
			name:    "an item that is not an object",
			status:  http.StatusOK,
			body:    `{"data":{"node":[1,2]}}`,
			wantErr: ErrMalformedResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			})

			_, err := client.FetchItem(t.Context(), NewToken(fakeToken), "I_fakeNodeId1", DetailOptions{})
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestFetchInboxReadsEachItemsState(t *testing.T) {
	t.Parallel()

	body := `{"data":{"viewer":{"login":"octo-user"},
	"reviewRequested":{"issueCount":2,"nodes":[
		{"__typename":"PullRequest","number":1,"prState":"MERGED","repository":{"name":"example-repo","owner":{"login":"acme-org"}}},
		{"__typename":"PullRequest","number":2,"prState":"OPEN","repository":{"name":"example-repo","owner":{"login":"acme-org"}}}
	]},
	"authored":{"issueCount":0,"nodes":[]},
	"assigned":{"issueCount":2,"nodes":[
		{"__typename":"Issue","number":3,"issueState":"CLOSED","repository":{"name":"example-repo","owner":{"login":"acme-org"}}},
		{"__typename":"Issue","number":4,"repository":{"name":"example-repo","owner":{"login":"acme-org"}}}
	]},
	"rateLimit":{"cost":1,"limit":5000,"remaining":4999,"used":1,"resetAt":"2026-01-06T13:00:00Z"}}}`

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Contains(t, string(raw), "prState: state")
		assert.Contains(t, string(raw), "issueState: state")

		_, _ = io.WriteString(w, body)
	})

	inbox, err := client.FetchInbox(t.Context(), NewToken(fakeToken))
	require.NoError(t, err)

	items := byNumber(inbox)
	assert.Equal(t, StateMerged, items[1].State)
	assert.True(t, items[1].Closed())
	assert.Equal(t, StateOpen, items[2].State)
	assert.False(t, items[2].Closed())
	assert.Equal(t, StateClosed, items[3].State)
	assert.True(t, items[3].Closed())
	assert.Empty(t, items[4].State)
	assert.False(t, items[4].Closed(), "an item whose state is unknown is not closed")
}
