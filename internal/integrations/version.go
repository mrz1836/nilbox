package integrations

import (
	"bufio"
	"bytes"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// ErrNoVersion is returned when a tool's output holds no version this package can read.
var ErrNoVersion = errors.New("no version in the output")

// Version is a tool's version as it reported it.
type Version struct {
	// Raw is the version as printed, without a leading v: "1.29.5", "dev", or a Go
	// pseudo-version such as "1.29.6-0.20261105041738-9a61f25f00aa+dirty".
	Raw string `json:"raw"`

	// Commit is the commit it was built from, as printed, or "".
	Commit string `json:"commit"`

	// Built is when it was built, as printed, or "".
	Built string `json:"built"`
}

// Development reports a build from a working copy rather than a release: "dev", a Go
// pseudo-version, or a version marked dirty. Nothing compares it with a release.
func (v Version) Development() bool {
	raw := v.Raw

	return raw == "dev" || raw == "(devel)" || pseudoVersion.MatchString(raw) ||
		strings.HasSuffix(raw, "+dirty") || strings.HasSuffix(raw, "-dirty")
}

// AtLeast reports whether v is min or newer. A development build, or a version that
// isn't one, is never at least anything.
func (v Version) AtLeast(minimum string) bool {
	if v.Development() {
		return false
	}

	c, ok := compareVersions(v.Raw, minimum)

	return ok && c >= 0
}

// OlderThan reports whether v is older than other. A development build, or a version
// that isn't one, is never older.
func (v Version) OlderThan(other string) bool {
	if v.Development() {
		return false
	}

	c, ok := compareVersions(v.Raw, other)

	return ok && c < 0
}

// pseudoVersion matches the timestamp and commit of a Go pseudo-version.
var pseudoVersion = regexp.MustCompile(`(^|[.-])\d{14}-[0-9a-f]{12}($|[+])`)

// semver is a parsed MAJOR.MINOR.PATCH[-PRE][+BUILD] version.
type semver struct {
	core [3]int
	pre  []string
}

// parseSemver reads a version, with or without a leading v; ok is false when it isn't one.
func parseSemver(raw string) (semver, bool) {
	raw = strings.TrimPrefix(raw, "v")
	raw, _, _ = strings.Cut(raw, "+")
	core, pre, hasPre := strings.Cut(raw, "-")

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}

	var out semver

	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || part == "" || (len(part) > 1 && part[0] == '0') {
			return semver{}, false
		}

		out.core[i] = n
	}

	if hasPre {
		if pre == "" {
			return semver{}, false
		}

		out.pre = strings.Split(pre, ".")
	}

	return out, true
}

// compareVersions compares two versions by semantic versioning's precedence: -1, 0, or 1,
// and false when either isn't a version.
func compareVersions(a, b string) (int, bool) {
	va, okA := parseSemver(a)
	vb, okB := parseSemver(b)

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

// ansiEscape matches a terminal escape sequence: a CSI sequence such as a color, or an
// OSC sequence such as a link.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)`)

// stripANSI removes terminal escape sequences.
func stripANSI(out []byte) []byte {
	return ansiEscape.ReplaceAll(out, nil)
}

// cleanVersion drops a leading v and surrounding space from a printed version.
func cleanVersion(raw string) string {
	return strings.TrimPrefix(strings.TrimSpace(raw), "v")
}

// ParseMagexVersion reads `magex --version`: a banner, then a "Version Information" block
// whose lines are "  Version:      1.29.5", "  Commit:       9a61f25", and
// "  Build Date:   2026-11-05T04:17:38Z".
func ParseMagexVersion(out []byte) (Version, error) {
	var v Version

	fields := map[string]*string{"Version": &v.Raw, "Commit": &v.Commit, "Build Date": &v.Built}

	sc := bufio.NewScanner(bytes.NewReader(stripANSI(out)))
	sc.Buffer(make([]byte, 0, 4096), 1<<20)

	for sc.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(sc.Text()), ":")
		if field := fields[strings.TrimSpace(key)]; ok && field != nil && *field == "" {
			*field = strings.TrimSpace(value)
		}
	}

	v.Raw = cleanVersion(v.Raw)

	if v.Raw == "" || strings.ContainsAny(v.Raw, " \t") {
		return Version{}, ErrNoVersion
	}

	return v, nil
}

// gopcVersion matches `go-pre-commit --version`'s line.
var gopcVersion = regexp.MustCompile(`go-pre-commit version (\S+)(?: \(commit: ([^,)]*), built: ([^)]*)\))?`)

// ParseGoPreCommitVersion reads `go-pre-commit --version`: "go-pre-commit version 1.13.1
// (commit: <sha>, built: <date>)". A go install build prints v1.13.1 and "built:
// go-install".
func ParseGoPreCommitVersion(out []byte) (Version, error) {
	m := gopcVersion.FindSubmatch(stripANSI(out))
	if m == nil || cleanVersion(string(m[1])) == "" {
		return Version{}, ErrNoVersion
	}

	return Version{
		Raw: cleanVersion(string(m[1])), Commit: strings.TrimSpace(string(m[2])), Built: strings.TrimSpace(string(m[3])),
	}, nil
}
