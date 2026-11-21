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
// session. A package's design belongs in its section of docs/architecture.md, which the
// guide's Layout links to in one line.
const guideMaxBytes = 30_000

// architectureDoc holds the design notes the guide links to.
const architectureDoc = "docs/architecture.md"

var (
	// architectureLink matches a link into the design notes from another file.
	architectureLink = regexp.MustCompile(`architecture\.md#([A-Za-z0-9_-]+)`)

	// sameFileLink matches a link to a heading of the file it is in.
	sameFileLink = regexp.MustCompile(`\]\(#([A-Za-z0-9_-]+)\)`)

	// markdownHeading matches an ATX heading line.
	markdownHeading = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*#*\s*$`)
)

// TestProjectGuideStaysSmall keeps CLAUDE.md lean: it is loaded whole into every session, so
// what one package does belongs in docs/architecture.md, not here.
func TestProjectGuideStaysSmall(t *testing.T) {
	t.Parallel()

	text, ok, err := readTextFile(moduleRoot(t), "CLAUDE.md")
	require.NoError(t, err)
	require.True(t, ok, "CLAUDE.md is missing")

	assert.LessOrEqual(t, len(text), guideMaxBytes,
		"CLAUDE.md is %d bytes, over %d: put package detail in its %s section and keep one line in Layout",
		len(text), guideMaxBytes, architectureDoc)
}

// TestProjectGuideLinksResolve checks that every link into docs/architecture.md, from the
// guide, the README, the other docs, and the notes themselves, names one of its headings,
// and that no two headings share an anchor.
func TestProjectGuideLinksResolve(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	notes, ok, err := readTextFile(root, architectureDoc)
	require.NoError(t, err)
	require.True(t, ok, "%s is missing", architectureDoc)

	anchors := map[string]int{}
	for _, heading := range markdownHeadings(notes) {
		anchors[headingAnchor(heading)]++
	}

	for anchor, n := range anchors {
		assert.Equal(t, 1, n, "%s has %d headings with the anchor #%s", architectureDoc, n, anchor)
	}

	for _, link := range sameFileLink.FindAllStringSubmatch(notes, -1) {
		_, found := anchors[link[1]]
		assert.True(t, found, "%s links to #%s, which no heading has", architectureDoc, link[1])
	}

	for _, rel := range []string{"CLAUDE.md", "README.md", "docs/desktop.md", "docs/performance.md", "docs/prompt-files.md", "docs/backup-files.md"} {
		text, found, readErr := readTextFile(root, rel)
		require.NoError(t, readErr)

		if !found {
			continue
		}

		for _, link := range architectureLink.FindAllStringSubmatch(text, -1) {
			_, known := anchors[link[1]]
			assert.True(t, known, "%s links to %s#%s, which no heading has", rel, architectureDoc, link[1])
		}
	}
}

// markdownHeadings returns the text of every heading outside a fenced code block.
func markdownHeadings(text string) []string {
	var headings []string

	fenced := false

	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(line, "```") {
			fenced = !fenced

			continue
		}

		if m := markdownHeading.FindStringSubmatch(line); m != nil && !fenced {
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
	}

	for heading, want := range tests {
		assert.Equal(t, want, headingAnchor(heading), heading)
	}
}
