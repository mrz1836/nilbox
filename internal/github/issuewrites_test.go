package github

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// issueRef is the issue the issue sheet's write tests change.
func issueRef() item.Ref {
	return item.Ref{Repo: repo.Name{Owner: filesOwner, Repo: exampleRepo}, Number: 12, Kind: item.KindIssue}
}

func TestCloseIssueSendsOneGatedClose(t *testing.T) {
	t.Parallel()

	for _, reason := range []string{IssueReasonCompleted, IssueReasonNotPlanned} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()

			client, rec := writeServer(t, ResourceCore, http.StatusOK,
				`{"number":12,"state":"closed","state_reason":"`+reason+`"}`, nil)
			gate := &fakeGate{}
			rates := &recordRates{}
			client.Writes, client.Rates = gate, rates

			closed, err := client.CloseIssue(t.Context(), NewToken(fakeToken), issueRef(), reason)
			require.NoError(t, err)
			assert.Equal(t, &IssueClosed{Reason: reason}, closed)

			sent := rec.all()
			require.Len(t, sent, 1)
			assert.Equal(t, http.MethodPatch, sent[0].method)
			assert.Equal(t, "/repos/acme-org/example-repo/issues/12", sent[0].path)
			assert.Equal(t, map[string]any{"state": "closed", "state_reason": reason}, sent[0].body)
			assert.Equal(t, []string{ResourceCore}, gate.allowed)
			require.Len(t, rates.all(), 1)
			assert.Equal(t, 1, rates.all()[0].spent, "one REST point")
		})
	}
}

func TestCloseIssueRefusesWhatItMustNotSend(t *testing.T) {
	t.Parallel()

	client, rec := writeServer(t, ResourceCore, http.StatusOK, `{"state":"closed"}`, nil)
	gate := &fakeGate{}
	client.Writes = gate

	for _, reason := range []string{"", "duplicate", "reopened", "Completed", "completed "} {
		_, err := client.CloseIssue(t.Context(), NewToken(fakeToken), issueRef(), reason)
		require.ErrorIs(t, err, ErrInvalidWrite, "%q", reason)
	}

	// The same path would close a pull request, which only ClosePull does.
	pull := issueRef()
	pull.Kind = item.KindPR
	_, err := client.CloseIssue(t.Context(), NewToken(fakeToken), pull, IssueReasonCompleted)
	require.ErrorIs(t, err, ErrInvalidItemRef)

	bad := issueRef()
	bad.Number = 0
	_, err = client.CloseIssue(t.Context(), NewToken(fakeToken), bad, IssueReasonCompleted)
	require.ErrorIs(t, err, ErrInvalidItemRef)

	assert.Empty(t, rec.all(), "nothing is sent")
	assert.Empty(t, gate.allowed, "nothing is counted")
}

func TestCloseIssueRefusals(t *testing.T) {
	t.Parallel()

	const refused = "GitHub wouldn't close acme-org/example-repo#12: "

	tests := []struct {
		name    string
		status  int
		body    string
		headers map[string]string
		want    error
		words   string
		hold    time.Duration
	}{
		{name: "left open", status: http.StatusOK, body: `{"state":"open"}`, want: ErrEditIgnored, words: "GitHub did not close acme-org/example-repo#12."},
		{name: "a bad answer", status: http.StatusOK, body: `[]`, want: ErrMalformedResponse},
		{
			name: "forbidden", status: http.StatusForbidden, body: `{"message":"Must have admin rights to Repository."}`,
			want: ErrUnexpectedStatus, words: refused + "Must have admin rights to Repository.",
		},
		{
			name: "forbidden, saying nothing", status: http.StatusForbidden, body: `{}`,
			want: ErrUnexpectedStatus, words: refused + "your account may not be allowed to close it.",
		},
		{
			name: "not found", status: http.StatusNotFound, body: `{"message":"Not Found"}`,
			want: ErrUnexpectedStatus, words: "GitHub could not find acme-org/example-repo#12, or your account cannot see it, so it wasn't closed.",
		},
		{
			name: "gone", status: http.StatusGone, body: `{"message":"This issue was deleted"}`,
			want: ErrUnexpectedStatus, words: "GitHub could not find acme-org/example-repo#12",
		},
		{
			name: "unprocessable", status: http.StatusUnprocessableEntity,
			body: `{"message":"Validation Failed","errors":[{"code":"invalid","field":"state_reason"}]}`,
			want: ErrUnexpectedStatus, words: refused,
		},
		{
			name: "slowed down", status: http.StatusForbidden, body: `{"message":"You have exceeded a secondary rate limit"}`,
			headers: map[string]string{"Retry-After": "90"}, want: ErrUnexpectedStatus, hold: 90 * time.Second,
			words: "GitHub is limiting how fast",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, _ := writeServer(t, ResourceCore, tt.status, tt.body, tt.headers)
			gate := &fakeGate{}
			client.Writes = gate

			closed, err := client.CloseIssue(t.Context(), NewToken(fakeToken), issueRef(), IssueReasonNotPlanned)
			require.ErrorIs(t, err, tt.want)
			assert.Nil(t, closed)
			require.NotErrorIs(t, err, ErrPushedSince, "a close is pinned to nothing")
			require.NotErrorIs(t, err, ErrNotMergeable, "a close is no merge")
			assert.Contains(t, uierrText(err), tt.words)

			if tt.hold > 0 {
				assert.Equal(t, []time.Duration{tt.hold}, gate.held)
			}
		})
	}
}

