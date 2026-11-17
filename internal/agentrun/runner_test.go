package agentrun

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// The conformance suite: every agent that claims background runs passes it, a fake binary
// replaying that agent's recorded stream. Claude Code's fixtures are S1's.

// waitFor bounds how long a test waits for a run.
const waitFor = 20 * time.Second

// errDirty is what a plan's Before refuses a worktree with changes with.
var errDirty = errors.New("the worktree has changes")

// rig is a runner over a real run engine and a fresh database.
type rig struct {
	t        *testing.T
	db       *sql.DB
	logs     string
	folders  string
	engine   *scriptrun.Engine
	service  *scriptrun.Service
	runner   *Runner
	sessions *fakeSessions
	sweeper  *fakeSweeper

	mu     sync.Mutex
	ends   map[int64]End
	nextID atomic.Int64
}

// newRig returns a runner over a fresh database, its engine stopped when the test ends.
func newRig(t *testing.T) *rig {
	t.Helper()

	r := &rig{
		t: t, db: testutil.DB(t), logs: filepath.Join(t.TempDir(), "script-runs"), folders: t.TempDir(),
		sessions: &fakeSessions{states: map[string][]SessionState{}}, sweeper: &fakeSweeper{}, ends: map[int64]End{},
	}

	r.engine, r.service, r.runner = r.build()

	t.Cleanup(r.engine.StopAll)

	return r
}

// build returns a run engine, its service, and a runner over the rig's database.
func (r *rig) build() (*scriptrun.Engine, *scriptrun.Service, *Runner) {
	engine := scriptrun.NewEngine(scriptrun.Config{
		DB: r.db, Locks: clonelock.New(), LogDir: r.logs, Folders: r.folders,
	})
	service := scriptrun.NewService(engine)
	runner := New(Config{
		Engine: engine, Runs: service, Sessions: r.sessions, Sweeper: r.sweeper,
		Ended: func(_ context.Context, end End) {
			r.mu.Lock()
			defer r.mu.Unlock()

			r.ends[end.Intent.RunID] = end
		},
	})

	return engine, service, runner
}

// plan returns a background run of a fake agent playing run in a fresh worktree.
func (r *rig) plan(run testutil.FakeRun) Plan {
	r.t.Helper()

	path, env := testutil.FakeTool(r.t, "claude", run)
	sid := uuid.NewString()

	return Plan{
		RunID: r.nextID.Add(1), MatchID: 1, RoutineID: 1, Title: "Review · acme-org/sdk #12", Source: "autopilot:1",
		Agent: agent.Claude, SessionID: sid, Repo: repo.Name{Owner: "acme-org", Repo: "sdk"}, Dir: testutil.RealTempDir(r.t),
		Argv: []string{path}, Env: append(os.Environ(), env), Folder: FolderFor(r.folders, sid), Prompt: "Review it.",
		Fence:   Fence{Preset: "readOnly", Timeout: 30 * time.Second, BudgetUSD: 0.5, ReadOnly: true},
		Session: Session{Agent: agent.Claude, Prompt: "Review", ItemID: 12, Kind: item.KindPR, Number: 12},
	}
}

// start starts plan, failing the test when it can't.
func (r *rig) start(plan Plan) int64 {
	r.t.Helper()

	id, err := r.runner.Start(r.t.Context(), plan)
	require.NoError(r.t, err)

	return id
}

// end waits for the ledger's run's end to be told, and returns it.
func (r *rig) end(runID int64) End {
	r.t.Helper()

	var got End

	require.Eventually(r.t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()

		end, ok := r.ends[runID]
		got = end

		return ok
	}, waitFor, 10*time.Millisecond, "run %d ends", runID)

	return got
}

// record returns the run's record in the history.
func (r *rig) record(id int64) scriptrun.Run {
	r.t.Helper()

	run, err := r.service.Run(r.t.Context(), id)
	require.NoError(r.t, err)

	return run
}

// lines returns the first n lines of a fixture.
func lines(t *testing.T, name string, n int) string {
	t.Helper()

	all := strings.SplitAfter(string(fixture(t, name)), "\n")

	return strings.Join(all[:n], "")
}

// lastLine returns a fixture's last line.
func lastLine(t *testing.T, name string) string {
	t.Helper()

	all := strings.Split(strings.TrimSpace(string(fixture(t, name))), "\n")

	return all[len(all)-1] + "\n"
}

