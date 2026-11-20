package stream

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
	"github.com/mrz1836/nilbox/internal/brand"
)

// The conversation spike's recordings (Claude Code 2.1.328, in testdata/conversation): turns
// of `claude -p` that ran only the app's own MCP server, named for brand.Slug, in the
// shared work folder.

// turnSession is the session the first and the resumed turn ran in.
const turnSession = "00000000-0000-4000-8000-000000000001"

// turn returns a parser fed one of the conversation spike's turns whole.
func turn(t *testing.T, name string) *Parser {
	t.Helper()

	p := Claude("")
	_, err := p.Write(fixture(t, filepath.Join("conversation", name)))
	require.NoError(t, err)

	return p
}

// turnLines returns the first n lines of one of the conversation spike's turns.
func turnLines(t *testing.T, name string, n int) []byte {
	t.Helper()

	all := strings.SplitAfter(string(fixture(t, filepath.Join("conversation", name))), "\n")

	return []byte(strings.Join(all[:n], ""))
}

// ownTools are the tools the app's own server gave the first turn.
func ownTools() []string {
	tools := []string{
		"autopilot_check", "autopilot_guide", "autopilot_inbox_names", "autopilot_my_setup", "autopilot_proposal_status",
		"autopilot_propose", "autopilot_reference",
	}

	for i, tool := range tools {
		tools[i] = "mcp__" + brand.Slug + "__" + tool
	}

	return tools
}

func TestTheInitLineSaysWhatTheTurnRunsWith(t *testing.T) {
	t.Parallel()

	p := turn(t, "turn-first.jsonl")

	got, ok := p.Init()
	require.True(t, ok)
	assert.Equal(t, Init{
		Model: "claude-haiku-5-5", Version: "2.1.328", APIKeySource: "none", SessionID: turnSession, Tools: ownTools(),
		MCPServers: []MCPServer{{Name: brand.Slug, Status: "connected"}},
	}, got, "its model, version, sign-in, session, tools, and servers, as the turn checks its own setup")

	got.Tools[0], got.MCPServers[0].Status = "Bash", "failed"

	again, _ := p.Init()
	assert.Equal(t, ownTools(), again.Tools, "what Init returns is the caller's own copy")
	assert.Equal(t, "connected", again.MCPServers[0].Status)

	assert.Equal(t, runevent.KindStarted, p.Page(0).Events[0].Kind)
	assert.Equal(t, "claude-haiku-5-5 · Claude Code 2.1.328", p.Page(0).Events[0].Text)
}

func TestAServerThatFailedShowsInTheInitLine(t *testing.T) {
	t.Parallel()

	p := turn(t, "turn-mcp-failed.jsonl")

	got, ok := p.Init()
	require.True(t, ok)
	assert.Equal(t, []MCPServer{{Name: brand.Slug, Status: "failed"}}, got.MCPServers)
	assert.NotNil(t, got.Tools, "no tools is an empty list, never nil")
	assert.Empty(t, got.Tools, "a server that failed gave the turn no tools")

	bare := Claude("")
	_, _ = bare.Write([]byte(`{"type":"system","subtype":"init","model":"m"}` + "\n"))

	got, ok = bare.Init()
	require.True(t, ok)
	assert.Equal(t, Init{Model: "m", Tools: []string{}, MCPServers: []MCPServer{}}, got,
		"an init line that names no tools or servers has none, never nil")
}

func TestTheSessionsTitleComesBeforeItsInitAsANote(t *testing.T) {
	t.Parallel()

	title, both := turnLines(t, "turn-first.jsonl", 1), turnLines(t, "turn-first.jsonl", 2)

	p := Claude("")
	_, _ = p.Write(title)

	_, ok := p.Init()
	assert.False(t, ok, "no init line yet")
	assert.Empty(t, p.Page(0).Events, "a named session's title shows nothing")
	assert.Zero(t, p.Progress().Unknown, "and it is a note the parser knows")

	_, _ = p.Write(both[len(title):])

	_, ok = p.Init()
	assert.True(t, ok, "the init line that follows it is read")
	assert.Equal(t, []runevent.Kind{runevent.KindStarted}, kinds(p.Page(0)))
}

func TestSeveralToolUsesOfOneMessageEachShow(t *testing.T) {
	t.Parallel()

	p := turn(t, "turn-first.jsonl")
	page := p.Page(0)

	var tools, results []string

	for _, e := range page.Events[1:7] {
		if e.Kind == runevent.KindTool {
			tools = append(tools, e.Tool)
		}

		if e.Kind == runevent.KindToolResult {
			results = append(results, e.Text)
		}
	}

	slug := "mcp__" + brand.Slug + "__"
	assert.Equal(t, []string{slug + "autopilot_guide", slug + "autopilot_my_setup", slug + "autopilot_inbox_names"}, tools,
		"the three calls of one message, each on a line of its own, show in order")
	require.Len(t, results, 3, "and then each one's result")
	assert.True(t, strings.HasPrefix(results[0], "# How Autopilot works"))

	for _, e := range page.Events {
		if e.Kind == runevent.KindTool {
			assert.Equal(t, runevent.CategoryOther, e.Category, "%s is the app's own tool, no category of Claude Code's", e.Tool)
		}
	}

	r, ok := p.Result()
	require.True(t, ok)
	assert.Equal(t, int64(12+5391), r.TokensIn, "the turn's tokens, from its result line")
	assert.Zero(t, page.Progress.Unknown, "every message of the turn is known")
}

