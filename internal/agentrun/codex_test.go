package agentrun

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// Codex's half of the conformance suite: its parser over S6's fixtures (codex-cli 0.163.0,
// in testdata/codex), and the same runs Claude Code's fixtures go through, replayed by a
// fake codex.

// The S6 fixtures' worktree, and the success fixture's thread.
const (
	codexDir    = "/tmp/example-repo/." + brand.Slug + "/worktrees/example-repo/pr-1"
	codexThread = "01a1e66a-b48a-7f43-a547-901d3ca2bb33"
)

// codexFixture returns an S6 stream fixture.
func codexFixture(t *testing.T, name string) string {
	t.Helper()

	return string(fixture(t, filepath.Join("codex", name)))
}

// codexParsed returns a Codex parser fed the fixture whole.
func codexParsed(t *testing.T, name string) *stream {
	t.Helper()

	p := newCodexParser(codexDir)
	_, err := p.Write([]byte(codexFixture(t, name)))
	require.NoError(t, err)

	return p
}

// codexLines returns the first n lines of a Codex fixture.
func codexLines(t *testing.T, name string, n int) string {
	t.Helper()

	return lines(t, filepath.Join("codex", name), n)
}

func TestTheCodexParserReadsAStructuredSuccess(t *testing.T) {
	t.Parallel()

	p := codexParsed(t, "success-structured.jsonl")
	page := p.Page(0)

	assert.Equal(t, []runevent.Kind{
		runevent.KindStarted, runevent.KindText, runevent.KindTool, runevent.KindToolResult, runevent.KindTool,
		runevent.KindToolResult, runevent.KindTool, runevent.KindText, runevent.KindTool, runevent.KindToolResult,
		runevent.KindText, runevent.KindResult,
	}, kinds(page))
	assert.Zero(t, page.Progress.Unknown, "every event of the fixture is known")
	assert.Equal(t, codexThread, p.AgentSessionID(), "the thread's ID, which Codex picks, read from its stream")

	assert.Equal(t, `$ rg --files -g 'AGENTS.md' -g 'add.go' -g 'go.mod' -g '*_test.go'`, page.Events[2].Text,
		"the command, without the login shell Codex runs it in")
	assert.Equal(t, runevent.CategoryCommand, page.Events[2].Category)
	assert.Equal(t, "add.go", page.Events[6].Text, "a path inside the worktree is written from it")
	assert.Equal(t, runevent.CategoryEdit, page.Events[6].Category)

	r, ok := p.Result()
	require.True(t, ok)
	assert.True(t, p.Over())
	assert.False(t, r.IsError)
	assert.Nil(t, r.Cost, "Codex reports tokens, never a cost")
	assert.False(t, r.Estimated)
	assert.Equal(t, int64(92974-73344), r.TokensIn, "its input counts what it read from the cache within it")
	assert.Equal(t, int64(73344), r.CacheRead)
	assert.Equal(t, int64(462), r.TokensOut)

	report := parseReport(r.Report)
	require.NotNil(t, report, "the report is its last message, in the strict schema")
	assert.Equal(t, "fixed", report.Outcome)
	assert.Equal(t, "Updated add.go so Add returns a + b. `go test ./...` passed.", report.Summary)
	assert.Empty(t, report.PushedCommit, "null reads as none")
}

