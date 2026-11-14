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

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/uierr"
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
		assert.Contains(t, req.Query, "closes: closingIssuesReferences(first: 10) { totalCount nodes { ...RefFields } }")
		assert.Contains(t, req.Query, "fragment RefFields on ReferencedSubject")
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
	assert.NotContains(t, pullReviewQuery, "message", "nothing reads a commit's message, so none is asked for")
	assert.Equal(t, 2, strings.Count(pullReviewQuery, "commits("), "the head's suites, once in each fragment")
	assert.Equal(t, 2, strings.Count(pullReviewQuery, "headSuites: commits(last: 1)"),
		"both fragments read the head's suites under one alias and arguments, which GraphQL merges into one field")
	assert.Equal(t, []ItemRef{{
		Repo: "acme-org/example-repo", Number: 12, Title: "Pin the code scanning action",
		URL: "https://github.com/acme-org/example-repo/issues/12", State: StateOpen,
	}}, review.Closes, "the issues it closes, a null skipped")
	assert.Equal(t, 1, review.CloseCount, "the null is one the token can't read, which isn't counted")

	assert.Equal(t, []Review{
		{
			Author: "octo-user", State: "COMMENTED", Commit: sha1s("2"),
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
	assert.Equal(t, []UnrunSuite{
		{
			Name: "PR Management", Conclusion: SuiteAwaitsApproval, RunID: 106,
			URL: "https://github.com/acme-org/example-repo/actions/runs/106",
		},
		{
			Name: "Auto-merge Trigger", Conclusion: "FAILURE", RunID: 107,
			URL: "https://github.com/acme-org/example-repo/actions/runs/107",
		},
		{Name: "Example CI", Conclusion: "STARTUP_FAILURE"},
	}, review.Unrun, "the suites that ended without a check and without passing, each workflow run with its ID and "+
		"page: one that ran a check (e2e failed in CS_5), passed, or is still queued is none of them, and a null is "+
		"skipped")
	assert.Equal(t, 1, review.Update.Details.Checks.Awaiting, "the card counts the run held for approval")

	observed := rates.all()
	require.NotEmpty(t, observed)
	assert.Equal(t, 1, observed[len(observed)-1].spent, "the query spends the point it reports")
}

func TestFetchPullReviewReadsHeldRunsWithoutARollup(t *testing.T) {
	t.Parallel()

	// A first-time contributor's fork whose every run waits for approval has no check, and
	// GitHub answers its rollup null: the held runs are read from the head commit anyway.
	var answer map[string]any
	require.NoError(t, json.Unmarshal(fixture(t, "pull_review_response.json"), &answer))

	node, ok := answer["data"].(map[string]any)["node"].(map[string]any)
	require.True(t, ok)

	node["statusCheckRollup"], node["checkList"] = nil, nil

	body, err := json.Marshal(answer)
	require.NoError(t, err)

	client := newTestClient(t, serveItem(string(body)))

	review, err := client.FetchPullReview(t.Context(), NewToken(fakeToken), "PR_fakeNodeId94")
	require.NoError(t, err)

	assert.Empty(t, review.Checks)
	assert.Equal(t, []UnrunSuite{
		{Name: "e2e", Conclusion: "FAILURE", RunID: 105, URL: "https://github.com/acme-org/example-repo/actions/runs/105"},
		{
			Name: "PR Management", Conclusion: SuiteAwaitsApproval, RunID: 106,
			URL: "https://github.com/acme-org/example-repo/actions/runs/106",
		},
		{
			Name: "Auto-merge Trigger", Conclusion: "FAILURE", RunID: 107,
			URL: "https://github.com/acme-org/example-repo/actions/runs/107",
		},
		{Name: "Example CI", Conclusion: "STARTUP_FAILURE"},
	}, review.Unrun, "with no check run, no suite ran one")
	assert.Equal(t, Checks{Awaiting: 1}, review.Update.Details.Checks, "the card has no rollup, and the held run")
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
	(&reviewNode{}).fill(review, ItemRef{}, "")

	assert.Equal(t, []Check{}, review.Checks)
	assert.Equal(t, []UnrunSuite{}, review.Unrun)
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

func TestFetchPullReviewReadsTheConversation(t *testing.T) {
	t.Parallel()

	body := fixture(t, "pull_review_response.json")

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		req := readIssueRequest(t, r)
		assert.Contains(t, req.Query, "query PullReview($id: ID!, $last: Int!)")
		assert.Contains(t, req.Query, "timeline: timelineItems(last: $last, itemTypes: "+pullTimelineItemTypes+")")
		assert.Contains(t, req.Query, "fragment PullTimelineFields on PullRequestTimelineItemsConnection")
		assert.Contains(t, req.Query, "totalCount filteredCount")
		assert.Contains(t, req.Query, "lineComments: comments(first: 20)")
		assert.Contains(t, req.Query, "mergeCommit: commit { abbreviatedOid }")
		assert.Contains(t, req.Query, "reactionGroups { content reactors { totalCount } }")
		assert.Equal(t, timelinePage, req.Variables.Last)

		_, _ = w.Write(body)
	})

	review, err := client.FetchPullReview(t.Context(), NewToken(fakeToken), "PR_fakeNodeId94")
	require.NoError(t, err)

	assert.Equal(t, time.Date(2026, 11, 9, 16, 53, 30, 0, time.UTC), review.LastEditedAt)
	assert.Equal(t, []Reaction{{Content: "THUMBS_UP", Count: 2}, {Content: "ROCKET", Count: 1}}, review.Reactions)
	assert.NotContains(t, reviewFieldsFragment, "state body url", "a reviewer's text is read in the conversation alone")

	page := review.Timeline
	assert.True(t, page.HasEarlier)
	assert.Equal(t, "Y3Vyc29yOnYyOpfakePullCursor94==", page.Before)
	assert.Equal(t, 14, page.Total, "counted by filteredCount, since a pull request's totalCount counts every type")
	assert.Equal(t, 1, page.RateLimit.Cost, "measured: the conversation costs no more than the read did without it")

	types := make([]string, 0, len(page.Items))
	for _, it := range page.Items {
		types = append(types, it.Type)
	}

	assert.Equal(t, []string{
		TimelineReviewRequested, TimelineReviewRequested, TimelineReviewRequested, TimelineComment, TimelineReview,
		TimelineForcePushed, TimelineConvertedToDraft, TimelineReadyForReview, TimelineReview, TimelineReview,
		TimelineReview,
	}, types, "a pending review, a review that says nothing, and a null node are skipped")

	assert.Equal(t, "octo-reviewer", page.Items[0].Reviewer)
	assert.Equal(t, "acme-org/maintainers", page.Items[1].Reviewer, "a team by its org and slug")
	assert.Empty(t, page.Items[2].Reviewer, "a reviewer the token can't see")
	assert.True(t, page.Items[3].Mine)

	mine := page.Items[4]
	assert.Equal(t, ReviewStateCommented, mine.ReviewState)
	assert.Equal(t, "Could you check the changelog?", mine.Body)
	assert.Equal(t, time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC), mine.CreatedAt, "a review is timed by its submission")
	assert.True(t, mine.Outdated, "left on an earlier commit than the head")
	assert.True(t, mine.Mine)
	assert.Equal(t, 3, mine.LineCommentCount, "the null one is one the token can't read, which isn't counted")
	assert.Equal(t, []ReviewLineComment{
		{
			ID: "PRRC_fakeNodeId1", URL: "https://github.com/acme-org/example-repo/pull/94#discussion_r1",
			CreatedAt: time.Date(2026, 11, 9, 16, 59, 50, 0, time.UTC), Body: "This pins the init step only.",
			Path: ".github/workflows/codeql.yml", Line: 12, StartLine: 11,
			DiffHunk: "@@ -5,6 +5,8 @@ jobs:\n       - uses: actions/checkout@v4\n-      - uses: github/codeql-action/init@v3\n" +
				"+      - uses: github/codeql-action/init@v4\n+        with:\n+          languages: go",
		},
		{
			ID: "PRRC_fakeNodeId2", URL: "https://github.com/acme-org/example-repo/pull/94#discussion_r2",
			CreatedAt: time.Date(2026, 11, 9, 16, 59, 55, 0, time.UTC), Body: "And this one?",
			Path: ".github/workflows/scan.yml", Line: 7, Outdated: true,
			DiffHunk: "@@ -7 +7 @@\n-      - uses: github/codeql-action/analyze@v3\n+      - uses: github/codeql-action/analyze@v4",
		},
	}, mine.LineComments, "an outdated comment is placed by its original line")

	assert.Equal(t, "2222222", page.Items[5].BeforeCommit)
	assert.Equal(t, "1111111", page.Items[5].AfterCommit)
	assert.Equal(t, "Bot", page.Items[5].ActorType)

	changes := page.Items[8]
	assert.Equal(t, ReviewStateChangesRequested, changes.ReviewState)
	assert.False(t, changes.Outdated, "left on the head")
	assert.Equal(t, time.Date(2026, 11, 9, 17, 4, 45, 0, time.UTC), changes.EditedAt)
	assert.Equal(t, "COLLABORATOR", changes.Association)

	approved := page.Items[9]
	assert.Equal(t, ReviewStateApproved, approved.ReviewState)
	assert.Empty(t, approved.Body)
	assert.Empty(t, approved.LineComments)
	assert.NotNil(t, approved.LineComments)
	assert.Equal(t, "octo-reviewer", approved.Actor)

	assert.Equal(t, "Bot", page.Items[10].ActorType)
}

