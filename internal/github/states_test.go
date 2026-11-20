package github

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// statesBody is a states response: a merged pull request, a closed issue, an open pull
// request, and a ref GitHub could not find.
const statesBody = `{
  "data": {
    "r0": {"item": {"__typename": "PullRequest", "title": "Add locktimes", "url": "https://github.com/acme-org/example-repo/pull/184",
      "prState": "MERGED", "closedAt": "2026-11-06T18:00:00Z", "mergedAt": "2026-11-06T18:00:05Z", "headRefOid": "fc1c3975011e66491785ace891c893e4fc716587",
      "headRefName": "add-locktimes"}},
    "r1": {"item": {"__typename": "Issue", "title": "Crash on start", "url": "https://github.com/acme-org/example-repo/issues/7",
      "issueState": "CLOSED", "closedAt": "2026-11-05T10:00:00Z"}},
    "r2": {"item": {"__typename": "PullRequest", "title": "Still going", "url": "https://github.com/acme-org/other-repo/pull/3",
      "prState": "OPEN", "closedAt": null, "mergedAt": null, "headRefOid": "abc"}},
    "r3": null
  },
  "errors": [{"type": "NOT_FOUND", "message": "Could not resolve to a Repository with the name 'acme-org/gone'."}]
}`

func TestFetchItemStatesReadsEachRef(t *testing.T) {
	t.Parallel()

	refs := []item.Ref{
		{Repo: repo.Name{Owner: "acme-org", Repo: "example-repo"}, Number: 184},
		{Repo: repo.Name{Owner: "acme-org", Repo: "example-repo"}, Number: 7},
		{Repo: repo.Name{Owner: "acme-org", Repo: "other-repo"}, Number: 3},
		{Repo: repo.Name{Owner: "acme-org", Repo: "gone"}, Number: 1},
	}

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/graphql", r.URL.Path)
		assert.Equal(t, "Bearer "+fakeToken, r.Header.Get("Authorization"))

		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		var req graphQLRequest
		assert.NoError(t, json.Unmarshal(raw, &req))
		assert.Contains(t, req.Query, `r0: repository(owner: "acme-org", name: "example-repo") { item: issueOrPullRequest(number: 184)`)
		assert.Contains(t, req.Query, `r3: repository(owner: "acme-org", name: "gone")`)
		assert.Contains(t, req.Query, "fragment StateFields on IssueOrPullRequest")

		_, _ = io.WriteString(w, statesBody)
	})

	states, err := client.FetchItemStates(t.Context(), NewToken(fakeToken), refs)
	require.NoError(t, err)
	require.Len(t, states, 3)

	assert.Equal(t, ItemState{
		PullRequest: true,
		Title:       "Add locktimes",
		URL:         "https://github.com/acme-org/example-repo/pull/184",
		State:       StateMerged,
		ClosedAt:    time.Date(2026, 11, 6, 18, 0, 5, 0, time.UTC),
		HeadOID:     "fc1c3975011e66491785ace891c893e4fc716587",
		HeadRef:     "add-locktimes",
	}, states[refs[0].Key()], "a merged pull request is dated by its merge")

	assert.Equal(t, ItemState{
		Title:    "Crash on start",
		URL:      "https://github.com/acme-org/example-repo/issues/7",
		State:    StateClosed,
		ClosedAt: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC),
	}, states[refs[1].Key()])

	assert.Equal(t, StateOpen, states[refs[2].Key()].State)
	assert.True(t, states[refs[2].Key()].ClosedAt.IsZero())
	assert.NotContains(t, states, refs[3].Key(), "a ref GitHub cannot find is left out")
}

func TestFetchItemStatesBatchesLargeRequests(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)

		_, _ = io.WriteString(w, `{"data": {}}`)
	})

	refs := make([]item.Ref, statesBatch+1)
	for i := range refs {
		refs[i] = item.Ref{Repo: repo.Name{Owner: "acme-org", Repo: "example-repo"}, Number: i + 1}
	}

	states, err := client.FetchItemStates(t.Context(), NewToken(fakeToken), refs)
	require.NoError(t, err)
	assert.Empty(t, states)
	assert.Equal(t, int32(2), requests.Load())
}

// statesRefs returns n refs in the files tests' repository, numbered from 1.
func statesRefs(n int) []item.Ref {
	refs := make([]item.Ref, n)
	for i := range refs {
		refs[i] = item.Ref{Repo: pullRef().Repo, Number: i + 1}
	}

	return refs
}

