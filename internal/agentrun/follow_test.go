package agentrun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// errLedger is what a ledger that can't record a session fails with.
var errLedger = errors.New("the ledger is read-only")

// errGitRead is what a worktree git can't read fails with.
var errGitRead = errors.New("not a git repository")

// followed returns the run the runner follows for the ledger's run.
func followed(t *testing.T, r *Runner, runID int64) *run {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()

	rn := r.byLedger[runID]
	require.NotNil(t, rn, "the runner follows run %d", runID)

	return rn
}

// awakeOf returns the caffeinate that keeps the Mac awake for rn, or nil.
func awakeOf(rn *run) *execpath.Proc {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	return rn.awake
}

func TestKeepAwakeHoldsTheMacAwakeWhileTheRunGoes(t *testing.T) {
	// The fake caffeinate is started with the app's own environment, so the test sets its
	// entry there, which no parallel test may do.
	path, env := testutil.FakeTool(t, "caffeinate", testutil.FakeRun{Sleep: time.Minute})
	name, value, _ := strings.Cut(env, "=")
	t.Setenv(name, value)

	r := newRig(t)
	logs := &testutil.Logs{}
	wanted := true
	r.runner.keepAwake = func(context.Context) bool { return wanted }
	r.runner.caffeinate, r.runner.logger = path, logs.Logger()

	plan := r.plan(testutil.FakeRun{Stdout: lines(t, "success-structured.jsonl", 2), Sleep: time.Minute})
	r.start(plan)

	rn := followed(t, r.runner, plan.RunID)
	awake := awakeOf(rn)
	require.NotNil(t, awake, "caffeinate starts beside the run")

	require.NoError(t, r.runner.Stop(t.Context(), plan.RunID))
	r.end(plan.RunID)

	select {
	case <-awake.Done():
	case <-time.After(waitFor):
		require.FailNow(t, "caffeinate outlived the run")
	}

	code, _ := awake.Wait()
	assert.Equal(t, 128+int(syscall.SIGTERM), code, "it ends as the run's end is told")
	assert.Nil(t, awakeOf(rn))

	wanted = false
	off := r.plan(testutil.FakeRun{Stdout: lines(t, "success-structured.jsonl", 2), Sleep: time.Minute})
	r.start(off)
	assert.Nil(t, awakeOf(followed(t, r.runner, off.RunID)), "nothing keeps the Mac awake once the user turns it off")
	require.NoError(t, r.runner.Stop(t.Context(), off.RunID))
	r.end(off.RunID)

	wanted, r.runner.caffeinate = true, filepath.Join(t.TempDir(), "caffeinate")
	missing := r.plan(testutil.FakeRun{Stdout: string(fixture(t, "success-structured.jsonl"))})
	r.start(missing)
	assert.Equal(t, StatusDone, r.end(missing.RunID).Outcome.Status, "a caffeinate that can't start holds no run back")
	assert.Contains(t, logs.String(), "could not keep the Mac awake for an agent's run")
}

func TestAnEndHeardBeforeTheSessionIsRecordedIsToldOnceItIs(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.plan(testutil.FakeRun{})
	rn := newRun(intentOf(plan), plan.Dir, newClaudeParser(plan.Dir))

	// A run can end before its start has recorded its session.
	r.runner.setSession(t.Context(), rn, SessionUpdate{State: SessionEnded, LastMessage: "Finished."})
	assert.Empty(t, r.sessions.of(plan.SessionID), "nothing is told of a session not yet recorded")

	r.runner.recordSession(t.Context(), plan, rn, 7)

	assert.Equal(t, []SessionState{SessionWorking, SessionEnded}, r.sessions.of(plan.SessionID),
		"its end is told once the session is there")

	r.sessions.mu.Lock()
	started := r.sessions.started[0]
	r.sessions.mu.Unlock()

	assert.Equal(t, int64(7), started.RunID)
	assert.Equal(t, plan.SessionID, started.AgentSessionID, "Claude Code takes the app's ID for its conversation")
	assert.Equal(t, plan.Prompt, started.PromptHead)
	assert.Equal(t, plan.Dir, started.Dir)
}

// failingSessions is a ledger that records no session.
type failingSessions struct{}

func (failingSessions) Started(context.Context, StartedSession) error      { return errLedger }
func (failingSessions) State(context.Context, string, SessionUpdate) error { return errLedger }

func TestASessionTheLedgerCantRecordHoldsNoRunBack(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	logs := &testutil.Logs{}
	r.runner.sessions, r.runner.logger = failingSessions{}, logs.Logger()

	plan := r.plan(testutil.FakeRun{Stdout: string(fixture(t, "success-structured.jsonl"))})
	r.start(plan)

	assert.Equal(t, StatusDone, r.end(plan.RunID).Outcome.Status)
	assert.Contains(t, logs.String(), "could not record an agent's run as a session")
	assert.Contains(t, logs.String(), "could not record what an agent's run does")
}

