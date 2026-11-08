package github

import (
	"encoding/json"
	"fmt"
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

// detailsRequest is the body FetchDetails sends.
type detailsRequest struct {
	Query     string `json:"query"`
	Variables struct {
		IDs []string `json:"ids"`
	} `json:"variables"`
}

// readDetailsRequest decodes a details request body.
func readDetailsRequest(t *testing.T, r *http.Request) detailsRequest {
	t.Helper()

	raw, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	var req detailsRequest
	assert.NoError(t, json.Unmarshal(raw, &req))

	return req
}

func TestFetchDetailsReadsEachItem(t *testing.T) {
	t.Parallel()

	body := fixture(t, "details_response.json")
	ids := []string{"PR_fakeNodeIdPassing", "PR_fakeNodeIdFailing", "PR_fakeNodeIdNoChecks", "I_fakeNodeIdIssue", "PR_fakeNodeIdGone"}

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/graphql", r.URL.Path)
		assert.Equal(t, "Bearer "+fakeToken, r.Header.Get("Authorization"))

		req := readDetailsRequest(t, r)
		assert.Contains(t, req.Query, "nodes(ids: $ids)")
		assert.Contains(t, req.Query, "statusCheckRollup")
		assert.Contains(t, req.Query, "headRefOid")
		assert.Equal(t, ids, req.Variables.IDs)

		_, _ = w.Write(body)
	})

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), ids, DetailOptions{})
	require.NoError(t, err)

	assert.Equal(t, 1, details.Requests)
	assert.Equal(t, QueryRate{
		Cost: 1, Limit: 5000, Remaining: 4980, Used: 20, ResetAt: time.Date(2026, 1, 10, 13, 0, 0, 0, time.UTC),
	}, details.RateLimit)

	assert.Equal(t, map[string]ItemDetails{
		"PR_fakeNodeIdPassing": {
			Labels:       []Label{{Name: "enhancement", Color: "#a2eeef"}, {Name: "odd-color"}},
			Comments:     4,
			Additions:    120,
			Deletions:    30,
			ChangedFiles: 6,
			Commits:      3,
			HeadOID:      "1111111111111111111111111111111111111111",
			Mergeable:    "MERGEABLE",
			Checks:       Checks{State: "SUCCESS", Passed: 15, Skipped: 15},
			// Asked: octo-user, the team, and octo-approver again. Reviewed and not asked
			// again: octo-second and octo-commenter. The author's own reply, the
			// dismissed, the pending, and the ghost reviews do not count.
			Reviews: Reviews{Decision: "APPROVED", Reviewers: 5, Reviewed: 2, Approvals: 2},
		},
		"PR_fakeNodeIdFailing": {
			Labels:       []Label{{Name: "needs-work", Color: "#ffd700"}},
			Comments:     14,
			Additions:    937,
			Deletions:    40,
			ChangedFiles: 8,
			Commits:      6,
			HeadOID:      "2222222222222222222222222222222222222222",
			Mergeable:    "CONFLICTING",
			Checks:       Checks{State: "FAILURE", Passed: 18, Failed: 5, Pending: 4},
			Reviews:      Reviews{Decision: "CHANGES_REQUESTED", Reviewers: 1, Reviewed: 1, ChangesRequested: 1},
		},
		"PR_fakeNodeIdNoChecks": {
			Labels:       []Label{},
			Additions:    2,
			Deletions:    2,
			ChangedFiles: 2,
			Commits:      1,
			HeadOID:      "3333333333333333333333333333333333333333",
			Mergeable:    "UNKNOWN",
		},
		"I_fakeNodeIdIssue": {
			Labels:   []Label{{Name: "bug", Color: "#d73a4a"}},
			Comments: 7,
		},
	}, details.Items, "the unresolvable item is left out, not a failure")
}

func TestFetchDetailsBatchesTheIDsSideBySide(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		requests atomic.Int32
		sizes    []int
	)

	ids := make([]string, 120)
	for i := range ids {
		ids[i] = fmt.Sprintf("I_fakeNodeId%d", i)
	}

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		req := readDetailsRequest(t, r)

		mu.Lock()

		sizes = append(sizes, len(req.Variables.IDs))

		mu.Unlock()

		nodes := make([]string, 0, len(req.Variables.IDs))
		for _, id := range req.Variables.IDs {
			nodes = append(nodes, fmt.Sprintf(`{"__typename":"Issue","id":%q,"comments":{"totalCount":1},"labels":{"nodes":[]}}`, id))
		}

		_, _ = fmt.Fprintf(w, `{"data":{"nodes":[%s],"rateLimit":{"cost":%d,"limit":5000,"remaining":%d,"used":%d,"resetAt":"2026-01-10T13:00:00Z"}}}`,
			strings.Join(nodes, ","), n, 5000-n*10, n*10)
	})

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), ids, DetailOptions{})
	require.NoError(t, err)

	assert.Equal(t, []int{30, 30, 30, 30}, sizes, "a large inbox gets bigger batches, not more requests")
	assert.Equal(t, 4, details.Requests)
	assert.Len(t, details.Items, 120)
	assert.Equal(t, 1+2+3+4, details.RateLimit.Cost, "the cost adds up across requests")
	assert.Equal(t, 4960, details.RateLimit.Remaining, "the budget is the lowest reading, the latest")
}

