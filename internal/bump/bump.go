// Package bump recognizes a dependency-update pull request from its title, as Dependabot
// and Renovate write them, and says how big its version jump is.
//
// It reads only the title (and, through FromCommit, Dependabot's commit trailer), so it
// needs no request of its own. It is conservative: a title it cannot read with
// confidence is not a dependency update, since calling an ordinary pull request one is
// worse than missing one.
package bump

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Level is how big a version change is.
type Level string

const (
	// LevelUnknown is a change that couldn't be judged.
	LevelUnknown Level = ""

	// LevelPatch is a change in the third or a later version part, or only in a suffix.
	LevelPatch Level = "patch"

	// LevelMinor is a change in the second version part.
	LevelMinor Level = "minor"

	// LevelMajor is a change in the first version part.
	LevelMajor Level = "major"
)

// Update is what a dependency-update title says.
type Update struct {
	// Name is the dependency, such as "golang.org/x/net": "" for a group, for two or more
	// dependencies updated together, and for Renovate's updates of all dependencies.
	Name string `json:"name"`

	// From is the version the dependency moves from, as written, or "" when the title
	// doesn't say, as Renovate's don't.
	From string `json:"from"`

	// To is the version the dependency moves to, as written ("v4" or "4", as the title
	// has it).
	To string `json:"to"`

	// Group is a Dependabot group's name, or "" for an update outside a group.
	Group string `json:"group"`

	// Dir is the directory the update is in, such as "/frontend", or "" for the root and
	// when the title doesn't say.
	Dir string `json:"dir"`

	// Count is how many dependencies the pull request updates: 1 for a single one, N for
	// a group "with N updates", and 0 when the title doesn't say, as for Renovate's
	// updates of all dependencies and of a monorepo's packages.
	Count int `json:"count"`

	// Level is how big the version jump is, LevelUnknown when the title can't tell.
	Level Level `json:"level"`

	// Security is a security update, marked [Security] or [SECURITY] in the title.
	Security bool `json:"security"`
}

// Single reports an update of one named dependency whose title gives both versions.
func (u Update) Single() bool {
	return u.Count == 1 && u.Name != "" && u.From != "" && u.To != ""
}

// whereSuffix matches what Dependabot appends to a single update's title: the directory
// (" in /frontend"), then the group (" in the go-deps group"), which may say how many
// directories the group spans (" across 2 directories").
const whereSuffix = `(?:\s+in\s+(/\S*))?(?:\s+in\s+the\s+(.+?)\s+group(?:\s+across\s+[0-9]+\s+director(?:y|ies))?)?`

// countPattern is a count of updates or directories, kept short enough to fit an int.
const countPattern = `([0-9]{1,9})`

var (
	// securityStart matches the [Security] marker Dependabot puts before a title's words.
	securityStart = regexp.MustCompile(`(?i)^\[security\]\s*`)

	// securityEnd matches the [SECURITY] marker Renovate puts after a title's words.
	securityEnd = regexp.MustCompile(`(?i)\s*\[security\]$`)

	// commitPrefix matches a conventional-commit prefix such as "build(deps): ",
	// "chore(deps-dev)!: ", or "deps: ".
	commitPrefix = regexp.MustCompile(`(?i)^[a-z][a-z0-9_-]*(?:\([^()]*\))?!?:\s*`)

	// levelEnd matches a level in parentheses at a title's end, such as " (major)".
	levelEnd = regexp.MustCompile(`(?i)\s*\((major|minor|patch)\)$`)

	// bumpPattern matches Dependabot's single update: "bump X from A to B", then where.
	bumpPattern = regexp.MustCompile(`(?i)^bump\s+(\S+)\s+from\s+(\S+)\s+to\s+(\S+)` + whereSuffix + `$`)

	// requirementPattern matches Dependabot's requirement update, "update X requirement
	// from ~> 6.0 to ~> 7.0", then where. A requirement may hold spaces.
	requirementPattern = regexp.MustCompile(`(?i)^update\s+(\S+)\s+requirement\s+from\s+(.+?)\s+to\s+(.+?)` +
		whereSuffix + `$`)

	// groupPattern matches Dependabot's group update, "bump the G group with N updates",
	// which may name a directory before "with".
	groupPattern = regexp.MustCompile(`(?i)^bump\s+the\s+(.+?)\s+group(?:\s+in\s+(/\S*))?\s+with\s+` +
		countPattern + `\s+updates?$`)

	// acrossPattern matches Dependabot's group update across directories, "bump the G
	// group across N directories with M updates".
	acrossPattern = regexp.MustCompile(`(?i)^bump\s+the\s+(.+?)\s+group\s+across\s+[0-9]+\s+director(?:y|ies)` +
		`\s+with\s+` + countPattern + `\s+updates?$`)

	// listPattern matches Dependabot's update of several dependencies together, "bump X
	// and Y" or "bump X, Y and Z", which may name a directory.
	listPattern = regexp.MustCompile(`(?i)^bump\s+([^\s,]+(?:,\s*[^\s,]+)*)\s+and\s+([^\s,]+)(?:\s+in\s+(/\S*))?$`)

	// renovateDependency matches Renovate's "update dependency X to V" and "update module
	// X to V".
	renovateDependency = regexp.MustCompile(`(?i)^update\s+(?:dependency|module)\s+(\S+)\s+to\s+(.+)$`)

	// renovateTopic matches Renovate's titles that name what kind of dependency X is:
	// "update X action to V", "update X docker tag to V", "update X docker digest to D",
	// "update X digest to D", and "update X monorepo to V".
	renovateTopic = regexp.MustCompile(`(?i)^update\s+(\S+)\s+(action|docker\s+tag|docker\s+digest|digest|monorepo)` +
		`\s+to\s+(.+)$`)

	// renovateAll matches Renovate's updates of every dependency, "update all
	// dependencies" and "update all non-major dependencies".
	renovateAll = regexp.MustCompile(`(?i)^update\s+all\s+(?:non-major\s+)?dependencies$`)
)

