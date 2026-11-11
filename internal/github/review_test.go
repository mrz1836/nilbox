package github

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/uierr"
)

// sentWrite is one request the fake GitHub received, with its JSON body.
type sentWrite struct {
	method, path string
	body         map[string]any
}

// writeRecorder keeps the requests a fake GitHub received.
type writeRecorder struct {
	mu  sync.Mutex
	got []sentWrite
}

func (w *writeRecorder) all() []sentWrite {
	w.mu.Lock()
	defer w.mu.Unlock()

	return append([]sentWrite(nil), w.got...)
}

// writeServer answers every request with status and body and a budget reading for
// resource, recording each request.
func writeServer(t *testing.T, resource string, status int, body string, headers map[string]string) (*Client, *writeRecorder) {
	t.Helper()

	rec := &writeRecorder{}

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		sent := sentWrite{method: r.Method, path: r.URL.Path}
		if len(raw) > 0 {
			assert.NoError(t, json.Unmarshal(raw, &sent.body))
		}

		rec.mu.Lock()
		rec.got = append(rec.got, sent)
		rec.mu.Unlock()

		setRateHeaders(w, resource, 4900, 100)

		for k, v := range headers {
			w.Header().Set(k, v)
		}

		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})

	return client, rec
}

// headSHA is the head commit the review tests pin their writes to.
var headSHA = strings.Repeat("a", 40) //nolint:gochecknoglobals // a fixed test value

func TestSubmitReviewSendsOnePinnedReview(t *testing.T) {
	t.Parallel()

	client, rec := writeServer(t, ResourceCore, http.StatusOK, `{"id":7,"state":"APPROVED","html_url":"https://github.com/acme-org/example-repo/pull/42#pullrequestreview-7","commit_id":"`+headSHA+`","submitted_at":"2026-11-09T17:00:00Z"}`, nil)
	gate := &fakeGate{}
	rates := &recordRates{}
	client.Writes, client.Rates = gate, rates

	review, err := client.SubmitReview(t.Context(), NewToken(fakeToken), editRef(), ReviewInput{
		Event: ReviewApprove, Body: "Looks good", CommitID: headSHA,
		Comments: []LineComment{
			{Path: "a.go", Line: 3, Side: SideRight, Body: "nit"},
			{Path: "a.go", Line: 9, Side: SideRight, StartLine: 7, Body: "```suggestion\nx\n```"},
		},
	})
	require.NoError(t, err)

	assert.Equal(t, &SubmittedReview{
		ID: 7, State: "APPROVED", URL: "https://github.com/acme-org/example-repo/pull/42#pullrequestreview-7",
		CommitID: headSHA, SubmittedAt: time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC),
	}, review)

	sent := rec.all()
	require.Len(t, sent, 1)
	assert.Equal(t, http.MethodPost, sent[0].method)
	assert.Equal(t, "/repos/acme-org/example-repo/pulls/42/reviews", sent[0].path)
	assert.Equal(t, map[string]any{
		"commit_id": headSHA, "body": "Looks good", "event": "APPROVE",
		"comments": []any{
			map[string]any{"path": "a.go", "line": float64(3), "side": "RIGHT", "body": "nit"},
			map[string]any{
				"path": "a.go", "line": float64(9), "side": "RIGHT", "start_line": float64(7), "start_side": "RIGHT",
				"body": "```suggestion\nx\n```",
			},
		},
	}, sent[0].body)

	assert.Equal(t, []string{ResourceCore}, gate.allowed)
	require.Len(t, rates.all(), 1)
	assert.Equal(t, 1, rates.all()[0].spent, "one REST point")
}

func TestSubmitReviewOfAnApprovalWithoutText(t *testing.T) {
	t.Parallel()

	client, rec := writeServer(t, ResourceCore, http.StatusOK, `{"id":1,"state":"APPROVED"}`, nil)

	_, err := client.SubmitReview(t.Context(), NewToken(fakeToken), editRef(),
		ReviewInput{Event: ReviewApprove, CommitID: headSHA})
	require.NoError(t, err)

	assert.Equal(t, map[string]any{"commit_id": headSHA, "event": "APPROVE"}, rec.all()[0].body)
}

func TestSubmitReviewKeptAsADraft(t *testing.T) {
	t.Parallel()

	client, _ := writeServer(t, ResourceCore, http.StatusOK, `{"id":1,"state":"PENDING"}`, nil)

	_, err := client.SubmitReview(t.Context(), NewToken(fakeToken), editRef(),
		ReviewInput{Event: ReviewApprove, CommitID: headSHA})
	require.ErrorIs(t, err, ErrEditIgnored)
}

