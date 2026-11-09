package review

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/uierr"
)

// The sample patches below are the ones frontend/src/lib/diff.test.ts reads too, with the
// same lines accepted on each side, so the two parsers are held to one answer.

// multiHunkPatch is a realistic two-hunk patch. Its first hunk numbers lines 1 to 4 of
// the base and 1 to 6 of the head; its second, lines 10 to 15 and 12 to 18.
func multiHunkPatch() string {
	return strings.Join([]string{
		"@@ -1,4 +1,6 @@",
		" package main",
		" ",
		`-import "fmt"`,
		"+import (",
		`+	"fmt"`,
		"+)",
		" ",
		"@@ -10,6 +12,7 @@ func main() {",
		" 	a := 1",
		" 	b := 2",
		"-	fmt.Println(a)",
		"+	fmt.Println(a, b)",
		"+	fmt.Println(b)",
		" 	c := 3",
		" 	_ = c",
		" }",
	}, "\n")
}

// junkPatch has git's file headers before its first hunk, and a malformed header whose
// lines are skipped.
func junkPatch() string {
	return strings.Join([]string{
		"diff --git a/x b/x",
		"index 1234567..89abcde 100644",
		"--- a/x",
		"+++ b/x",
		" stray context",
		"@@ -1,2 +1,2 @@",
		" a",
		"-b",
		"+c",
		"@@ -x,1 +5 @@",
		"+skipped",
		"@@ -10,1 +10,1 @@",
		"-d",
		"+e",
	}, "\n")
}

// samplePatches are patches with the lines each takes comments on, on each side.
func samplePatches() []struct {
	name        string
	text        string
	left, right []int
} {
	return []struct {
		name        string
		text        string
		left, right []int
	}{
		{name: "empty", text: "", left: nil, right: nil},
		{
			name: "multiple hunks", text: multiHunkPatch(),
			left:  []int{1, 2, 3, 4, 10, 11, 12, 13, 14, 15},
			right: []int{1, 2, 3, 4, 5, 6, 12, 13, 14, 15, 16, 17, 18},
		},
		{name: "new file", text: "@@ -0,0 +1,3 @@\n+one\n+two\n+three", left: nil, right: []int{1, 2, 3}},
		{name: "deleted file", text: "@@ -1,2 +0,0 @@\n-one\n-two", left: []int{1, 2}, right: nil},
		{name: "omitted counts", text: "@@ -1 +1 @@\n-old\n+new", left: []int{1}, right: []int{1}},
		{
			name: "no newline at end of file",
			text: "@@ -1,2 +1,2 @@\n keep\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file",
			left: []int{1, 2}, right: []int{1, 2},
		},
		{
			name: "CRLF line endings", text: "@@ -1,2 +1,2 @@ section\r\n line\r\n-old\r\n+new\r\n",
			left: []int{1, 2}, right: []int{1, 2},
		},
		{name: "junk and a malformed header", text: junkPatch(), left: []int{1, 2, 10}, right: []int{1, 2, 10}},
		{
			name: "rows past the header's counts",
			text: "@@ -1,1 +1,1 @@\n a\n+extra\n b\n@@ -5,1 +5,2 @@\n c",
			left: []int{1, 5}, right: []int{1, 5},
		},
		{name: "a line that is not a row", text: "@@ -1,3 +1,3 @@\n a\njunk\n b", left: []int{1}, right: []int{1}},
		{name: "an empty line", text: "@@ -1,3 +1,3 @@\n a\n\n b", left: []int{1}, right: []int{1}},
		{name: "a context row with one side full", text: "@@ -1,1 +1,2 @@\n a\n b", left: []int{1}, right: []int{1}},
		{name: "a removed row past the count", text: "@@ -1,1 +1,1 @@\n-a\n-b\n+c", left: []int{1}, right: nil},
		{name: "a range from line 0", text: "@@ -0,1 +1,1 @@\n-a\n+b", left: nil, right: nil},
		{name: "a number too large", text: "@@ -1000000001 +1 @@\n-a\n+b", left: nil, right: nil},
		{name: "a count too large", text: "@@ -1,1000000001 +1 @@\n-a\n+b", left: nil, right: nil},
		{name: "a number past int", text: "@@ -1 +99999999999999999999999 @@\n-a\n+b", left: nil, right: nil},
		{name: "a combined diff", text: "@@@ -1,2 -1,2 +1,3 @@@\n  a\n+ b", left: nil, right: nil},
		{name: "a header missing a count", text: "@@ -1, +1 @@\n a", left: nil, right: nil},
		{name: "the largest line", text: "@@ -1000000000 +1000000000 @@\n x", left: []int{1e9}, right: []int{1e9}},
		{name: "a note before any row", text: "@@ -3 +3 @@\n\\ note\n x", left: []int{3}, right: []int{3}},
	}
}

