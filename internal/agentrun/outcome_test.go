package agentrun

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agentrun/stream"
	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/scriptrun"
)

// ended returns the agent's own end of a whole fixture, as its parser reads it.
func ended(t *testing.T, p *stream.Parser, name string) Result {
	t.Helper()

	_, err := p.Write(fixture(t, name))
	require.NoError(t, err)

	r, ok := p.Result()
	require.True(t, ok, "%s ends with the agent's own result", name)

	return r
}

func TestEachAgentsReportIsReadFromItsStream(t *testing.T) {
	t.Parallel()

	claude := parseReport(ended(t, stream.Claude(""), "success-structured.jsonl").Report)
	require.NotNil(t, claude, "Claude Code's report is read from its structured_output")
	assert.Equal(t, "nothing_to_do", claude.Outcome)
	assert.False(t, claude.Pushed)
	assert.Contains(t, claude.Summary, "returns a - b")

	codex := parseReport(ended(t, stream.Codex(""), filepath.Join("codex", "success-structured.jsonl")).Report)
	require.NotNil(t, codex, "Codex's report is its last message, in the strict schema")
	assert.Equal(t, "fixed", codex.Outcome)
	assert.Equal(t, "Updated add.go so Add returns a + b. `go test ./...` passed.", codex.Summary)
	assert.Empty(t, codex.PushedCommit, "null reads as none")

	pushed := parseReport(ended(t, stream.Codex(""), filepath.Join("codex", "fence-hook-push.jsonl")).Report)
	require.NotNil(t, pushed)
	assert.True(t, pushed.Pushed)
	assert.Equal(t, "5bc5674913c19eb0aa26d17635645b45b1ff5dbd", pushed.PushedCommit)
}

func TestALimitIsNamedAsThePlanNamesIt(t *testing.T) {
	t.Parallel()

	for kind, want := range map[string]string{
		"five_hour": "five-hour", "seven_day": "weekly", "seven_day_opus": "weekly Opus", "monthly_cap": "monthly cap",
		"": "",
	} {
		assert.Equal(t, want, limitName(kind), kind)
	}

	assert.Equal(t, "Out of usage.", outOfUsageWords(Limit{Status: "rejected"}), "with nothing more to say")
}

func TestAQuitInterruptsARunAndAStopStopsIt(t *testing.T) {
	t.Parallel()

	result := &Result{Subtype: "error_during_execution", IsError: true}

	quit := judge(judgment{end: scriptrun.Ending{Quit: true, Stopped: true}, result: result})
	assert.Equal(t, StatusInterrupted, quit.Status, "no one chose to stop it, so its match comes back")
	assert.Equal(t, "Interrupted as the app quit.", quit.Summary)

	stopped := judge(judgment{end: scriptrun.Ending{Stopped: true}, result: result})
	assert.Equal(t, StatusStopped, stopped.Status, "the user's Stop")
}

func TestALeakedSandboxFailsTheRun(t *testing.T) {
	t.Parallel()

	done := Outcome{Status: StatusDone, Summary: "Finished: done."}

	leaked := sandboxed(done, fence.SandboxLeaked)
	assert.Equal(t, StatusFailed, leaked.Status, "its commands weren't held to the fence")
	assert.Equal(t, leakedWords, leaked.Summary)
	assert.Equal(t, fence.SandboxLeaked, leaked.Sandbox)

	held := sandboxed(done, fence.SandboxHeld)
	assert.Equal(t, done.Summary, held.Summary)
	assert.Equal(t, fence.SandboxHeld, held.Sandbox)

	stopped := sandboxed(Outcome{Status: StatusStopped, Summary: "Stopped."}, fence.SandboxLeaked)
	assert.Equal(t, StatusStopped, stopped.Status, "a stop says so already")

	assert.Empty(t, sandboxOf(""), "no folder, no policy")
	assert.Empty(t, sandboxOf(t.TempDir()), "a folder with no policy can't say")
}