func TestFetchPullReviewAsksForASmallerPageWhenTheAnswerIsTooLarge(t *testing.T) {
	t.Parallel()

	body := fixture(t, "pull_review_response.json")
	pages := &pageSizes{}

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		req := readIssueRequest(t, r)
		pages.add(req.Variables.Last)

		if req.Variables.Last == timelinePage {
			_, _ = io.WriteString(w, strings.Repeat(" ", maxResponseBytes+1))

			return
		}

		_, _ = w.Write(body)
	})

	review, err := client.FetchPullReview(t.Context(), NewToken(fakeToken), "PR_fakeNodeId94")
	require.NoError(t, err)
	assert.Len(t, review.Timeline.Items, 11)
	assert.Equal(t, []int{timelinePage, timelineSmallPage}, pages.all())
}

func TestFetchTimelineOfAPullRequest(t *testing.T) {
	t.Parallel()

	body := fixture(t, "pull_timeline_response.json")

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		req := readIssueRequest(t, r)
		assert.Contains(t, req.Query, "... on PullRequest {\n      number repository { nameWithOwner } headRefOid")
		assert.Contains(t, req.Query, "pullTimeline: timelineItems(last: $last, before: $before, itemTypes: "+
			pullTimelineItemTypes+")")
		assert.Contains(t, req.Query, "issueTimeline: timelineItems(last: $last, before: $before, itemTypes: "+
			timelineItemTypes+")")
		assert.Equal(t, "PR_fakeNodeId94", req.Variables.ID)
		assert.Equal(t, "Y3Vyc29yOnYyOpfakePullCursor94==", req.Variables.Before)

		_, _ = w.Write(body)
	})

	page, err := client.FetchTimeline(t.Context(), NewToken(fakeToken), item.KindPR, "PR_fakeNodeId94",
		"Y3Vyc29yOnYyOpfakePullCursor94==")
	require.NoError(t, err)

	assert.False(t, page.HasEarlier)
	assert.Empty(t, page.Before)
	assert.Equal(t, 3, page.Total)
	require.Len(t, page.Items, 3)

	assert.Equal(t, &ItemRef{
		Repo: "acme-org/example-repo", Number: 12, Title: "Pin the code scanning action",
		URL: "https://github.com/acme-org/example-repo/issues/12", State: StateOpen,
	}, page.Items[0].Ref, "the other side of a connection the pull request is on")

	dismissed := page.Items[1]
	assert.Equal(t, ReviewStateDismissed, dismissed.ReviewState, "a dismissed review is kept, though it says nothing")
	assert.True(t, dismissed.Outdated, "left on another commit than the head the page names")

	merged := page.Items[2]
	assert.Equal(t, TimelineMerged, merged.Type)
	assert.Equal(t, "4444444", merged.Commit)
	assert.Equal(t, "main", merged.Branch)
	assert.Equal(t, "https://github.com/acme-org/example-repo/pull/94#event-1", merged.URL)
	assert.Equal(t, "octo-maintainer", merged.Actor)
}

