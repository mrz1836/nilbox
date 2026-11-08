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
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/uierr"
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
	t.Parallel()

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
			`reviewRequested: search(query: "is:pr is:open archived:false review-requested:@me"`,
			`authored: search(query: "is:pr is:open archived:false author:@me"`,
			`assigned: search(query: "is:open archived:false assignee:@me"`,
			"repository { ...RepoFields }",
			"fragment RepoFields on Repository",
			"rateLimit { cost remaining resetAt limit used }",
			"viewer { login name url avatarUrl(size: 128) }",
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
	assert.Equal(t, Viewer{
		Login:     "octo-user",
		Name:      "Octo User",
		URL:       "https://github.com/octo-user",
		AvatarURL: "https://avatars.githubusercontent.com/u/101?v=4&s=128",
	}, inbox.Viewer)
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
	t.Parallel()

	items := byNumber(fetchFixtureInbox(t))

	fork := items[12]
	assert.True(t, fork.IsPR)
	assert.Equal(t, "CONTRIBUTOR", fork.AuthorAssociation)
	assert.True(t, fork.IsCrossRepository)
	assert.Equal(t, "fork-contributor", fork.AuthorLogin)

	assert.Equal(t, "User", fork.AuthorType)

	bot := items[15]
	assert.Equal(t, "NONE", bot.AuthorAssociation)
	assert.Equal(t, "dependabot", bot.AuthorLogin, "GraphQL names a bot by its app's slug")
	assert.Equal(t, "Bot", bot.AuthorType)
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
		AuthorType:        "User",
		AuthorAssociation: "OWNER",
		RepoOwner:         "acme-org",
		RepoName:          "example-repo",
		Repo:              own.Repo,
	}, own)

	issue := items[21]
	assert.False(t, issue.IsPR)
	assert.Equal(t, "MEMBER", issue.AuthorAssociation)
	assert.False(t, issue.IsCrossRepository)
	assert.Equal(t, "other-repo", issue.RepoName)
}

func TestFetchInboxParsesRepositories(t *testing.T) {
	t.Parallel()

	items := byNumber(fetchFixtureInbox(t))

	assert.Equal(t, Repository{
		NodeID:              "R_fakeRepoExample",
		Description:         "An example repository",
		Visibility:          "PUBLIC",
		DefaultBranch:       "main",
		ViewerPermission:    "ADMIN",
		ViewerMergeMethod:   "SQUASH",
		SquashMergeAllowed:  true,
		AutoMergeAllowed:    true,
		DeleteBranchOnMerge: true,
		AllowUpdateBranch:   true,
		Language:            "Go",
		LanguageColor:       "#00add8",
		PushedAt:            time.Date(2026, 1, 5, 8, 0, 0, 0, time.UTC),
	}, items[7].Repo)

	// A locked, private fork with no default branch, no language, and no push yet: GitHub's
	// nulls come out as zero values.
	assert.Equal(t, Repository{
		NodeID:             "R_fakeRepoOther",
		Visibility:         "PRIVATE",
		IsLocked:           true,
		LockReason:         "MIGRATING",
		IsFork:             true,
		Parent:             "acme-org/upstream-repo",
		ViewerPermission:   "WRITE",
		ViewerMergeMethod:  "MERGE",
		MergeCommitAllowed: true,
		RebaseMergeAllowed: true,
	}, items[21].Repo)
}

func TestFetchInboxParsesAnArchivedRepository(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"viewer":{"login":"octo-user"},
			"reviewRequested":{"issueCount":1,"nodes":[{"__typename":"Issue","id":"I_fakeNodeId1","number":1,
				"repository":{"name":"old-repo","owner":{"login":"acme-org"},"isArchived":true,
					"archivedAt":"2022-08-01T00:00:00Z","isDisabled":true}}]},
			"authored":{"issueCount":0,"nodes":[]},"assigned":{"issueCount":0,"nodes":[]},
			"rateLimit":{"cost":1,"limit":5000,"remaining":4999,"used":1,"resetAt":"2026-01-10T13:00:00Z"}}}`)
	})

	inbox, err := client.FetchInbox(t.Context(), NewToken(fakeToken))
	require.NoError(t, err)
	require.Len(t, inbox.ReviewRequested.Items, 1)

	repo := inbox.ReviewRequested.Items[0].Repo
	assert.True(t, repo.IsArchived)
	assert.True(t, repo.IsDisabled)
	assert.Equal(t, time.Date(2022, 8, 1, 0, 0, 0, 0, time.UTC), repo.ArchivedAt)
}

func TestFetchInboxGraphQLErrors(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":null,"errors":[{"message":"Something went wrong"},{"message":"Try again"}]}`))
	})

	inbox, err := client.FetchInbox(t.Context(), NewToken(fakeToken))
	require.ErrorIs(t, err, ErrGraphQL)
	assert.Nil(t, inbox)
	assert.Contains(t, err.Error(), "Something went wrong; Try again")
}

