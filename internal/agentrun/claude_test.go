package agentrun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/scriptrun"
)

// fixtureDir is the worktree the S1 fixtures ran in.
const fixtureDir = "/work/example-repo/." + brand.Slug + "/worktrees/example-repo/pr-1"

// fixture returns an S1 stream fixture.
func fixture(t *testing.T, name string) []byte {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // the package's own fixture
	require.NoError(t, err)

	return body
}

// parsed returns a parser fed the fixture whole.
func parsed(t *testing.T, name string) *stream {
	t.Helper()

	p := newClaudeParser(fixtureDir)
	_, err := p.Write(fixture(t, name))
	require.NoError(t, err)

	return p
}

// kinds returns the kinds of a page's events, in order.
func kinds(page runevent.Page) []runevent.Kind {
	out := make([]runevent.Kind, 0, len(page.Events))
	for _, e := range page.Events {
		out = append(out, e.Kind)
	}

	return out
}

func TestTheParserReadsAStructuredSuccess(t *testing.T) {
	t.Parallel()

	p := parsed(t, "success-structured.jsonl")
	page := p.Page(0)

	assert.Equal(t, []runevent.Kind{
		runevent.KindStarted, runevent.KindTool, runevent.KindToolResult, runevent.KindThinking, runevent.KindTool,
		runevent.KindToolResult, runevent.KindResult,
	}, kinds(page))
	assert.Equal(t, "main.go", page.Events[1].Text, "a path inside the worktree is written from it")
	assert.Equal(t, runevent.CategoryRead, page.Events[1].Category)
	assert.Equal(t, runevent.CategoryReport, page.Events[4].Category)
	assert.Zero(t, page.Progress.Unknown, "every message of the fixture is known")

	r, ok := p.Result()
	require.True(t, ok)
	assert.True(t, p.Over())
	assert.Equal(t, "success", r.Subtype)
	assert.False(t, r.IsError)
	assert.Equal(t, []string{"claude-haiku-5-5"}, r.Models, "the model actually used, from modelUsage")
	require.NotNil(t, r.Cost)
	assert.InDelta(t, 0.0055, *r.Cost, 0.0001)
	assert.True(t, r.Estimated, "signed in with the plan (apiKeySource none), its cost is an estimate")
	assert.Equal(t, int64(3), r.Turns)

	report := parseReport(r.Report)
	require.NotNil(t, report, "the report is read from structured_output")
	assert.Equal(t, "nothing_to_do", report.Outcome)
	assert.False(t, report.Pushed)
	assert.Contains(t, report.Summary, "returns a - b")

	limit := p.Limit()
	require.NotNil(t, limit)
	assert.Equal(t, Limit{Status: "allowed", Type: "seven_day", ResetsAt: 1795071600}, *limit)
}

func TestACostOnAnAPIKeyIsNoEstimate(t *testing.T) {
	t.Parallel()

	p := newClaudeParser(fixtureDir)
	_, err := p.Write([]byte(`{"type":"system","subtype":"init","apiKeySource":"ANTHROPIC_API_KEY","model":"m"}` + "\n" +
		`{"type":"result","subtype":"success","total_cost_usd":0.25,"result":"Done."}` + "\n"))
	require.NoError(t, err)

	r, ok := p.Result()
	require.True(t, ok)
	require.NotNil(t, r.Cost)
	assert.False(t, r.Estimated, "an API key's cost is what it was charged")
}

func TestTheParserReadsTheOtherFixtures(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]struct {
		subtype string
		has     runevent.Kind
	}{
		"background-task.jsonl":  {subtype: "success", has: runevent.KindDenied},
		"budget-exhausted.jsonl": {subtype: "error_max_budget_usd", has: runevent.KindTool},
		"sigint.jsonl":           {subtype: "error_during_execution", has: runevent.KindTask},
	} {
		p := parsed(t, name)

		r, ok := p.Result()
		require.True(t, ok, name)
		assert.Equal(t, want.subtype, r.Subtype, name)
		assert.Contains(t, kinds(p.Page(0)), want.has, name)
		assert.Zero(t, p.Progress().Unknown, "%s: every message is known", name)
	}
}