func TestCloseIssueSaysWhatAnIssueClosedFirstIsClosedAs(t *testing.T) {
	t.Parallel()

	// Someone closed it as completed first; GitHub keeps that reason, and says so.
	client, _ := writeServer(t, ResourceCore, http.StatusOK, `{"state":"closed","state_reason":"COMPLETED"}`, nil)

	closed, err := client.CloseIssue(t.Context(), NewToken(fakeToken), issueRef(), IssueReasonNotPlanned)
	require.NoError(t, err)
	assert.Equal(t, &IssueClosed{Reason: IssueReasonCompleted}, closed)

	// An answer that doesn't say leaves the reason unknown.
	client, _ = writeServer(t, ResourceCore, http.StatusOK, `{"state":"closed"}`, nil)

	closed, err = client.CloseIssue(t.Context(), NewToken(fakeToken), issueRef(), IssueReasonCompleted)
	require.NoError(t, err)
	assert.Empty(t, closed.Reason)
	assert.Empty(t, closed.ClosedBy)

	// Someone closed it first as the reason asked: GitHub says who.
	client, _ = writeServer(t, ResourceCore, http.StatusOK,
		`{"state":"closed","state_reason":"completed","closed_by":{"login":"octo-other"}}`, nil)

	closed, err = client.CloseIssue(t.Context(), NewToken(fakeToken), issueRef(), IssueReasonCompleted)
	require.NoError(t, err)
	assert.Equal(t, &IssueClosed{Reason: IssueReasonCompleted, ClosedBy: "octo-other"}, closed)
}

func TestCanWrite(t *testing.T) {
	t.Parallel()

	for permission, want := range map[string]bool{
		"ADMIN": true, "MAINTAIN": true, "WRITE": true, "TRIAGE": false, "READ": false, "": false, "write": false,
	} {
		assert.Equal(t, want, CanWrite(permission), permission)
	}
}

func TestAnIssueCommentIsWordedAsAComment(t *testing.T) {
	t.Parallel()

	const refused = "GitHub wouldn't take your comment on acme-org/example-repo#12: "

	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{
			name: "locked", status: http.StatusForbidden, body: `{"message":"Unable to create comment because issue is locked."}`,
			want: refused + "Unable to create comment because issue is locked.",
		},
		{name: "forbidden, saying nothing", status: http.StatusForbidden, body: `{}`, want: refused + "your account may not be allowed to comment on it."},
		{
			name: "a conflict is no push", status: http.StatusConflict, body: `{"message":"Conflict"}`,
			want: refused + "Conflict.",
		},
		{
			name: "unprocessable", status: http.StatusUnprocessableEntity, body: `{"message":"Validation Failed","errors":["body is too long"]}`,
			want: refused + "Validation Failed: body is too long.",
		},
		{
			name: "not found", status: http.StatusNotFound, body: `{"message":"Not Found"}`,
			want: "GitHub could not find acme-org/example-repo#12, or your account cannot see it, so your comment wasn't posted.",
		},
		{name: "gone", status: http.StatusGone, body: `{"message":"Gone"}`, want: "so your comment wasn't posted."},
		{
			name: "slowed down", status: http.StatusTooManyRequests, body: `{"message":"API rate limit exceeded"}`,
			want: "GitHub is limiting how fast your account can make changes.",
		},
		{name: "a server error keeps its words", status: http.StatusBadGateway, body: `{"message":"bad gateway"}`, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, _ := writeServer(t, ResourceCore, tt.status, tt.body, nil)
			client.Writes = &fakeGate{}

			_, err := client.AddComment(t.Context(), NewToken(fakeToken), issueRef(), "Can you share a trace?")
			require.ErrorIs(t, err, ErrUnexpectedStatus)
			require.NotErrorIs(t, err, ErrPushedSince, "a comment is pinned to nothing")
			assert.NotContains(t, uierrText(err), "pushed to")
			assert.NotContains(t, uierrText(err), "merge")
			assert.Contains(t, uierrText(err), tt.want)
		})
	}

	// A pull request's comment keeps the review sheet's words.
	client, _ := writeServer(t, ResourceCore, http.StatusConflict, `{"message":"Conflict"}`, nil)

	_, err := client.AddComment(t.Context(), NewToken(fakeToken), editRef(), "@dependabot rebase")
	require.ErrorIs(t, err, ErrPushedSince)
}
