package stream

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
)

// fixtureDir is the worktree the S1 fixtures ran in.
const fixtureDir = "/work/example-repo/." + brand.Slug + "/worktrees/example-repo/pr-1"

// fixture returns a recorded stream: S1's, S6's (codex/), or the conversation spike's
// (conversation/).
func fixture(t *testing.T, name string) []byte {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // the package's own fixture
	require.NoError(t, err)

	return body
}

// parsed returns a parser fed the fixture whole.
func parsed(t *testing.T, name string) *Parser {
	t.Helper()

	p := Claude(fixtureDir)
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

// reportOf reads the outcome, the summary, and the push of a structured report.
func reportOf(t *testing.T, raw json.RawMessage) report {
	t.Helper()

	var r report
	require.NoError(t, json.Unmarshal(raw, &r), "the report is JSON")

	return r
}

// report is what the tests read of a run's structured report.
type report struct {
	Outcome      string `json:"outcome"`
	Summary      string `json:"summary"`
	Pushed       bool   `json:"pushed"`
	PushedCommit string `json:"pushedCommit"`
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
	require.NotNil(t, r.TurnCost)
	assert.InDelta(t, *r.Cost, *r.TurnCost, 1e-12, "a run that starts its conversation costs the same in both")
	assert.True(t, r.Estimated, "signed in with the plan (apiKeySource none), its cost is an estimate")
	assert.Equal(t, int64(3), r.Turns)

	got := reportOf(t, r.Report)
	assert.Equal(t, "nothing_to_do", got.Outcome, "the report is read from structured_output")
	assert.False(t, got.Pushed)
	assert.Contains(t, got.Summary, "returns a - b")

	limit := p.Limit()
	require.NotNil(t, limit)
	assert.Equal(t, Limit{Status: "allowed", Type: "seven_day", ResetsAt: 1795071600}, *limit)
}

func TestACostOnAnAPIKeyIsNoEstimate(t *testing.T) {
	t.Parallel()

	p := Claude(fixtureDir)
	_, err := p.Write([]byte(`{"type":"system","subtype":"init","apiKeySource":"ANTHROPIC_API_KEY","model":"m"}` + "\n" +
		`{"type":"result","subtype":"success","total_cost_usd":0.25,"result":"Done."}` + "\n"))
	require.NoError(t, err)

	r, ok := p.Result()
	require.True(t, ok)
	require.NotNil(t, r.Cost)
	assert.False(t, r.Estimated, "an API key's cost is what it was charged")
	assert.Nil(t, r.TurnCost, "a result line that says nothing of its turn alone reports no turn's cost")
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

	p := Claude("")
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

	byteByByte := Claude(fixtureDir)
	for _, b := range fixture(t, "success-structured.jsonl") {
		_, _ = byteByByte.Write([]byte{b})
	}

	assert.Equal(t, whole, byteByByte.Page(0), "a line fed in parts reads as the line")
}

func TestTheParserWaitsForAPartialLastLine(t *testing.T) {
	t.Parallel()

	p := Claude("")
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

	p := Claude("")
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

	p := Claude("")
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

	p := Claude("")

	var b strings.Builder
	for range KeepEvents + 10 {
		b.WriteString(`{"type":"assistant","message":{"content":[{"type":"thinking"}]}}` + "\n")
	}

	_, _ = p.Write([]byte(b.String()))

	page := p.Page(0)
	assert.Len(t, page.Events, KeepEvents)
	assert.Equal(t, int64(11), page.First, "the oldest it still keeps")
	assert.Empty(t, p.Page(int64(KeepEvents+10)).Events)
	assert.Equal(t, page.Events, p.Events(), "every event it keeps, oldest first")
}

func TestTheParserReadsTheTodoListAsSteps(t *testing.T) {
	t.Parallel()

	p := Claude("")
	_, _ = p.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"TodoWrite","input":` +
		`{"todos":[{"content":"Read the log","status":"completed","activeForm":"Reading the log"},` +
		`{"content":"Fix the test","status":"in_progress","activeForm":"Fixing the test"}]}}]}}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./...\nmore"}}]}}` + "\n"))

	got := p.Progress()
	assert.Equal(t, []runevent.Todo{{Text: "Read the log", Status: "completed"}, {Text: "Fix the test", Status: "in_progress"}}, got.Todos)
	assert.Equal(t, "$ go test ./...", got.Step)
	assert.Equal(t, "1 of 2 done", p.Page(0).Events[0].Text)
	assert.Equal(t, "$ go test ./...", p.LastWords(), "with nothing said, its last words are its latest step")
}

// jsonInt writes n as JSON.
func jsonInt(n int64) string {
	return strconv.FormatInt(n, 10)
}

// toolUse returns an assistant's line that uses the tool name with input.
func toolUse(name, input string) string {
	return `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"` + name + `","input":` + input + `}]}}` + "\n"
}

