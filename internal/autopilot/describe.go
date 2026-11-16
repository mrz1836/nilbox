package autopilot

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The builder's reading of a draft as it is edited: its sentence, its problems, which
// block saving, and its warnings, which don't, all without saving it or asking GitHub
// anything (Describe). What a draft finds on GitHub, and what its searches cost, is
// Test's.

// Library reads the prompt library for the builder's checks of the prompts a routine's
// steps name. Core adapts the prompt library.
type Library interface {
	Prompts(ctx context.Context) ([]LibraryPrompt, error)
}

// LibraryPrompt is one of the library's prompts as a routine names it: by the starter it
// came from, else by its nickname.
type LibraryPrompt struct {
	Starter  string
	Nickname string

	// Reply reports a review reply, which a GitHub step comments, rather than a prompt an
	// agent runs.
	Reply bool

	// Kinds are the kinds of item it applies to.
	Kinds []item.Kind
}

// Described is a draft as the builder shows it before it is saved.
type Described struct {
	// Sentence says the draft in one sentence, as the routine list will; "" while it has
	// problems, so the builder keeps the last one that read.
	Sentence string `json:"sentence"`

	// Problems are what keeps it from being saved, each where it is in the spec.
	Problems []spec.Problem `json:"problems"`

	// Warnings are what may make it do less than meant, or more than is wise, each where
	// it is; none keeps it from being saved.
	Warnings []spec.Problem `json:"warnings"`

	// DryRunOnly are the kinds of step it has that this build can't run yet, so it runs
	// in Dry run alone, whatever its mode.
	DryRunOnly []catalog.StepKind `json:"dryRunOnly"`
}

// Describe reads the draft d as the builder shows it while it is edited: its sentence,
// the problems Save would refuse it for (the spec's own, and a prompt the library doesn't
// have or that doesn't apply to the items it acts on), its warnings, and the steps this
// build runs only in Dry run. It saves nothing, reads only the prompt library and the
// settings, and never asks GitHub.
func (s *Service) Describe(ctx context.Context, d Draft) (Described, error) {
	out := Described{Problems: []spec.Problem{}, Warnings: []spec.Problem{}, DryRunOnly: []catalog.StepKind{}}

	sp, problems, err := s.loadDraft(ctx, d.Spec)
	if err != nil {
		return out, err
	}

	out.Problems = append(out.Problems, problems...)
	out.DryRunOnly = s.offers.notOffered(sp)
	out.Warnings = draftWarnings(sp, d.Mode, len(out.DryRunOnly) > 0)

	if len(out.Problems) == 0 {
		out.Sentence = spec.Sentence(sp, s.view(ctx).names)
	}

	return out, nil
}

// loadDraft reads a draft's spec as Save stores it, normalized, with its problems: the
// spec's own, else the prompts its steps name that the library doesn't have or that
// don't fit. A spec that can't be read at all, as one past the size a spec may be, is
// an error.
func (s *Service) loadDraft(ctx context.Context, sp spec.Spec) (spec.Spec, []spec.Problem, error) {
	if sp.Version == 0 {
		sp.Version = spec.Version
	}

	normal := spec.Normalize(sp)

	data, err := spec.Encode(normal)
	if err != nil {
		return normal, nil, err
	}

	loaded, err := spec.Load(data)
	if v, ok := errors.AsType[*spec.ValidationError](err); ok {
		return normal, slices.Clone(v.Problems), nil
	} else if err != nil {
		return normal, nil, uierr.New("The routine can't be read: "+textutil.Sentence(err.Error()), err)
	}

	return loaded, append(s.offers.placeProblems(loaded), s.promptProblems(ctx, loaded)...), nil
}