func TestFetchItemStatesSendsTheBatchesSideBySide(t *testing.T) {
	t.Parallel()

	// Each request waits until both have arrived, which only happens when they are in
	// flight together; sent one after another, the first gives up and fails the call.
	var arrived sync.WaitGroup

	arrived.Add(2)

	both := make(chan struct{})

	go func() {
		arrived.Wait()
		close(both)
	}()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		arrived.Done()

		select {
		case <-both:
			_, _ = io.WriteString(w, `{"data": {}}`)
		case <-time.After(waitTimeout):
			w.WriteHeader(http.StatusGatewayTimeout)
		}
	})

	states, err := client.FetchItemStates(t.Context(), NewToken(fakeToken), statesRefs(statesBatch+1))
	require.NoError(t, err)
	assert.Empty(t, states)
}

func TestFetchItemStatesRefusesABadRefBeforeSendingAnything(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)

		_, _ = io.WriteString(w, `{"data": {}}`)
	})

	refs := statesRefs(statesBatch + 1)
	refs[statesBatch].Number = 0

	_, err := client.FetchItemStates(t.Context(), NewToken(fakeToken), refs)
	require.ErrorIs(t, err, ErrInvalidItemRef)
	assert.Zero(t, requests.Load(), "the first batch is not sent either")
}

func TestFetchItemStatesFailsWhenOneBatchFails(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 2 {
			w.WriteHeader(http.StatusBadGateway)
		}

		_, _ = io.WriteString(w, `{"data": {}}`)
	})

	_, err := client.FetchItemStates(t.Context(), NewToken(fakeToken), statesRefs(statesBatch+1))
	require.ErrorIs(t, err, ErrUnexpectedStatus)
}

