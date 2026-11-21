package github

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ownRequest is a merge state request as the fake server reads it.
type ownRequest struct {
	Query     string `json:"query"`
	Variables struct {
		ID  string `json:"id"`
		Own bool   `json:"own"`
	} `json:"variables"`
}

// ownNodeJSON is a whole own merge state answer's pull request, as GitHub answered one in
// November 2026: one signed commit, its checks passing, and a ruleset of five rules. HEAD
// stands for its head commit.
const ownNodeJSON = `{
  "__typename": "PullRequest", "mergeStateStatus": "BLOCKED", "headRefOid": "HEAD", "isDraft": false,
  "isMergeQueueEnabled": false, "prState": "OPEN", "viewerCanMergeAsAdmin": true, "autoMergeRequest": null,
  "reviewDecision": "REVIEW_REQUIRED", "viewerDidAuthor": true,
  "statusCheckRollup": {"state": "SUCCESS", "contexts": {"totalCount": 3, "nodes": [
    {"__typename": "CheckRun", "name": "All tests passed", "conclusion": "SUCCESS", "status": "COMPLETED"},
    {"__typename": "CheckRun", "name": "Queue", "conclusion": "SKIPPED", "status": "COMPLETED"},
    {"__typename": "StatusContext", "context": "ci/legacy", "state": "SUCCESS"}
  ]}},
  "ownCommits": {"totalCount": 1, "nodes": [
    {"commit": {"oid": "HEAD", "signature": {"isValid": true, "signer": {"login": "octo-user"}}}}
  ]},
  "reviewThreads": {"totalCount": 0, "nodes": []},
  "baseRef": {"branchProtectionRule": null, "rules": {"totalCount": 5, "nodes": [
    {"type": "COPILOT_CODE_REVIEW", "parameters": {"__typename": "CopilotCodeReviewParameters"}},
    {"type": "PULL_REQUEST", "parameters": {"__typename": "PullRequestParameters"}},
    {"type": "REQUIRED_STATUS_CHECKS", "parameters": {
      "__typename": "RequiredStatusChecksParameters", "requiredStatusChecks": [{"context": "All tests passed"}]
    }},
    {"type": "NON_FAST_FORWARD", "parameters": null},
    {"type": "DELETION", "parameters": null}
  ]}}
}`

// ownNode is ownNodeJSON, read to change.
func ownNode(t *testing.T) map[string]any {
	t.Helper()

	var n map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.ReplaceAll(ownNodeJSON, `"HEAD"`, `"`+sha1s("1")+`"`)), &n))

	return n
}

// set sets the value at path in n, a dotted path of keys: "baseRef.rules.totalCount".
func set(t *testing.T, n map[string]any, path string, value any) {
	t.Helper()

	keys := strings.Split(path, ".")
	at := n

	for _, key := range keys[:len(keys)-1] {
		next, ok := at[key].(map[string]any)
		require.True(t, ok, "%s has no object at %s", path, key)

		at = next
	}

	at[keys[len(keys)-1]] = value
}

// fromJSON reads raw JSON, a value a test sets.
func fromJSON(t *testing.T, raw string) any {
	t.Helper()

	var v any
	require.NoError(t, json.Unmarshal([]byte(raw), &v))

	return v
}

// jsonNull is JSON's null, a value a test sets.
const jsonNull = "null"

// ownAnswer is the answer holding node, with the viewer.
func ownAnswer(t *testing.T, node any) string {
	t.Helper()

	raw, err := json.Marshal(node)
	require.NoError(t, err)

	return `{"data":{"node":` + string(raw) + `,"viewer":{"login":"` + octoUser + `"},"rateLimit":{"cost":1}}}`
}

