package sensitive

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// globChars are the characters that make a name a glob rather than a literal.
const globChars = `*?[\`

// everyPath returns paths that any pattern matching every file also matches: a file at
// the root, one in a folder, and one nested deeper.
func everyPath() [][]string {
	return [][]string{{"a"}, {"b", "c"}, {"d", "e", "f.txt"}}
}

// pattern is one compiled pattern.
type pattern struct {
	// anchored patterns are matched from the repository's root; the rest match a name at
	// any depth.
	anchored bool

	// dir patterns match only a folder, and so only what is inside it.
	dir bool

	segs []segment
}

// segment is one name of a pattern, lowercased.
type segment struct {
	text string

	// literal names have no glob characters and match by equality.
	literal bool

	// doubleStar is a ** segment, which matches any number of folders.
	doubleStar bool
}

// compile parses a pattern as entered, or returns an ErrInvalidPattern saying why it
// cannot be used.
func compile(raw string) (pattern, error) {
	p := strings.TrimSpace(raw)
	if err := checkText(p); err != nil {
		return pattern{}, err
	}

	body := strings.Trim(p, "/")
	out := pattern{
		dir: strings.HasSuffix(p, "/"),

		// A slash at the start or in the middle anchors the pattern at the root.
		anchored: strings.HasPrefix(p, "/") || strings.Contains(body, "/"),
	}

	segs, err := parseNames(p, body)
	if err != nil {
		return pattern{}, err
	}

	out.segs = segs

	switch {
	case len(out.segs) == 0:
		return pattern{}, fmt.Errorf("%w %q: it names no file or folder", ErrInvalidPattern, shorten(p))
	case !out.anchored && out.segs[0].doubleStar, out.matchesEverything():
		// "**" alone names any folder, and "*" or "**/*" any file.
		return pattern{}, fmt.Errorf("%w %q: it matches every file", ErrInvalidPattern, shorten(p))
	}

	return out, nil
}

// checkText refuses a pattern's text before it is parsed: empty, too long, with control
// characters, or an exclusion.
func checkText(p string) error {
	switch {
	case p == "":
		return fmt.Errorf("%w: it is empty", ErrInvalidPattern)
	case len(p) > MaxPatternLength:
		return fmt.Errorf("%w %q: longer than %d characters", ErrInvalidPattern, shorten(p), MaxPatternLength)
	case strings.IndexFunc(p, unicode.IsControl) >= 0:
		return fmt.Errorf("%w %q: it has control characters", ErrInvalidPattern, shorten(p))
	case strings.HasPrefix(p, "!"):
		return fmt.Errorf("%w %q: exclusions (!) aren't supported", ErrInvalidPattern, shorten(p))
	default:
		return nil
	}
}

// parseNames splits a pattern's body (p without its leading and trailing slashes) into
// lowercased segments, skipping empty and "." names.
func parseNames(p, body string) ([]segment, error) {
	var (
		segs  []segment
		stars int
	)

	for name := range strings.SplitSeq(strings.ToLower(body), "/") {
		switch name {
		case "", ".":
			continue
		case "..":
			return nil, fmt.Errorf("%w %q: .. isn't allowed", ErrInvalidPattern, shorten(p))
		case "**":
			if stars++; stars > maxDoubleStars {
				return nil, fmt.Errorf("%w %q: more than %d ** parts", ErrInvalidPattern, shorten(p), maxDoubleStars)
			}

			segs = append(segs, segment{doubleStar: true})
		default:
			if _, err := path.Match(name, ""); err != nil {
				return nil, fmt.Errorf("%w %q: %q is not a valid name pattern", ErrInvalidPattern, shorten(p), name)
			}

			segs = append(segs, segment{text: name, literal: !strings.ContainsAny(name, globChars)})
		}
	}

	return segs, nil
}

// matchesEverything reports a pattern that matches every probe path, such as * or **/*,
// which would flag every file of every pull request.
func (p pattern) matchesEverything() bool {
	for _, probe := range everyPath() {
		if !p.match(probe) {
			return false
		}
	}

	return true
}

// match reports whether the pattern matches a path, given as its lowercased names.
func (p pattern) match(names []string) bool {
	if !p.anchored {
		// One name, matched against each folder on the path and, unless the pattern is
		// for a folder, the file's own name.
		last := len(names)
		if p.dir {
			last--
		}

		return slices.ContainsFunc(names[:max(last, 0)], p.segs[0].match)
	}

	return matchFrom(p.segs, names, p.dir)
}

// matchFrom reports whether segs match the start of names: all of names, or a folder
// whose contents are the rest. A dir pattern must leave at least one name, so it never
// matches a file of the same name.
func matchFrom(segs []segment, names []string, dir bool) bool {
	switch {
	case len(segs) == 0:
		return !dir || len(names) > 0
	case segs[0].doubleStar:
		return matchAnyDepth(segs[1:], names, dir)
	case len(names) == 0 || !segs[0].match(names[0]):
		return false
	default:
		return matchFrom(segs[1:], names[1:], dir)
	}
}

// matchAnyDepth matches what follows a ** segment: segs against names after skipping
// any number of them, none included.
func matchAnyDepth(segs []segment, names []string, dir bool) bool {
	for skip := 0; skip <= len(names); skip++ {
		if matchFrom(segs, names[skip:], dir) {
			return true
		}
	}

	return false
}

// match reports whether a segment matches one name.
func (s segment) match(name string) bool {
	if s.literal {
		return s.text == name
	}

	ok, _ := path.Match(s.text, name) // the pattern was checked when compiled

	return ok
}

// splitPath splits a repository path into its lowercased names, dropping empty ones.
func splitPath(p string) []string {
	parts := strings.Split(strings.ToLower(p), "/")
	out := parts[:0]

	for _, part := range parts {
		if part != "" && part != "." {
			out = append(out, part)
		}
	}

	return out
}

// shorten cuts a pattern for an error message to its first 60 characters and an
// ellipsis.
func shorten(p string) string {
	const limit = 60

	return textutil.Truncate(p, limit+1)
}
