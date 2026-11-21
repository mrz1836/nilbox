package mcpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FuzzReadRequest checks that any line reads as a request, a notification, nothing, or the
// refusal JSON-RPC gives it: a parse error for a line that isn't JSON, and for no other,
// and an invalid request otherwise, each written as one line; and that a request or a
// notification a client writes reads back with its ID, its method, and its params, as a
// request refused for its version is refused with its ID.
func FuzzReadRequest(f *testing.F) {
	for _, seed := range requestSeeds(f) {
		f.Add([]byte(seed.line), seed.method, seed.id, seed.num, seed.shape)
	}

	f.Fuzz(func(t *testing.T, line []byte, method, id string, num int64, shape uint8) {
		checkRequest(t, line)

		if method == "" || !utf8.ValidString(method) {
			return
		}

		switch shape % 4 {
		case 0:
			checkReadBack(t, method, mustJSON(t, id))
		case 1:
			checkReadBack(t, method, mustJSON(t, num))
		case 2:
			checkReadBack(t, method, nil)
		default:
			checkRefusedWithItsID(t, method, mustJSON(t, num))
		}
	})
}

// checkRequest checks what readRequest makes of any line.
func checkRequest(t *testing.T, line []byte) {
	t.Helper()

	req, refusal := readRequest(line)
	if refusal != nil {
		checkRefusal(t, line, req, refusal)

		return
	}

	if req.method == "" {
		assert.Equal(t, request{}, req, "a line that asks nothing reads as nothing")

		return
	}

	assert.True(t, json.Valid(line), "a request is JSON")

	if req.id != nil {
		assert.True(t, isID(req.id), "%s", req.id)
		checkOneLine(t, resultOf(req.id, struct{}{}))
	}
}

// checkRefusal checks the refusal of line: it reads nothing, is a parse error only for a
// line that isn't JSON and an invalid request otherwise, echoes an ID only when it is one,
// and is written as one line.
func checkRefusal(t *testing.T, line []byte, req request, refusal *reply) {
	t.Helper()

	assert.Equal(t, request{}, req, "a refusal reads nothing")
	assert.Equal(t, reply{JSONRPC: jsonRPC, ID: refusal.ID, Error: refusal.Error}, *refusal, "an error, never a result")
	require.NotNil(t, refusal.Error)
	assert.Contains(t, []int{codeParse, codeInvalidRequest}, refusal.Error.Code)
	assert.Equal(t, !json.Valid(line), refusal.Error.Code == codeParse, "a parse error is a line that isn't JSON")
	assert.NotEmpty(t, refusal.Error.Message)

	if refusal.ID != nil {
		assert.True(t, isID(refusal.ID), "%s", refusal.ID)
	}

	checkOneLine(t, refusal)
}

// checkOneLine checks that r is written as one line of JSON.
func checkOneLine(t *testing.T, r *reply) {
	t.Helper()

	var out bytes.Buffer

	require.NoError(t, newReplies(&out).write(r))
	assert.Equal(t, 1, bytes.Count(out.Bytes(), []byte("\n")), "one line: %q", out.Bytes())
	assert.True(t, bytes.HasSuffix(out.Bytes(), []byte("\n")))
	assert.True(t, json.Valid(out.Bytes()), "%q", out.Bytes())
}

// checkReadBack checks that the request with id calling method, or the notification for a
// nil id, reads back as it was written: its ID as it came, its method, and its params.
func checkReadBack(t *testing.T, method string, id json.RawMessage) {
	t.Helper()

	msg := map[string]any{"jsonrpc": jsonRPC, "method": method, "params": map[string]string{"echo": method}}
	if id != nil {
		msg["id"] = id
	}

	req, refusal := readRequest(mustJSON(t, msg))
	require.Nil(t, refusal)
	assert.Equal(t, string(id), string(req.id))
	assert.Equal(t, method, req.method)
	assert.JSONEq(t, string(mustJSON(t, msg["params"])), string(req.params))
}

// checkRefusedWithItsID checks that a request with id naming a JSON-RPC version other than
// 2.0 is refused, the refusal echoing its ID as it came.
func checkRefusedWithItsID(t *testing.T, method string, id json.RawMessage) {
	t.Helper()

	req, refusal := readRequest(mustJSON(t, map[string]any{"jsonrpc": "1.0", "id": id, "method": method}))
	assert.Equal(t, request{}, req)
	require.NotNil(t, refusal)
	require.NotNil(t, refusal.Error)
	assert.Equal(t, codeInvalidRequest, refusal.Error.Code)
	assert.Equal(t, string(id), string(refusal.ID))
}

