package core

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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
	"github.com/mrz1836/nilbox/internal/github/githubtest"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/localrepos"
	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/release"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/worktrees"
)

func TestSettingsViewsReadTheSettingsAsTheyAreNow(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t, ".claude"))
	ctx := t.Context()
	launch := settingsView[launcher.LaunchSettings]{settings: c.Settings, part: launchSettings}
	sweep := settingsView[worktrees.SweepSettings]{settings: c.Settings, part: sweepSettings}

	all, err := c.Settings.Get(ctx)
	require.NoError(t, err)

	got, err := launch.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, launcher.LaunchSettings{
		DefaultAgent: agent.Claude, DefaultProfiles: all.DefaultProfiles, WorktreeDefault: true, Terminal: terminal.ITerm2,
		CloneRoot: settings.DefaultCloneRoot, CloneLocations: []localrepos.Location{}, TrustedBots: all.TrustedBots,
		APIKeys: map[agent.ID]bool{agent.Claude: false, agent.Codex: false},
	}, got)

	swept, err := sweep.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, worktrees.SweepSettings{
		CloneRoot: settings.DefaultCloneRoot, CloneLocations: []localrepos.Location{}, AutoCleanup: true,
		Branches: branchrules.Defaults(),
	}, swept)

	// A change the user makes reaches the next read, with no restart.
	root := t.TempDir()
	work := t.TempDir()
	require.NoError(t, testutil.ErrOf(c.Settings.AddCloneLocations(ctx, []string{work})))

	locations := []localrepos.Location{{Path: work, Kind: localrepos.KindFolder}}

	require.NoError(t, testutil.ErrOf(c.Settings.SetWorktreeDefault(ctx, false)))
	require.NoError(t, testutil.ErrOf(c.Settings.SetWorktreeAutoCleanup(ctx, false)))
	require.NoError(t, testutil.ErrOf(c.Settings.SetTerminal(ctx, settings.TerminalApp)))
	require.NoError(t, testutil.ErrOf(c.Settings.SetCloneRoot(ctx, root)))
	require.NoError(t, testutil.ErrOf(c.Settings.SetTrustedBots(ctx, []string{"renovate"})))
	require.NoError(t, testutil.ErrOf(c.Settings.SetBranchRules(ctx, branchrules.Rules{Own: true, Protected: []string{"develop"}})))
	require.NoError(t, testutil.ErrOf(c.Settings.SetCodexAPIKey(ctx, true)))

	got, err = launch.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, launcher.LaunchSettings{
		DefaultAgent: agent.Claude, DefaultProfiles: all.DefaultProfiles, Terminal: terminal.TerminalApp, CloneRoot: root,
		CloneLocations: locations, TrustedBots: []string{"renovate"},
		APIKeys: map[agent.ID]bool{agent.Claude: false, agent.Codex: true},
	}, got)

	swept, err = sweep.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, worktrees.SweepSettings{
		CloneRoot: root, CloneLocations: locations, Branches: branchrules.Rules{Own: true, Protected: []string{"develop"}},
	}, swept)
}

func TestTheReleasePageReadsTheRepoRulesAsSettingsStoreThem(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t))
	ctx := t.Context()
	rules := releaseRules{prefs: prefsstore.New(c.DB, nil)}

	got, err := rules.RepoRules(ctx)
	require.NoError(t, err)
	assert.Equal(t, reporules.Rules{Pinned: []string{}, Muted: []string{}}, got)

	require.NoError(t, testutil.ErrOf(c.Settings.SetRepoRule(ctx, boundOwner, reporules.RuleMuted)))
	require.NoError(t, testutil.ErrOf(c.Settings.SetRepoRule(ctx, boundOwner+"/"+boundRepo, reporules.RulePinned)))

	got, err = rules.RepoRules(ctx)
	require.NoError(t, err)
	assert.Equal(t, reporules.Rules{Pinned: []string{boundOwner + "/" + boundRepo}, Muted: []string{boundOwner}}, got)

	_, err = releaseRules{prefs: prefsstore.New(testutil.ClosedDB(t), nil)}.RepoRules(ctx)
	require.Error(t, err)
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

	opts := testOptions(t)
	opts.Runner = runner
	c, err := Bootstrap(t.Context(), opts)
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

