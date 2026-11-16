package github

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The pull requests checks_response.json answers: one whose checks failed, one whose
// checks passed.
const (
	failingChecksPR = "PR_NTcSg12ylCGP3VhAx6p6hn"
	passingChecksPR = "PR_QE1wDWEF0g7mkfxdqy8v-Y" //nolint:gosec // a fake node ID, not a credential
)

func TestFetchChecksReadsEachPullRequest(t *testing.T) {
	t.Parallel()

	body := fixture(t, "checks_response.json")
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query := watchRequest(t, r)
		assert.Contains(t, query, `p0: node(id: "`+failingChecksPR+`")`)
		assert.Contains(t, query, `isRequired(pullRequestId: "`+failingChecksPR+`")`,
			"whether a check is required is asked with the pull request's own ID")
		assert.Contains(t, query, "startedAt completedAt")
		assert.Contains(t, query, "fragment CheckFields on Node")

		_, _ = w.Write(body)
	})

	got, err := client.FetchChecks(t.Context(), NewToken(fakeToken), []string{failingChecksPR, passingChecksPR, failingChecksPR})
	require.NoError(t, err)
	assert.Equal(t, 1, got.Requests)
	assert.Equal(t, 1, got.RateLimit.Cost)
	require.Len(t, got.Items, 2)

	failing := got.Items[failingChecksPR]
	assert.True(t, failing.Complete)
	assert.Len(t, failing.Checks, 47, "every run, a name repeated once per event that ran it")
	assert.True(t, strings.HasPrefix(failing.Head, "90a6cadf"))

	var gate HeadCheck

	required := 0

	for _, c := range failing.Checks {
		if c.Name == "Frontend gate" {
			gate = c
		}

		if c.Required != nil && *c.Required {
			required++
		}
	}

	assert.Equal(t, CheckKindRun, gate.Kind)
	assert.Equal(t, CheckFailed, gate.Outcome)
	assert.Equal(t, "Frontend", gate.Workflow)
	assert.Equal(t, time.Date(2026, 11, 15, 23, 38, 52, 0, time.UTC), gate.CompletedAt)
	assert.Equal(t, 1, required)

	settled, ok := failing.Settled()
	assert.True(t, ok, "every check finished")
	assert.False(t, settled.IsZero())

	passing := got.Items[passingChecksPR]
	assert.Len(t, passing.Checks, 37)
}

func TestFetchChecksSettlesOnlyOnceEveryCheckFinished(t *testing.T) {
	t.Parallel()

	done := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	running := HeadChecks{Complete: true, Checks: []HeadCheck{
		{Name: "lint", Outcome: CheckPassed, CompletedAt: done},
		{Name: "test", Outcome: CheckPending},
	}}

	_, ok := running.Settled()
	assert.False(t, ok, "a check still runs")

	finished := HeadChecks{Complete: true, Checks: []HeadCheck{
		{Name: "lint", Outcome: CheckPassed, CompletedAt: done},
		{Name: "skipped", Outcome: CheckSkipped, StartedAt: done.Add(time.Minute), CompletedAt: done.Add(-time.Minute)},
	}}

	at, ok := finished.Settled()
	assert.True(t, ok)
	assert.Equal(t, done, at, "the newest completion, never a start")

	_, ok = HeadChecks{Complete: true, Checks: []HeadCheck{}}.Settled()
	assert.False(t, ok, "no checks never settle")
}

