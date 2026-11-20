package mcpserver_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/mcpserver"
)

// nullID is the ID of a reply to a message whose ID can't be read.
const nullID = "null"

// untouched is a stdin or stdout that fails the test when the tool mode uses it.
type untouched struct {
	t *testing.T
}

// Read fails the test: nothing may be read.
func (u untouched) Read([]byte) (int, error) {
	u.t.Error("the tool mode read stdin")

	return 0, io.EOF
}

// Write fails the test: nothing may be written.
func (u untouched) Write(p []byte) (int, error) {
	u.t.Errorf("the tool mode wrote %q", p)

	return len(p), nil
}

func TestOtherArgumentsAreNoToolModeCall(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{nil, {"--data-dir=/data"}, {"--conversation=" + conversationID}, {"--fence-hook-v1=/run"}} {
		code, handled := mcpserver.Serve(args, untouched{t}, untouched{t}, untouched{t})
		assert.False(t, handled, "%q", args)
		assert.Zero(t, code)
	}
}

func TestACallTheToolModeCantServeIsRefusedBeforeItReadsAnything(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	tests := []struct {
		name string
		args []string
		says string
	}{
		{"a protocol this build doesn't know", []string{"--mcp-server-v2=" + dir}, `the tool mode's protocol of "--mcp-server-v2"`},
		{"a later version of the first", []string{"--mcp-server-v10=" + dir}, `"--mcp-server-v10"`},
		{"no version", []string{"--mcp-server"}, `the tool mode's protocol of "--mcp-server"`},
		{"no folder", []string{mcpserver.Flag}, `the tool mode's protocol of "--mcp-server-v1"`},
		{"an empty folder", []string{mcpserver.Arg("")}, "isn't an absolute path"},
		{"a relative folder", []string{mcpserver.Arg("data/describe")}, "isn't an absolute path"},
		{"the folder twice", []string{mcpserver.Arg(dir), mcpserver.Arg(dir)}, "given twice"},
		{"a conversation that isn't one", []string{mcpserver.Arg(dir), mcpserver.ConversationArg("../../elsewhere")}, "isn't one"},
		{"an empty conversation", []string{mcpserver.Arg(dir), mcpserver.ConversationArg("")}, "isn't one"},
		{"a conversation flag alone", []string{mcpserver.Arg(dir), mcpserver.ConversationFlag}, "names no conversation"},
		{
			"the conversation twice",
			[]string{mcpserver.Arg(dir), mcpserver.ConversationArg(conversationID), mcpserver.ConversationArg(conversationID)},
			"given twice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer

			code, handled := mcpserver.Serve(tt.args, untouched{t}, untouched{t}, &stderr)
			assert.True(t, handled, "refused, never the app")
			assert.Equal(t, 2, code)
			assert.Contains(t, stderr.String(), tt.says)
			assert.Equal(t, 1, strings.Count(stderr.String(), "\n"), "one line")
		})
	}
}

func TestEachMessageIsAnsweredAsItComesWithStdinOpen(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	synctest.Test(t, func(t *testing.T) {
		s := startSession(t, r.args(kit.InApp), r.host())

		initialized := s.request("initialize", map[string]any{"protocolVersion": "2025-06-18"})
		require.Nil(t, initialized.Error)
		assert.Contains(t, string(initialized.Result), `"protocolVersion":"2025-06-18"`)

		cancel := "notifications/cancelled" //nolint:misspell // MCP spells its method so

		s.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
		s.send(map[string]any{"jsonrpc": "2.0", "method": cancel, "params": map[string]any{"requestId": 1}})

		pong := s.request("ping", nil)
		require.Nil(t, pong.Error, "the notifications got no answer: the next is the ping's")
		assert.JSONEq(t, "{}", string(pong.Result))

		guide := s.call(kit.ToolGuide, map[string]any{})
		assert.Equal(t, kit.Guide(r.snap.Vocabulary), guide.text(t))

		assert.Zero(t, s.close(), "stdin's end ends the session")
		assert.Empty(t, s.stderr.String())
	})
}

