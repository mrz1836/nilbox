package mcpserver_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
)

// listedTool is one tool as tools/list gives it.
type listedTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema struct {
		Type       string `json:"type"`
		Properties map[string]struct {
			Type        string   `json:"type"`
			Description string   `json:"description"`
			Enum        []string `json:"enum"`
		} `json:"properties"`
		Required             []string `json:"required"`
		AdditionalProperties *bool    `json:"additionalProperties"`
	} `json:"inputSchema"`
}

// listTools returns the tools/list answer where w says, raw and read.
func listTools(t *testing.T, r rig, w kit.Where) (json.RawMessage, []listedTool) {
	t.Helper()

	_, replies, _ := serveAll(t, r.args(w), r.host(), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n")
	require.Len(t, replies, 1)
	require.Nil(t, replies[0].Error)

	var got struct {
		Tools []listedTool `json:"tools"`
	}

	require.NoError(t, json.Unmarshal(replies[0].Result, &got))

	return replies[0].Result, got.Tools
}

func TestTheToolListIsKitsToolsWithSmallInputs(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	for _, w := range places() {
		raw, tools := listTools(t, r, w)
		assert.Less(t, len(raw), 8<<10, "every request carries the list: keep it small (the spike's S11)")

		names := make([]string, 0, len(tools))
		byName := map[string]listedTool{}

		for _, tool := range tools {
			names = append(names, tool.Name)
			byName[tool.Name] = tool

			assert.NotEmpty(t, tool.Description, "%s says what it is for", tool.Name)
			assert.Equal(t, "object", tool.InputSchema.Type, tool.Name)

			if assert.NotNil(t, tool.InputSchema.AdditionalProperties, tool.Name) {
				assert.False(t, *tool.InputSchema.AdditionalProperties, "%s takes no key it doesn't list", tool.Name)
			}
		}

		assert.Equal(t, kit.Tools(w), names, "the tools kit's words name, in its order")

		topic := byName[kit.ToolReference].InputSchema
		assert.Equal(t, []string{"topic"}, topic.Required)
		assert.Equal(t, kit.Topics(), topic.Properties["topic"].Enum)

		setup := byName[kit.ToolMySetup].InputSchema
		assert.Empty(t, setup.Required, "the routine is optional")
		assert.Equal(t, "integer", setup.Properties["routine"].Type)

		for _, name := range []string{kit.ToolCheck, kit.ToolPropose} {
			input := byName[name].InputSchema
			assert.Equal(t, []string{"proposal"}, input.Required)
			assert.Equal(t, "object", input.Properties["proposal"].Type)
			assert.Contains(t, input.Properties["proposal"].Description, "topic `schema`",
				"the full schema is a topic, never the input's")
		}

		for _, name := range []string{kit.ToolGuide, kit.ToolInboxNames, kit.ToolProposalStatus} {
			assert.Empty(t, byName[name].InputSchema.Properties, "%s takes nothing", name)
		}
	}
}

// toolCall is one call of a tool, and the words it answers with.
type toolCall struct {
	tool string
	args any
	want string
}

// readingCalls are a call of each tool that reads, with the words kit gives for it where
// w says: the guide, each topic, the setup and one routine, a check, and in the app the
// inbox's names.
func readingCalls(t *testing.T, r rig, w kit.Where) []toolCall {
	t.Helper()

	clean := newRoutine("Tell me", checksFail(notify()))
	calls := []toolCall{
		{kit.ToolGuide, map[string]any{}, kit.Guide(r.snap.Vocabulary)},
		{kit.ToolGuide, nil, kit.Guide(r.snap.Vocabulary)},
		{kit.ToolMySetup, map[string]any{}, kit.SetupWords(r.snap, w)},
		{kit.ToolMySetup, map[string]any{"routine": nil}, kit.SetupWords(r.snap, w)},
		{kit.ToolMySetup, map[string]any{"routine": 7}, routineWords(t, r.snap, w, 7)},
		{kit.ToolCheck, proposalArgs(t, clean), kit.Check(t.Context(), r.snap, clean).Words()},
	}

	for _, topic := range kit.Topics() {
		text, ok := kit.Topic(topic, r.snap.Vocabulary)
		require.True(t, ok)

		calls = append(calls, toolCall{kit.ToolReference, map[string]any{"topic": topic}, text})
	}

	if w == kit.InApp {
		calls = append(calls, toolCall{kit.ToolInboxNames, map[string]any{}, kit.InboxWords(r.snap.Inbox)})
	}

	return calls
}

func TestEveryToolAnswersWithKitsWordsInBothPlaces(t *testing.T) {
	t.Parallel()

	for _, w := range places() {
		t.Run(string(w), func(t *testing.T) {
			t.Parallel()

			r := newRig(t)
			calls := readingCalls(t, r, w)

			var stdin strings.Builder

			for i, c := range calls {
				stdin.Write(mustJSON(t, map[string]any{
					"jsonrpc": "2.0", "id": i, "method": "tools/call", "params": map[string]any{"name": c.tool, "arguments": c.args},
				}))
				stdin.WriteString("\n")
			}

			_, replies, _ := serveAll(t, r.args(w), r.host(), stdin.String())
			require.Len(t, replies, len(calls))

			for i, c := range calls {
				res := resultOf(t, replies[i])
				assert.False(t, res.IsError, "%s %v", c.tool, c.args)
				assert.Equal(t, c.want, res.text(t), "%s %v", c.tool, c.args)
				assert.Less(t, len(res.text(t)), 60<<10, "a larger result never reaches the agent (the spike's S11)")
			}
		})
	}
}

// routineWords returns kit's words for the routine id where w says.
func routineWords(t *testing.T, snap kit.Snapshot, w kit.Where, id int64) string {
	t.Helper()

	text, ok := kit.RoutineWords(snap, w, id)
	require.True(t, ok)

	return text
}

// resultOf reads a tools/call reply's result.
func resultOf(t *testing.T, r reply) toolResult {
	t.Helper()

	require.Nil(t, r.Error)

	var res toolResult

	require.NoError(t, json.Unmarshal(r.Result, &res))

	return res
}

func TestAnOutsideSessionReadsLessOfTheSetup(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	_, replies, _ := serveAll(t, r.args(kit.Outside), r.host(),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"autopilot_my_setup"}}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"autopilot_inbox_names"}}`+"\n")
	require.Len(t, replies, 2)

	setup := resultOf(t, replies[0]).text(t)
	assert.Equal(t, kit.SetupWords(r.snap.Outside(), kit.Outside), setup)
	assert.Contains(t, setup, "Nightly")

	for _, private := range []string{`"Lint"`, "acme-org/api", "dependabot", `"work"`, "Tells me when my checks fail"} {
		assert.NotContains(t, setup, private, "no script, rule, profile, or description outside")
	}

	require.NotNil(t, replies[1].Error, "nor the inbox's names, which aren't a tool there")
	assert.Equal(t, -32602, replies[1].Error.Code)
}

func TestCheckAndProposeReadTheWholeSnapshotOutsideToo(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	// A routine that runs the script Lint, which only the whole snapshot holds.
	p := newRoutine("Lint my pull requests", checksFail(lintScript()))
	whole := kit.Check(t.Context(), r.snap, p)
	require.Zero(t, whole.Problems())
	require.Positive(t, kit.Check(t.Context(), r.snap.Outside(), p).Problems(), "the outside view holds no script")

	_, replies, _ := serveAll(t, r.args(kit.Outside), r.host(),
		string(mustJSON(t, map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "tools/call",
			"params": map[string]any{"name": kit.ToolCheck, "arguments": proposalArgs(t, p)},
		}))+"\n"+string(mustJSON(t, map[string]any{
			"jsonrpc": "2.0", "id": 2, "method": "tools/call",
			"params": map[string]any{"name": kit.ToolPropose, "arguments": proposalArgs(t, p)},
		}))+"\n")
	require.Len(t, replies, 2)

	assert.Equal(t, whole.Words(), resultOf(t, replies[0]).text(t), "checked as the app checks its card")

	proposed := resultOf(t, replies[1])
	assert.False(t, proposed.IsError, proposed.text(t))
	assert.Len(t, r.files(t, kit.Outside), 1)
}

func TestToolArgumentsAreReadStrictly(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	clean := newRoutine("Tell me", checksFail(notify()))

	tests := []struct {
		name string
		tool string
		args string
		says string
	}{
		{"no topic", kit.ToolReference, `{}`, "Name the topic to read"},
		{"a topic that isn't text", kit.ToolReference, `{"topic":5}`, "Name the topic to read"},
		{"a topic there isn't", kit.ToolReference, `{"topic":"routines"}`, `There is no topic "routines"`},
		{"a key the tool doesn't take", kit.ToolReference, `{"topic":"steps","verbose":true}`, `"verbose" isn't one of them`},
		{"arguments that aren't an object", kit.ToolGuide, `"read it"`, "they aren't one JSON object"},
		{"an argument to a tool that takes none", kit.ToolGuide, `{"topic":"steps"}`, "takes no arguments"},
		{"a routine's id as text", kit.ToolMySetup, `{"routine":"7"}`, "a whole number"},
		{"a routine's id that isn't whole", kit.ToolMySetup, `{"routine":7.5}`, "a whole number"},
		{"a routine there isn't", kit.ToolMySetup, `{"routine":99}`, "no routine with id 99"},
		{"no proposal", kit.ToolCheck, `{}`, "Give the proposal as `proposal`"},
		{"a proposal that isn't an object", kit.ToolCheck, `{"proposal":[1]}`, "Give the proposal as `proposal`"},
		{"a proposal of nothing", kit.ToolCheck, `{"proposal":{"summary":"Nothing."}}`, "it proposes nothing"},
		{"the proposal's fields as arguments", kit.ToolPropose, `{"summary":"Adds it."}`, `"summary" isn't one of them`},
		{
			"Autopilot's switch", kit.ToolPropose, `{"proposal":{"summary":"On.","settings":{"enabled":true}}}`,
			"Autopilot's switch is the user's alone",
		},
	}

	var stdin strings.Builder

	for i, tt := range tests {
		stdin.WriteString(`{"jsonrpc":"2.0","id":` + string(mustJSON(t, i)) + `,"method":"tools/call","params":{"name":"` +
			tt.tool + `","arguments":` + tt.args + "}}\n")
	}

	// A proposal given as the JSON of one, in a string, is read as the proposal.
	stdin.WriteString(string(mustJSON(t, map[string]any{
		"jsonrpc": "2.0", "id": "text", "method": "tools/call",
		"params": map[string]any{"name": kit.ToolCheck, "arguments": map[string]any{"proposal": string(mustJSON(t, clean))}},
	})) + "\n")

	_, replies, _ := serveAll(t, r.args(kit.InApp), r.host(), stdin.String())
	require.Len(t, replies, len(tests)+1)

	for i, tt := range tests {
		res := resultOf(t, replies[i])
		assert.True(t, res.IsError, tt.name)
		assert.Contains(t, res.text(t), tt.says, tt.name)
	}

	text := resultOf(t, replies[len(tests)])
	assert.False(t, text.IsError)
	assert.Equal(t, kit.Check(t.Context(), r.snap, clean).Words(), text.text(t))
	assert.Empty(t, r.files(t, kit.InApp), "a refused proposal writes nothing")
}

