package launcher

import (
	"errors"
	"fmt"

	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Situation is the situation a pull request is in that a launch is made for, as the
// inbox's card names it: its prompt is then the launch defaults' prompt for that situation,
// the repository's, else its org's, else the app's, and with none of them the pull
// request's own. Each has the name of its prompts.DefaultSlot.
type Situation string

// The situations a pull request can be in.
const (
	// SituationNone is a launch for no situation: the item's own prompt.
	SituationNone Situation = ""

	// SituationChecksFailing is a pull request whose checks fail.
	SituationChecksFailing Situation = "checks_failing"

	// SituationConflicts is a pull request that conflicts with its base.
	SituationConflicts Situation = "conflicts"

	// SituationChangesRequested is a pull request a reviewer asked to change.
	SituationChangesRequested Situation = "changes_requested"

	// SituationRereview is a pull request the user is asked to review again.
	SituationRereview Situation = "rereview"
)

// ErrUnknownSituation is returned for a launch request whose situation this build doesn't
// know.
var ErrUnknownSituation = errors.New("unknown situation")

// situations returns every situation but none, in the order the settings list them.
func situations() []Situation {
	return []Situation{SituationChecksFailing, SituationConflicts, SituationChangesRequested, SituationRereview}
}

// check refuses a situation this build doesn't know, with a sentence for the window.
func (s Situation) check() error {
	switch s {
	case SituationNone, SituationChecksFailing, SituationConflicts, SituationChangesRequested, SituationRereview:
		return nil
	default:
		return uierr.New("This version doesn't know that situation, so it can't choose its prompt.",
			fmt.Errorf("%w: %q", ErrUnknownSituation, string(s)))
	}
}

// slot is the prompt library's default for the situation.
func (s Situation) slot() prompts.DefaultSlot {
	return prompts.DefaultSlot(s)
}

// promptID returns the prompt d names for the situation, nil when it inherits.
func (s Situation) promptID(d *launchdefaults.Defaults) *int64 {
	switch s {
	case SituationChecksFailing:
		return d.ChecksPromptID
	case SituationConflicts:
		return d.ConflictsPromptID
	case SituationChangesRequested:
		return d.ChangesPromptID
	case SituationRereview:
		return d.RereviewPromptID
	case SituationNone:
		return nil
	default:
		return nil
	}
}

// when names the situation in a sentence, as in "when checks fail".
func (s Situation) when() string {
	switch s {
	case SituationChecksFailing:
		return "when checks fail"
	case SituationConflicts:
		return "when it has conflicts"
	case SituationChangesRequested:
		return "when changes are requested"
	case SituationRereview:
		return "when you're asked to review again"
	case SituationNone:
		return ""
	default:
		return ""
	}
}

// SituationChoice is the prompt a launch for a situation would start with, as Resolve
// resolves it with no choice of the launch's own.
type SituationChoice struct {
	Situation Situation    `json:"situation"`
	Prompt    PromptChoice `json:"prompt"`
}
