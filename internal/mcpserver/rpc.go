package mcpserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// The messages' framing: JSON-RPC 2.0, one message a line.
const (
	// jsonRPC is the JSON-RPC version every message names.
	jsonRPC = "2.0"

	// maxLine caps one message, in bytes, without its newline: a proposal at its largest
	// (kit.MaxProposalBytes) fits in a call. A longer line is read to its end and dropped.
	maxLine = 4 << 20

	// readSize is how much of stdin is read at once.
	readSize = 64 << 10
)

// JSON-RPC's error codes the tool mode answers with.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeNoMethod       = -32601
	codeInvalidParams  = -32602
)

// errTooLong is a message larger than maxLine.
var errTooLong = errors.New("the message is larger than 4 MB")

// lines reads stdin a line at a time.
type lines struct {
	r *bufio.Reader
}

// newLines returns the lines of r.
func newLines(r io.Reader) *lines {
	return &lines{r: bufio.NewReaderSize(r, readSize)}
}

// next returns the next line, without its newline, as soon as the newline is read; or
// errTooLong for one past maxLine, read to its end and dropped, so the line after it is
// read whole; or io.EOF once stdin ends. A last line without a newline is a line too.
func (l *lines) next() ([]byte, error) {
	var (
		line    []byte
		tooLong bool
	)

	for {
		chunk, err := l.r.ReadSlice('\n')
		line, tooLong = grow(line, chunk, err == nil, tooLong)

		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case err == nil || (errors.Is(err, io.EOF) && (len(line) > 0 || tooLong)):
			return lineOf(line, tooLong)
		default:
			return nil, err
		}
	}
}

// grow adds chunk, the line's last when ended, to line, and drops the line once it is
// past maxLine (tooLong), keeping none of it.
func grow(line, chunk []byte, ended, tooLong bool) ([]byte, bool) {
	size := len(line) + len(chunk)
	if ended {
		size-- // the newline isn't the message's
	}

	if tooLong || size > maxLine {
		return nil, true
	}

	return append(line, chunk...), false
}

// lineOf returns a line read whole: without its newline, or errTooLong.
func lineOf(line []byte, tooLong bool) ([]byte, error) {
	if tooLong {
		return nil, errTooLong
	}

	return bytes.TrimSuffix(line, []byte("\n")), nil
}

// reply is one answer to the request with ID: its result, or its error.
type reply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *replyError     `json:"error,omitempty"`
}

// replyError is a reply's error: its code, and what it says.
type replyError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// resultOf is the reply to the request id carrying result.
func resultOf(id json.RawMessage, result any) *reply {
	return &reply{JSONRPC: jsonRPC, ID: id, Result: result}
}

// errorOf is the reply refusing the request id with code, saying message. A nil id is
// the null of a message whose ID can't be read.
func errorOf(id json.RawMessage, code int, message string) *reply {
	return &reply{JSONRPC: jsonRPC, ID: id, Error: &replyError{Code: code, Message: message}}
}

// replies writes the replies, one line each.
type replies struct {
	enc *json.Encoder
}

// newReplies returns the replies written to w, each in one write.
func newReplies(w io.Writer) *replies {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	return &replies{enc: enc}
}

// write writes r as one line, and nothing for nil. JSON escapes every line break in a
// string, so a reply is always one line.
func (o *replies) write(r *reply) error {
	if r == nil {
		return nil
	}

	return o.enc.Encode(r)
}

// request is one request the client sent: its ID, nil for a notification, which is never
// answered; its method; and its params, as sent.
type request struct {
	id     json.RawMessage
	method string
	params json.RawMessage
}

// readRequest reads one line as a request or a notification, or returns the reply that
// refuses it. A line that asks nothing, a response or a notification that isn't one,
// returns neither: a request with no method, and no reply.
func readRequest(line []byte) (request, *reply) {
	var fields map[string]json.RawMessage

	if err := json.Unmarshal(line, &fields); err != nil || fields == nil {
		if !json.Valid(line) {
			return request{}, errorOf(nil, codeParse, "Parse error: the message isn't JSON.")
		}

		return request{}, errorOf(nil, codeInvalidRequest, "Invalid request: a message is one JSON object.")
	}

	id, hasID := fields["id"]
	if hasID && !isID(id) {
		return request{}, errorOf(nil, codeInvalidRequest, "Invalid request: an id is a string or a number.")
	}

	why := requestProblem(fields)

	switch {
	case why == "":
		var method string

		_ = json.Unmarshal(fields["method"], &method) // requestProblem read it

		return request{id: id, method: method, params: fields["params"]}, nil
	case !hasID || isResponse(fields):
		return request{}, nil
	default:
		return request{}, errorOf(id, codeInvalidRequest, "Invalid request: "+why+".")
	}
}

// requestProblem says what keeps a message's fields from being a request or a
// notification, "" for nothing.
func requestProblem(fields map[string]json.RawMessage) string {
	var version, method string

	switch {
	case json.Unmarshal(fields["jsonrpc"], &version) != nil || version != jsonRPC:
		return `its "jsonrpc" isn't "2.0"`
	case json.Unmarshal(fields["method"], &method) != nil || method == "":
		return "it names no method"
	default:
		return ""
	}
}

// isResponse reports a message's fields that answer a request rather than make one: a
// result or an error, and no method. The tool mode sends no request, so it reads none.
func isResponse(fields map[string]json.RawMessage) bool {
	_, method := fields["method"]
	_, result := fields["result"]
	_, failed := fields["error"]

	return !method && (result || failed)
}

// isID reports a JSON value that may be a request's ID, which a reply echoes as it came:
// a string or a number. MCP refuses null, and JSON-RPC every other type.
func isID(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return false
	}

	c := raw[0]

	return c == '"' || c == '-' || (c >= '0' && c <= '9')
}
