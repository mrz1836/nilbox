package core

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

	status, err := gh.ReleaseStatus(t.Context(), []repo.Name{sdk})
	require.NoError(t, err)
	assert.Equal(t, map[string]release.RepoStatus{sdk.Key(): {
		Permission: "WRITE", DefaultBranch: "master", HeadOID: "c2", HeadAt: at, Headline: "Fix retries",
		Checks: release.ChecksSuccess,
		Tags:   []release.Tag{{Name: "v1.4.2", Commit: "c1"}},
	}}, status, "an annotated tag is peeled to its commit")

	compared, err := gh.ReleaseCompare(t.Context(), []release.CompareRequest{{Repo: sdk, Tag: "v1.4.2", Branch: "master"}})
	require.NoError(t, err)
	assert.Equal(t, map[string]release.Compare{sdk.Key(): {
		Ahead: 2, Commits: []release.Commit{{OID: "c2", Headline: "Fix retries", At: at}},
	}}, compared)
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

	_, err := gh.ReleaseStatus(t.Context(), []repo.Name{sdk})
	require.ErrorIs(t, err, release.ErrSignedOut)

	_, err = gh.ReleaseCompare(t.Context(), []release.CompareRequest{{Repo: sdk, Tag: "v1.4.2", Branch: "master"}})
	require.ErrorIs(t, err, release.ErrSignedOut)
	assert.Zero(t, requests.Load())
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

	place := hooksSettings{settings: c.Settings}

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
