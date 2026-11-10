package core

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/integrations"
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
