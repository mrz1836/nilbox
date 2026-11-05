package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
)

// testUserAgent is the user agent test clients send.
const testUserAgent = "test-agent/0.0.1"

// fixture reads a recorded response from testdata.
func fixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // fixed fixture names
	require.NoError(t, err)

	return data
}

// newTestClient returns a client pointed at a local server running handler.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &Client{BaseURL: srv.URL, HTTP: srv.Client(), UserAgent: testUserAgent}
}

// serveFixture answers every request with a recorded body and the given scopes header.
func serveFixture(t *testing.T, name, scopes string) http.HandlerFunc {
	t.Helper()

	body := fixture(t, name)

	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-OAuth-Scopes", scopes)
		_, _ = w.Write(body)
	}
}

// fetchFixtureInbox runs FetchInbox against the recorded inbox response.
func fetchFixtureInbox(t *testing.T) *Inbox {
	t.Helper()

	inbox, err := newTestClient(t, serveFixture(t, "inbox_response.json", "repo, read:org")).
		FetchInbox(t.Context(), NewToken(fakeToken))
	require.NoError(t, err)

	return inbox
}

// byNumber indexes every item in the inbox by number.
func byNumber(inbox *Inbox) map[int]Item {
	items := make(map[int]Item)

	for _, list := range []SearchResult{inbox.ReviewRequested, inbox.Authored, inbox.Assigned} {
		for _, item := range list.Items {
			items[item.Number] = item
		}
	}

	return items
}

func TestFetchInboxSingleBatchedRequest(t *testing.T) {
	var requests atomic.Int32

	body := fixture(t, "inbox_response.json")

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)

		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/graphql", r.URL.Path)
		assert.Equal(t, "Bearer "+fakeToken, r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, testUserAgent, r.Header.Get("User-Agent"))

		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		var req struct {
			Query string `json:"query"`
		}

		assert.NoError(t, json.Unmarshal(raw, &req))

		for _, part := range []string{
			`reviewRequested: search(query: "is:pr is:open review-requested:@me"`,
			`authored: search(query: "is:pr is:open author:@me"`,
			`assigned: search(query: "is:open assignee:@me"`,
			"rateLimit { cost remaining resetAt limit used }",
			"authorAssociation",
			"isCrossRepository",
		} {
			assert.Contains(t, req.Query, part)
		}

		w.Header().Set("X-OAuth-Scopes", "repo, read:org")
		_, _ = w.Write(body)
	})

	inbox, err := client.FetchInbox(t.Context(), NewToken(fakeToken))
	require.NoError(t, err)

	assert.Equal(t, int32(1), requests.Load(), "the whole inbox is one request")
	assert.Equal(t, "octo-user", inbox.Viewer)
	assert.Equal(t, "repo, read:org", inbox.OAuthScopes)

	assert.Equal(t, 5, inbox.ReviewRequested.IssueCount)
	assert.Len(t, inbox.ReviewRequested.Items, 3)
	assert.Equal(t, 2, inbox.Authored.IssueCount)
	assert.Len(t, inbox.Authored.Items, 2)
	assert.Equal(t, 2, inbox.Assigned.IssueCount)
	assert.Len(t, inbox.Assigned.Items, 2, "null nodes are skipped")

	assert.Equal(t, QueryRate{
		Cost: 1, Limit: 5000, Remaining: 4999, Used: 1,
		ResetAt: time.Date(2026, 1, 6, 13, 0, 0, 0, time.UTC),
	}, inbox.RateLimit)
}

