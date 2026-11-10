package github

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/uierr"
)

// errNoAnswer stands for a request GitHub never answered.
var errNoAnswer = errors.New("no answer")

// testMutation is a mutation the mutate tests send.
const testMutation = `mutation Test($id: ID!) { touch(input: {id: $id}) { clientMutationId } }`

// touchData is testMutation's data object.
type touchData struct {
	Touch struct {
		ClientMutationID string `json:"clientMutationId"`
	} `json:"touch"`
}

// graphQLServer answers every request with status and body, with a GraphQL budget
// reading and any extra headers, and counts the requests.
func graphQLServer(t *testing.T, status int, body string, headers map[string]string) (*Client, *int) {
	t.Helper()

	calls := 0

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++

		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, graphQLPath, r.URL.Path)

		_, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		setRateHeaders(w, ResourceGraphQL, 4990, 10)

		for k, v := range headers {
			w.Header().Set(k, v)
		}

		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})

	return client, &calls
}

func TestMutateSendsOneGatedWrite(t *testing.T) {
	t.Parallel()

	client, calls := graphQLServer(t, http.StatusOK, `{"data":{"touch":{"clientMutationId":"x"}}}`, nil)
	gate := &fakeGate{}
	rates := &recordRates{}
	client.Writes, client.Rates = gate, rates

	got, err := mutate[touchData](t.Context(), client, NewToken(fakeToken), "touch",
		graphQLRequest{Query: testMutation, Variables: map[string]any{"id": "PR_1"}})
	require.NoError(t, err)

	assert.Equal(t, "x", got.Touch.ClientMutationID)
	assert.Equal(t, 1, *calls)
	assert.Equal(t, []string{ResourceGraphQL}, gate.allowed, "the gate counts it against the GraphQL budget")

	observed := rates.all()
	require.Len(t, observed, 2, "do observes the answer, and the mutation its one point")
	assert.Equal(t, 0, observed[0].spent)
	assert.Equal(t, mutationCost, observed[1].spent)
}

func TestMutateWaitsForTheGate(t *testing.T) {
	t.Parallel()

	client, calls := graphQLServer(t, http.StatusOK, `{"data":{}}`, nil)
	client.Writes = &fakeGate{refuse: errGateClosed}

	_, err := mutate[touchData](t.Context(), client, NewToken(fakeToken), "touch", graphQLRequest{Query: testMutation})
	require.ErrorIs(t, err, errGateClosed)
	assert.Zero(t, *calls, "a refused write is never sent")
}

func TestMutateWithoutATokenIsNotCounted(t *testing.T) {
	t.Parallel()

	client, calls := graphQLServer(t, http.StatusOK, `{"data":{}}`, nil)
	gate := &fakeGate{}
	client.Writes = gate

	_, err := mutate[touchData](t.Context(), client, Token{}, "touch", graphQLRequest{Query: testMutation})
	require.Error(t, err)
	assert.Empty(t, gate.allowed)
	assert.Zero(t, *calls)
}

func TestMutateRefusesAQuery(t *testing.T) {
	t.Parallel()

	client, calls := graphQLServer(t, http.StatusOK, `{"data":{}}`, nil)
	gate := &fakeGate{}
	client.Writes = gate

	_, err := mutate[touchData](t.Context(), client, NewToken(fakeToken), "touch", graphQLRequest{Query: viewerQuery})
	require.ErrorIs(t, err, ErrMalformedResponse)
	assert.Empty(t, gate.allowed)
	assert.Zero(t, *calls)
}

func TestMutateFailsOnAnyGraphQLError(t *testing.T) {
	t.Parallel()

	client, _ := graphQLServer(t, http.StatusOK,
		`{"data":{"touch":null},"errors":[{"type":"UNPROCESSABLE","message":"Pull request is in clean status"}]}`, nil)
	client.Writes = &fakeGate{}

	_, err := mutate[touchData](t.Context(), client, NewToken(fakeToken), "touch", graphQLRequest{Query: testMutation})
	require.ErrorIs(t, err, ErrGraphQL)
	assert.Contains(t, err.Error(), "clean status")
}

