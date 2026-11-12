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

	"github.com/mrz1836/nilbox/internal/repo"
)

// The release the progress tests follow: acme-org/sdk v1.4.3, at its commit.
const (
	progressTag = "v1.4.3"
	progressSHA = "1111111111111111111111111111111111111111"

	// tagRunsPath is the path the tag's runs are read from.
	tagRunsPath = "/repos/acme-org/sdk/actions/runs"

	// conclusionCanceled is a run canceled, as GitHub spells it.
	conclusionCanceled = "cancelled" //nolint:misspell // GitHub's own spelling
)

// The progress query's answers: a drafted release, a published one, and none yet.
const (
	draftReleaseBody = `{"data": {"repository": {"release": {
		"url": "https://github.com/acme-org/sdk/releases/tag/untagged-1111111111111111111a", "isDraft": true,
		"publishedAt": null}}, "rateLimit": {"cost": 1}}}`
	publishedReleaseBody = `{"data": {"repository": {"release": {
		"url": "https://github.com/acme-org/sdk/releases/tag/v1.4.3", "isDraft": false,
		"publishedAt": "2026-11-11T10:30:00-05:00"}}, "rateLimit": {"cost": 1}}}`
	noReleaseBody = `{"data": {"repository": {"release": null}, "rateLimit": {"cost": 1}}}`
)

// progressRequest asks about the release the progress tests follow.
func progressRequest() ProgressRequest {
	return ProgressRequest{Repo: repo.Name{Owner: filesOwner, Repo: "sdk"}, Tag: progressTag, Commit: progressSHA}
}

// progressServer answers the progress query with release and the runs request with runs,
// as GitHub would, with its budget's headers on each.
func progressServer(t *testing.T, release string, runs []byte) (*Client, *recordRates) {
	t.Helper()

	return observingClient(t, func(w http.ResponseWriter, r *http.Request) {
		setRateHeaders(w, ResourceGraphQL, 4980, 20)

		if r.URL.Path == "/graphql" {
			_, _ = io.WriteString(w, release)

			return
		}

		_, _ = w.Write(runs)
	})
}

func TestFetchReleaseProgressReadsTheTagsOwnRuns(t *testing.T) {
	t.Parallel()

	runs := fixture(t, "tag_runs_response.json")

	var (
		mu    sync.Mutex
		paths []string
	)

	client, rates := observingClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer "+fakeToken, r.Header.Get("Authorization"))
		setRateHeaders(w, ResourceGraphQL, 4980, 20)

		mu.Lock()

		paths = append(paths, r.URL.Path)

		mu.Unlock()

		if r.URL.Path == "/graphql" {
			raw, err := io.ReadAll(r.Body)
			assert.NoError(t, err)

			var req graphQLRequest
			assert.NoError(t, json.Unmarshal(raw, &req))
			assert.Contains(t, req.Query, "release(tagName: $tag) { url isDraft publishedAt }")
			assert.Contains(t, req.Query, "rateLimit { cost }")
			assert.NotContains(t, req.Query, "checkSuites", "the runs come from the REST API")
			assert.Equal(t, map[string]any{"owner": filesOwner, "name": "sdk", "tag": progressTag}, req.Variables,
				"every value goes as a variable, never into the query's text")

			_, _ = io.WriteString(w, draftReleaseBody)

			return
		}

		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, tagRunsPath, r.URL.Path)
		assert.Equal(t, map[string][]string{
			"event": {"push"}, "branch": {progressTag}, "head_sha": {progressSHA},
			"exclude_pull_requests": {"true"}, "per_page": {"100"},
		}, map[string][]string(r.URL.Query()), "the tag's own push runs, at its commit")

		_, _ = w.Write(runs)
	})

	got, err := client.FetchReleaseProgress(t.Context(), NewToken(fakeToken), progressRequest())
	require.NoError(t, err)

	mu.Lock()
	assert.Equal(t, []string{"/graphql", tagRunsPath}, paths)
	mu.Unlock()
	require.NotNil(t, got.Release)
	assert.Equal(t, TagRelease{URL: "https://github.com/acme-org/sdk/releases/tag/untagged-1111111111111111111a", Draft: true},
		*got.Release, "the workflow drafted the release")
	assert.False(t, got.Published(), "a draft isn't published")
	assert.Equal(t, []WorkflowRun{
		{
			Name: "goreleaser", Status: runCompleted, Conclusion: conclusionFailure,
			URL: "https://github.com/acme-org/sdk/actions/runs/1000000005",
		},
		{Name: "CI", Status: "in_progress", URL: "https://github.com/acme-org/sdk/actions/runs/1000000004"},
	}, got.Runs, "the branch's run, another tag's, and a schedule run are left out")

	reading := RateReading{Resource: ResourceGraphQL, Limit: 5000, Remaining: 4980, Used: 20, ResetAt: rateResetAt}
	assert.Equal(t, []observed{{reading: reading, spent: 0}, {reading: reading, spent: 1}, {reading: reading, spent: 1}},
		rates.all(), "the budget hears the query's point and the runs request")
}

func TestFetchReleaseProgressOfAPublishedReleaseReadsNoRuns(t *testing.T) {
	t.Parallel()

	client, rates := progressServer(t, publishedReleaseBody, nil)

	got, err := client.FetchReleaseProgress(t.Context(), NewToken(fakeToken), progressRequest())
	require.NoError(t, err)

	assert.True(t, got.Published())
	assert.Equal(t, time.Date(2026, 11, 11, 15, 30, 0, 0, time.UTC), got.Release.PublishedAt)
	assert.NotNil(t, got.Runs)
	assert.Empty(t, got.Runs)
	assert.Len(t, rates.all(), 2, "one query, and no runs request: published says it all")
}