func TestTheCodexParserReadsTheOtherFixtures(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]struct {
		over, failed bool
		thread       string
	}{
		"fence-hook-push.jsonl":      {over: true, thread: "01a1e674-57d8-75e1-a31e-274b06aa73a8"},
		"review-readonly-note.jsonl": {over: true, thread: "01a1e670-d359-7720-8a38-a2402fc62c90"},
		"resume.jsonl":               {over: true, thread: "01a1e670-d359-7720-8a38-a2402fc62c90"},
		"sandbox-probe.jsonl":        {over: true, thread: "01a1e66f-a8ab-79c1-b021-fb9820d12e1c"},
		"schema-rejected.jsonl":      {over: true, failed: true, thread: "01a1e66a-5ccd-7a70-b0dc-de87c1bbb3d3"},
		"sigint.jsonl":               {thread: "01a1e679-71dd-7683-94d3-00ba4f1e6b22"},
		"sigkill.jsonl":              {thread: "01a1e67a-0ffe-71c2-b1bb-8f230a46e08c"},
	} {
		p := codexParsed(t, name)

		assert.Equal(t, want.over, p.Over(), name)
		assert.Equal(t, want.thread, p.AgentSessionID(), name)
		assert.Zero(t, p.Progress().Unknown, "%s: every event is known", name)

		if r, ok := p.Result(); ok {
			assert.Equal(t, want.failed, r.IsError, name)
		}

		for _, e := range p.Page(0).Events {
			assert.NotContains(t, e.Text, "dangerously-bypass-hook-trust", "%s: the hook bypass's warning isn't shown", name)
		}
	}

	push := codexParsed(t, "fence-hook-push.jsonl")
	r, _ := push.Result()
	report := parseReport(r.Report)
	require.NotNil(t, report)
	assert.True(t, report.Pushed)
	assert.Equal(t, "5bc5674913c19eb0aa26d17635645b45b1ff5dbd", report.PushedCommit)

	review := codexParsed(t, "review-readonly-note.jsonl")
	r, _ = review.Result()
	assert.Nil(t, parseReport(r.Report), "a message that isn't the schema's JSON is no report")
	assert.True(t, strings.HasPrefix(r.Message, "UNATTENDED The `add.go` change is correct"),
		"a read-only run's notes are its last message, whole")

	rejected := codexParsed(t, "schema-rejected.jsonl")
	r, _ = rejected.Result()
	assert.Equal(t, []string{"Invalid schema for response_format 'codex_output_schema': 'schema.additionalProperties' " +
		"is required to be supplied and to be false when strict is true."}, r.Errors, "the provider's error, by its message")
	assert.Nil(t, rejected.Limit(), "an error that isn't the usage limit")
	assert.Contains(t, kinds(rejected.Page(0)), runevent.KindRetry)

	sigint := codexParsed(t, "sigint.jsonl")
	assert.Equal(t, "$ sleep 45", sigint.Progress().Step, "cut off mid-command: no turn.completed or turn.failed")
}

func TestTheCodexParserReadsTheUsageLimit(t *testing.T) {
	t.Parallel()

	for _, stream := range []string{
		`{"type":"error","message":"You've hit your usage limit. Try again at 3:04 PM."}` + "\n" +
			`{"type":"turn.failed","error":{"message":"You've hit your usage limit. Try again at 3:04 PM."}}` + "\n",
		`{"type":"turn.failed","error":{"message":"{\"error\":{\"message\":\"Quota exceeded\",\"code\":\"quota_exceeded\"}}"}}` + "\n",
		`{"type":"turn.failed","error":{"message":"workspace_owner_credits_depleted"}}` + "\n",
	} {
		p := newCodexParser("")
		_, _ = p.Write([]byte(`{"type":"thread.started","thread_id":"` + codexThread + `"}` + "\n" + stream))

		limit := p.Limit()
		require.NotNil(t, limit, stream)
		assert.Equal(t, "rejected", limit.Status)

		r, ok := p.Result()
		require.True(t, ok)
		assert.True(t, r.IsError)

		out := judge(judgment{result: &r, limit: limit, fence: Fence{Timeout: time.Minute}})
		assert.Equal(t, StatusOutOfUsage, out.Status, "shown as out of usage, which the next profile falls back from")
	}
}