// commentable lists the lines p takes comments on, on side, among 0 to 20 and the lines
// near the largest line.
func commentable(p patch, side Side) []int {
	var lines []int

	for _, line := range append(rangeOf(0, 20), rangeOf(1e9-1, 1e9+1)...) {
		if _, ok := p.hunkOf(side, line); ok {
			lines = append(lines, line)
		}
	}

	return lines
}

// rangeOf lists the numbers from first to last.
func rangeOf(first, last int) []int {
	out := make([]int, 0, last-first+1)

	for n := first; n <= last; n++ {
		out = append(out, n)
	}

	return out
}

func TestParsePatchCommentableLines(t *testing.T) {
	t.Parallel()

	for _, tt := range samplePatches() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := parsePatch(tt.text)

			assert.Equal(t, tt.left, commentable(p, SideLeft), "base side")
			assert.Equal(t, tt.right, commentable(p, SideRight), "head side")
		})
	}
}

func TestParsePatchHunks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want []patchHunk
	}{
		{name: "empty", text: "", want: nil},
		{
			name: "multiple hunks", text: multiHunkPatch(),
			want: []patchHunk{
				{before: patchRange{start: 1, count: 4, shown: 4}, after: patchRange{start: 1, count: 6, shown: 6}},
				{before: patchRange{start: 10, count: 6, shown: 6}, after: patchRange{start: 12, count: 7, shown: 7}},
			},
		},
		{
			name: "new file", text: "@@ -0,0 +1,3 @@\n+one\n+two\n+three",
			want: []patchHunk{{before: patchRange{}, after: patchRange{start: 1, count: 3, shown: 3}}},
		},
		{
			name: "deleted file", text: "@@ -1,2 +0,0 @@\n-one\n-two",
			want: []patchHunk{{before: patchRange{start: 1, count: 2, shown: 2}, after: patchRange{}}},
		},
		{
			name: "omitted counts", text: "@@ -1 +1 @@\n-old\n+new",
			want: []patchHunk{{before: patchRange{start: 1, count: 1, shown: 1}, after: patchRange{start: 1, count: 1, shown: 1}}},
		},
		{
			name: "an insertion after a line", text: "@@ -5,0 +6,1 @@\n+new",
			want: []patchHunk{{before: patchRange{start: 5}, after: patchRange{start: 6, count: 1, shown: 1}}},
		},
		{
			name: "junk and a malformed header", text: junkPatch(),
			want: []patchHunk{
				{before: patchRange{start: 1, count: 2, shown: 2}, after: patchRange{start: 1, count: 2, shown: 2}},
				{before: patchRange{start: 10, count: 1, shown: 1}, after: patchRange{start: 10, count: 1, shown: 1}},
			},
		},
		{
			name: "a hunk cut short", text: "@@ -1,3 +1,3 @@\n a\njunk\n b\n c",
			want: []patchHunk{{before: patchRange{start: 1, count: 3, shown: 1}, after: patchRange{start: 1, count: 3, shown: 1}}},
		},
		{
			name: "a header with no rows", text: "@@ -0,0 +0,0 @@",
			want: []patchHunk{{}},
		},
		{name: "only junk", text: "Binary files differ\n", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, parsePatch(tt.text).hunks)
		})
	}
}