func TestFetchItemStatesFailures(t *testing.T) {
	t.Parallel()

	ref := []item.Ref{{Repo: repo.Name{Owner: "acme-org", Repo: "example-repo"}, Number: 1}}

	tests := []struct {
		name    string
		refs    []item.Ref
		body    string
		status  int
		wantErr error
		want    string
	}{
		{
			name:    "an error other than not found fails the call",
			refs:    ref,
			body:    `{"data": {"r0": null}, "errors": [{"type": "RATE_LIMITED", "message": "slow down"}]}`,
			wantErr: ErrGraphQL,
			want:    "slow down",
		},
		{
			name:    "a response without data",
			refs:    ref,
			body:    `{"data": null}`,
			wantErr: ErrMalformedResponse,
		},
		{
			name:    "a body that is not JSON",
			refs:    ref,
			body:    `<html>`,
			wantErr: ErrMalformedResponse,
		},
		{
			name:    "an HTTP error",
			refs:    ref,
			status:  http.StatusBadGateway,
			body:    `{"message": "bad gateway"}`,
			wantErr: ErrUnexpectedStatus,
		},
		{
			name:    "a number that is not positive is never sent",
			refs:    []item.Ref{{Repo: repo.Name{Owner: "acme-org", Repo: "example-repo"}, Number: 0}},
			wantErr: ErrInvalidItemRef,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				if tt.status != 0 {
					w.WriteHeader(tt.status)
				}

				_, _ = io.WriteString(w, tt.body)
			})

			_, err := client.FetchItemStates(t.Context(), NewToken(fakeToken), tt.refs)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestStatesQueryQuotesNames(t *testing.T) {
	t.Parallel()

	query, err := statesQuery([]item.Ref{{Repo: repo.Name{Owner: `acme"org`, Repo: "example-repo"}, Number: 9}}, nil)
	require.NoError(t, err)
	assert.Contains(t, query, `owner: "acme\"org"`)
	assert.Equal(t, 1, strings.Count(query, "repository("))
}

func TestStatesNodeIgnoresOtherTypes(t *testing.T) {
	t.Parallel()

	_, ok := (&statesRepo{Item: &statesNode{TypeName: "Discussion", Title: "x"}}).state()
	assert.False(t, ok)

	_, ok = (&statesRepo{Item: &statesNode{TypeName: typeIssue}}).state()
	assert.False(t, ok, "an item without a state is no answer")

	_, ok = (*statesRepo)(nil).state()
	assert.False(t, ok)
}

// headBranch is the branch the head asks of these tests are about.
const headBranch = "feature-x"

// linkBody answers a states request with one item and two head asks: feature-x had three
// pull requests, the newest from a fork whose repository GitHub no longer names, then a
// merged one and a closed one, with a null node beside; old-idea had none.
const linkBody = `{
  "data": {
    "r0": {"item": {"__typename": "PullRequest", "title": "Add locktimes", "url": "https://github.com/acme-org/example-repo/pull/184",
      "prState": "OPEN", "closedAt": null, "mergedAt": null, "headRefOid": "fc1c3975011e66491785ace891c893e4fc716587",
      "headRefName": "add-locktimes"}},
    "h0": {"pulls": {"nodes": [
      {"number": 201, "title": "From a fork", "url": "https://github.com/acme-org/example-repo/pull/201", "state": "OPEN",
       "closedAt": null, "mergedAt": null, "headRefOid": "0b1c3975011e66491785ace891c893e4fc716588", "headRefName": "feature-x",
       "headRepository": null},
      {"number": 199, "title": "Describe it", "url": "https://github.com/acme-org/example-repo/pull/199", "state": "MERGED",
       "closedAt": "2026-11-06T18:00:00Z", "mergedAt": "2026-11-06T18:00:05Z", "headRefOid": "fc1c3975011e66491785ace891c893e4fc716587",
       "headRefName": "feature-x", "headRepository": {"nameWithOwner": "acme-org/example-repo"}},
      null,
      {"number": 0, "state": "OPEN"},
      {"number": 150, "title": "First try", "url": "https://github.com/acme-org/example-repo/pull/150", "state": "CLOSED",
       "closedAt": "2026-10-01T09:00:00Z", "mergedAt": null, "headRefOid": "abc", "headRefName": "feature-x",
       "headRepository": {"nameWithOwner": "Acme-Org/Example-Repo"}}
    ]}},
    "h1": {"pulls": {"nodes": []}},
    "rateLimit": {"cost": 1}
  }
}`

func TestFetchLinkStatesAsksAboutHeadsInTheStatesRequest(t *testing.T) {
	t.Parallel()

	example := repo.Name{Owner: filesOwner, Repo: exampleRepo}

	var requests atomic.Int32

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)

		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		var req graphQLRequest
		assert.NoError(t, json.Unmarshal(raw, &req))
		assert.Contains(t, req.Query, `r0: repository(owner: "acme-org", name: "example-repo") { item: issueOrPullRequest(number: 184)`)
		assert.Contains(t, req.Query, `h0: repository(owner: "acme-org", name: "example-repo") { pulls: pullRequests(`+
			`headRefName: "feature-x", states: [OPEN, MERGED, CLOSED], first: 20, orderBy: {field: UPDATED_AT, direction: DESC})`)
		assert.Contains(t, req.Query, `h1: repository(owner: "acme-org", name: "example-repo") { pulls: pullRequests(headRefName: "old-idea"`)
		assert.NotContains(t, req.Query, "h2:", "an empty name and one that is not UTF-8 are never asked about")
		assert.Contains(t, req.Query, "headRepository { nameWithOwner }")

		_, _ = io.WriteString(w, linkBody)
	})

	states, heads, err := client.FetchLinkStates(t.Context(), NewToken(fakeToken),
		[]item.Ref{{Repo: example, Number: 184}},
		[]HeadQuery{
			{Repo: example, Branch: headBranch},
			{Repo: example, Branch: ""},
			{Repo: example, Branch: "old-idea"},
			{Repo: example, Branch: "feat-\xff"},
		})
	require.NoError(t, err)
	assert.Equal(t, int32(1), requests.Load(), "the items and the heads go in one request")

	require.Len(t, states, 1)
	assert.Equal(t, StateOpen, states[item.Ref{Repo: example, Number: 184}.Key()].State)

	require.Len(t, heads, 1, "a branch no pull request had is left out")
	assert.Equal(t, []HeadPR{
		{Number: 201, State: ItemState{
			PullRequest: true, Title: "From a fork", URL: "https://github.com/acme-org/example-repo/pull/201", State: StateOpen,
			HeadOID: "0b1c3975011e66491785ace891c893e4fc716588", HeadRef: headBranch,
		}},
		{Number: 199, HeadRepo: filesOwner + "/" + exampleRepo, State: ItemState{
			PullRequest: true, Title: "Describe it", URL: "https://github.com/acme-org/example-repo/pull/199", State: StateMerged,
			ClosedAt: time.Date(2026, 11, 6, 18, 0, 5, 0, time.UTC), HeadOID: "fc1c3975011e66491785ace891c893e4fc716587",
			HeadRef: headBranch,
		}},
		{Number: 150, HeadRepo: "Acme-Org/Example-Repo", State: ItemState{
			PullRequest: true, Title: "First try", URL: "https://github.com/acme-org/example-repo/pull/150", State: StateClosed,
			ClosedAt: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), HeadOID: "abc", HeadRef: headBranch,
		}},
	}, heads[HeadKey(repo.Name{Owner: "Acme-Org", Repo: "Example-Repo"}, headBranch)],
		"in GitHub's order, a merged one dated by its merge, a null node and one without a number left out, keyed whatever the casing")
}