func TestTheCodexParserReadsItsTodosAndToolsOnce(t *testing.T) {
	t.Parallel()

	p := newCodexParser(codexDir)
	_, _ = p.Write([]byte(
		`{"type":"item.started","item":{"id":"t","type":"todo_list","items":[{"text":"Read the log","completed":true},` +
			`{"text":"Fix the test","completed":false}]}}` + "\n" +
			`{"type":"item.updated","item":{"id":"t","type":"todo_list","items":[{"text":"Read the log","completed":true},` +
			`{"text":"Fix the test","completed":true}]}}` + "\n" +
			`{"type":"item.completed","item":{"id":"c","type":"command_execution","command":"/bin/bash -lc \"go test ./...\"",` +
			`"aggregated_output":"FAIL","exit_code":1,"status":"failed"}}` + "\n" +
			`{"type":"item.completed","item":{"id":"m","type":"mcp_tool_call","server":"docs","tool":"search","status":"completed"}}` + "\n" +
			`{"type":"item.completed","item":{"id":"w","type":"web_search","query":"x"}}` + "\n" +
			`{"type":"item.completed","item":{"id":"r","type":"reasoning","text":"…"}}` + "\n" +
			`{"type":"a_newer_event"}` + "\n",
	))

	got := p.Progress()
	assert.Equal(t, []runevent.Todo{{Text: "Read the log", Status: "completed"}, {Text: "Fix the test", Status: "completed"}},
		got.Todos)
	assert.Equal(t, int64(1), got.Unknown, "a newer Codex's events are counted, never fatal")

	page := p.Page(0)
	assert.Equal(t, []runevent.Kind{
		runevent.KindTool, runevent.KindTool, runevent.KindToolResult, runevent.KindTool, runevent.KindTool,
		runevent.KindThinking,
	}, kinds(page))
	assert.Equal(t, "$ go test ./...", page.Events[1].Text)
	assert.True(t, page.Events[2].Error, "a command that failed")
	assert.Equal(t, "docs.search", page.Events[3].Text)
	assert.Equal(t, runevent.CategoryWeb, page.Events[4].Category)
}

// codexRig is a runner whose Codex runs read their limit from a fake rollout.
func codexRig(t *testing.T, limits LimitReader) *rig {
	t.Helper()

	r := newRig(t)
	r.limits = limits
	r.engine.StopAll()
	r.engine, r.service, r.runner = r.build()
	t.Cleanup(r.engine.StopAll)

	return r
}

func TestCodexConformanceSuccess(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	got := filepath.Join(t.TempDir(), "stdin")
	plan := r.planFor(agent.Codex, testutil.FakeRun{Stdout: codexFixture(t, "success-structured.jsonl"), StdinTo: got})

	id := r.start(plan)
	end := r.end(plan.RunID)

	assert.Equal(t, StatusDone, end.Outcome.Status)
	assert.Equal(t, id, end.ScriptRunID)
	require.NotNil(t, end.Outcome.Report)
	assert.Equal(t, "fixed", end.Outcome.Report.Outcome)
	assert.Equal(t, "Finished: Updated add.go so Add returns a + b. `go test ./...` passed.", end.Outcome.Summary)
	assert.Equal(t, int64(462), end.Outcome.TokensOut)
	assert.Nil(t, end.Outcome.Result.Cost)
	assert.Zero(t, end.Outcome.Refused)
	assert.NotNil(t, end.Outcome.Refusals)

	body, err := os.ReadFile(got) //nolint:gosec // the test's own temp file
	require.NoError(t, err)
	assert.Equal(t, "Review it.", string(body), "the prompt came on stdin")

	assert.Equal(t, []SessionState{SessionWorking, SessionEnded}, r.sessions.of(plan.SessionID))
	assert.Equal(t, codexThread, r.sessions.linked(plan.SessionID),
		"its session is linked to the thread its stream named, so it can be taken over")
	assert.Equal(t, scriptrun.StatusSucceeded, r.record(id).Status)

	page, err := r.runner.Events(t.Context(), plan.RunID, 0)
	require.NoError(t, err)
	assert.True(t, page.Done)
	assert.Equal(t, runevent.KindStarted, page.Events[0].Kind, "an ended run's events are read from its log, as Codex's")
}

func TestCodexConformanceAnErrorIsJudgedByItsTurn(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.planFor(agent.Codex, testutil.FakeRun{Stdout: codexFixture(t, "schema-rejected.jsonl"), Exit: 1})

	r.start(plan)
	end := r.end(plan.RunID)

	assert.Equal(t, StatusFailed, end.Outcome.Status)
	assert.True(t, strings.HasPrefix(end.Outcome.Summary, "It failed: Invalid schema for response_format"),
		end.Outcome.Summary)
	assert.Equal(t, []SessionState{SessionWorking, SessionFailed}, r.sessions.of(plan.SessionID))
}

