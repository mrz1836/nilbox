package repoguard_test

import (
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// guideMaxBytes caps the project guide, CLAUDE.md, which coding agents load whole into every
// session. A package's design belongs in its file under docs/architecture/, which the
// guide's Layout links to in one line.
const guideMaxBytes = 30_000

// markdownHeading matches an ATX heading line.
var markdownHeading = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*#*\s*$`)

// TestProjectGuideStaysSmall keeps CLAUDE.md lean: it is loaded whole into every session, so
// what one package does belongs in its file under docs/architecture/, not here.
func TestProjectGuideStaysSmall(t *testing.T) {
	t.Parallel()

	text, ok, err := readTextFile(moduleRoot(t), "CLAUDE.md")
	require.NoError(t, err)
	require.True(t, ok, "CLAUDE.md is missing")

	assert.LessOrEqual(t, len(text), guideMaxBytes,
		"CLAUDE.md is %d bytes, over %d: put package detail in its file under docs/architecture/ "+
			"and keep one line in Layout", len(text), guideMaxBytes)
}

// markdownFence follows a markdown text's fenced code blocks, line by line: a fence of three
// or more backticks or tildes, indented (as inside a list) or not, closed by a run of the
// same character at least as long.
type markdownFence struct {
	open   bool
	marker rune
	length int
}

// inside reports whether line is part of a fenced code block, its fence lines included.
func (f *markdownFence) inside(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	marker, length := fenceRun(trimmed)

	if !f.open {
		if length < 3 {
			return false
		}

		f.open, f.marker, f.length = true, marker, length

		return true
	}

	if marker == f.marker && length >= f.length && strings.TrimSpace(trimmed[length:]) == "" {
		f.open = false
	}

	return true
}

// fenceRun returns the backtick or tilde a line starts with and how many of it there are.
func fenceRun(line string) (rune, int) {
	if line == "" || (line[0] != '`' && line[0] != '~') {
		return 0, 0
	}

	marker := line[0]
	n := 0

	for n < len(line) && line[n] == marker {
		n++
	}

	return rune(marker), n
}

// markdownHeadings returns the text of every heading outside a fenced code block.
func markdownHeadings(text string) []string {
	var headings []string

	var fence markdownFence

	for line := range strings.SplitSeq(text, "\n") {
		if fence.inside(line) {
			continue
		}

		if m := markdownHeading.FindStringSubmatch(line); m != nil {
			headings = append(headings, m[1])
		}
	}

	return headings
}

// headingAnchor is the anchor GitHub gives a heading: lower case, with every character but
// letters, digits, spaces, hyphens, and underscores dropped, and each space a hyphen.
func headingAnchor(heading string) string {
	var b strings.Builder

	for _, r := range strings.ToLower(heading) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}

	return b.String()
}

func TestHeadingAnchor(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"internal/inbox":                                      "internalinbox",
		"internal/budget_v2":                                  "internalbudget_v2",
		"internal/event and internal/desktop":                 "internalevent-and-internaldesktop",
		"Agents in the background":                            "agents-in-the-background",
		"Verify, tries, the loop guard, and profile fallback": "verify-tries-the-loop-guard-and-profile-fallback",
		"Merge it and Post the agent's review":                "merge-it-and-post-the-agents-review",
		"📚 Documentation":                                     "-documentation",
		"Settings → Autopilot":                                "settings--autopilot",
	}

	for heading, want := range tests {
		assert.Equal(t, want, headingAnchor(heading), heading)
	}
}

func TestMarkdownHeadingsSkipFences(t *testing.T) {
	t.Parallel()

	text := strings.Join([]string{
		"# Title",
		"- a list item:",
		"",
		"  ```bash",
		"  # a shell comment",
		"  ```",
		"~~~~",
		"## inside tildes",
		"~~~",
		"still inside",
		"~~~~",
		"## After",
	}, "\n")

	assert.Equal(t, []string{"Title", "After"}, markdownHeadings(text))
}