func TestHunkOf(t *testing.T) {
	t.Parallel()

	p := parsePatch(multiHunkPatch())

	tests := []struct {
		name string
		side Side
		line int
		want int
		ok   bool
	}{
		{name: "an added line", side: SideRight, line: 3, want: 0, ok: true},
		{name: "a context line on the head side", side: SideRight, line: 16, want: 1, ok: true},
		{name: "a removed line", side: SideLeft, line: 12, want: 1, ok: true},
		{name: "a context line on the base side", side: SideLeft, line: 4, want: 0, ok: true},
		{name: "a context line by its base number", side: SideLeft, line: 13, want: 1, ok: true},
		{name: "a context line by its head number", side: SideRight, line: 13, want: 1, ok: true},
		{name: "a head number on the base side", side: SideLeft, line: 18, want: -1, ok: false},
		{name: "a base number on the head side", side: SideRight, line: 10, want: -1, ok: false},
		{name: "between hunks", side: SideRight, line: 9, want: -1, ok: false},
		{name: "past the last hunk", side: SideLeft, line: 16, want: -1, ok: false},
		{name: "line 0", side: SideRight, line: 0, want: -1, ok: false},
		{name: "a negative line", side: SideLeft, line: -1, want: -1, ok: false},
		{name: "no side", side: "", line: 3, want: -1, ok: false},
		{name: "an unknown side", side: "right", line: 3, want: -1, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := p.hunkOf(tt.side, tt.line)

			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// appPath is the file the comment tests comment on.
const appPath = "src/app.go"

// commentOn is a valid comment on line of side in src/app.go, which a test changes.
func commentOn(side Side, line int) LineComment {
	return LineComment{Path: appPath, Line: line, Side: side, Body: "Looks good."}
}

// suggestion is a comment body that suggests a change.
const suggestion = "Try this:\n```suggestion\n\tfmt.Println(a)\n```"

func TestCheckCommentAccepts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		comment LineComment
	}{
		{name: "an added line", comment: commentOn(SideRight, 3)},
		{name: "a context line on the head side", comment: commentOn(SideRight, 6)},
		{name: "a removed line", comment: commentOn(SideLeft, 12)},
		{name: "a context line on the base side", comment: commentOn(SideLeft, 1)},
		{name: "a range", comment: LineComment{Path: appPath, Line: 5, Side: SideRight, StartLine: 3, Body: "Why?"}},
		{
			name:    "a range naming its start side",
			comment: LineComment{Path: appPath, Line: 15, Side: SideLeft, StartLine: 10, StartSide: SideLeft, Body: "Why?"},
		},
		{
			name:    "a one-line comment naming its start side",
			comment: LineComment{Path: appPath, Line: 13, Side: SideRight, StartSide: SideRight, Body: "Why?"},
		},
		{name: "a suggestion", comment: LineComment{Path: appPath, Line: 14, Side: SideRight, Body: suggestion}},
		{
			name:    "a suggestion over a range",
			comment: LineComment{Path: appPath, Line: 15, Side: SideRight, StartLine: 14, Body: suggestion},
		},
		{
			name:    "a suggestion word on the base side",
			comment: LineComment{Path: appPath, Line: 12, Side: SideLeft, Body: "A suggestion: use ```suggestion blocks."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.NoError(t, checkComment(tt.comment, multiHunkPatch(), true))
		})
	}
}

func TestCheckCommentRefuses(t *testing.T) {
	t.Parallel()

	withRange := func(side Side, start, end int) LineComment {
		c := commentOn(side, end)
		c.StartLine = start

		return c
	}

	tests := []struct {
		name     string
		comment  LineComment
		sentinel error
		message  string
	}{
		{
			name: "no path", comment: LineComment{Line: 3, Side: SideRight, Body: "Hi"},
			sentinel: ErrInvalidComment, message: "A comment must name the file it's on.",
		},
		{
			name: "no body", comment: LineComment{Path: appPath, Line: 3, Side: SideRight},
			sentinel: ErrInvalidComment, message: "Write something in the comment before you post it.",
		},
		{
			name: "a body of only space", comment: LineComment{Path: appPath, Line: 3, Side: SideRight, Body: " \n\t "},
			sentinel: ErrInvalidComment, message: "Write something in the comment before you post it.",
		},
		{
			name: "no side", comment: commentOn("", 3),
			sentinel: ErrInvalidComment, message: "A comment must go on the old or the new version of a file.",
		},
		{
			name: "an unknown side", comment: commentOn("right", 3),
			sentinel: ErrInvalidComment, message: "A comment must go on the old or the new version of a file.",
		},
		{
			name: "line 0", comment: commentOn(SideRight, 0),
			sentinel: ErrInvalidComment, message: "A comment on src/app.go must name a line, counting from 1.",
		},
		{
			name: "a negative line", comment: commentOn(SideLeft, -4),
			sentinel: ErrInvalidComment, message: "A comment on src/app.go must name a line, counting from 1.",
		},
		{
			name: "a negative start line", comment: withRange(SideRight, -1, 3),
			sentinel: ErrInvalidComment, message: "A comment on src/app.go must name a line, counting from 1.",
		},
		{
			name: "a range across sides",
			comment: LineComment{
				Path: appPath, Line: 5, Side: SideRight, StartLine: 3, StartSide: SideLeft, Body: "Hi",
			},
			sentinel: ErrInvalidComment, message: "A comment's lines must all be on the same version of the file.",
		},
		{
			name:     "an unknown start side",
			comment:  LineComment{Path: appPath, Line: 5, Side: SideRight, StartSide: "middle", Body: "Hi"},
			sentinel: ErrInvalidComment, message: "A comment's lines must all be on the same version of the file.",
		},
		{
			name: "a range of one line", comment: withRange(SideRight, 3, 3),
			sentinel: ErrInvalidComment, message: "A comment's first line must come before its last line.",
		},
		{
			name: "a backward range", comment: withRange(SideRight, 5, 3),
			sentinel: ErrInvalidComment, message: "A comment's first line must come before its last line.",
		},
		{
			name:     "a suggestion on the base side",
			comment:  LineComment{Path: appPath, Line: 12, Side: SideLeft, Body: suggestion},
			sentinel: ErrInvalidComment, message: "A suggested change can only go on the new version of a file.",
		},
		{
			name:     "a suggestion over a range on the base side",
			comment:  LineComment{Path: appPath, Line: 12, Side: SideLeft, StartLine: 11, Body: suggestion},
			sentinel: ErrInvalidComment, message: "A suggested change can only go on the new version of a file.",
		},
		{
			name: "a line between hunks", comment: commentOn(SideRight, 7),
			sentinel: ErrNotInDiff, message: "Line 7 of src/app.go isn't part of the diff, so GitHub can't take a comment there.",
		},
		{
			name: "a head line past the last hunk", comment: commentOn(SideRight, 19),
			sentinel: ErrNotInDiff, message: "Line 19 of src/app.go isn't part of the diff, so GitHub can't take a comment there.",
		},
		{
			name: "a base line not in the diff", comment: commentOn(SideLeft, 16), sentinel: ErrNotInDiff,
			message: "Line 16 of the old version of src/app.go isn't part of the diff, so GitHub can't take a comment there.",
		},
		{
			name: "a range starting outside the diff", comment: withRange(SideRight, 8, 12),
			sentinel: ErrNotInDiff, message: "Line 8 of src/app.go isn't part of the diff, so GitHub can't take a comment there.",
		},
		{
			name: "a range ending outside the diff", comment: withRange(SideRight, 5, 8),
			sentinel: ErrNotInDiff, message: "Line 8 of src/app.go isn't part of the diff, so GitHub can't take a comment there.",
		},
		{
			name: "a range across hunks", comment: withRange(SideRight, 6, 12),
			sentinel: ErrNotInDiff, message: "A comment's lines must sit in one part of the diff.",
		},
		{
			name: "a base range across hunks", comment: withRange(SideLeft, 4, 10),
			sentinel: ErrNotInDiff, message: "A comment's lines must sit in one part of the diff.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := checkComment(tt.comment, multiHunkPatch(), true)

			require.ErrorIs(t, err, tt.sentinel)
			assert.Equal(t, tt.message, uierr.Message(err))
		})
	}
}

