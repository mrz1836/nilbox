package kit

import (
	"fmt"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// maxCheckedWords caps a checked proposal's words, well under what a tool's result may be
// before the agent's client sets it aside unread.
const maxCheckedWords = 48 << 10

// Words says the checked proposal as the agent reads it in a check's result: what was
// checked; each part, with where each problem and warning is in the proposal
// ("routines[0].spec.steps[1].agent.prompt"), the sentence each routine reads as, the
// name it is saved under, and the risks the user sees on the card; and a closing line
// saying whether it can be proposed. It stays within maxCheckedWords, its last lines cut
// when it would pass it, the closing line kept.
func (c Checked) Words() string {
	lines := []string{checkedLead(c)}

	for _, p := range c.Prompts {
		lines = append(lines, promptLines(p)...)
	}

	for _, r := range c.Routines {
		lines = append(lines, routineLines(r)...)
	}

	if c.Settings != nil {
		lines = append(lines, settingsLines(*c.Settings)...)
	}

	if c.Rules != nil {
		lines = append(lines, rulesLines(*c.Rules)...)
	}

	return keptLines(lines, "", closingLine(c.Problems()))
}

// checkedLead says what was checked.
func checkedLead(c Checked) string {
	var parts []string

	if n := len(c.Prompts); n > 0 {
		parts = append(parts, textutil.Counted(n, "new prompt", "new prompts"))
	}

	if n := len(c.Routines); n > 0 {
		parts = append(parts, textutil.Counted(n, "routine", "routines"))
	}

	if c.Settings != nil {
		parts = append(parts, "a settings change")
	}

	if c.Rules != nil {
		parts = append(parts, "a rules change")
	}

	if len(parts) == 0 {
		return "Checked nothing: the proposal holds no part."
	}

	return "Checked " + textutil.Series(parts) + "."
}

// promptLines says a checked prompt.
func promptLines(p CheckedPrompt) []string {
	where := fmt.Sprintf("prompts[%d]", p.Index)
	lines := []string{"", fmt.Sprintf("Prompt %d, %s:", p.Index+1, headName(p.Prompt.Nickname))}

	for _, why := range p.Problems {
		lines = append(lines, "- Problem at "+where+": "+why)
	}

	return append(lines, partEnd(len(p.Problems), nil, p.Risks)...)
}

// routineLines says a checked routine.
func routineLines(r CheckedRoutine) []string {
	lines := []string{"", routineHead(r)}

	if r.Sentence != "" {
		lines = append(lines, "It reads: "+r.Sentence)
	}

	for _, p := range r.Problems {
		lines = append(lines, "- Problem at "+problemPath(r.Index, p)+": "+p.Message)
	}

	notes := make([]string, 0, len(r.Warnings)+1)
	for _, w := range r.Warnings {
		notes = append(notes, "- Warning at "+problemPath(r.Index, w)+": "+w.Message)
	}

	if len(r.DryRunOnly) > 0 {
		notes = append(notes, "- This build runs it only in Dry run, whatever its mode, for "+stepLabels(r.DryRunOnly)+".")
	}

	return append(lines, partEnd(len(r.Problems), notes, r.Risks)...)
}

// routineHead heads a checked routine: new or a change, and the name it is saved under.
func routineHead(r CheckedRoutine) string {
	head := fmt.Sprintf("Routine %d, new", r.Index+1)

	switch {
	case r.Gone:
		return fmt.Sprintf("Routine %d, a change to a routine your setup doesn't have:", r.Index+1)
	case r.Before != nil:
		head = fmt.Sprintf("Routine %d, a change to %s (id %d)", r.Index+1, headName(r.Before.Name), r.Before.ID)
	}

	if r.Name != "" {
		head += ", saved as " + headName(r.Name)
	}

	return head + ":"
}

// problemPath says where a routine's problem is in the proposal: in its spec, or one of
// its own fields.
func problemPath(i int, p spec.Problem) string {
	if p.Part == "" {
		return fmt.Sprintf("routines[%d].%s", i, p.Field)
	}

	return fmt.Sprintf("routines[%d].spec.%s", i, p.Path())
}

// settingsLines says a checked settings change.
func settingsLines(s CheckedSettings) []string {
	changes := make([]string, 0, len(s.Changes))
	for _, c := range s.Changes {
		changes = append(changes, c.Label+" "+c.Before+" → "+c.After)
	}

	lines := []string{"", "Settings: " + changedWords(changes)}

	for _, why := range s.Problems {
		lines = append(lines, "- Problem at settings: "+why)
	}

	return append(lines, partEnd(len(s.Problems), nil, s.Risks)...)
}

// rulesLines says a checked rules change.
func rulesLines(r CheckedRules) []string {
	changes := make([]string, 0, len(r.Changes))
	for _, c := range r.Changes {
		changes = append(changes, c.Words)
	}

	lines := []string{"", "Rules: " + changedWords(changes)}

	for _, why := range r.Problems {
		lines = append(lines, "- Problem: "+why)
	}

	return append(lines, partEnd(len(r.Problems), nil, r.Risks)...)
}

// changedWords lists a part's changes as one line: "Trust renovate[bot]; Mute
// acme-org/legacy."
func changedWords(changes []string) string {
	if len(changes) == 0 {
		return "no change."
	}

	return textutil.EndSentence(strings.Join(changes, "; "))
}

// partEnd says a part has no problem, when it has none, then its notes, then each of its
// risks.
func partEnd(problems int, notes []string, risks []Risk) []string {
	var lines []string

	if problems == 0 {
		lines = append(lines, "- No problems.")
	}

	lines = append(lines, notes...)

	for _, r := range risks {
		lines = append(lines, fmt.Sprintf("- The card shows this %s: %s", r.Level, r.Text))
	}

	return lines
}

// closingLine says whether a proposal with problems problems can be proposed.
func closingLine(problems int) string {
	if problems == 0 {
		return "It has no problems: it can be proposed with autopilot_propose, and the user reviews it in the " +
			"Autopilot tab."
	}

	return fmt.Sprintf("It has %s: fix each where it is, then check it again. A proposal with a problem can't be "+
		"proposed.", textutil.Counted(problems, "problem", "problems"))
}

// keptLines joins lines, and then end, each on a line of its own, keeping as many of the
// lines as fit within maxCheckedWords, and saying so when some are cut.
func keptLines(lines []string, end ...string) string {
	const cut = "… (the rest is cut: fix what is above, then check it again)"

	budget := maxCheckedWords - len(strings.Join(end, "\n")) - len(cut) - 2
	kept := make([]string, 0, len(lines)+len(end)+1)
	size := 0

	for _, l := range lines {
		if size+len(l)+1 > budget {
			kept = append(kept, cut)

			break
		}

		size += len(l) + 1
		kept = append(kept, l)
	}

	return strings.Join(append(kept, end...), "\n")
}

// headName writes a name in curly quotes, one line, "(no name)" for none.
func headName(name string) string {
	if name = textutil.OneLine(name); name == "" {
		return "(no name)"
	}

	return "“" + name + "”"
}

// stepLabels names kinds of step as the builder does: "“Merge it” and “Tell me”".
func stepLabels(kinds []catalog.StepKind) string {
	labels := make([]string, 0, len(kinds))

	for _, k := range kinds {
		label := string(k)
		if info, ok := catalog.StepByKind(k); ok {
			label = info.Label
		}

		labels = append(labels, "“"+label+"”")
	}

	return textutil.Series(labels)
}
