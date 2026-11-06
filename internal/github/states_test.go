package github

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statesBody is a states response: a merged pull request, a closed issue, an open pull
// request, and a ref GitHub could not find.
const statesBody = `{
  "data": {
    "r0": {"item": {"__typename": "PullRequest", "title": "Add locktimes", "url": "https://github.com/acme-org/example-repo/pull/184",
      "prState": "MERGED", "closedAt": "2026-11-06T18:00:00Z", "mergedAt": "2026-11-06T18:00:05Z", "headRefOid": "fc1c3975011e66491785ace891c893e4fc716587"}},
    "r1": {"item": {"__typename": "Issue", "title": "Crash on start", "url": "https://github.com/acme-org/example-repo/issues/7",
      "issueState": "CLOSED", "closedAt": "2026-11-05T10:00:00Z"}},
    "r2": {"item": {"__typename": "PullRequest", "title": "Still going", "url": "https://github.com/acme-org/other-repo/pull/3",
      "prState": "OPEN", "closedAt": null, "mergedAt": null, "headRefOid": "abc"}},
    "r3": null
  },
  "errors": [{"type": "NOT_FOUND", "message": "Could not resolve to a Repository with the name 'acme-org/gone'."}]
}`

func TestFetchItemStatesReadsEachRef(t *testing.T) {
	refs := []ItemRef{
		{Owner: "acme-org", Name: "example-repo", Number: 184},
		{Owner: "acme-org", Name: "example-repo", Number: 7},
		{Owner: "acme-org", Name: "other-repo", Number: 3},
		{Owner: "acme-org", Name: "gone", Number: 1},
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
	}, states[refs[0]], "a merged pull request is dated by its merge")

	assert.Equal(t, ItemState{
		Title:    "Crash on start",
		URL:      "https://github.com/acme-org/example-repo/issues/7",
		State:    StateClosed,
		ClosedAt: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC),
	}, states[refs[1]])

	assert.Equal(t, StateOpen, states[refs[2]].State)
	assert.True(t, states[refs[2]].ClosedAt.IsZero())
	assert.NotContains(t, states, refs[3], "a ref GitHub cannot find is left out")
}

func TestFetchItemStatesBatchesLargeRequests(t *testing.T) {
	var requests atomic.Int32

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)

		_, _ = io.WriteString(w, `{"data": {}}`)
	})

	refs := make([]ItemRef, statesBatch+1)
	for i := range refs {
		refs[i] = ItemRef{Owner: "acme-org", Name: "example-repo", Number: i + 1}
	}

	states, err := client.FetchItemStates(t.Context(), NewToken(fakeToken), refs)
	require.NoError(t, err)
	assert.Empty(t, states)
	assert.Equal(t, int32(2), requests.Load())
}

func TestFetchItemStatesFailures(t *testing.T) {
	ref := []ItemRef{{Owner: "acme-org", Name: "example-repo", Number: 1}}

	tests := []struct {
		name    string
		refs    []ItemRef
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
			refs:    []ItemRef{{Owner: "acme-org", Name: "example-repo", Number: 0}},
			wantErr: ErrInvalidItemRef,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

func TestFetchItemStatesNeedsAToken(t *testing.T) {
	client := newTestClient(t, func(http.ResponseWriter, *http.Request) {
		t.Error("no request is sent without a token")
	})

	_, err := client.FetchItemStates(t.Context(), Token{}, []ItemRef{{Owner: "acme-org", Name: "example-repo", Number: 1}})
	require.ErrorIs(t, err, ErrNotLoggedIn)
}

func TestStatesQueryQuotesNames(t *testing.T) {
	query, err := statesQuery([]ItemRef{{Owner: `acme"org`, Name: "example-repo", Number: 9}})
	require.NoError(t, err)
	assert.Contains(t, query, `owner: "acme\"org"`)
	assert.Equal(t, 1, strings.Count(query, "repository("))
}

func TestStatesNodeIgnoresOtherTypes(t *testing.T) {
	_, ok := (&statesRepo{Item: &statesNode{TypeName: "Discussion", Title: "x"}}).state()
	assert.False(t, ok)

	_, ok = (&statesRepo{Item: &statesNode{TypeName: typeIssue}}).state()
	assert.False(t, ok, "an item without a state is no answer")

	_, ok = (*statesRepo)(nil).state()
	assert.False(t, ok)
}