func TestFetchReleaseProgressBeforeTheWorkflowDraftsIt(t *testing.T) {
	t.Parallel()

	client, _ := progressServer(t, noReleaseBody, []byte(`{"total_count": 0, "workflow_runs": []}`))

	got, err := client.FetchReleaseProgress(t.Context(), NewToken(fakeToken), progressRequest())
	require.NoError(t, err)
	assert.Nil(t, got.Release)
	assert.False(t, got.Published())
	assert.NotNil(t, got.Runs)
	assert.Empty(t, got.Runs)
}

func TestFetchReleaseProgressFailsWhenTheRunsCantBeRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		runs string
		want error
	}{
		{name: "not JSON", runs: `nope`, want: ErrMalformedResponse},
		{name: "a refusal", runs: "", want: ErrUnexpectedStatus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/graphql" {
					_, _ = io.WriteString(w, draftReleaseBody)

					return
				}

				if tt.runs == "" {
					w.WriteHeader(http.StatusForbidden)
					_, _ = io.WriteString(w, `{"message": "Resource not accessible by integration"}`)

					return
				}

				_, _ = io.WriteString(w, tt.runs)
			})

			_, err := client.FetchReleaseProgress(t.Context(), NewToken(fakeToken), progressRequest())
			require.ErrorIs(t, err, tt.want)
			assert.NotContains(t, err.Error(), fakeToken)
		})
	}
}

func TestFetchReleaseProgressOfARepositoryGitHubCantFind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "GitHub says so",
			body: `{"data": {"repository": null, "rateLimit": {"cost": 1}}, "errors": [{"type": "NOT_FOUND",
				"path": ["repository"], "message": "Could not resolve to a Repository with the name 'acme-org/sdk'."}]}`,
			want: "Could not resolve",
		},
		{name: "GitHub says nothing", body: `{"data": {"repository": null}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/graphql", r.URL.Path, "no runs are asked for")

				_, _ = io.WriteString(w, tt.body)
			})

			_, err := client.FetchReleaseProgress(t.Context(), NewToken(fakeToken), progressRequest())
			require.ErrorIs(t, err, ErrRepositoryNotFound)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestFetchReleaseProgressFailsWithoutAnAnswer(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"errors": [{"type": "INTERNAL", "message": "Something went wrong"}]}`)
	})

	_, err := client.FetchReleaseProgress(t.Context(), NewToken(fakeToken), progressRequest())
	require.ErrorIs(t, err, ErrGraphQL)
	assert.NotContains(t, err.Error(), fakeToken)
}

func TestFetchReleaseProgressSendsNothingItCantAsk(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(http.ResponseWriter, *http.Request) {
		t.Error("nothing is asked")
	})

	sdk := repo.Name{Owner: filesOwner, Repo: "sdk"}

	for name, req := range map[string]ProgressRequest{
		"no repository":       {Tag: progressTag, Commit: progressSHA},
		"no tag":              {Repo: sdk, Commit: progressSHA},
		"no commit":           {Repo: sdk, Tag: progressTag},
		"a short commit":      {Repo: sdk, Tag: progressTag, Commit: "1111111"},
		"a commit not in hex": {Repo: sdk, Tag: progressTag, Commit: strings.Repeat("g", 40)},
	} {
		_, err := client.FetchReleaseProgress(t.Context(), NewToken(fakeToken), req)
		require.ErrorIs(t, err, ErrInvalidProgressRequest, name)
	}

	sha256 := ProgressRequest{Repo: sdk, Tag: progressTag, Commit: strings.Repeat("aB", 32)}
	assert.True(t, sha256.valid(), "a SHA-256 repository's commit")
}

func TestWhatCountsForARun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		run       WorkflowRun
		completed bool
		failed    bool
	}{
		{run: WorkflowRun{Status: "queued"}},
		{run: WorkflowRun{Status: "in_progress"}},
		{run: WorkflowRun{Status: runCompleted, Conclusion: "success"}, completed: true},
		{run: WorkflowRun{Status: runCompleted, Conclusion: conclusionFailure}, completed: true, failed: true},
		{run: WorkflowRun{Status: runCompleted, Conclusion: conclusionTimedOut}, completed: true, failed: true},
		{run: WorkflowRun{Status: runCompleted, Conclusion: conclusionStartupFailure}, completed: true, failed: true},
		{run: WorkflowRun{Status: runCompleted, Conclusion: conclusionCanceled}, completed: true},
		{run: WorkflowRun{Status: runCompleted, Conclusion: "skipped"}, completed: true},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.completed, tt.run.Completed(), "%+v completed", tt.run)
		assert.Equal(t, tt.failed, tt.run.Failed(), "%+v failed", tt.run)
	}

	published := time.Date(2026, 11, 11, 15, 30, 0, 0, time.UTC)

	assert.True(t, ReleaseProgress{Release: &TagRelease{PublishedAt: published}}.Published())
	assert.False(t, ReleaseProgress{}.Published(), "no release yet")
	assert.False(t, ReleaseProgress{Release: &TagRelease{Draft: true, PublishedAt: published}}.Published(), "a draft")
	assert.False(t, ReleaseProgress{Release: &TagRelease{}}.Published(), "not published yet")
}
