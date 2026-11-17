package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/agentrun"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/shellenv"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/worktrees"
)

// agentHead is the pull request's head the tests' runs start on.
const agentHead = "4f2a9c1e0d5b6a7c8e9f0a1b2c3d4e5f6a7b8c9d"

// waitAgent bounds how long a test waits for a run.
const waitAgent = 20 * time.Second

func TestAgentEnvIsTheShellsWithThePlanOnTopAndTheAgentsOwnDropped(t *testing.T) {
	t.Parallel()

	home := "/srv/home"
	base := []string{
		"PATH=/opt/bin:/usr/bin", "CLAUDECODE=1", "CLAUDE_CODE_ENTRYPOINT=cli", "CLAUDE_EFFORT=high", "CLAUDE_PID=1",
		"CLAUDE_TMPDIR=/tmp/x", "ANTHROPIC_API_KEY=secret", "CLAUDE_CONFIG_DIR=/srv/home/.claude_old", "EDITOR=vim",
	}

	// launcher.AgentEnv's plan for the work profile, the API key setting off.
	work := launcher.Plan{
		Agent: agent.Claude, Env: map[string]string{"CLAUDE_CONFIG_DIR": "/srv/home/.claude_work", "PROFILE_VAR": "on"},
		Unset: []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"},
	}
	got := agentEnv(base, work, home)

	assert.Equal(t, "/opt/bin:/usr/bin", shellenv.Lookup(got, "PATH"), "the login shell's PATH, as a terminal gives")
	assert.Equal(t, "/srv/home/.claude_work", shellenv.Lookup(got, "CLAUDE_CONFIG_DIR"), "the plan's on top")
	assert.Equal(t, "on", shellenv.Lookup(got, "PROFILE_VAR"))
	assert.Equal(t, "vim", shellenv.Lookup(got, "EDITOR"))

	for _, name := range []string{"CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_EFFORT", "CLAUDE_PID", "CLAUDE_TMPDIR"} {
		_, set := shellenv.LookupOK(got, name)
		assert.False(t, set, "%s steers the agent itself, so it is dropped", name)
	}

	_, set := shellenv.LookupOK(got, "ANTHROPIC_API_KEY")
	assert.False(t, set, "no API key unless the user lets the agent use one")

	work.Unset = nil
	_, set = shellenv.LookupOK(agentEnv(base, work, home), "ANTHROPIC_API_KEY")
	assert.True(t, set, "the setting lets it through")

	def := agentEnv(base, launcher.Plan{Agent: agent.Claude, Unset: []string{"CLAUDE_CONFIG_DIR"}}, home)
	_, set = shellenv.LookupOK(def, "CLAUDE_CONFIG_DIR")
	assert.False(t, set, "the default profile unsets the config folder")

	named := agentEnv(base, launcher.Plan{Agent: agent.Claude, Env: map[string]string{
		"CLAUDE_CONFIG_DIR": filepath.Join(home, ".claude"),
	}}, home)
	_, set = shellenv.LookupOK(named, "CLAUDE_CONFIG_DIR")
	assert.False(t, set, "the default folder is never named, which signs the agent out")
}

func TestAgentEnvFallsBackToTheFindersFoldersWithoutTheShell(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	unread := shellenv.New(shellenv.Config{Home: home})
	b := &backgroundAgents{shellEnv: unread.Env, home: home}

	got := b.envFor(launcher.Plan{Agent: agent.Claude})
	assert.Equal(t, execpath.FallbackPath(home, shellenv.Lookup(os.Environ(), "PATH")), shellenv.Lookup(got, "PATH"),
		"while the login shell can't be read, the app's own environment with the finder's folders")
}