// promptProblems returns the problems of the prompts sp's steps name: one the library
// doesn't have, a reply named where an agent's prompt goes or the other way about, and
// one that doesn't apply to every kind of item the routine acts on. Without a library,
// or one that can't be read (logged), it finds none here; the engine checks a step's
// prompt again right before each start (promptHold), so a prompt deleted, changed, or
// renamed beyond what the routine follows after the save, as after SetEnabled, Duplicate,
// a restore, or an import, keeps the run from starting and says why.
func (s *Service) promptProblems(ctx context.Context, sp spec.Spec) []spec.Problem {
	if s.library == nil {
		return nil
	}

	library, err := s.library.Prompts(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "could not read the prompt library to check a routine's prompts", "error", err)

		return nil
	}

	return libraryProblems(library, sp)
}

// libraryProblems returns the problems of the prompts sp's steps name, against library.
func libraryProblems(library []LibraryPrompt, sp spec.Spec) []spec.Problem {
	kinds := actsOn(sp)
	out := make([]spec.Problem, 0, len(sp.Steps))

	for i, st := range sp.Steps {
		out = append(out, stepPromptProblems(library, st, kinds, fmt.Sprintf("[%d]", i))...)
	}

	return out
}

// stepPromptProblems returns the problems of the prompts one step, at field, names: an
// agent step's prompt (the item's own default needs no check), and a GitHub step's replies.
func stepPromptProblems(library []LibraryPrompt, st spec.Step, kinds []item.Kind, field string) []spec.Problem {
	var out []spec.Problem

	if st.Agent != nil && !st.Agent.Prompt.ItemDefault {
		out = append(out, promptProblem(library, st.Agent.Prompt, false, kinds, field+".agent.prompt")...)
	}

	for j, a := range st.GitHub {
		if a.Reply != nil {
			at := fmt.Sprintf("%s.github[%d].reply.prompt", field, j)
			out = append(out, promptProblem(library, a.Reply.Prompt, true, kinds, at)...)
		}
	}

	return out
}

// promptProblem checks one prompt a step names, a reply when reply, at field.
func promptProblem(library []LibraryPrompt, ref spec.PromptRef, reply bool, kinds []item.Kind, field string,
) []spec.Problem {
	p, ok := findPrompt(library, ref)
	at := func(msg string) []spec.Problem {
		return []spec.Problem{{Part: spec.PartSteps, Field: field, Message: msg}}
	}

	switch {
	case !ok && ref.Starter != "":
		return at("that starter prompt isn't in your library any more: pick another prompt")
	case !ok:
		return at(fmt.Sprintf("no prompt in your library has the nickname %q: pick one that does", ref.Nickname))
	case p.Reply && !reply:
		return at(fmt.Sprintf("%q is a review reply: an agent runs a prompt", p.Nickname))
	case !p.Reply && reply:
		return at(fmt.Sprintf("%q is a prompt for an agent: a GitHub step comments a reply", p.Nickname))
	}

	if msg := kindsProblem(p, kinds); msg != "" {
		return at(msg)
	}

	return nil
}

// kindsProblem says what is wrong with p for a routine that acts on kinds, or "": a prompt
// for pull requests on a routine of issues alone, or one for one kind on a routine of both.
func kindsProblem(p LibraryPrompt, kinds []item.Kind) string {
	for _, k := range kinds {
		switch {
		case slices.Contains(p.Kinds, k):
		case len(kinds) == 1:
			return fmt.Sprintf("%q is written for %s, and this routine acts only on %s: pick a prompt for %s",
				p.Nickname, kindsWords(p.Kinds), k.Plural(), k.Plural())
		default:
			return fmt.Sprintf("%q is written for %s, and this routine acts on %s too: pick a prompt for both, "+
				"or watch only %s", p.Nickname, kindsWords(p.Kinds), k.Plural(), kindsWords(p.Kinds))
		}
	}

	return ""
}

