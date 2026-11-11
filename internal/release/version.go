package release

import (
	"fmt"
	"regexp"
	"strconv"
)

// Bump is which part of the version a release raises.
type Bump string

// The bumps a release offers.
const (
	// BumpPatch raises the patch version: v1.4.2 → v1.4.3.
	BumpPatch Bump = "patch"

	// BumpMinor raises the minor version: v1.4.2 → v1.5.0.
	BumpMinor Bump = "minor"

	// BumpMajor raises the major version: v1.4.2 → v2.0.0.
	BumpMajor Bump = "major"
)

// Valid reports whether b is one of the bumps.
func (b Bump) Valid() bool {
	return b == BumpPatch || b == BumpMinor || b == BumpMajor
}

// versionTag is a tag magex reads as a version: v1.2.3 or 1.2.3, with an optional
// prerelease and build. Two-part (v1.5), module-prefixed (sdk/v1.9.0), and other tags
// aren't versions to it.
var versionTag = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

// Version is a release version, vMajor.Minor.Patch. A prerelease tag reads as the
// release it leads to, as magex reads it: v1.5.0-rc.1 is v1.5.0 here.
type Version struct {
	Major, Minor, Patch int
}

// String returns the version as a tag: v1.4.3.
func (v Version) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Less reports whether v comes before w, by number: v1.4.2 comes before v1.4.10.
func (v Version) Less(w Version) bool {
	if v.Major != w.Major {
		return v.Major < w.Major
	}

	if v.Minor != w.Minor {
		return v.Minor < w.Minor
	}

	return v.Patch < w.Patch
}

// Bumped returns v raised by b.
func (v Version) Bumped(b Bump) Version {
	switch b {
	case BumpMajor:
		return Version{Major: v.Major + 1}
	case BumpMinor:
		return Version{Major: v.Major, Minor: v.Minor + 1}
	case BumpPatch:
		return Version{Major: v.Major, Minor: v.Minor, Patch: v.Patch + 1}
	default:
		return v
	}
}

// ParseVersion reads a tag as magex does, and reports whether it is a version at all.
func ParseVersion(tag string) (Version, bool) {
	m := versionTag.FindStringSubmatch(tag)
	if m == nil {
		return Version{}, false
	}

	var parts [3]int

	for i := range parts {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return Version{}, false // too large to be a version anyone tagged
		}

		parts[i] = n
	}

	return Version{Major: parts[0], Minor: parts[1], Patch: parts[2]}, true
}

// Highest returns the highest version among tags, by number, and whether there was one.
// Every version tag counts, on any commit, reachable or not, as magex counts them.
func Highest(tags []string) (Version, bool) {
	var (
		best  Version
		found bool
	)

	for _, tag := range tags {
		v, ok := ParseVersion(tag)
		if ok && (!found || best.Less(v)) {
			best, found = v, true
		}
	}

	return best, found
}

// Next returns the version magex makes for b over tags: the highest version raised by b,
// or b over v0.0.0 when there is none. It matches magex's own choice, recorded from magex
// 1.29.5 and 1.30.2 in testdata/magex-*/next-version.json. A raised highest version is
// above every tag, so no tag ever names it already: magex's skip of a taken version never
// comes into it.
func Next(tags []string, b Bump) Version {
	highest, _ := Highest(tags)

	return highest.Bumped(b)
}
