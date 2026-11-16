package github

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
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

// watchNodeJSON is one pull request result of a routine's search, as WatchFields reads it.
func watchNodeJSON(number int) string {
	return fmt.Sprintf(`{"__typename":"PullRequest","id":"PR_%d","number":%d,"title":"Bump %d",`+
		`"url":"https://github.com/acme-org/example-repo/pull/%d","createdAt":"2026-11-01T00:00:00Z",`+
		`"updatedAt":"2026-11-02T00:00:00Z","isDraft":false,"headRefName":"bump-%d","prState":"OPEN","closedAt":null,`+
		`"authorAssociation":"NONE","isCrossRepository":false,"author":{"__typename":"Bot","login":"dependabot"},`+
		`"headRefOid":"abc","baseRefName":"main","statusCheckRollup":{"state":"SUCCESS"},`+
		`"repository":{"id":"R_1","name":"example-repo","owner":{"login":"acme-org"},"isArchived":false}}`,
		number, number, number, number, number)
}

// watchAnswerJSON is a request's answer: each alias's results, by alias, and errors, the
// raw JSON of GitHub's errors list ("" for none).
func watchAnswerJSON(results map[string][]int, errs string) string {
	var b strings.Builder

	b.WriteString(`{"data":{`)

	for _, alias := range slices.Sorted(maps.Keys(results)) {
		nodes := make([]string, 0, len(results[alias]))
		for _, n := range results[alias] {
			nodes = append(nodes, watchNodeJSON(n))
		}

		fmt.Fprintf(&b, `%q:{"issueCount":%d,"nodes":[%s]},`, alias, len(nodes), strings.Join(nodes, ","))
	}

	b.WriteString(`"rateLimit":{"cost":1,"limit":5000,"remaining":4990,"used":10,"resetAt":"2026-11-16T06:43:00Z"}}`)

	if errs != "" {
		b.WriteString(`,"errors":` + errs)
	}

	b.WriteString(`}`)

	return b.String()
}

// twoSearches are two searches that share one request.
func twoSearches() []WatchSearch {
	return []WatchSearch{
		{Key: "deps", Query: "is:pr org:acme-org author:app/dependabot", Last: 3},
		{Key: "chores", Query: "is:pr org:acme-org label:chore", Last: 1},
	}
}

func TestFetchWatchesLeavesOutAResultGitHubFailedAlone(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, watchAnswerJSON(map[string][]int{"w0": {1, 2, 3}, "w1": {4}},
			`[{"message":"Something went wrong while executing your query.","path":["w0","nodes",1,"author"]}]`))
	})

	got, err := client.FetchWatches(t.Context(), NewToken(fakeToken), twoSearches())
	require.NoError(t, err)

	deps := got.Results["deps"]
	require.NoError(t, deps.Err, "an error about one result never fails its search")
	assert.Equal(t, []int{1, 3}, numbersOf(deps.Items), "the result it names is left out")
	assert.True(t, deps.Partial, "so an item missing from the answer may still match")

	chores := got.Results["chores"]
	require.NoError(t, chores.Err)
	assert.Equal(t, []int{4}, numbersOf(chores.Items))
	assert.False(t, chores.Partial, "the other search is whole")
}

func TestFetchWatchesFailsASearchWhoseResultsGitHubFailed(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, watchAnswerJSON(map[string][]int{"w0": {1}, "w1": {4}},
			`[{"message":"Something went wrong while executing your query.","path":["w0","nodes"]}]`))
	})

	got, err := client.FetchWatches(t.Context(), NewToken(fakeToken), twoSearches())
	require.NoError(t, err)
	require.ErrorIs(t, got.Results["deps"].Err, ErrSearchFailed, "an error about its list of results fails the search")
	require.NoError(t, got.Results["chores"].Err)
}

