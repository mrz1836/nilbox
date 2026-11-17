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

func TestRequestReviewSendsOnePost(t *testing.T) {
	t.Parallel()

	client, got := editServer(t, http.StatusCreated, `{
  "updated_at": "2026-01-10T12:30:00Z",
  "requested_reviewers": [{"login": "someone"}, {"login": "Octo-User"}, {"login": ""}],
  "requested_teams": [{"slug": "backend"}]
}`, nil)
	gate := &fakeGate{}
	rates := &recordRates{}
	client.Writes, client.Rates = gate, rates

	edited, err := client.RequestReview(t.Context(), NewToken(fakeToken), editRef(), octoUser)
	require.NoError(t, err)

	assert.Equal(t, &Edited{
		UpdatedAt:      time.Date(2026, 1, 10, 12, 30, 0, 0, time.UTC),
		ReviewRequests: ReviewRequests{Users: []string{"someone", "Octo-User"}, Teams: []string{"backend"}},
	}, edited, "who GitHub asks now, the user among them")
	assert.Equal(t, []editRequest{{
		method: http.MethodPost, path: "/repos/acme-org/example-repo/pulls/42/requested_reviewers",
		body: map[string][]string{"reviewers": {octoUser}},
	}}, *got, "one request, naming only the user")
	assert.Equal(t, []string{ResourceCore}, gate.allowed, "the gate counted it")

	spent := rates.all()
	require.Len(t, spent, 1)
	assert.Equal(t, 1, spent[0].spent, "an edit spends one REST point")
}

func TestRequestReviewSeesARequestGitHubIgnored(t *testing.T) {
	t.Parallel()

	client, _ := editServer(t, http.StatusCreated,
		`{"updated_at":"2026-01-10T12:30:00Z","requested_reviewers":[{"login":"someone"}],"requested_teams":[]}`, nil)

	_, err := client.RequestReview(t.Context(), NewToken(fakeToken), editRef(), octoUser)
	require.ErrorIs(t, err, ErrEditIgnored)
	assert.Equal(t, "GitHub didn't ask you to review acme-org/example-repo#42 again. "+
		"Your account may need write access to the repository.", uierr.Message(err))
}

func TestRequestReviewWordsARefusal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{
			name: "not a collaborator", status: http.StatusUnprocessableEntity,
			body: `{"message":"Reviews may only be requested from collaborators. One or more of the users or teams ` +
				`you specified is not a collaborator of the acme-org/example-repo repository."}`,
			want: "GitHub didn't ask you to review acme-org/example-repo#42 again: Reviews may only be requested from " +
				"collaborators. One or more of the users or teams you specified is not a collaborator of the " +
				"acme-org/example-repo repository.",
		},
		{
			name: "unprocessable, without words", status: http.StatusUnprocessableEntity, body: `{}`,
			want: "GitHub didn't ask you to review acme-org/example-repo#42 again. " +
				"Your account may need write access to the repository.",
		},
		{
			name: "a pull request GitHub can't find", status: http.StatusNotFound, body: `{"message":"Not Found"}`,
			want: "GitHub could not find acme-org/example-repo#42, or your account cannot see it.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, got := editServer(t, tt.status, tt.body, nil)

			_, err := client.RequestReview(t.Context(), NewToken(fakeToken), editRef(), octoUser)
			require.Error(t, err)

			var status *StatusError
			require.ErrorAs(t, err, &status, "the refusal stays in the chain")
			assert.Equal(t, tt.want, uierr.Message(err))
			assert.Len(t, *got, 1, "sent once, never again on its own")
		})
	}
}

func TestEditsWaitForTheGate(t *testing.T) {
	t.Parallel()

	client, got := editServer(t, http.StatusOK, `{}`, nil)
	client.Writes = &fakeGate{refuse: errGateClosed}

	_, err := client.RemoveAssignee(t.Context(), NewToken(fakeToken), editRef(), "octo-user")
	require.ErrorIs(t, err, errGateClosed)

	_, err = client.RemoveReviewRequest(t.Context(), NewToken(fakeToken), editRef(), "octo-user")
	require.ErrorIs(t, err, errGateClosed)

	_, err = client.RequestReview(t.Context(), NewToken(fakeToken), editRef(), octoUser)
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

		_, err = client.RequestReview(t.Context(), NewToken(fakeToken), editRef(), login)
		require.ErrorIs(t, err, ErrInvalidLogin, login)
	}

	_, err = client.RequestReview(t.Context(), NewToken(fakeToken), item.Ref{Number: 1}, octoUser)
	require.ErrorIs(t, err, ErrInvalidItemRef)

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
		assert.Contains(t, string(raw), "viewer { login name url }")

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

func TestFetchSignInReadsTheAccountAndItsScopes(t *testing.T) {
	t.Parallel()

	client, rates := observingClient(t, func(w http.ResponseWriter, _ *http.Request) {
		setRateHeaders(w, ResourceGraphQL, 4999, 1)
		w.Header().Set("X-OAuth-Scopes", "repo, read:org, read:project")
		_, _ = io.WriteString(w, `{"data":{"viewer":{"login":"octo-user","name":"Octo User",`+
			`"url":"https://github.com/octo-user"},"rateLimit":{"cost":1}}}`)
	})

	in, err := client.FetchSignIn(t.Context(), NewToken(fakeToken))
	require.NoError(t, err)
	assert.Equal(t, SignIn{
		Login: octoUser, Name: "Octo User", URL: "https://github.com/octo-user", Scopes: "repo, read:org, read:project",
	}, in)

	total := 0
	for _, o := range rates.all() {
		total += o.spent
	}

	assert.Equal(t, 1, total, "one query, whose point the budget counts")
}

func TestFetchSignInKeepsTheTokenOutOfItsErrors(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"Bad credentials `+fakeToken+`"}`)
	})

	_, err := client.FetchSignIn(t.Context(), NewToken(fakeToken))
	require.Error(t, err)
	assert.True(t, IsBadCredentials(err), "the setup tells a refused token apart")
	assert.NotContains(t, err.Error(), fakeToken)
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
