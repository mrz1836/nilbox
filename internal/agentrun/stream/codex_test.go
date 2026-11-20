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

// Codex's parser over S6's fixtures (codex-cli 0.163.0, in testdata/codex).

// The S6 fixtures' worktree, and the success fixture's thread.
const (
	codexDir    = "/tmp/example-repo/." + brand.Slug + "/worktrees/example-repo/pr-1"
	codexThread = "01a1e66a-b48a-7f43-a547-901d3ca2bb33"
)

// codexParsed returns a Codex parser fed an S6 fixture whole.
func codexParsed(t *testing.T, name string) *Parser {
	t.Helper()

	p := Codex(codexDir)
	_, err := p.Write(fixture(t, filepath.Join("codex", name)))
	require.NoError(t, err)

	return p
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
	assert.Nil(t, r.TurnCost)
	assert.False(t, r.Estimated)
	assert.Equal(t, int64(92974-73344), r.TokensIn, "its input counts what it read from the cache within it")
	assert.Equal(t, int64(73344), r.CacheRead)
	assert.Equal(t, int64(462), r.TokensOut)

	got := reportOf(t, r.Report)
	assert.Equal(t, "fixed", got.Outcome, "the report is its last message, in the strict schema")
	assert.Equal(t, "Updated add.go so Add returns a + b. `go test ./...` passed.", got.Summary)

	_, ok = p.Init()
	assert.False(t, ok, "Codex's stream has no init line")
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
	pushed := reportOf(t, r.Report)
	assert.True(t, pushed.Pushed)
	assert.Equal(t, "5bc5674913c19eb0aa26d17635645b45b1ff5dbd", pushed.PushedCommit)

	review := codexParsed(t, "review-readonly-note.jsonl")
	r, _ = review.Result()
	assert.Nil(t, r.Report, "a message that isn't the schema's JSON is no report")
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
	assert.Equal(t, "$ sleep 45", sigint.LastWords(), "what a run that never reported last said")
}

func TestTheCodexParserReadsTheUsageLimit(t *testing.T) {
	t.Parallel()

	for _, lines := range []string{
		`{"type":"error","message":"You've hit your usage limit. Try again at 3:04 PM."}` + "\n" +
			`{"type":"turn.failed","error":{"message":"You've hit your usage limit. Try again at 3:04 PM."}}` + "\n",
		`{"type":"turn.failed","error":{"message":"{\"error\":{\"message\":\"Quota exceeded\",\"code\":\"quota_exceeded\"}}"}}` + "\n",
		`{"type":"turn.failed","error":{"message":"workspace_owner_credits_depleted"}}` + "\n",
	} {
		p := Codex("")
		_, _ = p.Write([]byte(`{"type":"thread.started","thread_id":"` + codexThread + `"}` + "\n" + lines))

		limit := p.Limit()
		require.NotNil(t, limit, lines)
		assert.Equal(t, LimitRejected, limit.Status)

		r, ok := p.Result()
		require.True(t, ok)
		assert.True(t, r.IsError)
		assert.Equal(t, "usage_limit", r.Subtype, "a turn its usage refused says so")
		assert.Contains(t, p.Page(0).Events[len(p.Page(0).Events)-1].Text, "out of usage")
	}
}

func TestTheCodexParserReadsItsTodosAndToolsOnce(t *testing.T) {
	t.Parallel()

	p := Codex(codexDir)
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

func TestTheCodexParserShowsWhatAToolGaveBack(t *testing.T) {
	t.Parallel()

	p := Codex(codexDir)
	_, _ = p.Write([]byte(
		`{"type":"item.started","item":{"id":"m","type":"mcp_tool_call","server":"docs","tool":"search"}}` + "\n" +
			`{"type":"item.updated","item":{"id":"m","type":"mcp_tool_call","server":"docs","tool":"search"}}` + "\n" +
			`{"type":"item.completed","item":{"id":"m","type":"mcp_tool_call","server":"docs","tool":"search",` +
			`"status":"completed","result":{"hits":2}}}` + "\n" +
			`{"type":"item.completed","item":{"id":"f","type":"file_change","status":"declined",` +
			`"changes":[{"path":"` + codexDir + `/add.go","kind":"update"}]}}` + "\n" +
			`{"type":"item.completed","item":{"id":"a","type":"agent_message","text":"   "}}` + "\n",
	))

	page := p.Page(0)
	require.Len(t, page.Events, 4, "a tool shows once, however often it is updated, and an empty message not at all")
	assert.Equal(t, "docs.search", page.Events[0].Text)
	assert.Equal(t, `{"hits":2}`, page.Events[1].Text, "what an MCP tool gave back")
	assert.False(t, page.Events[1].Error)
	assert.Equal(t, "add.go", page.Events[2].Text)
	assert.Equal(t, runevent.KindToolResult, page.Events[3].Kind)
	assert.Equal(t, "declined", page.Events[3].Text, "a change its fence declined says so")
	assert.True(t, page.Events[3].Error)
	assert.Equal(t, "Editing add.go", p.Progress().Step)
}

func TestTheCodexParserWritesACommandAsItWasTyped(t *testing.T) {
	t.Parallel()

	for command, want := range map[string]string{
		`/bin/zsh -lc 'git add add.go'`:      "git add add.go",
		`/bin/zsh -lc 'echo '\''hi'\'''`:     "echo 'hi'",
		`/bin/bash -lc "echo \"\$HOME\""`:    `echo "$HOME"`,
		`/bin/zsh -lc ls`:                    "ls",
		`/usr/bin/env bash -lc 'git status'`: `/usr/bin/env bash -lc 'git status'`,
		`git status`:                         "git status",
		`/bin/zsh -lc '`:                     "'",
	} {
		assert.Equal(t, want, codexCommand(command), command)
	}
}

func TestTheCodexParserCountsWhatItCantRead(t *testing.T) {
	t.Parallel()

	p := Codex(codexDir)
	_, _ = p.Write([]byte("not json\n" +
		`{"type":"item.completed"}` + "\n" +
		`{"type":"item.completed","item":{"id":"x","type":"a_newer_item"}}` + "\n" +
		`{"type":"item.completed","item":{"id":"e","type":"error","message":"hooks bypassed"}}` + "\n"))

	got := p.Progress()
	assert.Equal(t, int64(3), got.Unknown, "each is counted, never fatal")
	assert.Zero(t, got.Events, "and a warning shows nothing either")
}
