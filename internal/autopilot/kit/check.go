package kit

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Check checks the proposal p against the snapshot snap as the app checks each part as it
// is applied, and works out each part's risks from what it changes, never from the agent's
// words. The tool mode checks against the snapshot it serves, and the app checks a card
// again against a snapshot of the setup as it is now, so a card says what applying it
// would do then.
//
//   - Each routine as the builder reads a draft (draft.Read), against the snapshot's prompt
//     library with the proposal's new prompts added, its scripts, and the starter prompts'
//     names, in the mode it would run in: the one Settings says new routines start in for a
//     new routine, whatever the agent suggests, and the routine's own for a change. Its
//     name is checked as Save checks one, and one another routine has, ignoring case, takes
//     the free name a save gives it ("Fix checks 2"); its description and template as Save
//     checks them, the template one of the gallery's. A change names one of the snapshot's
//     routines (else it is Gone) and its base, that routine's updatedAt as the agent read
//     it (else it is Stale), and keeps the routine's mode and template.
//   - Each new prompt as the prompt library checks one as it is added: its names, purpose,
//     review, kinds, and text, the variables its text names among them; the rest of its
//     template (its functions, and how it fills in) is the library's to check as it is
//     added, which the card does.
//   - The settings change as Settings checks the settings it leaves (autopilotprefs), with
//     each setting it changes in the window's words.
//   - The rules change through the rules' own packages: trust's, reporules', and heldruns'.
//
// Every list in the result is there, none nil; Settings and Rules are nil when p changes
// neither.
func Check(ctx context.Context, snap Snapshot, p Proposal) Checked {
	snap = snapshotLists(snap)
	c := newProposalChecker(snap, p.Prompts)

	out := Checked{
		Prompts: make([]CheckedPrompt, 0, len(p.Prompts)), Routines: make([]CheckedRoutine, 0, len(p.Routines)),
	}

	specs := make([]spec.Spec, 0, len(p.Routines))

	for i, rc := range p.Routines {
		r := c.routine(ctx, i, rc)
		out.Routines = append(out.Routines, r)
		specs = append(specs, r.Spec)
	}

	out.Prompts = checkPrompts(snap, p.Prompts, specs)

	if p.Settings != nil {
		settings := checkSettings(snap.Setup.Settings, *p.Settings)
		out.Settings = &settings
	}

	if p.Rules != nil {
		rules := checkRules(snap.Setup.Rules, *p.Rules)
		out.Rules = &rules
	}

	return out
}

// Problems counts what keeps the proposal's parts from being applied: every part's
// problems.
func (c Checked) Problems() int {
	n := 0

	for _, p := range c.Prompts {
		n += len(p.Problems)
	}

	for _, r := range c.Routines {
		n += len(r.Problems)
	}

	if c.Settings != nil {
		n += len(c.Settings.Problems)
	}

	if c.Rules != nil {
		n += len(c.Rules.Problems)
	}

	return n
}

// proposalChecker checks a proposal's routines against one snapshot, in the proposal's
// order, keeping the names they take as applying them in that order would.
type proposalChecker struct {
	snap  Snapshot
	setup checkSetup
	adds  []NewPrompt

	// current are the snapshot's routines' names as the routines checked so far leave
	// them, by ID, and added the names the new ones checked so far take.
	current map[int64]string
	added   []string

	// changed are the routines changed so far, by ID, each with the index of its change.
	changed map[int64]int
}

// newProposalChecker returns a checker of routines against snap, beside the proposal's
// new prompts, adds.
func newProposalChecker(snap Snapshot, adds []NewPrompt) *proposalChecker {
	c := &proposalChecker{
		snap: snap, setup: newCheckSetup(snap, adds), adds: adds,
		current: make(map[int64]string, len(snap.Setup.Routines)), changed: map[int64]int{},
	}

	for _, r := range snap.Setup.Routines {
		c.current[r.ID] = r.Name
	}

	return c
}

