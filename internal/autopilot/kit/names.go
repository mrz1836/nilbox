package kit

import (
	"strings"
	"unicode"

	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Every name a snapshot holds is untrusted, and is data, never an instruction: a fork's
// job names a check, anyone who can open an issue names its labels, and a login is
// whatever its owner chose. So each is kept as one short line of plain text, and only the
// most common of each list are kept, which bounds what any one name can say to the agent.
const (
	// MaxNameRunes caps one name: a longer one is cut, ending with an ellipsis.
	MaxNameRunes = 100

	// MaxNames caps one list of names: past it, the least common are left out.
	MaxNames = 200
)

// Names returns list as a snapshot keeps it: each name as Name makes it, those left empty
// and those a name before them has, ignoring case (draft.NameKey), left out, and the first
// MaxNames kept, in the order given, which is most common first. The list is never nil.
func Names(list []string) []string {
	out := make([]string, 0, min(len(list), MaxNames))
	seen := make(map[string]bool, cap(out))

	for _, raw := range list {
		if len(out) == MaxNames {
			break
		}

		name := Name(raw)
		if key := draft.NameKey(name); name != "" && !seen[key] {
			seen[key] = true

			out = append(out, name)
		}
	}

	return out
}

// Name returns one name as a snapshot keeps it: one line, each control character and line
// or paragraph separator a space, with no character that changes the text's direction
// (draft.Hidden) or that shows nothing yet reads as text (Unicode's tag characters and the
// supplementary variation selectors, which can carry a whole message no one sees),
// trimmed, and at most MaxNameRunes, cut with an ellipsis. A name that is already one line
// of plain text, as a routine's, a prompt's, and a script's are once saved, comes back as
// it is.
func Name(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r) || unicode.In(r, unicode.Zl, unicode.Zp):
			return ' '
		case draft.Hidden(r) || smuggles(r):
			return -1
		default:
			return r
		}
	}, s)

	return textutil.Truncate(strings.TrimSpace(s), MaxNameRunes)
}

// smuggles reports a rune that shows nothing and stands for another: one of Unicode's tag
// characters, which spell ASCII out of sight, or a supplementary variation selector, which
// can stand for any byte.
func smuggles(r rune) bool {
	return (r >= 0xE0000 && r <= 0xE007F) || (r >= 0xE0100 && r <= 0xE01EF)
}