// Parse reads a pull request's title as a dependency update, and reports false for a
// title that is not one.
//
// It reads Dependabot's titles ("bump X from A to B", in a directory or a group, "update
// X requirement from A to B", "bump the G group with N updates", "bump X and Y") and
// Renovate's ("update dependency X to V", "update module X to V", "update X action to
// V", "update X docker tag to V", "update X digest to D", "update X monorepo to V",
// "update all dependencies"), after a conventional-commit prefix such as
// "build(deps): ", either tool's security marker, and a level in parentheses such as
// " (major)", which sets the level when present.
func Parse(title string) (Update, bool) {
	words, security, label := unwrap(title)

	parsers := []func(string) (Update, bool){
		parseGroup, parseAcross, parseBump, parseRequirement, parseList,
		parseRenovateDependency, parseRenovateTopic, parseRenovateAll,
	}

	for _, parse := range parsers {
		u, ok := parse(words)
		if !ok {
			continue
		}

		u.Security = security
		if label != LevelUnknown {
			u.Level = label
		}

		return u, true
	}

	return Update{}, false
}

// unwrap strips what wraps a title's own words: a conventional-commit prefix, a security
// marker at either end, and a level in parentheses at the end. It returns the words,
// whether a security marker was there, and the level the parentheses named.
func unwrap(title string) (string, bool, Level) {
	s := strings.TrimSpace(title)
	security := false

	s, security = cut(securityStart, s, security)
	s = strings.TrimSpace(commitPrefix.ReplaceAllLiteralString(s, ""))
	s, security = cut(securityStart, s, security)

	label := LevelUnknown

	// The level and the security marker may come in either order, so each is tried twice.
	for range 2 {
		s, security = cut(securityEnd, s, security)

		if m := levelEnd.FindStringSubmatch(s); m != nil {
			label = Level(strings.ToLower(m[1]))
			s = strings.TrimSpace(s[:len(s)-len(m[0])])
		}
	}

	return s, security, label
}

// cut removes what pattern matches from s, trimmed, and reports whether it matched or
// found was already true.
func cut(pattern *regexp.Regexp, s string, found bool) (string, bool) {
	loc := pattern.FindStringIndex(s)
	if loc == nil {
		return s, found
	}

	return strings.TrimSpace(s[:loc[0]] + s[loc[1]:]), true
}

// parseBump reads Dependabot's "bump X from A to B", in a directory or a group.
func parseBump(words string) (Update, bool) {
	return parseSingle(bumpPattern, words)
}

// parseRequirement reads Dependabot's "update X requirement from A to B", in a directory
// or a group.
func parseRequirement(words string) (Update, bool) {
	return parseSingle(requirementPattern, words)
}

// parseSingle reads one dependency's update with both versions, as pattern captures it:
// the name, the two versions, the directory, and the group.
func parseSingle(pattern *regexp.Regexp, words string) (Update, bool) {
	m := pattern.FindStringSubmatch(words)
	if m == nil {
		return Update{}, false
	}

	n, okName := name(m[1])
	from, okFrom := version(m[2])
	to, okTo := version(m[3])

	if !okName || !okFrom || !okTo {
		return Update{}, false
	}

	return Update{
		Name: n, From: from, To: to, Dir: dir(m[4]), Group: strings.TrimSpace(m[5]),
		Count: 1, Level: Compare(from, to),
	}, true
}

// parseGroup reads Dependabot's "bump the G group with N updates", which may name a
// directory.
func parseGroup(words string) (Update, bool) {
	m := groupPattern.FindStringSubmatch(words)
	if m == nil {
		return Update{}, false
	}

	return Update{Group: strings.TrimSpace(m[1]), Dir: dir(m[2]), Count: atoi(m[3])}, true
}

