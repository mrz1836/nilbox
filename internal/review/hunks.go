package review

// A line comment names a line of the diff GitHub shows, and GitHub refuses one on any other
// line, so the sheet checks each one against the file's cached patch before sending it.
// The frontend's parser (frontend/src/lib/diff.ts) follows the same rules, so the sheet
// offers a comment only where it will be taken.

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/uierr"
)

// Side is which version of a file a line comment is on.
type Side string

const (
	// SideLeft is the base version, where the diff's removed lines are.
	SideLeft Side = "LEFT"

	// SideRight is the head version, where the diff's added and unchanged lines are.
	SideRight Side = "RIGHT"
)

// LineComment is one comment on a pull request's diff, as the window sends it.
type LineComment struct {
	// Path is the file's path in the head version.
	Path string `json:"path"`

	// Line is the comment's line, or a range's last line, numbered on Side.
	Line int `json:"line"`

	// Side is the version of the file Line is numbered in.
	Side Side `json:"side"`

	// StartLine is a range's first line, or 0 for a one-line comment.
	StartLine int `json:"startLine"`

	// StartSide is the version StartLine is numbered in: "" for a one-line comment, or
	// Side.
	StartSide Side `json:"startSide"`

	// Body is the comment's Markdown text.
	Body string `json:"body"`
}

var (
	// ErrNotInDiff is returned for a comment on a line the diff doesn't show.
	ErrNotInDiff = errors.New("the line is not part of the diff")

	// ErrInvalidComment is returned for a comment no diff could take: one with no path or
	// no text, an unknown side, a line that is not positive, a range that runs backward or
	// across sides, or a suggested change on the base version.
	ErrInvalidComment = errors.New("the comment is malformed")
)

// patchMaxLine is the largest line number or count a hunk header may name. It keeps the
// arithmetic far from overflow, and the frontend's parser (frontend/src/lib/diff.ts)
// refuses the same headers.
const patchMaxLine = 1_000_000_000

// patchHeader matches a hunk header, "@@ -a,b +c,d @@" with optional section text after
// it, and captures its four numbers. A count left out (",b" or ",d") means 1.
var patchHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// patch is a file's patch as GitHub sends it, read into its hunks. It keeps only what a
// comment check needs: which lines each hunk numbers on each side.
type patch struct {
	// hunks are the patch's hunks in order, one per well-formed header.
	hunks []patchHunk
}

// patchHunk is one hunk of a patch.
type patchHunk struct {
	// before is the hunk's base side, which its context and removed rows number.
	before patchRange

	// after is the hunk's head side, which its context and added rows number.
	after patchRange
}

// patchRange is one side of a hunk: the range its header names, and how many of those
// lines its rows went on to number. The numbered lines are start to start+shown-1, and
// every one of them can take a comment on that side.
type patchRange struct {
	// start is the first line number the header names.
	start int

	// count is how many lines the header says the side covers.
	count int

	// shown is how many lines the hunk's rows numbered, never more than count.
	shown int
}

// parsePatch reads a patch's hunks. It never fails: text before the first hunk header is
// ignored, a malformed header starts no hunk (the lines up to the next good header are
// skipped), and a hunk ends early at a line that is not a row or at a row its header has
// no room left for, skipping the lines after it up to the next header. A trailing "\r" on
// any line is ignored. The frontend's parsePatch follows the same rules, line for line.
func parsePatch(text string) patch {
	var p patch

	open := -1

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")

		if strings.HasPrefix(line, "@@") {
			open = -1

			if h, ok := parsePatchHeader(line); ok {
				p.hunks = append(p.hunks, h)
				open = len(p.hunks) - 1
			}

			continue
		}

		if open >= 0 && !p.hunks[open].take(line) {
			open = -1
		}
	}

	return p
}

// hunkOf returns the index of the first hunk in which line can take a comment on side,
// or -1 and false when none can.
func (p patch) hunkOf(side Side, line int) (int, bool) {
	for i := range p.hunks {
		if p.hunks[i].side(side).holds(line) {
			return i, true
		}
	}

	return -1, false
}