func TestARunsStreamTellsWhatChangedAndItsTokensAtMostEveryUsageEvery(t *testing.T) {
	t.Parallel()

	clk := testutil.NewClock(time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC))

	var (
		mu      sync.Mutex
		changed []int64
		usages  []Usage
	)

	runner := New(Config{
		Clock: clk,
		Changed: func(id int64) {
			mu.Lock()
			defer mu.Unlock()

			changed = append(changed, id)
		},
		Usage: func(_ context.Context, u Usage) {
			mu.Lock()
			defer mu.Unlock()

			usages = append(usages, u)
		},
	})

	f := feed{runner: runner, rn: newRun(Intent{RunID: 7}, "", newClaudeParser(""))}
	said := func(id string, in, out int64) []byte {
		return []byte(`{"type":"assistant","message":{"id":"` + id + `","content":[{"type":"text","text":"Working"}],` +
			`"usage":{"input_tokens":` + jsonInt(in) + `,"output_tokens":` + jsonInt(out) + `,"cache_read_input_tokens":1}}}` + "\n")
	}

	_, err := f.Write(said("m1", 10, 3))
	require.NoError(t, err)

	_, err = f.Write(said("m2", 5, 2))
	require.NoError(t, err)

	clk.Advance(UsageEvery)

	_, err = f.Write(said("m3", 1, 1))
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, []int64{7, 7, 7}, changed, "each part read tells the window the run's events changed")
	assert.Equal(t, []Usage{
		{RunID: 7, TokensIn: 10, TokensOut: 3, CacheRead: 1},
		{RunID: 7, TokensIn: 16, TokensOut: 6, CacheRead: 3},
	}, usages, "its first tokens at once, then at most every UsageEvery")
}

// limitsFunc is a LimitReader from a function.
type limitsFunc func() (Limit, bool)

func (f limitsFunc) Limit(context.Context, agent.ID, string, string) (Limit, bool) { return f() }

func TestTheAgentsRecordSaysWhichLimitEndedARun(t *testing.T) {
	t.Parallel()

	refused := `{"type":"result","subtype":"success","is_error":true,"result":"You've hit your limit"}` + "\n"
	rejected := `{"type":"rate_limit_event","rate_limit_info":{"status":"rejected"}}` + "\n"
	weekly := Limit{Status: "rejected", Type: "seven_day", ResetsAt: 1795071600}

	for name, tc := range map[string]struct {
		stream string
		record limitsFunc
		status Status
		limit  *Limit
	}{
		"the record names the limit the stream never mentioned": {
			stream: refused, record: func() (Limit, bool) { return weekly, true }, status: StatusOutOfUsage, limit: &weekly,
		},
		"a limit the record says still allows is no reason": {
			stream: refused, record: func() (Limit, bool) { return Limit{Status: "allowed", Type: "seven_day"}, true },
			status: StatusFailed,
		},
		"the record fills in which limit refused, and when it resets": {
			stream: rejected + refused, record: func() (Limit, bool) { return weekly, true }, status: StatusOutOfUsage,
			limit: &weekly,
		},
		"a record that can't be read leaves what the stream said": {
			stream: rejected + refused, record: func() (Limit, bool) { return Limit{}, false }, status: StatusOutOfUsage,
			limit: &Limit{Status: "rejected"},
		},
	} {
		runner := New(Config{Limits: tc.record})
		rn := newRun(Intent{Agent: agent.Claude, Fence: Fence{Timeout: time.Minute}}, "", newClaudeParser(""))
		_, _ = rn.parser.Write([]byte(tc.stream))

		got, ok := runner.finish(t.Context(), rn, scriptrun.Ending{}).Detail.(Outcome)
		require.True(t, ok, name)
		assert.Equal(t, tc.status, got.Status, name)
		assert.Equal(t, tc.limit, got.Limit, name)
	}
}

// worktreeGit reads a worktree's head, the files changed since, and its diff, or fails to.
type worktreeGit struct {
	head       string
	headErr    error
	changed    []string
	changedErr error
	diffErr    error

	mu    sync.Mutex
	asked []string
}

func (g *worktreeGit) Head(_ context.Context, dir string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.asked = append(g.asked, "head "+dir)

	return g.head, g.headErr
}

func (g *worktreeGit) ChangedFiles(_ context.Context, dir, from, to string) ([]string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.asked = append(g.asked, "changed "+dir+" "+from+".."+to)

	return g.changed, g.changedErr
}

func (g *worktreeGit) WorktreeDiff(context.Context, string, string) (gitread.WorktreeDiff, error) {
	return gitread.WorktreeDiff{}, g.diffErr
}

// reads returns what the run's end read of the worktree, in order.
func (g *worktreeGit) reads() []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	return append([]string{}, g.asked...)
}