func TestDetailsBatches(t *testing.T) {
	t.Parallel()

	ids := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("I_fakeNodeId%d", i)
		}

		return out
	}

	sizes := func(batches [][]string) []int {
		out := make([]int, 0, len(batches))
		for _, b := range batches {
			out = append(out, len(b))
		}

		return out
	}

	tests := []struct {
		items int
		want  []int
	}{
		{items: 0, want: []int{}},
		{items: 3, want: []int{3}},
		{items: 10, want: []int{10}},
		{items: 20, want: []int{10, 10}},
		{items: 41, want: []int{11, 11, 11, 8}},
		{items: 300, want: []int{50, 50, 50, 50, 50, 50}},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.items), func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, sizes(detailsBatches(ids(tt.items))))
		})
	}
}

func TestDetailsAdd(t *testing.T) {
	t.Parallel()

	first := &Details{
		Items:     map[string]ItemDetails{"I_a": {Comments: 1}},
		RateLimit: QueryRate{Cost: 1, Limit: 5000, Remaining: 4990},
		Requests:  1,
	}

	first.Add(nil)
	assert.Equal(t, 1, first.Requests, "nothing to add")

	first.Add(&Details{Items: map[string]ItemDetails{"I_b": {Comments: 2}}, RateLimit: QueryRate{Cost: 2, Limit: 5000, Remaining: 4995}, Requests: 1})
	assert.Equal(t, QueryRate{Cost: 3, Limit: 5000, Remaining: 4990}, first.RateLimit, "the lower reading is kept")

	first.Add(&Details{Items: map[string]ItemDetails{}, RateLimit: QueryRate{Cost: 1, Limit: 5000, Remaining: 4980}, Requests: 1})
	assert.Equal(t, QueryRate{Cost: 4, Limit: 5000, Remaining: 4980}, first.RateLimit)
	assert.Equal(t, 3, first.Requests)
	assert.Len(t, first.Items, 2)

	// Adding to nothing takes the other's reading, whatever it is.
	empty := &Details{}
	empty.Add(&Details{RateLimit: QueryRate{Cost: 1, Limit: 5000, Remaining: 4999}, Requests: 1})
	assert.Equal(t, QueryRate{Cost: 1, Limit: 5000, Remaining: 4999}, empty.RateLimit)
	assert.NotNil(t, empty.Items)

	// An answer with no requests behind it does not replace a reading.
	empty.Add(&Details{})
	assert.Equal(t, 4999, empty.RateLimit.Remaining)
}

func TestFetchDetailsWithoutIDs(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(http.ResponseWriter, *http.Request) {
		t.Error("nothing is requested without IDs")
	})

	details, err := client.FetchDetails(t.Context(), NewToken(fakeToken), nil, DetailOptions{})
	require.NoError(t, err)
	assert.Zero(t, details.Requests)
	assert.Empty(t, details.Items)
}

func TestFetchDetailsFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{name: "server error", status: http.StatusBadGateway, body: `{"message":"bad gateway"}`, wantErr: ErrUnexpectedStatus},
		{name: "errors without data", status: http.StatusOK, body: `{"errors":[{"message":"timeout"}]}`, wantErr: ErrGraphQL},
		{name: "neither data nor errors", status: http.StatusOK, body: `{}`, wantErr: ErrMalformedResponse},
		{name: "not json", status: http.StatusOK, body: `<html>`, wantErr: ErrMalformedResponse},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			})

			_, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{"I_fakeNodeId1"}, DetailOptions{})
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestCheckOutcomes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state string
		run   outcome
		other outcome
	}{
		{"SUCCESS", outcomePassed, outcomePassed},
		{"NEUTRAL", outcomePassed, outcomePending},
		{"SKIPPED", outcomeSkipped, outcomePending},
		{"FAILURE", outcomeFailed, outcomeFailed},
		{"ERROR", outcomePending, outcomeFailed},
		{"TIMED_OUT", outcomeFailed, outcomePending},
		{"CANCELLED", outcomeFailed, outcomePending}, //nolint:misspell // GitHub's enum value
		{"ACTION_REQUIRED", outcomeFailed, outcomePending},
		{"STARTUP_FAILURE", outcomeFailed, outcomePending},
		{"STALE", outcomeFailed, outcomePending},
		{"QUEUED", outcomePending, outcomePending},
		{"IN_PROGRESS", outcomePending, outcomePending},
		{"WAITING", outcomePending, outcomePending},
		{"PENDING", outcomePending, outcomePending},
		{"EXPECTED", outcomePending, outcomePending},
		{"SOMETHING_NEW", outcomePending, outcomePending},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.run, checkRunOutcome(tt.state), "check run")
			assert.Equal(t, tt.other, statusOutcome(tt.state), "commit status")
		})
	}
}

func TestLabelColor(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"0e8a16":  "#0e8a16",
		"FBCA04":  "#fbca04",
		"":        "",
		"fff":     "",
		"0e8a1g":  "",
		"#0e8a16": "",
	}

	for in, want := range tests {
		assert.Equal(t, want, labelColor(in), in)
	}
}

func TestDetailsNodeSkipsWhatIsNotAnItem(t *testing.T) {
	t.Parallel()

	for _, n := range []*detailsNode{nil, {TypeName: typePullRequest}, {TypeName: "Discussion", ID: "D_fakeNodeId"}} {
		_, _, ok := n.details()
		assert.False(t, ok)
	}
}

func TestReviewerKey(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "user:octo-user", reviewerKey("User", "Octo-User", ""))
	assert.Equal(t, "user:octo-bot", reviewerKey("Bot", "octo-bot", ""))
	assert.Equal(t, "team:reviewers", reviewerKey("Team", "", "Reviewers"))
	assert.Empty(t, reviewerKey("Team", "", ""))
	assert.Empty(t, reviewerKey("User", "", ""))
}