func TestConformanceSuccess(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	got := filepath.Join(t.TempDir(), "stdin")
	plan := r.plan(testutil.FakeRun{Stdout: string(fixture(t, "success-structured.jsonl")), StdinTo: got})

	id := r.start(plan)
	end := r.end(plan.RunID)

	assert.Equal(t, StatusDone, end.Outcome.Status)
	assert.Equal(t, id, end.ScriptRunID)
	require.NotNil(t, end.Outcome.Report)
	assert.Equal(t, "nothing_to_do", end.Outcome.Report.Outcome)
	assert.True(t, strings.HasPrefix(end.Outcome.Summary, "Finished: Add(a, b int)"), end.Outcome.Summary)
	require.NotNil(t, end.Outcome.Result)
	assert.Equal(t, []string{"claude-haiku-5-5"}, end.Outcome.Result.Models)
	assert.Positive(t, end.Outcome.TokensOut)

	rec := r.record(id)
	assert.Equal(t, scriptrun.StatusSucceeded, rec.Status)
	assert.Equal(t, scriptrun.KindAgent, rec.Kind)
	assert.NotContains(t, strings.Join(rec.Argv, " "), "Review it.", "the prompt is never on the command line")

	body, err := os.ReadFile(got) //nolint:gosec // the test's own temp file
	require.NoError(t, err)
	assert.Equal(t, "Review it.", string(body), "the prompt came on stdin")

	assertPrivate(t, plan.Folder, 0o700)
	assertPrivate(t, plan.PromptPath(), 0o600)

	assert.Equal(t, []SessionState{SessionWorking, SessionEnded}, r.sessions.of(plan.SessionID),
		"its session works, then ends, as its run says")

	live, err := r.runner.Alive(t.Context(), plan.RunID)
	require.NoError(t, err)
	assert.Equal(t, LiveGone, live)

	page, err := r.runner.Events(t.Context(), plan.RunID, 0)
	require.NoError(t, err)
	assert.True(t, page.Done)
	assert.NotEmpty(t, page.Events, "an ended run's events are read from its log")
}

func TestConformanceAnErrorIsJudgedByTheResultNotTheExitCode(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.plan(testutil.FakeRun{Stdout: string(fixture(t, "sigint.jsonl")), Exit: 0})

	r.start(plan)
	end := r.end(plan.RunID)

	assert.Equal(t, StatusFailed, end.Outcome.Status, "exit 0 with a result in error is a failure")
	assert.Equal(t, "It failed: Aborted tools.", end.Outcome.Summary)
	assert.Equal(t, []SessionState{SessionWorking, SessionFailed}, r.sessions.of(plan.SessionID))
}

func TestConformanceTheBudgetReached(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.plan(testutil.FakeRun{Stdout: string(fixture(t, "budget-exhausted.jsonl")), Exit: 1})

	r.start(plan)
	end := r.end(plan.RunID)

	assert.Equal(t, StatusOutOfBudget, end.Outcome.Status)
	assert.Equal(t, "Out of budget ($0.50).", end.Outcome.Summary)
}

func TestConformanceACrashMidRun(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.plan(testutil.FakeRun{Stdout: lines(t, "background-task.jsonl", 13), Exit: 1})

	r.start(plan)
	end := r.end(plan.RunID)

	assert.Equal(t, StatusFailed, end.Outcome.Status)
	assert.Nil(t, end.Outcome.Report)
	assert.Contains(t, end.Outcome.NoReport, "The agent didn't report. It last said:")
	assert.Equal(t, scriptrun.StatusFailed, r.record(end.ScriptRunID).Status)
}

func TestConformanceAHangUntilTheTimeLimit(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.plan(testutil.FakeRun{Stdout: lines(t, "success-structured.jsonl", 3), Sleep: time.Minute})
	plan.Fence.Timeout = time.Second

	r.start(plan)
	end := r.end(plan.RunID)

	assert.Equal(t, StatusTimedOut, end.Outcome.Status)
	assert.Equal(t, "Stopped after 1s.", end.Outcome.Summary)
}

