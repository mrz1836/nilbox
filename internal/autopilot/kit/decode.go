package kit

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// What a proposal may be as the agent sends it, beside MaxRoutines, MaxPrompts, and
// MaxPromptBody.
const (
	// MaxProposalBytes caps a proposal's JSON, as the tool mode caps a request.
	MaxProposalBytes = 4 << 20

	// MaxSummaryRunes caps a proposal's summary: a sentence or two.
	MaxSummaryRunes = 400

	// MaxRuleChanges caps each of a rules change's lists: one proposal changes a few rules,
	// never the whole of one.
	MaxRuleChanges = 100

	// proposalDepth fits a spec at its deepest under the three levels a proposal holds it
	// in: the proposal, its routines, and a routine.
	proposalDepth = 3 + specDepth
)

// ErrUnreadableProposal is returned for a proposal that isn't one: not JSON, a key the
// format doesn't have or doesn't spell exactly (Autopilot's switch and Pause among them),
// a key given twice, a value of the wrong type, anything after it, past one of its caps,
// or proposing nothing. Its text says what to fix, and where.
var ErrUnreadableProposal = errors.New("the proposal can't be read")

// DecodeProposal reads a proposal as the agent sends it, strictly, as spec.DecodeExact
// reads one, so a spec inside it is held to a stored spec's rules, and within its caps: at
// most MaxRoutines routines and MaxPrompts prompts, each prompt's text within
// MaxPromptBody, each spec within spec.MaxBytes once encoded, at most MaxRuleChanges in
// each of the rules' lists, and a summary of one line of plain text within
// MaxSummaryRunes. A proposal that proposes nothing, or a settings or rules change that
// names nothing, is refused too. The proposal it returns is as it encodes and reads back:
// its summary trimmed, and an empty list left out, as encoding it leaves one out, so what
// is checked is what a file stores. Each refusal is an ErrUnreadableProposal, worded for
// the agent to fix: "the proposal can't be read: routines[2].spec: ...".
func DecodeProposal(data []byte) (Proposal, error) {
	p, err := spec.DecodeExact[Proposal](data, MaxProposalBytes, proposalDepth)
	if err != nil {
		if key := switchKey(data); key != "" {
			return Proposal{}, fmt.Errorf("%w: settings.%s: %s", ErrUnreadableProposal, key, switchWords(key))
		}

		return Proposal{}, fmt.Errorf("%w: %w", ErrUnreadableProposal, err)
	}

	return readProposal(p)
}

// readProposal returns p as it encodes and reads back, refusing it past one of its caps.
func readProposal(p Proposal) (Proposal, error) {
	p, err := canonical(p)
	if err != nil {
		return Proposal{}, fmt.Errorf("%w: %w", ErrUnreadableProposal, err)
	}

	if why := proposalCaps(p); why != "" {
		return Proposal{}, fmt.Errorf("%w: %s", ErrUnreadableProposal, why)
	}

	return p, nil
}

// canonical returns p as it encodes and reads back: an empty list that encoding leaves
// out read as none, as a file stores it, and its summary trimmed.
func canonical(p Proposal) (Proposal, error) {
	data, err := storedJSON(p)
	if err != nil {
		return Proposal{}, err
	}

	// What kit encoded, it reads; should it not, the proposal is refused all the same.
	var out Proposal

	err = json.Unmarshal(data, &out)
	out.Summary = strings.TrimSpace(out.Summary)

	return out, err
}

// proposalCaps says which of its caps p is past, where, and what to do, or "".
func proposalCaps(p Proposal) string {
	checks := []func(Proposal) string{summaryCap, emptyCap, routinesCap, promptsCap, settingsCap, rulesCap}

	for _, check := range checks {
		if why := check(p); why != "" {
			return why
		}
	}

	return ""
}

// summaryCap says what is wrong with p's summary, or "".
func summaryCap(p Proposal) string {
	switch {
	case p.Summary == "":
		return "summary: say what it does in a sentence or two, as the user reads it on the card"
	case utf8.RuneCountInString(p.Summary) > MaxSummaryRunes:
		return fmt.Sprintf("summary: keep it to %d characters", MaxSummaryRunes)
	case strings.ContainsFunc(p.Summary, notInALine):
		return "summary: keep it to one line of plain text"
	default:
		return ""
	}
}