func TestTheParserSaysWhatEachToolDoes(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		line     string
		category runevent.Category
		text     string
		step     string
	}{
		"an edit in the worktree": {
			line: toolUse("Edit", `{"file_path":"`+fixtureDir+`/add.go"}`), category: runevent.CategoryEdit, text: "add.go",
			step: "Editing add.go",
		},
		"a read outside it": {
			line: toolUse("Read", `{"file_path":"/etc/hosts"}`), category: runevent.CategoryRead, text: "/etc/hosts",
			step: "Reading /etc/hosts",
		},
		"a search": {
			line: toolUse("Grep", `{"pattern":"func Add","path":"`+fixtureDir+`/internal"}`), category: runevent.CategorySearch,
			text: "func Add internal", step: "Searching for func Add",
		},
		"a subagent": {
			line: toolUse("Task", `{"description":"Review the tests"}`), category: runevent.CategorySubagent,
			text: "Review the tests", step: "Review the tests",
		},
		"a page read": {
			line: toolUse("WebFetch", `{"url":"https://example.com/docs"}`), category: runevent.CategoryWeb,
			text: "https://example.com/docs", step: "Reading https://example.com/docs",
		},
		"a web search": {
			line: toolUse("WebSearch", `{"query":"go timers"}`), category: runevent.CategoryWeb, text: "go timers",
			step: "Reading go timers",
		},
		"a tool it doesn't know": {
			line: toolUse("mcp__acme__deploy", `{"target":"prod"}`), category: runevent.CategoryOther, text: "mcp__acme__deploy",
			step: "mcp__acme__deploy",
		},
		"a todo in progress named only by its content": {
			line:     toolUse("TodoWrite", `{"todos":[{"content":"Fix the test","status":"in_progress"}]}`),
			category: runevent.CategoryTodo, text: "0 of 1 done", step: "Fix the test",
		},
	} {
		p := Claude(fixtureDir)
		_, _ = p.Write([]byte(tc.line))

		page := p.Page(0)
		require.Len(t, page.Events, 1, name)
		assert.Equal(t, runevent.KindTool, page.Events[0].Kind, name)
		assert.Equal(t, tc.category, page.Events[0].Category, name)
		assert.Equal(t, tc.text, page.Events[0].Text, name)
		assert.Equal(t, tc.step, p.Progress().Step, name)
		assert.Zero(t, p.Progress().Unknown, name)
	}
}

func TestTheParserReadsAToolsResultInEitherShape(t *testing.T) {
	t.Parallel()

	result := func(content string, isError bool) string {
		e := "false"
		if isError {
			e = "true"
		}

		return `{"type":"user","message":{"content":[{"type":"tool_result","is_error":` + e + `,"content":` + content + `}]}}` + "\n"
	}

	p := Claude("")
	_, _ = p.Write([]byte(result(`"  ok\n"`, false) +
		result(`[{"type":"text","text":"first"},{"type":"image"},{"type":"text","text":"second"}]`, true) +
		result(`42`, false) +
		`{"type":"user","message":{"content":"a plain prompt"}}` + "\n" +
		`{"type":"user","message":{"content":[{"type":"text","text":"a note"}]}}` + "\n"))

	page := p.Page(0)
	require.Len(t, page.Events, 3, "a plain message and a text block show nothing")
	assert.Equal(t, "ok", page.Events[0].Text, "a string result, trimmed")
	assert.Equal(t, "first\nsecond", page.Events[1].Text, "the text blocks of a result, and nothing else")
	assert.True(t, page.Events[1].Error)
	assert.Empty(t, page.Events[2].Text, "a result in no shape it knows shows no text")
	assert.Zero(t, p.Progress().Unknown)
}

func TestTheParserCountsKnownMessagesItCantRead(t *testing.T) {
	t.Parallel()

	p := Claude("")
	_, _ = p.Write([]byte(`{"type":"assistant"}` + "\n" +
		`{"type":"assistant","message":{"content":"not blocks"}}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"server_tool_use"},{"type":"text","text":"  "}]}}` + "\n" +
		`{"type":"user","message":"not a message"}` + "\n" +
		`{"type":"user","message":{"content":7}}` + "\n" +
		`{"type":"user","message":{"content":[{"type":"document"}]}}` + "\n" +
		`{"type":"rate_limit_event"}` + "\n"))

	got := p.Progress()
	assert.Equal(t, int64(7), got.Unknown, "each is counted, never fatal")
	assert.Zero(t, got.Events, "and none shows")
	assert.Nil(t, p.Limit(), "a limit event without its limit says nothing of it")
}

func TestAPageReadFromTheLogsMiddleCountsItsOffsetsFromThere(t *testing.T) {
	t.Parallel()

	said := `{"type":"assistant","message":{"id":"m","content":[{"type":"text","text":"Reading the log"}]}}`

	p := Claude("").From(1000)
	_, _ = p.Write([]byte(said + "\n" + said))

	require.Len(t, p.Events(), 1, "a last line not ended yet is held")

	p.Flush()

	events := p.Events()
	require.Len(t, events, 2, "a log that won't grow has its last line read whole")
	assert.Equal(t, int64(1000), events[0].Offset, "the first line starts where the page does")
	assert.Equal(t, int64(1000+len(said)+1), events[1].Offset)
	assert.Equal(t, "Reading the log", p.LastWords())
}