// findPrompt returns the library's prompt ref names: the user's copy of a starter, or the
// one with a nickname, ignoring case.
func findPrompt(library []LibraryPrompt, ref spec.PromptRef) (LibraryPrompt, bool) {
	for _, p := range library {
		if (ref.Starter != "" && p.Starter == ref.Starter) ||
			(ref.Starter == "" && ref.Nickname != "" && strings.EqualFold(p.Nickname, ref.Nickname)) {
			return p, true
		}
	}

	return LibraryPrompt{}, false
}

// kindsWords names kinds of item: "pull requests", "issues", "pull requests and issues".
func kindsWords(kinds []item.Kind) string {
	words := make([]string, 0, len(kinds))
	for _, k := range kinds {
		words = append(words, k.Plural())
	}

	return series(words)
}

// actsOn returns the kinds of item a routine acts on: those both its watch and its trigger
// take.
func actsOn(sp spec.Spec) []item.Kind {
	kinds := sp.Watch.Kinds()

	t, ok := catalog.TriggerByEvent(sp.Trigger.Event)
	if !ok {
		return kinds
	}

	return slices.DeleteFunc(slices.Clone(kinds), func(k item.Kind) bool { return !t.Applies(k) })
}

// triageSteps are the steps that change only how an item shows in the inbox.
func triageSteps() []catalog.StepKind {
	return []catalog.StepKind{catalog.StepPin, catalog.StepSnooze, catalog.StepDismiss, catalog.StepRestore}
}

// draftWarnings returns what may make sp, in mode, do less than meant or more than is
// wise, without asking GitHub: triage steps over a watch that is only searches, an agent
// in the terminal or with the profile's own permissions on Autopilot. A draft held in Dry
// run (dryRunOnly) runs nothing on its own, so the mode's warnings wait until it can.
func draftWarnings(sp spec.Spec, mode autopilotprefs.Mode, dryRunOnly bool) []spec.Problem {
	out := triageWarnings(sp)

	if mode != autopilotprefs.ModeAuto || dryRunOnly {
		return out
	}

	return append(out, autopilotWarnings(sp)...)
}

// triageWarnings warns of each triage step of a routine that watches only searches: it
// changes nothing the user sees of an item no inbox list holds.
func triageWarnings(sp spec.Spec) []spec.Problem {
	out := []spec.Problem{}

	if !onlySearches(sp.Watch) {
		return out
	}

	for i, st := range sp.Steps {
		if slices.Contains(triageSteps(), st.Kind()) {
			info, _ := catalog.StepByKind(st.Kind())
			out = append(out, spec.Problem{
				Part: spec.PartSteps, Field: fmt.Sprintf("[%d]", i),
				Message: info.Label + " changes how an item shows in your inbox, and this routine watches only " +
					"searches: items that aren't in your inbox lists won't look any different.",
			})
		}
	}

	return out
}

// autopilotWarnings warns, of a routine on Autopilot, of an agent step that runs in the
// terminal, or with the profile's own permissions, where nobody answers.
func autopilotWarnings(sp spec.Spec) []spec.Problem {
	var out []spec.Problem

	for i, st := range sp.Steps {
		switch {
		case st.Agent == nil:
		case st.Agent.Place() == catalog.WhereTerminal:
			out = append(out, spec.Problem{
				Part: spec.PartSteps, Field: fmt.Sprintf("[%d].agent.where", i),
				Message: "On Autopilot, a terminal run opens a terminal tab while you work: Ask first suits it better.",
			})
		case st.Agent.Tools() == catalog.PresetProfile:
			out = append(out, spec.Problem{
				Part: spec.PartSteps, Field: fmt.Sprintf("[%d].agent.fence.tools", i),
				Message: "On Autopilot with the profile's own permissions, the agent may do whatever a click's launch " +
					"may, with nobody there to answer.",
			})
		}
	}

	return out
}

// onlySearches reports a watch whose sources are all searches.
func onlySearches(w spec.Watch) bool {
	if len(w.Sources) == 0 {
		return false
	}

	for _, src := range w.Sources {
		if src.Search == nil {
			return false
		}
	}

	return true
}
