package github

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

var errGateClosed = errors.New("the gate is closed")

// editRef is the item the edit tests change.
func editRef() item.Ref {
	return item.Ref{Repo: repo.Name{Owner: "acme-org", Repo: exampleRepo}, Number: 42, Kind: item.KindPR}
}

// fakeGate is a WriteGate that allows edits until refuse is set, and records them.
type fakeGate struct {
	mu      sync.Mutex
	refuse  error
	allowed []string
	held    []time.Duration
}

func (g *fakeGate) AllowWrite(resource string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.refuse != nil {
		return g.refuse
	}

	g.allowed = append(g.allowed, resource)

	return nil
}

func (g *fakeGate) HoldWrites(wait time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.held = append(g.held, wait)
}

// editRequest is what the fake GitHub received.
type editRequest struct {
	method, path string
	body         map[string][]string
}

// editServer answers every request with status and body, recording each, with a REST
// budget reading and any extra headers.
func editServer(t *testing.T, status int, body string, headers map[string]string) (*Client, *[]editRequest) {
	t.Helper()

	var got []editRequest

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		req := editRequest{method: r.Method, path: r.URL.Path}
		if len(raw) > 0 {
			assert.NoError(t, json.Unmarshal(raw, &req.body))
		}

		got = append(got, req)

		setRateHeaders(w, ResourceCore, 4900, 100)

		for k, v := range headers {
			w.Header().Set(k, v)
		}

		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})

	return client, &got
}

func TestRemoveAssigneeSendsOneEdit(t *testing.T) {
	t.Parallel()

	client, got := editServer(t, http.StatusOK,
		`{"updated_at":"2026-01-10T12:30:00Z","assignees":[{"login":"someone"}]}`, nil)
	gate := &fakeGate{}
	rates := &recordRates{}
	client.Writes, client.Rates = gate, rates

	edited, err := client.RemoveAssignee(t.Context(), NewToken(fakeToken), editRef(), "octo-user")
	require.NoError(t, err)

	assert.Equal(t, &Edited{UpdatedAt: time.Date(2026, 1, 10, 12, 30, 0, 0, time.UTC)}, edited)
	assert.Equal(t, []editRequest{{
		method: http.MethodDelete, path: "/repos/acme-org/example-repo/issues/42/assignees",
		body: map[string][]string{"assignees": {"octo-user"}},
	}}, *got)
	assert.Equal(t, []string{ResourceCore}, gate.allowed, "the gate counted it")

	spent := rates.all()
	require.Len(t, spent, 1)
	assert.Equal(t, 1, spent[0].spent, "an edit spends one REST point")
}

func TestRemoveAssigneeSeesAnEditGitHubIgnored(t *testing.T) {
	t.Parallel()

	client, _ := editServer(t, http.StatusOK,
		`{"updated_at":"2026-01-10T12:30:00Z","assignees":[{"login":"Octo-User"}]}`, nil)

	_, err := client.RemoveAssignee(t.Context(), NewToken(fakeToken), editRef(), "octo-user")
	require.ErrorIs(t, err, ErrEditIgnored)
	assert.Equal(t, "GitHub kept you assigned to acme-org/example-repo#42. "+
		"Your account may not be allowed to change its assignees.", uierr.Message(err))
}

func TestRemoveReviewRequestSaysWhoIsStillAsked(t *testing.T) {
	t.Parallel()

	client, got := editServer(t, http.StatusOK, `{
  "updated_at": "2026-01-10T12:30:00Z",
  "requested_reviewers": [{"login": "someone"}, {"login": ""}],
  "requested_teams": [{"slug": "backend"}]
}`, nil)

	edited, err := client.RemoveReviewRequest(t.Context(), NewToken(fakeToken), editRef(), "octo-user")
	require.NoError(t, err)

	assert.Equal(t, &Edited{
		UpdatedAt:      time.Date(2026, 1, 10, 12, 30, 0, 0, time.UTC),
		ReviewRequests: ReviewRequests{Users: []string{"someone"}, Teams: []string{"backend"}},
	}, edited)
	assert.Equal(t, []editRequest{{
		method: http.MethodDelete, path: "/repos/acme-org/example-repo/pulls/42/requested_reviewers",
		body: map[string][]string{"reviewers": {"octo-user"}},
	}}, *got)
}

func TestRemoveReviewRequestSeesAnEditGitHubIgnored(t *testing.T) {
	t.Parallel()

	client, _ := editServer(t, http.StatusOK,
		`{"updated_at":"2026-01-10T12:30:00Z","requested_reviewers":[{"login":"OCTO-USER"}],"requested_teams":[]}`, nil)

	_, err := client.RemoveReviewRequest(t.Context(), NewToken(fakeToken), editRef(), "octo-user")
	require.ErrorIs(t, err, ErrEditIgnored)
}

func TestEditsWaitForTheGate(t *testing.T) {
	t.Parallel()

	client, got := editServer(t, http.StatusOK, `{}`, nil)
	client.Writes = &fakeGate{refuse: errGateClosed}

	_, err := client.RemoveAssignee(t.Context(), NewToken(fakeToken), editRef(), "octo-user")
	require.ErrorIs(t, err, errGateClosed)

	_, err = client.RemoveReviewRequest(t.Context(), NewToken(fakeToken), editRef(), "octo-user")
	require.ErrorIs(t, err, errGateClosed)

	assert.Empty(t, *got, "nothing was sent")
}

