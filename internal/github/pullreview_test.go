package github

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sha1s is a commit SHA of one repeated digit, as the fixtures scrub them.
func sha1s(digit string) string {
	return strings.Repeat(digit, 40)
}

func TestFetchPullReviewReadsTheSheet(t *testing.T) {
	t.Parallel()

	body := fixture(t, "pull_review_response.json")

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		var req itemRequest
		assert.NoError(t, json.Unmarshal(raw, &req))
		assert.Contains(t, req.Query, "node(id: $id) { ...ItemFields ...DetailFields ...ReviewFields }")
		assert.Contains(t, req.Query, "fragment ReviewFields on PullRequest")
		assert.Contains(t, req.Query, "viewerCanClose")
		assert.Contains(t, req.Query, "isRequired(pullRequestId: $id)")
		assert.Equal(t, "PR_fakeNodeId94", req.Variables.ID)

		setRateHeaders(w, ResourceGraphQL, 4995, 5)
		_, _ = w.Write(body)
	})

	rates := &recordRates{}
	client.Rates = rates

	review, err := client.FetchPullReview(t.Context(), NewToken(fakeToken), "PR_fakeNodeId94")
	require.NoError(t, err)

	require.True(t, review.Update.Found)
	assert.Equal(t, 94, review.Update.Item.Number)
	assert.Equal(t, "Bump github/codeql-action from 4.39.1 to 4.39.3 in the github-actions group", review.Update.Item.Title)
	assert.Equal(t, "Bot", review.Update.Item.AuthorType)
	assert.Equal(t, sha1s("1"), review.Update.Details.HeadOID)
	assert.Equal(t, AutoMerge{
		Method: MergeMethodSquash, EnabledAt: time.Date(2026, 11, 9, 17, 10, 0, 0, time.UTC), EnabledBy: "octo-reviewer",
	}, review.Update.Details.AutoMerge, "the card reads auto-merge too")
	assert.Equal(t, 1, review.Update.RateLimit.Cost)

	assert.True(t, strings.HasPrefix(review.Body, "Bumps the github-actions group"), review.Body)
	assert.Equal(t, "master", review.BaseRef)
	assert.Equal(t, MergeStateBlocked, review.MergeState)
	assert.False(t, review.MergeQueue)
	assert.False(t, review.ViewerDidAuthor)
	assert.False(t, review.ViewerCanUpdateBranch)
	assert.False(t, review.ViewerCanEnableAutoMerge)
	assert.True(t, review.ViewerCanBypass)
	assert.True(t, review.ViewerCanClose, "GitHub's own say that the viewer may close it")
	assert.Equal(t, ReviewMark{State: "COMMENTED", Commit: sha1s("2")}, review.ViewerReview)
	assert.Equal(t, 1, review.PendingReviews)
	assert.True(t, review.AutoMerge.On())
	assert.NotContains(t, pullReviewQuery, "commits(", "nothing reads a commit's message, so none is asked for")

	assert.Equal(t, []Review{
		{
			Author: "octo-user", State: "COMMENTED", Body: "Could you check the changelog?", Commit: sha1s("2"),
			URL:         "https://github.com/acme-org/example-repo/pull/94#pullrequestreview-1",
			SubmittedAt: time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC),
		},
		{
			Author: "octo-reviewer", State: "APPROVED", Commit: sha1s("1"),
			URL:         "https://github.com/acme-org/example-repo/pull/94#pullrequestreview-2",
			SubmittedAt: time.Date(2026, 11, 9, 17, 5, 0, 0, time.UTC),
		},
	}, review.Reviews, "a null review and one without an author are skipped")

	require.Len(t, review.Checks, 7, "the null context is skipped")
	assert.Equal(t, Check{
		Kind: CheckKindRun, Name: "Analyze (go)", Workflow: "CodeQL", State: "SUCCESS", Outcome: CheckPassed,
		URL: "https://github.com/acme-org/example-repo/actions/runs/101/job/201",
	}, review.Checks[0])
	assert.True(t, review.Checks[1].Required)
	assert.Equal(t, Check{
		Kind: CheckKindRun, Name: "lint", State: "IN_PROGRESS", Outcome: CheckPending, Required: true,
		URL: "https://github.com/acme-org/example-repo/actions/runs/101/job/202",
	}, review.Checks[4])
	assert.Equal(t, CheckFailed, review.Checks[5].Outcome)
	assert.Equal(t, Check{
		Kind: CheckKindStatus, Name: "ci/legacy", State: "SUCCESS", Outcome: CheckPassed,
		URL: "https://ci.example.com/acme-org/example-repo/7",
	}, review.Checks[6])

	observed := rates.all()
	require.NotEmpty(t, observed)
	assert.Equal(t, 1, observed[len(observed)-1].spent, "the query spends the point it reports")
}

func TestFetchPullReviewOfAPullRequestThatIsGone(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, serveItem(`{"data":{"node":null,"rateLimit":{"cost":1}},`+
		`"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a node"}]}`))

	review, err := client.FetchPullReview(t.Context(), NewToken(fakeToken), "PR_gone")
	require.NoError(t, err)
	assert.False(t, review.Update.Found)
	assert.Empty(t, review.Checks)
}

