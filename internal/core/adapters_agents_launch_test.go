package core

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/worktrees"
)

// rigViewer is the account the routine rigs' items are stored for.
const rigViewer = "octo-user"

// errDiskFull is a worktree that couldn't be made.
var errDiskFull = errors.New("disk full")

// scriptOpener is a terminal that records each launch script it is asked to open, and
// opens none.
type scriptOpener struct {
	id terminal.ID

	mu      sync.Mutex
	scripts []string
}

func (o *scriptOpener) ID() terminal.ID { return o.id }

func (o *scriptOpener) Open(_ context.Context, path string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.scripts = append(o.scripts, path)

	return nil
}

// opened returns the scripts the terminal was asked to open.
func (o *scriptOpener) opened() []string {
	o.mu.Lock()
	defer o.mu.Unlock()

	return append([]string(nil), o.scripts...)
}

// busyFolders answers that a session of the user's runs in each folder it names.
type busyFolders map[string]launcher.RunningSession

func (b busyFolders) RunsIn(_ context.Context, dir string) []launcher.RunningSession {
	if s, ok := b[dir]; ok {
		return []launcher.RunningSession{s}
	}

	return nil
}

// routineRig is a routine's agent steps over a real launcher on a fresh database: a clone
// of acme-org/example-repo under the clone root, a pull request and an issue in it, a pull
// request in a repository with no clone, Claude Code's and Codex's default profiles and a
// work profile, the starter prompts, and fake agent programs that are found but never run
// by a launch, since its terminal only records the script.
type routineRig struct {
	db       *sql.DB
	deps     agentDeps
	opener   *scriptOpener
	maker    *fakeAgentMaker
	busy     busyFolders
	rec      *envRecorder
	prompts  *prompts.Store
	settings *settings.Service
	defaults *launchdefaults.Store
	home     string
	clone    string

	pr, issue, orphan int64
}

// routineRigConfig changes what a routineRig is built with: the fake agent programs'
// runs.
type routineRigConfig struct {
	runs map[string]testutil.FakeRun
}

// newRoutineRig builds the rig with Claude Code signed in. Nothing is busy until a test
// puts a session in busy.
func newRoutineRig(t *testing.T) *routineRig {
	t.Helper()

	return newRoutineRigWith(t, routineRigConfig{
		runs: claudeSays(`{"loggedIn": true, "authMethod": "claude.ai", "subscriptionType": "max"}`, 0),
	})
}

// newRoutineRigWith builds the rig with cfg.
func newRoutineRigWith(t *testing.T, cfg routineRigConfig) *routineRig {
	t.Helper()

	ctx := t.Context()
	pool := testutil.DB(t)
	home := testutil.RealTempDir(t)
	testutil.MakeClaudeDirs(t, home, ".claude", ".codex", ".claude_work", "projects/example-repo/.git", "worktrees/pr-7")

	profileStore := profiles.NewStore(pool, nil)
	require.NoError(t, profileStore.Sync(ctx, []profiles.Profile{
		{Agent: agent.Claude, Name: profiles.DefaultName, AddDirs: []string{}, ExtraArgs: []string{}, Env: map[string]string{}},
		{
			Agent: agent.Claude, Name: workProfile, ConfigDir: filepath.Join(home, ".claude_work"), AddDirs: []string{},
			ExtraArgs: []string{}, Env: map[string]string{},
		},
		{Agent: agent.Codex, Name: profiles.DefaultName, AddDirs: []string{}, ExtraArgs: []string{}, Env: map[string]string{}},
	}))

	promptStore := prompts.NewStore(pool, nil)
	require.NoError(t, prompts.Seed(ctx, promptStore))

	now := time.Now()
	stored := func(number int, name string, pr bool) inbox.FixtureItem {
		return inbox.FixtureItem{Item: github.Item{
			IsPR: pr, NodeID: "NODE_" + strconv.Itoa(number), Number: number, Title: "Change " + strconv.Itoa(number),
			URL:       "https://github.com/acme-org/" + name + "/pull/" + strconv.Itoa(number),
			CreatedAt: now, UpdatedAt: now, HeadRef: "feature-branch", AuthorLogin: rigViewer, AuthorAssociation: "OWNER",
			RepoOwner: boundOwner, RepoName: name,
		}, Authored: true}
	}

	ids, err := inbox.StoreFixture(ctx, pool, inbox.Fixture{Viewer: rigViewer, Items: []inbox.FixtureItem{
		stored(7, "example-repo", true), stored(3, "example-repo", false), stored(9, "no-clone", true),
	}})
	require.NoError(t, err)

	rec := &envRecorder{}
	found := testutil.Finder{Paths: map[string]string{}}

	for name, run := range cfg.runs {
		path, env := testutil.FakeTool(t, name, run)
		found.Paths[name] = path

		rec.fakes = append(rec.fakes, env)
	}

	r := &routineRig{
		opener: &scriptOpener{id: terminal.ITerm2}, maker: &fakeAgentMaker{dir: filepath.Join(home, "worktrees", "pr-7")},
		rec: rec, prompts: promptStore, settings: settings.NewService(pool, profileStore, home),
		defaults: launchdefaults.NewStore(pool, nil), home: home, clone: filepath.Join(home, "projects", "example-repo"),
		db: pool, busy: busyFolders{}, pr: ids[0], issue: ids[1], orphan: ids[2],
	}

	inboxStore := inbox.NewStore(pool, nil)
	launchCfg := launcher.Config{
		Items:    launchItems{Store: inboxStore},
		Settings: settingsView[launcher.LaunchSettings]{settings: r.settings, part: launchSettings},
		Profiles: profileStore, Prompts: promptStore, Defaults: r.defaults, Git: originGit(), Finder: found,
		Registry: terminal.NewRegistry(r.opener), Paths: appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home: home, Worktrees: r.maker, Trust: trusters(home, nil), RunFolders: r.busy,
	}

	r.deps = agentDeps{
		launch: launcher.NewService(launchCfg), prompts: promptStore, profiles: profileStore, settings: r.settings,
		defaults: r.defaults, inbox: inboxStore, finder: found, home: home, runners: rec.runner,
		registry: terminal.NewRegistry(r.opener), environ: func() ([]string, bool) { return []string{"PATH=/usr/bin"}, true },
	}

	return r
}