func TestCheckCommentOnAFileWithoutADiff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		text     string
		hasPatch bool
	}{
		{name: "no patch", path: "docs/logo.png", text: "", hasPatch: false},
		{name: "a patch it was told is absent", path: appPath, text: multiHunkPatch(), hasPatch: false},
		{name: "an empty patch", path: appPath, text: "", hasPatch: true},
		{name: "a patch with no hunks", path: appPath, text: "Binary files differ", hasPatch: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := commentOn(SideRight, 3)
			c.Path = tt.path

			err := checkComment(c, tt.text, tt.hasPatch)

			require.ErrorIs(t, err, ErrNotInDiff)
			assert.Equal(t, "GitHub shows no diff for "+tt.path+", so it can't take line comments.", uierr.Message(err))
		})
	}
}

func TestIsSuggestion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "a suggestion block", body: "```suggestion\nreturn nil\n```", want: true},
		{name: "after text", body: "Try:\n```suggestion\nreturn nil\n```", want: true},
		{name: "indented with spaces", body: "   ```suggestion\nx\n```", want: true},
		{name: "indented with a tab", body: "\t```suggestion\nx\n```", want: true},
		{name: "a longer fence", body: "````suggestion\nx\n````", want: true},
		{name: "space before the word", body: "``` suggestion\nx\n```", want: true},
		{name: "a CRLF line", body: "```suggestion\r\nx\r\n```", want: true},
		{name: "an empty suggestion", body: "```suggestion\n```", want: true},
		{name: "a code block", body: "```go\nreturn nil\n```", want: false},
		{name: "two backticks", body: "``suggestion\nx\n``", want: false},
		{name: "mid-line", body: "Use a ```suggestion block.", want: false},
		{name: "another case", body: "```Suggestion\nx\n```", want: false},
		{name: "plain text", body: "suggestion", want: false},
		{name: "empty", body: "", want: false},
		{name: "after a lone carriage return", body: "x\r```suggestion", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, isSuggestion(tt.body))
		})
	}
}