func TestConformanceAResultEndsARunWhoseShellLingers(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.plan(testutil.FakeRun{Stdout: string(fixture(t, "background-task.jsonl")), Sleep: time.Minute})

	r.start(plan)
	end := r.end(plan.RunID)

	assert.Equal(t, StatusDone, end.Outcome.Status, "the result line is the end; what lingers is stopped")
}

func TestConformanceOutOfUsage(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	rejected := `{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":1795071600,` +
		`"rateLimitType":"five_hour"}}` + "\n"
	result := `{"type":"result","subtype":"success","is_error":true,"result":"You've hit your session limit",` +
		`"num_turns":1}` + "\n"
	plan := r.plan(testutil.FakeRun{Stdout: lines(t, "success-structured.jsonl", 1) + rejected + result, Exit: 1})

	r.start(plan)
	end := r.end(plan.RunID)

	assert.Equal(t, StatusOutOfUsage, end.Outcome.Status, "shown as out of usage, not a failure")
	require.NotNil(t, end.Outcome.Limit)
	assert.Equal(t, Limit{Status: "rejected", Type: "five_hour", ResetsAt: 1795071600}, *end.Outcome.Limit)
	assert.True(t, strings.HasPrefix(end.Outcome.Summary, "Out of usage: its plan's five-hour limit is reached. It resets "),
		end.Outcome.Summary)
}

func TestConformanceATokenBudget(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	heavy := `{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"Working"}],` +
		`"usage":{"input_tokens":900,"output_tokens":200}}}` + "\n"
	plan := r.plan(testutil.FakeRun{Stdout: lines(t, "success-structured.jsonl", 1) + heavy, Sleep: time.Minute})
	plan.Fence.BudgetUSD, plan.Fence.BudgetTokens = 0, 1000

	r.start(plan)
	end := r.end(plan.RunID)

	assert.Equal(t, StatusOutOfBudget, end.Outcome.Status, "the runner stops a run at its budget in tokens")
	assert.Equal(t, "Out of budget (1000 tokens).", end.Outcome.Summary)
	assert.Equal(t, int64(900), end.Outcome.TokensIn)
}

func TestStopStopsARunAndStopAllEvery(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	ready := lines(t, "success-structured.jsonl", 2)

	one, two, three := r.plan(testutil.FakeRun{Stdout: ready, Sleep: time.Minute}),
		r.plan(testutil.FakeRun{Stdout: ready, Sleep: time.Minute}), r.plan(testutil.FakeRun{Stdout: ready, Sleep: time.Minute})

	for _, p := range []Plan{one, two, three} {
		r.start(p)
	}

	require.Eventually(t, func() bool { return len(r.runner.Folders()) == 3 }, waitFor, 10*time.Millisecond)

	alive, err := r.runner.Alive(t.Context(), one.RunID)
	require.NoError(t, err)
	assert.Equal(t, LiveAlive, alive)

	require.NoError(t, r.runner.Stop(t.Context(), one.RunID))
	assert.Equal(t, StatusStopped, r.end(one.RunID).Outcome.Status)

	require.NoError(t, r.runner.StopAll(t.Context()))
	assert.Equal(t, StatusStopped, r.end(two.RunID).Outcome.Status)
	assert.Equal(t, StatusStopped, r.end(three.RunID).Outcome.Status)

	after := r.plan(testutil.FakeRun{Stdout: string(fixture(t, "success-structured.jsonl"))})
	r.start(after)
	assert.Equal(t, StatusDone, r.end(after.RunID).Outcome.Status, "a run starts after Stop all")
}

func TestARunInTheWorktreeShowsAmongTheFolders(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.plan(testutil.FakeRun{Stdout: lines(t, "success-structured.jsonl", 2), Sleep: time.Minute})
	id := r.start(plan)

	require.Eventually(t, func() bool { return len(r.runner.Folders()) == 1 }, waitFor, 10*time.Millisecond)

	f := r.runner.Folders()[0]
	assert.Equal(t, id, f.RunID)
	assert.Equal(t, plan.Dir, f.Dir)
	assert.Positive(t, f.PID)

	require.Eventually(t, func() bool {
		progress, ok := r.runner.Progress(plan.RunID)

		return ok && strings.HasPrefix(progress.Step, "Reading ") && strings.HasSuffix(progress.Step, "main.go")
	}, waitFor, 10*time.Millisecond, "its latest step, as its stream says")

	require.NoError(t, r.runner.Stop(t.Context(), plan.RunID))
	r.end(plan.RunID)
	assert.Empty(t, r.runner.Folders())
}