func TestFetchTimelineOfAPullRequestThatIsGoneOrIsAnother(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, serveItem(`{"data":{"node":null},"errors":[{"type":"NOT_FOUND","message":"gone"}]}`))

	_, err := client.FetchTimeline(t.Context(), NewToken(fakeToken), item.KindPR, "PR_gone", "cursor")
	require.ErrorIs(t, err, ErrPullGone)
	assert.Equal(t, "GitHub can't find the pull request any more, or your account can no longer see it.",
		uierr.Message(err))

	issue := fixture(t, "issue_timeline_response.json")

	_, err = newTestClient(t, serveItem(string(issue))).FetchTimeline(t.Context(), NewToken(fakeToken), item.KindPR,
		"I_fakeNodeId12", "cursor")
	require.ErrorIs(t, err, ErrMalformedResponse, "an issue's answer to a pull request's read")

	_, err = client.FetchTimeline(t.Context(), NewToken(fakeToken), "commit", "C_x", "cursor")
	require.ErrorIs(t, err, ErrInvalidItemRef)
}

func TestAReviewThatOnlyCommentsIsShownWithLineComments(t *testing.T) {
	t.Parallel()

	self := ItemRef{IsPR: true, Repo: "acme-org/example-repo", Number: 94}
	node := &graphQLTimelineNode{
		TypeName: TimelineReview, ID: "PRR_x", State: ReviewStateCommented,
		LineComments: &graphQLLineComments{TotalCount: 1, Nodes: []*graphQLLineComment{{ID: "PRRC_x", Path: "a.go"}}},
	}

	it, ok := node.item(self, sha1s("1"))
	require.True(t, ok, "a reply in a thread is a review that only holds its line comment")
	assert.False(t, it.Outdated, "a review that names no commit is never outdated")
	assert.Zero(t, it.LineComments[0].Line, "a comment with no line")

	node.LineComments = nil
	_, ok = node.item(self, sha1s("1"))
	assert.False(t, ok)

	assert.Empty(t, (*graphQLReviewer)(nil).name())
	assert.Empty(t, (*graphQLShortCommit)(nil).short())
}
