package github

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The items query, the item query, and the issue view each read an issue's projects when
// asked, and when GitHub refuses them for want of the read:project scope they ask again
// without them and say so in the answer. These tests pin that as it is, read by read.

// scopeRead is one of the reads that ask again without the projects. plain is its answer
// to a read without them. read sends it with opts and returns whether the answer says the
// projects were refused, and the answer with that mark cleared, so the rest of it compares
// with a read that never asked for them.
type scopeRead struct {
	name  string
	plain func(t *testing.T) []byte
	read  func(t *testing.T, c *Client, opts DetailOptions) (bool, any, error)
}

// scopeReads are the reads that ask again without the projects.
func scopeReads() []scopeRead {
	return []scopeRead{
		{name: "items", plain: scopeItemsBody, read: func(t *testing.T, c *Client, opts DetailOptions) (bool, any, error) {
			t.Helper()

			got, err := c.FetchItems(t.Context(), NewToken(fakeToken), []string{"PR_fakeNodeId12", issueID, "PR_fakeGone"}, opts)
			if err != nil {
				return false, got, err
			}

			refused := got.ProjectsRefused
			got.ProjectsRefused = false

			return refused, got, nil
		}},
		{name: "item, a pull request", plain: func(t *testing.T) []byte {
			t.Helper()

			return fixture(t, "item_response.json")
		}, read: func(t *testing.T, c *Client, opts DetailOptions) (bool, any, error) {
			t.Helper()

			return scopeItem(t, c, "PR_fakeNodeId12", opts)
		}},
		{name: "item, an issue", plain: func(t *testing.T) []byte {
			t.Helper()

			return []byte(issueItem(""))
		}, read: func(t *testing.T, c *Client, opts DetailOptions) (bool, any, error) {
			t.Helper()

			return scopeItem(t, c, issueID, opts)
		}},
		{name: "issue view", plain: func(t *testing.T) []byte {
			t.Helper()

			return fixture(t, "issue_view_response.json")
		}, read: func(t *testing.T, c *Client, opts DetailOptions) (bool, any, error) {
			t.Helper()

			got, err := c.FetchIssueView(t.Context(), NewToken(fakeToken), "I_fakeNodeId12", opts)
			if err != nil {
				return false, got, err
			}

			refused := got.Update.ProjectsRefused
			got.Update.ProjectsRefused = false

			return refused, got, nil
		}},
	}
}

// scopeItem reads the item with node ID id with opts, as a scopeRead does.
func scopeItem(t *testing.T, c *Client, id string, opts DetailOptions) (bool, any, error) {
	t.Helper()

	got, err := c.FetchItem(t.Context(), NewToken(fakeToken), id, opts)
	if err != nil {
		return false, got, err
	}

	refused := got.ProjectsRefused
	got.ProjectsRefused = false

	return refused, got, nil
}

// scopeItemsBody is an items answer: the item fixture's pull request, issueItem's issue,
// and a null for an ID GitHub couldn't find, with its error.
func scopeItemsBody(t *testing.T) []byte {
	t.Helper()

	type single struct {
		Data struct {
			Node      json.RawMessage `json:"node"`
			RateLimit json.RawMessage `json:"rateLimit"`
		} `json:"data"`
	}

	var pull, issue single
	require.NoError(t, json.Unmarshal(fixture(t, "item_response.json"), &pull))
	require.NoError(t, json.Unmarshal([]byte(issueItem("")), &issue))

	body, err := json.Marshal(map[string]any{
		"data": map[string]any{
			"nodes":     []json.RawMessage{pull.Data.Node, issue.Data.Node, json.RawMessage("null")},
			"rateLimit": pull.Data.RateLimit,
		},
		"errors": []map[string]any{{"type": "NOT_FOUND", "message": "Could not resolve to a node"}},
	})
	require.NoError(t, err)

	return body
}

// withScopeError is the answer body with GitHub's refusal of the projects added to its
// errors, as GitHub sends when it answers the rest of the query.
func withScopeError(t *testing.T, body []byte) []byte {
	t.Helper()

	var answer map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &answer))

	errs := make([]json.RawMessage, 0, 1)
	if raw, ok := answer["errors"]; ok {
		require.NoError(t, json.Unmarshal(raw, &errs))
	}

	errs = append(errs, json.RawMessage(`{"type":"INSUFFICIENT_SCOPES","message":"read:project"}`))

	raw, err := json.Marshal(errs)
	require.NoError(t, err)

	answer["errors"] = raw

	out, err := json.Marshal(answer)
	require.NoError(t, err)

	return out
}

