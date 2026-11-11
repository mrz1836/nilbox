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

	"github.com/mrz1836/nilbox/internal/repo"
)

// The release the progress tests follow: acme-org/sdk v1.4.3, at its commit, released
// from master.
const (
	progressTag    = "v1.4.3"
	progressSHA    = "1111111111111111111111111111111111111111"
	progressBranch = "master"

	// conclusionCanceled is a run canceled, as GitHub's CheckConclusionState spells it.
	conclusionCanceled = "CANCELLED" //nolint:misspell // GitHub's own spelling
)

// progressRequest asks about the release the progress tests follow.
func progressRequest() ProgressRequest {
	return ProgressRequest{Repo: repo.Name{Owner: filesOwner, Repo: "sdk"}, Tag: progressTag, Commit: progressSHA}
}

// readProgressRequest reads the request a progress query sends. It runs in a handler, so
// it reports a problem without stopping the test.
func readProgressRequest(t *testing.T, r *http.Request) graphQLRequest {
	t.Helper()

	raw, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	var req graphQLRequest
	assert.NoError(t, json.Unmarshal(raw, &req))

	return req
}

func TestFetchReleaseProgressReadsTheTagsCommit(t *testing.T) {
	t.Parallel()

	body := fixture(t, "release_progress_response.json")
	client, rates := observingClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/graphql", r.URL.Path)
		assert.Equal(t, "Bearer "+fakeToken, r.Header.Get("Authorization"))

		req := readProgressRequest(t, r)
		for _, part := range []string{
			"repository(owner: $owner, name: $name)",
			"release(tagName: $tag) { url isDraft publishedAt }",
			"object(oid: $oid)",
			"checkSuites(last: 50)",
			"app { slug name }",
			"branch { name }",
			"workflowRun { url event workflow { name } }",
			"rateLimit { cost }",
		} {
			assert.Contains(t, req.Query, part)
		}

		assert.Equal(t, map[string]any{
			"owner": filesOwner, "name": "sdk", "tag": progressTag, "oid": progressSHA,
		}, req.Variables, "every value goes as a variable, never into the query's text")

		setRateHeaders(w, ResourceGraphQL, 4980, 20)
		_, _ = w.Write(body)
	})

	got, err := client.FetchReleaseProgress(t.Context(), NewToken(fakeToken), progressRequest())
	require.NoError(t, err)

	require.NotNil(t, got.Release)
	assert.Equal(t, TagRelease{URL: "https://github.com/acme-org/sdk/releases/tag/untagged-1111111111111111111a", Draft: true},
		*got.Release, "the workflow drafted the release")
	require.Len(t, got.Suites, 8, "a null suite is left out")
	assert.Equal(t, CheckSuite{
		App: actionsApp, AppName: "GitHub Actions", Status: suiteCompleted, Conclusion: conclusionFailure,
		Branch: progressBranch, Event: pushEvent, RunURL: "https://github.com/acme-org/sdk/actions/runs/1000000001",
		Workflow: "CI",
	}, got.Suites[0], "the branch's own push run")
	assert.Equal(t, CheckSuite{App: "codecov", AppName: "Codecov", Status: "QUEUED", Branch: progressBranch}, got.Suites[1])
	assert.Equal(t, CheckSuite{
		App: actionsApp, AppName: "GitHub Actions", Status: "IN_PROGRESS", Event: pushEvent,
		RunURL: "https://github.com/acme-org/sdk/actions/runs/1000000003", Workflow: "CI",
	}, got.Suites[6], "the tag's own push run")

	assert.False(t, got.Published(), "a draft isn't published")

	_, failed := got.Failed(progressTag)
	assert.False(t, failed, "apps' queued suites, failed schedule, dynamic, and branch runs say nothing of the release")

	reading := RateReading{Resource: ResourceGraphQL, Limit: 5000, Remaining: 4980, Used: 20, ResetAt: rateResetAt}
	assert.Equal(t, []observed{{reading: reading, spent: 0}, {reading: reading, spent: 1}}, rates.all(),
		"the budget hears the reading, then the query's cost of one point")
}

func TestFetchReleaseProgressReadsAPublishedRelease(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data": {"repository": {
			"release": {"url": "https://github.com/acme-org/sdk/releases/tag/v1.4.3", "isDraft": false,
				"publishedAt": "2026-11-11T10:30:00-05:00"},
			"object": {"checkSuites": {"nodes": [
				{"app": {"slug": "github-actions", "name": "GitHub Actions"}, "status": "COMPLETED",
					"conclusion": "SUCCESS", "branch": null, "workflowRun": {"url": "https://github.com/acme-org/sdk/actions/runs/7",
					"event": "push", "workflow": null}},
				{"app": null, "status": "QUEUED", "conclusion": null, "branch": null, "workflowRun": null}
			]}}
		}, "rateLimit": {"cost": 1}}}`)
	})

	got, err := client.FetchReleaseProgress(t.Context(), NewToken(fakeToken), progressRequest())
	require.NoError(t, err)

	assert.True(t, got.Published())
	assert.Equal(t, time.Date(2026, 11, 11, 15, 30, 0, 0, time.UTC), got.Release.PublishedAt)
	assert.Equal(t, []CheckSuite{
		{
			App: actionsApp, AppName: "GitHub Actions", Status: suiteCompleted, Conclusion: "SUCCESS", Event: pushEvent,
			RunURL: "https://github.com/acme-org/sdk/actions/runs/7",
		},
		{Status: "QUEUED"},
	}, got.Suites)
}

func TestFetchReleaseProgressBeforeTheWorkflowDraftsIt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		object string
	}{
		{name: "the commit with no suites yet", object: `{"checkSuites": {"nodes": []}}`},
		{name: "a commit GitHub doesn't have", object: `null`},
		{name: "a commit that isn't one", object: `{"checkSuites": null}`},
		{name: "an answer that can't be read", object: `[]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `{"data": {"repository": {"release": null, "object": `+tt.object+`},
					"rateLimit": {"cost": 1}}}`)
			})

			got, err := client.FetchReleaseProgress(t.Context(), NewToken(fakeToken), progressRequest())
			require.NoError(t, err)
			assert.Nil(t, got.Release)
			assert.NotNil(t, got.Suites)
			assert.Empty(t, got.Suites)
			assert.False(t, got.Published())
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

			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
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

