package core

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/release"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
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

	prefs := settings.NewService(testutil.ClosedDB(t), nil, t.TempDir())

	got, err := settingsView[launcher.LaunchSettings]{settings: prefs, part: launchSettings}.Get(t.Context())
	require.Error(t, err)
	assert.Zero(t, got)
}

func TestSettingsAdaptersReadTheirPartOfTheSettings(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := testutil.DB(t)
	prefs := settings.NewService(db, nil, t.TempDir())
	places, review := releaseSettings{settings: prefs}, reviewSettings{settings: prefs}

	where, err := places.Where(ctx)
	require.NoError(t, err)
	assert.Equal(t, release.Where{RunsIn: integrationprefs.RunsInApp, Terminal: terminal.Name(terminal.ITerm2)}, where)

	files, err := review.Sensitive(ctx)
	require.NoError(t, err)
	assert.Equal(t, sensitive.DefaultConfig(), files)

	require.NoError(t, testutil.ErrOf(prefs.SetRunsIn(ctx, integrationprefs.RunsInTerminal)))
	require.NoError(t, testutil.ErrOf(prefs.SetTerminal(ctx, settings.TerminalApp)))
	require.NoError(t, testutil.ErrOf(prefs.SetSensitiveScope(ctx, sensitive.ScopeOutside)))

	where, err = places.Where(ctx)
	require.NoError(t, err)
	assert.Equal(t, release.Where{
		RunsIn: integrationprefs.RunsInTerminal, Terminal: terminal.Name(terminal.TerminalApp),
	}, where, "a change reaches the next read")

	files, err = review.Sensitive(ctx)
	require.NoError(t, err)
	assert.Equal(t, sensitive.ScopeOutside, files.Scope)

	require.NoError(t, db.Close())

	where, err = places.Where(ctx)
	require.Error(t, err)
	assert.Zero(t, where, "settings that can't be read give no part")

	files, err = review.Sensitive(ctx)
	require.Error(t, err)
	assert.Zero(t, files)

	offer, err := places.OfferAfterMerge(ctx)
	require.Error(t, err)
	assert.False(t, offer)

	cfg, err := integrationSettings{settings: prefs}.Integrations(ctx)
	require.Error(t, err)
	assert.Zero(t, cfg)

	runsIn, err := integrationSettings{settings: prefs}.RunsIn(ctx)
	require.Error(t, err)
	assert.Zero(t, runsIn)
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

func TestAgentRunnerReplacesThePathOfTheEnvironmentItIsGiven(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	command := filepath.Join(dir, "claude")
	require.NoError(t, os.WriteFile(command, []byte("#!/bin/sh\nprintf '%s|%s' \"$KEPT\" \"$PATH\"\n"), 0o700)) //nolint:gosec // a command must be executable

	env := []string{"PATH=/first", "KEPT=yes", "PATH=/second"}
	runner := agentRunner{search: execpath.Search{}, env: env}

	stdout, _, err := runner.Run(t.Context(), command, "--version")
	require.NoError(t, err)

	assert.Equal(t, "yes|"+dir+string(os.PathListSeparator)+os.Getenv("PATH"), string(stdout),
		"every PATH it had gives way to the command's directory, then this process's search path")
	assert.Equal(t, []string{"PATH=/first", "KEPT=yes", "PATH=/second"}, env, "the runner's own environment is left alone")
}

func TestWorktreeRunnerTurnsGitsTerminalPromptsOff(t *testing.T) {
	t.Setenv("GIT_TERMINAL_PROMPT", "1")

	search := execpath.Search{Path: "/opt/fake/bin"}
	runner := worktreeRunner(search)

	assert.Equal(t, search, runner.Search)
	require.NotEmpty(t, runner.Env)
	assert.Equal(t, "GIT_TERMINAL_PROMPT=0", runner.Env[len(runner.Env)-1], "set last")

	var prompts []string

	for _, kv := range runner.Env {
		if strings.HasPrefix(kv, "GIT_TERMINAL_PROMPT=") {
			prompts = append(prompts, kv)
		}
	}

	assert.Equal(t, []string{"GIT_TERMINAL_PROMPT=0"}, prompts, "in place of the process's own")
	assert.Len(t, runner.Env, len(os.Environ()), "the rest of the process's environment, as it is")
	assert.Contains(t, runner.Env, "PATH="+os.Getenv("PATH"))
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

func TestTerminalRunsOpenAFollowedScriptInTheTerminal(t *testing.T) {
	t.Parallel()

	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))
	runner := &testutil.Runner{}
	c := bootstrap(t, Options{Paths: paths, Home: testutil.ClaudeHome(t), Runner: runner})
	runs := terminalRuns{launch: paths.Launch, opener: launcher.NewCommandOpener(c.Launcher)}
	logs := filepath.Join(paths.Root, "script-runs")

	err := runs.OpenRun(t.Context(), scriptrun.TerminalRun{
		Name: "run-7", Dir: "/work/example-repo", Argv: []string{"/usr/local/bin/magex", "lint"},
		Env: []string{"MAGEX_NO_UPDATE_CHECK=1"},
		Log: filepath.Join(logs, "7.log"), PIDFile: filepath.Join(logs, "7.pid"), ExitFile: filepath.Join(logs, "7.exit"),
	})

	scripts, globErr := filepath.Glob(filepath.Join(paths.Launch, "run-7-*"))
	require.NoError(t, globErr)
	require.Len(t, scripts, 1, "the run's script is written into the launch folder")

	data, err2 := os.ReadFile(scripts[0])
	require.NoError(t, err2)

	body := string(data)
	assert.Contains(t, body, "export MAGEX_NO_UPDATE_CHECK='1'\n")
	assert.Contains(t, body, "-q -F '"+filepath.Join(logs, "7.log")+"' '/usr/local/bin/magex' 'lint'\n",
		"the command runs recorded into the run's log")

	if runtime.GOOS != "darwin" {
		require.ErrorIs(t, err, terminal.ErrUnsupportedOS)

		return
	}

	require.NoError(t, err)

	calls := runner.Calls()
	require.NotEmpty(t, calls)
	assert.Contains(t, strings.Join(calls[len(calls)-1], " "), scripts[0], "the terminal chosen for launches opens it")

	require.ErrorIs(t, runs.OpenRun(t.Context(), scriptrun.TerminalRun{Name: "run-8"}), errNoCommand)
}

