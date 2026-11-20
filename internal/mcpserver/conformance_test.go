package mcpserver_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/brand"
)

// recordedClient returns the lines Claude Code 2.1.328 sent a stdio MCP server in the
// spike, as recorded: the newer protocol's discovery probe, initialize, initialized, the
// tool list, and a turn's tool calls, the last two with a proposal of the spike's own
// shape, which no build reads.
func recordedClient(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "mcp-client.jsonl"))
	require.NoError(t, err)

	return string(data)
}

// recordedIDs are the IDs of the recorded client's requests, as it wrote them: a string
// for the probe, and numbers after it. Its one notification has none.
func recordedIDs() []string {
	return []string{`"server-discover-probe-1"`, "0", "1", "2", "3", "4", "5", "6", "7", "8"}
}

func TestTheRecordedClientIsServedInBothPlaces(t *testing.T) {
	t.Parallel()

	for _, w := range places() {
		t.Run(string(w), func(t *testing.T) {
			t.Parallel()

			r := newRig(t)

			code, replies, stderr := serveAll(t, r.args(w), r.host(), recordedClient(t))
			assert.Zero(t, code, "stdin's end ends the session")
			assert.Empty(t, stderr)
			require.Len(t, replies, len(recordedIDs()), "every request answered, the notification not")

			for i, id := range recordedIDs() {
				assert.Equal(t, id, string(replies[i].ID), "reply %d echoes its request's ID as it came", i)
			}

			probe := replies[0]
			require.NotNil(t, probe.Error, "the newer protocol's probe is a method the tool mode doesn't have")
			assert.Equal(t, -32601, probe.Error.Code, "so the client falls back to initialize")

			assertInitialized(t, replies[1], w)
			assertListed(t, replies[2], w)
			assertTheTurn(t, r, w, replies[3:])
		})
	}
}

// assertInitialized checks initialize's answer: the client's version, which the tool
// mode speaks, tools alone, the server's name, and its instructions where w says.
func assertInitialized(t *testing.T, r reply, w kit.Where) {
	t.Helper()

	require.Nil(t, r.Error)

	var got struct {
		ProtocolVersion string                     `json:"protocolVersion"`
		Capabilities    map[string]json.RawMessage `json:"capabilities"`
		ServerInfo      map[string]string          `json:"serverInfo"`
		Instructions    string                     `json:"instructions"`
	}

	require.NoError(t, json.Unmarshal(r.Result, &got))
	assert.Equal(t, "2025-11-25", got.ProtocolVersion)
	assert.Equal(t, map[string]json.RawMessage{"tools": json.RawMessage("{}")}, got.Capabilities)
	assert.Equal(t, map[string]string{"name": brand.Slug, "version": "1"}, got.ServerInfo)
	assert.Equal(t, kit.ServerInstructions(w), got.Instructions)
}

// assertListed checks the tool list's answer: kit's tools where w says, on one page.
func assertListed(t *testing.T, r reply, w kit.Where) {
	t.Helper()

	require.Nil(t, r.Error)

	var got struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
		NextCursor *string `json:"nextCursor"`
	}

	require.NoError(t, json.Unmarshal(r.Result, &got))
	assert.Nil(t, got.NextCursor, "one page")

	names := make([]string, 0, len(got.Tools))
	for _, tool := range got.Tools {
		names = append(names, tool.Name)
	}

	assert.Equal(t, kit.Tools(w), names)
}

// assertTheTurn checks the answers to the recorded turn's tool calls: the guide, the
// setup, and the inbox's names read; two topics this build doesn't have, refused with the
// ones it has; and a proposal of the spike's shape, refused by the check and the proposal
// alike, with nothing written.
func assertTheTurn(t *testing.T, r rig, w kit.Where, replies []reply) {
	t.Helper()

	results := make([]toolResult, len(replies))

	for i, rep := range replies {
		if w == kit.Outside && i == 2 {
			require.NotNil(t, rep.Error, "outside, the inbox's names aren't a tool")
			assert.Equal(t, -32602, rep.Error.Code)
			assert.Contains(t, rep.Error.Message, kit.ToolInboxNames)

			continue
		}

		require.Nil(t, rep.Error, "a tool's answer is a result, a refusal too")
		require.NoError(t, json.Unmarshal(rep.Result, &results[i]))
		assert.NotContains(t, string(rep.Result), "structuredContent")
	}

	assert.Equal(t, kit.Guide(r.snap.Vocabulary), results[0].text(t))
	assert.False(t, results[0].IsError)
	assert.Equal(t, kit.SetupWords(r.snap, w), results[1].text(t))

	if w == kit.InApp {
		assert.Equal(t, kit.InboxWords(r.snap.Inbox), results[2].text(t))
	}

	for _, i := range []int{3, 4} {
		assert.True(t, results[i].IsError, "no such topic")
		assert.Contains(t, results[i].text(t), "`proposal`, `routine`, `watch`", "the refusal lists the topics")
	}

	for _, i := range []int{5, 6} {
		assert.True(t, results[i].IsError)
		assert.Contains(t, results[i].text(t), "The proposal can't be read")
	}

	assert.Contains(t, results[6].text(t), "Nothing was proposed")
	assert.Empty(t, r.files(t, w), "a refused proposal writes nothing")
}
