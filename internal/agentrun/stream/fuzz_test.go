package stream

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
)

// FuzzParserChunking checks that any log reads the same to each agent's parser whether it
// comes in one write or in parts cut anywhere, inside a character's bytes too, both before
// and after a last line the log never ended is flushed; that a parser keeps none of the
// bytes it was handed; and that what it keeps stays in bounds: its events numbered from 1
// in order, each from a line inside the log, with its text cut to runevent.MaxText.
func FuzzParserChunking(f *testing.F) {
	for _, seed := range chunkingSeeds(f) {
		f.Add(seed.data, seed.cuts)
	}

	f.Fuzz(func(t *testing.T, data, cuts []byte) {
		for _, p := range fuzzedParsers() {
			whole, parts := p.open(p.dir), p.open(p.dir)

			n, err := whole.Write(data)
			require.NoError(t, err, p.name)
			assert.Equal(t, len(data), n, p.name)

			writeInParts(t, parts, data, cuts)
			assert.Equal(t, stateOf(whole), stateOf(parts), "%s: a log fed in parts reads as the log", p.name)

			whole.Flush()
			parts.Flush()
			assert.Equal(t, stateOf(whole), stateOf(parts), "%s: and so does a last line never ended", p.name)

			checkKept(t, whole, int64(len(data)))
		}
	})
}

// fuzzedParser is one agent's parser, as the fuzz test opens it, for a run in dir.
type fuzzedParser struct {
	name string
	open func(dir string) *Parser
	dir  string
}

// fuzzedParsers are every agent's parser, each in its fixtures' folder.
func fuzzedParsers() []fuzzedParser {
	return []fuzzedParser{{"claude", Claude, fixtureDir}, {"codex", Codex, codexDir}}
}

// writeInParts writes data to p in parts as long as cuts' bytes say, 0 for an empty write,
// then the rest in one.
func writeInParts(t *testing.T, p *Parser, data, cuts []byte) {
	t.Helper()

	for _, c := range cuts {
		size := min(int(c), len(data))
		writePart(t, p, data[:size])
		data = data[size:]
	}

	writePart(t, p, data)
}

// writePart writes part to p from a buffer of its own, overwritten once p has it, as a
// reader of a growing log reuses its buffer: a parser that kept any of it reads otherwise.
func writePart(t *testing.T, p *Parser, part []byte) {
	t.Helper()

	buf := append([]byte{}, part...)

	n, err := p.Write(buf)
	require.NoError(t, err)
	require.Equal(t, len(part), n)

	for i := range buf {
		buf[i] = '\n'
	}
}

// parserState is everything a parser tells of the log it read.
type parserState struct {
	Page      runevent.Page
	Events    []runevent.Event
	Result    Result
	Ended     bool
	Over      bool
	Limit     *Limit
	Init      Init
	Inited    bool
	Session   string
	LastWords string
}

// stateOf reads everything p tells of the log it read.
func stateOf(p *Parser) parserState {
	s := parserState{
		Page: p.Page(0), Events: p.Events(), Over: p.Over(), Limit: p.Limit(), Session: p.AgentSessionID(),
		LastWords: p.LastWords(),
	}

	s.Result, s.Ended = p.Result()
	s.Init, s.Inited = p.Init()

	return s
}

// checkKept checks that what p keeps of a log of size bytes is in bounds: its counts, its
// last events numbered in order up to the count, each from a line inside the log, in the
// log's order, its text cut to runevent.MaxText.
func checkKept(t *testing.T, p *Parser, size int64) {
	t.Helper()

	page := p.Page(0)
	progress := page.Progress

	assert.GreaterOrEqual(t, progress.Events, int64(0))
	assert.GreaterOrEqual(t, progress.Unknown, int64(0))
	assert.NotNil(t, progress.Todos, "a todo list is never nil")
	assert.LessOrEqual(t, len(progress.Step), 200)
	assert.LessOrEqual(t, len(page.Events), KeepEvents)
	assert.Equal(t, page.Events, p.Events())
	assert.Empty(t, p.Page(progress.Events).Events, "nothing comes after the last event")

	if len(page.Events) == 0 {
		assert.Zero(t, progress.Events, "an event is let go only for a later one")

		return
	}

	assert.Equal(t, page.Events[0].Seq, page.First)
	assert.Equal(t, page.Events[0].Offset, page.Before)

	first, offset := progress.Events-int64(len(page.Events))+1, int64(0)

	for i, e := range page.Events {
		assert.Equal(t, first+int64(i), e.Seq, "numbered in order")
		assert.GreaterOrEqual(t, e.Offset, offset, "in the log's order")
		assert.Less(t, e.Offset, size, "from a line inside the log")
		assert.GreaterOrEqual(t, e.Part, 0)
		assert.LessOrEqual(t, len(e.Text), runevent.MaxText)

		offset = e.Offset
	}
}

// chunkingSeed is a log and the parts it is cut in.
type chunkingSeed struct {
	data, cuts []byte
}