func TestARunsCommitsAreReadFromItsWorktreeAsItEnds(t *testing.T) {
	t.Parallel()

	const (
		dir    = "/srv/wt/pr-12"
		before = "1111111111111111111111111111111111111111"
		after  = "2222222222222222222222222222222222222222"
	)

	success := lastLine(t, "success-structured.jsonl")

	for name, tc := range map[string]struct {
		git        *worktreeGit
		headBefore string
		commit     string
		changed    []string
		reads      []string
	}{
		"its new commits and the files they changed": {
			git: &worktreeGit{head: after, changed: []string{"add.go", "add_test.go"}}, headBefore: before, commit: after,
			changed: []string{"add.go", "add_test.go"}, reads: []string{"head " + dir, "changed " + dir + " " + before + ".." + after},
		},
		"no commit of its own": {
			git: &worktreeGit{head: before}, headBefore: before, commit: before, changed: []string{},
			reads: []string{"head " + dir},
		},
		"a run that didn't know where it started": {
			git: &worktreeGit{head: after}, commit: after, changed: []string{}, reads: []string{"head " + dir},
		},
		"a head that can't be read": {
			git: &worktreeGit{headErr: errGitRead}, headBefore: before, changed: []string{}, reads: []string{"head " + dir},
		},
		"changes that can't be read": {
			git: &worktreeGit{head: after, changedErr: errGitRead}, headBefore: before, commit: after, changed: []string{},
			reads: []string{"head " + dir, "changed " + dir + " " + before + ".." + after},
		},
	} {
		logs := &testutil.Logs{}
		runner := New(Config{Git: tc.git, Logger: logs.Logger()})
		rn := newRun(Intent{HeadBefore: tc.headBefore, Fence: Fence{Timeout: time.Minute}}, dir, newClaudeParser(dir))
		_, _ = rn.parser.Write([]byte(success))

		got, ok := runner.finish(t.Context(), rn, scriptrun.Ending{}).Detail.(Outcome)
		require.True(t, ok, name)
		assert.Equal(t, StatusDone, got.Status, name)
		assert.Equal(t, tc.commit, got.Commit, name)
		assert.Equal(t, tc.changed, got.Changed, name)
		assert.Equal(t, tc.reads, tc.git.reads(), name)

		if tc.git.headErr != nil || tc.git.changedErr != nil {
			assert.Contains(t, logs.String(), "could not read", "%s: the run's end says what it couldn't read", name)
		}
	}

	git := &worktreeGit{head: after}
	rn := newRun(Intent{HeadBefore: before, Fence: Fence{Timeout: time.Minute}}, dir, newClaudeParser(dir))

	got, ok := New(Config{Git: git}).finish(t.Context(), rn, scriptrun.Ending{Interrupted: "The Mac shut down."}).
		Detail.(Outcome)
	require.True(t, ok)
	assert.Equal(t, StatusInterrupted, got.Status)
	assert.Empty(t, git.reads(), "an interrupted run's worktree may be mid-change, so nothing is read of it")

	got, ok = New(Config{}).finish(t.Context(), rn, scriptrun.Ending{}).Detail.(Outcome)
	require.True(t, ok)
	assert.Empty(t, got.Commit, "without git, nothing is read")
	assert.Equal(t, []string{}, got.Changed)
}

func TestDiffNeedsGitAndPassesOnWhatItCouldntRead(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	_, err := r.runner.Diff(t.Context(), 1)
	require.ErrorIs(t, err, ErrNoGit, "a runner without git has no diff to show")

	r.runner.git = &worktreeGit{head: strings.Repeat("b", 40), diffErr: errGitRead}

	plan := r.plan(testutil.FakeRun{Stdout: string(fixture(t, "success-structured.jsonl"))})
	plan.HeadBefore = strings.Repeat("a", 40)
	r.start(plan)
	r.end(plan.RunID)

	_, err = r.runner.Diff(t.Context(), plan.RunID)
	require.ErrorIs(t, err, errGitRead)
}

// listerFunc is a process table from a function.
type listerFunc func() ([]procs.Process, error)

func (f listerFunc) Processes(context.Context) ([]procs.Process, error) { return f() }

func TestTheSweeperSparesTheAppAndWhatItCantList(t *testing.T) {
	t.Parallel()

	worktree := testutil.RealTempDir(t)
	logs := &testutil.Logs{}

	ProcSweeper{
		Lister: listerFunc(func() ([]procs.Process, error) { return nil, procs.ErrUnsupported }), Logger: logs.Logger(),
	}.Sweep(t.Context(), worktree)
	assert.Contains(t, logs.String(), "could not list what an agent's run left running")

	// Were the app's own process not spared, this test would end here.
	ProcSweeper{Lister: listerFunc(func() ([]procs.Process, error) {
		return []procs.Process{{PID: os.Getpid(), Dir: worktree}, {PID: procs.MaxPID + 1, Dir: worktree}}, nil
	}), Logger: logs.Logger()}.Sweep(t.Context(), worktree)
	assert.NotContains(t, logs.String(), "ended what an agent's run left running", "neither the app nor a PID that can't be one")

	ProcSweeper{}.Sweep(t.Context(), worktree) // without a process table, it does nothing
}