// parsePatchHeader reads a hunk header line into an empty hunk. It reports false for a
// line that is not a header, a number above patchMaxLine, or a range of lines that starts
// at line 0.
func parsePatchHeader(line string) (patchHunk, bool) {
	m := patchHeader.FindStringSubmatch(line)
	if m == nil {
		return patchHunk{}, false
	}

	before, okBefore := parsePatchRange(m[1], m[2])
	after, okAfter := parsePatchRange(m[3], m[4])

	return patchHunk{before: before, after: after}, okBefore && okAfter
}

// parsePatchRange reads one side of a hunk header from its start and count digits, count
// being "" when the header leaves it out. A side with lines must start at line 1 or later;
// an empty one names the line it follows, which can be 0.
func parsePatchRange(start, count string) (patchRange, bool) {
	first, ok := parsePatchNumber(start)
	if !ok {
		return patchRange{}, false
	}

	lines := 1

	if count != "" {
		if lines, ok = parsePatchNumber(count); !ok {
			return patchRange{}, false
		}
	}

	return patchRange{start: first, count: lines}, lines == 0 || first > 0
}

// parsePatchNumber reads a header number made of ASCII digits, refusing one above
// patchMaxLine.
func parsePatchNumber(digits string) (int, bool) {
	n, err := strconv.Atoi(digits)

	return n, err == nil && n <= patchMaxLine
}

// take reads one line after the hunk's header and reports whether the hunk goes on. A
// row numbers the next line on each side it is on, and needs room left on each of them;
// a line that is not a row, or a row with no room, ends the hunk.
func (h *patchHunk) take(line string) bool {
	before, after, ok := patchRowSides(line)
	if !ok || before && !h.before.room() || after && !h.after.room() {
		return false
	}

	if before {
		h.before.shown++
	}

	if after {
		h.after.shown++
	}

	return true
}

// patchRowSides reports which sides a row of a hunk numbers a line on: both for a context
// row (" "), the base for a removed row ("-"), the head for an added row ("+"), and
// neither for a "\" note such as "\ No newline at end of file". ok is false for any other
// line, an empty one included, which is no row.
func patchRowSides(line string) (before, after, ok bool) {
	if line == "" {
		return false, false, false
	}

	switch line[0] {
	case ' ':
		return true, true, true
	case '-':
		return true, false, true
	case '+':
		return false, true, true
	case '\\':
		return false, false, true
	default:
		return false, false, false
	}
}

// side returns the hunk's range on side, or an empty range for a side that is neither.
func (h *patchHunk) side(side Side) patchRange {
	switch side {
	case SideLeft:
		return h.before
	case SideRight:
		return h.after
	default:
		return patchRange{}
	}
}

// room reports whether the header leaves room for another row on this side.
func (r patchRange) room() bool {
	return r.shown < r.count
}

// holds reports whether one of the side's rows numbered line.
func (r patchRange) holds(line int) bool {
	return line >= r.start && line < r.start+r.shown
}

// checkComment checks a comment against its file's patch the way GitHub would, so a
// comment GitHub would refuse never reaches it. text is the file's patch, and hasPatch
// reports whether GitHub sent one at all: it sends none for a binary file or one too large
// to show. A malformed comment wraps ErrInvalidComment, and one on lines the diff doesn't
// show wraps ErrNotInDiff; either carries a sentence for the user.
func checkComment(c LineComment, text string, hasPatch bool) error {
	if err := checkCommentFields(c); err != nil {
		return err
	}

	if err := checkCommentRange(c); err != nil {
		return err
	}

	return checkCommentPlace(c, text, hasPatch)
}

