package github

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The searches watches_response.json answers, in alias order: Dependabot's pull requests
// in an account and in an org, an account's and an org's issues, a user GitHub doesn't know
// (a silent zero), and a search sent with a cursor GitHub refused (a partial error).
var fixtureSearches = []WatchSearch{ //nolint:gochecknoglobals // fixed test values
	{Key: "dependabot-user", Query: "is:pr is:open archived:false user:octocat author:app/dependabot sort:updated-desc", Last: 1},
	{Key: "dependabot-org", Query: "is:pr is:open archived:false org:acme-org author:app/dependabot sort:updated-desc", Last: 2},
	{Key: "issues", Query: "is:issue is:open archived:false user:octocat org:acme-org sort:updated-desc", Last: 6},
	{Key: "nobody", Query: "is:pr is:open archived:false user:no-such-user-0000 author:app/dependabot sort:updated-desc", Last: 0},
	{Key: "bad-cursor", Query: "is:pr is:open archived:false user:octocat sort:updated-desc", Last: 2},
}

// watchRequest is one request a test server received: its query's text.
func watchRequest(t *testing.T, r *http.Request) string {
	t.Helper()

	raw, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	var req struct {
		Query string `json:"query"`
	}

	assert.NoError(t, json.Unmarshal(raw, &req))

	return req.Query
}

func TestPackWatches(t *testing.T) {
	t.Parallel()

	sizes := func(packs [][]WatchSearch) [][]string {
		out := make([][]string, 0, len(packs))

		for _, p := range packs {
			keys := make([]string, 0, len(p))
			for _, s := range p {
				keys = append(keys, s.Key)
			}

			out = append(out, keys)
		}

		return out
	}

	t.Run("by the counts each returned last", func(t *testing.T) {
		t.Parallel()

		got := packWatches([]WatchSearch{
			{Key: "a", Last: 40}, {Key: "b", Last: 50}, {Key: "c", Last: 20}, {Key: "d", Last: 100}, {Key: "e", Last: 3},
		})
		assert.Equal(t, [][]string{{"a", "b", "c"}, {"d", "e"}}, sizes(got))
	})

	t.Run("a new search counts as a full page", func(t *testing.T) {
		t.Parallel()

		got := packWatches([]WatchSearch{{Key: "a", Last: 10}, {Key: "new", Last: NewSearch}, {Key: "b", Last: 15}})
		assert.Equal(t, [][]string{{"a", "new"}, {"b"}}, sizes(got))
	})

	t.Run("each key once, and at most ten a request", func(t *testing.T) {
		t.Parallel()

		keys := strings.Split("a b c d e f g h i j k a b", " ")
		searches := make([]WatchSearch, 0, len(keys))

		for _, k := range keys {
			searches = append(searches, WatchSearch{Key: k, Last: 0})
		}

		got := packWatches(searches)
		assert.Equal(t, [][]string{{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}, {"k"}}, sizes(got))
	})

	t.Run("none", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, packWatches(nil))
	})
}

func TestFetchWatchesReadsEachSearch(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	body := fixture(t, "watches_response.json")
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)

		query := watchRequest(t, r)
		assert.Contains(t, query, `w0: search(query: "is:pr is:open archived:false user:octocat author:app/dependabot sort:updated-desc", type: ISSUE, first: 100)`)
		assert.Contains(t, query, "fragment WatchFields on SearchResultItem")
		assert.Contains(t, query, "rateLimit { cost remaining resetAt limit used }")
		assert.NotContains(t, query, "mergeStateStatus", "the merge state comes with the details")
		assert.NotContains(t, query, "reviewDecision", "the review decision comes with the details")

		_, _ = w.Write(body)
	})

	got, err := client.FetchWatches(t.Context(), NewToken(fakeToken), fixtureSearches)
	require.NoError(t, err)
	assert.Equal(t, int32(1), requests.Load(), "eleven results fit one request")
	assert.Equal(t, 1, got.Requests)
	assert.Equal(t, 1, got.RateLimit.Cost)
	assert.Len(t, got.Results, len(fixtureSearches))

	user := got.Results["dependabot-user"]
	require.NoError(t, user.Err)
	require.Len(t, user.Items, 1)

	first := user.Items[0]
	assert.True(t, first.IsPR)
	assert.Equal(t, 188, first.Number)
	assert.Equal(t, StateOpen, first.State)
	assert.Equal(t, "041c7e076d5dfb540ed9f3441ac0408323c0965a", first.HeadOID)
	assert.Equal(t, "master", first.BaseRef)
	assert.Equal(t, "SUCCESS", first.ChecksState)
	assert.Equal(t, "Bot", first.AuthorType)
	assert.Equal(t, "octocat", first.RepoOwner)
	assert.Equal(t, "example-repo", first.RepoName)
	assert.False(t, user.Truncated())

	issues := got.Results["issues"]
	require.NoError(t, issues.Err)
	assert.Len(t, issues.Items, 6)
	assert.False(t, issues.Items[0].IsPR)
	assert.Empty(t, issues.Items[0].HeadOID, "an issue has no head")

	nobody := got.Results["nobody"]
	require.NoError(t, nobody.Err, "GitHub answers an unknown user with nothing, not an error")
	assert.Zero(t, nobody.IssueCount)
	assert.Empty(t, nobody.Items)

	bad := got.Results["bad-cursor"]
	require.ErrorIs(t, bad.Err, ErrSearchFailed, "an error about one alias fails that search alone")
	assert.Contains(t, bad.Err.Error(), "valid cursor")
	assert.NotNil(t, bad.Items)
	assert.False(t, bad.Truncated())
}

