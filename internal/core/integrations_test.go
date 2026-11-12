package core

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/release"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// errSignedOut is what gh says when no account is signed in.
var errSignedOut = errors.New("not logged in to any GitHub hosts")

// countWakes counts how often it is woken.
type countWakes struct {
	n atomic.Int32
}

func (w *countWakes) Wake() { w.n.Add(1) }

func TestIntegrationsChangedWakesTheToolFinder(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	tools := &countWakes{}
	relay := &followSwitch{sink: events, tools: tools}

	relay.Emit(event.IntegrationsChanged{CopyChosen: true})
	assert.Equal(t, int32(1), tools.n.Load(), "a chosen copy: look for the tools again")
	assert.Empty(t, events.All(), "the finder tells the window once it has looked")

	relay.Emit(event.IntegrationsChanged{})
	relay.Emit(event.InboxUpdated{})

	assert.Equal(t, int32(1), tools.n.Load(), "a flipped switch changes no detection")
	assert.Equal(t, []event.Event{event.ToolsUpdated{}, event.InboxUpdated{}}, events.All(),
		"so the window hears at once that the list changed; the rest pass on")
}

func TestAChangedIntervalWakesWhatRefreshesAndStaysInGo(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	inboxRefresh, checks, sweep := &countWakes{}, &countWakes{}, &countWakes{}
	relay := &followSwitch{sink: events, intervals: []waker{inboxRefresh, checks, sweep}}

	relay.Emit(event.IntervalsChanged{})

	for _, w := range []*countWakes{inboxRefresh, checks, sweep} {
		assert.Equal(t, int32(1), w.n.Load())
	}

	assert.Empty(t, events.All(), "the window has the settings from the setter already")
}

func TestRefreshEveryReadsTheSettingAsItIsNow(t *testing.T) {
	t.Parallel()

	opts := testOptions(t)
	opts.Runner = &testutil.Runner{}
	c := bootstrap(t, opts)
	inboxEvery := refreshEvery(c.Settings, settings.RefreshInbox, nil)
	checksEvery := refreshEvery(c.Settings, settings.RefreshChecks, nil)

	assert.Equal(t, 5*time.Minute, inboxEvery(t.Context()))
	assert.Equal(t, 45*time.Second, checksEvery(t.Context()))

	_, err := c.Settings.SetRefreshInterval(t.Context(), settings.RefreshInbox, 900)
	require.NoError(t, err)
	_, err = c.Settings.SetRefreshInterval(t.Context(), settings.RefreshChecks, 0)
	require.NoError(t, err)

	assert.Equal(t, 15*time.Minute, inboxEvery(t.Context()))
	assert.Zero(t, checksEvery(t.Context()), "off")
}