func TestFetchInboxParsesTrustFields(t *testing.T) {
	items := byNumber(fetchFixtureInbox(t))

	fork := items[12]
	assert.True(t, fork.IsPR)
	assert.Equal(t, "CONTRIBUTOR", fork.AuthorAssociation)
	assert.True(t, fork.IsCrossRepository)
	assert.Equal(t, "fork-contributor", fork.AuthorLogin)

	bot := items[15]
	assert.Equal(t, "NONE", bot.AuthorAssociation)
	assert.Equal(t, "dependabot[bot]", bot.AuthorLogin)
	assert.False(t, bot.IsCrossRepository)

	firstTimer := items[18]
	assert.Equal(t, "FIRST_TIME_CONTRIBUTOR", firstTimer.AuthorAssociation)
	assert.True(t, firstTimer.IsCrossRepository)

	own := items[7]
	assert.Equal(t, Item{
		IsPR:              true,
		NodeID:            "PR_fakeNodeId7",
		Number:            7,
		Title:             "Make the inbox refresh faster",
		URL:               "https://github.com/acme-org/example-repo/pull/7",
		CreatedAt:         time.Date(2026, 1, 2, 8, 0, 0, 0, time.UTC),
		UpdatedAt:         time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC),
		IsDraft:           true,
		HeadRef:           "feat/faster-refresh",
		AuthorLogin:       "octo-user",
		AuthorAssociation: "OWNER",
		RepoOwner:         "acme-org",
		RepoName:          "example-repo",
	}, own)

	issue := items[21]
	assert.False(t, issue.IsPR)
	assert.Equal(t, "MEMBER", issue.AuthorAssociation)
	assert.False(t, issue.IsCrossRepository)
	assert.Equal(t, "other-repo", issue.RepoName)
}

func TestFetchInboxGraphQLErrors(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":null,"errors":[{"message":"Something went wrong"},{"message":"Try again"}]}`))
	})

	inbox, err := client.FetchInbox(t.Context(), NewToken(fakeToken))
	require.ErrorIs(t, err, ErrGraphQL)
	assert.Nil(t, inbox)
	assert.Contains(t, err.Error(), "Something went wrong; Try again")
}

func TestFetchInboxStatusErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantMsg []string
	}{
		{
			name: "rejected token", status: http.StatusUnauthorized,
			body:    `{"message":"Bad credentials"}`,
			wantMsg: []string{"HTTP 401", "Bad credentials", "gh auth login"},
		},
		{
			name: "rate limited", status: http.StatusForbidden,
			body:    `{"message":"API rate limit exceeded"}`,
			wantMsg: []string{"HTTP 403", "API rate limit exceeded"},
		},
		{name: "html error page", status: http.StatusBadGateway, body: "<html>bad gateway</html>", wantMsg: []string{"HTTP 502"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})

			_, err := client.FetchInbox(t.Context(), NewToken(fakeToken))
			require.ErrorIs(t, err, ErrUnexpectedStatus)

			var status *StatusError
			require.ErrorAs(t, err, &status)
			assert.Equal(t, tc.status, status.StatusCode)

			for _, want := range tc.wantMsg {
				assert.Contains(t, err.Error(), want)
			}

			assert.NotContains(t, err.Error(), "<html>")
		})
	}
}

func TestFetchInboxMalformedResponses(t *testing.T) {
	cases := map[string]string{
		"not json": "this is not json",
		"no data":  `{}`,
		"too big":  `{"data":{"viewer":{"login":"` + strings.Repeat("a", maxResponseBytes) + `"}}}`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			})

			_, err := client.FetchInbox(t.Context(), NewToken(fakeToken))
			require.ErrorIs(t, err, ErrMalformedResponse)
		})
	}
}

func TestFetchInboxSkipsOtherNodes(t *testing.T) {
	body := `{"data":{"viewer":{"login":"octo-user"},
	"reviewRequested":{"issueCount":4,"nodes":[
		{"__typename":"Repository"},
		{"__typename":"PullRequest","number":0,"repository":{"name":"example-repo","owner":{"login":"acme-org"}}},
		{"__typename":"Issue","number":9},
		{"__typename":"Issue","number":10,"author":null,"isCrossRepository":true,"isDraft":true,"headRefName":"x",
		 "repository":{"name":"example-repo","owner":{"login":"acme-org"}}}
	]},
	"authored":{"issueCount":0,"nodes":[]},
	"assigned":{"issueCount":0,"nodes":null},
	"rateLimit":{"cost":1,"limit":5000,"remaining":4999,"used":1,"resetAt":"2026-01-06T13:00:00Z"}}}`

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})

	inbox, err := client.FetchInbox(t.Context(), NewToken(fakeToken))
	require.NoError(t, err)
	require.Len(t, inbox.ReviewRequested.Items, 1)

	issue := inbox.ReviewRequested.Items[0]
	assert.Equal(t, 10, issue.Number)
	assert.Empty(t, issue.AuthorLogin, "a deleted author has no login")
	assert.False(t, issue.IsCrossRepository, "only pull requests carry the cross-repository flag")
	assert.False(t, issue.IsDraft)
	assert.Empty(t, issue.HeadRef)
	assert.NotNil(t, inbox.Authored.Items)
	assert.NotNil(t, inbox.Assigned.Items)
	assert.Empty(t, inbox.OAuthScopes, "a token without a scopes header")
}

