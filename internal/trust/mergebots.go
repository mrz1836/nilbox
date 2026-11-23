package trust

import (
	"errors"
	"slices"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
)

// PrefMergeBotsOn is whether a pull request's card says when one of the user's auto-merge
// bots approved it, as prefval writes a switch: off until the user turns it on.
// PrefMergeBots lists those bots, as a JSON array of logins: none until the user adds one,
// and no build ships a name. The settings package writes both.
const (
	PrefMergeBotsOn = "trust.mergeBotsOn"
	PrefMergeBots   = "trust.mergeBots"
)

// ErrTooManyMergeBots is returned for an auto-merge list over 100 bots.
var ErrTooManyMergeBots = errors.New("too many auto-merge bots")

// MergeBots are the user's auto-merge bots: the bots (GitHub Apps) that approve a pull
// request and merge it once its checks pass, such as "acme-org-automerge", and whether a
// card says so.
type MergeBots struct {
	// On is whether a card says that a listed bot approved its pull request.
	On bool

	// Bots are the listed logins, normalized and sorted (NormalizeMergeBots); never nil.
	Bots []string
}

// DecodeMergeBots reads the auto-merge bots from the stored prefs: off when PrefMergeBotsOn
// is unset or unreadable, and none when PrefMergeBots is.
func DecodeMergeBots(stored map[string]string) MergeBots {
	bots, _ := prefval.List[string](stored[PrefMergeBots])
	if bots == nil {
		bots = []string{}
	}

	return MergeBots{On: prefval.Bool(stored[PrefMergeBotsOn], false), Bots: bots}
}

// NormalizeMergeBots cleans an auto-merge list as the user entered it, as NormalizeBots
// cleans a trusted list: each login trimmed and lowercased, an "app/" prefix or "[bot]"
// suffix dropped, empty entries and duplicates removed, and the list sorted. A login GitHub
// could not have issued is an ErrInvalidBot, and a list over 100 bots an
// ErrTooManyMergeBots.
func NormalizeMergeBots(logins []string) ([]string, error) {
	return normalizeBots(logins, ErrTooManyMergeBots)
}

// Match returns the first listed bot, in the list's order, among approving, the bots whose
// latest review approves a pull request (by login, as GitHub gives an app's slug), matched
// ignoring case and any "[bot]" suffix. It returns "" while the setting is off, and when
// no listed bot approved.
func (m MergeBots) Match(approving []string) string {
	if !m.On || len(approving) == 0 {
		return ""
	}

	folded := make([]string, 0, len(approving))
	for _, a := range approving {
		folded = append(folded, foldBot(a))
	}

	for _, b := range m.Bots {
		if bot := foldBot(b); bot != "" && slices.Contains(folded, bot) {
			return bot
		}
	}

	return ""
}