func TestSubmitReviewRefusesWhatGitHubWouldNot(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", maxBodyRunes+1)
	many := make([]LineComment, maxLineComments+1)

	for i := range many {
		many[i] = LineComment{Path: "a.go", Line: i + 1, Side: SideRight, Body: "x"}
	}

	tests := []struct {
		name string
		in   ReviewInput
	}{
		{name: "no event", in: ReviewInput{CommitID: headSHA}},
		{name: "no commit", in: ReviewInput{Event: ReviewApprove}},
		{name: "a short commit", in: ReviewInput{Event: ReviewApprove, CommitID: "abc"}},
		{name: "a commit that is not hex", in: ReviewInput{Event: ReviewApprove, CommitID: strings.Repeat("z", 40)}},
		{name: "changes without text", in: ReviewInput{Event: ReviewRequestChanges, CommitID: headSHA, Body: " "}},
		{name: "a comment without text", in: ReviewInput{Event: ReviewComment, CommitID: headSHA}},
		{name: "too long", in: ReviewInput{Event: ReviewApprove, CommitID: headSHA, Body: long}},
		{name: "too many comments", in: ReviewInput{Event: ReviewComment, CommitID: headSHA, Comments: many}},
		{name: "a comment on no file", in: ReviewInput{
			Event: ReviewComment, CommitID: headSHA, Comments: []LineComment{{Line: 1, Side: SideRight, Body: "x"}},
		}},
		{name: "a comment on line 0", in: ReviewInput{
			Event: ReviewComment, CommitID: headSHA, Comments: []LineComment{{Path: "a", Side: SideRight, Body: "x"}},
		}},
		{name: "a backwards range", in: ReviewInput{
			Event: ReviewComment, CommitID: headSHA,
			Comments: []LineComment{{Path: "a", Line: 2, StartLine: 2, Side: SideRight, Body: "x"}},
		}},
		{name: "a bad side", in: ReviewInput{
			Event: ReviewComment, CommitID: headSHA, Comments: []LineComment{{Path: "a", Line: 2, Side: "UP", Body: "x"}},
		}},
		{name: "a bad start side", in: ReviewInput{
			Event: ReviewComment, CommitID: headSHA,
			Comments: []LineComment{{Path: "a", Line: 2, StartLine: 1, StartSide: "UP", Side: SideRight, Body: "x"}},
		}},
		{name: "a blank line comment", in: ReviewInput{
			Event: ReviewComment, CommitID: headSHA, Comments: []LineComment{{Path: "a", Line: 2, Side: SideLeft}},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, rec := writeServer(t, ResourceCore, http.StatusOK, `{}`, nil)
			gate := &fakeGate{}
			client.Writes = gate

			_, err := client.SubmitReview(t.Context(), NewToken(fakeToken), editRef(), tt.in)
			require.ErrorIs(t, err, ErrInvalidWrite)
			assert.Empty(t, rec.all(), "nothing is sent")
			assert.Empty(t, gate.allowed, "nothing is counted")
		})
	}
}

func TestReviewWritesSayWhyGitHubRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{
			name: "own pull request", status: http.StatusUnprocessableEntity,
			body: `{"message":"Unprocessable Entity","errors":["Review Can not approve your own pull request"]}`, want: ErrOwnPull,
		},
		{
			name: "pending review", status: http.StatusUnprocessableEntity,
			body: `{"message":"Unprocessable Entity","errors":["User can only have one pending review per pull request"]}`,
			want: ErrPendingReview,
		},
		{
			name: "line off the diff", status: http.StatusUnprocessableEntity,
			body: `{"message":"Unprocessable Entity","errors":["Pull request review thread line must be part of the diff"]}`,
			want: ErrLineNotInDiff,
		},
		{name: "a bad answer", status: http.StatusOK, body: `[]`, want: ErrMalformedResponse},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, _ := writeServer(t, ResourceCore, tt.status, tt.body, nil)

			_, err := client.SubmitReview(t.Context(), NewToken(fakeToken), editRef(),
				ReviewInput{Event: ReviewApprove, CommitID: headSHA})
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestMergePullSendsOnePinnedMerge(t *testing.T) {
	t.Parallel()

	client, rec := writeServer(t, ResourceCore, http.StatusOK,
		`{"sha":"bbbb","merged":true,"message":"Pull Request successfully merged"}`, nil)
	gate := &fakeGate{}
	client.Writes = gate

	merged, err := client.MergePull(t.Context(), NewToken(fakeToken), editRef(), MergeInput{SHA: headSHA, Method: MergeMethodSquash})
	require.NoError(t, err)
	assert.Equal(t, &Merged{SHA: "bbbb"}, merged)

	sent := rec.all()
	require.Len(t, sent, 1)
	assert.Equal(t, http.MethodPut, sent[0].method)
	assert.Equal(t, "/repos/acme-org/example-repo/pulls/42/merge", sent[0].path)
	assert.Equal(t, map[string]any{"sha": headSHA, "merge_method": "squash"}, sent[0].body)
	assert.Equal(t, []string{ResourceCore}, gate.allowed)
}

func TestMergePullRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		body    string
		headers map[string]string
		want    error
		hold    time.Duration
	}{
		{name: "pushed since", status: http.StatusConflict, body: `{"message":"Head branch was modified. Review and try the merge again."}`, want: ErrPushedSince},
		{name: "not mergeable", status: http.StatusMethodNotAllowed, body: `{"message":"Required status check \"lint\" is expected."}`, want: ErrNotMergeable},
		{name: "not merged", status: http.StatusOK, body: `{"merged":false,"message":""}`, want: ErrEditIgnored},
		{name: "a bad answer", status: http.StatusOK, body: `"x"`, want: ErrMalformedResponse},
		{
			name: "slowed down", status: http.StatusForbidden, body: `{"message":"You have exceeded a secondary rate limit"}`,
			headers: map[string]string{"Retry-After": "120"}, want: ErrUnexpectedStatus, hold: 2 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, _ := writeServer(t, ResourceCore, tt.status, tt.body, tt.headers)
			gate := &fakeGate{}
			client.Writes = gate

			_, err := client.MergePull(t.Context(), NewToken(fakeToken), editRef(), MergeInput{SHA: headSHA, Method: MergeMethodMerge})
			require.ErrorIs(t, err, tt.want)

			if tt.hold > 0 {
				assert.Equal(t, []time.Duration{tt.hold}, gate.held)
			}
		})
	}
}

func TestMergePullChecksItsInput(t *testing.T) {
	t.Parallel()

	client, rec := writeServer(t, ResourceCore, http.StatusOK, `{}`, nil)
	bad := editRef()
	bad.Repo.Owner = ""

	_, err := client.MergePull(t.Context(), NewToken(fakeToken), bad, MergeInput{SHA: headSHA, Method: MergeMethodMerge})
	require.ErrorIs(t, err, ErrInvalidItemRef)

	_, err = client.MergePull(t.Context(), NewToken(fakeToken), editRef(), MergeInput{SHA: "", Method: MergeMethodMerge})
	require.ErrorIs(t, err, ErrInvalidWrite)

	_, err = client.MergePull(t.Context(), NewToken(fakeToken), editRef(), MergeInput{SHA: headSHA, Method: "squash"})
	require.ErrorIs(t, err, ErrInvalidWrite)

	assert.Empty(t, rec.all())
}

func TestEnableAutoMergeSendsOneGatedMutation(t *testing.T) {
	t.Parallel()

	client, rec := writeServer(t, ResourceGraphQL, http.StatusOK, `{"data":{"enablePullRequestAutoMerge":{"pullRequest":{"autoMergeRequest":{"mergeMethod":"SQUASH","enabledAt":"2026-11-09T17:00:00Z","enabledBy":{"login":"octo-user"}}}}}}`, nil)
	gate := &fakeGate{}
	client.Writes = gate

	auto, err := client.EnableAutoMerge(t.Context(), NewToken(fakeToken), editRef(), "PR_x", MergeMethodSquash, headSHA)
	require.NoError(t, err)
	assert.Equal(t, &AutoMerge{Method: MergeMethodSquash, EnabledAt: time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC), EnabledBy: "octo-user"}, auto)

	sent := rec.all()
	require.Len(t, sent, 1)
	assert.Equal(t, graphQLPath, sent[0].path)
	assert.Contains(t, sent[0].body["query"], "enablePullRequestAutoMerge")
	assert.Equal(t, map[string]any{"id": "PR_x", "method": "SQUASH", "head": headSHA}, sent[0].body["variables"])
	assert.Equal(t, []string{ResourceGraphQL}, gate.allowed)
}

func TestEnableAutoMergeRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		body  string
		want  error
		words string
	}{
		{name: "mergeable now", body: `{"errors":[{"type":"UNPROCESSABLE","message":"Pull request Pull request is in clean status"}]}`, want: ErrMergeableNow},
		{name: "unstable", body: `{"errors":[{"message":"Pull request is in unstable status"}]}`, want: ErrMergeableNow},
		{name: "pushed since", body: `{"errors":[{"message":"Head sha didn't match expected head oid"}]}`, want: ErrPushedSince},
		{name: "merge queue", body: `{"errors":[{"message":"Pull request must be merged through the merge queue"}]}`, want: ErrNotMergeable},
		{name: "not allowed", body: `{"errors":[{"message":"Auto merge is not allowed for this repository"}]}`, want: ErrGraphQL, words: "does not allow auto-merge"},
		{name: "other", body: `{"errors":[{"message":"Pull request is in draft state"}]}`, want: ErrGraphQL, words: "GitHub refused to turn on auto-merge"},
		{name: "no pull request", body: `{"data":{"enablePullRequestAutoMerge":null}}`, want: ErrMalformedResponse},
		{name: "not turned on", body: `{"data":{"enablePullRequestAutoMerge":{"pullRequest":{"autoMergeRequest":null}}}}`, want: ErrEditIgnored},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, _ := writeServer(t, ResourceGraphQL, http.StatusOK, tt.body, nil)

			_, err := client.EnableAutoMerge(t.Context(), NewToken(fakeToken), editRef(), "PR_x", MergeMethodMerge, headSHA)
			require.ErrorIs(t, err, tt.want)

			if tt.words != "" {
				assert.Contains(t, err.Error()+uierrText(err), tt.words)
			}
		})
	}
}

func TestEnableAutoMergeOverRESTRefusal(t *testing.T) {
	t.Parallel()

	client, _ := writeServer(t, ResourceGraphQL, http.StatusForbidden, `{"message":"Resource not accessible by integration"}`, nil)

	_, err := client.EnableAutoMerge(t.Context(), NewToken(fakeToken), editRef(), "PR_x", MergeMethodRebase, headSHA)
	require.ErrorIs(t, err, ErrUnexpectedStatus)
	assert.Contains(t, uierrText(err), "Resource not accessible by integration")
}

func TestEnableAutoMergeChecksItsInput(t *testing.T) {
	t.Parallel()

	client, rec := writeServer(t, ResourceGraphQL, http.StatusOK, `{}`, nil)
	bad := editRef()
	bad.Number = 0

	_, err := client.EnableAutoMerge(t.Context(), NewToken(fakeToken), bad, "PR_x", MergeMethodMerge, headSHA)
	require.ErrorIs(t, err, ErrInvalidItemRef)

	_, err = client.EnableAutoMerge(t.Context(), NewToken(fakeToken), editRef(), "", MergeMethodMerge, headSHA)
	require.ErrorIs(t, err, ErrInvalidItemRef)

	_, err = client.EnableAutoMerge(t.Context(), NewToken(fakeToken), editRef(), "PR_x", MergeMethodMerge, "x")
	require.ErrorIs(t, err, ErrInvalidWrite)

	_, err = client.EnableAutoMerge(t.Context(), NewToken(fakeToken), editRef(), "PR_x", "FAST", headSHA)
	require.ErrorIs(t, err, ErrInvalidWrite)

	assert.Empty(t, rec.all())
}

func TestUpdateBranchSendsOnePinnedUpdate(t *testing.T) {
	t.Parallel()

	client, rec := writeServer(t, ResourceCore, http.StatusAccepted, `{"message":"Updating pull request branch.","url":"x"}`, nil)

	update, err := client.UpdateBranch(t.Context(), NewToken(fakeToken), editRef(), headSHA)
	require.NoError(t, err)
	assert.Equal(t, &BranchUpdate{Message: "Updating pull request branch."}, update)

	sent := rec.all()
	require.Len(t, sent, 1)
	assert.Equal(t, http.MethodPut, sent[0].method)
	assert.Equal(t, "/repos/acme-org/example-repo/pulls/42/update-branch", sent[0].path)
	assert.Equal(t, map[string]any{"expected_head_sha": headSHA}, sent[0].body)
}