func TestFetchInboxRequiresToken(t *testing.T) {
	var requests atomic.Int32

	client := newTestClient(t, func(http.ResponseWriter, *http.Request) { requests.Add(1) })

	_, err := client.FetchInbox(t.Context(), Token{})
	require.ErrorIs(t, err, ErrNotLoggedIn)
	assert.Zero(t, requests.Load())
}

func TestFetchInboxTransportErrors(t *testing.T) {
	t.Run("server unreachable", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		client := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
		srv.Close()

		_, err := client.FetchInbox(t.Context(), NewToken(fakeToken))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "call the GitHub API")
	})

	t.Run("canceled", func(t *testing.T) {
		client := newTestClient(t, serveFixture(t, "inbox_response.json", ""))

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := client.FetchInbox(ctx, NewToken(fakeToken))
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("bad base URL", func(t *testing.T) {
		client := &Client{BaseURL: "http://bad host"}

		_, err := client.FetchInbox(t.Context(), NewToken(fakeToken))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "build the GitHub API request")
	})
}

func TestNewClientDefaults(t *testing.T) {
	client := NewClient("1.2.3")

	assert.Equal(t, DefaultBaseURL, client.BaseURL)
	assert.Equal(t, brand.Slug+"/1.2.3", client.UserAgent)
	require.NotNil(t, client.HTTP)
	assert.Equal(t, defaultTimeout, client.HTTP.Timeout)

	assert.Equal(t, brand.Slug+"/dev", NewClient("").UserAgent)
}

func TestClientFallbacks(t *testing.T) {
	assert.Equal(t, DefaultBaseURL, (&Client{}).baseURL())
	assert.Equal(t, "http://127.0.0.1:9", (&Client{BaseURL: "http://127.0.0.1:9/"}).baseURL())
	assert.Equal(t, defaultTimeout, (&Client{}).httpClient().Timeout)

	// A client without an HTTP client still works against the local server.
	srv := httptest.NewServer(serveFixture(t, "inbox_response.json", "repo, read:org"))
	t.Cleanup(srv.Close)

	inbox, err := (&Client{BaseURL: srv.URL + "/"}).FetchInbox(t.Context(), NewToken(fakeToken))
	require.NoError(t, err)
	assert.Equal(t, "octo-user", inbox.Viewer)
}

func TestStatusErrorMessages(t *testing.T) {
	assert.Equal(t, "the GitHub API returned HTTP 500", (&StatusError{StatusCode: 500}).Error())
	assert.Equal(t, "the GitHub API returned HTTP 401: Bad credentials (run gh auth login)",
		(&StatusError{StatusCode: 401, Message: "Bad credentials"}).Error())
}

func TestTruncateCapsMessages(t *testing.T) {
	long := strings.Repeat("word ", 200)

	got := truncate(long)
	assert.Len(t, []rune(got), maxMessageRunes+1)
	assert.True(t, strings.HasSuffix(got, "…"))
	assert.Equal(t, "a b", truncate(" a \n\t b "))
	assert.Empty(t, apiMessage([]byte("not json")))
	assert.Equal(t, "Not Found", apiMessage([]byte(fmt.Sprintf(`{"message":%q}`, "Not Found"))))
}