// emptyCap says p proposes nothing, or "".
func emptyCap(p Proposal) string {
	if len(p.Prompts) == 0 && len(p.Routines) == 0 && p.Settings == nil && p.Rules == nil {
		return "it proposes nothing: add a prompt, a routine, a settings change, or a rules change"
	}

	return ""
}

// routinesCap says p holds too many routines, or one whose spec is too large, or "".
func routinesCap(p Proposal) string {
	if len(p.Routines) > MaxRoutines {
		return fmt.Sprintf("routines: at most %d in one proposal; split the rest into proposals of their own", MaxRoutines)
	}

	for i, r := range p.Routines {
		data, err := spec.Encode(r.Spec)
		if err != nil {
			return fmt.Sprintf("routines[%d].spec: %s", i, err)
		}

		if len(data) > spec.MaxBytes {
			return fmt.Sprintf("routines[%d].spec: a routine is at most %d bytes once encoded, and this is %d",
				i, spec.MaxBytes, len(data))
		}
	}

	return ""
}

// promptsCap says p holds too many prompts, or one whose text is too long, or "".
func promptsCap(p Proposal) string {
	if len(p.Prompts) > MaxPrompts {
		return fmt.Sprintf("prompts: at most %d in one proposal; split the rest into proposals of their own", MaxPrompts)
	}

	for i, np := range p.Prompts {
		if len(np.Body) > MaxPromptBody {
			return fmt.Sprintf("prompts[%d].body: keep a prompt's text to %d bytes", i, MaxPromptBody)
		}
	}

	return ""
}

// settingsCap says p's settings change names no setting, or "".
func settingsCap(p Proposal) string {
	if p.Settings != nil && *p.Settings == (SettingsChange{}) {
		return "settings: name at least one setting it changes, or leave settings out"
	}

	return ""
}

// rulesCap says p's rules change names nothing, or too much in one of its lists, or "".
func rulesCap(p Proposal) string {
	if p.Rules == nil {
		return ""
	}

	lists := ruleLists(*p.Rules)
	total := 0

	for _, l := range lists {
		if len(l.values) > MaxRuleChanges {
			return fmt.Sprintf("rules.%s: at most %d in one proposal", l.key, MaxRuleChanges)
		}

		total += len(l.values)
	}

	if total == 0 {
		return "rules: name at least one change, or leave rules out"
	}

	return ""
}

// ruleList is one of a rules change's lists, by its key.
type ruleList struct {
	key    string
	values []string
}

// ruleLists returns c's lists, in the proposal's order.
func ruleLists(c RulesChange) []ruleList {
	return []ruleList{
		{"trust", c.Trust},
		{"untrust", c.Untrust},
		{"pin", c.Pin},
		{"mute", c.Mute},
		{"clear", c.Clear},
		{"ignoreWorkflows", c.IgnoreWorkflows},
		{"unignoreWorkflows", c.UnignoreWorkflows},
	}
}

// notInALine reports a rune one line of plain text doesn't hold: a control character, a
// line or paragraph separator, one that changes the text's direction, or one that shows
// nothing and stands for another.
func notInALine(r rune) bool {
	return draft.Hidden(r) || unicode.In(r, unicode.Zl, unicode.Zp) || smuggles(r)
}

// switchKey returns the setting of Autopilot's that data's settings change names but no
// proposal may, its switch or Pause, or "": strict decoding refuses either as a key the
// format lacks, and this says which, and why.
func switchKey(data []byte) string {
	var probe struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}

	if json.Unmarshal(data, &probe) != nil {
		return ""
	}

	for _, key := range []string{"enabled", "paused"} {
		if _, ok := probe.Settings[key]; ok {
			return key
		}
	}

	return ""
}

// switchWords says why a proposal never names key, Autopilot's switch or Pause.
func switchWords(key string) string {
	what := "Autopilot's switch"
	if key == "paused" {
		what = "Pause"
	}

	return what + " is the user's alone, so a proposal never names it: leave it out"
}
