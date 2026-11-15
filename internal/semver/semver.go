// Package semver reads semantic versions and compares them by semantic versioning's
// precedence: MAJOR.MINOR.PATCH, with an optional prerelease (-rc.1) and build metadata
// (+build.5), with or without a leading v. The integrations compare the tools' versions
// with it, and the app's updates compare a release's version with the running build's.
//
// It is a leaf: it imports nothing of the app's.
package semver

import (
	"strconv"
	"strings"
)

// Version is a parsed MAJOR.MINOR.PATCH[-PRE][+BUILD] version. Build metadata is dropped,
// since it never changes precedence.
type Version struct {
	core [3]int
	pre  []string
}

// Prerelease reports whether v has a prerelease part, as 0.3.0-rc.1 has.
func (v Version) Prerelease() bool {
	return len(v.pre) > 0
}

// Parse reads a version, with or without a leading v; ok is false when it isn't one. It
// holds to semantic versioning 2.0: each prerelease and build identifier is a non-empty
// run of ASCII letters, digits, and hyphens, and a numeric prerelease identifier has no
// leading zero (rc.1, never rc.01).
func Parse(raw string) (Version, bool) {
	raw = strings.TrimPrefix(raw, "v")

	raw, build, hasBuild := strings.Cut(raw, "+")
	if hasBuild && !identifiers(build, false) {
		return Version{}, false
	}

	core, pre, hasPre := strings.Cut(raw, "-")

	out, ok := parseCore(core)
	if !ok {
		return Version{}, false
	}

	if hasPre {
		if !identifiers(pre, true) {
			return Version{}, false
		}

		out.pre = strings.Split(pre, ".")
	}

	return out, true
}

// parseCore reads MAJOR.MINOR.PATCH, each a number with no leading zero.
func parseCore(core string) (Version, bool) {
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, false
	}

	var out Version

	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || !numeric(part) {
			return Version{}, false
		}

		out.core[i] = n
	}

	return out, true
}

// identifiers reports whether s is dot-separated identifiers, each a non-empty run of
// ASCII letters, digits, and hyphens; for a prerelease, a numeric one has no leading zero.
func identifiers(s string, prerelease bool) bool {
	for ident := range strings.SplitSeq(s, ".") {
		if ident == "" || strings.IndexFunc(ident, notIdentRune) >= 0 {
			return false
		}

		if prerelease && allDigits(ident) && !numeric(ident) {
			return false
		}
	}

	return true
}

// notIdentRune reports a rune an identifier can't hold.
func notIdentRune(r rune) bool {
	return (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && r != '-'
}

// numeric reports whether s is a number as a version writes one: digits, with no leading
// zero but for 0 itself.
func numeric(s string) bool {
	return allDigits(s) && (len(s) == 1 || s[0] != '0')
}

// allDigits reports whether s is one or more ASCII digits.
func allDigits(s string) bool {
	return s != "" && strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) < 0
}

// Compare compares two versions by semantic versioning's precedence: -1, 0, or 1, and
// false when either isn't a version.
func Compare(a, b string) (int, bool) {
	va, okA := Parse(a)
	vb, okB := Parse(b)

	if !okA || !okB {
		return 0, false
	}

	for i := range va.core {
		if c := cmpInt(va.core[i], vb.core[i]); c != 0 {
			return c, true
		}
	}

	return comparePre(va.pre, vb.pre), true
}

// comparePre compares prerelease identifiers: a release sorts after any prerelease, and
// identifiers compare one by one, numbers numerically and below words.
func comparePre(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}

	for i := 0; i < len(a) && i < len(b); i++ {
		if c := compareIdent(a[i], b[i]); c != 0 {
			return c
		}
	}

	return cmpInt(len(a), len(b))
}

// compareIdent compares two prerelease identifiers: numbers numerically, and below words.
func compareIdent(a, b string) int {
	na, errA := strconv.Atoi(a)
	nb, errB := strconv.Atoi(b)

	switch {
	case errA == nil && errB == nil:
		return cmpInt(na, nb)
	case errA == nil:
		return -1
	case errB == nil:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

// cmpInt compares two ints.
func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