func TestCodexConformanceOutOfUsageReadsWhenItResets(t *testing.T) {
	t.Parallel()

	reader := &fakeLimits{limit: Limit{Status: "rejected", Type: "seven_day", ResetsAt: 1794984399}}
	r := codexRig(t, reader)
	failed := `{"type":"turn.failed","error":{"message":"You've hit your usage limit."}}` + "\n"
	plan := r.planFor(agent.Codex, testutil.FakeRun{Stdout: codexLines(t, "success-structured.jsonl", 2) + failed, Exit: 1})
	plan.Session.ConfigDir = "/srv/.codex_work"
	plan.Fence.BudgetUSD = 0

	r.start(plan)
	end := r.end(plan.RunID)

	assert.Equal(t, StatusOutOfUsage, end.Outcome.Status, "out of usage, not a failure: the next profile runs")
	require.NotNil(t, end.Outcome.Limit)
	assert.Equal(t, Limit{Status: "rejected", Type: "seven_day", ResetsAt: 1794984399}, *end.Outcome.Limit,
		"which limit, and when it resets, from the thread's own rollout")
	assert.True(t, strings.HasPrefix(end.Outcome.Summary, "Out of usage: its plan's weekly limit is reached. It resets "))
	assert.Equal(t, []string{"codex /srv/.codex_work " + codexThread}, reader.asked(), "read from the profile's own home")
}

func TestCodexConformanceACrashMidRun(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.planFor(agent.Codex, testutil.FakeRun{Stdout: codexFixture(t, "sigkill.jsonl"), Exit: 1})

	r.start(plan)
	end := r.end(plan.RunID)

	assert.Equal(t, StatusFailed, end.Outcome.Status, "no turn.completed or turn.failed: it never said how it ended")
	assert.Contains(t, end.Outcome.NoReport, "The agent didn't report. It last said: $ sleep 45")
}

func TestCodexConformanceAHangUntilTheTimeLimit(t *testing.T) {
	t.Parallel()

	limit := newHeldLimit()
	r := newRig(t, limit.hold)
	plan := r.planFor(agent.Codex, testutil.FakeRun{Stdout: codexLines(t, "success-structured.jsonl", 4), Sleep: time.Minute})
	plan.Fence.Timeout = 20 * time.Minute

	r.start(plan)
	assert.Equal(t, 20*time.Minute, limit.await(t), "the run is held to its fence's time limit")

	r.awaitEvents(plan.RunID)
	limit.reach()

	end := r.end(plan.RunID)

	assert.Equal(t, StatusTimedOut, end.Outcome.Status, "it hung mid-run until its limit came")
	assert.Equal(t, "Stopped after 20 minutes.", end.Outcome.Summary)
}

func TestCodexConformanceStopAndTheSweepAfterSIGKILL(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.planFor(agent.Codex, testutil.FakeRun{Stdout: codexLines(t, "sigint.jsonl", 3), Sleep: time.Minute})

	r.start(plan)
	require.Eventually(t, func() bool { return len(r.runner.Folders()) == 1 }, waitFor, 10*time.Millisecond)

	require.NoError(t, r.runner.Stop(t.Context(), plan.RunID))
	assert.Equal(t, StatusStopped, r.end(plan.RunID).Outcome.Status, "Stop's SIGINT ends it: no turn's end, stopped")
	assert.NotContains(t, r.sweeper.swept(), plan.Dir, "SIGINT ends what it started")
}

func TestCodexRunsNeverOutliveTheQuit(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.planFor(agent.Codex, testutil.FakeRun{
		Stdout: codexLines(t, "sigint.jsonl", 3), Sleep: time.Minute, IgnoreSignals: true,
	})
	plan.Fence.BudgetUSD = 5

	assert.False(t, outlives(intentOf(plan)), "Codex keeps no budget of its own, whatever the step's dollars say")

	r.start(plan)
	require.Eventually(t, func() bool { return len(r.runner.Folders()) == 1 }, waitFor, 10*time.Millisecond)

	pid := r.runner.Folders()[0].PID

	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })

	require.Eventually(t, func() bool {
		progress, ok := r.runner.Progress(plan.RunID)

		return ok && progress.Step == "$ sleep 45"
	}, waitFor, 10*time.Millisecond, "it runs, its signals ignored")

	// The app quits letting the runs that can finish go on: a Codex run isn't one of them.
	// It ignores SIGINT and SIGTERM, so the quit's SIGKILL ends it, and what it left in its
	// worktree, as a command it ran does (S6), is swept.
	r.engine.LetFinish(true)
	r.engine.StopAll()

	end := r.end(plan.RunID)
	assert.Equal(t, StatusInterrupted, end.Outcome.Status, "stopped as the app quit, its match back under Needs you")
	assert.False(t, procs.Alive(pid))
	assert.Contains(t, r.sweeper.swept(), plan.Dir, "after SIGKILL, every process in its worktree is ended")
}