func TestEditsRefuseABadItemOrLogin(t *testing.T) {
	t.Parallel()

	client, got := editServer(t, http.StatusOK, `{}`, nil)
	gate := &fakeGate{}
	client.Writes = gate

	_, err := client.RemoveAssignee(t.Context(), NewToken(fakeToken), item.Ref{Number: 1}, "octo-user")
	require.ErrorIs(t, err, ErrInvalidItemRef)

	for _, login := range []string{"", "acme-org/octo", "-octo", "octo user"} {
		_, err = client.RemoveReviewRequest(t.Context(), NewToken(fakeToken), editRef(), login)
		require.ErrorIs(t, err, ErrInvalidLogin, login)
	}

	assert.Empty(t, *got)
	assert.Empty(t, gate.allowed, "nothing was counted")
}

func TestAnEditWithoutATokenIsNotCounted(t *testing.T) {
	t.Parallel()

	client, got := editServer(t, http.StatusOK, `{}`, nil)
	gate := &fakeGate{}
	client.Writes = gate

	_, err := client.RemoveAssignee(t.Context(), Token{}, editRef(), "octo-user")
	require.Error(t, err)
	assert.Empty(t, *got)
	assert.Empty(t, gate.allowed)
}

func TestARateLimitedEditHoldsTheNext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		body    string
		headers map[string]string
		want    time.Duration
	}{
		{
			name: "retry-after", status: http.StatusForbidden,
			body:    `{"message":"You have exceeded a secondary rate limit."}`,
			headers: map[string]string{"Retry-After": "75"}, want: 75 * time.Second,
		},
		{
			name: "secondary limit without a wait", status: http.StatusForbidden,
			body: `{"message":"You have exceeded a secondary rate limit."}`, want: time.Minute,
		},
		{name: "too many requests", status: http.StatusTooManyRequests, body: `{}`, want: time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, _ := editServer(t, tt.status, tt.body, tt.headers)
			gate := &fakeGate{}
			client.Writes = gate

			_, err := client.RemoveAssignee(t.Context(), NewToken(fakeToken), editRef(), "octo-user")
			require.Error(t, err)
			assert.Equal(t, "GitHub is limiting how fast your account can make changes. Try again in a minute.",
				uierr.Message(err))
			assert.Equal(t, []time.Duration{tt.want}, gate.held)
		})
	}
}

func TestRefusedEditsSayWhy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{
			name: "not found", status: http.StatusNotFound, body: `{"message":"Not Found"}`,
			want: "GitHub could not find acme-org/example-repo#42, or your account cannot see it.",
		},
		{
			name: "forbidden", status: http.StatusForbidden,
			body: `{"message":"Resource protected by organization SAML enforcement"}`,
			want: "GitHub refused the change to acme-org/example-repo#42: Resource protected by organization SAML enforcement.",
		},
		{
			name: "forbidden without a message", status: http.StatusForbidden, body: `{}`,
			want: "GitHub refused the change to acme-org/example-repo#42. Your account may not be allowed to make it.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, _ := editServer(t, tt.status, tt.body, nil)
			gate := &fakeGate{}
			client.Writes = gate

			_, err := client.RemoveReviewRequest(t.Context(), NewToken(fakeToken), editRef(), "octo-user")
			require.ErrorIs(t, err, ErrUnexpectedStatus)
			assert.Equal(t, tt.want, uierr.Message(err))
			assert.Empty(t, gate.held, "a refusal is no reason to wait")
		})
	}
}

func TestARefusedTokenStaysRecognizable(t *testing.T) {
	t.Parallel()

	client, _ := editServer(t, http.StatusUnauthorized, `{"message":"Bad credentials"}`, nil)

	_, err := client.RemoveAssignee(t.Context(), NewToken(fakeToken), editRef(), "octo-user")
	assert.True(t, IsBadCredentials(err), "WithToken retries it with a fresh token")
	assert.NotContains(t, err.Error(), fakeToken)
}

func TestFetchViewerReadsTheTokensAccount(t *testing.T) {
	t.Parallel()

	client, rates := observingClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Contains(t, string(raw), "viewer { login }")

		setRateHeaders(w, ResourceGraphQL, 4999, 1)
		_, _ = io.WriteString(w, `{"data":{"viewer":{"login":"octo-user"},"rateLimit":{"cost":1}}}`)
	})

	login, err := client.FetchViewer(t.Context(), NewToken(fakeToken))
	require.NoError(t, err)
	assert.Equal(t, "octo-user", login)

	total := 0
	for _, o := range rates.all() {
		total += o.spent
	}

	assert.Equal(t, 1, total, "the check spends its query's point")
}

func TestFetchViewerRefusesAnAnswerWithoutALogin(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"viewer":{"login":""}}}`)
	})

	_, err := client.FetchViewer(t.Context(), NewToken(fakeToken))
	require.ErrorIs(t, err, ErrMalformedResponse)
}

func TestTokenSame(t *testing.T) {
	t.Parallel()

	assert.True(t, NewToken("abc").Same(NewToken("abc")))
	assert.False(t, NewToken("abc").Same(NewToken("abd")))
	assert.False(t, Token{}.Same(Token{}), "no token is the same as no other")
	assert.False(t, Token{}.Same(NewToken("abc")))
}

func TestReviewRequestsAsks(t *testing.T) {
	t.Parallel()

	r := ReviewRequests{Users: []string{"Octo-User"}, Teams: []string{"octo-user"}}

	assert.True(t, r.Asks("octo-user"), "logins ignore case")
	assert.False(t, r.Asks("someone"), "a team of the same name is not the user")
}