func TestARateLimitedMutationHoldsTheNext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		body    string
		headers map[string]string
		want    time.Duration
	}{
		{
			name: "graphql rate limit error", status: http.StatusOK,
			body: `{"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`, want: secondaryLimitWait,
		},
		{
			name: "retry after", status: http.StatusForbidden, body: `{"message":"You have exceeded a secondary rate limit"}`,
			headers: map[string]string{"Retry-After": "90"}, want: 90 * time.Second,
		},
		{name: "refusal", status: http.StatusOK, body: `{"errors":[{"type":"FORBIDDEN","message":"no"}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, _ := graphQLServer(t, tt.status, tt.body, tt.headers)
			gate := &fakeGate{}
			client.Writes = gate

			_, err := mutate[touchData](t.Context(), client, NewToken(fakeToken), "touch", graphQLRequest{Query: testMutation})
			require.Error(t, err)

			if tt.want == 0 {
				assert.Empty(t, gate.held)

				return
			}

			assert.Equal(t, []time.Duration{tt.want}, gate.held)
		})
	}
}

func TestQueriesRefuseAMutation(t *testing.T) {
	t.Parallel()

	client, calls := graphQLServer(t, http.StatusOK, `{"data":{}}`, nil)

	_, _, err := postGraphQL[touchData](t.Context(), client, NewToken(fakeToken), "touch",
		graphQLRequest{Query: "\n  " + testMutation}, anyError)
	require.ErrorIs(t, err, ErrMutationAsQuery)
	assert.Zero(t, *calls, "a mutation never goes out as a query")
}

func TestWriteErrorSaysWhatWentWrong(t *testing.T) {
	t.Parallel()

	ref := editRef()

	tests := []struct {
		name   string
		status int
		msg    string
		target error
		words  string
	}{
		{
			name: "merge pinned to an old head", status: http.StatusConflict, msg: "Head branch was modified. Review and try the merge again.",
			target: ErrPushedSince, words: "was pushed to since you opened it",
		},
		{
			name: "update pinned to an old head", status: http.StatusUnprocessableEntity,
			msg: "expected head sha didn't match current head ref.", target: ErrPushedSince, words: "was pushed to",
		},
		{
			name: "not mergeable", status: http.StatusMethodNotAllowed,
			msg: "At least 1 approving review is required by reviewers with write access", target: ErrNotMergeable,
			words: "GitHub won't merge acme-org/example-repo#42 yet: At least 1 approving review is required by reviewers with write access.",
		},
		{
			name: "merge queue", status: http.StatusUnprocessableEntity, msg: "Changes must be made through the merge queue",
			target: ErrNotMergeable, words: "merge queue",
		},
		{
			name: "own pull request", status: http.StatusUnprocessableEntity,
			msg: "Unprocessable Entity: Review Can not approve your own pull request", target: ErrOwnPull, words: "your own pull request",
		},
		{
			name: "pending review", status: http.StatusUnprocessableEntity,
			msg: "Unprocessable Entity: User can only have one pending review per pull request", target: ErrPendingReview,
			words: "not submitted",
		},
		{
			name: "line off the diff", status: http.StatusUnprocessableEntity,
			msg: "Unprocessable Entity: Pull request review thread line must be part of the diff", target: ErrLineNotInDiff,
			words: "isn't part of the diff",
		},
		{
			name: "line unresolved", status: http.StatusUnprocessableEntity, msg: "Validation Failed: Line could not be resolved",
			target: ErrLineNotInDiff, words: "isn't part of the diff",
		},
		{
			name: "other validation", status: http.StatusUnprocessableEntity, msg: "Validation Failed: body is too long",
			target: ErrUnexpectedStatus, words: "GitHub refused the change to acme-org/example-repo#42: Validation Failed: body is too long.",
		},
		{
			name: "forbidden", status: http.StatusForbidden, msg: "Must have push access to repository",
			target: ErrUnexpectedStatus, words: "GitHub refused the change to acme-org/example-repo#42: Must have push access to repository.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := writeError(ref, fmt.Errorf("merge: %w", &StatusError{StatusCode: tt.status, Message: tt.msg}))
			require.ErrorIs(t, err, tt.target)
			require.ErrorIs(t, err, ErrUnexpectedStatus, "GitHub's refusal stays in the chain")
			assert.Contains(t, uierr.Message(err), tt.words)
		})
	}
}

func TestWriteErrorKeepsWhatItCannotWord(t *testing.T) {
	t.Parallel()

	assert.Same(t, errNoAnswer, writeError(editRef(), errNoAnswer))

	creds := &StatusError{StatusCode: http.StatusUnauthorized, Message: "Bad credentials"}
	assert.Equal(t, error(creds), writeError(editRef(), creds))

	bare := &StatusError{StatusCode: http.StatusUnprocessableEntity}
	assert.Equal(t, error(bare), writeError(editRef(), bare), "a 422 with no words keeps its own")

	notMergeable := writeError(editRef(), &StatusError{StatusCode: http.StatusMethodNotAllowed})
	assert.Equal(t, "GitHub won't merge acme-org/example-repo#42 yet.", uierr.Message(notMergeable))
}

func TestNotMergeableErrorSaysWhy(t *testing.T) {
	t.Parallel()

	assert.Equal(t, ErrNotMergeable.Error(), (&NotMergeableError{}).Error())

	refused := &NotMergeableError{Reason: "Merge queue is required", MergeQueue: true}
	assert.Equal(t, ErrNotMergeable.Error()+": Merge queue is required", refused.Error())
	require.ErrorIs(t, refused, ErrNotMergeable)

	wrapped := writeError(editRef(), &StatusError{StatusCode: http.StatusUnprocessableEntity, Message: "Merge queue is required"})

	got, ok := errors.AsType[*NotMergeableError](wrapped)
	require.True(t, ok)
	assert.True(t, got.MergeQueue)
}