func TestAToolThatNeedsTheSnapshotSaysWhyItCantReadIt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		write func(t *testing.T, path string)
		says  string
	}{
		{"none written yet", func(*testing.T, string) {}, "The user's setup isn't there to read yet"},
		{"not JSON", writeText("{not json"), "The user's setup can't be read: the snapshot can't be read"},
		{"another version", writeText(`{"version":1}`), "the snapshot is of another version"},
		{"a key it doesn't have", writeText(`{"version":2,"inbox":{"titles":[]}}`), "The user's setup can't be read"},
		{"a folder", func(t *testing.T, path string) {
			t.Helper()
			require.NoError(t, os.Mkdir(path, 0o700))
		}, "not a regular file"},
		{"larger than a snapshot may be", func(t *testing.T, path string) {
			t.Helper()
			require.NoError(t, os.WriteFile(path, []byte(`{"version":2}`), 0o600))
			require.NoError(t, os.Truncate(path, kit.MaxSnapshotBytes+1))
		}, "larger than"},
	}

	// The tools that read the snapshot, each with arguments it takes.
	reads := map[string]string{
		kit.ToolGuide: `{}`, kit.ToolReference: `{"topic":"steps"}`, kit.ToolMySetup: `{}`, kit.ToolInboxNames: `{}`,
		kit.ToolCheck:   `{"proposal":` + string(mustJSON(t, newRoutine("Tell me", checksFail(notify())))) + `}`,
		kit.ToolPropose: `{"proposal":` + string(mustJSON(t, newRoutine("Tell me", checksFail(notify())))) + `}`,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newRig(t)
			path := filepath.Join(r.dir, "context.json")
			require.NoError(t, os.Remove(path))
			tt.write(t, path)

			var stdin strings.Builder

			tools := make([]string, 0, len(reads)+1)

			for tool, args := range reads {
				tools = append(tools, tool)
				stdin.WriteString(`{"jsonrpc":"2.0","id":` + string(mustJSON(t, len(tools))) +
					`,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + args + "}}\n")
			}

			stdin.WriteString(`{"jsonrpc":"2.0","id":0,"method":"tools/call","params":{"name":"autopilot_proposal_status"}}` + "\n")

			_, replies, _ := serveAll(t, r.args(kit.InApp), r.host(), stdin.String())
			require.Len(t, replies, len(tools)+1)

			for i, tool := range tools {
				res := resultOf(t, replies[i])
				assert.True(t, res.IsError, tool)
				assert.Contains(t, res.text(t), tt.says, tool)
				assert.Contains(t, res.text(t), "Ask the user to open", tool)
				assert.NotContains(t, res.text(t), r.dir, "%s: never where the app keeps the user's data", tool)
			}

			status := resultOf(t, replies[len(tools)])
			assert.False(t, status.IsError, "the status reads no snapshot")
			assert.Empty(t, r.files(t, kit.InApp))
		})
	}
}

// writeText returns a writer of text at a path.
func writeText(text string) func(t *testing.T, path string) {
	return func(t *testing.T, path string) {
		t.Helper()
		require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
	}
}

func TestTheSnapshotIsReadAgainAtEachCall(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	changed := fixtureSnapshot()
	changed.Setup.Routines[0].Name = "Nightly checks"

	synctest.Test(t, func(t *testing.T) {
		s := startSession(t, r.args(kit.InApp), r.host())

		first := s.call(kit.ToolMySetup, map[string]any{})
		assert.Contains(t, first.text(t), `"Nightly"`)

		r.writeSnapshot(t, changed)

		second := s.call(kit.ToolMySetup, map[string]any{})
		assert.Contains(t, second.text(t), `"Nightly checks"`, "the app writes it again as the setup changes")

		assert.Zero(t, s.close())
	})
}
