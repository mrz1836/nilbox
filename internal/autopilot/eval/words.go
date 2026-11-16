package eval

import (
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// globMatch reports whether s matches pattern, where * is any run of characters and ? is
// one. It walks both once, going back only to the last *, so a pattern can't run away.
func globMatch(pattern, s string) bool {
	p, t := []rune(pattern), []rune(s)
	pi, ti := 0, 0
	star, mark := -1, 0

	for ti < len(t) {
		switch {
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, ti
			pi++
		case pi < len(p) && (p[pi] == '?' || p[pi] == t[ti]):
			pi++
			ti++
		case star >= 0:
			mark++
			pi, ti = star+1, mark
		default:
			return false
		}
	}

	for pi < len(p) && p[pi] == '*' {
		pi++
	}

	return pi == len(p)
}

// actualWords says a known value, for a condition's reason.
func actualWords(field catalog.Field, v facts.Value, now time.Time) string {
	switch field.Type {
	case catalog.TypeText:
		return strconv.Quote(v.Text)
	case catalog.TypeList:
		return listWords(field, v)
	case catalog.TypeNumber:
		return strconv.Itoa(v.Number)
	case catalog.TypeBool:
		if v.Bool {
			return "yes"
		}

		return "no"
	case catalog.TypeChoice:
		if c, ok := field.Choice(v.Text); ok {
			return c.Label
		}

		return strconv.Quote(v.Text)
	case catalog.TypeDuration:
		return elapsedWords(now.Sub(v.Since))
	}

	return ""
}

// listWords says a list: its values, or "none", and whether there may be more.
func listWords(field catalog.Field, v facts.Value) string {
	if len(v.List) == 0 {
		if v.Partial {
			return "none read so far"
		}

		return "none"
	}

	words := make([]string, 0, len(v.List))

	for _, e := range v.List {
		if c, ok := field.Choice(e); ok {
			words = append(words, c.Label)

			continue
		}

		words = append(words, strconv.Quote(e))
	}

	out := strings.Join(words, ", ")
	if v.Partial {
		out += ", and maybe more"
	}

	return out
}

// elapsedWords says how long ago something happened: "3 days", "5 hours", "12 minutes".
func elapsedWords(d time.Duration) string {
	const day = 24 * time.Hour

	switch {
	case d >= 2*day:
		return textutil.Counted(int(d/day), "day", "days")
	case d >= 2*time.Hour:
		return textutil.Counted(int(d/time.Hour), "hour", "hours")
	case d < 0:
		return "in the future"
	default:
		return textutil.Counted(int(d/time.Minute), "minute", "minutes")
	}
}