// routine checks rc, the proposal's routine at i.
func (c *proposalChecker) routine(ctx context.Context, i int, rc RoutineChange) CheckedRoutine {
	out := CheckedRoutine{
		Index: i, Problems: []spec.Problem{}, Warnings: []spec.Problem{}, DryRunOnly: []catalog.StepKind{},
		Risks: []Risk{},
	}

	mode := c.snap.Setup.Settings.DefaultMode

	if rc.ID != 0 {
		mode = c.change(&out, rc)
	} else {
		c.newOnly(&out, rc)
	}

	c.words(&out, rc)

	read := c.read(ctx, &out, rc.Spec, mode)

	out.Risks = append(out.Risks, routineCautions(out, strings.TrimSpace(rc.Name))...)

	if read {
		out.Risks = append(out.Risks, RoutineRisks(out.Before, out.Spec, c.adds, c.setup.starterNames())...)
	}

	return out
}

// change reads rc as a change to one of the snapshot's routines into out: the routine it
// changes, or Gone, and whether its base is still that routine's updatedAt; it keeps the
// routine's mode and template. It returns the mode the routine runs in.
func (c *proposalChecker) change(out *CheckedRoutine, rc RoutineChange) autopilotprefs.Mode {
	if rc.Mode != "" {
		out.Problems = append(out.Problems, routineProblem("mode", "a change keeps the routine's own mode: leave mode out"))
	}

	if rc.Template != "" {
		out.Problems = append(out.Problems, routineProblem("template",
			"a change keeps the template the routine was made from: leave template out"))
	}

	i := slices.IndexFunc(c.snap.Setup.Routines, func(r Routine) bool { return r.ID == rc.ID })
	if i < 0 {
		out.Gone = true
		out.Problems = append(out.Problems, routineProblem("id", fmt.Sprintf(
			"no routine has the ID %d: leave id out for a new routine, or take a routine's from autopilot_my_setup",
			rc.ID)))

		return c.snap.Setup.Settings.DefaultMode
	}

	before := c.snap.Setup.Routines[i]
	out.Before = &before

	if first, ok := c.changed[rc.ID]; ok {
		out.Problems = append(out.Problems, routineProblem("id", fmt.Sprintf(
			"routines[%d] changes this routine already: make every change to it in one", first)))
	} else {
		c.changed[rc.ID] = out.Index
	}

	if why := baseProblem(rc.Base, before.UpdatedAt, &out.Stale); why != "" {
		out.Problems = append(out.Problems, routineProblem("base", why))
	}

	return before.Mode
}

// baseProblem says what is wrong with a change's base, the updatedAt of the routine it
// changes as the agent read it, or "", and sets stale when it names another moment than
// updated, the routine's updatedAt now.
func baseProblem(base string, updated time.Time, stale *bool) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return "set base to the routine's updatedAt from autopilot_my_setup, so the card can tell when it changed since"
	}

	at, err := time.Parse(time.RFC3339Nano, base)
	if err != nil {
		return fmt.Sprintf("%q isn't a time: set base to the routine's updatedAt from autopilot_my_setup, "+
			"as it is written there", textutil.Truncate(base, 40))
	}

	*stale = !sameMoment(at, updated)

	return ""
}

// sameMoment reports whether base names the moment updated: exactly, or, for a base
// written to the second, the second updated falls in.
func sameMoment(base, updated time.Time) bool {
	return base.Equal(updated) || (base.Nanosecond() == 0 && base.Equal(updated.Truncate(time.Second)))
}

