package terminal

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// posixShell is the shell the quoting is checked against.
const posixShell = "/bin/sh"

// shellRunTimeout bounds one shell run, so a stuck shell fails its input instead of
// hanging the fuzzer.
const shellRunTimeout = 10 * time.Second

// quotingSeeds are strings that exercise both quoters: empty, plain, every special
// character, newlines, unicode, and a long run of quotes.
func quotingSeeds() []string {
	return []string{
		"",
		"plain",
		"it's",
		`'`,
		`a "b" \c \\d`,
		"line\nbreak\r\ttab",
		"$(echo hi) `id` ; | & * ? ~ $HOME",
		"unicode: h\u00e9llo \u4e16\u754c \U0001F600 \u202e",
		`\n \t \r \"`,
		strings.Repeat(`'"\`, 64),
	}
}

// FuzzShQuote checks that a quoted string reaches a POSIX shell as exactly the bytes it
// was built from: printf prints every input back unchanged, whatever quotes, dollar
// signs, backslashes, or newlines it holds.
func FuzzShQuote(f *testing.F) {
	if _, err := os.Stat(posixShell); err != nil {
		f.Skipf("no %s to run the quoted strings: %v", posixShell, err)
	}

	for _, seed := range quotingSeeds() {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, s string) {
		if strings.ContainsRune(s, 0) {
			t.Skip("a shell argument cannot carry a NUL byte")
		}

		ctx, cancel := context.WithTimeout(t.Context(), shellRunTimeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, posixShell, "-c", "printf %s "+shQuote(s)) //nolint:gosec // the shell run is the test

		out, err := cmd.Output()
		require.NoError(t, err)
		assert.Equal(t, s, string(out))
	})
}

// FuzzAppleScriptString checks that an escaped string, put between double quotes, is an
// AppleScript string literal that reads back as the original, and that the escaping
// adds exactly one backslash per backslash and double quote.
func FuzzAppleScriptString(f *testing.F) {
	for _, seed := range quotingSeeds() {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, s string) {
		escaped := appleScriptString(s)

		got, ok := decodeAppleScriptLiteral(`"` + escaped + `"`)
		require.True(t, ok, "not one AppleScript string literal: %q", escaped)
		assert.Equal(t, s, got)

		specials := strings.Count(s, `\`) + strings.Count(s, `"`)
		assert.Len(t, escaped, len(s)+specials)
		assert.Equal(t, strings.Count(s, `\`)+specials, strings.Count(escaped, `\`))
	})
}

// decodeAppleScriptLiteral reads text that must be exactly one AppleScript string
// literal: a double quote, characters and backslash escapes, and a closing double quote
// that ends the text. AppleScript knows the escapes \", \\, \n, \r, and \t, so a backslash
// left unescaped before n, r, or t would change the text; any other escape does not
// compile, and ok is false for it.
func decodeAppleScriptLiteral(lit string) (string, bool) {
	body, ok := strings.CutPrefix(lit, `"`)
	if !ok {
		return "", false
	}

	var b strings.Builder

	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '"':
			return b.String(), i == len(body)-1
		case '\\':
			i++

			c, known := appleScriptEscape(body, i)
			if !known {
				return "", false
			}

			b.WriteByte(c)
		default:
			b.WriteByte(body[i])
		}
	}

	return "", false
}

// appleScriptEscape returns the character an AppleScript escape stands for, given the
// index of the character after its backslash; known is false past the end of body or for
// an escape AppleScript does not compile.
func appleScriptEscape(body string, i int) (c byte, known bool) {
	if i >= len(body) {
		return 0, false
	}

	switch body[i] {
	case '"', '\\':
		return body[i], true
	case 'n':
		return '\n', true
	case 'r':
		return '\r', true
	case 't':
		return '\t', true
	default:
		return 0, false
	}
}
