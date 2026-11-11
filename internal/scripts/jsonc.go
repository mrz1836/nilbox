package scripts

import (
	"bytes"
	"encoding/json"
)

// bom is the byte order mark an editor may write at the start of a UTF-8 file.
const bom = "\xEF\xBB\xBF"

// decodeJSONC decodes JSON with comments, as VS Code writes tasks.json, into v: plain JSON
// once stripJSONC has taken out what JSON doesn't allow.
func decodeJSONC(data []byte, v any) error {
	return json.Unmarshal(stripJSONC(data), v)
}

// stripJSONC returns data as plain JSON: without a leading byte order mark, without //
// line comments and /* */ block comments, and without a comma before a } or ], each
// outside strings, whose escapes it follows. A block comment becomes one space, so it
// never joins the tokens on either side of it, and a line comment ends before its newline.
// Plain JSON comes back unchanged, byte for byte.
func stripJSONC(data []byte) []byte {
	data = bytes.TrimPrefix(data, []byte(bom))
	s := stripper{out: make([]byte, 0, len(data)), comma: -1}

	for i := 0; i < len(data); i++ {
		i = s.step(data, i)
	}

	return s.out
}

// stripper is stripJSONC's output so far, and where in it a comma waits to learn whether a
// } or ] follows it, or -1.
type stripper struct {
	out   []byte
	comma int
}

// step copies, or drops, what starts at data[i]: a string, a comment, or one byte. It
// returns the index of the last byte it took.
func (s *stripper) step(data []byte, i int) int {
	switch {
	case data[i] == '"':
		end := stringEnd(data, i)
		s.out, s.comma = append(s.out, data[i:end]...), -1

		return end - 1
	case data[i] == '/' && i+1 < len(data) && data[i+1] == '/':
		if end := bytes.IndexByte(data[i:], '\n'); end > 0 {
			return i + end - 1 // the newline stays
		}

		return len(data) - 1
	case data[i] == '/' && i+1 < len(data) && data[i+1] == '*':
		end := bytes.Index(data[i+2:], []byte("*/"))
		if end < 0 {
			return len(data) - 1
		}

		s.out = append(s.out, ' ')

		return i + end + 3
	default:
		s.byte(data[i])

		return i
	}
}

// byte copies one byte outside strings and comments: a comma waits, and a } or ] drops the
// comma waiting before it; space keeps it waiting, and anything else ends the wait.
func (s *stripper) byte(c byte) {
	switch c {
	case ' ', '\t', '\n', '\r':
	case '}', ']':
		if s.comma >= 0 {
			s.out = append(s.out[:s.comma], s.out[s.comma+1:]...)
		}

		s.comma = -1
	case ',':
		s.comma = len(s.out)
	default:
		s.comma = -1
	}

	s.out = append(s.out, c)
}

// stringEnd returns the index just past the string that starts with the quote at
// data[start], following its escapes, or len(data) for a string that never ends.
func stringEnd(data []byte, start int) int {
	for i := start + 1; i < len(data); i++ {
		switch data[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}

	return len(data)
}
