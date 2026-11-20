package draft

import "strings"

// Series joins words as a sentence lists them: "a", "a and b", "a, b, and c"; no words
// are "its steps".
func Series(words []string) string {
	switch len(words) {
	case 0:
		return "its steps"
	case 1:
		return words[0]
	case 2:
		return words[0] + " and " + words[1]
	default:
		return strings.Join(words[:len(words)-1], ", ") + ", and " + words[len(words)-1]
	}
}