func TestTerminalRunsOpenATerminalInAFolder(t *testing.T) {
	t.Parallel()

	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))
	runner := &testutil.Runner{}
	c := bootstrap(t, Options{Paths: paths, Home: testutil.ClaudeHome(t), Runner: runner})
	runs := terminalRuns{
		launch: paths.Launch, opener: launcher.NewCommandOpener(c.Launcher), shell: func() string { return "/opt/shells/fish" },
	}

	err := runs.OpenFolder(t.Context(), "run-9", "/work/example-repo")

	scripts, globErr := filepath.Glob(filepath.Join(paths.Launch, "run-9-*"))
	require.NoError(t, globErr)
	require.Len(t, scripts, 1)

	data, readErr := os.ReadFile(scripts[0])
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "cd -- '/work/example-repo' || exit 1\nexec '/opt/shells/fish' '-l'\n",
		"the user's login shell, in the folder, and nothing else")

	if runtime.GOOS != "darwin" {
		require.ErrorIs(t, err, terminal.ErrUnsupportedOS)

		return
	}

	require.NoError(t, err)
}

func TestFinderRevealShowsTheFile(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "darwin" {
		assert.Nil(t, revealer(&testutil.Runner{}), "no Finder: Show the log file says so")

		return
	}

	runner := &testutil.Runner{}
	require.NoError(t, revealer(runner).Reveal(t.Context(), "/data/script-runs/7.log"))
	assert.Equal(t, [][]string{{"/usr/bin/open", "-R", "/data/script-runs/7.log"}}, runner.Calls())

	failing := &testutil.Runner{Stderr: "The file does not exist.\nmore", Err: errNoPrograms}
	err := finderReveal{runner: failing}.Reveal(t.Context(), "/gone")
	require.ErrorIs(t, err, errNoPrograms)
	assert.Equal(t, "Finder couldn't show the file: The file does not exist.", uierr.Message(err))

	err = finderReveal{runner: &testutil.Runner{Err: errNoPrograms}}.Reveal(t.Context(), "/gone")
	assert.Equal(t, "Finder couldn't show the file: "+errNoPrograms.Error(), uierr.Message(err))
}
