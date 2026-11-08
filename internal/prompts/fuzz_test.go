package prompts

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

// FuzzSanitizeLine checks that sanitizeLine always returns one line of valid, plain text:
// no control or bidirectional-text characters, single spaces only between words, never
// more characters than it was given, unchanged when sanitized again, and keeping every
// other character of the input in order.
func FuzzSanitizeLine(f *testing.F) {
	for _, seed := range []string{
		"",
		"Fix the login flow",
		"  leading and trailing  ",
		"line one\nline two\r\n\tindented",
		"bidi \u202egnp.exe\u202c and \u2066isolate\u2069",
		"nbsp\u00a0and line\u2028separators\u0085",
		"invalid \xff\xfe utf-8",
		strings.Repeat("word \x00\x1b[31m", 50),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, s string) {
		out := sanitizeLine(s)

		assert.True(t, utf8.ValidString(out), "invalid UTF-8 in %q", out)
		checkPlainLine(t, out)
		assert.Equal(t, out, sanitizeLine(out), "sanitizing must be idempotent")
		assert.LessOrEqual(t, utf8.RuneCountInString(out), utf8.RuneCountInString(s))
		assert.Equal(t, keptRunes(s), strings.ReplaceAll(out, " ", ""), "only spaces, controls, and bidi controls may change")
	})
}

// checkPlainLine checks that out has no control or bidirectional-text characters, no
// whitespace but the ASCII space, and no space at either end or next to another.
func checkPlainLine(t *testing.T, out string) {
	t.Helper()

	for _, r := range out {
		assert.False(t, unicode.IsControl(r), "control character %U in %q", r, out)
		assert.False(t, unicode.Is(unicode.Bidi_Control, r), "bidi control %U in %q", r, out)
		assert.False(t, r != ' ' && unicode.IsSpace(r), "whitespace %U in %q", r, out)
	}

	assert.False(t, strings.HasPrefix(out, " ") || strings.HasSuffix(out, " "), "space at an end of %q", out)
	assert.NotContains(t, out, "  ")
}

// keptRunes is s without its whitespace, control, and bidirectional-text characters, the
// ones sanitizeLine may drop or turn into spaces; strings.Map reads an invalid byte as
// U+FFFD, as sanitizeLine does.
func keptRunes(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
			return -1
		}

		return r
	}, s)
}