func TestFetchLinkStatesReachesTheRepositorysOwnPullRequestBehindForks(t *testing.T) {
	t.Parallel()

	example := repo.Name{Owner: filesOwner, Repo: exampleRepo}

	// Three forks' pull requests of a branch of the same name, updated since, then the
	// repository's own, which GitHub answers only as far as the query's first takes it.
	nodes := make([]string, 0, 4)
	for i := range 3 {
		nodes = append(nodes, `{"number": `+strconv.Itoa(301+i)+`, "state": "OPEN", "headRefName": "feature-x", `+
			`"headRepository": {"nameWithOwner": "fork-`+strconv.Itoa(i)+`/example-repo"}}`)
	}

	nodes = append(nodes, `{"number": 42, "state": "OPEN", "headRefName": "feature-x", `+
		`"headRepository": {"nameWithOwner": "acme-org/example-repo"}}`)

	firstArg := regexp.MustCompile(`first: (\d+),`)

	var requests atomic.Int32

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)

		var req graphQLRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Contains(t, req.Query, "rateLimit { cost }", "what the request cost is still counted")

		match := firstArg.FindStringSubmatch(req.Query)
		if !assert.Len(t, match, 2) {
			return
		}

		first, err := strconv.Atoi(match[1])
		assert.NoError(t, err)

		_, _ = io.WriteString(w, `{"data": {"h0": {"pulls": {"nodes": [`+strings.Join(nodes[:min(first, len(nodes))], ",")+
			`]}}, "rateLimit": {"cost": 1}}}`)
	})

	_, heads, err := client.FetchLinkStates(t.Context(), NewToken(fakeToken), nil,
		[]HeadQuery{{Repo: example, Branch: headBranch}})
	require.NoError(t, err)
	assert.Equal(t, int32(1), requests.Load(), "still one request")

	got := heads[HeadKey(example, headBranch)]
	require.Len(t, got, 4)
	assert.Equal(t, 42, got[3].Number)
	assert.Equal(t, filesOwner+"/"+exampleRepo, got[3].HeadRepo, "the repository's own pull request is in the answer")
}

func TestFetchLinkStatesBatchesHeadsBesideItems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		refs, heads  int
		wantRequests int32
	}{
		{name: "nothing to ask sends nothing", wantRequests: 0},
		{name: "heads alone are one request per fifty", heads: headsBatch + 1, wantRequests: 2},
		{name: "heads ride in the items' requests", refs: statesBatch + 1, heads: headsBatch, wantRequests: 2},
		{name: "more heads than the items' requests hold", refs: 1, heads: 2*headsBatch + 1, wantRequests: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				requests atomic.Int32
				mu       sync.Mutex
				asked    []string
			)

			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)

				raw, err := io.ReadAll(r.Body)
				assert.NoError(t, err)

				mu.Lock()
				defer mu.Unlock()

				asked = append(asked, string(raw))

				_, _ = io.WriteString(w, `{"data": {}}`)
			})

			heads := make([]HeadQuery, tt.heads)
			for i := range heads {
				heads[i] = HeadQuery{Repo: pullRef().Repo, Branch: "feature-" + strconv.Itoa(i)}
			}

			states, prs, err := client.FetchLinkStates(t.Context(), NewToken(fakeToken), statesRefs(tt.refs), heads)
			require.NoError(t, err)
			assert.NotNil(t, states)
			assert.NotNil(t, prs)
			assert.Equal(t, tt.wantRequests, requests.Load())

			total := 0
			for _, body := range asked {
				total += strings.Count(body, "pulls: pullRequests(")
			}

			assert.Equal(t, tt.heads, total, "each head is asked about once")
		})
	}
}

func TestStatesRepoPullRequestsWithoutAnAnswer(t *testing.T) {
	t.Parallel()

	assert.Empty(t, (*statesRepo)(nil).pullRequests())
	assert.Empty(t, (&statesRepo{}).pullRequests())
	assert.Empty(t, (&statesRepo{Pulls: &headConnection{Nodes: []*headNode{nil, {Number: 3}}}}).pullRequests(),
		"a node without a state is no answer")
}

func TestFetchLinkStatesFailsWhenGitHubDoes(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data": {"h0": null}, "errors": [{"type": "RATE_LIMITED", "message": "slow down"}]}`)
	})

	_, _, err := client.FetchLinkStates(t.Context(), NewToken(fakeToken), nil,
		[]HeadQuery{{Repo: pullRef().Repo, Branch: headBranch}})
	require.ErrorIs(t, err, ErrGraphQL, "a passing problem never leaves a branch looking like no pull request's")

	_, _, err = client.FetchLinkStates(t.Context(), NewToken(fakeToken), nil,
		[]HeadQuery{{Repo: repo.Name{Owner: "acme-\xff", Repo: exampleRepo}, Branch: headBranch}})
	require.Error(t, err, "a repository that can't be written is never sent")
}
