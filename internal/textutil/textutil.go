package textutil

import (
	"bytes"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ellipsis ends text that was cut.
const ellipsis = "…"

// Truncate cuts s to at most n runes, ending it with an ellipsis (which counts toward n)
// when it was cut. Space before the ellipsis is dropped. An n below 1 leaves s as it is.
func Truncate(s string, n int) string {
	if n < 1 || utf8.RuneCountInString(s) <= n {
		return s
	}

	runes := []rune(s)

	return strings.TrimRightFunc(string(runes[:n-1]), unicode.IsSpace) + ellipsis
}

// Clip cuts s to at most n runes, without an ellipsis, and trims the space around what is
// left when it was cut. An n below 1 leaves s as it is.
func Clip(s string, n int) string {
	if n < 1 || utf8.RuneCountInString(s) <= n {
		return s
	}

	return strings.TrimSpace(string([]rune(s)[:n]))
}

// Capitalize upper-cases the first letter of s.
func Capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return s
	}

	return string(unicode.ToUpper(r)) + s[size:]
}

// Sentence turns a message such as an error's into a sentence: trimmed, its first letter
// upper-cased, and ended as EndSentence ends it. An empty message stays empty.
func Sentence(msg string) string {
	return Capitalize(EndSentence(msg))
}

// EndSentence ends a message as a sentence without changing its words: trimmed, and with
// a period unless it already ends in one, a question mark, or an exclamation mark. It
// suits a line quoted as it was written. An empty message stays empty.
func EndSentence(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" || strings.HasSuffix(msg, ".") || strings.HasSuffix(msg, "?") || strings.HasSuffix(msg, "!") {
		return msg
	}

	return msg + "."
}

// HasText reports text that says something: anything left once the white space the
// window's String.prototype.trim drops is gone from both ends, so the window and the
// services that post the user's text always agree. That is unicode.IsSpace's set but
// U+0085, which trim keeps, plus U+FEFF, which trim drops.
func HasText(s string) bool {
	return strings.TrimFunc(s, func(r rune) bool {
		return r == '\uFEFF' || (unicode.IsSpace(r) && r != '\u0085')
	}) != ""
}

// NonNil returns list, or an empty list for nil, so JSON carries [] rather than null and
// the UI always gets an array.
func NonNil[T any](list []T) []T {
	if list == nil {
		return []T{}
	}

	return list
}

// NonNilMap returns m, or an empty map for nil, so JSON carries {} rather than null and
// the UI always gets an object.
func NonNilMap[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		return map[K]V{}
	}

	return m
}

// Join joins string-typed values with sep, as strings.Join joins strings, for listing the
// choices of a named string type in a message.
func Join[S ~string](elems []S, sep string) string {
	parts := make([]string, len(elems))
	for i, e := range elems {
		parts[i] = string(e)
	}

	return strings.Join(parts, sep)
}

// terminalEscape matches a terminal escape sequence: a CSI sequence such as a color, an
// OSC sequence such as a link, or a two-character escape.
var terminalEscape = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)

// TerminalLine returns one line of a tool's output as a terminal would show it: from its
// last bare \r, which overwrites the line, without escapes or other control characters,
// valid UTF-8, and no trailing space.
func TerminalLine(raw string) string {
	raw = strings.TrimRight(raw, "\r\n")
	if i := strings.LastIndexByte(raw, '\r'); i >= 0 {
		raw = raw[i+1:]
	}

	raw = terminalEscape.ReplaceAllString(raw, "")
	raw = strings.Map(func(c rune) rune {
		if (c < ' ' && c != '\t') || c == 0x7f {
			return -1
		}

		return c
	}, raw)

	return strings.TrimRightFunc(strings.ToValidUTF8(raw, "�"), unicode.IsSpace)
}

// WithReason ends lead with ": reason", the reason ended as a sentence, or, without one,
// ends lead as a sentence: "Couldn't check whether v1.4.3 reached GitHub: origin hung up."
func WithReason(lead, reason string) string {
	if reason = EndSentence(reason); reason == "" {
		return EndSentence(lead)
	}

	return lead + ": " + reason
}

// MaxLine is how much of one line of a tool's output a LineSplitter keeps by default.
const MaxLine = 64 << 10

// LineSplitter cuts a tool's output into lines as it arrives, split anywhere: each whole
// line, ended by \n, goes to the callback as TerminalLine shows it, and a line longer than
// Max keeps its first Max bytes, its rest dropped up to its newline. Flush hands over a
// last line that has no newline. The zero value is ready, keeping MaxLine. It isn't safe
// for concurrent use: the reader that holds it guards it, as it guards what the callback
// changes.
type LineSplitter struct {
	// Max is how much of a line is kept; 0 is MaxLine.
	Max int

	// pending is the line read so far, and dropping reports one that ran past Max.
	pending  []byte
	dropping bool
}

// Write hands line every line p ends, and keeps the rest for the next Write.
func (s *LineSplitter) Write(p []byte, line func(string)) {
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			s.keep(p)

			return
		}

		s.keep(p[:i])
		s.end(line)

		p = p[i+1:]
	}
}

// Flush hands line the last line, when the output ended without a newline.
func (s *LineSplitter) Flush(line func(string)) {
	if len(s.pending) > 0 {
		s.end(line)
	}
}

// keep adds b to the line being read, up to Max.
func (s *LineSplitter) keep(b []byte) {
	if s.dropping {
		return
	}

	limit := s.Max
	if limit <= 0 {
		limit = MaxLine
	}

	if room := limit - len(s.pending); len(b) > room {
		b = b[:room]
		s.dropping = true
	}

	s.pending = append(s.pending, b...)
}

// end hands line the line read so far, and starts the next.
func (s *LineSplitter) end(line func(string)) {
	text := TerminalLine(string(s.pending))
	s.pending = s.pending[:0]
	s.dropping = false

	line(text)
}