func TestWhatCountsForARelease(t *testing.T) {
	t.Parallel()

	published := &TagRelease{
		URL:         "https://github.com/acme-org/sdk/releases/tag/v1.4.3",
		PublishedAt: time.Date(2026, 11, 11, 15, 30, 0, 0, time.UTC),
	}
	tagRun := func(status, conclusion string) CheckSuite {
		return CheckSuite{
			App: actionsApp, Status: status, Conclusion: conclusion, Event: pushEvent,
			RunURL: "https://github.com/acme-org/sdk/actions/runs/3",
		}
	}
	failedTagRun := tagRun(suiteCompleted, conclusionFailure)

	tests := []struct {
		name      string
		progress  ReleaseProgress
		published bool
		failed    bool
	}{
		{name: "nothing yet", progress: ReleaseProgress{}},
		{name: "published", progress: ReleaseProgress{Release: published}, published: true},
		{
			name:     "a draft",
			progress: ReleaseProgress{Release: &TagRelease{Draft: true, PublishedAt: published.PublishedAt}},
		},
		{name: "a release not published yet", progress: ReleaseProgress{Release: &TagRelease{URL: published.URL}}},
		{
			name:     "the tag's run failed",
			progress: ReleaseProgress{Suites: []CheckSuite{failedTagRun}}, failed: true,
		},
		{
			name:     "the tag's run timed out",
			progress: ReleaseProgress{Suites: []CheckSuite{tagRun(suiteCompleted, conclusionTimedOut)}}, failed: true,
		},
		{
			name:     "the tag's run never started",
			progress: ReleaseProgress{Suites: []CheckSuite{tagRun(suiteCompleted, conclusionStartupFailure)}}, failed: true,
		},
		{
			name:     "the tag's run, under the tag's own name",
			progress: ReleaseProgress{Suites: []CheckSuite{{App: actionsApp, Status: suiteCompleted, Conclusion: conclusionFailure, Event: pushEvent, Branch: progressTag}}},
			failed:   true,
		},
		{name: "the tag's run was canceled", progress: ReleaseProgress{Suites: []CheckSuite{tagRun(suiteCompleted, conclusionCanceled)}}},
		{name: "the tag's run succeeded", progress: ReleaseProgress{Suites: []CheckSuite{tagRun(suiteCompleted, "SUCCESS")}}},
		{name: "the tag's run still goes", progress: ReleaseProgress{Suites: []CheckSuite{tagRun("IN_PROGRESS", "")}}},
		{
			name:     "one of the tag's runs failed while another still goes",
			progress: ReleaseProgress{Suites: []CheckSuite{failedTagRun, tagRun("QUEUED", "")}},
		},
		{
			name:     "published, though the tag's run failed after",
			progress: ReleaseProgress{Release: published, Suites: []CheckSuite{failedTagRun}}, published: true, failed: true,
		},
		{
			name: "another app's suite queued forever",
			progress: ReleaseProgress{Suites: []CheckSuite{
				{App: "codecov", Status: "QUEUED"}, {App: "example-bot", Status: "QUEUED"}, tagRun(suiteCompleted, "SUCCESS"),
			}},
		},
		{
			name:     "another app's suite failed",
			progress: ReleaseProgress{Suites: []CheckSuite{{App: "example-bot", Status: suiteCompleted, Conclusion: conclusionFailure}}},
		},
		{
			name: "a schedule run failed on the commit",
			progress: ReleaseProgress{Suites: []CheckSuite{
				{App: actionsApp, Status: suiteCompleted, Conclusion: conclusionFailure, Event: "schedule"},
			}},
		},
		{
			name: "a dynamic run failed on the commit",
			progress: ReleaseProgress{Suites: []CheckSuite{
				{App: actionsApp, Status: suiteCompleted, Conclusion: conclusionFailure, Event: "dynamic"},
			}},
		},
		{
			name: "the branch's own push run failed",
			progress: ReleaseProgress{Suites: []CheckSuite{
				{App: actionsApp, Status: suiteCompleted, Conclusion: conclusionFailure, Event: pushEvent, Branch: progressBranch},
				tagRun("IN_PROGRESS", ""),
			}},
		},
		{
			name: "the branch's own push run still goes after the tag's failed",
			progress: ReleaseProgress{Suites: []CheckSuite{
				{App: actionsApp, Status: "IN_PROGRESS", Event: pushEvent, Branch: progressBranch}, failedTagRun,
			}},
			failed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.published, tt.progress.Published(), "published")

			run, failed := tt.progress.Failed(progressTag)
			assert.Equal(t, tt.failed, failed, "failed")

			if tt.failed {
				assert.True(t, run.TagRun(progressTag), "the failed run is the tag's own")
			} else {
				assert.Equal(t, CheckSuite{}, run)
			}
		})
	}
}