// chunkingSeeds are each line of the recorded streams in testdata, cut in growing parts,
// the shortest streams whole, and short logs of each agent's lines: none at all, a
// character cut between its bytes, a last line never ended, lines that aren't JSON, a CRLF
// and blank lines, a whole turn of each agent. A seed is kept small, so the fuzzer keeps
// its pace.
func chunkingSeeds(f *testing.F) []chunkingSeed {
	f.Helper()

	var seeds []chunkingSeed

	growing := []byte{1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 233}

	for _, pattern := range []string{"*.jsonl", filepath.Join("*", "*.jsonl")} {
		paths, err := filepath.Glob(filepath.Join("testdata", pattern))
		require.NoError(f, err)

		for _, path := range paths {
			data, err := os.ReadFile(path) //nolint:gosec // the package's own fixture
			require.NoError(f, err)

			if len(data) <= 4<<10 {
				seeds = append(seeds, chunkingSeed{data, growing})
			}

			for line := range bytes.Lines(data) {
				seeds = append(seeds, chunkingSeed{line, growing})
			}
		}
	}

	require.NotEmpty(f, seeds, "the recorded streams are seeds")

	// The first cut, 79 bytes in, falls between the two bytes of "é"; the third ends the line.
	text := `{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"Café ☕ done"}]}}` + "\n"
	require.False(f, utf8.RuneStart(text[79]), "the cut falls inside a character")
	require.Len(f, text, 79+16)

	return append(seeds,
		chunkingSeed{nil, nil},
		chunkingSeed{[]byte(text), bytes.Repeat([]byte{1}, len(text))},
		chunkingSeed{[]byte(text + text), []byte{79, 0, 16}},
		chunkingSeed{[]byte(`{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"half a line"}}`), []byte{10}},
		chunkingSeed{[]byte("not json\n{\"type\":\"assistant\",\"message\":\n{\"type\":\"result\",\"subtype\":\n\xff\xfe\n{}\n[]\nnull\n"), []byte{3, 0, 17}},
		chunkingSeed{[]byte(claudeTurn()), []byte{40, 0, 1, 255, 7}},
		chunkingSeed{[]byte(codexTurn()), []byte{5, 200, 1, 1, 64}},
	)
}

// claudeTurn is a short turn of the first agent's lines: its init after a CRLF and blank
// lines, a tool and its result, a todo list, a limit's warning, and its result.
func claudeTurn() string {
	return "\r\n\n  \n" +
		`{"type":"system","subtype":"init","model":"a-model","claude_code_version":"2.1.325","apiKeySource":"none",` +
		`"session_id":"s1","tools":["Read","Bash"],"mcp_servers":[{"name":"m","status":"connected"}]}` + "\r\n" +
		`{"type":"assistant","timestamp":"2026-11-20T10:00:00.5+02:00","message":{"id":"m1","usage":{"input_tokens":3,` +
		`"output_tokens":4,"cache_read_input_tokens":5},"content":[{"type":"tool_use","name":"Read",` +
		`"input":{"file_path":"/work/example-repo/main.go"}},{"type":"thinking"}]}}` + "\n" +
		`{"type":"user","message":{"content":[{"type":"tool_result","content":[{"type":"text","text":"ok"}],"is_error":true}]}}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"TodoWrite","input":{"todos":[` +
		`{"content":"Read","status":"completed"},{"content":"Fix","status":"in_progress","activeForm":"Fixing"}]}}]}}` + "\n" +
		`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning","rateLimitType":"seven_day","resetsAt":1}}` + "\n" +
		`{"type":"system","subtype":"api_retry","attempt":1,"max_retries":3,"error":"overloaded"}` + "\n" +
		`{"type":"result","subtype":"success","result":"Done","structured_output":{"outcome":"fixed"},` +
		`"total_cost_usd":0.01,"model_usage_since_last_result":{"a-model":{"costUSD":0.01}},"num_turns":2}` + "\n"
}

// codexTurn is a short turn of the second agent's lines: its thread, a command that failed,
// a todo list, a retry, its last words, and its end, then a turn refused for its usage.
func codexTurn() string {
	return `{"type":"thread.started","thread_id":"t1"}` + "\n" +
		`{"type":"turn.started"}` + "\n" +
		`{"type":"item.started","item":{"id":"c1","type":"command_execution","command":"/bin/zsh -lc 'go test ./...'"}}` + "\n" +
		`{"type":"item.updated","item":{"id":"c1","type":"command_execution"}}` + "\n" +
		`{"type":"item.completed","item":{"id":"c1","type":"command_execution","aggregated_output":"FAIL\n",` +
		`"exit_code":1,"status":"failed"}}` + "\n" +
		`{"type":"item.started","item":{"id":"l1","type":"todo_list","items":[{"text":"Read","completed":true},` +
		`{"text":"Fix","completed":false}]}}` + "\n" +
		`{"type":"error","message":"{\"error\":{\"message\":\"Reconnecting\"}}"}` + "\n" +
		`{"type":"item.completed","item":{"id":"a1","type":"agent_message","text":"{\"outcome\":\"fixed\"}"}}` + "\n" +
		`{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":4,"output_tokens":2}}` + "\n" +
		`{"type":"turn.failed","error":{"message":"You've hit your usage limit."}}` + "\n"
}
