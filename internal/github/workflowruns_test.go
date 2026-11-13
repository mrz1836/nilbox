package github

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/repo"
)

// runRepo is the repository whose held workflow runs the approval tests approve.
func runRepo() repo.Name {
	return repo.Name{Owner: filesOwner, Repo: exampleRepo}
}

func TestApproveWorkflowRunSendsOneGatedApproval(t *testing.T) {
	t.Parallel()

	client, rec := writeServer(t, ResourceCore, http.StatusCreated, `{}`, nil)
	gate := &fakeGate{}
	rates := &recordRates{}
	client.Writes, client.Rates = gate, rates

	require.NoError(t, client.ApproveWorkflowRun(t.Context(), NewToken(fakeToken), runRepo(), 30433642))

	sent := rec.all()
	require.Len(t, sent, 1)
	assert.Equal(t, http.MethodPost, sent[0].method)
	assert.Equal(t, "/repos/acme-org/example-repo/actions/runs/30433642/approve", sent[0].path)
	assert.Nil(t, sent[0].body, "an approval sends no body")
	assert.Equal(t, []string{ResourceCore}, gate.allowed)
	require.Len(t, rates.all(), 1)
	assert.Equal(t, 1, rates.all()[0].spent, "one REST point")
}

func TestApproveWorkflowRunEscapesThePath(t *testing.T) {
	t.Parallel()

	var paths []string

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.EscapedPath())

		setRateHeaders(w, ResourceCore, 4900, 100)
		w.WriteHeader(http.StatusCreated)
	})

	require.NoError(t, client.ApproveWorkflowRun(t.Context(), NewToken(fakeToken),
		repo.Name{Owner: filesOwner, Repo: "a b?c"}, 7))
	assert.Equal(t, []string{"POST /repos/acme-org/a%20b%3Fc/actions/runs/7/approve"}, paths)
}

func TestApproveWorkflowRunRefusesWhatItMustNotSend(t *testing.T) {
	t.Parallel()

	client, rec := writeServer(t, ResourceCore, http.StatusCreated, `{}`, nil)
	gate := &fakeGate{}
	client.Writes = gate

	for _, tt := range []struct {
		name  repo.Name
		runID int64
	}{
		{name: runRepo(), runID: 0},
		{name: runRepo(), runID: -1},
		{name: repo.Name{Repo: exampleRepo}, runID: 7},
		{name: repo.Name{Owner: filesOwner}, runID: 7},
	} {
		err := client.ApproveWorkflowRun(t.Context(), NewToken(fakeToken), tt.name, tt.runID)
		require.ErrorIs(t, err, ErrInvalidWrite, "%v %d", tt.name, tt.runID)
	}

	assert.Empty(t, rec.all(), "nothing is sent")
	assert.Empty(t, gate.allowed, "nothing is counted")
}

func TestApproveWorkflowRunWaitsForTheGate(t *testing.T) {
	t.Parallel()

	client, rec := writeServer(t, ResourceCore, http.StatusCreated, `{}`, nil)
	client.Writes = &fakeGate{refuse: errGateClosed}

	err := client.ApproveWorkflowRun(t.Context(), NewToken(fakeToken), runRepo(), 7)
	require.ErrorIs(t, err, errGateClosed)
	assert.Empty(t, rec.all(), "nothing was sent")
}

func TestApproveWorkflowRunRefusals(t *testing.T) {
	t.Parallel()

	const refused = "GitHub wouldn't approve workflow run 7 of acme-org/example-repo: "

	tests := []struct {
		name    string
		status  int
		body    string
		headers map[string]string
		words   string
		hold    time.Duration
	}{
		{
			name: "forbidden", status: http.StatusForbidden, body: `{"message":"Resource not accessible by integration"}`,
			words: refused + "Resource not accessible by integration.",
		},
		{
			name: "forbidden, saying nothing", status: http.StatusForbidden, body: `{}`,
			words: refused + "approving takes write access to the repository, and works only on a run waiting for approval.",
		},
		{
			name: "not found", status: http.StatusNotFound, body: `{"message":"Not Found"}`,
			words: "GitHub could not find workflow run 7 of acme-org/example-repo, or your account cannot see it, so " +
				"it wasn't approved; it may have been deleted or run again.",
		},
		{
			name: "unprocessable", status: http.StatusUnprocessableEntity,
			body:  `{"message":"This workflow run is not waiting for approval"}`,
			words: refused + "This workflow run is not waiting for approval.",
		},
		{
			name: "slowed down", status: http.StatusForbidden, body: `{"message":"You have exceeded a secondary rate limit"}`,
			headers: map[string]string{"Retry-After": "90"}, hold: 90 * time.Second, words: "GitHub is limiting how fast",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, _ := writeServer(t, ResourceCore, tt.status, tt.body, tt.headers)
			gate := &fakeGate{}
			client.Writes = gate

			err := client.ApproveWorkflowRun(t.Context(), NewToken(fakeToken), runRepo(), 7)
			require.ErrorIs(t, err, ErrUnexpectedStatus)
			require.NotErrorIs(t, err, ErrPushedSince, "an approval is pinned to nothing")
			require.NotErrorIs(t, err, ErrNotMergeable, "an approval is no merge")
			assert.True(t, strings.HasPrefix(uierrText(err), tt.words), uierrText(err))
			assert.NotContains(t, err.Error(), fakeToken, "the token is redacted")

			if tt.hold > 0 {
				assert.Equal(t, []time.Duration{tt.hold}, gate.held)
			}
		})
	}
}
