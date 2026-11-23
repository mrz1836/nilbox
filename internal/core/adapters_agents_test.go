package core

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/shellenv"
	"github.com/mrz1836/nilbox/internal/statusprefs"
	"github.com/mrz1836/nilbox/internal/svcstatus"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// envRecorder runs agents' commands with the environment it is given plus the fakes', and
// records each environment it was given.
type envRecorder struct {
	mu     sync.Mutex
	search execpath.Search
	fakes  []string
	envs   [][]string
}

func (r *envRecorder) runner(env []string) execpath.Runner {
	r.mu.Lock()
	r.envs = append(r.envs, slices.Clone(env))
	r.mu.Unlock()

	return execpath.Exec{Search: r.search, Env: append(slices.Clone(env), r.fakes...)}
}

// runs returns how many commands ran.
func (r *envRecorder) runs() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.envs)
}

// readyRig is a readiness checker over a fresh database with Claude Code's default
// profile and a work profile, and fake agent programs.
type readyRig struct {
	checks   *readyChecks
	settings *settings.Service
	rec      *envRecorder
	home     string
	shell    []string
}

// newReadyRig makes the agents in runs fake programs, each playing its FakeRun, and
// returns a checker whose login shell's environment is shell.
func newReadyRig(t *testing.T, runs map[string]testutil.FakeRun, shell []string) *readyRig {
	t.Helper()

	db := testutil.DB(t)
	home := t.TempDir()
	work := filepath.Join(home, ".claude_work")
	require.NoError(t, os.MkdirAll(work, 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex"), 0o700))

	store := profiles.NewStore(db, nil)
	require.NoError(t, store.Sync(t.Context(), []profiles.Profile{
		{Agent: agent.Claude, Name: profiles.DefaultName, AddDirs: []string{}, ExtraArgs: []string{}, Env: map[string]string{}},
		{
			Agent: agent.Claude, Name: "work", ConfigDir: work, AddDirs: []string{}, ExtraArgs: []string{},
			Env: map[string]string{},
		},
		{Agent: agent.Codex, Name: profiles.DefaultName, AddDirs: []string{}, ExtraArgs: []string{}, Env: map[string]string{}},
	}))

	// The fakes are the only programs found: nothing on this machine's PATH ever runs.
	rec := &envRecorder{}
	found := testutil.Finder{Paths: map[string]string{}}

	for name, run := range runs {
		path, env := testutil.FakeTool(t, name, run)
		found.Paths[name] = path

		rec.fakes = append(rec.fakes, env)
	}

	r := &readyRig{
		settings: settings.NewService(db, store, home), rec: rec, home: home, shell: shell,
	}
	r.checks = newReadyChecks(agentDeps{
		profiles: store, settings: r.settings, finder: found, home: home, runners: rec.runner,
		registry: terminal.NewRegistry(), environ: func() ([]string, bool) { return r.shell, true },
	})

	return r
}

// check checks one need.
func (r *readyRig) check(t *testing.T, n autopilot.Need, fresh bool) autopilot.ReadyCheck {
	t.Helper()

	got, err := r.checks.Check(t.Context(), []autopilot.Need{n}, fresh)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, n.ID(), got[0].ID)

	return got[0]
}

// claudeSays is a fake Claude Code that prints its version, then stdout, and exits with
// exit: what both its --version and its auth status print.
func claudeSays(stdout string, exit int) map[string]testutil.FakeRun {
	return map[string]testutil.FakeRun{"claude": {Stdout: "2.1.325 (Claude Code)\n" + stdout, Exit: exit}}
}

// The needs the tests check.
const (
	needClaude = iota
	needDefault
	needWork
	needCodex
	needCodexSignIn
)

// needOf is one of the needs the tests check.
func needOf(which int) autopilot.Need {
	return []autopilot.Need{
		{Kind: autopilot.NeedAgent, Agent: "claude"},
		{Kind: autopilot.NeedProfile, Agent: "claude", Profile: "default"},
		{Kind: autopilot.NeedProfile, Agent: "claude", Profile: "work"},
		{Kind: autopilot.NeedAgent, Agent: "codex"},
		{Kind: autopilot.NeedProfile, Agent: "codex", Profile: "default"},
	}[which]
}

func TestReadinessReadsAnAgentsVersionAndASubscriptionsSignIn(t *testing.T) {
	t.Parallel()

	r := newReadyRig(t, claudeSays(`{"loggedIn": true, "authMethod": "claude.ai", "email": "someone@example.com", `+
		`"subscriptionType": "max"}`, 0), []string{"PATH=/usr/bin"})

	version := r.check(t, needOf(needClaude), true)
	assert.Equal(t, autopilot.ReadyOK, version.Level)
	assert.Equal(t, "2.1.325", version.Found)

	signedIn := r.check(t, needOf(needWork), true)
	assert.Equal(t, autopilot.ReadyOK, signedIn.Level, "a Max subscription signed in through the browser is ready")
	assert.Equal(t, "signed in · max", signedIn.Found)
	assert.NotContains(t, signedIn.Found+signedIn.Fix, "example.com", "the account's email is never kept")

	runs := r.rec.runs()
	again := r.check(t, needOf(needWork), false)
	assert.Equal(t, signedIn.Found, again.Found)
	assert.Equal(t, runs, r.rec.runs(), "a sign-in checked within ten minutes isn't asked again")

	r.check(t, needOf(needWork), true)
	assert.Equal(t, runs+1, r.rec.runs(), "Check again asks again")
}

func TestReadinessSaysAProfileSignedOutIsAProblemWithItsFix(t *testing.T) {
	t.Parallel()

	r := newReadyRig(t, claudeSays(`{"loggedIn": false}`, 1), nil)

	work := r.check(t, needOf(needWork), true)
	assert.Equal(t, autopilot.ReadyProblem, work.Level)
	assert.Equal(t, "is signed out", work.Found)
	assert.True(t, work.Command)
	assert.Equal(t, "CLAUDE_CONFIG_DIR=~/.claude_work claude auth login", work.Fix,
		"the fix is the agent's own login under the profile's folder, which the user runs")
	assert.NotContains(t, work.Fix, "API_KEY", "readiness never offers a key as a way to get ready")

	def := r.check(t, needOf(needDefault), true)
	assert.Equal(t, "claude auth login", def.Fix, "the default profile signs in without the config variable")

	// codex-cli says "Not logged in" on stderr alone, and exits 1.
	codex := newReadyRig(t, map[string]testutil.FakeRun{"codex": {Stderr: "Not logged in\n", Exit: 1}}, nil)
	out := codex.check(t, needOf(needCodexSignIn), true)
	assert.Equal(t, autopilot.ReadyProblem, out.Level)
	assert.Equal(t, "is signed out", out.Found)
	assert.Equal(t, "codex login", out.Fix)
	assert.NotContains(t, out.Fix, "API_KEY")

	// codex-cli refuses a CODEX_HOME that doesn't exist; the check says so without asking it.
	require.NoError(t, os.Remove(filepath.Join(codex.home, ".codex")))

	runs := codex.rec.runs()
	gone := codex.check(t, needOf(needCodexSignIn), true)
	assert.Equal(t, autopilot.ReadyProblem, gone.Level)
	assert.Equal(t, "has no folder at ~/.codex", gone.Found)
	assert.Equal(t, runs, codex.rec.runs(), "nothing runs for a profile with no folder")
}

func TestReadinessWarnsOfASignInItCantReadOrThatExpiresSoon(t *testing.T) {
	t.Parallel()

	soon := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	r := newReadyRig(t, claudeSays(`{"loggedIn": true, "authMethod": "oauth_token", "loginExpiresAt": "`+soon+`"}`, 0), nil)

	expiring := r.check(t, needOf(needWork), true)
	assert.Equal(t, autopilot.ReadyWarning, expiring.Level, "a sign-in that expires within a week is a warning")
	assert.Contains(t, expiring.Found, "expires in 1 day")

	odd := newReadyRig(t, map[string]testutil.FakeRun{"claude": {Stdout: "something new\n"}}, nil)
	unknown := odd.check(t, needOf(needWork), true)
	assert.Equal(t, autopilot.ReadyWarning, unknown.Level)

	version := odd.check(t, needOf(needClaude), true)
	assert.Equal(t, autopilot.ReadyWarning, version.Level, "an agent whose version can't be read only warns")
}

func TestReadinessSaysAnAgentThatIsntInstalled(t *testing.T) {
	t.Parallel()

	r := newReadyRig(t, nil, nil)

	got := r.check(t, needOf(needCodex), true)
	assert.Equal(t, autopilot.ReadyProblem, got.Level)
	assert.Equal(t, "isn't installed", got.Found)

	profile := r.check(t, needOf(needCodexSignIn), true)
	assert.Equal(t, autopilot.ReadyProblem, profile.Level)

	missing := r.check(t, autopilot.Need{Kind: autopilot.NeedProfile, Agent: "claude", Profile: "gone"}, true)
	assert.Equal(t, autopilot.ReadyProblem, missing.Level)
	assert.Equal(t, "isn't on this Mac", missing.Found)
}

func TestASignInCheckRunsWithoutAnAPIKeyUnlessTheSettingLetsOne(t *testing.T) {
	t.Parallel()

	shell := []string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=sk-from-the-shell", "ANTHROPIC_AUTH_TOKEN=tok", "HOME=/x"}
	r := newReadyRig(t, claudeSays(`{"loggedIn": true, "authMethod": "claude.ai"}`, 0), shell)

	got := r.check(t, needOf(needWork), true)
	assert.Equal(t, autopilot.ReadyOK, got.Level)

	r.rec.mu.Lock()
	env := r.rec.envs[len(r.rec.envs)-1]
	r.rec.mu.Unlock()

	assert.Empty(t, shellenv.Lookup(env, "ANTHROPIC_API_KEY"), "a key in the shell never reaches the agent while off")
	assert.Empty(t, shellenv.Lookup(env, "ANTHROPIC_AUTH_TOKEN"))
	assert.Equal(t, filepath.Join(r.home, ".claude_work"), shellenv.Lookup(env, "CLAUDE_CONFIG_DIR"),
		"it reads the profile's own folder")

	_, err := r.settings.SetClaudeAPIKey(t.Context(), true)
	require.NoError(t, err)

	runs := r.rec.runs()
	keyed := r.check(t, needOf(needWork), true)
	assert.Equal(t, autopilot.ReadyOK, keyed.Level, "on, a key from the shell counts as ready too")
	assert.Equal(t, "an API key from your shell signs it in", keyed.Found)
	assert.Equal(t, runs, r.rec.runs(), "with nothing to ask the agent")

	// codex-cli writes its sign-in to stderr alone.
	codex := newReadyRig(t, map[string]testutil.FakeRun{"codex": {Stderr: "Logged in using ChatGPT\n"}},
		[]string{"OPENAI_API_KEY=sk-x", "CODEX_API_KEY=sk-y", "CODEX_ACCESS_TOKEN=tok"})

	signedIn := codex.check(t, needOf(needCodexSignIn), true)
	assert.Equal(t, autopilot.ReadyOK, signedIn.Level, "a sign-in told on stderr is read")

	codex.rec.mu.Lock()
	codexEnv := codex.rec.envs[len(codex.rec.envs)-1]
	codex.rec.mu.Unlock()

	assert.Empty(t, shellenv.Lookup(codexEnv, "OPENAI_API_KEY"))
	assert.Empty(t, shellenv.Lookup(codexEnv, "CODEX_API_KEY"))
	assert.Empty(t, shellenv.Lookup(codexEnv, "CODEX_ACCESS_TOKEN"), "an access token overrides the ChatGPT sign-in too")
	_, set := shellenv.LookupOK(codexEnv, "CODEX_HOME")
	assert.False(t, set, "the default profile runs without CODEX_HOME")
}

func TestAMajorOutageOnTheAgentsPartHoldsItsStart(t *testing.T) {
	t.Parallel()

	st := svcstatus.Status{Providers: []svcstatus.Provider{{
		Name: "Claude", Affected: []svcstatus.Part{{ID: "claude.code", State: svcstatus.StateMajor}},
	}}}

	name, down := outageOf(st, statusprefs.PartID(agent.Default().StatusPart))
	assert.True(t, down)
	assert.Equal(t, "Claude", name)

	st.Providers[0].Affected[0].State = svcstatus.StateMinor
	_, down = outageOf(st, "claude.code")
	assert.False(t, down, "a minor incident holds nothing")

	st.Providers[0].Affected[0].State = svcstatus.StateCritical
	_, down = outageOf(st, "openai.codex-cli")
	assert.False(t, down, "another agent's provider holds nothing of this one")
}

// claudeDown answers Claude's status page with Claude Code in a major outage, and every
// other page with an empty summary.
type claudeDown struct{}

func (claudeDown) FetchStatus(_ context.Context, req github.StatusRequest) (github.StatusAnswer, error) {
	if req.Host != "status.claude.com" {
		return github.StatusAnswer{Body: []byte(`{"components":[],"incidents":[]}`)}, nil
	}

	return github.StatusAnswer{Body: []byte(`{"status":{"indicator":"major"},"components":[` +
		`{"id":"yyzkbfz2thpt","name":"Claude Code","status":"major_outage"}],"incidents":[]}`)}, nil
}

// holdPrefs is Settings → Autopilot, with Hold agent starts while their provider reports
// an outage as given.
type holdPrefs bool

func (h holdPrefs) Autopilot(context.Context) (autopilotprefs.Config, error) {
	cfg := autopilotprefs.Defaults()
	cfg.HoldOnOutage = bool(h)

	return cfg, nil
}

func TestAMajorOutageHoldsTheAgentsStartOnlyWhileTheSettingSaysSo(t *testing.T) {
	t.Parallel()

	rules := svcstatus.Rules{Every: 30 * time.Minute, Watch: statusprefs.Defaults()}
	status := svcstatus.NewService(svcstatus.Config{
		Fetcher: claudeDown{},
		Rules:   func(context.Context) svcstatus.Rules { return rules },
	})
	status.CheckNow(t.Context())

	held := agentHolds{deps: agentDeps{status: status}, prefs: holdPrefs(true)}

	blocker, words := held.outage(t.Context(), agent.Default())
	assert.Equal(t, autopilot.BlockOutage, blocker, "a major outage on Claude Code holds its start")
	assert.Equal(t, "Queued: Claude reports a major outage; starts once it's back.", words, "and says why")

	codex, err := agent.Lookup(agent.Codex)
	require.NoError(t, err)

	blocker, _ = held.outage(t.Context(), codex)
	assert.Empty(t, blocker, "another provider's outage holds nothing of Codex")

	off := agentHolds{deps: agentDeps{status: status}, prefs: holdPrefs(false)}
	blocker, words = off.outage(t.Context(), agent.Default())
	assert.Empty(t, blocker, "with the setting off, nothing waits for the outage")
	assert.Empty(t, words)
}

func TestALaunchHoldBecomesWhatTheMatchWaitsOn(t *testing.T) {
	t.Parallel()

	files := []sensitive.Finding{{Path: ".claude/settings.json"}}

	agentFiles := heldResult(launcher.Hold{Key: "k1", Reason: launcher.HoldAgentFiles, Head: "abc", Files: files})
	assert.Equal(t, autopilot.BlockHold, agentFiles.Wait)
	require.NotNil(t, agentFiles.Hold)
	assert.Equal(t, "k1", agentFiles.Hold.Key)
	assert.Equal(t, "Held: it changes agent configuration (.claude/settings.json).", agentFiles.WaitWords)

	unchecked := heldResult(launcher.Hold{Key: "k2", Reason: launcher.HoldUnchecked})
	assert.Equal(t, autopilot.BlockHold, unchecked.Wait)

	dirty := heldResult(launcher.Hold{Key: "k3", Reason: launcher.HoldDirty, Worktree: "pr-88", Dirty: 3})
	assert.Equal(t, autopilot.BlockDirty, dirty.Wait)
	assert.Equal(t, "pr-88 has 3 uncommitted files.", dirty.WaitWords)

	running := heldResult(launcher.Hold{Key: "k4", Reason: launcher.HoldRunning, Worktree: "pr-88"})
	assert.Equal(t, autopilot.BlockBusyWorktree, running.Wait, "a session working there queues it")
	assert.Nil(t, running.Hold, "with nothing for the user to confirm")
}

func TestARepositorysLaunchRowWinsOverItsOrgsAndTheStepsOwn(t *testing.T) {
	t.Parallel()

	step := spec.AgentStep{Agent: "claude", Profile: "default", Model: "sonnet", LaunchFor: []spec.LaunchRow{
		{Target: "acme-org/api", Profile: "work"},
		{Target: "ACME-ORG", Agent: "codex", Model: "gpt-6"},
	}}

	assert.Equal(t, spec.LaunchRow{Agent: "codex", Profile: "work", Model: "gpt-6"}, launchRowFor(step, "acme-org/api"))
	assert.Equal(t, spec.LaunchRow{Agent: "codex", Profile: "default", Model: "gpt-6"}, launchRowFor(step, "acme-org/web"))
	assert.Equal(t, spec.LaunchRow{Agent: "claude", Profile: "default", Model: "sonnet"}, launchRowFor(step, "other/web"))
	assert.Equal(t, agent.Inherit, agentOf("inherit"))
}

// originGit answers `git config --get-regexp` in every clone with origin acme-org/<the
// clone's folder>, the read the launcher matches a clone by, and runs nothing else.
func originGit() *testutil.Runner {
	return &testutil.Runner{Script: func(_ context.Context, argv []string) ([]byte, []byte, error) {
		if dir, ok := gitDir(argv); ok && slices.Contains(argv, "--get-regexp") {
			return []byte("remote.origin.url git@github.com:acme-org/" + filepath.Base(dir) + ".git\n"), nil, nil
		}

		return nil, []byte("not run"), exitError{}
	}}
}

// gitDir returns the folder a `git -C <dir> ...` argv runs in.
func gitDir(argv []string) (string, bool) {
	i := slices.Index(argv, "-C")
	if i < 0 || i+1 >= len(argv) {
		return "", false
	}

	return argv[i+1], true
}

func TestARunOnARepositoryWhoseToolIsMissingWaitsAndNamesTheTool(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	home := testutil.RealTempDir(t)
	testutil.MakeClaudeDirs(t, home, ".claude", "projects/example-repo/.git", "projects/plain-repo/.git")

	// The clone's files say it needs go and pnpm.
	repoDir := filepath.Join(home, "projects", "example-repo")
	testutil.WriteFile(t, filepath.Join(repoDir, "go.mod"), "module example.com/example-repo\n")
	testutil.WriteFile(t, filepath.Join(repoDir, "pnpm-lock.yaml"), "lockfileVersion: '9.0'\n")

	opts := testOptions(t)
	opts.Home = home
	opts.Runner = originGit()
	c := bootstrap(t, opts)

	now := time.Now()
	pr := func(number int, name string) inbox.FixtureItem {
		return inbox.FixtureItem{Item: github.Item{
			IsPR: true, NodeID: "NODE_" + strconv.Itoa(number), Number: number, Title: "Bump example-lib",
			URL:       "https://github.com/acme-org/" + name + "/pull/" + strconv.Itoa(number),
			CreatedAt: now, UpdatedAt: now, AuthorLogin: "dependabot", RepoOwner: boundOwner, RepoName: name,
		}, ReviewRequested: true}
	}

	ids, err := inbox.StoreFixture(ctx, c.DB, inbox.Fixture{
		Viewer: "octo-user", Items: []inbox.FixtureItem{pr(7, "example-repo"), pr(8, "plain-repo"), pr(9, "no-clone")},
	})
	require.NoError(t, err)

	holdsWith := func(found ...string) agentHolds {
		paths := map[string]string{}
		for _, name := range found {
			paths[name] = "/opt/tools/bin/" + name
		}

		return agentHolds{
			deps:   agentDeps{launch: c.Launcher, home: home},
			checks: &readyChecks{deps: agentDeps{finder: testutil.Finder{Paths: paths}}},
		}
	}
	job := func(id int64, name string) autopilot.Job {
		return autopilot.Job{Item: autopilot.ItemRef{ID: id, Kind: item.KindPR, Repo: "acme-org/" + name}}
	}

	blocker, words, err := holdsWith("go").cloneHolds(ctx, job(ids[0], "example-repo"))
	require.NoError(t, err)
	assert.Equal(t, autopilot.BlockMissingTool, blocker, "a tool the clone's files need, missing on this Mac, holds the run")
	assert.Equal(t, "acme-org/example-repo needs pnpm, which isn't on this Mac.", words, "naming the tool")

	blocker, words, err = holdsWith("go", "pnpm").cloneHolds(ctx, job(ids[0], "example-repo"))
	require.NoError(t, err)
	assert.Empty(t, blocker, "once the tool is found, nothing holds it")
	assert.Empty(t, words)

	blocker, _, err = holdsWith().cloneHolds(ctx, job(ids[1], "plain-repo"))
	require.NoError(t, err)
	assert.Empty(t, blocker, "a clone whose files name no tool needs none")

	blocker, words, err = holdsWith("go", "pnpm").cloneHolds(ctx, job(ids[2], "no-clone"))
	require.NoError(t, err)
	assert.Equal(t, autopilot.BlockNoClone, blocker)
	assert.Equal(t, "No local clone of acme-org/no-clone.", words)
}

func TestARepositorysMarkersNameTheToolsItNeeds(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"go.mod", "pnpm-lock.yaml", ".mage.yaml"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, 0o600))
	}

	require.NoError(t, os.Mkdir(filepath.Join(dir, "magefiles"), 0o700))

	assert.Equal(t, []string{"go", "pnpm", "magex"}, repoTools(dir), "each once, from the files alone")
	assert.Empty(t, repoTools(t.TempDir()))
}

// TestReadinessWaitsOnlyForToolsTheFenceLetsRun keeps readiness's markers within the fence's:
// a tool a routine waits for on a marker must be one fence.Markers lets an agent run on that
// marker, or a fenced run would wait for a tool it then may not run.
func TestReadinessWaitsOnlyForToolsTheFenceLetsRun(t *testing.T) {
	t.Parallel()

	allowed := map[string][]string{}
	for _, m := range fence.Markers() {
		allowed[m.Name] = m.Tools
	}

	for _, m := range toolMarkers() {
		tools, ok := allowed[m.file]
		if assert.True(t, ok, "readiness reads %s, which fence.Markers doesn't name", m.file) {
			assert.Contains(t, tools, m.tool,
				"readiness waits for %s on %s, which fence.Markers doesn't let an agent run there", m.tool, m.file)
		}
	}
}

// ensure the recorder satisfies the runner shape the deps take.
var _ = func(context.Context) {}