func TestAnAgentTooOldForTheBackgroundIsNotReadyThere(t *testing.T) {
	t.Parallel()

	claude, err := agent.Lookup(agent.Claude)
	require.NoError(t, err)

	check := func(found string) autopilot.ReadyCheck {
		return autopilot.ReadyCheck{
			Need: autopilot.Need{Kind: autopilot.NeedAgent, Agent: string(agent.Claude)}, Level: autopilot.ReadyOK,
			Found: found,
		}
	}

	assert.Equal(t, claude.Name+" 2.1.300 is older than 2.1.319, which runs agents in the background. Update it, "+
		"then Check again.", tooOld(claude, check("2.1.300"), catalog.WhereBackground))
	assert.Empty(t, tooOld(claude, check("2.1.319"), catalog.WhereBackground), "its first version runs them")
	assert.Empty(t, tooOld(claude, check("2.1.300"), catalog.WhereTerminal), "a terminal's launch takes any version")
	assert.Empty(t, tooOld(claude, check("unreadable"), catalog.WhereBackground), "a version not read holds nothing")
}

func TestABackgroundStepGoesFromItsMatchToDone(t *testing.T) {
	t.Parallel()

	r := newAgentRig(t)
	plan := r.planner.plan(t, testutil.FakeRun{Stdout: agentFixture(t, "success-structured.jsonl")})
	r.planner.set(plan)

	res, err := r.agents.Dispatch(t.Context(), agentJob(7))
	require.NoError(t, err)
	assert.True(t, res.Running, "the run goes on after the start")
	require.NotNil(t, res.Launched, "what it started is told for the run's record")

	end := r.engine.end(t, 7)
	assert.Equal(t, autopilot.RunDone, end.Outcome)
	assert.True(t, strings.HasPrefix(end.Summary, "Finished: "), end.Summary)
	require.NotNil(t, end.Cost)
	assert.Positive(t, end.TokensOut)
	assert.Empty(t, end.Commit, "a read-only run pushes nothing")

	started := res.Launched
	assert.Positive(t, started.ScriptRunID)
	assert.Equal(t, plan.SessionID, started.SessionID)
	assert.Equal(t, r.maker.dir, started.Worktree)

	assert.Equal(t, []launcher.WorktreeRequest{{Kind: item.KindPR, Number: 12, Head: agentHead}}, r.maker.asked(),
		"the worktree is made at the head the match was decided on")
	assert.Equal(t, 1, r.planner.trusted(), "the worktree is trusted before the agent starts")

	fences := r.planner.fences()
	require.Len(t, fences, 1, "its fence is worked out for the worktree made")
	assert.Equal(t, r.maker.dir, fences[0].Worktree)
	assert.Equal(t, r.agents.program, fences[0].Program, "its hook is the app's own executable")
	assert.FileExists(t, filepath.Join(plan.Folder, fence.PolicyFile), "the fence's policy is in the run's folder")

	noted := r.changes.noted()
	assert.Empty(t, noted, "a run that changed nothing notes nothing")

	run, err := r.history.Run(t.Context(), started.ScriptRunID)
	require.NoError(t, err)
	assert.Equal(t, scriptrun.KindAgent, run.Kind)
	assert.Equal(t, "Review · acme-org/example-repo #12", run.Title)
	assert.Equal(t, r.maker.dir, run.Dir)
}