func TestFetchPullReviewRefusesWhatIsNotAPullRequest(t *testing.T) {
	t.Parallel()

	issue := fixture(t, "item_response.json")
	issueBody := strings.Replace(string(issue), `"__typename": "PullRequest"`, `"__typename": "Issue"`, 1)

	tests := []struct {
		name string
		id   string
		body string
		want error
	}{
		{name: "no ID", id: "", body: `{}`, want: ErrInvalidItemRef},
		{name: "an issue", id: "I_x", body: issueBody, want: ErrMalformedResponse},
		{name: "another error", id: "PR_x", body: `{"data":{"node":null},"errors":[{"type":"FORBIDDEN","message":"no"}]}`, want: ErrGraphQL},
		{name: "a bad node", id: "PR_x", body: `{"data":{"node":{"__typename":"PullRequest","id":"PR_x","number":"x"}}}`, want: ErrMalformedResponse},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, serveItem(tt.body))

			_, err := client.FetchPullReview(t.Context(), NewToken(fakeToken), tt.id)
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestFetchPullReviewFailsOnABadReviewField(t *testing.T) {
	t.Parallel()

	body := strings.Replace(string(fixture(t, "pull_review_response.json")), `"pendingReviews": {`, `"pendingReviews": "x", "unused": {`, 1)
	client := newTestClient(t, serveItem(body))

	_, err := client.FetchPullReview(t.Context(), NewToken(fakeToken), "PR_fakeNodeId94")
	require.ErrorIs(t, err, ErrMalformedResponse)
}

func TestReviewChecksWithoutARollup(t *testing.T) {
	t.Parallel()

	review := &PullReview{}
	(&reviewNode{}).fill(review)

	assert.Equal(t, []Check{}, review.Checks)
	assert.Equal(t, []Review{}, review.Reviews)
	assert.Equal(t, ReviewMark{}, review.ViewerReview)

	var check *graphQLCheck

	_, ok := check.check()
	assert.False(t, ok)

	_, ok = (&graphQLCheck{TypeName: "Other", Name: "x"}).check()
	assert.False(t, ok)

	run, ok := (&graphQLCheck{TypeName: "CheckRun", Name: "build", Conclusion: "SKIPPED"}).check()
	require.True(t, ok)
	assert.Equal(t, CheckSkipped, run.Outcome)

	status, ok := (&graphQLCheck{TypeName: "StatusContext", Context: "ci", State: "ERROR"}).check()
	require.True(t, ok)
	assert.Equal(t, CheckFailed, status.Outcome)

	assert.Equal(t, CheckPending, outcome(99).word())
}

func TestFetchMergeState(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		var req itemRequest
		assert.NoError(t, json.Unmarshal(raw, &req))
		assert.Contains(t, req.Query, "mergeStateStatus headRefOid")
		assert.Contains(t, req.Query, "viewerCanMergeAsAdmin")
		assert.Equal(t, "PR_fakeNodeId94", req.Variables.ID)

		_, _ = io.WriteString(w, `{"data":{"node":{"__typename":"PullRequest","mergeStateStatus":"CLEAN",`+
			`"headRefOid":"`+sha1s("1")+`","isDraft":false,"isMergeQueueEnabled":true,"prState":"OPEN",`+
			`"viewerCanMergeAsAdmin":true,"autoMergeRequest":null},"rateLimit":{"cost":1}}}`)
	})

	state, err := client.FetchMergeState(t.Context(), NewToken(fakeToken), "PR_fakeNodeId94")
	require.NoError(t, err)
	assert.Equal(t, &MergeState{
		Found: true, Status: MergeStateClean, Head: sha1s("1"), State: StateOpen, MergeQueue: true, CanBypass: true,
	}, state)
}

func TestFetchMergeStateFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
		body string
		want error
	}{
		{name: "no ID", body: `{}`, want: ErrInvalidItemRef},
		{name: "a GraphQL failure", id: "PR_x", body: `{"errors":[{"message":"boom"}]}`, want: ErrGraphQL},
		{name: "a refusal", id: "PR_x", body: `{"data":{"node":null},"errors":[{"type":"FORBIDDEN","message":"no"}]}`, want: ErrGraphQL},
		{name: "an issue", id: "I_x", body: `{"data":{"node":{"__typename":"Issue"}}}`, want: ErrMalformedResponse},
		{name: "no head", id: "PR_x", body: `{"data":{"node":{"__typename":"PullRequest","headRefOid":""}}}`, want: ErrMalformedResponse},
		{name: "a bad node", id: "PR_x", body: `{"data":{"node":{"isDraft":"x"}}}`, want: ErrMalformedResponse},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, serveItem(tt.body))

			_, err := client.FetchMergeState(t.Context(), NewToken(fakeToken), tt.id)
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestFetchMergeStateOfAPullRequestThatIsGone(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, serveItem(`{"data":{"node":null},"errors":[{"type":"NOT_FOUND","message":"gone"}]}`))

	state, err := client.FetchMergeState(t.Context(), NewToken(fakeToken), "PR_gone")
	require.NoError(t, err)
	assert.False(t, state.Found)
}

func TestMergeable(t *testing.T) {
	t.Parallel()

	for _, status := range []string{MergeStateClean, MergeStateHasHooks, "clean"} {
		assert.True(t, Mergeable(status), status)
	}

	for _, status := range []string{MergeStateUnstable, MergeStateBlocked, MergeStateBehind, MergeStateDirty, MergeStateUnknown, ""} {
		assert.False(t, Mergeable(status), status)
	}
}