// TestAgentRunnerPutsTheCommandsDirectoryFirstOnThePath runs a runner with no environment
// of its own, which passes on this process's, so the fake's entry goes there with
// t.Setenv and the test runs serially.
func TestAgentRunnerPutsTheCommandsDirectoryFirstOnThePath(t *testing.T) {
	// An npm-installed agent's command is a script that finds node beside it.
	command, entry := testutil.FakeTool(t, "codex", testutil.FakeRun{ReportEnv: []string{"PATH"}})
	key, value, _ := strings.Cut(entry, "=")
	t.Setenv(key, value)

	stdout, _, err := agentRunner{search: execpath.Search{}}.Run(t.Context(), command, "--version")
	require.NoError(t, err)

	path, found := strings.CutPrefix(strings.TrimSuffix(string(stdout), "\n"), "PATH=")
	require.True(t, found, "the fake reports its PATH: %q", stdout)

	first, _, _ := strings.Cut(path, string(os.PathListSeparator))
	assert.Equal(t, filepath.Dir(command), first)
	assert.Contains(t, path, os.Getenv("PATH"), "the rest of the search path follows")
}

func TestAgentRunnerReplacesThePathOfTheEnvironmentItIsGiven(t *testing.T) {
	t.Parallel()

	command, entry := testutil.FakeTool(t, "claude", testutil.FakeRun{ReportEnv: []string{"KEPT", "PATH"}})

	env := []string{"PATH=/first", "KEPT=yes", "PATH=/second", entry}
	runner := agentRunner{search: execpath.Search{}, env: env}

	stdout, _, err := runner.Run(t.Context(), command, "--version")
	require.NoError(t, err)

	// A program reads the first of a variable's entries, so any PATH left would win.
	assert.Equal(t, "KEPT=yes\nPATH="+filepath.Dir(command)+string(os.PathListSeparator)+os.Getenv("PATH")+"\n",
		string(stdout), "every PATH it had gives way to the command's directory, then this process's search path")
	assert.Equal(t, []string{"PATH=/first", "KEPT=yes", "PATH=/second", entry}, env,
		"the runner's own environment is left alone")
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

func TestTheTerminalRunnerStartsATerminalWithoutAnyAgentSessionsVariables(t *testing.T) {
	t.Parallel()

	search := execpath.Search{Path: "/opt/fake/bin"}
	environ := []string{
		"PATH=/opt/tools/bin:/usr/bin", "HOME=/srv/home", "CLAUDECODE=1", "CLAUDE_CODE_CHILD_SESSION=1",
		"CLAUDE_CODE_SESSION_ID=0b8f", "CLAUDE_PID=4100", "CLAUDE_CODE_MAX_OUTPUT_TOKENS=32000",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "CLAUDE_CONFIG_DIR=/srv/home/.claude_work",
		"CODEX_SANDBOX=seatbelt", "CODEX_SANDBOX_NETWORK_DISABLED=1", "CODEX_THREAD_ID=019a", "CODEX_HOME=/srv/home/.codex",
		"LANG=C",
	}
	given := slices.Clone(environ)

	runner, ok := terminalRunner(Options{}, search, environ).(execpath.Exec)
	require.True(t, ok, "the app's own runner, with an environment of its own")
	assert.Equal(t, search, runner.Search)
	assert.Equal(t, []string{
		"PATH=/opt/tools/bin:/usr/bin", "HOME=/srv/home", "CLAUDE_CODE_MAX_OUTPUT_TOKENS=32000",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "CLAUDE_CONFIG_DIR=/srv/home/.claude_work",
		"CODEX_HOME=/srv/home/.codex", "LANG=C",
	}, runner.Env, "every agent session's variables go, and the rest stays as it was, in its order")
	assert.Equal(t, given, environ, "the environment it is given is left alone")

	for _, kv := range runner.Env {
		name, _, _ := strings.Cut(kv, "=")
		assert.NotContains(t, agent.SessionEnv(), name)
	}

	none, ok := terminalRunner(Options{}, search, nil).(execpath.Exec)
	require.True(t, ok)
	assert.NotNil(t, none.Env, "an empty environment is still the terminal's whole one, never this process's")

	fake := noPrograms()
	assert.Same(t, fake, terminalRunner(Options{Runner: fake}, search, environ), "a test's runner runs the terminals")
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

func TestLinkStatesAskAboutItemsAndHeadsInOneRequest(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)

		_, _ = io.WriteString(w, `{"data": {
			"r0": {"item": {"__typename": "PullRequest", "title": "Add locktimes", "url": "https://github.com/acme-org/example-repo/pull/184",
				"prState": "MERGED", "closedAt": "2026-11-06T18:00:00Z", "mergedAt": "2026-11-06T18:00:05Z",
				"headRefOid": "fc1c3975011e66491785ace891c893e4fc716587", "headRefName": "add-locktimes"}},
			"h0": {"pulls": {"nodes": [{"number": 199, "title": "Describe it", "url": "https://github.com/acme-org/example-repo/pull/199",
				"state": "OPEN", "closedAt": null, "mergedAt": null, "headRefOid": "0b1c3975011e66491785ace891c893e4fc716588",
				"headRefName": "feature-x", "headRepository": {"nameWithOwner": "acme-org/example-repo"}}]}},
			"h1": {"pulls": {"nodes": []}}}}`)
	}))
	t.Cleanup(srv.Close)

	states := itemStates{
		tokens: githubtest.NewTokens(),
		client: &github.Client{BaseURL: srv.URL, HTTP: srv.Client()},
	}
	example := repo.Name{Owner: boundOwner, Repo: "Example-Repo"}
	ref := item.Ref{Repo: example, Number: 184}

	got, err := states.LinkStates(t.Context(), []item.Ref{ref},
		[]worktrees.HeadQuery{{Repo: example, Branch: "feature-x"}, {Repo: example, Branch: "old-idea"}})
	require.NoError(t, err)
	assert.Equal(t, int32(1), requests.Load())
	assert.Equal(t, worktrees.ItemMerged, got.Items[ref.Key()].State)
	assert.Equal(t, map[worktrees.BranchKey][]worktrees.HeadPR{
		{Repo: example.Key(), Branch: "feature-x"}: {{
			Number: 199, HeadRepo: example.Key(), State: worktrees.ItemState{
				PullRequest: true, Title: "Describe it", URL: "https://github.com/acme-org/example-repo/pull/199",
				State: worktrees.ItemOpen, HeadOID: "0b1c3975011e66491785ace891c893e4fc716588", HeadRef: "feature-x",
			},
		}},
	}, got.Heads, "keyed by the repository's key, whatever its case; a branch none had is left out")

	_, err = itemStates{
		tokens: githubtest.FailingTokens([]error{github.ErrGHNotFound}),
		client: &github.Client{BaseURL: srv.URL, HTTP: srv.Client()},
	}.LinkStates(t.Context(), []item.Ref{ref}, nil)
	require.Error(t, err, "no token, no answer")
}

