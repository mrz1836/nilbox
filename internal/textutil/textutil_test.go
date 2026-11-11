package textutil_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/textutil"
)

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{name: "short", in: "abc", n: 5, want: "abc"},
		{name: "exact", in: "abcde", n: 5, want: "abcde"},
		{name: "cut", in: "abcdef", n: 5, want: "abcd…"},
		{name: "space before the ellipsis", in: "abc defg", n: 5, want: "abc…"},
		{name: "runes, not bytes", in: "ééééé€", n: 5, want: "éééé…"},
		{name: "one rune", in: "abc", n: 1, want: "…"},
		{name: "no limit", in: "abc", n: 0, want: "abc"},
		{name: "empty", in: "", n: 3, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, textutil.Truncate(tt.in, tt.n))
		})
	}
}

func TestClip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{name: "short", in: " abc ", n: 10, want: " abc "},
		{name: "cut", in: "abcdef", n: 3, want: "abc"},
		{name: "cut and trimmed", in: "ab cdef", n: 3, want: "ab"},
		{name: "runes, not bytes", in: "éééé", n: 2, want: "éé"},
		{name: "no limit", in: "abc", n: -1, want: "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, textutil.Clip(tt.in, tt.n))
		})
	}
}

func TestCapitalize(t *testing.T) {
	t.Parallel()

	assert.Empty(t, textutil.Capitalize(""))
	assert.Equal(t, "Éclair", textutil.Capitalize("éclair"))
	assert.Equal(t, "Already", textutil.Capitalize("Already"))
	assert.Equal(t, " leading", textutil.Capitalize(" leading"))
}

func TestSentence(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"":                          "",
		"  ":                        "",
		"no prompt applies":         "No prompt applies.",
		"Done.":                     "Done.",
		"  --model is set by it \n": "--model is set by it.",
		"éclair":                    "Éclair.",
		"is it there?":              "Is it there?",
		"refused!":                  "Refused!",
	}

	for in, want := range tests {
		assert.Equal(t, want, textutil.Sentence(in), in)
	}
}

func TestEndSentence(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"":                      "",
		" \n":                   "",
		"failed to push tag":    "failed to push tag.",
		" pre-push: blocked \n": "pre-push: blocked.",
		"Done.":                 "Done.",
		"is it there?":          "is it there?",
		"refused!":              "refused!",
		"v1.4.3 already exists": "v1.4.3 already exists.",
	}

	for in, want := range tests {
		assert.Equal(t, want, textutil.EndSentence(in), in)
	}
}

func TestNonNil(t *testing.T) {
	t.Parallel()

	var none []string

	assert.NotNil(t, textutil.NonNil(none))
	assert.Empty(t, textutil.NonNil(none))
	assert.Equal(t, []int{1, 2}, textutil.NonNil([]int{1, 2}))
}

func TestNonNilMap(t *testing.T) {
	t.Parallel()

	var none map[string]int

	assert.NotNil(t, textutil.NonNilMap(none))
	assert.Empty(t, textutil.NonNilMap(none))
	assert.Equal(t, map[string]int{"a": 1}, textutil.NonNilMap(map[string]int{"a": 1}))
}

func TestJoin(t *testing.T) {
	t.Parallel()

	type choice string

	assert.Equal(t, "a, b", textutil.Join([]choice{"a", "b"}, ", "))
	assert.Empty(t, textutil.Join([]choice(nil), ", "))
}

func TestTerminalLine(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		"plain":                "plain",
		"trailing  \r\n":       "trailing",
		"⏳ 10%\r⏳ 50%\r✓ done": "✓ done",
		"\x1b[1;3;mtoken\x1b[0m = \x1b[32mok\x1b[m":       "token = ok",
		"\x1b]8;;https://example.com\x07link\x1b]8;;\x07": "link",
		"bell\x07 and\ttab":                               "bell and\ttab",
		"bad \xff byte":                                   "bad � byte",
		"  indented":                                      "  indented",
		"":                                                "",
	} {
		assert.Equal(t, want, textutil.TerminalLine(raw), "%q", raw)
	}
}

func TestWithReason(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Couldn't check: origin hung up.", textutil.WithReason("Couldn't check", "origin hung up"))
	assert.Equal(t, "Couldn't check: origin hung up.", textutil.WithReason("Couldn't check", " origin hung up. "))
	assert.Equal(t, "Couldn't check.", textutil.WithReason("Couldn't check", "  "))
}

func TestLineSplitter(t *testing.T) {
	t.Parallel()

	var (
		s     textutil.LineSplitter
		lines []string
	)

	keep := func(line string) { lines = append(lines, line) }

	for _, part := range []string{"fir", "st\nsec", "ond\r\n\x1b[32mthi", "rd\x1b[0m\n", "\n", "last"} {
		s.Write([]byte(part), keep)
	}

	assert.Equal(t, []string{"first", "second", "third", ""}, lines)

	s.Flush(keep)
	s.Flush(keep)
	assert.Equal(t, []string{"first", "second", "third", "", "last"}, lines, "a flush hands over the last line once")

	short := textutil.LineSplitter{Max: 4}
	lines = nil

	short.Write([]byte("abcdefgh"), keep)
	short.Write([]byte("ij\nxy\n"), keep)
	assert.Equal(t, []string{"abcd", "xy"}, lines, "a long line keeps its start, and the next line reads whole")
}