// parseAcross reads Dependabot's "bump the G group across N directories with M updates".
func parseAcross(words string) (Update, bool) {
	m := acrossPattern.FindStringSubmatch(words)
	if m == nil {
		return Update{}, false
	}

	return Update{Group: strings.TrimSpace(m[1]), Count: atoi(m[2])}, true
}

// parseList reads Dependabot's "bump X and Y" and "bump X, Y and Z", which may name a
// directory. Every name must read as a dependency.
func parseList(words string) (Update, bool) {
	m := listPattern.FindStringSubmatch(words)
	if m == nil {
		return Update{}, false
	}

	names := append(strings.Split(m[1], ","), m[2])
	for _, raw := range names {
		if _, ok := name(raw); !ok {
			return Update{}, false
		}
	}

	return Update{Dir: dir(m[3]), Count: len(names)}, true
}

// parseRenovateDependency reads Renovate's "update dependency X to V" and "update module
// X to V".
func parseRenovateDependency(words string) (Update, bool) {
	m := renovateDependency.FindStringSubmatch(words)
	if m == nil {
		return Update{}, false
	}

	return renovateUpdate(m[1], m[2], 1, false)
}

// parseRenovateTopic reads Renovate's titles that name the kind of dependency: an action,
// a Docker tag, a digest, or a monorepo, whose packages are not counted.
func parseRenovateTopic(words string) (Update, bool) {
	m := renovateTopic.FindStringSubmatch(words)
	if m == nil {
		return Update{}, false
	}

	topic := strings.ToLower(m[2])

	count := 1
	if topic == "monorepo" {
		count = 0
	}

	return renovateUpdate(m[1], m[3], count, strings.HasSuffix(topic, "digest"))
}

// renovateUpdate builds a Renovate update of the dependency rawName to rawTo. A digest is
// any one word and has no level; any other target must read as a version, and is a major
// update when it is a bare major version ("v9" or "9"), which Renovate writes only for
// one.
func renovateUpdate(rawName, rawTo string, count int, digest bool) (Update, bool) {
	n, ok := name(rawName)
	if !ok {
		return Update{}, false
	}

	to := strings.TrimSpace(rawTo)
	if digest {
		ok = to != "" && !strings.ContainsFunc(to, unicode.IsSpace)
	} else {
		to, ok = version(to)
	}

	if !ok {
		return Update{}, false
	}

	u := Update{Name: n, To: to, Count: count}
	if !digest && bareMajor(to) {
		u.Level = LevelMajor
	}

	return u, true
}

// parseRenovateAll reads Renovate's "update all dependencies" and "update all non-major
// dependencies", which name no dependency and no count.
func parseRenovateAll(words string) (Update, bool) {
	return Update{}, renovateAll.MatchString(words)
}

// name returns raw trimmed and without surrounding backticks, and whether it reads as a
// dependency's name: one word, and not "version", which a release's own title bumps.
func name(raw string) (string, bool) {
	n := strings.TrimSpace(unquote(strings.TrimSpace(raw)))
	if n == "" || strings.ContainsFunc(n, unicode.IsSpace) {
		return n, false
	}

	lower := strings.ToLower(n)

	return n, lower != "version" && lower != "versions"
}

// version returns raw trimmed and without the backticks Dependabot writes around a commit
// SHA, and whether it reads as a version: a word in backticks, a word holding a digit,
// or a requirement such as "~> 6.0" that starts with an operator and holds a digit.
func version(raw string) (string, bool) {
	v := strings.TrimSpace(raw)
	if inner := unquote(v); inner != v {
		inner = strings.TrimSpace(inner)

		return inner, inner != "" && !strings.ContainsFunc(inner, unicode.IsSpace)
	}

	if !strings.ContainsFunc(v, isDigit) {
		return v, false
	}

	return v, !strings.ContainsFunc(v, unicode.IsSpace) || strings.ContainsAny(v[:1], "~^<>=!")
}

// unquote returns s without one pair of surrounding backticks, or s as it is when it has
// none.
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '`' && s[len(s)-1] == '`' {
		return s[1 : len(s)-1]
	}

	return s
}

// dir returns a directory as written, trimmed, or "" for the root.
func dir(raw string) string {
	d := strings.TrimSpace(raw)
	if d == "/" {
		return ""
	}

	return d
}

// atoi reads a count the patterns already limited to nine digits, so it always fits.
func atoi(digits string) int {
	n, _ := strconv.Atoi(digits)

	return n
}

// bareMajor reports a target that is only a major version, "v9" or "9".
func bareMajor(to string) bool {
	digits := strings.TrimPrefix(strings.TrimPrefix(to, "v"), "V")

	return digits != "" && !strings.ContainsFunc(digits, func(r rune) bool { return !isDigit(r) })
}

// isDigit reports an ASCII digit.
func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}
