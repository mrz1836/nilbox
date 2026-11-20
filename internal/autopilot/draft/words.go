package draft

import (
	"strings"

	"github.com/mrz1836/nilbox/internal/uierr"
)

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

// Message returns what a refusal from one of the draft's checks says to the user: the
// sentence CheckName, CheckDescription, CheckTemplate, or Read words it with, as the window
// shows it (uierr.Message). A caller that words a draft's problems for someone other than
// the window, as a check of a proposed routine does for the agent that proposed it, reads
// them here.
func Message(err error) string {
	return uierr.Message(err)
}