func TestUpdateBranchRefusals(t *testing.T) {
	t.Parallel()

	client, _ := writeServer(t, ResourceCore, http.StatusUnprocessableEntity,
		`{"message":"expected head sha didn't match current head ref."}`, nil)

	_, err := client.UpdateBranch(t.Context(), NewToken(fakeToken), editRef(), headSHA)
	require.ErrorIs(t, err, ErrPushedSince)

	client, _ = writeServer(t, ResourceCore, http.StatusAccepted, `[]`, nil)

	_, err = client.UpdateBranch(t.Context(), NewToken(fakeToken), editRef(), headSHA)
	require.ErrorIs(t, err, ErrMalformedResponse)

	bad := editRef()
	bad.Repo.Repo = ""

	_, err = client.UpdateBranch(t.Context(), NewToken(fakeToken), bad, headSHA)
	require.ErrorIs(t, err, ErrInvalidItemRef)

	_, err = client.UpdateBranch(t.Context(), NewToken(fakeToken), editRef(), "")
	require.ErrorIs(t, err, ErrInvalidWrite)
}

func TestAddCommentSendsOneComment(t *testing.T) {
	t.Parallel()

	client, rec := writeServer(t, ResourceCore, http.StatusCreated,
		`{"id":9,"html_url":"https://github.com/acme-org/example-repo/pull/42#issuecomment-9","created_at":"2026-11-09T17:00:00Z"}`, nil)

	comment, err := client.AddComment(t.Context(), NewToken(fakeToken), editRef(), "@dependabot rebase")
	require.NoError(t, err)
	assert.Equal(t, &Comment{
		ID: 9, URL: "https://github.com/acme-org/example-repo/pull/42#issuecomment-9",
		CreatedAt: time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC),
	}, comment)

	sent := rec.all()
	require.Len(t, sent, 1)
	assert.Equal(t, http.MethodPost, sent[0].method)
	assert.Equal(t, "/repos/acme-org/example-repo/issues/42/comments", sent[0].path)
	assert.Equal(t, map[string]any{"body": "@dependabot rebase"}, sent[0].body)
}

func TestAddCommentRefusals(t *testing.T) {
	t.Parallel()

	client, rec := writeServer(t, ResourceCore, http.StatusNotFound, `{"message":"Not Found"}`, nil)

	_, err := client.AddComment(t.Context(), NewToken(fakeToken), editRef(), "")
	require.ErrorIs(t, err, ErrInvalidWrite)
	assert.Empty(t, rec.all())

	bad := editRef()
	bad.Number = -1

	_, err = client.AddComment(t.Context(), NewToken(fakeToken), bad, "x")
	require.ErrorIs(t, err, ErrInvalidItemRef)

	_, err = client.AddComment(t.Context(), NewToken(fakeToken), editRef(), "x")
	require.ErrorIs(t, err, ErrUnexpectedStatus)
	assert.Contains(t, uierrText(err), "could not find")

	client, _ = writeServer(t, ResourceCore, http.StatusCreated, `"x"`, nil)

	_, err = client.AddComment(t.Context(), NewToken(fakeToken), editRef(), "x")
	require.ErrorIs(t, err, ErrMalformedResponse)
}

func TestClosePullSendsOneGatedClose(t *testing.T) {
	t.Parallel()

	client, rec := writeServer(t, ResourceCore, http.StatusOK,
		`{"number":42,"state":"closed","merged":false,"closed_at":"2026-11-09T17:00:00Z"}`, nil)
	gate := &fakeGate{}
	rates := &recordRates{}
	client.Writes, client.Rates = gate, rates

	closed, err := client.ClosePull(t.Context(), NewToken(fakeToken), editRef())
	require.NoError(t, err)
	assert.Equal(t, &Closed{ClosedAt: time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC)}, closed)

	sent := rec.all()
	require.Len(t, sent, 1)
	assert.Equal(t, http.MethodPatch, sent[0].method)
	assert.Equal(t, "/repos/acme-org/example-repo/pulls/42", sent[0].path)
	assert.Equal(t, map[string]any{"state": "closed"}, sent[0].body, "only the state changes")
	assert.Equal(t, []string{ResourceCore}, gate.allowed)
	require.Len(t, rates.all(), 1)
	assert.Equal(t, 1, rates.all()[0].spent, "one REST point")
}

func TestClosePullWithoutATime(t *testing.T) {
	t.Parallel()

	client, _ := writeServer(t, ResourceCore, http.StatusOK, `{"state":"closed","closed_at":null}`, nil)

	closed, err := client.ClosePull(t.Context(), NewToken(fakeToken), editRef())
	require.NoError(t, err)
	assert.Equal(t, &Closed{}, closed)
}

func TestClosePullRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		body    string
		headers map[string]string
		want    error
		words   string
		hold    time.Duration
	}{
		{name: "left open", status: http.StatusOK, body: `{"state":"open"}`, want: ErrEditIgnored, words: "GitHub did not close"},
		{name: "a bad answer", status: http.StatusOK, body: `[]`, want: ErrMalformedResponse},
		{
			name: "not allowed", status: http.StatusForbidden, body: `{"message":"Resource not accessible by integration"}`,
			want: ErrUnexpectedStatus, words: "Resource not accessible by integration",
		},
		{
			name: "unprocessable", status: http.StatusUnprocessableEntity,
			body: `{"message":"Validation Failed","errors":["state cannot be changed"]}`,
			want: ErrUnexpectedStatus, words: "GitHub refused the change to acme-org/example-repo#42",
		},
		{name: "gone", status: http.StatusNotFound, body: `{"message":"Not Found"}`, want: ErrUnexpectedStatus, words: "could not find"},
		{
			name: "slowed down", status: http.StatusForbidden, body: `{"message":"You have exceeded a secondary rate limit"}`,
			headers: map[string]string{"Retry-After": "120"}, want: ErrUnexpectedStatus, hold: 2 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, _ := writeServer(t, ResourceCore, tt.status, tt.body, tt.headers)
			gate := &fakeGate{}
			client.Writes = gate

			_, err := client.ClosePull(t.Context(), NewToken(fakeToken), editRef())
			require.ErrorIs(t, err, tt.want)

			if tt.words != "" {
				assert.Contains(t, uierrText(err), tt.words)
			}

			if tt.hold > 0 {
				assert.Equal(t, []time.Duration{tt.hold}, gate.held)
			}
		})
	}
}

func TestClosePullChecksItsInput(t *testing.T) {
	t.Parallel()

	client, rec := writeServer(t, ResourceCore, http.StatusOK, `{"state":"closed"}`, nil)
	gate := &fakeGate{}
	client.Writes = gate
	bad := editRef()
	bad.Number = 0

	_, err := client.ClosePull(t.Context(), NewToken(fakeToken), bad)
	require.ErrorIs(t, err, ErrInvalidItemRef)
	assert.Empty(t, rec.all(), "nothing is sent")
	assert.Empty(t, gate.allowed, "nothing is counted")
}

func TestEveryWriteScrubsTheToken(t *testing.T) {
	t.Parallel()

	client, _ := writeServer(t, ResourceCore, http.StatusUnauthorized, `{"message":"Bad credentials `+fakeToken+`"}`, nil)
	tok := NewToken(fakeToken)

	_, err := client.SubmitReview(t.Context(), tok, editRef(), ReviewInput{Event: ReviewApprove, CommitID: headSHA})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), fakeToken)

	_, err = client.MergePull(t.Context(), tok, editRef(), MergeInput{SHA: headSHA, Method: MergeMethodMerge})
	assert.NotContains(t, err.Error(), fakeToken)

	_, err = client.EnableAutoMerge(t.Context(), tok, editRef(), "PR_x", MergeMethodMerge, headSHA)
	assert.NotContains(t, err.Error(), fakeToken)

	_, err = client.UpdateBranch(t.Context(), tok, editRef(), headSHA)
	assert.NotContains(t, err.Error(), fakeToken)

	_, err = client.AddComment(t.Context(), tok, editRef(), "x")
	assert.NotContains(t, err.Error(), fakeToken)

	_, err = client.ClosePull(t.Context(), tok, editRef())
	assert.NotContains(t, err.Error(), fakeToken)
}

func TestStartSideAndNonEmpty(t *testing.T) {
	t.Parallel()

	assert.Empty(t, startSide(LineComment{Side: SideRight}))
	assert.Equal(t, SideRight, startSide(LineComment{Side: SideRight, StartLine: 1}))
	assert.Equal(t, SideLeft, startSide(LineComment{Side: SideRight, StartLine: 1, StartSide: SideLeft}))
	assert.Equal(t, "x", nonEmpty(" ", "x"))
	assert.Equal(t, "y", nonEmpty("y", "x"))
	require.NoError(t, validateHead(strings.Repeat("A", 64)))
}

// uierrText is what the user is told about err.
func uierrText(err error) string {
	return uierr.Message(err)
}