func TestFetchChecksReadsStatusesAndAppRuns(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"p0":{"__typename":"PullRequest","id":"PR_a","headRefOid":"abc",
"checkList":{"state":"PENDING","contexts":{"totalCount":3,"pageInfo":{"hasNextPage":false},"nodes":[
{"__typename":"CheckRun","name":"CodeQL","conclusion":"NEUTRAL","status":"COMPLETED","completedAt":"2026-11-15T10:00:00Z",
 "checkSuite":{"app":{"name":"GitHub Advanced Security"},"workflowRun":null}},
{"__typename":"StatusContext","context":"ci/legacy","state":"PENDING","createdAt":"2026-11-15T10:01:00Z"},
{"__typename":"CheckRun","name":"build","conclusion":null,"status":"IN_PROGRESS","completedAt":null,
 "checkSuite":{"workflowRun":{"workflow":{"name":"CI"}}}}]}},
"req":{"contexts":{"nodes":[{"isRequired":false},{"isRequired":true},{"isRequired":true}]}}},
"p1":null,"rateLimit":{"cost":1,"limit":5000,"remaining":4990,"used":10,"resetAt":"2026-11-16T06:43:00Z"}},
"errors":[{"type":"NOT_FOUND","path":["p1"],"message":"Could not resolve to a node"}]}`)
	})

	got, err := client.FetchChecks(t.Context(), NewToken(fakeToken), []string{"PR_a", "PR_gone"})
	require.NoError(t, err)
	require.Len(t, got.Items, 1, "a pull request GitHub couldn't find is left out")

	checks := got.Items["PR_a"].Checks
	require.Len(t, checks, 3)
	assert.Empty(t, checks[0].Workflow, "an app's own run has no workflow")
	assert.Equal(t, "GitHub Advanced Security", checks[0].App)
	assert.Equal(t, CheckPassed, checks[0].Outcome)
	assert.Equal(t, CheckKindStatus, checks[1].Kind)
	assert.Equal(t, CheckPending, checks[1].Outcome)
	assert.True(t, checks[1].CompletedAt.IsZero(), "a pending status hasn't completed")
	assert.Equal(t, CheckPending, checks[2].Outcome)
	assert.Equal(t, "CI", checks[2].Workflow)
	require.NotNil(t, checks[2].Required)
	assert.True(t, *checks[2].Required)
}

func TestDetailsReadTheMergeState(t *testing.T) {
	t.Parallel()

	body := fixture(t, "details_merge_state_response.json")
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query := watchRequest(t, r)
		assert.Contains(t, query, "baseRefName mergeStateStatus")
		assert.NotContains(t, query, "DetailRepo", "the repository is read only when asked for")

		_, _ = w.Write(body)
	})

	got, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{"PR_C2mOimpF9GAaaU0q"}, DetailOptions{})
	require.NoError(t, err)

	d, ok := got.Items["PR_C2mOimpF9GAaaU0q"]
	require.True(t, ok)
	assert.Equal(t, "main", d.BaseRef)
	assert.NotEmpty(t, d.MergeState)
	assert.Nil(t, d.Repo)
}

func TestDetailsReadTheRepositoryWhenAsked(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query := watchRequest(t, r)
		assert.Contains(t, query, "...DetailFields ...DetailRepo")
		assert.Contains(t, query, "fragment RepoFields on Repository")

		_, _ = io.WriteString(w, `{"data":{"nodes":[{"__typename":"PullRequest","id":"PR_a","baseRefName":"main",
"mergeStateStatus":"BEHIND","repository":{"id":"R_a","name":"example-repo","owner":{"login":"acme-org"},
"defaultBranchRef":{"name":"main"},"viewerPermission":"WRITE","allowUpdateBranch":true}}],
"rateLimit":{"cost":1,"limit":5000,"remaining":4990,"used":10,"resetAt":"2026-11-16T06:43:00Z"}}}`)
	})

	got, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{"PR_a"}, DetailOptions{Repository: true})
	require.NoError(t, err)

	d := got.Items["PR_a"]
	assert.Equal(t, MergeStateBehind, d.MergeState)
	require.NotNil(t, d.Repo)
	assert.Equal(t, "acme-org", d.Repo.Owner)
	assert.Equal(t, "example-repo", d.Repo.Name)
	assert.Equal(t, "main", d.Repo.Repo.DefaultBranch)
	assert.True(t, d.Repo.Repo.AllowUpdateBranch)
}

func TestDetailsCanBeReadInFixedBatchesKeepingWhatSucceeded(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		sizes []int
	)

	ids := make([]string, 0, 32)
	for i := range 32 {
		ids = append(ids, fmt.Sprintf("I_fakeNodeId%d", i))
	}

	record := func(size int) {
		mu.Lock()
		defer mu.Unlock()

		sizes = append(sizes, size)
	}

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		req := readDetailsRequest(t, r)
		record(len(req.Variables.IDs))

		if slices.Contains(req.Variables.IDs, "I_fakeNodeId0") {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "<html>502 Bad Gateway</html>")

			return
		}

		nodes := make([]string, 0, len(req.Variables.IDs))
		for _, id := range req.Variables.IDs {
			nodes = append(nodes, fmt.Sprintf(`{"__typename":"Issue","id":%q,"comments":{"totalCount":1},"labels":{"nodes":[]}}`, id))
		}

		_, _ = fmt.Fprintf(w, `{"data":{"nodes":[%s],"rateLimit":{"cost":1,"limit":5000,"remaining":4990,"used":10,`+
			`"resetAt":"2026-01-10T13:00:00Z"}}}`, strings.Join(nodes, ","))
	})

	got, err := client.FetchDetails(t.Context(), NewToken(fakeToken), ids, DetailOptions{Batch: 15, Partial: true})
	require.ErrorIs(t, err, ErrUnexpectedStatus, "the batch that failed says so")
	require.NotNil(t, got)
	assert.Len(t, got.Items, 17, "the batches that succeeded are kept")
	assert.Equal(t, 2, got.Requests)

	mu.Lock()
	slices.Sort(sizes)
	assert.Equal(t, []int{2, 15, 15}, sizes, "fixed batches, so a few IDs cost one request")
	mu.Unlock()

	_, err = client.FetchDetails(t.Context(), NewToken(fakeToken), ids[:3], DetailOptions{Batch: 15})
	require.Error(t, err, "without Partial, a batch that fails fails the call")
}