// newOnly checks into out what only a new routine names: no base, and a mode it suggests
// that is one of the modes.
func (c *proposalChecker) newOnly(out *CheckedRoutine, rc RoutineChange) {
	if rc.Base != "" {
		out.Problems = append(out.Problems, routineProblem("base",
			"only a change to one of your routines names a base: leave base out of a new routine"))
	}

	if rc.Mode != "" && !rc.Mode.Valid() {
		out.Problems = append(out.Problems, routineProblem("mode", fmt.Sprintf(
			"say %s, the mode it suggests, which the card shows beside the one it starts in, or leave mode out",
			orChoices(autopilotprefs.Modes()))))
	}

	if key, err := draft.CheckTemplate(rc.Template); err != nil {
		out.Problems = append(out.Problems, routineProblem("template", draft.Message(err)))
	} else if key != "" && !slices.ContainsFunc(c.snap.Vocabulary.Templates, func(t Template) bool { return t.Key == key }) {
		out.Problems = append(out.Problems, routineProblem("template",
			fmt.Sprintf("no template has the key %q: name one of the gallery's, or leave template out", key)))
	}
}

// words checks rc's name and description into out, as Save checks them, and the name it
// would be saved under.
func (c *proposalChecker) words(out *CheckedRoutine, rc RoutineChange) {
	if name, err := draft.CheckName(rc.Name); err != nil {
		out.Problems = append(out.Problems, routineProblem("name", draft.Message(err)))
	} else {
		out.Name, out.NameTaken = c.claim(name, rc.ID, out.Gone)
	}

	if _, err := draft.CheckDescription(rc.Description); err != nil {
		out.Problems = append(out.Problems, routineProblem("description", draft.Message(err)))
	}
}

// claim returns the name the routine id (0 for a new one) would be saved under, given
// name: name, or, when another routine has it, ignoring case, the free name a save gives
// it, and whether it was taken; and keeps it as that routine's from then on. A change to a
// routine that is gone keeps nothing, since it can't be saved.
func (c *proposalChecker) claim(name string, id int64, gone bool) (string, bool) {
	taken := func(n string) bool {
		key := draft.NameKey(n)

		for other, held := range c.current {
			if other != id && draft.NameKey(held) == key {
				return true
			}
		}

		return slices.ContainsFunc(c.added, func(a string) bool { return draft.NameKey(a) == key })
	}

	free := draft.FreeName(name, taken)

	switch {
	case gone:
	case id == 0:
		c.added = append(c.added, free)
	default:
		c.current[id] = free
	}

	return free, free != name
}

// read reads sp, a routine's spec that runs in mode, into out as the builder reads a
// draft, and reports whether it could be read at all.
func (c *proposalChecker) read(ctx context.Context, out *CheckedRoutine, sp spec.Spec, mode autopilotprefs.Mode) bool {
	r, err := draft.Read(ctx, sp, mode, c.setup, nil)
	if err != nil {
		out.Problems = append(out.Problems, routineProblem("spec", draft.Message(err)))

		return false
	}

	out.Spec, out.Sentence = r.Spec, r.Sentence
	out.Problems = append(out.Problems, r.Problems...)
	out.Warnings = append(out.Warnings, r.Warnings...)
	out.DryRunOnly = append(out.DryRunOnly, r.DryRunOnly...)

	return true
}

// routineProblem is a problem with one of a routine's own fields beside its spec (its
// name, description, template, mode, ID, or base), which names no part of the spec.
func routineProblem(field, message string) spec.Problem {
	return spec.Problem{Field: field, Message: message}
}

// checkSetup is what a proposal's routines are checked against beyond themselves: the
// snapshot's prompt library with the proposal's new prompts, the snapshot's scripts, and
// the starter prompts' names. Each is the whole of it, so every check runs.
type checkSetup struct {
	library []draft.Prompt
	scripts []draft.Script

	// names are the starter prompts' nicknames by key: the user's copy's, else the
	// starter's own.
	names map[string]string
}