func TestFetchInboxStatusErrors(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

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
	t.Parallel()

	cases := map[string]string{
		"not json": "this is not json",
		"no data":  `{}`,
		"too big":  `{"data":{"viewer":{"login":"` + strings.Repeat("a", maxResponseBytes) + `"}}}`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			})

			_, err := client.FetchInbox(t.Context(), NewToken(fakeToken))
			require.ErrorIs(t, err, ErrMalformedResponse)
		})
	}
}

func TestFetchInboxSkipsOtherNodes(t *testing.T) {
	t.Parallel()

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

// fetchCall is one exported fetch that sends a request with a token.
type fetchCall struct {
	name string

	// graphQL is set for a GraphQL query, whose errors come in a successful response's
	// body rather than as a failed status.
	graphQL bool

	call func(ctx context.Context, c *Client, tok Token) error
}

// everyFetch lists every exported fetch that sends a request with a token, each with
// arguments that make it send one.
func everyFetch() []fetchCall {
	return []fetchCall{
		{name: "FetchInbox", graphQL: true, call: func(ctx context.Context, c *Client, tok Token) error {
			_, err := c.FetchInbox(ctx, tok)
			return err
		}},
		{name: "FetchRateLimits", call: func(ctx context.Context, c *Client, tok Token) error {
			_, err := c.FetchRateLimits(ctx, tok)
			return err
		}},
		{name: "FetchDetails", graphQL: true, call: func(ctx context.Context, c *Client, tok Token) error {
			_, err := c.FetchDetails(ctx, tok, []string{"I_fakeNodeId1"})
			return err
		}},
		{name: "FetchItem", graphQL: true, call: func(ctx context.Context, c *Client, tok Token) error {
			_, err := c.FetchItem(ctx, tok, "I_fakeNodeId1")
			return err
		}},
		{name: "FetchPullHead", call: func(ctx context.Context, c *Client, tok Token) error {
			_, err := c.FetchPullHead(ctx, tok, pullRef())
			return err
		}},
		{name: "FetchChangedFiles", call: func(ctx context.Context, c *Client, tok Token) error {
			_, err := c.FetchChangedFiles(ctx, tok, pullRef(), "aaa")
			return err
		}},
		{name: "FetchItemStates", graphQL: true, call: func(ctx context.Context, c *Client, tok Token) error {
			_, err := c.FetchItemStates(ctx, tok, []item.Ref{pullRef()})
			return err
		}},
	}
}

func TestEveryFetchNeedsAToken(t *testing.T) {
	t.Parallel()

	for _, fetch := range everyFetch() {
		t.Run(fetch.name, func(t *testing.T) {
			t.Parallel()

			var requests atomic.Int32

			client := newTestClient(t, func(http.ResponseWriter, *http.Request) { requests.Add(1) })

			err := fetch.call(t.Context(), client, Token{})
			require.ErrorIs(t, err, ErrNotLoggedIn)
			assert.Equal(t, loggedOutMessage, uierr.Message(err), "the user is told to sign the GitHub CLI in")
			assert.Zero(t, requests.Load(), "no request is sent without a token")
		})
	}
}

func TestEveryFetchScrubsTheTokenFromItsErrors(t *testing.T) {
	t.Parallel()

	// Every answer is an error whose message echoes the token back.
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"upstream saw ` + fakeToken + `"}`))
	})
	tok := NewToken(fakeToken)

	for _, fetch := range everyFetch() {
		t.Run(fetch.name, func(t *testing.T) {
			t.Parallel()

			err := fetch.call(t.Context(), client, tok)
			require.ErrorIs(t, err, ErrUnexpectedStatus)
			assert.Contains(t, err.Error(), "[redacted]")
			assert.NotContains(t, err.Error(), fakeToken)
			assert.NotContains(t, uierr.Message(err), fakeToken)
			assert.NotContains(t, fmt.Sprintf("%+v", err), fakeToken)
		})
	}
}

func TestEveryQueryScrubsTheTokenFromGraphQLErrors(t *testing.T) {
	t.Parallel()

	// Every answer succeeds with only a GraphQL error that echoes the token back.
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"errors":[{"message":"bad token %s"}]}`, fakeToken)
	})
	tok := NewToken(fakeToken)

	for _, fetch := range everyFetch() {
		if !fetch.graphQL {
			continue
		}

		t.Run(fetch.name, func(t *testing.T) {
			t.Parallel()

			err := fetch.call(t.Context(), client, tok)
			require.ErrorIs(t, err, ErrGraphQL)
			assert.NotContains(t, err.Error(), fakeToken)
			assert.NotContains(t, uierr.Message(err), fakeToken)
			assert.NotContains(t, fmt.Sprintf("%+v", err), fakeToken)
		})
	}
}