// job is a routine's terminal step on the item id in repo, as the engine hands it over.
func (r *routineRig) job(id int64, repo string, step spec.AgentStep) autopilot.Job {
	step.Where = catalog.WhereTerminal

	return autopilot.Job{
		MatchID: 1, RunID: 2, RoutineID: 3, RoutineName: "Heal the build", Trigger: catalog.EventChecksFailed, Attempt: 1,
		Item: autopilot.ItemRef{ID: id, Kind: item.KindPR, Repo: repo, Number: 7}, Step: spec.Step{Agent: &step},
	}
}

// addPrompt adds an agent prompt of the user's own to the library.
func (r *routineRig) addPrompt(t *testing.T, nickname, body string) prompts.Prompt {
	t.Helper()

	p, err := prompts.NewService(prompts.Config{Store: r.prompts}).Create(t.Context(), prompts.Input{
		Name: nickname + " prompt", Nickname: nickname, AppliesTo: prompts.AppliesToAny, Body: body,
	})
	require.NoError(t, err)

	return p
}

func TestARoutinesTerminalStepOpensTheLaunchWithItsChoices(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	r.addPrompt(t, "Heal", "Routine {{.Routine}} after {{.Trigger}}, try {{.Attempt}}.")

	got, err := agentRuns{deps: r.deps}.Dispatch(t.Context(), r.job(r.pr, "acme-org/example-repo", spec.AgentStep{
		Agent: claudeID, Profile: workProfile, Model: "opus", Effort: "high", Prompt: spec.PromptRef{Nickname: "heal"},
		Worktree: new(false),
	}))
	require.NoError(t, err)

	assert.True(t, got.Sent)
	assert.Equal(t, "Opened claude in iTerm2, in ~/projects/example-repo.", got.Summary,
		"a run in the clone names the clone it opened in")
	require.NotNil(t, got.Launched)
	assert.NotEmpty(t, got.Launched.SessionID, "the session is followed by the ID the launch minted")
	assert.Equal(t, claudeID, got.Launched.Agent)
	assert.Equal(t, workProfile, got.Launched.Profile, "the step's profile is the launch's")
	assert.Equal(t, "~/projects/example-repo", got.Launched.ClonePath)
	assert.Empty(t, got.Launched.Worktree)

	scripts := r.opener.opened()
	require.Len(t, scripts, 1, "the terminal opens the one launch")

	script, err := os.ReadFile(scripts[0])
	require.NoError(t, err)
	assert.Contains(t, string(script), "opus", "the step's model is the agent's")

	prompt, err := os.ReadFile(promptOf(scripts[0]))
	require.NoError(t, err)
	assert.Equal(t, "Routine Heal the build after Checks fail, try 2.\n", string(prompt),
		"the prompt, found by its nickname ignoring case, names the routine, its trigger, and this try")
}

// promptOf is the prompt file a launch script reads.
func promptOf(script string) string {
	return script[:len(script)-len(filepath.Ext(script))] + terminal.PromptSuffix
}

func TestARoutinesTerminalStepInAWorktreeNamesTheWorktree(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)

	got, err := agentRuns{deps: r.deps}.Dispatch(t.Context(), r.job(r.pr, "acme-org/example-repo", spec.AgentStep{
		Prompt: spec.PromptRef{Starter: prompts.StarterQuickLook}, Worktree: new(true),
	}))
	require.NoError(t, err)

	assert.True(t, got.Sent)
	assert.Equal(t, "Opened claude in iTerm2, in pr-7.", got.Summary, "a run in a worktree names the worktree")
	require.NotNil(t, got.Launched)
	assert.Equal(t, "pr-7", got.Launched.Worktree)
	assert.Equal(t, profiles.DefaultName, got.Launched.Profile, "an inherited profile is the agent's default")
	assert.Len(t, r.maker.asked(), 1, "the worktree is made once the launch isn't held")
}

