package textutil_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// FuzzLineSplitter checks that a LineSplitter hands over the same lines however the
// output is cut into writes: one line for each newline, and one more for a last line
// without one, each the line's first Max bytes as TerminalLine shows them, valid UTF-8
// with no control character but a tab.
func FuzzLineSplitter(f *testing.F) {
	for _, seed := range []struct {
		out  string
		max  uint8
		cuts []byte
	}{
		{"", 0, nil},
		{"one\ntwo\n", 0, []byte{2, 5}},
		{"\x1b[31mred\x1b[0m\rover\r\nlast without a newline", 4, []byte{1, 1, 1}},
		{"\n\n\xff\xfe\n\t tab \n", 1, []byte{0, 9}},
		{strings.Repeat("long ", 40) + "\nshort", 7, []byte{3, 200}},
	} {
		f.Add([]byte(seed.out), seed.max, seed.cuts)
	}

	f.Fuzz(func(t *testing.T, out []byte, maxLine uint8, cuts []byte) {
		keep := int(maxLine)
		splitter := textutil.LineSplitter{Max: keep}

		var got []string

		line := func(s string) { got = append(got, s) }

		rest := out
		for _, cut := range cuts {
			n := min(int(cut), len(rest))
			splitter.Write(rest[:n], line)
			rest = rest[n:]
		}

		splitter.Write(rest, line)
		splitter.Flush(line)

		if keep <= 0 {
			keep = textutil.MaxLine
		}

		assert.Equal(t, wholeLines(string(out), keep), got)

		for _, s := range got {
			assert.True(t, utf8.ValidString(s), "%q", s)
			assert.False(t, strings.ContainsFunc(s, func(r rune) bool { return (r < ' ' && r != '\t') || r == 0x7f }), "%q", s)
		}
	})
}

// wholeLines is what a LineSplitter keeping keep bytes of a line hands over for out
// written at once: each line ended by a newline, then any last line without one, each
// cut to keep bytes and shown as TerminalLine shows it.
func wholeLines(out string, keep int) []string {
	var lines []string

	parts := strings.Split(out, "\n")
	for i, part := range parts {
		if i == len(parts)-1 && part == "" {
			break
		}

		lines = append(lines, textutil.TerminalLine(part[:min(len(part), keep)]))
	}

	return lines
}