func TestTerminalRunsOpenAFollowedScriptInTheTerminal(t *testing.T) {
	t.Parallel()

	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))
	runner := &testutil.Runner{}
	c := bootstrap(t, Options{Paths: paths, Home: testutil.ClaudeHome(t), Runner: runner})
	runs := terminalRuns{launch: paths.Launch, opener: launcher.NewCommandOpener(c.Launcher)}
	logs := filepath.Join(paths.Root, "script-runs")

	opened, err := runs.OpenRun(t.Context(), scriptrun.TerminalRun{
		Name: "run-7", Dir: "/work/example-repo", Argv: []string{"/usr/local/bin/magex", "lint"},
		Env: []string{"MAGEX_NO_UPDATE_CHECK=1"},
		Log: filepath.Join(logs, "7.log"), PIDFile: filepath.Join(logs, "7.pid"), ExitFile: filepath.Join(logs, "7.exit"),
		LinkFile: filepath.Join(logs, "7.link"),
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
	assert.Contains(t, body, `printf '%s\n' "$WARP_FOCUS_URL" > '`+filepath.Join(logs, "7.link")+"'\n",
		"with its tab's focus link recorded beside it")

	if runtime.GOOS != "darwin" {
		require.ErrorIs(t, err, terminal.ErrUnsupportedOS)
		assert.Empty(t, opened, "it opened in no terminal")

		return
	}

	require.NoError(t, err)
	assert.Equal(t, string(terminal.ITerm2), opened, "it says which terminal it opened in: the default")

	calls := runner.Calls()
	require.NotEmpty(t, calls)
	assert.Contains(t, strings.Join(calls[len(calls)-1], " "), scripts[0], "the terminal chosen for launches opens it")

	require.NoError(t, testutil.ErrOf(c.Settings.SetTerminal(t.Context(), settings.TerminalWarp)))

	opened, err = runs.OpenRun(t.Context(), scriptrun.TerminalRun{
		Name: "run-8", Dir: "/work/example-repo", Argv: []string{"/usr/local/bin/magex", "lint"},
		Log: filepath.Join(logs, "8.log"), PIDFile: filepath.Join(logs, "8.pid"), ExitFile: filepath.Join(logs, "8.exit"),
		LinkFile: filepath.Join(logs, "8.link"),
	})
	require.NoError(t, err)
	assert.Equal(t, string(terminal.Warp), opened, "the terminal chosen as the run starts")

	_, err = runs.OpenRun(t.Context(), scriptrun.TerminalRun{Name: "run-9"})
	require.ErrorIs(t, err, errNoCommand)
}

// focusRig is terminalRuns over a real launcher whose terminal programs runner answers,
// for Show in terminal.
type focusRig struct {
	c      *Core
	runner *testutil.Runner
	runs   terminalRuns
}

func newFocusRig(t *testing.T, stdout string) focusRig {
	t.Helper()

	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))
	runner := &testutil.Runner{Stdout: stdout}
	c := bootstrap(t, Options{Paths: paths, Home: testutil.ClaudeHome(t), Runner: runner})

	return focusRig{
		c: c, runner: runner, runs: terminalRuns{launch: paths.Launch, opener: launcher.NewCommandOpener(c.Launcher)},
	}
}