func TestFetchWatchesReadsTheLeanFields(t *testing.T) {
	t.Parallel()

	body := fixture(t, "watches_lean_response.json")
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) })

	got, err := client.FetchWatches(t.Context(), NewToken(fakeToken), []WatchSearch{
		{Key: "w0", Query: "is:pr is:open archived:false org:acme-org author:app/dependabot sort:updated-desc", Last: 2},
		{Key: "w1", Query: "is:open archived:false repo:octocat/example-repo-6 sort:updated-desc", Last: 0},
	})
	require.NoError(t, err)

	first := got.Results["w0"]
	require.Len(t, first.Items, 2)
	assert.Equal(t, "FAILURE", first.Items[0].ChecksState)
	assert.Equal(t, "R_38MDu6ImV1", first.Items[0].Repo.NodeID)
	assert.Equal(t, time.Date(2026, 11, 6, 10, 4, 34, 0, time.UTC), first.Items[0].UpdatedAt)
	assert.Empty(t, got.Results["w1"].Items)
}

func TestFetchWatchesFailsTheSearchesOfARequestThatFails(t *testing.T) {
	t.Parallel()

	html := fixture(t, "watches_502.html")
	excessive := fixture(t, "watches_excessive_pagination_response.json")
	ok := `{"data":{"w0":{"issueCount":0,"nodes":[]},"rateLimit":{"cost":1,"limit":5000,"remaining":4990,` +
		`"used":10,"resetAt":"2026-11-16T06:43:00Z"}}}`

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query := watchRequest(t, r)

		switch {
		case strings.Contains(query, "slow-org"):
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write(html)
		case strings.Contains(query, "too-many"):
			_, _ = w.Write(excessive)
		default:
			_, _ = io.WriteString(w, ok)
		}
	})

	got, err := client.FetchWatches(t.Context(), NewToken(fakeToken), []WatchSearch{
		{Key: "slow", Query: "is:pr org:slow-org", Last: NewSearch},
		{Key: "slow-too", Query: "is:pr org:slow-org label:x", Last: 10},
		{Key: "fine", Query: "is:pr org:fine-org", Last: NewSearch},
		{Key: "too-many", Query: "is:pr org:too-many", Last: NewSearch},
	})
	require.NoError(t, err, "a failed request fails its own searches, never the call")

	require.ErrorIs(t, got.Results["slow"].Err, ErrUnexpectedStatus, "the 502's HTML is a failed request")
	require.ErrorIs(t, got.Results["slow-too"].Err, ErrUnexpectedStatus, "both searches of the request failed")
	require.NoError(t, got.Results["fine"].Err)
	require.ErrorIs(t, got.Results["too-many"].Err, ErrGraphQL, "errors with no data fail the request's searches")
	assert.Equal(t, 1, got.RateLimit.Cost, "only the answered request's cost")
}

func TestFetchWatchesReturnsARefusedToken(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"Bad credentials"}`)
	})

	_, err := client.FetchWatches(t.Context(), NewToken(fakeToken), fixtureSearches[:1])
	require.Error(t, err)
	assert.True(t, IsBadCredentials(err), "so the caller borrows a fresh token")
}

func TestFetchWatchesSendsPacksSideBySide(t *testing.T) {
	t.Parallel()

	var (
		mu      sync.Mutex
		queries []string
	)

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query := watchRequest(t, r)

		mu.Lock()
		defer mu.Unlock()

		queries = append(queries, query)

		_, _ = io.WriteString(w, `{"data":{"w0":{"issueCount":0,"nodes":[]},"rateLimit":{"cost":1,"limit":5000,`+
			`"remaining":4990,"used":10,"resetAt":"2026-11-16T06:43:00Z"}}}`)
	})

	got, err := client.FetchWatches(t.Context(), NewToken(fakeToken), []WatchSearch{
		{Key: "a", Query: `is:pr label:"a \"quoted\" one"`, Last: NewSearch},
		{Key: "b", Query: "is:pr org:b", Last: NewSearch},
		{Key: "c", Query: "is:pr org:c", Last: NewSearch},
	})
	require.NoError(t, err)
	assert.Equal(t, 3, got.Requests, "three new searches, a full page each")
	assert.Equal(t, 3, got.RateLimit.Cost)
	assert.Len(t, queries, 3)

	joined := strings.Join(queries, "\n")
	assert.Contains(t, joined, `search(query: "is:pr label:\"a \\\"quoted\\\" one\""`, "the query can't break out of its argument")
}
