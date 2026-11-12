package sensitive

import (
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
)

// childName is a name appended to a matched path to stand for something inside it.
const childName = "zz-child.txt"

// FuzzCompilePattern checks that compiling any pattern either fails with
// ErrInvalidPattern or yields a usable pattern: one that ignores surrounding whitespace
// and ASCII case, never matches every file or an empty path, never matches a file at the
// root when it names a folder, and, matching a folder, matches what is inside it. It also
// checks that a pattern NormalizePatterns accepts survives being stored and read back.
func FuzzCompilePattern(f *testing.F) {
	for _, seed := range [][2]string{
		{".github/workflows/", ".github/workflows/ci.yml"},
		{"*.sh", "scripts/deploy.sh"},
		{"deploy/**/*.tf", "deploy/prod/main.tf"},
		{"/Makefile", "Makefile"},
		{"CLAUDE.md", "docs/Claude.MD"},
		{"[", "a"},
		{"**", "a/b"},
		{"  !secret  ", "secret"},
		{"a/../b", "b"},
		{"tab\there", ""},
		{strings.Repeat("**/", 6) + "x", "x"},
		{"\xdb", "\xdb"},
	} {
		f.Add(seed[0], seed[1])
	}

	f.Fuzz(func(t *testing.T, raw, filePath string) {
		p, err := compile(raw)

		checkCompileIsStable(t, raw, p, err)
		checkNormalized(t, raw, err)

		if err != nil {
			require.ErrorIs(t, err, ErrInvalidPattern)

			return
		}

		checkCompiledText(t, strings.TrimSpace(raw), p)
		checkPatternMatch(t, p, filePath)
	})
}

// FuzzDecodeConfig checks that any stored settings decode to a valid configuration (a
// known scope, sorted built-in categories that may be turned off, at most MaxPatterns
// patterns that compile) and that the configuration, written back as the settings
// service writes it, decodes to itself.
func FuzzDecodeConfig(f *testing.F) {
	for _, seed := range [][3]string{
		{"", "", ""},
		{"outside", `["workflows","tooling"]`, `["deploy/**","*.pem"]`},
		{"collaborators", `["agent","agent","nope","review","dependencies"]`, `["", "**", "!x", "ok.txt"]`},
		{"OUTSIDE", `{"off":true}`, `[1,2,3]`},
		{"\x00", `["tooling"`, `["été/", "a\nb"]`},
		{"collaborators", "null", "null"},
	} {
		f.Add(seed[0], seed[1], seed[2])
	}

	f.Fuzz(func(t *testing.T, scope, off, patterns string) {
		cfg := DecodeConfig(map[string]string{PrefScope: scope, PrefOff: off, PrefPatterns: patterns})

		assert.True(t, cfg.Scope.Valid(), "scope %q", cfg.Scope)
		checkDecodedOff(t, cfg.Off)
		checkDecodedPatterns(t, cfg.Patterns)

		again := DecodeConfig(map[string]string{
			PrefScope: string(cfg.Scope), PrefOff: prefval.EncodeList(cfg.Off), PrefPatterns: prefval.EncodeList(cfg.Patterns),
		})
		assert.Equal(t, cfg, again)
	})
}

// checkCompileIsStable checks that compiling raw padded with whitespace gives the same
// result, and that for ASCII text the case of the pattern does not matter.
func checkCompileIsStable(t *testing.T, raw string, p pattern, err error) {
	t.Helper()

	padded, paddedErr := compile(" \t" + raw + "\n ")
	assert.Equal(t, p, padded)
	assert.Equal(t, errText(err), errText(paddedErr))

	if !isASCII(raw) {
		return
	}

	upper, upperErr := compile(strings.ToUpper(raw))
	assert.Equal(t, err == nil, upperErr == nil, "%q compiles differently in upper case", raw)
	assert.Equal(t, p, upper)
}

// checkNormalized checks NormalizePatterns against compile for one pattern, and that the
// cleaned list reads back unchanged from its stored form. The store holds JSON text, which
// replaces invalid UTF-8 with U+FFFD, so only valid text is expected back byte for byte;
// patterns arrive from the UI as valid text.
func checkNormalized(t *testing.T, raw string, compileErr error) {
	t.Helper()

	trimmed := strings.TrimSpace(raw)

	got, err := NormalizePatterns([]string{raw})

	switch {
	case trimmed == "":
		require.NoError(t, err)
		assert.Empty(t, got)
	case compileErr != nil:
		require.ErrorIs(t, err, ErrInvalidPattern)

		return
	default:
		require.NoError(t, err)
		assert.Equal(t, []string{trimmed}, got)
	}

	if utf8.ValidString(trimmed) {
		assert.Equal(t, got, decodePatterns(prefval.EncodeList(got)))
	}
}

// checkCompiledText checks that a compiled pattern's text passed every rule on text.
func checkCompiledText(t *testing.T, trimmed string, p pattern) {
	t.Helper()

	assert.NotEmpty(t, trimmed)
	assert.LessOrEqual(t, len(trimmed), MaxPatternLength)
	assert.False(t, strings.HasPrefix(trimmed, "!"))
	assert.Negative(t, strings.IndexFunc(trimmed, unicode.IsControl))
	require.NotEmpty(t, p.segs)
	assert.False(t, p.matchesEverything())
	assert.False(t, p.match(nil), "a pattern must not match an empty path")
}

// checkPatternMatch checks how a compiled pattern matches one path.
func checkPatternMatch(t *testing.T, p pattern, filePath string) {
	t.Helper()

	names := splitPath(filePath)
	got := p.match(names)

	assert.Equal(t, got, p.match(names), "matching must be deterministic")

	if isASCII(filePath) {
		assert.Equal(t, got, p.match(splitPath(strings.ToUpper(filePath))), "matching must ignore case")
	}

	if !got {
		return
	}

	if p.dir {
		assert.GreaterOrEqual(t, len(names), 2, "a folder pattern matched %q, which has nothing inside a folder", filePath)
	}

	assert.True(t, p.match(append(slices.Clone(names), childName)), "matching %q must match what is inside it", filePath)
}

// checkDecodedOff checks the decoded categories turned off: sorted, without repeats, and
// each a built-in category that is not locked.
func checkDecodedOff(t *testing.T, off []string) {
	t.Helper()

	require.NotNil(t, off)
	assert.True(t, slices.IsSorted(off))
	assert.Len(t, slices.Compact(slices.Clone(off)), len(off))

	for _, id := range off {
		c, ok := builtIn(id)
		assert.True(t, ok, "unknown category %q", id)
		assert.False(t, c.Locked, "locked category %q turned off", id)
	}
}

// checkDecodedPatterns checks the decoded patterns: at most MaxPatterns, each compiling.
func checkDecodedPatterns(t *testing.T, patterns []string) {
	t.Helper()

	require.NotNil(t, patterns)
	assert.LessOrEqual(t, len(patterns), MaxPatterns)

	for _, p := range patterns {
		_, err := compile(p)
		assert.NoError(t, err, "decoded pattern %q", p)
	}
}

// errText is an error's message, or "" for nil.
func errText(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}

// isASCII reports whether s holds only ASCII bytes, for which upper and lower case
// convert back and forth exactly.
func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] > unicode.MaxASCII {
			return false
		}
	}

	return true
}