// FuzzParsePatch checks that any text parses without panicking into hunks that number no
// more lines than their headers allow, and that every line hunkOf accepts lies in the
// ranges its hunk's header names, on the side asked, while every numbered line is accepted.
func FuzzParsePatch(f *testing.F) {
	for _, s := range samplePatches() {
		f.Add(s.text, 3)
	}

	f.Add("@@ -1,2 +1,2 @@\n a\n@@ -1,2 +1,2 @@\n a\n b", 2)
	f.Add("@@ -1 +1 @@\r\n\\\r\n-\r\n+", 1)
	f.Add("@@ -5,0 +6 @@\n+x", 6)

	f.Fuzz(func(t *testing.T, text string, line int) {
		p := parsePatch(text)

		require.LessOrEqual(t, len(p.hunks), strings.Count(text, "@@"))

		for _, side := range []Side{SideLeft, SideRight} {
			checkAccepted(t, p, side, line)

			for i := range p.hunks {
				checkNumbered(t, p, side, p.hunks[i].side(side))
			}
		}

		_, ok := p.hunkOf("", line)
		assert.False(t, ok)
	})
}

// checkNumbered checks that one side of a hunk numbers no more lines than its header
// allows, that p accepts every line it numbers, and that the lines just around them are
// accepted only where some hunk's header covers them.
func checkNumbered(t *testing.T, p patch, side Side, r patchRange) {
	t.Helper()

	require.LessOrEqual(t, r.shown, r.count)

	for line := r.start - 1; line <= r.start+r.shown; line++ {
		_, ok := checkAccepted(t, p, side, line)
		if r.holds(line) {
			require.True(t, ok, "line %d of %s is numbered but refused", line, side)
		}
	}
}