func TestFetchInboxTransportErrors(t *testing.T) {
	t.Parallel()

	t.Run("server unreachable", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.NotFoundHandler())
		client := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
		srv.Close()

		_, err := client.FetchInbox(t.Context(), NewToken(fakeToken))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "call the GitHub API")
		assert.Equal(t, unreachableMessage, uierr.Message(err), "the user is told GitHub could not be reached")
	})

	t.Run("canceled", func(t *testing.T) {
		t.Parallel()

		client := newTestClient(t, serveFixture(t, "inbox_response.json", ""))

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := client.FetchInbox(ctx, NewToken(fakeToken))
		require.ErrorIs(t, err, context.Canceled)
		assert.NotEqual(t, unreachableMessage, uierr.Message(err), "a caller that gave up is not told GitHub is down")
	})

	t.Run("bad base URL", func(t *testing.T) {
		t.Parallel()

		client := &Client{BaseURL: "http://bad host"}

		_, err := client.FetchInbox(t.Context(), NewToken(fakeToken))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "build the GitHub API request")
	})
}

func TestNewClientDefaults(t *testing.T) {
	t.Parallel()

	client := NewClient("1.2.3")

	assert.Equal(t, DefaultBaseURL, client.BaseURL)
	assert.Equal(t, brand.Slug+"/1.2.3", client.UserAgent)
	assert.Equal(t, DefaultAvatarHost, client.AvatarHost)
	require.NotNil(t, client.HTTP)
	assert.Equal(t, defaultTimeout, client.HTTP.Timeout)

	assert.Equal(t, brand.Slug+"/dev", NewClient("").UserAgent)
}

func TestClientFallbacks(t *testing.T) {
	t.Parallel()

	assert.Equal(t, DefaultBaseURL, (&Client{}).baseURL())
	assert.Equal(t, "http://127.0.0.1:9", (&Client{BaseURL: "http://127.0.0.1:9/"}).baseURL())
	assert.Equal(t, defaultTimeout, (&Client{}).httpClient().Timeout)

	// A client without an HTTP client still works against the local server.
	srv := httptest.NewServer(serveFixture(t, "inbox_response.json", "repo, read:org"))
	t.Cleanup(srv.Close)

	inbox, err := (&Client{BaseURL: srv.URL + "/"}).FetchInbox(t.Context(), NewToken(fakeToken))
	require.NoError(t, err)
	assert.Equal(t, "octo-user", inbox.Viewer.Login)
}

func TestStatusErrorMessages(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "the GitHub API returned HTTP 500", (&StatusError{StatusCode: 500}).Error())
	assert.Equal(t, "the GitHub API returned HTTP 401: Bad credentials (run gh auth login)",
		(&StatusError{StatusCode: 401, Message: "Bad credentials"}).Error())
}

func TestTruncateCapsMessages(t *testing.T) {
	t.Parallel()

	got := truncate(strings.Repeat("abcdef ", 100))
	assert.Len(t, []rune(got), maxMessageRunes+1, "the kept text, then the ellipsis")
	assert.True(t, strings.HasSuffix(got, "…"))

	got = truncate(strings.Repeat("word ", 200))
	assert.True(t, strings.HasSuffix(got, "word…"), "space before the ellipsis is dropped: %q", got)
	assert.Equal(t, "a b", truncate(" a \n\t b "))
	assert.Empty(t, apiMessage([]byte("not json")))
	assert.Equal(t, "Not Found", apiMessage([]byte(fmt.Sprintf(`{"message":%q}`, "Not Found"))))
}