func TestTheParserCountsTokensOncePerMessage(t *testing.T) {
	t.Parallel()

	p := newClaudeParser("")
	line := func(id string, out int64) string {
		return `{"type":"assistant","message":{"id":"` + id + `","content":[{"type":"text","text":"hi"}],` +
			`"usage":{"input_tokens":10,"cache_creation_input_tokens":5,"cache_read_input_tokens":2,"output_tokens":` +
			jsonInt(out) + `}}}` + "\n"
	}

	_, _ = p.Write([]byte(line("m1", 3) + line("m1", 7) + line("m2", 1)))

	got := p.Progress()
	assert.Equal(t, int64(30), got.TokensIn, "a message the stream repeats per block counts once")
	assert.Equal(t, int64(8), got.TokensOut)
	assert.Equal(t, int64(4), got.CacheRead)
}

func TestTheParserSplitsLinesItself(t *testing.T) {
	t.Parallel()

	whole := parsed(t, "success-structured.jsonl").Page(0)

	byteByByte := newClaudeParser(fixtureDir)
	for _, b := range fixture(t, "success-structured.jsonl") {
		_, _ = byteByByte.Write([]byte{b})
	}

	assert.Equal(t, whole, byteByByte.Page(0), "a line fed in parts reads as the line")
}

func TestTheParserWaitsForAPartialLastLine(t *testing.T) {
	t.Parallel()

	p := newClaudeParser("")
	text := `{"type":"assistant","message":{"id":"m","content":[{"type":"text","text":"Reading the log"}]}}`

	_, _ = p.Write([]byte(text[:40]))
	assert.Empty(t, p.Page(0).Events, "half a line is nothing yet")

	_, _ = p.Write([]byte(text[40:]))
	assert.Empty(t, p.Page(0).Events, "a line isn't read before its newline")

	_, _ = p.Write([]byte("\n"))
	require.Len(t, p.Page(0).Events, 1)
	assert.Equal(t, "Reading the log", p.Progress().Step)
}

func TestTheParserKeepsEightKilobytesOfAHugeLine(t *testing.T) {
	t.Parallel()

	huge := strings.Repeat("log line\n", 600_000) // over 5 MB

	content, err := json.Marshal(huge)
	require.NoError(t, err)

	p := newClaudeParser("")
	line := `{"type":"user","message":{"content":[{"type":"tool_result","content":` + string(content) + `}]}}` + "\n"
	require.Greater(t, len(line), 5<<20)

	_, _ = p.Write([]byte(line))

	page := p.Page(0)
	require.Len(t, page.Events, 1)
	assert.LessOrEqual(t, len(page.Events[0].Text), runevent.MaxText)
	assert.True(t, page.Events[0].Cut, "the event says its text was cut")
}

func TestTheParserCountsMessagesItDoesntKnow(t *testing.T) {
	t.Parallel()

	p := newClaudeParser("")
	_, _ = p.Write([]byte(`{"type":"a_newer_message","x":1}` + "\n" +
		`{"type":"system","subtype":"a_newer_note"}` + "\n" +
		"not json\n" +
		`{"type":"assistant","message":{"id":"m","content":[{"type":"text","text":"still here"}]}}` + "\n"))

	got := p.Progress()
	assert.Equal(t, int64(3), got.Unknown, "a newer agent's messages are counted, never fatal")
	assert.Equal(t, int64(1), got.Events)
}

func TestTheParserKeepsTheLastEvents(t *testing.T) {
	t.Parallel()

	p := newClaudeParser("")

	var b strings.Builder
	for range keepEvents + 10 {
		b.WriteString(`{"type":"assistant","message":{"content":[{"type":"thinking"}]}}` + "\n")
	}

	_, _ = p.Write([]byte(b.String()))

	page := p.Page(0)
	assert.Len(t, page.Events, keepEvents)
	assert.Equal(t, int64(11), page.First, "the oldest it still keeps")
	assert.Empty(t, p.Page(int64(keepEvents+10)).Events)
}

func TestTheParserReadsTheTodoListAsSteps(t *testing.T) {
	t.Parallel()

	p := newClaudeParser("")
	_, _ = p.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"TodoWrite","input":` +
		`{"todos":[{"content":"Read the log","status":"completed","activeForm":"Reading the log"},` +
		`{"content":"Fix the test","status":"in_progress","activeForm":"Fixing the test"}]}}]}}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./...\nmore"}}]}}` + "\n"))

	got := p.Progress()
	assert.Equal(t, []runevent.Todo{{Text: "Read the log", Status: "completed"}, {Text: "Fix the test", Status: "in_progress"}}, got.Todos)
	assert.Equal(t, "$ go test ./...", got.Step)
	assert.Equal(t, "1 of 2 done", p.Page(0).Events[0].Text)
}

// jsonInt writes n as JSON.
func jsonInt(n int64) string {
	return strconv.FormatInt(n, 10)
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
