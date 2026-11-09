package github

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/repo"
)

// mergedBody answers a merged-branches request: feature-x merged twice, the second
// pull request the newest; old-idea never merged; and a repository GitHub could not find.
const mergedBody = `{
  "data": {
    "r0": {
      "b0": {"nodes": [
        {"number": 184, "title": "Add locktimes", "url": "https://github.com/acme-org/example-repo/pull/184",
         "mergedAt": "2026-11-06T18:00:05Z", "headRefOid": "fc1c3975011e66491785ace891c893e4fc716587"},
        {"number": 90, "title": "First try", "url": "https://github.com/acme-org/example-repo/pull/90",
         "mergedAt": "2026-10-01T09:00:00Z", "headRefOid": "0123456789abcdef0123456789abcdef01234567"},
        null
      ]},
      "b1": {"nodes": []}
    },
    "r1": null,
    "rateLimit": {"cost": 1}
  },
  "errors": [{"type": "NOT_FOUND", "message": "Could not resolve to a Repository with the name 'acme-org/gone'."}]
}`

func TestFetchMergedPullRequestsAsksForEachBranch(t *testing.T) {
	t.Parallel()

	example := repo.Name{Owner: filesOwner, Repo: exampleRepo}
	gone := repo.Name{Owner: filesOwner, Repo: "gone"}

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		var req graphQLRequest
		assert.NoError(t, json.Unmarshal(raw, &req))
		assert.Contains(t, req.Query, `r0: repository(owner: "acme-org", name: "example-repo") {`)
		assert.Contains(t, req.Query, `b0: pullRequests(headRefName: "feature-x", states: MERGED, first: 5`)
		assert.Contains(t, req.Query, `b1: pullRequests(headRefName: "old-idea", states: MERGED`)
		assert.Contains(t, req.Query, `r1: repository(owner: "acme-org", name: "gone")`)
		assert.Contains(t, req.Query, "rateLimit { cost }")

		_, _ = io.WriteString(w, mergedBody)
	})

	got, err := client.FetchMergedPullRequests(t.Context(), NewToken(fakeToken), []BranchQuery{
		{Repo: example, Branches: []string{"feature-x", "old-idea", ""}},
		{Repo: gone, Branches: []string{"main-fix"}},
		{Repo: repo.Name{Owner: filesOwner, Repo: "quiet"}},
	})
	require.NoError(t, err)

	assert.Equal(t, map[string][]MergedPR{
		MergedKey(example, "feature-x"): {
			{
				Number: 184, Title: "Add locktimes", URL: "https://github.com/acme-org/example-repo/pull/184",
				MergedAt: time.Date(2026, 11, 6, 18, 0, 5, 0, time.UTC), HeadOID: "fc1c3975011e66491785ace891c893e4fc716587",
			},
			{
				Number: 90, Title: "First try", URL: "https://github.com/acme-org/example-repo/pull/90",
				MergedAt: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), HeadOID: "0123456789abcdef0123456789abcdef01234567",
			},
		},
	}, got, "a branch no merged pull request had, and a repository GitHub cannot see, are left out")
}

func TestFetchMergedPullRequestsBatchesAndSkipsEmptyAsks(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)

		_, _ = io.WriteString(w, `{"data": {}}`)
	})

	branches := make([]string, mergedBatch+1)
	for i := range branches {
		branches[i] = "b" + strconv.Itoa(i)
	}

	got, err := client.FetchMergedPullRequests(t.Context(), NewToken(fakeToken), []BranchQuery{
		{Repo: repo.Name{Owner: filesOwner, Repo: exampleRepo}, Branches: branches},
	})
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, int32(2), requests.Load())

	got, err = client.FetchMergedPullRequests(t.Context(), NewToken(fakeToken), nil)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, int32(2), requests.Load(), "nothing to ask sends nothing")
}

func TestFetchMergedPullRequestsFailsOnAnyOtherError(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data": null, "errors": [{"type": "FORBIDDEN", "message": "no access"}]}`)
	})

	_, err := client.FetchMergedPullRequests(t.Context(), NewToken(fakeToken), []BranchQuery{
		{Repo: repo.Name{Owner: filesOwner, Repo: exampleRepo}, Branches: []string{"feature-x"}},
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), fakeToken)
}

func TestMergedQueryQuotesNames(t *testing.T) {
	t.Parallel()

	query, err := mergedQuery([]mergedAsk{
		{repo: repo.Name{Owner: filesOwner, Repo: exampleRepo}, branches: []string{`x") { evil }`}},
	})
	require.NoError(t, err)
	assert.Contains(t, query, `headRefName: "x\") { evil }"`)
	assert.Equal(t, 1, strings.Count(query, "pullRequests("))
}

func TestMergedRepoIgnoresWhatItCannotRead(t *testing.T) {
	t.Parallel()

	r := mergedRepo{"b0": json.RawMessage(`"not a connection"`)}
	assert.Empty(t, r.pullRequests("b0"))
	assert.Empty(t, r.pullRequests("b9"))

	unmerged := mergedRepo{"b0": json.RawMessage(`{"nodes": [{"number": 3, "mergedAt": null}, {"number": 0}]}`)}
	assert.Empty(t, unmerged.pullRequests("b0"), "a node without a merge or a number is no merged pull request")
}