func TestTerminalRunsFocusARunsTabInTheTerminalItOpenedIn(t *testing.T) {
	t.Parallel()

	const link = "warp://session/0123456789abcdef0123456789abcdef"

	t.Run("Warp, by the link the run's script recorded", func(t *testing.T) {
		t.Parallel()

		r := newFocusRig(t, "")
		err := r.runs.FocusRun(t.Context(), scriptrun.TerminalTab{
			Terminal: string(terminal.Warp), TTY: "/dev/ttys004", PID: 4100, Link: link,
		})

		if runtime.GOOS != "darwin" {
			assert.Equal(t, "This terminal can't show a run's tab.", uierr.Message(err))

			return
		}

		require.NoError(t, err)
		assert.Equal(t, [][]string{{"open", "-b", "dev.warp.Warp-Stable", link}}, r.runner.Calls(),
			"the recorded terminal, though iTerm2 is chosen now")
	})

	t.Run("iTerm2, by the run's tty", func(t *testing.T) {
		t.Parallel()

		r := newFocusRig(t, "found\n")
		require.NoError(t, testutil.ErrOf(r.c.Settings.SetTerminal(t.Context(), settings.TerminalWarp)))

		err := r.runs.FocusRun(t.Context(), scriptrun.TerminalTab{
			Terminal: string(terminal.ITerm2), TTY: "/dev/ttys004", PID: 4100,
		})

		if runtime.GOOS != "darwin" {
			assert.Equal(t, "This terminal can't show a run's tab.", uierr.Message(err))

			return
		}

		require.NoError(t, err)

		calls := r.runner.Calls()
		require.Len(t, calls, 1)
		assert.Equal(t, "osascript", calls[0][0], "the recorded terminal, though Warp is chosen now")
		assert.Contains(t, strings.Join(calls[0], " "), `if tty of s is "/dev/ttys004" then`)
	})

	t.Run("a run recorded before runs recorded their terminal", func(t *testing.T) {
		t.Parallel()

		r := newFocusRig(t, "")
		require.NoError(t, testutil.ErrOf(r.c.Settings.SetTerminal(t.Context(), settings.TerminalWarp)))

		err := r.runs.FocusRun(t.Context(), scriptrun.TerminalTab{TTY: "/dev/ttys004", PID: 4100, Link: link})

		if runtime.GOOS != "darwin" {
			assert.Equal(t, "This terminal can't show a run's tab.", uierr.Message(err))

			return
		}

		require.NoError(t, err)
		assert.Equal(t, [][]string{{"open", "-b", "dev.warp.Warp-Stable", link}}, r.runner.Calls(),
			"the terminal chosen now stands in for the one it didn't record")
	})
}