// mustJSON returns v as JSON.
func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()

	data, err := json.Marshal(v)
	require.NoError(t, err)

	return data
}

// requestSeed is a line, and the request a client writes: its method, its ID as a string
// or a number, and its shape (a string ID, a number ID, a notification, a wrong version).
type requestSeed struct {
	line, method, id string
	num              int64
	shape            uint8
}

// requestSeeds are the lines a recorded client sent, in each shape, and lines of each kind
// the tool mode reads: a request, a notification, a response, a batch, lines that aren't
// JSON or a request, an empty line, a CRLF line, IDs of every type.
func requestSeeds(f *testing.F) []requestSeed {
	f.Helper()

	recorded, err := os.ReadFile(filepath.Join("testdata", "mcp-client.jsonl"))
	require.NoError(f, err)

	seeds := make([]requestSeed, 0, 32)

	for line := range strings.Lines(string(recorded)) {
		seeds = append(seeds, requestSeed{strings.TrimSuffix(line, "\n"), methodToolsCall, "a", 0, uint8(len(seeds) % 4)})
	}

	require.NotEmpty(f, seeds, "the recorded client's lines are seeds")

	return append(seeds,
		requestSeed{`{"jsonrpc":"2.0","id":1,"method":"ping"}`, methodPing, "1", 1, 0},
		requestSeed{`{"jsonrpc":"2.0","method":"notifications/initialized"}`, "notifications/initialized", "", 0, 2},
		requestSeed{`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, methodToolsList, "<&>", -1, 1},
		requestSeed{`{"jsonrpc":"2.0","id":6,"result":{}}`, "é", "two words", 1 << 62, 3},
		requestSeed{`{"jsonrpc":"2.0","id":7,"error":{"code":-1,"message":"no"}}`, methodInitialize, " ", 0, 0},
		requestSeed{`{"jsonrpc":"2.0","id":1,"method":"ping"`, "x", "", 0, 1},
		requestSeed{`not json`, "\xff", "\xff", 0, 0},
		requestSeed{``, methodPing, "", 0, 2},
		requestSeed{" \t ", methodPing, "", 0, 3},
		requestSeed{`{"jsonrpc":"2.0","id":12,"method":"ping"}` + "\r", methodPing, "12", 12, 0},
		requestSeed{`{"jsonrpc":"2.0","id":null,"method":"ping"}`, methodPing, "null", 0, 1},
		requestSeed{`{"jsonrpc":"2.0","id":true,"method":"ping"}`, methodPing, "true", 0, 2},
		requestSeed{`{"jsonrpc":"2.0","id":{"n":1},"method":"ping"}`, methodPing, "{}", 0, 3},
		requestSeed{`{"jsonrpc":"2.0","id":"",  "method" : "ping" , "params":[1]}`, methodPing, "", 0, 0},
		requestSeed{`{"jsonrpc":"2.0","id":1e3,"method":7}`, methodPing, "1e3", 1000, 1},
		requestSeed{`{"jsonrpc":"1.0","id":-0.5,"method":"ping"}`, methodPing, "-0.5", 0, 3},
		requestSeed{`{"jsonrpc":"2.0","method":"ping","method":"tools/list","id":"\ud800"}`, methodPing, `\ud800`, 0, 0},
		requestSeed{`null`, methodPing, "", 0, 1},
		requestSeed{`"ping"`, methodPing, "", 0, 2},
		requestSeed{`{} {}`, methodPing, "", 0, 3},
	)
}

// FuzzLines checks that any stdin, however its reads come, reads as its lines, each as
// soon as its newline comes, without the newline, a last one without a newline too, and
// then as ended for good; a line longer than the reader's buffer included. No line here
// is past maxLine: TestALineOverTheCapIsDroppedAndTheNextIsReadWhole reads those.
func FuzzLines(f *testing.F) {
	for _, seed := range []struct {
		data, cuts []byte
		wide       uint8
	}{
		{nil, nil, 0},
		{[]byte("\n"), nil, 1},
		{[]byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n"), []byte{3, 0, 255}, 0},
		{[]byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`), []byte{1, 1, 1}, 2},
		{[]byte("a\r\n\r\nb\n\nc"), []byte{0, 0, 0, 0}, 3},
		{[]byte("\xff\xfe\n\x00\n"), []byte{1}, 0},
		{[]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n{}\n"), []byte{40, 7}, 1},
	} {
		f.Add(seed.data, seed.cuts, seed.wide)
	}

	f.Fuzz(func(t *testing.T, data, cuts []byte, wide uint8) {
		// wide makes the first line up to about 1.5 times the reader's buffer longer.
		input := append(bytes.Repeat([]byte("x"), int(wide%4)*(readSize/2+7)), data...)

		assert.Equal(t, splitLines(input), readLines(t, newLines(&inParts{data: input, cuts: cuts}), input))
	})
}

// splitLines returns input's lines without their newlines, a last one without a newline
// too: what the tool mode reads of it.
func splitLines(input []byte) []string {
	lines := []string{}

	for line := range strings.Lines(string(input)) {
		lines = append(lines, strings.TrimSuffix(line, "\n"))
	}

	return lines
}

// readLines reads l to its end, which must come by the time it has read every line of
// input, and stay.
func readLines(t *testing.T, l *lines, input []byte) []string {
	t.Helper()

	got := []string{}

	for range bytes.Count(input, []byte("\n")) + 2 {
		line, err := l.next()
		if errors.Is(err, io.EOF) {
			_, err = l.next()
			require.ErrorIs(t, err, io.EOF, "an ended stdin stays ended")

			return got
		}

		require.NoError(t, err, "no line here is past maxLine")
		assert.NotContains(t, string(line), "\n")

		got = append(got, string(line))
	}

	require.Fail(t, "more lines than the input holds", "%q", got)

	return got
}

// inParts is a stdin whose reads return data in parts one longer than cuts' bytes, then
// the rest, as a pipe returns what was written so far.
type inParts struct {
	data, cuts []byte
}

// Read returns the next part.
func (r *inParts) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}

	size := len(r.data)
	if len(r.cuts) > 0 {
		size = min(size, int(r.cuts[0])+1)
		r.cuts = r.cuts[1:]
	}

	n := copy(p, r.data[:size])
	r.data = r.data[n:]

	return n, nil
}

