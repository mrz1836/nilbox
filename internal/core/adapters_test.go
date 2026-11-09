package core

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/branchrules"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/worktrees"
)

func TestSettingsViewsReadTheSettingsAsTheyAreNow(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t, ".claude"),
		Runner: noPrograms(),
	})
	ctx := t.Context()
	launch := settingsView[launcher.LaunchSettings]{settings: c.Settings, part: launchSettings}
	sweep := settingsView[worktrees.SweepSettings]{settings: c.Settings, part: sweepSettings}

	all, err := c.Settings.Get(ctx)
	require.NoError(t, err)

	got, err := launch.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, launcher.LaunchSettings{
		DefaultAgent: agent.Claude, DefaultProfiles: all.DefaultProfiles, WorktreeDefault: true, Terminal: terminal.ITerm2,
		CloneRoot: settings.DefaultCloneRoot, TrustedBots: all.TrustedBots,
	}, got)

	swept, err := sweep.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, worktrees.SweepSettings{
		CloneRoot: settings.DefaultCloneRoot, AutoCleanup: true, Branches: branchrules.Defaults(),
	}, swept)

	// A change the user makes reaches the next read, with no restart.
	root := t.TempDir()
	require.NoError(t, testutil.ErrOf(c.Settings.SetWorktreeDefault(ctx, false)))
	require.NoError(t, testutil.ErrOf(c.Settings.SetWorktreeAutoCleanup(ctx, false)))
	require.NoError(t, testutil.ErrOf(c.Settings.SetTerminal(ctx, settings.TerminalApp)))
	require.NoError(t, testutil.ErrOf(c.Settings.SetCloneRoot(ctx, root)))
	require.NoError(t, testutil.ErrOf(c.Settings.SetTrustedBots(ctx, []string{"renovate"})))
	require.NoError(t, testutil.ErrOf(c.Settings.SetBranchRules(ctx, branchrules.Rules{Own: true, Protected: []string{"develop"}})))

	got, err = launch.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, launcher.LaunchSettings{
		DefaultAgent: agent.Claude, DefaultProfiles: all.DefaultProfiles, Terminal: terminal.TerminalApp, CloneRoot: root,
		TrustedBots: []string{"renovate"},
	}, got)

	swept, err = sweep.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, worktrees.SweepSettings{
		CloneRoot: root, Branches: branchrules.Rules{Own: true, Protected: []string{"develop"}},
	}, swept)
}

func TestSettingsViewReportsUnreadableSettings(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	prefs := settings.NewService(db, nil, t.TempDir())
	require.NoError(t, db.Close())

	got, err := settingsView[launcher.LaunchSettings]{settings: prefs, part: launchSettings}.Get(t.Context())
	require.Error(t, err)
	assert.Zero(t, got)
}

func TestLaunchItemsHandTheLauncherTheStoredItems(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	pr := github.Item{
		IsPR: true, Number: 7, Title: "Fix things", URL: "https://github.com/acme-org/example-repo/pull/7",
		CreatedAt: at, UpdatedAt: at, HeadRef: "feature-branch", AuthorLogin: "octo-user", AuthorAssociation: "MEMBER",
		RepoOwner: "acme-org", RepoName: "example-repo",
	}

	ids, err := inbox.StoreFixture(t.Context(), db, inbox.Fixture{
		Viewer: "octo-user", At: at, Items: []inbox.FixtureItem{{Item: pr, Authored: true}},
	})
	require.NoError(t, err)

	store := inbox.NewStore(db, nil)

	want, err := store.LaunchItems(t.Context(), ids)
	require.NoError(t, err)
	require.Len(t, want, 1)

	got, err := launchItems{Store: store}.LaunchItems(t.Context(), append(ids, 999))
	require.NoError(t, err)
	require.Len(t, got, 1, "an id with no stored item is left out")
	assert.Equal(t, want[0].ID, got[0].ID)
	assert.Equal(t, "feature-branch", got[0].HeadRef)
	assert.Equal(t, "example-repo", got[0].Name)
	assert.Equal(t, launcher.StoredItem(want[0]), got[0])

	require.NoError(t, db.Close())

	_, err = launchItems{Store: store}.LaunchItems(t.Context(), ids)
	require.Error(t, err)
}