func TestMalformedMessagesAreRefusedAndTheSessionGoesOn(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	propose := `{"name":"autopilot_propose","arguments":{"proposal":` + string(mustJSON(t, newRoutine("Tell me", checksFail(notify())))) + `}}`

	tests := []struct {
		line string
		id   string // the ID the reply echoes; "" for no reply
		code int    // the reply's error code; 0 for a result
	}{
		{line: `not json`, id: nullID, code: -32700},
		{line: `{"jsonrpc":"2.0","id":1,"method":"ping"`, id: nullID, code: -32700},
		{line: `{"jsonrpc":"2.0","id":1,"method":"ping"} {"jsonrpc":"2.0","id":2,"method":"ping"}`, id: nullID, code: -32700},
		{line: `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, id: nullID, code: -32600},
		{line: `"ping"`, id: nullID, code: -32600},
		{line: `null`, id: nullID, code: -32600},
		{line: `{"jsonrpc":"2.0","id":{"n":1},"method":"ping"}`, id: nullID, code: -32600},
		{line: `{"jsonrpc":"2.0","id":null,"method":"ping"}`, id: nullID, code: -32600},
		{line: `{"jsonrpc":"2.0","id":true,"method":"ping"}`, id: nullID, code: -32600},
		{line: `{"jsonrpc":"1.0","id":2,"method":"ping"}`, id: "2", code: -32600},
		{line: `{"id":3,"method":"ping"}`, id: "3", code: -32600},
		{line: `{"jsonrpc":"2.0","id":4}`, id: "4", code: -32600},
		{line: `{"jsonrpc":"2.0","id":5,"method":7}`, id: "5", code: -32600},
		{line: `{"jsonrpc":"2.0","id":6,"result":{}}`},
		{line: `{"jsonrpc":"2.0","id":7,"error":{"code":-1,"message":"no"}}`},
		{line: `{"jsonrpc":"2.0","method":"tools/call","params":` + propose + `}`},
		{line: `{"jsonrpc":"2.0","method":"bogus"}`},
		{line: `{"jsonrpc":"1.0","method":"ping"}`},
		{line: ``},
		{line: " \t "},
		{line: `{"jsonrpc":"2.0","id":8,"method":"resources/list"}`, id: "8", code: -32601},
		{line: `{"jsonrpc":"2.0","id":9,"method":"tools/call"}`, id: "9", code: -32602},
		{line: `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":"guide"}`, id: "10", code: -32602},
		{line: `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"no_such_tool"}}`, id: "11", code: -32602},
		{line: `{"jsonrpc":"2.0","id":12,"method":"ping"}` + "\r", id: "12"},
		{line: `{"jsonrpc":"2.0","id":"last","method":"ping"}`, id: `"last"`},
	}

	var stdin strings.Builder

	var want []int

	for i, tt := range tests {
		stdin.WriteString(tt.line + "\n")

		if tt.id != "" {
			want = append(want, i)
		}
	}

	code, replies, _ := serveAll(t, r.args(kit.InApp), r.host(), stdin.String())
	assert.Zero(t, code)
	require.Len(t, replies, len(want), "a reply for each request, none for a notification, a response, or an empty line")

	for i, at := range want {
		tt := tests[at]

		assert.Equal(t, tt.id, string(replies[i].ID), "%s", tt.line)

		if tt.code == 0 {
			assert.Nil(t, replies[i].Error, "%s", tt.line)

			continue
		}

		if assert.NotNil(t, replies[i].Error, "%s", tt.line) {
			assert.Equal(t, tt.code, replies[i].Error.Code, "%s", tt.line)
			assert.NotEmpty(t, replies[i].Error.Message)
		}
	}

	assert.Empty(t, r.files(t, kit.InApp), "a tool call sent as a notification runs nothing")
}

func TestIDsAreEchoedAsTheyCame(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	ids := []string{`"abc"`, `"two words"`, `""`, `0`, `-1`, `1.5`, `12345678901234567890`, `1e3`}

	var stdin strings.Builder

	for _, id := range ids {
		stdin.WriteString(`{"jsonrpc":"2.0","id":` + id + `,"method":"ping"}` + "\n")
	}

	_, replies, _ := serveAll(t, r.args(kit.Outside), r.host(), stdin.String())
	require.Len(t, replies, len(ids))

	for i, id := range ids {
		assert.Equal(t, id, string(replies[i].ID))
	}
}

func TestALineLargerThan4MBIsRefusedAndTheNextIsServed(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	// padded returns a ping with id, exactly size bytes long.
	padded := func(id string, size int) string {
		head := `{"jsonrpc":"2.0","id":` + id + `,"method":"ping","params":{"pad":"`
		tail := `"}}`

		return head + strings.Repeat("x", size-len(head)-len(tail)) + tail
	}

	stdin := padded("1", mcpserver.MaxLine) + "\n" + // the most a line may be
		padded("2", mcpserver.MaxLine+1) + "\n" + // one byte more
		`{"jsonrpc":"2.0","id":3,"method":"ping"}` + "\n" +
		padded("4", mcpserver.MaxLine+1) // the last line, with no newline

	code, replies, stderr := serveAll(t, r.args(kit.InApp), r.host(), stdin)
	assert.Zero(t, code)
	assert.Empty(t, stderr)
	require.Len(t, replies, 4)

	assert.Equal(t, "1", string(replies[0].ID))
	assert.Nil(t, replies[0].Error, "a line of 4 MB is read")

	for _, i := range []int{1, 3} {
		assert.Equal(t, nullID, string(replies[i].ID), "a line past 4 MB is dropped unread")
		require.NotNil(t, replies[i].Error)
		assert.Equal(t, -32600, replies[i].Error.Code)
		assert.Contains(t, replies[i].Error.Message, "larger than 4 MB")
	}

	assert.Equal(t, "3", string(replies[2].ID), "the line after it is read whole")
	assert.Nil(t, replies[2].Error)
}

func TestStdinsEndEndsTheSession(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	code, replies, stderr := serveAll(t, r.args(kit.InApp), r.host(), "")
	assert.Zero(t, code, "a session with no message")
	assert.Empty(t, replies)
	assert.Empty(t, stderr)

	code, replies, _ = serveAll(t, r.args(kit.InApp), r.host(), `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	assert.Zero(t, code)
	require.Len(t, replies, 1, "a last line without a newline is a line")
	assert.Nil(t, replies[0].Error)
}

func TestInitializeSpeaksTheClientsVersionWhenItCan(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	tests := []struct {
		params string
		want   string
	}{
		{`{"protocolVersion":"2025-11-25"}`, "2025-11-25"},
		{`{"protocolVersion":"2025-06-18"}`, "2025-06-18"},
		{`{"protocolVersion":"2025-03-26"}`, "2025-03-26"},
		{`{"protocolVersion":"2024-11-05"}`, "2024-11-05"},
		{`{"protocolVersion":"2026-07-28"}`, "2025-11-25"},
		{`{"protocolVersion":"2025-01-01"}`, "2025-11-25"},
		{`{"protocolVersion":20251125}`, "2025-11-25"},
		{`{}`, "2025-11-25"},
		{`"2025-06-18"`, "2025-11-25"},
		{`null`, "2025-11-25"},
	}

	for _, tt := range tests {
		_, replies, _ := serveAll(t, r.args(kit.InApp), r.host(),
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":`+tt.params+`}`+"\n")
		require.Len(t, replies, 1)
		require.Nil(t, replies[0].Error, tt.params)

		var got struct {
			ProtocolVersion string `json:"protocolVersion"`
		}

		require.NoError(t, json.Unmarshal(replies[0].Result, &got))
		assert.Equal(t, tt.want, got.ProtocolVersion, tt.params)
	}
}

// errBroken is a stream that fails.
var errBroken = errors.New("broken pipe")

// broken is a stdin or stdout that fails.
type broken struct{}

// Read fails.
func (broken) Read([]byte) (int, error) { return 0, errBroken }

// Write fails.
func (broken) Write([]byte) (int, error) { return 0, errBroken }

func TestAStreamThatFailsEndsTheSessionOnStderr(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	var stderr bytes.Buffer

	code, handled := mcpserver.ServeOn(r.args(kit.InApp), broken{}, untouched{t}, &stderr, r.host())
	assert.True(t, handled)
	assert.Equal(t, 1, code)
	assert.Equal(t, "Read a message: broken pipe.\n", stderr.String())

	stderr.Reset()

	code, _ = mcpserver.ServeOn(r.args(kit.InApp), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n"),
		broken{}, &stderr, r.host())
	assert.Equal(t, 1, code)
	assert.Equal(t, "Write an answer: broken pipe.\n", stderr.String())
}