func TestTerminalRunsWordWhyARunsTabCantBeShown(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "darwin" {
		r := newFocusRig(t, "")
		err := r.runs.FocusRun(t.Context(), scriptrun.TerminalTab{Terminal: string(terminal.ITerm2), TTY: "/dev/ttys004"})
		require.ErrorIs(t, err, terminal.ErrUnsupportedOS)
		assert.Equal(t, "This terminal can't show a run's tab.", uierr.Message(err))

		return
	}

	tests := []struct {
		name    string
		stdout  string
		tab     scriptrun.TerminalTab
		wantErr error
		want    string
	}{
		{
			name:    "no tab runs on the tty",
			stdout:  "missing\n",
			tab:     scriptrun.TerminalTab{Terminal: string(terminal.ITerm2), TTY: "/dev/ttys004", PID: 4100},
			wantErr: terminal.ErrNoSuchTab,
			want:    "Couldn't find the run's terminal tab. It may have been closed, or moved to another app.",
		},
		{
			// launchd stands in for the run's shell: both are platform binaries, whose
			// environment macOS hides.
			name:    "no link file, as a run opened before scripts recorded one",
			tab:     scriptrun.TerminalTab{Terminal: string(terminal.Warp), TTY: "/dev/ttys004", PID: 1},
			wantErr: terminal.ErrNoFocusLink,
		},
		{
			name: "a link file that holds no link",
			tab: scriptrun.TerminalTab{
				Terminal: string(terminal.Warp), TTY: "/dev/ttys004", PID: 1, Link: "warp://action/new_tab?path=/tmp",
			},
			wantErr: terminal.ErrNoFocusLink,
		},
		{
			name:    "a terminal this build doesn't know",
			tab:     scriptrun.TerminalTab{Terminal: "hyper", TTY: "/dev/ttys004", PID: 4100},
			wantErr: terminal.ErrUnknownTerminal,
			want:    "This terminal can't show a run's tab.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newFocusRig(t, tt.stdout)
			err := r.runs.FocusRun(t.Context(), tt.tab)
			require.ErrorIs(t, err, tt.wantErr)

			if tt.want != "" {
				assert.Equal(t, tt.want, uierr.Message(err))
			}

			if errors.Is(tt.wantErr, terminal.ErrNoFocusLink) {
				assert.Contains(t, uierr.Message(err), "Couldn't tell which Warp tab runs this.")
				assert.Contains(t, uierr.Message(err), "v0.2026.05.27")
				assert.NotContains(t, uierr.Message(err), "path=", "the words never repeat what the file held")
				assert.Empty(t, r.runner.Calls(), "nothing is opened in Warp")
			}
		})
	}
}

// folderRig is the terminal the runs open in, over a real launcher whose terminal programs
// runner answers, the folder Open in terminal asks for, and the scripts written for it.
type folderRig struct {
	c      *Core
	runner *testutil.Runner
	runs   terminalRuns
	dir    string
	launch string
}