// heardRequests records the GraphQL requests a fake GitHub hears, in order.
type heardRequests struct {
	mu   sync.Mutex
	list []graphQLRequest
}

// hear decodes and records the request r carries, and returns it.
func (h *heardRequests) hear(t *testing.T, r *http.Request) graphQLRequest {
	t.Helper()

	var req graphQLRequest

	err := json.NewDecoder(r.Body).Decode(&req)

	h.mu.Lock()
	h.list = append(h.list, req)
	h.mu.Unlock()

	assert.NoError(t, err)

	return req
}

// all returns the requests heard so far.
func (h *heardRequests) all() []graphQLRequest {
	h.mu.Lock()
	defer h.mu.Unlock()

	return slices.Clone(h.list)
}

// readsProjects reports whether a request's query reads the projects.
func readsProjects(req graphQLRequest) bool {
	return strings.Contains(req.Query, "projectItems(")
}

// refusingProjects is a fake GitHub that records what it hears, refuses every request that
// reads the projects for want of the scope, and answers any other with other.
func refusingProjects(t *testing.T, heard *heardRequests, other func(w http.ResponseWriter)) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		if readsProjects(heard.hear(t, r)) {
			_, _ = io.WriteString(w, refusedProjects)

			return
		}

		other(w)
	}
}

// answering is a fake GitHub that records what it hears and answers every request with
// body.
func answering(t *testing.T, heard *heardRequests, body []byte) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		heard.hear(t, r)

		_, _ = w.Write(body)
	}
}

func TestTheReadsAskAgainWithoutTheProjectsWhenTheScopeIsMissing(t *testing.T) {
	t.Parallel()

	for _, read := range scopeReads() {
		t.Run(read.name, func(t *testing.T) {
			t.Parallel()

			body := read.plain(t)

			plainHeard := &heardRequests{}
			refused, want, err := read.read(t, newTestClient(t, answering(t, plainHeard, body)), DetailOptions{})
			require.NoError(t, err)
			require.False(t, refused, "a read that never asked for the projects")

			heard := &heardRequests{}
			client := newTestClient(t, refusingProjects(t, heard, func(w http.ResponseWriter) {
				_, _ = w.Write(body)
			}))

			refused, got, err := read.read(t, client, DetailOptions{Projects: true})
			require.NoError(t, err, "a missing read:project scope never fails the read")

			assert.True(t, refused, "the answer says the projects were refused")
			assert.Equal(t, want, got, "the rest of the answer is a read without the projects")

			requests := heard.all()
			require.Len(t, requests, 2, "asked once more")
			assert.True(t, readsProjects(requests[0]))
			assert.False(t, readsProjects(requests[1]), "the second request leaves the projects out")
			assert.Equal(t, plainHeard.all(), requests[1:], "and is a read that never asked for them")
		})
	}
}

func TestTheReadsAskAgainWithoutTheProjectsWhateverElseTheyAskFor(t *testing.T) {
	t.Parallel()

	// The items, the item, and the issue view read nothing but the projects of what
	// DetailOptions holds, so the second request is a plain read's however much else
	// the first asked for.
	for _, read := range scopeReads() {
		t.Run(read.name, func(t *testing.T) {
			t.Parallel()

			body := read.plain(t)

			plainHeard := &heardRequests{}
			_, want, err := read.read(t, newTestClient(t, answering(t, plainHeard, body)), DetailOptions{})
			require.NoError(t, err)

			heard := &heardRequests{}
			client := newTestClient(t, refusingProjects(t, heard, func(w http.ResponseWriter) {
				_, _ = w.Write(body)
			}))

			refused, got, err := read.read(t, client, DetailOptions{
				Projects: true, Repository: true, Batch: 3, Partial: true, Progress: func(int) {},
			})
			require.NoError(t, err)

			assert.True(t, refused)
			assert.Equal(t, want, got)

			requests := heard.all()
			require.Len(t, requests, 2)
			assert.Equal(t, plainHeard.all(), requests[1:])
		})
	}
}

