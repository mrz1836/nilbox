package textutil

import (
	"strconv"
	"strings"
)

// Counted writes n with its noun: one when n is 1, else many, as in "1 file", "0 files",
// and "3 files".
func Counted(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}

	return strconv.Itoa(n) + " " + many
}

// Series joins names as a sentence lists them, with a comma before the last one's "and"
// once there are three: "a", "a and b", "a, b, and c", and "" for none.
func Series(names []string) string {
	if len(names) < 3 {
		return strings.Join(names, " and ")
	}

	return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
}