func TestABackgroundStepWaitsForWhatHoldsIt(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		hold     *launcher.Hold
		planErr  error
		fenceErr error
		ensure   error
		wt       launcher.Worktree
		blocker  autopilot.Blocker
		key      string
		requeue  bool
	}{
		"a session in the worktree": {
			hold: &launcher.Hold{Reason: launcher.HoldRunning, Key: "running:x"}, blocker: autopilot.BlockBusyWorktree,
		},
		"agent files": {
			hold:    &launcher.Hold{Reason: launcher.HoldAgentFiles, Key: "agentFiles:" + agentHead, Head: agentHead},
			blocker: autopilot.BlockHold, key: "agentFiles:" + agentHead,
		},
		"files not all checked": {
			hold: &launcher.Hold{Reason: launcher.HoldUnchecked, Key: "unchecked:x"}, blocker: autopilot.BlockHold,
			key: "unchecked:x",
		},
		"a preset the agent can't keep": {planErr: launcher.ErrFenceNotKept, blocker: autopilot.BlockNotReady},
		"a push from a fork":            {fenceErr: fence.ErrCrossRepository, blocker: autopilot.BlockCrossRepo},
		"no clone":                      {planErr: localrepos.ErrCloneNotFound, blocker: autopilot.BlockNoClone},
		"the agent isn't installed":     {planErr: execpath.ErrNotFound, blocker: autopilot.BlockNotReady},
		"the head moved":                {ensure: worktrees.ErrHeadMoved, requeue: true},
		"a busy clone":                  {ensure: clonelock.ErrBusy, blocker: autopilot.BlockBusyWorktree},
		"the user's changes": {
			wt: launcher.Worktree{Dirty: 2, Head: agentHead}, blocker: autopilot.BlockDirty, key: "dirty:pr-12:2",
		},
		"a worktree off the head": {wt: launcher.Worktree{Head: strings.Repeat("a", 40)}, blocker: autopilot.BlockDirty},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := newAgentRig(t)
			r.planner.set(r.planner.plan(t, testutil.FakeRun{}))
			r.planner.hold, r.planner.err, r.planner.fenceErr, r.maker.err = tc.hold, tc.planErr, tc.fenceErr, tc.ensure

			if tc.wt.Head != "" {
				r.maker.wt = &tc.wt
			}

			res, err := r.agents.Dispatch(t.Context(), agentJob(9))
			require.NoError(t, err, "a hold is an answer, not a failure")
			assert.Equal(t, tc.blocker, res.Wait)
			assert.Equal(t, tc.requeue, res.Requeue)
			assert.NotEmpty(t, res.WaitWords)

			if tc.key == "" {
				assert.Nil(t, res.Hold, "nothing for the user to confirm")
			} else {
				require.NotNil(t, res.Hold)
				assert.Equal(t, tc.key, res.Hold.Key, "the user's Run confirms exactly this")
			}

			assert.False(t, res.Running, "nothing started")
			assert.Nil(t, res.Launched)
		})
	}
}

func TestATerminalStepWaitsForItsRunner(t *testing.T) {
	t.Parallel()

	r := newAgentRig(t)
	job := agentJob(3)
	job.Step.Agent.Where = catalog.WhereTerminal

	res, err := r.agents.Dispatch(t.Context(), job)
	require.NoError(t, err)
	assert.Equal(t, autopilot.BlockNotReady, res.Wait, "with no terminal runner, it isn't ready")

	r.agents.terminal = fakeTerminal{}
	res, err = r.agents.Dispatch(t.Context(), job)
	require.NoError(t, err)
	assert.Equal(t, "opened", res.Summary, "a step in the terminal goes to the terminal's runner")
}

// fakeTerminal is the terminal's runner, which opens every step at once.
type fakeTerminal struct{}

func (fakeTerminal) Dispatch(context.Context, autopilot.Job) (autopilot.Result, error) {
	return autopilot.Result{Sent: true, Summary: "opened"}, nil
}

func (fakeTerminal) Stop(context.Context, int64) error { return nil }

func (fakeTerminal) StopAll(context.Context) error { return nil }

func (fakeTerminal) Alive(context.Context, int64) (autopilot.Liveness, error) {
	return autopilot.LiveGone, nil
}

func TestABackgroundRunRestartsCleanly(t *testing.T) {
	t.Parallel()

	r := newAgentRig(t)
	success := agentLastLine(t, "success-structured.jsonl")
	plan := r.planner.plan(t, testutil.FakeRun{
		Stdout: agentLines(t, "success-structured.jsonl", 2), Sleep: time.Minute,
		OnInterrupt: &testutil.FakeInterrupt{Stdout: success},
	})
	r.planner.set(plan)

	_, err := r.agents.Dispatch(t.Context(), agentJob(11))
	require.NoError(t, err)

	var pid int

	require.Eventually(t, func() bool {
		folders := r.agents.RunFolders(t.Context())
		if len(folders) == 1 {
			pid = folders[0].PID
		}

		return pid > 0
	}, waitAgent, 10*time.Millisecond)

	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })

	// The app quits, letting the run finish: its agent keeps to its own budget.
	r.runs.LetFinish(true)
	r.runs.StopAll()

	// The next start follows it again, and hears how it ended.
	next := r.restart(t)

	require.Eventually(t, func() bool { return len(next.agents.RunFolders(t.Context())) == 1 }, waitAgent,
		10*time.Millisecond, "the orphan is followed again, its worktree still in use")

	live, err := next.agents.Alive(t.Context(), 11)
	require.NoError(t, err)
	assert.Equal(t, autopilot.LiveAlive, live)

	require.NoError(t, syscall.Kill(-pid, syscall.SIGINT))
	assert.Equal(t, autopilot.RunDone, next.engine.end(t, 11).Outcome, "its result is read from its log")
}

