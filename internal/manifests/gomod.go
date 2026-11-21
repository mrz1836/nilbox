package manifests

import (
	"strconv"
	"strings"
)

// The go.mod directives Deps reads.
const (
	moduleVerb    = "module"
	goVerb        = "go"
	toolchainVerb = "toolchain"
	requireVerb   = "require"
)

// goMod is what Deps reads of a go.mod: its module path, go directive, and toolchain, and
// how many modules it requires directly and indirectly.
type goMod struct {
	module    string
	goVersion string
	toolchain string
	direct    int
	indirect  int
}

// parseGoMod reads a go.mod's module path, go directive, toolchain, and requirements, the
// single-line and block forms alike, line by line as go.mod's grammar has it, by hand:
// golang.org/x/mod is no dependency of the app's. A directive it doesn't read (replace,
// exclude, retract, tool, godebug, ignore) is skipped, its block too, so a go.mod from a
// newer Go still reads. ok is false for one that names no module.
func parseGoMod(data []byte) (mod goMod, ok bool) {
	block := ""

	for line := range strings.Lines(string(data)) {
		code, comment := splitComment(line)
		fields := strings.Fields(code)

		if block == "" {
			block = mod.directive(fields, comment)
		} else {
			block = mod.inBlock(block, fields, comment)
		}
	}

	return mod, mod.module != ""
}

// directive reads a line outside any block, and returns the verb of the block it opens,
// or "".
func (m *goMod) directive(fields []string, comment string) string {
	if verb, opens := blockStart(fields); opens {
		return verb
	}

	if len(fields) < 2 {
		return ""
	}

	switch fields[0] {
	case moduleVerb:
		if m.module == "" {
			m.module = unquote(fields[1])
		}
	case goVerb:
		m.goVersion = fields[1]
	case toolchainVerb:
		m.toolchain = fields[1]
	case requireVerb:
		m.require(fields[1:], comment)
	}

	return ""
}

// inBlock reads a line inside a block of block's, and returns the block still open after
// it, or "" once a line closes it.
func (m *goMod) inBlock(block string, fields []string, comment string) string {
	if len(fields) > 0 && fields[0] == ")" {
		return ""
	}

	if block == requireVerb {
		m.require(fields, comment)
	}

	return block
}

// require counts a requirement, "<path> <version>", as indirect when its comment says so.
func (m *goMod) require(fields []string, comment string) {
	if len(fields) < 2 {
		return
	}

	if comment == "indirect" || strings.HasPrefix(comment, "indirect;") {
		m.indirect++
	} else {
		m.direct++
	}
}

// blockStart reports a line that opens a block, "<verb> (" or "<verb>(", and its verb.
func blockStart(fields []string) (string, bool) {
	switch {
	case len(fields) == 2 && fields[1] == "(":
		return fields[0], true
	case len(fields) == 1 && len(fields[0]) > 1 && strings.HasSuffix(fields[0], "("):
		return strings.TrimSuffix(fields[0], "("), true
	default:
		return "", false
	}
}

// splitComment splits a go.mod line at its comment: the code before "//", and the
// comment's text after it, trimmed.
func splitComment(line string) (code, comment string) {
	code, comment, _ = strings.Cut(line, "//")

	return code, strings.TrimSpace(comment)
}

// unquote returns a go.mod word without its quotes, when it is a quoted string.
func unquote(word string) string {
	if s, err := strconv.Unquote(word); err == nil {
		return s
	}

	return word
}
