package bump

import "strings"

// Compare says how big the change from one version to another is: major, minor, or patch
// by the first numeric part that differs, patch when only a suffix or a further part
// does, and LevelUnknown for the same version or for one that has no numeric part, such
// as a digest or "latest". A downgrade reports the level of the part that changed.
//
// Each side may carry a range operator ("^", "~", "~>", ">=", "=") and a leading "v".
// Only the leading run of dot-separated numeric parts is compared: "3-beta" counts as 3
// and ends the run.
func Compare(from, to string) Level {
	a, b := bare(from), bare(to)
	if a == b {
		return LevelUnknown
	}

	if digest(a) || digest(b) {
		return LevelUnknown
	}

	pa, pb := numericParts(a), numericParts(b)
	if len(pa) == 0 || len(pb) == 0 {
		return LevelUnknown
	}

	for i := range min(len(pa), len(pb)) {
		if pa[i] != pb[i] {
			return levelAt(i)
		}
	}

	return LevelPatch
}

// bare returns a version without surrounding space, one leading range operator, and a
// leading "v" or "V".
func bare(v string) string {
	v = strings.TrimSpace(v)

	// The two-character operators come first, so "~>" is not read as "~".
	for _, op := range []string{"~>", ">=", "^", "~", "="} {
		if rest, ok := strings.CutPrefix(v, op); ok {
			v = strings.TrimSpace(rest)

			break
		}
	}

	if v != "" && (v[0] == 'v' || v[0] == 'V') {
		return v[1:]
	}

	return v
}

// minDigestLen is the shortest digest a title shows, a commit's short SHA.
const minDigestLen = 7

// digest reports a commit SHA or an image digest, such as "1a2b3c4": hexadecimal, at
// least seven characters, and holding a letter, which no numeric version does.
func digest(v string) bool {
	if len(v) < minDigestLen {
		return false
	}

	letter := false

	for _, r := range v {
		switch {
		case isDigit(r):
		case (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F'):
			letter = true
		default:
			return false
		}
	}

	return letter
}

// numericParts returns the leading run of a version's dot-separated numeric parts,
// without leading zeros so "01" equals "1". A part that starts with digits and goes on
// ("3-beta") gives its digits and ends the run; one that starts otherwise ends it.
func numericParts(v string) []string {
	var parts []string

	for part := range strings.SplitSeq(v, ".") {
		digits := part[:len(part)-len(strings.TrimLeftFunc(part, isDigit))]
		if digits == "" {
			break
		}

		parts = append(parts, strings.TrimLeft(digits, "0"))

		if digits != part {
			break
		}
	}

	return parts
}

// levelAt is the level of a change in the numeric part at index i.
func levelAt(i int) Level {
	switch i {
	case 0:
		return LevelMajor
	case 1:
		return LevelMinor
	default:
		return LevelPatch
	}
}