// checkCommentFields checks a comment's path, text, side, and line.
func checkCommentFields(c LineComment) error {
	switch {
	case c.Path == "":
		return uierr.New("A comment must name the file it's on.",
			fmt.Errorf("comment with no path: %w", ErrInvalidComment))
	case strings.TrimSpace(c.Body) == "":
		return uierr.New("Write something in the comment before you post it.",
			fmt.Errorf("comment on %s with no text: %w", c.Path, ErrInvalidComment))
	case c.Side != SideLeft && c.Side != SideRight:
		return uierr.New("A comment must go on the old or the new version of a file.",
			fmt.Errorf("comment on %s on side %q: %w", c.Path, c.Side, ErrInvalidComment))
	case c.Line <= 0:
		return uierr.New(fmt.Sprintf("A comment on %s must name a line, counting from 1.", c.Path),
			fmt.Errorf("comment on line %d of %s: %w", c.Line, c.Path, ErrInvalidComment))
	default:
		return nil
	}
}

// checkCommentRange checks a range's first line and side against its last, and keeps a
// suggested change, on one line or a range, to the head version.
func checkCommentRange(c LineComment) error {
	switch {
	case c.StartLine < 0:
		return uierr.New(fmt.Sprintf("A comment on %s must name a line, counting from 1.", c.Path),
			fmt.Errorf("comment from line %d of %s: %w", c.StartLine, c.Path, ErrInvalidComment))
	case c.StartSide != "" && c.StartSide != c.Side:
		return uierr.New("A comment's lines must all be on the same version of the file.",
			fmt.Errorf("comment on %s from side %q to %q: %w", c.Path, c.StartSide, c.Side, ErrInvalidComment))
	case c.StartLine > 0 && c.StartLine >= c.Line:
		return uierr.New("A comment's first line must come before its last line.",
			fmt.Errorf("comment on %s from line %d to %d: %w", c.Path, c.StartLine, c.Line, ErrInvalidComment))
	case c.Side == SideLeft && isSuggestion(c.Body):
		return uierr.New("A suggested change can only go on the new version of a file.",
			fmt.Errorf("suggestion on the base side of %s: %w", c.Path, ErrInvalidComment))
	default:
		return nil
	}
}

// checkCommentPlace checks that the diff shows the comment's line, and that a range's two
// ends sit in the same hunk.
func checkCommentPlace(c LineComment, text string, hasPatch bool) error {
	var p patch
	if hasPatch {
		p = parsePatch(text)
	}

	if len(p.hunks) == 0 {
		return uierr.New(fmt.Sprintf("GitHub shows no diff for %s, so it can't take line comments.", c.Path),
			fmt.Errorf("comment on %s, which has no diff: %w", c.Path, ErrNotInDiff))
	}

	last, ok := p.hunkOf(c.Side, c.Line)
	if !ok {
		return notInDiff(c, c.Line)
	}

	if c.StartLine == 0 {
		return nil
	}

	first, ok := p.hunkOf(c.Side, c.StartLine)
	if !ok {
		return notInDiff(c, c.StartLine)
	}

	if first != last {
		return uierr.New("A comment's lines must sit in one part of the diff.",
			fmt.Errorf("comment on %s from line %d in hunk %d to line %d in hunk %d: %w",
				c.Path, c.StartLine, first, c.Line, last, ErrNotInDiff))
	}

	return nil
}

// notInDiff is the error for a comment on a line of its side that the diff doesn't show.
func notInDiff(c LineComment, line int) error {
	file := c.Path
	if c.Side == SideLeft {
		file = "the old version of " + c.Path
	}

	return uierr.New(fmt.Sprintf("Line %d of %s isn't part of the diff, so GitHub can't take a comment there.", line, file),
		fmt.Errorf("comment on line %d (%s) of %s: %w", line, c.Side, c.Path, ErrNotInDiff))
}

// isSuggestion reports whether a comment's body suggests a change: whether one of its
// lines opens a suggestion block. Such a line starts, after any spaces or tabs, with a
// fence of three or more backticks and then, after any spaces or tabs, "suggestion". The
// frontend's isSuggestion follows the same rule.
func isSuggestion(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimLeft(line, " \t")
		info := strings.TrimLeft(line, "`")

		if len(line)-len(info) >= 3 && strings.HasPrefix(strings.TrimLeft(info, " \t"), "suggestion") {
			return true
		}
	}

	return false
}