// checkAccepted asks p for the hunk where line takes a comment on side and, when there
// is one, checks that the line lies in that hunk's header range and its numbered lines.
func checkAccepted(t *testing.T, p patch, side Side, line int) (int, bool) {
	t.Helper()

	i, ok := p.hunkOf(side, line)
	if !ok {
		require.Equal(t, -1, i)

		return i, ok
	}

	r := p.hunks[i].side(side)

	require.GreaterOrEqual(t, line, 1)
	require.GreaterOrEqual(t, line, r.start, "line %d of %s", line, side)
	require.Less(t, line, r.start+r.count, "line %d of %s", line, side)
	require.Less(t, line, r.start+r.shown, "line %d of %s", line, side)

	return i, ok
}

// FuzzCheckComment checks that any comment against any patch either passes every rule or
// is refused with one of the two errors and a sentence for the user.
func FuzzCheckComment(f *testing.F) {
	f.Add(multiHunkPatch(), true, appPath, "Hi", 3, "RIGHT", 0, "")
	f.Add(multiHunkPatch(), true, appPath, suggestion, 12, "LEFT", 10, "LEFT")
	f.Add(multiHunkPatch(), true, appPath, suggestion, 15, "RIGHT", 13, "RIGHT")
	f.Add(multiHunkPatch(), true, appPath, "Hi", 12, "RIGHT", 6, "")
	f.Add("@@ -0,0 +1,3 @@\n+a\n+b\n+c", true, "new.txt", " ", 2, "RIGHT", 1, "LEFT")
	f.Add("", false, "", "", 0, "", -1, "x")

	f.Fuzz(func(t *testing.T, text string, hasPatch bool, path, body string, line int, side string,
		startLine int, startSide string,
	) {
		c := LineComment{
			Path: path, Line: line, Side: Side(side), StartLine: startLine, StartSide: Side(startSide), Body: body,
		}

		err := checkComment(c, text, hasPatch)
		if err != nil {
			require.NotEqual(t, errors.Is(err, ErrNotInDiff), errors.Is(err, ErrInvalidComment), "%v", err)
			assert.True(t, strings.HasSuffix(uierr.Message(err), "."), "%q", uierr.Message(err))

			return
		}

		checkAcceptedComment(t, c, text, hasPatch)
	})
}

// checkAcceptedComment checks that a comment checkComment accepted keeps every rule.
func checkAcceptedComment(t *testing.T, c LineComment, text string, hasPatch bool) {
	t.Helper()

	require.True(t, hasPatch)
	require.NotEmpty(t, c.Path)
	require.NotEmpty(t, strings.TrimSpace(c.Body))
	require.Contains(t, []Side{SideLeft, SideRight}, c.Side)
	require.Positive(t, c.Line)

	p := parsePatch(text)

	last, ok := p.hunkOf(c.Side, c.Line)
	require.True(t, ok)

	require.Contains(t, []Side{"", c.Side}, c.StartSide)

	if c.Side == SideLeft {
		require.False(t, isSuggestion(c.Body))
	}

	if c.StartLine == 0 {
		return
	}

	require.Less(t, c.StartLine, c.Line)

	first, ok := p.hunkOf(c.Side, c.StartLine)
	require.True(t, ok)
	require.Equal(t, last, first)
}