func TestWorktreeStateReadsGitHubsWords(t *testing.T) {
	t.Parallel()

	closed := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	for state, want := range map[string]worktrees.ItemStatus{
		github.StateMerged: worktrees.ItemMerged,
		github.StateClosed: worktrees.ItemClosed,
		github.StateOpen:   worktrees.ItemOpen,
		"":                 worktrees.ItemOpen,
	} {
		got := worktreeState(github.ItemState{
			PullRequest: true, Title: "Fix things", URL: "https://github.com/acme-org/example-repo/pull/1",
			State: state, ClosedAt: closed, HeadOID: "abc123",
		})

		assert.Equal(t, worktrees.ItemState{
			PullRequest: true, Title: "Fix things", URL: "https://github.com/acme-org/example-repo/pull/1",
			State: want, ClosedAt: closed, HeadOID: "abc123",
		}, got, state)
	}
}

func TestItemStatesBorrowTheTokenAgainWhenGitHubRefusesIt(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"message": "Bad credentials"}`)

			return
		}

		_, _ = io.WriteString(w, `{"data": {}}`)
	}))
	t.Cleanup(srv.Close)

	runner := &testutil.Runner{Stdout: "test-token-not-real\n"}
	states := itemStates{
		tokens: github.NewTokenSource(runner, testutil.Finder{Path: "/opt/fake/bin/gh"}),
		client: &github.Client{BaseURL: srv.URL, HTTP: srv.Client()},
	}

	got, err := states.ItemStates(t.Context(), []item.Ref{{Repo: repo.Name{Owner: "acme-org", Repo: "example-repo"}, Number: 1}})
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, int32(2), requests.Load())
	assert.Len(t, runner.Calls(), 2, "the refused token was borrowed again")
}

func TestCloseForgetsTheToken(t *testing.T) {
	t.Parallel()

	runner := &testutil.Runner{Stdout: "test-token-not-real\n"}

	c, err := Bootstrap(t.Context(), Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t),
		Runner: runner,
	})
	require.NoError(t, err)

	for range 2 {
		_, err = c.tokens.Token(t.Context())
		require.NoError(t, err)
	}

	assert.Len(t, runner.Calls(), 1, "the token is kept while the app runs")

	require.NoError(t, c.Close())

	_, err = c.tokens.Token(t.Context())
	require.NoError(t, err)
	assert.Len(t, runner.Calls(), 2, "the token was forgotten when the app closed")
}

func TestAgentRunnerPutsTheCommandsDirectoryFirstOnThePath(t *testing.T) {
	t.Parallel()

	// An npm-installed agent's command is a script that finds node beside it.
	dir := t.TempDir()
	command := filepath.Join(dir, "codex")
	require.NoError(t, os.WriteFile(command, []byte("#!/bin/sh\nprintf '%s' \"$PATH\"\n"), 0o700)) //nolint:gosec // a command must be executable

	stdout, _, err := agentRunner{search: execpath.Search{}}.Run(t.Context(), command, "--version")
	require.NoError(t, err)

	first, _, _ := strings.Cut(string(stdout), string(os.PathListSeparator))
	assert.Equal(t, dir, first)
	assert.Contains(t, string(stdout), os.Getenv("PATH"), "the rest of the search path follows")
}

func TestMergedPullRequestsKeyEachAnswerByRepositoryAndBranch(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data": {"r0": {
			"b0": {"nodes": [{"number": 184, "title": "Add locktimes", "url": "https://github.com/acme-org/Example-Repo/pull/184",
				"mergedAt": "2026-11-06T18:00:05Z", "headRefOid": "fc1c3975011e66491785ace891c893e4fc716587"}]},
			"b1": {"nodes": []}}}}`)
	}))
	t.Cleanup(srv.Close)

	states := itemStates{
		tokens: github.NewTokenSource(&testutil.Runner{Stdout: "test-token-not-real\n"}, testutil.Finder{Path: "/opt/fake/bin/gh"}),
		client: &github.Client{BaseURL: srv.URL, HTTP: srv.Client()},
	}
	example := repo.Name{Owner: boundOwner, Repo: "Example-Repo"}

	got, err := states.MergedPullRequests(t.Context(), []worktrees.BranchQuery{
		{Repo: example, Branches: []string{"feature-x", "old-idea"}},
	})
	require.NoError(t, err)
	assert.Equal(t, map[worktrees.BranchKey][]worktrees.MergedPR{
		{Repo: "acme-org/example-repo", Branch: "feature-x"}: {{
			Number: 184, Title: "Add locktimes", URL: "https://github.com/acme-org/Example-Repo/pull/184",
			MergedAt: time.Date(2026, 11, 6, 18, 0, 5, 0, time.UTC), HeadOID: "fc1c3975011e66491785ace891c893e4fc716587",
		}},
	}, got, "keyed by the repository's key, whatever its case; a branch none had is left out")
}