func TestLatestReleasesAdaptsGitHubsAnswer(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"t0":{"latestRelease":{"tagName":"v1.30.2",
			"url":"https://github.com/acme-org/example-tool/releases/tag/v1.30.2","publishedAt":"2026-11-10T04:00:00Z",
			"releaseAssets":{"nodes":[{"name":"example-tool_1.30.2_darwin_arm64.tar.gz",
			"downloadUrl":"https://github.com/acme-org/example-tool/releases/download/v1.30.2/example-tool_1.30.2_darwin_arm64.tar.gz",
			"size":42,"digest":"sha256:abc"}]}}},"rateLimit":{"cost":1}}}`)
	}))
	t.Cleanup(srv.Close)

	releases := latestReleases{
		tokens: github.NewTokenSource(&testutil.Runner{Stdout: "test-token-not-real\n"}, testutil.Finder{Path: "/opt/fake/bin/gh"}),
		client: &github.Client{BaseURL: srv.URL, HTTP: srv.Client()},
	}

	tool := repo.Name{Owner: boundOwner, Repo: "example-tool"}

	got, err := releases.LatestReleases(t.Context(), []repo.Name{tool})
	require.NoError(t, err)
	assert.Equal(t, map[string]integrations.Release{tool.Key(): {
		Tag: "v1.30.2", URL: "https://github.com/acme-org/example-tool/releases/tag/v1.30.2",
		PublishedAt: time.Date(2026, 11, 10, 4, 0, 0, 0, time.UTC),
		Assets: []integrations.Asset{{
			Name: "example-tool_1.30.2_darwin_arm64.tar.gz",
			URL:  "https://github.com/acme-org/example-tool/releases/download/v1.30.2/example-tool_1.30.2_darwin_arm64.tar.gz",
			Size: 42, Digest: "sha256:abc",
		}},
	}}, got)
}

func TestLatestReleasesAsksNothingWhenSignedOut(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(srv.Close)

	releases := latestReleases{
		tokens: github.NewTokenSource(&testutil.Runner{Err: errSignedOut}, testutil.Finder{Path: "/opt/fake/bin/gh"}),
		client: &github.Client{BaseURL: srv.URL, HTTP: srv.Client()},
	}

	_, err := releases.LatestReleases(t.Context(), []repo.Name{{Owner: boundOwner, Repo: "example-tool"}})
	require.ErrorIs(t, err, integrations.ErrSignedOut, "the integrations can say to sign in")
	assert.Zero(t, requests.Load(), "no check runs while signed out")
}

func TestARunsChangeReachesTheRunsAndTheTools(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	opts := testOptions(t)
	opts.Events = events
	c := bootstrap(t, opts)

	before := len(events.All())

	require.NoError(t, c.ScriptRuns.ClearHistory(t.Context()))
	assert.Equal(t, []event.Event{event.ScriptRunsUpdated{}, event.ToolsUpdated{}}, events.All()[before:],
		"the window lists the runs again, and the tools, whose updates a run can block")
}

func TestReleaseSettingsReadWhetherAMergeOffersARelease(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t))
	adapter := releaseSettings{settings: c.Settings}

	on, err := adapter.OfferAfterMerge(t.Context())
	require.NoError(t, err)
	assert.True(t, on, "on until the user turns it off")

	_, err = c.Settings.SetOfferAfterMerge(t.Context(), false)
	require.NoError(t, err)

	on, err = adapter.OfferAfterMerge(t.Context())
	require.NoError(t, err)
	assert.False(t, on)

	got, err := c.Release.OfferAfterMerge(t.Context(), boundOwner, boundRepo, "main")
	require.NoError(t, err)
	assert.False(t, got.Offer, "the setting is off")

	require.NoError(t, c.Close())

	_, err = adapter.OfferAfterMerge(t.Context())
	require.Error(t, err, "a closed database can't say")
}

func TestReleaseNoticesRecordARelease(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	opts := testOptions(t)
	opts.Events = events
	c := bootstrap(t, opts)

	pool := c.DB
	releaseNotices{dispatcher: notify.NewDispatcher(notify.Config{Pool: pool, Events: events})}.Released(t.Context(),
		release.Notice{
			Repo: repo.Name{Owner: boundOwner, Repo: boundRepo}, Title: "v1.4.3 is on GitHub", Body: "Tagged abc1234 on main.",
			URL: "https://github.com/acme-org/example-repo/releases/tag/v1.4.3",
		})

	log, err := c.Notifications.List(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, 1)
	assert.Equal(t, notify.KindReleaseDone, log.Items[0].Kind)
	assert.Equal(t, "acme-org/example-repo", log.Items[0].Repo)
	assert.Equal(t, "v1.4.3 is on GitHub", log.Items[0].Title)
	assert.Equal(t, "Tagged abc1234 on main.", log.Items[0].Detail)
	assert.Empty(t, log.Items[0].ItemKind, "about no item")
	assert.Contains(t, events.All(), event.Event(event.NotificationsUpdated{}))
}

func TestRunFailuresRecordAFailedRun(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	opts := testOptions(t)
	opts.Events = events
	c := bootstrap(t, opts)

	dispatcher := notify.NewDispatcher(notify.Config{Pool: c.DB, Events: events})
	t.Cleanup(dispatcher.Close)

	started := time.Date(2026, 11, 10, 16, 0, 0, 0, time.UTC)
	failures := runFailures{dispatcher: dispatcher}
	failures.Failed(t.Context(), scriptrun.Failure{
		ID: 7, Started: started, Repo: repo.Name{Owner: boundOwner, Repo: boundRepo},
		Title: "Checks failed: Checks · all files", Body: "4 passed, 1 failed: whitespace.",
	})
	failures.Failed(t.Context(), scriptrun.Failure{
		ID: 8, Started: started, Title: "A tool's update failed in ~/go/bin: Update magex",
		Body: "Timed out after 10 minutes.",
	})

	log, err := c.Notifications.List(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, 2, "recorded at once, while their banners gather")
	assert.Equal(t, notify.Notification{
		ID: log.Items[1].ID, Kind: notify.KindRunFailed, CreatedAt: log.Items[1].CreatedAt, Repo: "acme-org/example-repo",
		Title: "Checks failed: Checks · all files", Detail: "4 passed, 1 failed: whitespace.", RunID: 7,
		RunStarted: clock.Format(started),
	}, log.Items[1], "about no item, naming the run a click opens and when it started, as the run reads")
	assert.Empty(t, log.Items[0].Repo, "a folder that isn't a repository names none")
	assert.Equal(t, int64(8), log.Items[0].RunID)
	assert.Contains(t, events.All(), event.Event(event.NotificationsUpdated{}))
}

func TestTheRunsTellTheNotificationsOfAFailedRun(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t))

	// A run the app can't start isn't told: its start's error reaches the window.
	_, err := c.Runs.Start(t.Context(), scriptrun.Spec{
		Kind: scriptrun.KindScript, Title: "Lint", Repo: repo.Name{Owner: boundOwner, Repo: boundRepo},
		Dir: t.TempDir(), Argv: []string{filepath.Join(t.TempDir(), "missing")},
	})
	require.Error(t, err)

	// One that fails is, through the engine core built.
	path, env := testutil.FakeTool(t, "magex", testutil.FakeRun{Stderr: "lint failed\n", Exit: 2})

	id, err := c.Runs.Start(t.Context(), scriptrun.Spec{
		Kind: scriptrun.KindScript, Title: "Lint", Repo: repo.Name{Owner: boundOwner, Repo: boundRepo},
		Dir: t.TempDir(), Argv: []string{path, "lint"}, Env: []string{env},
	})
	require.NoError(t, err)

	// The run's follower tells of its end once it has recorded it and let go of the clone:
	// wait for what it tells, not for the record, which reads as ended a moment before.
	var log notify.Log

	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var listErr error

		log, listErr = c.Notifications.List(t.Context())
		require.NoError(collect, listErr)
		assert.NotEmpty(collect, log.Items)
	}, 15*time.Second, 10*time.Millisecond, "the failed run is told")

	run, err := c.ScriptRuns.Run(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, scriptrun.StatusFailed, run.Status)

	require.Len(t, log.Items, 1, "the run that couldn't start isn't told")
	assert.Equal(t, notify.KindRunFailed, log.Items[0].Kind)
	assert.Equal(t, id, log.Items[0].RunID)
	assert.Equal(t, run.StartedAt, log.Items[0].RunStarted, "the key the window checks the run against")
	assert.Equal(t, "A script failed: Lint", log.Items[0].Title)
	assert.Equal(t, "acme-org/example-repo", log.Items[0].Repo)
	assert.Equal(t, "It exited with status 2.", log.Items[0].Detail)
}

func TestReleaseGitHubAdaptsBothRounds(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "compare") {
			_, _ = io.WriteString(w, `{"data":{"r0":{"ref":{"compare":{"aheadBy":2,"commits":{"totalCount":2,
				"nodes":[{"oid":"c2","messageHeadline":"Fix retries","committedDate":"2026-11-10T04:00:00Z"}]}}}},
				"rateLimit":{"cost":1}}}`)

			return
		}

		_, _ = io.WriteString(w, `{"data":{"r0":{"isArchived":false,"viewerPermission":"WRITE",
			"defaultBranchRef":{"name":"master","target":{"oid":"c2","messageHeadline":"Fix retries",
			"committedDate":"2026-11-10T04:00:00Z","statusCheckRollup":{"state":"SUCCESS"}}},
			"refs":{"nodes":[{"name":"v1.4.2","target":{"__typename":"Tag","oid":"t1",
			"target":{"__typename":"Commit","oid":"c1"}}}]}},"rateLimit":{"cost":1}}}`)
	}))
	t.Cleanup(srv.Close)

	gh := releaseGitHub{
		tokens: github.NewTokenSource(&testutil.Runner{Stdout: "test-token-not-real\n"}, testutil.Finder{Path: "/opt/fake/bin/gh"}),
		client: &github.Client{BaseURL: srv.URL, HTTP: srv.Client()},
	}
	sdk := repo.Name{Owner: boundOwner, Repo: boundRepo}
	at := time.Date(2026, 11, 10, 4, 0, 0, 0, time.UTC)

	var answered []int

	status, err := gh.ReleaseStatus(t.Context(), []repo.Name{sdk}, func(n int) { answered = append(answered, n) })
	require.NoError(t, err)
	assert.Equal(t, map[string]release.RepoStatus{sdk.Key(): {
		Permission: "WRITE", DefaultBranch: "master", HeadOID: "c2", HeadAt: at, Headline: "Fix retries",
		Checks: release.ChecksSuccess,
		Tags:   []release.Tag{{Name: "v1.4.2", Commit: "c1"}},
	}}, status, "an annotated tag is peeled to its commit")

	compared, err := gh.ReleaseCompare(t.Context(), []release.CompareRequest{{Repo: sdk, Tag: "v1.4.2", Branch: "master"}},
		func(n int) { answered = append(answered, n) })
	require.NoError(t, err)
	assert.Equal(t, map[string]release.Compare{sdk.Key(): {
		Ahead: 2, Commits: []release.Commit{{OID: "c2", Headline: "Fix retries", At: at}},
	}}, compared)
	assert.Equal(t, []int{1, 1}, answered, "each round tells the page as GitHub answers")
}

func TestReleaseGitHubAsksNothingWhenSignedOut(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(srv.Close)

	gh := releaseGitHub{
		tokens: github.NewTokenSource(&testutil.Runner{Err: errSignedOut}, testutil.Finder{Path: "/opt/fake/bin/gh"}),
		client: &github.Client{BaseURL: srv.URL, HTTP: srv.Client()},
	}
	sdk := repo.Name{Owner: boundOwner, Repo: boundRepo}

	_, err := gh.ReleaseStatus(t.Context(), []repo.Name{sdk}, nil)
	require.ErrorIs(t, err, release.ErrSignedOut)

	_, err = gh.ReleaseCompare(t.Context(), []release.CompareRequest{{Repo: sdk, Tag: "v1.4.2", Branch: "master"}}, nil)
	require.ErrorIs(t, err, release.ErrSignedOut)

	_, err = gh.ReleaseWorkflow(t.Context(), sdk, "v1.4.3", strings.Repeat("1", 40))
	require.ErrorIs(t, err, release.ErrSignedOut)
	assert.Zero(t, requests.Load())
}

// workflowReleases are GitHub's answers about a followed release's tag, poll by poll: none
// yet, a draft, then the release published.
func workflowReleases() []string {
	return []string{
		`{"data": {"repository": {"release": null}, "rateLimit": {"cost": 1}}}`,
		`{"data": {"repository": {"release": {"url": "https://github.com/acme-org/example-repo/releases/tag/untagged-1",
			"isDraft": true, "publishedAt": null}}, "rateLimit": {"cost": 1}}}`,
		`{"data": {"repository": {"release": {"url": "https://github.com/acme-org/example-repo/releases/tag/v1.4.3",
			"isDraft": false, "publishedAt": "2026-11-11T15:30:00Z"}}, "rateLimit": {"cost": 1}}}`,
	}
}

// workflowRuns are GitHub's answers about the tag's runs, for the polls that ask: the
// release's run going, beside the branch's own failed run, then the release's run failed.
func workflowRuns(commit string) []string {
	run := func(branch, status, conclusion string, id int) string {
		return `{"name": "goreleaser", "head_branch": "` + branch + `", "head_sha": "` + commit + `", "event": "push",
			"status": "` + status + `", "conclusion": ` + conclusion + `,
			"html_url": "https://github.com/acme-org/example-repo/actions/runs/` + strconv.Itoa(id) + `"}`
	}

	return []string{
		`{"total_count": 2, "workflow_runs": [` + run("v1.4.3", "in_progress", "null", 2) + `, ` +
			run("master", "completed", `"failure"`, 1) + `]}`,
		`{"total_count": 1, "workflow_runs": [` + run("v1.4.3", "completed", `"failure"`, 2) + `]}`,
	}
}

// budgetHeaders writes the headers GitHub sends with every answer, for resource, its nth
// request this hour.
func budgetHeaders(w http.ResponseWriter, resource string, n int, resetAt int64) {
	w.Header().Set("X-Ratelimit-Resource", resource)
	w.Header().Set("X-Ratelimit-Limit", "5000")
	w.Header().Set("X-Ratelimit-Remaining", strconv.Itoa(5000-n))
	w.Header().Set("X-Ratelimit-Used", strconv.Itoa(n))
	w.Header().Set("X-Ratelimit-Reset", strconv.FormatInt(resetAt, 10))
}

func TestReleaseGitHubReadsTheWorkflowAndTheBudgetCountsEveryPoll(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t))

	commit := strings.Repeat("1", 40)
	releases, runs := workflowReleases(), workflowRuns(commit)
	resetAt := time.Now().Add(time.Hour).Unix()

	var queries, requests atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			body, _ := io.ReadAll(r.Body)
			assert.Contains(t, string(body), "query ReleaseProgress")

			n := int(queries.Add(1))
			budgetHeaders(w, github.ResourceGraphQL, n, resetAt)
			_, _ = io.WriteString(w, releases[n-1])

			return
		}

		assert.Equal(t, "/repos/acme-org/example-repo/actions/runs", r.URL.Path)
		assert.Equal(t, "v1.4.3", r.URL.Query().Get("branch"), "the tag's own runs")

		n := int(requests.Add(1))
		budgetHeaders(w, github.ResourceCore, n, resetAt)
		_, _ = io.WriteString(w, runs[n-1])
	}))
	t.Cleanup(srv.Close)

	// The app's one client, which tells the budget about every response, pointed at GitHub's
	// stand-in.
	c.client.BaseURL, c.client.HTTP = srv.URL, srv.Client()
	gh := releaseGitHub{
		tokens: github.NewTokenSource(&testutil.Runner{Stdout: "test-token-not-real\n"}, testutil.Finder{Path: "/opt/fake/bin/gh"}),
		client: c.client,
	}
	sdk := repo.Name{Owner: boundOwner, Repo: boundRepo}
	runURL := "https://github.com/acme-org/example-repo/actions/runs/2"

	got, err := gh.ReleaseWorkflow(t.Context(), sdk, "v1.4.3", commit)
	require.NoError(t, err)
	assert.Equal(t, release.Workflow{Runs: []release.TagRun{{Workflow: "goreleaser", URL: runURL}}}, got,
		"the tag's run goes on; the branch's failed run says nothing")

	got, err = gh.ReleaseWorkflow(t.Context(), sdk, "v1.4.3", commit)
	require.NoError(t, err)
	assert.Equal(t, release.Workflow{
		ReleaseURL: "https://github.com/acme-org/example-repo/releases/tag/untagged-1",
		Runs:       []release.TagRun{{Workflow: "goreleaser", URL: runURL, Done: true, Failed: true}},
	}, got, "a draft isn't published")

	got, err = gh.ReleaseWorkflow(t.Context(), sdk, "v1.4.3", commit)
	require.NoError(t, err)
	assert.Equal(t, release.Workflow{
		Published: true, ReleaseURL: "https://github.com/acme-org/example-repo/releases/tag/v1.4.3",
		Runs: []release.TagRun{},
	}, got)

	gauge := c.Budget.Gauge(t.Context())
	assert.Equal(t, 3, gauge.GraphQL.Spent, "the budget gauge counts every poll's point")
	assert.Equal(t, 3, gauge.GraphQL.Requests)
	assert.Equal(t, 2, gauge.Core.Spent, "and every runs request, none once it is published")
	assert.Equal(t, 2, gauge.Core.Requests)
}

func TestReleaseNoticesRecordTheReleaseWorkflow(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	opts := testOptions(t)
	opts.Events = events
	c := bootstrap(t, opts)

	notices := releaseNotices{dispatcher: notify.NewDispatcher(notify.Config{Pool: c.DB, Events: events})}
	sdk := repo.Name{Owner: boundOwner, Repo: boundRepo}

	notices.Published(t.Context(), release.Notice{
		Repo: sdk, Title: "acme-org/example-repo v1.4.3 is published",
		URL: "https://github.com/acme-org/example-repo/releases/tag/v1.4.3",
	})
	notices.WorkflowFailed(t.Context(), release.Notice{
		Repo: sdk, Title: "The release workflow failed for acme-org/example-repo v1.4.4",
		URL: "https://github.com/acme-org/example-repo/actions/runs/2",
	})

	log, err := c.Notifications.List(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, 2)

	failed, published := log.Items[0], log.Items[1]
	assert.Equal(t, notify.KindReleasePublished, published.Kind)
	assert.Equal(t, "acme-org/example-repo v1.4.3 is published", published.Title)
	assert.Equal(t, "https://github.com/acme-org/example-repo/releases/tag/v1.4.3", published.URL, "it opens the release")
	assert.Empty(t, published.ItemKind, "about no item")

	assert.Equal(t, notify.KindReleaseFailed, failed.Kind)
	assert.Equal(t, "https://github.com/acme-org/example-repo/actions/runs/2", failed.URL, "it opens the run")
	assert.Equal(t, "acme-org/example-repo", failed.Repo)
}

func TestReleaseClonesListTheCloneRoot(t *testing.T) {
	t.Parallel()

	home := testutil.ClaudeHome(t)
	root := filepath.Join(home, "projects")
	env := testutil.Git(t)

	for _, c := range []struct{ dir, origin string }{
		{"sdk", "https://github.com/acme-org/example-repo.git"},
		{"notes", ""},
	} {
		dir := filepath.Join(root, c.dir)
		testutil.InitRepo(t, dir)

		if c.origin != "" {
			testutil.RunGit(t, dir, "remote", "add", "origin", c.origin)
		}
	}

	opts := testOptions(t)
	opts.Home = home
	c := bootstrap(t, opts)
	require.NoError(t, testutil.ErrOf(c.Settings.SetCloneRoot(t.Context(), root)))

	clones := releaseClones{settings: c.Settings, home: home, git: execpath.Exec{Env: env}}

	gotRoot, err := clones.Root(t.Context())
	require.NoError(t, err)
	assert.Equal(t, root, gotRoot)

	got, err := clones.Clones(t.Context(), gotRoot)
	require.NoError(t, err)
	assert.Equal(t, []release.Clone{
		{Dir: filepath.Join(root, "notes")},
		{Dir: filepath.Join(root, "sdk"), Origin: repo.Name{Owner: boundOwner, Repo: boundRepo}, OnHub: true},
	}, got)
}

func TestHooksClonesListTheCloneRootWithoutGit(t *testing.T) {
	t.Parallel()

	home := testutil.ClaudeHome(t)
	root := filepath.Join(home, "projects")
	env := testutil.Git(t)

	sdk, notes := filepath.Join(root, "sdk"), filepath.Join(root, "notes")
	testutil.InitRepo(t, sdk)
	testutil.InitRepo(t, notes)
	testutil.RunGit(t, sdk, "remote", "add", "origin", "https://github.com/acme-org/example-repo.git")

	opts := testOptions(t)
	opts.Home = home
	c := bootstrap(t, opts)
	require.NoError(t, testutil.ErrOf(c.Settings.SetCloneRoot(t.Context(), root)))

	clones := hooksClones{settings: c.Settings, home: home, git: execpath.Exec{Env: env}}

	gotRoot, err := clones.Root(t.Context())
	require.NoError(t, err)
	assert.Equal(t, root, gotRoot)

	dirs, err := clones.Dirs(t.Context(), gotRoot)
	require.NoError(t, err)
	assert.Equal(t, []string{notes, sdk}, dirs)

	name, onHub, err := clones.Origin(t.Context(), sdk)
	require.NoError(t, err)
	assert.True(t, onHub)
	assert.Equal(t, repo.Name{Owner: boundOwner, Repo: boundRepo}, name)

	_, onHub, err = clones.Origin(t.Context(), notes)
	require.NoError(t, err)
	assert.False(t, onHub)

	_, err = clones.Dirs(t.Context(), filepath.Join(root, "missing"))
	require.Error(t, err)
}

func TestHooksToolAndSettingsReadWhatTheAppKnows(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t))

	facts := hooksTool{tools: c.Integrations}.HooksTool(t.Context())
	assert.False(t, facts.Usable(), "no go-pre-commit is found")
	assert.Empty(t, facts.Copies)

	place := integrationSettings{settings: c.Settings}

	got, err := place.RunsIn(t.Context())
	require.NoError(t, err)
	assert.Equal(t, integrationprefs.RunsInApp, got)

	_, err = c.Settings.SetRunsIn(t.Context(), integrationprefs.RunsInTerminal)
	require.NoError(t, err)

	got, err = place.RunsIn(t.Context())
	require.NoError(t, err)
	assert.Equal(t, integrationprefs.RunsInTerminal, got)

	require.NoError(t, c.Close())

	_, err = place.RunsIn(t.Context())
	require.Error(t, err, "a closed database can't say")
	assert.False(t, hooksTool{tools: c.Integrations}.HooksTool(t.Context()).Usable())
}

func TestScriptsToolsReadWhatTheAppKnows(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t))

	got := scriptsTools{tools: c.Integrations}.Tools(t.Context())
	assert.False(t, got.Magex.Usable(), "no magex is found")
	assert.False(t, got.MageXOn)
	assert.False(t, got.GoPreCommitOn)

	assert.Equal(t, integrations.Tool{}, toolByID(integrations.Tools{}, integrationprefs.MageX), "a tool the list lacks")

	cfg, err := integrationSettings{settings: c.Settings}.Integrations(t.Context())
	require.NoError(t, err)
	assert.Equal(t, integrationprefs.ScriptSources{MagexTargets: true, RepoFiles: true}, cfg.ScriptSources)
}
