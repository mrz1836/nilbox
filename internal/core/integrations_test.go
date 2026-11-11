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

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/release"
	"github.com/mrz1836/nilbox/internal/repo"
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
	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t),
		Runner: noPrograms(),
		Events: events,
	})

	before := len(events.All())

	require.NoError(t, c.ScriptRuns.ClearHistory(t.Context()))
	assert.Equal(t, []event.Event{event.ScriptRunsUpdated{}, event.ToolsUpdated{}}, events.All()[before:],
		"the window lists the runs again, and the tools, whose updates a run can block")
}

func TestReleaseSettingsReadWhetherAMergeOffersARelease(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t),
		Runner: noPrograms(),
	})
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
	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t),
		Runner: noPrograms(),
		Events: events,
	})

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

// workflowAnswers are GitHub's answers to a followed release's polls, in turn: its
// workflow still going, then failed, then the release published.
func workflowAnswers() []string {
	suites := func(conclusion string) string {
		return `{"checkSuites": {"nodes": [
			{"app": {"slug": "codecov", "name": "Codecov"}, "status": "QUEUED", "conclusion": null, "branch": null,
				"workflowRun": null},
			{"app": {"slug": "github-actions", "name": "GitHub Actions"}, "status": "COMPLETED", "conclusion": "FAILURE",
				"branch": {"name": "master"}, "workflowRun": {"url": "https://github.com/acme-org/example-repo/actions/runs/1",
				"event": "push", "workflow": {"name": "CI"}}},
			{"app": {"slug": "github-actions", "name": "GitHub Actions"}, "status": ` + conclusion + `,
				"branch": null, "workflowRun": {"url": "https://github.com/acme-org/example-repo/actions/runs/2",
				"event": "push", "workflow": {"name": "CI"}}}
		]}}`
	}

	return []string{
		`{"data": {"repository": {"release": null, "object": ` + suites(`"IN_PROGRESS", "conclusion": null`) +
			`}, "rateLimit": {"cost": 1}}}`,
		`{"data": {"repository": {"release": null, "object": ` + suites(`"COMPLETED", "conclusion": "FAILURE"`) +
			`}, "rateLimit": {"cost": 1}}}`,
		`{"data": {"repository": {"release": {"url": "https://github.com/acme-org/example-repo/releases/tag/v1.4.3",
			"isDraft": false, "publishedAt": "2026-11-11T15:30:00Z"}, "object": ` +
			suites(`"COMPLETED", "conclusion": "SUCCESS"`) + `}, "rateLimit": {"cost": 1}}}`,
	}
}

func TestReleaseGitHubReadsTheWorkflowAndTheBudgetCountsEveryPoll(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, Options{
		Paths: appdata.NewPaths(filepath.Join(t.TempDir(), "data")), Home: testutil.ClaudeHome(t), Runner: noPrograms(),
	})

	answers := workflowAnswers()
	resetAt := time.Now().Add(time.Hour).Unix()

	var polls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "query ReleaseProgress")

		n := int(polls.Add(1))

		w.Header().Set("X-Ratelimit-Resource", github.ResourceGraphQL)
		w.Header().Set("X-Ratelimit-Limit", "5000")
		w.Header().Set("X-Ratelimit-Remaining", strconv.Itoa(5000-n))
		w.Header().Set("X-Ratelimit-Used", strconv.Itoa(n))
		w.Header().Set("X-Ratelimit-Reset", strconv.FormatInt(resetAt, 10))
		_, _ = io.WriteString(w, answers[n-1])
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
	commit := strings.Repeat("1", 40)

	got, err := gh.ReleaseWorkflow(t.Context(), sdk, "v1.4.3", commit)
	require.NoError(t, err)
	assert.Equal(t, release.Workflow{}, got, "the tag's run goes on; codecov's queued suite and the branch's failed run say nothing")

	got, err = gh.ReleaseWorkflow(t.Context(), sdk, "v1.4.3", commit)
	require.NoError(t, err)
	assert.Equal(t, release.Workflow{Failed: true, RunURL: "https://github.com/acme-org/example-repo/actions/runs/2"}, got)

	got, err = gh.ReleaseWorkflow(t.Context(), sdk, "v1.4.3", commit)
	require.NoError(t, err)
	assert.Equal(t, release.Workflow{
		Published: true, ReleaseURL: "https://github.com/acme-org/example-repo/releases/tag/v1.4.3",
	}, got)

	gauge := c.Budget.Gauge()
	assert.Equal(t, 3, gauge.GraphQL.Spent, "the budget gauge counts every poll's point")
	assert.Equal(t, 3, gauge.GraphQL.Requests)
}

func TestReleaseNoticesRecordTheReleaseWorkflow(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t),
		Runner: noPrograms(),
		Events: events,
	})

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

	c := bootstrap(t, Options{
		Paths: appdata.NewPaths(filepath.Join(t.TempDir(), "data")), Home: home, Runner: noPrograms(),
	})
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

	c := bootstrap(t, Options{
		Paths: appdata.NewPaths(filepath.Join(t.TempDir(), "data")), Home: home, Runner: noPrograms(),
	})
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

	c := bootstrap(t, Options{
		Paths: appdata.NewPaths(filepath.Join(t.TempDir(), "data")), Home: testutil.ClaudeHome(t), Runner: noPrograms(),
	})

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

	c := bootstrap(t, Options{
		Paths: appdata.NewPaths(filepath.Join(t.TempDir(), "data")), Home: testutil.ClaudeHome(t), Runner: noPrograms(),
	})

	got := scriptsTools{tools: c.Integrations}.Tools(t.Context())
	assert.False(t, got.Magex.Usable(), "no magex is found")
	assert.False(t, got.MageXOn)
	assert.False(t, got.GoPreCommitOn)

	assert.Equal(t, integrations.Tool{}, toolByID(integrations.Tools{}, integrationprefs.MageX), "a tool the list lacks")

	cfg, err := integrationSettings{settings: c.Settings}.Integrations(t.Context())
	require.NoError(t, err)
	assert.Equal(t, integrationprefs.ScriptSources{MagexTargets: true, RepoFiles: true}, cfg.ScriptSources)
}