func TestFetchOwnMergeStateReadsWhatARoutineChecks(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		var req ownRequest
		assert.NoError(t, json.Unmarshal(raw, &req))
		assert.True(t, req.Variables.Own)
		assert.Equal(t, "PR_own", req.Variables.ID)

		for _, want := range []string{
			"query MergeState($id: ID!, $own: Boolean!)", "...OwnMergeFields @include(if: $own)",
			"viewer @include(if: $own) { login }", "fragment OwnMergeFields on PullRequest", "ownCommits: commits(last: 100)",
			"signature { isValid signer { login } }", "reviewThreads(first: 100) { totalCount nodes { isResolved } }",
			"rules(first: 50)", "... on RequiredStatusChecksParameters { requiredStatusChecks { context } }",
		} {
			assert.Contains(t, req.Query, want)
		}

		_, _ = io.WriteString(w, ownAnswer(t, ownNode(t)))
	})

	state, err := client.FetchOwnMergeState(t.Context(), NewToken(fakeToken), "PR_own")
	require.NoError(t, err)

	assert.Equal(t, MergeStateBlocked, state.Status)
	assert.True(t, state.CanBypass)
	require.NotNil(t, state.Own)
	assert.Equal(t, &OwnMerge{
		Viewer: octoUser, ViewerDidAuthor: true, ReviewDecision: ReviewDecisionRequired, Rollup: RollupSuccess,
		Contexts: []RollupContext{
			{Name: "All tests passed", State: RollupSuccess},
			{Name: "Queue", State: "SKIPPED"},
			{Name: "ci/legacy", State: RollupSuccess},
		},
		ContextCount: 3,
		Commits:      []SignedCommit{{OID: sha1s("1"), Signed: true, Valid: true, Signer: octoUser}},
		CommitCount:  1,
		BaseRead:     true, RulesRead: true, RuleCount: 5,
		Rules: []BranchRule{
			{Type: RuleCopilotCodeReview, Checks: []string{}, ParamsRead: true},
			{Type: RulePullRequest, Checks: []string{}, ParamsRead: true},
			{Type: RuleRequiredStatusChecks, Checks: []string{"All tests passed"}, ParamsRead: true},
			{Type: RuleNonFastForward, Checks: []string{}, ParamsRead: true},
			{Type: RuleDeletion, Checks: []string{}, ParamsRead: true},
		},
	}, state.Own)

	assert.True(t, state.Own.ChecksWhole())
	assert.True(t, state.Own.CommitsWhole())
	assert.True(t, state.Own.ThreadsWhole())
	assert.True(t, state.Own.RulesWhole())
	assert.True(t, state.Own.Contexts[0].Passed())
	assert.False(t, state.Own.Contexts[1].Passed(), "a skipped check didn't succeed")
}

func TestFetchMergeStateAsksForNoneOfTheOwnFields(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		var req ownRequest
		assert.NoError(t, json.Unmarshal(raw, &req))
		assert.False(t, req.Variables.Own)

		_, _ = io.WriteString(w, `{"data":{"node":{"__typename":"PullRequest","mergeStateStatus":"CLEAN",`+
			`"headRefOid":"`+sha1s("1")+`","prState":"OPEN"},"rateLimit":{"cost":1}}}`)
	})

	state, err := client.FetchMergeState(t.Context(), NewToken(fakeToken), "PR_x")
	require.NoError(t, err)
	assert.Nil(t, state.Own)
}

func TestFetchOwnMergeStateFailsClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		path  string
		value string
		check func(t *testing.T, o *OwnMerge)
	}{
		{
			name: "no rollup", path: "statusCheckRollup", value: jsonNull,
			check: func(t *testing.T, o *OwnMerge) { t.Helper(); assert.Empty(t, o.Rollup) },
		},
		{
			name: "the rollup's checks left out", path: "statusCheckRollup.contexts", value: jsonNull,
			check: func(t *testing.T, o *OwnMerge) { t.Helper(); assert.False(t, o.ChecksWhole()) },
		},
		{
			name: "more checks than a page", path: "statusCheckRollup.contexts.totalCount", value: `140`,
			check: func(t *testing.T, o *OwnMerge) { t.Helper(); assert.False(t, o.ChecksWhole()) },
		},
		{
			name: "more commits than a page", path: "ownCommits.totalCount", value: `101`,
			check: func(t *testing.T, o *OwnMerge) { t.Helper(); assert.False(t, o.CommitsWhole()) },
		},
		{
			name: "no commits read", path: "ownCommits", value: jsonNull,
			check: func(t *testing.T, o *OwnMerge) { t.Helper(); assert.False(t, o.CommitsWhole()) },
		},
		{
			name: "a null commit", path: "ownCommits", value: `{"totalCount": 1, "nodes": [null]}`,
			check: func(t *testing.T, o *OwnMerge) { t.Helper(); assert.False(t, o.CommitsWhole()) },
		},
		{
			name: "an unsigned commit", path: "ownCommits", value: `{"totalCount": 1, "nodes": [{"commit": {"oid": "c2"}}]}`,
			check: func(t *testing.T, o *OwnMerge) {
				t.Helper()
				assert.Equal(t, []SignedCommit{{OID: "c2"}}, o.Commits)
			},
		},
		{
			name: "a signature with no signer", path: "ownCommits",
			value: `{"totalCount": 1, "nodes": [{"commit": {"oid": "c2", "signature": {"isValid": true, "signer": null}}}]}`,
			check: func(t *testing.T, o *OwnMerge) {
				t.Helper()
				assert.Equal(t, []SignedCommit{{OID: "c2", Signed: true, Valid: true}}, o.Commits)
			},
		},
		{
			name: "unresolved threads", path: "reviewThreads",
			value: `{"totalCount": 2, "nodes": [{"isResolved": true}, {"isResolved": false}]}`,
			check: func(t *testing.T, o *OwnMerge) {
				t.Helper()
				assert.Equal(t, 1, o.Unresolved)
				assert.True(t, o.ThreadsWhole())
			},
		},
		{
			name: "more threads than a page", path: "reviewThreads.totalCount", value: `101`,
			check: func(t *testing.T, o *OwnMerge) { t.Helper(); assert.False(t, o.ThreadsWhole()) },
		},
		{
			name: "no threads read", path: "reviewThreads", value: jsonNull,
			check: func(t *testing.T, o *OwnMerge) { t.Helper(); assert.False(t, o.ThreadsWhole()) },
		},
		{
			name: "no base branch", path: "baseRef", value: jsonNull,
			check: func(t *testing.T, o *OwnMerge) { t.Helper(); assert.False(t, o.RulesWhole()) },
		},
		{
			name: "no rules read", path: "baseRef.rules", value: jsonNull,
			check: func(t *testing.T, o *OwnMerge) { t.Helper(); assert.False(t, o.RulesWhole()) },
		},
		{
			name: "more rules than a page", path: "baseRef.rules.totalCount", value: `51`,
			check: func(t *testing.T, o *OwnMerge) { t.Helper(); assert.False(t, o.RulesWhole()) },
		},
		{
			name: "a required-checks rule with no parameters", path: "baseRef.rules",
			value: `{"totalCount": 1, "nodes": [{"type": "REQUIRED_STATUS_CHECKS", "parameters": null}]}`,
			check: func(t *testing.T, o *OwnMerge) { t.Helper(); assert.False(t, o.RulesWhole()) },
		},
		{
			name: "a required check with no name", path: "baseRef.rules",
			value: `{"totalCount": 1, "nodes": [{"type": "REQUIRED_STATUS_CHECKS", "parameters": ` +
				`{"__typename": "RequiredStatusChecksParameters", "requiredStatusChecks": [{"context": ""}]}}]}`,
			check: func(t *testing.T, o *OwnMerge) { t.Helper(); assert.False(t, o.RulesWhole()) },
		},
		{
			name: "a null rule", path: "baseRef.rules", value: `{"totalCount": 1, "nodes": [null]}`,
			check: func(t *testing.T, o *OwnMerge) { t.Helper(); assert.False(t, o.RulesWhole()) },
		},
		{
			name: "classic protection", path: "baseRef.branchProtectionRule",
			value: `{"requiredStatusCheckContexts": ["build"], "requiresDeployments": true, "lockBranch": true, ` +
				`"requiresLinearHistory": true}`,
			check: func(t *testing.T, o *OwnMerge) {
				t.Helper()
				assert.Equal(t, &ClassicProtection{
					RequiredChecks: []string{"build"}, RequiresDeployments: true, LockBranch: true, RequiresLinear: true,
				}, o.Protection)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			node := ownNode(t)
			set(t, node, tt.path, fromJSON(t, tt.value))

			client := newTestClient(t, serveItem(ownAnswer(t, node)))

			state, err := client.FetchOwnMergeState(t.Context(), NewToken(fakeToken), "PR_own")
			require.NoError(t, err)
			require.NotNil(t, state.Own)
			tt.check(t, state.Own)
		})
	}
}

func TestFetchOwnMergeStateRefusesAPartialAnswer(t *testing.T) {
	t.Parallel()

	head := `"headRefOid":"` + sha1s("1") + `"`

	tests := []struct {
		name string
		body string
		want error
	}{
		{
			name: "an error beside the data",
			body: strings.TrimSuffix(ownAnswer(t, ownNode(t)), "}") + `,"errors":[{"type":"FORBIDDEN","message":"no rules"}]}`,
			want: ErrGraphQL,
		},
		{
			name: "no viewer", body: `{"data":{"node":{"__typename":"PullRequest",` + head + `,"prState":"OPEN"}}}`,
			want: ErrMalformedResponse,
		},
		{
			name: "a bad own field",
			body: `{"data":{"node":{"__typename":"PullRequest",` + head + `,"viewerDidAuthor":"x"},"viewer":{"login":"` +
				octoUser + `"}}}`,
			want: ErrMalformedResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, serveItem(tt.body))

			_, err := client.FetchOwnMergeState(t.Context(), NewToken(fakeToken), "PR_own")
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestFetchOwnMergeStateOfAPullRequestThatIsGone(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, serveItem(`{"data":{"node":null,"viewer":{"login":"`+octoUser+`"}},`+
		`"errors":[{"type":"NOT_FOUND","message":"gone"}]}`))

	state, err := client.FetchOwnMergeState(t.Context(), NewToken(fakeToken), "PR_gone")
	require.NoError(t, err)
	assert.False(t, state.Found)
	assert.Nil(t, state.Own)
}