// agentRig is the background agents over a real run engine, with a fake launcher plan, a
// fake worktree maker, and a fake engine.
type agentRig struct {
	db      string
	logs    string
	folders string

	runs    *scriptrun.Engine
	history *scriptrun.Service
	agents  *backgroundAgents
	planner *fakePlanner
	maker   *fakeAgentMaker
	engine  *fakeRunEngine
	changes *fakeChangeNotes
}

// newAgentRig returns the background agents over a fresh database.
func newAgentRig(t *testing.T) *agentRig {
	t.Helper()

	dir := t.TempDir()
	r := &agentRig{
		db: filepath.Join(dir, "data.db"), logs: filepath.Join(dir, "script-runs"), folders: filepath.Join(dir, "agent-runs"),
		maker: &fakeAgentMaker{dir: testutil.RealTempDir(t)},
	}
	r.planner = &fakePlanner{folders: r.folders}
	require.NoError(t, os.Mkdir(r.folders, 0o700))

	r.build(t)

	return r
}

// build builds the run engine, the runner, and the background agents over the rig's data.
func (r *agentRig) build(t *testing.T) {
	t.Helper()

	r.runs = scriptrun.NewEngine(scriptrun.Config{
		DB: testutil.DBAt(t, r.db), Locks: clonelock.New(), LogDir: r.logs, Folders: r.folders,
	})
	r.history = scriptrun.NewService(r.runs)
	r.engine = &fakeRunEngine{ends: map[int64]autopilot.RunEnd{}}

	r.changes = &fakeChangeNotes{}
	r.agents = &backgroundAgents{
		launch: r.planner, maker: r.maker, shellEnv: func() ([]string, bool) { return os.Environ(), true },
		home: t.TempDir(), runs: r.runs, program: "/Applications/App.app/Contents/MacOS/app", changes: r.changes,
		requests: func(_ context.Context, run autopilot.RunRequest) (launcher.LaunchRequest, error) {
			return launcher.LaunchRequest{
				ItemID: run.ItemID, Routine: &launcher.RoutineLaunch{Name: run.Routine, Confirmed: run.Confirmed},
			}, nil
		},
	}
	r.agents.runner = agentrun.New(agentrun.Config{
		Engine: r.runs, Runs: r.history, Ended: r.agents.Ended,
		Sweeper: agentrun.ProcSweeper{Lister: procs.System{}},
	})
	r.agents.engine = func() runEngine { return r.engine }

	t.Cleanup(r.runs.StopAll)
}

// restart builds the app's runs again over the same data, as the next start does, and runs
// the run engine's worker, which follows what the last start left.
func (r *agentRig) restart(t *testing.T) *agentRig {
	t.Helper()

	next := &agentRig{db: r.db, logs: r.logs, folders: r.folders, planner: r.planner, maker: r.maker}
	next.build(t)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		next.runs.Run(ctx)
		close(done)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	return next
}

// agentJob is a Read only background step of the routine Review on pull request 12, as
// the ledger's run runID.
func agentJob(runID int64) autopilot.Job {
	return autopilot.Job{
		MatchID: 1, RunID: runID, RoutineID: 1, RoutineName: "Review", Head: agentHead,
		Item: autopilot.ItemRef{ID: 12, Kind: item.KindPR, Repo: "acme-org/example-repo", Number: 12, Title: "Bump zod"},
		Step: spec.Step{Agent: &spec.AgentStep{
			Prompt: spec.PromptRef{ItemDefault: true}, Fence: &spec.Fence{Tools: catalog.PresetReadOnly},
		}},
	}
}