func newFolderRig(t *testing.T) folderRig {
	t.Helper()

	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))
	runner := &testutil.Runner{}
	c := bootstrap(t, Options{Paths: paths, Home: testutil.ClaudeHome(t), Runner: runner})

	return folderRig{
		c: c, runner: runner, dir: filepath.Join(t.TempDir(), "example-repo"), launch: paths.Launch,
		runs: terminalRuns{
			launch: paths.Launch, opener: launcher.NewCommandOpener(c.Launcher), shell: func() string { return "/opt/shells/fish" },
		},
	}
}

// scripts returns the scripts written into the launch folder under name.
func (r folderRig) scripts(t *testing.T, name string) []string {
	t.Helper()

	scripts, err := filepath.Glob(filepath.Join(r.launch, name+"-*"))
	require.NoError(t, err)

	return scripts
}

func TestTerminalRunsOpenATerminalInAFolder(t *testing.T) {
	t.Parallel()

	r := newFolderRig(t)
	require.NoError(t, os.Mkdir(r.dir, 0o700))

	err := r.runs.OpenFolder(t.Context(), "run-9", r.dir)

	if runtime.GOOS != "darwin" {
		require.ErrorIs(t, err, terminal.ErrUnsupportedOS)
		assert.Empty(t, r.scripts(t, "run-9"), "no terminal here, so no script for one")

		return
	}

	require.NoError(t, err)

	scripts := r.scripts(t, "run-9")
	require.Len(t, scripts, 1, "iTerm2, the default, can't open a folder itself, so it opens a script")

	data, readErr := os.ReadFile(scripts[0])
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "cd -- '"+r.dir+"' || exit 1\nexec '/opt/shells/fish' '-l'\n",
		"the user's login shell, in the folder, and nothing else")

	calls := r.runner.Calls()
	require.NotEmpty(t, calls)
	assert.Equal(t, "osascript", calls[len(calls)-1][0])
	assert.Contains(t, strings.Join(calls[len(calls)-1], " "), scripts[0], "iTerm2 opens the script")

	require.NoError(t, testutil.ErrOf(r.c.Settings.SetTerminal(t.Context(), settings.TerminalApp)))
	require.NoError(t, r.runs.OpenFolder(t.Context(), "run-10", r.dir))

	scripts = r.scripts(t, "run-10")
	require.Len(t, scripts, 1, "nor can Terminal.app")

	calls = r.runner.Calls()
	assert.Equal(t, []string{"open", "-a", "Terminal", scripts[0]}, calls[len(calls)-1], "Terminal.app opens the script")
}

func TestTerminalRunsOpenAFolderInWarpItself(t *testing.T) {
	t.Parallel()

	r := newFolderRig(t)
	require.NoError(t, os.Mkdir(r.dir, 0o700))
	require.NoError(t, testutil.ErrOf(r.c.Settings.SetTerminal(t.Context(), settings.TerminalWarp)))

	err := r.runs.OpenFolder(t.Context(), "run-9", r.dir)
	assert.Empty(t, r.scripts(t, "run-9"), "no script: a script's nested shell would lose Warp's blocks")

	if runtime.GOOS != "darwin" {
		require.ErrorIs(t, err, terminal.ErrUnsupportedOS)

		return
	}

	require.NoError(t, err)
	assert.Equal(t, [][]string{{"open", "-b", "dev.warp.Warp-Stable", r.dir}}, r.runner.Calls(),
		"Warp opens the folder itself, a tab whose own shell starts there")
}

func TestTerminalRunsOpenNoTerminalInAFolderThatIsGone(t *testing.T) {
	t.Parallel()

	for _, id := range []terminal.ID{settings.TerminalWarp, settings.TerminalITerm2} {
		t.Run(string(id), func(t *testing.T) {
			t.Parallel()

			r := newFolderRig(t)
			require.NoError(t, testutil.ErrOf(r.c.Settings.SetTerminal(t.Context(), id)))

			err := r.runs.OpenFolder(t.Context(), "run-9", r.dir)
			require.ErrorIs(t, err, terminal.ErrInvalidFolder)
			assert.Equal(t, r.dir+" isn't there anymore.", uierr.Message(err))
			assert.Empty(t, r.scripts(t, "run-9"), "no script for a folder that isn't there")
			assert.Empty(t, r.runner.Calls(), "nothing runs")
		})
	}
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