func TestTheReadsFailWhenTheSecondTryFails(t *testing.T) {
	t.Parallel()

	for _, read := range scopeReads() {
		t.Run(read.name, func(t *testing.T) {
			t.Parallel()

			heard := &heardRequests{}
			client := newTestClient(t, refusingProjects(t, heard, func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(w, `{"message":"bad gateway"}`)
			}))

			refused, got, err := read.read(t, client, DetailOptions{Projects: true})
			require.ErrorIs(t, err, ErrUnexpectedStatus)
			assert.NotContains(t, err.Error(), fakeToken)
			assert.False(t, refused)
			assert.Nil(t, got, "nothing is answered")
			assert.Len(t, heard.all(), 2)
		})
	}
}

func TestTheReadsFailOnAScopeRefusalTheyDidNotAskFor(t *testing.T) {
	t.Parallel()

	for _, read := range scopeReads() {
		t.Run(read.name, func(t *testing.T) {
			t.Parallel()

			heard := &heardRequests{}
			client := newTestClient(t, answering(t, heard, []byte(refusedProjects)))

			_, got, err := read.read(t, client, DetailOptions{})
			require.ErrorIs(t, err, ErrGraphQL)
			assert.Contains(t, err.Error(), "read:project")
			assert.Nil(t, got)
			assert.Len(t, heard.all(), 1, "only a refusal of the projects is asked again")
		})
	}
}

func TestTheReadsKeepTheDataGitHubSendsWithARefusal(t *testing.T) {
	t.Parallel()

	for _, read := range scopeReads() {
		t.Run(read.name, func(t *testing.T) {
			t.Parallel()

			body := read.plain(t)

			_, want, err := read.read(t, newTestClient(t, answering(t, &heardRequests{}, body)), DetailOptions{})
			require.NoError(t, err)

			refusing := withScopeError(t, body)

			heard := &heardRequests{}
			refused, got, err := read.read(t, newTestClient(t, answering(t, heard, refusing)), DetailOptions{Projects: true})
			require.NoError(t, err)

			assert.True(t, refused, "GitHub refused the projects it was asked for")
			assert.Equal(t, want, got)
			assert.Len(t, heard.all(), 1, "the data came with the refusal, so nothing is asked again")

			// A refusal with data of projects it never asked for is no refusal of its own.
			refused, got, err = read.read(t, newTestClient(t, answering(t, &heardRequests{}, refusing)), DetailOptions{})
			require.NoError(t, err)

			assert.False(t, refused)
			assert.Equal(t, want, got)
		})
	}
}

func TestTheIssueViewAsksAgainWithoutTheProjectsAtTheFullPage(t *testing.T) {
	t.Parallel()

	body := fixture(t, "issue_view_response.json")
	huge := `{"data":{"node":{"body":"` + strings.Repeat("x", maxResponseBytes) + `"}}}`

	// try is one request of the view: whether it reads the projects, and how much of the
	// timeline.
	type try struct {
		projects bool
		last     int
	}

	tryOf := func(req graphQLRequest) try {
		last, _ := req.Variables["last"].(float64)

		return try{projects: readsProjects(req), last: int(last)}
	}

	for _, tc := range []struct {
		name     string
		tooLarge try
		want     []try
	}{
		{
			name:     "too large with the projects",
			tooLarge: try{projects: true, last: timelinePage},
			want:     []try{{true, timelinePage}, {true, timelineSmallPage}, {false, timelinePage}},
		},
		{
			name:     "too large without them",
			tooLarge: try{projects: false, last: timelinePage},
			want:     []try{{true, timelinePage}, {false, timelinePage}, {false, timelineSmallPage}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			heard := &heardRequests{}

			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				got := tryOf(heard.hear(t, r))

				switch {
				case got == tc.tooLarge:
					_, _ = io.WriteString(w, huge)
				case got.projects:
					_, _ = io.WriteString(w, refusedProjects)
				default:
					_, _ = w.Write(body)
				}
			})

			view, err := client.FetchIssueView(t.Context(), NewToken(fakeToken), "I_fakeNodeId12",
				DetailOptions{Projects: true})
			require.NoError(t, err)

			assert.True(t, view.Update.ProjectsRefused)
			assert.True(t, view.Update.Found)

			requests := heard.all()
			tries := make([]try, 0, len(requests))

			for _, req := range requests {
				tries = append(tries, tryOf(req))
			}

			assert.Equal(t, tc.want, tries,
				"the second read starts again at the full page, and pages down on its own")
		})
	}
}