// agentFixture returns an S1 stream fixture, as the background runner's suite keeps it.
func agentFixture(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("..", "agentrun", "testdata", name)) //nolint:gosec // the runner's own fixture
	require.NoError(t, err)

	return string(body)
}

// agentLines returns the first n lines of a fixture.
func agentLines(t *testing.T, name string, n int) string {
	t.Helper()

	return strings.Join(strings.SplitAfter(agentFixture(t, name), "\n")[:n], "")
}

// agentLastLine returns a fixture's last line.
func agentLastLine(t *testing.T, name string) string {
	t.Helper()

	all := strings.Split(strings.TrimSpace(agentFixture(t, name)), "\n")

	return all[len(all)-1] + "\n"
}

// fakePlanner plans every run as the plan it holds, or holds it back, or fails.
type fakePlanner struct {
	folders string

	mu       sync.Mutex
	bp       launcher.BackgroundPlan
	hold     *launcher.Hold
	err      error
	fenceErr error
	trusts   int
	fenced   []launcher.FenceIn
}

func (f *fakePlanner) Plan(_ context.Context, _ launcher.BackgroundRequest) (launcher.BackgroundPlan, *launcher.Hold,
	error,
) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.bp, f.hold, f.err
}

func (f *fakePlanner) Preview(context.Context, launcher.BackgroundRequest) (launcher.Preview, error) {
	return launcher.Preview{Command: "claude -p --restricted < prompt.md", Env: []string{}}, nil
}

func (f *fakePlanner) Made(_ launcher.BackgroundPlan, wt launcher.Worktree, routine *launcher.RoutineLaunch) *launcher.Hold {
	key := fmt.Sprintf("dirty:pr-12:%d", wt.Dirty)
	if wt.Dirty == 0 || slices.Contains(routine.Confirmed, key) {
		return nil
	}

	return &launcher.Hold{Reason: launcher.HoldDirty, Key: key, Dirty: wt.Dirty}
}

// Fenced works the Read only fence out for the worktree made, as the launcher does: the
// plan in the worktree, and the fence's files, which the runner's folder then holds.
func (f *fakePlanner) Fenced(bp launcher.BackgroundPlan, in launcher.FenceIn) (launcher.BackgroundPlan, agent.FencedRun,
	error,
) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.fenced = append(f.fenced, in)

	if f.fenceErr != nil {
		return bp, agent.FencedRun{}, f.fenceErr
	}

	built := fence.Fence{Policy: fence.Policy{
		Version: fence.PolicyVersion, Preset: fence.ReadOnly, Worktree: in.Worktree, Home: in.Worktree,
		GitDirs: []string{}, Tools: []string{}, Allow: []string{}, Deny: []string{}, ReadDeny: []string{},
	}}
	bp.Plan.Dir = in.Worktree

	return bp, agent.FencedRun{Fence: built, Note: built.AgentNote()}, nil
}

func (f *fakePlanner) Trust(context.Context, launcher.BackgroundPlan) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.trusts++

	return nil
}

// fences returns what each run's fence was worked out from.
func (f *fakePlanner) fences() []launcher.FenceIn {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.fenced)
}

// plan returns a background plan of a fake agent playing run.
func (f *fakePlanner) plan(t *testing.T, run testutil.FakeRun) launcher.BackgroundPlan {
	t.Helper()

	path, env := testutil.FakeTool(t, "claude", run)
	name, value, _ := strings.Cut(env, "=")
	sid := uuid.NewString()

	return launcher.BackgroundPlan{
		Plan:   launcher.Plan{Agent: agent.Claude, Binary: path, Env: map[string]string{name: value}},
		Prompt: "Review it.", SessionID: sid, Folder: filepath.Join(f.folders, sid), ReadOnly: true,
		Preset:   fence.ReadOnly,
		Command:  "claude -p < prompt.md",
		Worktree: launcher.WorktreeRequest{Kind: item.KindPR, Number: 12},
	}
}