func TestTwoItemsOfOneCloneRunSideBySide(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	clone := testutil.RealTempDir(t)
	first, second := r.plan(testutil.FakeRun{Stdout: lines(t, "success-structured.jsonl", 2), Sleep: time.Minute}),
		r.plan(testutil.FakeRun{Stdout: lines(t, "success-structured.jsonl", 2), Sleep: time.Minute})
	first.Dir, second.Dir = filepath.Join(clone, "pr-1"), filepath.Join(clone, "pr-2")

	for _, dir := range []string{first.Dir, second.Dir} {
		require.NoError(t, os.Mkdir(dir, 0o700))
	}

	r.start(first)
	r.start(second)

	require.Eventually(t, func() bool { return len(r.runner.Folders()) == 2 }, waitFor, 10*time.Millisecond,
		"each holds its own worktree, never the clone")

	require.NoError(t, r.runner.StopAll(t.Context()))
	r.end(first.RunID)
	r.end(second.RunID)
}

func TestBeforeRefusesARunThatCantStart(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.plan(testutil.FakeRun{})
	plan.Before = func(context.Context) error { return errDirty }

	_, err := r.runner.Start(t.Context(), plan)
	require.ErrorIs(t, err, errDirty)

	assert.NoDirExists(t, plan.Folder, "nothing is written once Before refuses")
	assert.Empty(t, r.sessions.of(plan.SessionID))

	live, err := r.runner.Alive(t.Context(), plan.RunID)
	require.NoError(t, err)
	assert.Equal(t, LiveNever, live, "a run that never started is never in the history")
}

