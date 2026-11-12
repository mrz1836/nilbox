package textutil_test

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// posixShell is the shell the quoting is checked against.
const posixShell = "/bin/sh"

// shellRunTimeout bounds one shell run, so a stuck shell fails its input instead of
// hanging the fuzzer.
const shellRunTimeout = 10 * time.Second

// shellWords runs a POSIX shell on line, the words of a command, and returns how the shell
// split it: how many words, then each word, every one ended by a NUL. It runs in the
// package's folder, where a glob left unquoted would match files.
func shellWords(t *testing.T, line string) string {
	t.Helper()

	if _, err := os.Stat(posixShell); err != nil {
		t.Skipf("no %s to run the quoted words: %v", posixShell, err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), shellRunTimeout)
	defer cancel()

	script := "set -- " + line + `; printf '%s\0' "$#" "$@"`
	cmd := exec.CommandContext(ctx, posixShell, "-c", script) //nolint:gosec // the shell run is the test

	out, err := cmd.Output()
	require.NoError(t, err, "the shell runs %q", script)

	return string(out)
}

// splitBack is what shellWords returns when the shell reads exactly words.
func splitBack(words ...string) string {
	return strconv.Itoa(len(words)) + "\x00" + strings.Join(append(words, ""), "\x00")
}

// quotingSeeds are strings that exercise both quoters: empty, plain, every special
// character, newlines, unicode, invalid UTF-8, and a long run of quotes.
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
		"=ls",
		"key=value",
		"a/b.c-d_e=f:g,h+i@j%k",
		"-n",
		"*",
		"\xff\xfe",
	}
}

func TestShellQuote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain", input: "plain", want: "'plain'"},
		{name: "empty", input: "", want: "''"},
		{name: "spaces", input: "two words  here", want: "'two words  here'"},
		{name: "single quote", input: "it's", want: `'it'\''s'`},
		{name: "only quotes", input: "''", want: `''\'''\'''`},
		{name: "double quote", input: `say "hi"`, want: `'say "hi"'`},
		{name: "dollar", input: "$HOME and ${USER}", want: "'$HOME and ${USER}'"},
		{name: "command substitution", input: "$(id) `whoami`", want: "'$(id) `whoami`'"},
		{name: "newline", input: "line one\nline two", want: "'line one\nline two'"},
		{name: "unicode", input: "café naïve 🚀", want: "'café naïve 🚀'"},
		{name: "leading hash", input: "#7", want: "'#7'"},
		{name: "backslash", input: `C:\path\n`, want: `'C:\path\n'`},
		{name: "shell operators", input: "a; b && c | d > e < f &", want: "'a; b && c | d > e < f &'"},
		{name: "globs and tilde", input: "~/x/*.go [ab]?", want: "'~/x/*.go [ab]?'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := textutil.ShellQuote(tt.input)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, splitBack(tt.input), shellWords(t, got), "the shell reads back exactly the input")
		})
	}
}

func TestShellWord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain", input: "plain", want: "plain"},
		{name: "branch", input: "origin/feature/x-1", want: "origin/feature/x-1"},
		{name: "version", input: "v1.4.3+build.7", want: "v1.4.3+build.7"},
		{name: "every safe character", input: "a/b.c-d_e=f:g,h+i@j%k", want: "a/b.c-d_e=f:g,h+i@j%k"},
		{name: "a setting", input: "bump=patch", want: "bump=patch"},
		{name: "an option", input: "--add-dir=/srv/x", want: "--add-dir=/srv/x"},
		{name: "empty", input: "", want: "''"},
		{name: "space", input: "a b", want: "'a b'"},
		{name: "command substitution", input: "$(touch pwned)", want: "'$(touch pwned)'"},
		{name: "backticks", input: "`id`", want: "'`id`'"},
		{name: "single quote", input: "it's", want: `'it'\''s'`},
		{name: "a second command", input: "x;rm -rf ~", want: "'x;rm -rf ~'"},
		{name: "variable", input: "$HOME", want: "'$HOME'"},
		{name: "semicolon", input: "a;b", want: "'a;b'"},
		{name: "glob", input: "*.go", want: "'*.go'"},
		{name: "tilde", input: "~/x", want: "'~/x'"},
		{name: "leading hash", input: "#7", want: "'#7'"},
		{name: "a leading = zsh reads as a path", input: "=ls", want: "'=ls'"},
		{name: "a lone =", input: "=", want: "'='"},
		{name: "unicode", input: "é", want: "'é'"},
		{name: "a bidirectional control", input: "a\u202eb", want: "'a\u202eb'"},
		{name: "invalid UTF-8", input: "\xff", want: "'\xff'"},
		{name: "newline", input: "a\nb", want: "'a\nb'"},
		{name: "tab", input: "a\tb", want: "'a\tb'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := textutil.ShellWord(tt.input)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, got == tt.input, textutil.ShellSafe(tt.input), "plain exactly when ShellSafe passes it")
			assert.Equal(t, splitBack(tt.input), shellWords(t, got), "the shell reads back exactly the input")
		})
	}
}

// FuzzShellQuote checks that two quoted words reach a POSIX shell as exactly the two
// strings they were built from, whatever quotes, dollar signs, backslashes, or newlines
// they hold.
func FuzzShellQuote(f *testing.F) {
	for _, seed := range quotingSeeds() {
		f.Add(seed, "--flag")
	}

	f.Fuzz(func(t *testing.T, a, b string) {
		if strings.ContainsRune(a+b, 0) {
			t.Skip("a shell argument cannot carry a NUL byte")
		}

		line := textutil.ShellQuote(a) + " " + textutil.ShellQuote(b)
		assert.Equal(t, splitBack(a, b), shellWords(t, line))
	})
}

// FuzzShellWord checks that ShellWord leaves a string as it is exactly when ShellSafe
// passes it, quotes it as ShellQuote does otherwise, and that two such words reach a
// POSIX shell as exactly the two strings they were built from.
func FuzzShellWord(f *testing.F) {
	for _, seed := range quotingSeeds() {
		f.Add(seed, "--flag")
	}

	f.Fuzz(func(t *testing.T, a, b string) {
		if strings.ContainsRune(a+b, 0) {
			t.Skip("a shell argument cannot carry a NUL byte")
		}

		for _, s := range []string{a, b} {
			if textutil.ShellSafe(s) {
				assert.Equal(t, s, textutil.ShellWord(s))
			} else {
				assert.Equal(t, textutil.ShellQuote(s), textutil.ShellWord(s))
			}
		}

		line := textutil.ShellWord(a) + " " + textutil.ShellWord(b)
		assert.Equal(t, splitBack(a, b), shellWords(t, line))
	})
}