// newCheckSetup returns what snap's routines, and a proposal adding the prompts adds, are
// checked against. A new prompt joins the library once its nickname, purpose, and kinds
// are ones there are, so a routine may run a prompt the same proposal adds; the prompt's
// other problems are its own.
func newCheckSetup(snap Snapshot, adds []NewPrompt) checkSetup {
	out := checkSetup{
		library: make([]draft.Prompt, 0, len(snap.Setup.Prompts)+len(adds)),
		scripts: make([]draft.Script, 0, len(snap.Setup.Scripts)),
		names:   make(map[string]string, len(snap.Vocabulary.Starters)),
	}

	for _, p := range snap.Setup.Prompts {
		out.library = append(out.library, libraryPrompt(p.Starter, p.Nickname, p.Purpose, p.ReplyAction, p.AppliesTo))
	}

	for _, np := range adds {
		if nick := strings.TrimSpace(np.Nickname); nick != "" && knownPurpose(np.Purpose) && len(promptKinds(np.AppliesTo)) > 0 {
			out.library = append(out.library, libraryPrompt("", nick, np.Purpose, np.ReplyAction, np.AppliesTo))
		}
	}

	for _, s := range snap.Setup.Scripts {
		out.scripts = append(out.scripts, draft.Script{ID: s.ID, Name: s.Name})
	}

	for _, st := range snap.Vocabulary.Starters {
		out.names[st.Key] = st.Nickname
	}

	for _, p := range snap.Setup.Prompts {
		if p.Starter != "" {
			out.names[p.Starter] = p.Nickname
		}
	}

	return out
}

// Library returns the snapshot's prompt library with the proposal's new prompts.
func (s checkSetup) Library(context.Context) ([]draft.Prompt, bool) {
	return s.library, true
}

// Scripts returns the snapshot's scripts.
func (s checkSetup) Scripts(context.Context) ([]draft.Script, bool) {
	return s.scripts, true
}

// StarterNames names a starter prompt by its nickname.
func (s checkSetup) StarterNames(context.Context) spec.PromptNames {
	return s.starterNames()
}

// starterNames names a starter prompt by its nickname, "" for a prompt named otherwise.
func (s checkSetup) starterNames() spec.PromptNames {
	return func(ref spec.PromptRef) string { return s.names[ref.Starter] }
}

// libraryPrompt is a library's prompt as a routine names it: a reply that isn't a
// comment's is a review, of a pull request alone, as the prompt library reads one.
func libraryPrompt(starter, nickname string, purpose Purpose, action ReplyAction, appliesTo AppliesTo) draft.Prompt {
	reply := purpose == PurposeReply
	kinds := promptKinds(appliesTo)

	if reply && action != ReplyComment {
		kinds = []item.Kind{item.KindPR}
	}

	return draft.Prompt{Starter: starter, Nickname: nickname, Reply: reply, ReplyFor: draft.ReplyFor(action), Kinds: kinds}
}

// knownPurpose reports a purpose that is one of the purposes.
func knownPurpose(p Purpose) bool {
	return p == PurposeAgent || p == PurposeReply
}

// knownReview reports a review that is one of the reviews a reply is written for.
func knownReview(a ReplyAction) bool {
	return a == ReplyApprove || a == ReplyRequestChanges || a == ReplyComment
}

// promptKinds returns the kinds of item a prompt written for a applies to, none for one
// that isn't a kind.
func promptKinds(a AppliesTo) []item.Kind {
	switch a {
	case AppliesToPR:
		return []item.Kind{item.KindPR}
	case AppliesToIssue:
		return []item.Kind{item.KindIssue}
	case AppliesToAny:
		return item.Kinds()
	default:
		return nil
	}
}

// orChoices writes values as a problem offers them, each quoted, with or before the
// last: "dryRun", "ask", or "auto".
func orChoices[T ~string](values []T) string {
	quoted := make([]string, 0, len(values))
	for _, v := range values {
		quoted = append(quoted, fmt.Sprintf("%q", string(v)))
	}

	if len(quoted) < 3 {
		return strings.Join(quoted, " or ")
	}

	return strings.Join(quoted[:len(quoted)-1], ", ") + ", or " + quoted[len(quoted)-1]
}