// tooLong is what readAllLines says of a line past maxLine.
const tooLong = "(too long)"

func TestALineOverTheCapIsDroppedAndTheNextIsReadWhole(t *testing.T) {
	t.Parallel()

	long := bytes.Repeat([]byte("x"), maxLine+1)
	most := strconv.Itoa(maxLine) + " bytes"

	tests := []struct {
		name  string
		parts []string
		want  []string
	}{
		{"a line of exactly the cap is read", []string{string(long[:maxLine]), "\nnext\n"}, []string{most, "next"}},
		{"one byte more is dropped, and the next read whole", []string{string(long), "\nnext\n"}, []string{tooLong, "next"}},
		{"a carriage return is the line's", []string{string(long[:maxLine]), "\r\nnext"}, []string{tooLong, "next"}},
		{"a last line past the cap with no newline", []string{"first\n", string(long)}, []string{"first", tooLong}},
		{"a last line of exactly the cap with no newline", []string{string(long[:maxLine])}, []string{most}},
		{"two in a row", []string{string(long), "\n", string(long), "\n{}\n"}, []string{tooLong, tooLong, "{}"}},
		{"and short lines after, in the same read", []string{string(long), "\nnext\nlast"}, []string{tooLong, "next", "last"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			readers := make([]io.Reader, 0, len(tt.parts))
			for _, part := range tt.parts {
				readers = append(readers, strings.NewReader(part))
			}

			assert.Equal(t, tt.want, readAllLines(t, newLines(io.MultiReader(readers...))))
		})
	}
}

// readAllLines reads l to its end: each line, by its size when it is long, or tooLong.
func readAllLines(t *testing.T, l *lines) []string {
	t.Helper()

	var got []string

	for range 10 {
		line, err := l.next()

		switch {
		case errors.Is(err, io.EOF):
			return got
		case errors.Is(err, errTooLong):
			assert.Nil(t, line, "none of a line past the cap is kept")

			got = append(got, tooLong)
		case len(line) > 64:
			require.NoError(t, err)

			got = append(got, strconv.Itoa(len(line))+" bytes")
		default:
			require.NoError(t, err)

			got = append(got, string(line))
		}
	}

	require.Fail(t, "more lines than the input holds", "%q", got)

	return got
}