func TestAResumedTurnsCostIsItsOwn(t *testing.T) {
	t.Parallel()

	first, ok := turn(t, "turn-first.jsonl").Result()
	require.True(t, ok)
	require.NotNil(t, first.Cost)
	require.NotNil(t, first.TurnCost)
	assert.InDelta(t, 0.00173011, *first.TurnCost, 1e-12)
	assert.InDelta(t, *first.Cost, *first.TurnCost, 1e-12, "the first turn's total is its own")

	resumed, ok := turn(t, "turn-resumed.jsonl").Result()
	require.True(t, ok)
	require.NotNil(t, resumed.Cost)
	require.NotNil(t, resumed.TurnCost)
	assert.InDelta(t, 0.00235782, *resumed.Cost, 1e-12, "total_cost_usd is the conversation's so far")
	assert.InDelta(t, 0.00062771, *resumed.TurnCost, 1e-12, "and the turn's own is its model_usage_since_last_result")
	assert.InDelta(t, *resumed.Cost, *first.Cost+*resumed.TurnCost, 1e-12, "the two turns add up to the total")
	assert.True(t, resumed.Estimated, "on the user's plan, both are estimates")
	assert.Equal(t, int64(6+842), resumed.TokensIn, "the tokens are the turn's own")
	assert.Equal(t, int64(580), resumed.TokensOut)
	assert.Equal(t, int64(16871), resumed.CacheRead)
}

func TestATurnsCostAddsUpEachModelsShare(t *testing.T) {
	t.Parallel()

	p := Claude("")
	_, _ = p.Write([]byte(`{"type":"result","subtype":"success","total_cost_usd":1.5,` +
		`"model_usage_since_last_result":{"m2":{"costUSD":0.25},"m1":{"costUSD":0.5},"m3":{}}}` + "\n"))

	r, ok := p.Result()
	require.True(t, ok)
	require.NotNil(t, r.TurnCost)
	assert.InDelta(t, 0.75, *r.TurnCost, 1e-12)
	assert.Empty(t, r.Models, "the models come from modelUsage, which this line has none of")

	none := Claude("")
	_, _ = none.Write([]byte(`{"type":"result","subtype":"success","model_usage_since_last_result":{}}` + "\n"))

	r, _ = none.Result()
	require.NotNil(t, r.TurnCost, "a turn that used no model cost nothing")
	assert.Zero(t, *r.TurnCost)
	assert.Nil(t, r.Cost)
}

func TestTheParserReadsEveryRecordedTurn(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]struct {
		subtype, reason string
		isError         bool
		has             runevent.Kind
	}{
		"turn-first.jsonl":                {subtype: "success", reason: "completed", has: runevent.KindText},
		"turn-resumed.jsonl":              {subtype: "success", reason: "completed", has: runevent.KindText},
		"turn-after-sigint.jsonl":         {subtype: "success", reason: "completed", has: runevent.KindText},
		"turn-mcp-failed.jsonl":           {subtype: "success", reason: "completed", has: runevent.KindThinking},
		"turn-error-and-structured.jsonl": {subtype: "success", reason: "completed", has: runevent.KindToolResult},
		"turn-denied.jsonl":               {subtype: "success", reason: "completed", has: runevent.KindDenied},
		"turn-budget.jsonl": {
			subtype: "error_max_budget_usd", reason: "budget_exhausted", isError: true, has: runevent.KindTool,
		},
		"turn-sigint.jsonl": {
			subtype: "error_during_execution", reason: "aborted_streaming", isError: true, has: runevent.KindToolResult,
		},
	} {
		p := turn(t, name)

		r, ok := p.Result()
		require.True(t, ok, name)
		assert.Equal(t, want.subtype, r.Subtype, name)
		assert.Equal(t, want.reason, r.TerminalReason, name)
		assert.Equal(t, want.isError, r.IsError, name)
		assert.NotNil(t, r.TurnCost, "%s: every 2.1.328 result says what its turn cost", name)
		assert.Contains(t, kinds(p.Page(0)), want.has, name)
		assert.Zero(t, p.Progress().Unknown, "%s: every message is known", name)

		in, ok := p.Init()
		require.True(t, ok, name)
		assert.Equal(t, "2.1.328", in.Version, name)
	}

	denied, _ := turn(t, "turn-denied.jsonl").Result()
	assert.Equal(t, 3, denied.Denials, "without an allow rule, dontAsk denies the app's own tools")

	budget, _ := turn(t, "turn-budget.jsonl").Result()
	assert.Equal(t, []string{"Reached maximum budget ($0.0001)"}, budget.Errors)
}