// set has every plan be bp, its folder in the rig's.
func (f *fakePlanner) set(bp launcher.BackgroundPlan) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.bp = bp
}

func (f *fakePlanner) trusted() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.trusts
}

// fakeAgentMaker makes every worktree as dir, at the head asked for, or as wt, or fails.
type fakeAgentMaker struct {
	mu   sync.Mutex
	dir  string
	wt   *launcher.Worktree
	err  error
	reqs []launcher.WorktreeRequest
}

func (m *fakeAgentMaker) PlanWorktree(context.Context, launcher.WorktreeRequest) (launcher.Worktree, error) {
	return launcher.Worktree{Path: m.dir}, nil
}

func (m *fakeAgentMaker) EnsureWorktree(_ context.Context, req launcher.WorktreeRequest) (launcher.Worktree, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.reqs = append(m.reqs, req)

	if m.err != nil {
		return launcher.Worktree{}, m.err
	}

	if m.wt != nil {
		wt := *m.wt
		wt.Path = m.dir

		return wt, nil
	}

	return launcher.Worktree{Path: m.dir, Head: req.Head, Action: launcher.WorktreeCreate}, nil
}

func (m *fakeAgentMaker) asked() []launcher.WorktreeRequest {
	m.mu.Lock()
	defer m.mu.Unlock()

	return append([]launcher.WorktreeRequest(nil), m.reqs...)
}

// fakeRunEngine records what the runner told Autopilot's engine.
type fakeRunEngine struct {
	mu    sync.Mutex
	ends  map[int64]autopilot.RunEnd
	taken []int64
}

func (f *fakeRunEngine) RunUsage(int64, autopilot.RunUsage) {}

func (f *fakeRunEngine) TakeOver(_ context.Context, runID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.taken = append(f.taken, runID)

	return nil
}

func (f *fakeRunEngine) RunEnded(end autopilot.RunEnd) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.ends[end.RunID] = end
}

// takenOver returns the runs Take over told the engine of, in order.
func (f *fakeRunEngine) takenOver() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]int64{}, f.taken...)
}

// end waits for the run's end to be told.
func (f *fakeRunEngine) end(t *testing.T, runID int64) autopilot.RunEnd {
	t.Helper()

	var got autopilot.RunEnd

	require.Eventually(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()

		e, ok := f.ends[runID]
		got = e

		return ok
	}, waitAgent, 10*time.Millisecond, "run %d ends", runID)

	return got
}

func TestARunsChangesAreNotedOnItsItemAtBothHeads(t *testing.T) {
	t.Parallel()

	changes := &fakeChangeNotes{}
	b := &backgroundAgents{changes: changes}

	b.noteChanges(t.Context(), agentrun.End{
		Intent:  agentrun.Intent{RunID: 7, ItemID: 12, HeadBefore: agentHead},
		Outcome: agentrun.Outcome{Commit: strings.Repeat("b", 40), Changed: []string{".claude/settings.json", "main.go"}},
	})
	b.noteChanges(t.Context(), agentrun.End{Intent: agentrun.Intent{RunID: 8, ItemID: 12}, Outcome: agentrun.Outcome{}})

	assert.Equal(t, []notedChange{{
		item: 12, heads: []string{agentHead, strings.Repeat("b", 40)}, files: []string{".claude/settings.json", "main.go"},
		run: 7,
	}}, changes.noted(), "the next launch at either head holds for the agent file it wrote; a run that changed nothing notes nothing")
}

// notedChange is one NoteAgentChanges call.
type notedChange struct {
	item  int64
	heads []string
	files []string
	run   int64
}

// fakeChangeNotes records what agent runs changed (inbox.Store.NoteAgentChanges).
type fakeChangeNotes struct {
	mu    sync.Mutex
	calls []notedChange
}

func (f *fakeChangeNotes) NoteAgentChanges(_ context.Context, itemID int64, heads, files []string, runID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, notedChange{item: itemID, heads: heads, files: files, run: runID})

	return nil
}

func (f *fakeChangeNotes) noted() []notedChange {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.calls)
}