func TestReconcileFollowsAnOrphanAgainAndJudgesTheEnded(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	success := lastLine(t, "success-structured.jsonl")
	partial := lines(t, "success-structured.jsonl", 2)
	onInt := &testutil.FakeInterrupt{Stdout: success}

	alive := r.plan(testutil.FakeRun{Stdout: partial, Sleep: time.Minute, OnInterrupt: onInt})
	exited := r.plan(testutil.FakeRun{Stdout: partial, Sleep: time.Minute, OnInterrupt: onInt})
	gone := r.plan(testutil.FakeRun{Stdout: partial, Sleep: time.Minute, OnInterrupt: onInt})
	late := r.plan(testutil.FakeRun{Stdout: partial, Sleep: time.Minute, OnInterrupt: onInt})
	late.Fence.Timeout = 2 * time.Second

	for _, p := range []Plan{alive, exited, gone, late} {
		r.start(p)
	}

	require.Eventually(t, func() bool { return len(r.runner.Folders()) == 4 }, waitFor, 10*time.Millisecond)

	pids := map[string]int{}
	for _, f := range r.runner.Folders() {
		pids[f.Dir] = f.PID
	}

	// The fake answers SIGINT only once it is ready, and it writes its first lines after
	// that: a signal sent sooner ends it with no result, as a real agent killed at its start
	// would be, so the one that is to finish is signaled only once its lines are read.
	require.Eventually(t, func() bool {
		page, err := r.runner.Events(t.Context(), exited.RunID, 0)

		return err == nil && len(page.Events) > 0
	}, waitFor, 10*time.Millisecond, "the run that is to finish wrote its first lines")

	// The app quits, letting the runs finish: each keeps to a budget of its own.
	r.engine.LetFinish(true)
	r.engine.StopAll()

	t.Cleanup(func() {
		for _, pid := range pids {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	})

	// While the app is away: one finishes, writing its result; one is killed without one.
	require.NoError(t, syscall.Kill(-pids[exited.Dir], syscall.SIGINT))
	require.NoError(t, syscall.Kill(-pids[gone.Dir], syscall.SIGKILL))
	require.Eventually(t, func() bool {
		return !procs.Alive(pids[exited.Dir]) && !procs.Alive(pids[gone.Dir])
	}, waitFor, 10*time.Millisecond)
	time.Sleep(2 * time.Second) // past late's time limit

	// The next start follows them again.
	r.mu.Lock()
	clear(r.ends)
	r.mu.Unlock()

	engine, _, runner := r.build()
	r.engine, r.runner = engine, runner

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		engine.Run(ctx)
		close(done)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	assert.Equal(t, StatusDone, r.end(exited.RunID).Outcome.Status, "an exited run's result is read from its log")
	assert.Equal(t, StatusInterrupted, r.end(gone.RunID).Outcome.Status, "a gone one, without a result, is interrupted")
	assert.Equal(t, StatusTimedOut, r.end(late.RunID).Outcome.Status, "an orphan past its time limit is stopped at once")

	require.Eventually(t, func() bool {
		live, err := runner.Alive(t.Context(), alive.RunID)

		return err == nil && live == LiveAlive
	}, waitFor, 10*time.Millisecond, "an orphan still running is followed again")

	page, err := runner.Events(t.Context(), alive.RunID, 0)
	require.NoError(t, err)
	assert.NotEmpty(t, page.Events, "its log is read again from its start")

	require.NoError(t, runner.Stop(t.Context(), alive.RunID))
	assert.Equal(t, StatusStopped, r.end(alive.RunID).Outcome.Status)
}

func TestAKilledRunsLeftoversAreSwept(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	rn := &run{intent: Intent{Fence: Fence{Timeout: time.Minute}}, dir: "/srv/wt/pr-1", parser: newClaudeParser("")}

	r.runner.finish(t.Context(), rn, scriptrun.Ending{Stopped: true})
	assert.Empty(t, r.sweeper.swept(), "SIGINT ends what the agent started in the background")

	r.runner.finish(t.Context(), rn, scriptrun.Ending{Stopped: true, Killed: true})
	assert.Equal(t, []string{"/srv/wt/pr-1"}, r.sweeper.swept(), "after SIGKILL, what is left in the worktree goes")
}

func TestProcSweeperEndsAnEscapedShellInTheWorktree(t *testing.T) {
	t.Parallel()

	if _, err := (procs.System{}).Processes(t.Context()); err != nil {
		t.Skip("the processes can't be listed here")
	}

	worktree := testutil.RealTempDir(t)
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	path, env := testutil.FakeTool(t, "claude", testutil.FakeRun{Grandchild: &testutil.FakeGrandchild{
		PIDFile: pidFile, Sleep: time.Minute, Setsid: true,
	}})

	proc, err := execpath.Start(t.Context(), execpath.Cmd{Path: path, Dir: worktree, Env: append(os.Environ(), env)})
	require.NoError(t, err)

	_, _ = proc.Wait()

	var escaped int

	require.Eventually(t, func() bool {
		body, readErr := os.ReadFile(pidFile) //nolint:gosec // the test's own temp file
		if readErr != nil {
			return false
		}

		pid, scanErr := strconv.Atoi(strings.TrimSpace(string(body)))
		escaped = pid

		return scanErr == nil && escaped > 0
	}, waitFor, 10*time.Millisecond)

	t.Cleanup(func() { _ = syscall.Kill(escaped, syscall.SIGKILL) })
	require.True(t, procs.Alive(escaped), "the shell left the group, and outlived it")

	ProcSweeper{Lister: procs.System{}}.Sweep(t.Context(), worktree)

	require.Eventually(t, func() bool { return !procs.Alive(escaped) }, waitFor, 10*time.Millisecond)
}

// assertPrivate checks path's permissions.
func assertPrivate(t *testing.T, path string, mode os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, mode, info.Mode().Perm(), path)
}

// fakeSessions records what the runner told of each session.
type fakeSessions struct {
	mu      sync.Mutex
	started []StartedSession
	states  map[string][]SessionState
}

func (f *fakeSessions) Started(_ context.Context, s StartedSession) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.started = append(f.started, s)
	f.states[s.ID] = append(f.states[s.ID], SessionWorking)

	return nil
}

func (f *fakeSessions) State(_ context.Context, id string, u SessionUpdate) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.states[id] = append(f.states[id], u.State)

	return nil
}

// of returns what the session was told, in order.
func (f *fakeSessions) of(id string) []SessionState {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]SessionState(nil), f.states[id]...)
}

// fakeSweeper records the folders it was asked to sweep.
type fakeSweeper struct {
	mu   sync.Mutex
	dirs []string
}

func (f *fakeSweeper) Sweep(_ context.Context, dir string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.dirs = append(f.dirs, dir)
}

func (f *fakeSweeper) swept() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.dirs...)
}