func TestCodexConformanceReconcileAfterARestart(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.planFor(agent.Codex, testutil.FakeRun{Stdout: codexFixture(t, "success-structured.jsonl")})

	id := r.start(plan)
	assert.Equal(t, StatusDone, r.end(plan.RunID).Outcome.Status)

	// After a restart, the run's log is read again through Codex's parser: its events, its
	// thread, and its end.
	_, _, runner := r.build()

	page, err := runner.EventsBefore(t.Context(), plan.RunID, 0)
	require.NoError(t, err)
	assert.True(t, page.Done)
	assert.Equal(t, "Finished: Updated add.go so Add returns a + b. `go test ./...` passed.", page.Ending.Summary)
	assert.Equal(t, "fixed", page.Ending.Outcome)

	rec := r.record(id)
	v := runner.Verify(scriptrun.Record{ID: rec.ID, Dir: rec.Dir, Result: rec.Result})
	assert.False(t, v.OutlivesQuit, "a Codex run found going after a restart is stopped, never let run unbounded")

	_, _ = v.Output.Write([]byte(codexFixture(t, "success-structured.jsonl")))
	out := v.Finish(t.Context(), scriptrun.Ending{}, scriptrun.Outcome{})
	detail, ok := out.Detail.(Outcome)
	require.True(t, ok)
	assert.Equal(t, StatusDone, detail.Status, "an exited run is judged from its log")

	gone := runner.Verify(scriptrun.Record{ID: rec.ID, Dir: rec.Dir, Result: rec.Result})
	_, _ = gone.Output.Write([]byte(codexLines(t, "sigkill.jsonl", 3)))
	out = gone.Finish(t.Context(), scriptrun.Ending{Interrupted: "The Mac shut down."}, scriptrun.Outcome{})
	detail, ok = out.Detail.(Outcome)
	require.True(t, ok)
	assert.Equal(t, StatusInterrupted, detail.Status, "one gone without its turn's end is interrupted")
}

func TestARunsRefusalsAreReadFromItsFolder(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.planFor(agent.Codex, testutil.FakeRun{Stdout: codexFixture(t, "fence-hook-push.jsonl")})

	require.NoError(t, os.MkdirAll(plan.Folder, 0o700))

	for _, c := range []string{"git push origin HEAD:other", "git push --force origin HEAD:pr-1", "curl -sS https://example.com"} {
		fence.LogDenial(plan.Folder, fence.Call{Tool: "Bash", Command: c}, "fence: refused")
	}

	r.start(plan)
	end := r.end(plan.RunID)

	assert.Equal(t, StatusDone, end.Outcome.Status)
	assert.Equal(t, 3, end.Outcome.Refused, "the calls its hook refused, which Codex's stream never shows")
	require.Len(t, end.Outcome.Refusals, 3)
	assert.Equal(t, "curl -sS https://example.com", end.Outcome.Refusals[2].What)

	page, err := r.runner.EventsBefore(t.Context(), plan.RunID, 0)
	require.NoError(t, err)
	require.NotNil(t, page.Ending)
	assert.Equal(t, 3, page.Ending.Refused, "the run view says how many its fence refused")
}

// fakeLimits is a LimitReader that reads one limit, and records what it was asked.
type fakeLimits struct {
	limit Limit
	calls []string
}

func (f *fakeLimits) Limit(_ context.Context, id agent.ID, configDir, agentSessionID string) (Limit, bool) {
	f.calls = append(f.calls, string(id)+" "+configDir+" "+agentSessionID)

	return f.limit, true
}

func (f *fakeLimits) asked() []string {
	return append([]string(nil), f.calls...)
}