func TestFetchWatchesKeepsTheDataBesideAnErrorThatNamesNoSearch(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, watchAnswerJSON(map[string][]int{"w0": {1, 2}, "w1": {4}},
			`[{"message":"Something went wrong while executing your query."}]`))
	})

	got, err := client.FetchWatches(t.Context(), NewToken(fakeToken), twoSearches())
	require.NoError(t, err)

	for _, key := range []string{"deps", "chores"} {
		r := got.Results[key]
		require.NoError(t, r.Err, "the data GitHub sent is read")
		assert.NotEmpty(t, r.Items)
		assert.True(t, r.Partial, "but nothing is taken to have left the search")
	}
}

func TestFetchWatchesFailsOnlyASearchThatCantBeSent(t *testing.T) {
	t.Parallel()

	var queries []string

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, watchRequest(t, r))

		_, _ = io.WriteString(w, watchAnswerJSON(map[string][]int{"w0": {4}}, ""))
	})

	got, err := client.FetchWatches(t.Context(), NewToken(fakeToken), []WatchSearch{
		{Key: "broken", Query: "is:pr label:\xff", Last: 1},
		{Key: "chores", Query: "is:pr org:acme-org label:chore", Last: 1},
	})
	require.NoError(t, err)
	require.ErrorIs(t, got.Results["broken"].Err, errNotUTF8, "a query that can't be written fails its search alone")
	assert.NotNil(t, got.Results["broken"].Items)
	require.NoError(t, got.Results["chores"].Err, "and the search beside it is sent")
	assert.Equal(t, []int{4}, numbersOf(got.Results["chores"].Items))
	require.Len(t, queries, 1)
	assert.Equal(t, 1, strings.Count(queries[0], "search("), "the broken one never goes out")
}

func TestFetchWatchesHoldsFourRequestsInFlight(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		held := &heldTransport{release: make(chan struct{})}
		client := &Client{BaseURL: "https://github.test", UserAgent: testUserAgent, HTTP: &http.Client{Transport: held}}

		searches := make([]WatchSearch, 0, 8)
		for i := range 8 {
			searches = append(searches, WatchSearch{Key: fmt.Sprintf("s%d", i), Query: fmt.Sprintf("is:pr org:org-%d", i), Last: NewSearch})
		}

		done := make(chan *Watches, 1)

		go func() {
			got, err := client.FetchWatches(t.Context(), NewToken(fakeToken), searches)
			assert.NoError(t, err)

			done <- got
		}()

		synctest.Wait()
		assert.Equal(t, int32(watchParallel), held.inFlight.Load(), "eight new searches, four requests at a time")

		close(held.release)

		got := <-done
		assert.Equal(t, 8, got.Requests)
		assert.Equal(t, int32(watchParallel), held.most.Load())
	})
}

// heldTransport answers each request in memory, with an empty search, once release is
// closed, counting the requests in flight and the most there were at once.
type heldTransport struct {
	inFlight, most atomic.Int32
	release        chan struct{}
}

func (h *heldTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	n := h.inFlight.Add(1)
	defer h.inFlight.Add(-1)

	for old := h.most.Load(); n > old && !h.most.CompareAndSwap(old, n); old = h.most.Load() {
	}

	select {
	case <-h.release:
	case <-r.Context().Done():
		return nil, r.Context().Err()
	}

	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Request: r,
		Body: io.NopCloser(strings.NewReader(watchAnswerJSON(map[string][]int{"w0": {}}, ""))),
	}, nil
}

// numbersOf returns the items' numbers, in order.
func numbersOf(items []WatchItem) []int {
	out := make([]int, 0, len(items))
	for _, it := range items {
		out = append(out, it.Number)
	}

	return out
}

func TestWatchRequestsCountsThePacks(t *testing.T) {
	t.Parallel()

	assert.Zero(t, WatchRequests(nil))
	assert.Equal(t, 1, WatchRequests([]int{40, 50, 20}))
	assert.Equal(t, 2, WatchRequests([]int{40, 50, 20, 100}))
	assert.Equal(t, 3, WatchRequests([]int{NewSearch, NewSearch, NewSearch}), "a new search is a full page")
}