func TestARoutinesTerminalStepWhoseHeadMovedGoesBackInTheQueue(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	r.maker.err = worktrees.ErrHeadMoved

	got, err := agentRuns{deps: r.deps}.Dispatch(t.Context(), r.job(r.pr, "acme-org/example-repo", spec.AgentStep{
		Prompt: spec.PromptRef{ItemDefault: true}, Worktree: new(true),
	}))
	require.NoError(t, err)

	assert.True(t, got.Requeue, "a head that moved as the worktree was made decides the match again")
	assert.Equal(t, "Its head moved as its worktree was made.", got.WaitWords)
	assert.False(t, got.Sent)
	assert.Empty(t, r.opener.opened(), "and opens nothing")

	r.maker.err = errDiskFull

	_, err = agentRuns{deps: r.deps}.Dispatch(t.Context(), r.job(r.pr, "acme-org/example-repo", spec.AgentStep{
		Worktree: new(true),
	}))
	require.ErrorContains(t, err, "disk full", "any other failure is the step's")
}

func TestARoutinesTerminalStepWaitsForASessionWorkingInItsFolder(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	r.busy[r.clone] = launcher.RunningSession{
		SessionID: "s-1", Agent: agent.Claude, Doing: "is working", Dir: "~/projects/example-repo",
	}

	got, err := agentRuns{deps: r.deps}.Dispatch(t.Context(), r.job(r.pr, "acme-org/example-repo", spec.AgentStep{
		Worktree: new(false),
	}))
	require.NoError(t, err)

	assert.False(t, got.Sent)
	assert.Equal(t, autopilot.BlockBusyWorktree, got.Wait, "a session working where it would start queues it")
	assert.Equal(t, "Queued: ~/projects/example-repo is in use by a session in your terminal; starts when it ends.",
		got.WaitWords)
	assert.Empty(t, r.opener.opened(), "and opens nothing")
}

func TestADispatchOfAStepWithNoAgentOrInTheBackgroundStartsNothing(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	runs := agentRuns{deps: r.deps}

	_, err := runs.Dispatch(t.Context(), autopilot.Job{Step: spec.Step{Pin: &spec.Empty{}}})
	require.ErrorIs(t, err, autopilot.ErrNoAgentStep)
	assert.Equal(t, "This step runs no agent.", uierr.Message(err))

	background := r.job(r.pr, "acme-org/example-repo", spec.AgentStep{})
	background.Step.Agent.Where = catalog.WhereBackground

	got, err := runs.Dispatch(t.Context(), background)
	require.NoError(t, err)
	assert.Equal(t, autopilot.BlockNotReady, got.Wait, "a copy with no background runner can't run it")
	assert.Contains(t, got.WaitWords, "can't run an agent in the background")
	assert.Empty(t, r.opener.opened())

	require.NoError(t, runs.Stop(t.Context(), 1), "a terminal run is the user's once it opens")
	require.NoError(t, runs.StopAll(t.Context()))

	live, err := runs.Alive(t.Context(), 1)
	require.NoError(t, err)
	assert.Equal(t, autopilot.LiveGone, live, "a terminal run still recorded was cut off, never started again")
}

func TestARoutinesStepWhosePromptIsGoneSaysSo(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)

	_, err := agentRuns{deps: r.deps}.Dispatch(t.Context(), r.job(r.pr, "acme-org/example-repo", spec.AgentStep{
		Prompt: spec.PromptRef{Nickname: "Renamed away"},
	}))
	require.ErrorIs(t, err, prompts.ErrPromptNotFound)
	assert.Equal(t, "Its prompt, “Renamed away”, isn't in your library any more.", uierr.Message(err))

	_, err = agentRuns{deps: r.deps}.Dispatch(t.Context(), r.job(r.pr, "acme-org/example-repo", spec.AgentStep{
		Prompt: spec.PromptRef{Starter: "a-starter-of-a-later-version"},
	}))
	require.ErrorIs(t, err, prompts.ErrPromptNotFound)
	assert.Equal(t, "Its prompt, “a-starter-of-a-later-version”, isn't in your library any more.", uierr.Message(err),
		"a starter is named by its key when the step has no nickname")
	assert.Empty(t, r.opener.opened())

	_, err = agentRuns{deps: r.deps}.Dispatch(t.Context(), r.job(r.pr+100, "acme-org/example-repo", spec.AgentStep{}))
	require.Error(t, err, "an item no longer stored can't be launched")
}
