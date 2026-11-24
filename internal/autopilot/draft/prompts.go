package draft

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/item"
)

// promptProblems returns the problems of the prompts sp's steps name: one the library
// doesn't have, a reply named where an agent's prompt goes or the other way about, a
// GitHub step's reply that isn't a comment's (an approving one, LGTM, or a request for
// changes), and one that doesn't apply to every kind of item the routine acts on, as one
// for repositories applies to none. Without a library, or one that can't be read, it finds
// none here; the engine checks a step's prompt, and a GitHub step's replies, again right
// before each start (StepPromptProblems), so a prompt deleted, changed, or renamed beyond
// what the routine follows after the save, as after the routine is switched on,
// duplicated, restored, or imported, keeps the run or the action from starting and says
// why.
func promptProblems(ctx context.Context, sp spec.Spec, s Setup) []spec.Problem {
	library, ok := s.Library(ctx)
	if !ok {
		return nil
	}

	return libraryProblems(library, sp)
}

// libraryProblems returns the problems of the prompts sp's steps name, against library.
func libraryProblems(library []Prompt, sp spec.Spec) []spec.Problem {
	kinds := ActsOn(sp)
	out := make([]spec.Problem, 0, len(sp.Steps))

	for i, st := range sp.Steps {
		out = append(out, StepPromptProblems(library, st, kinds, fmt.Sprintf("[%d]", i))...)
	}

	return out
}

// StepPromptProblems returns the problems of the prompts one step, at field, names, against
// library, for a routine that acts on kinds: an agent step's prompt (the item's own default
// needs no check), and a GitHub step's replies.
func StepPromptProblems(library []Prompt, st spec.Step, kinds []item.Kind, field string) []spec.Problem {
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
func promptProblem(library []Prompt, ref spec.PromptRef, reply bool, kinds []item.Kind, field string,
) []spec.Problem {
	p, ok := FindPrompt(library, ref)
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

	if msg := commentProblem(p, reply); msg != "" {
		return at(msg)
	}

	if msg := kindsProblem(p, kinds); msg != "" {
		return at(msg)
	}

	return nil
}

// commentProblem says what is wrong with the reply p for a GitHub step, or "": a GitHub
// step only comments (D5), so it posts a comment's reply alone, never an approval's, whose
// words would read as one, nor a request for changes'.
func commentProblem(p Prompt, reply bool) string {
	switch {
	case !reply || p.ReplyFor == ReplyForComment:
		return ""
	case p.ReplyFor == ReplyForApproval:
		return fmt.Sprintf("%q is a reply that approves: a GitHub step only comments, so it never posts an "+
			"approval's words; pick a comment reply", p.Nickname)
	default:
		return fmt.Sprintf("%q is a reply that requests changes: a GitHub step only comments; pick a comment reply",
			p.Nickname)
	}
}

// kindsProblem says what is wrong with p for a routine that acts on kinds, or "": a prompt
// written for no kind of item at all, as one for repositories is, whatever the routine acts
// on; a prompt for pull requests on a routine of issues alone; or one for one kind on a
// routine of both.
func kindsProblem(p Prompt, kinds []item.Kind) string {
	if len(p.Kinds) == 0 {
		return fmt.Sprintf("%q isn't written for pull requests or issues, and a routine runs its prompts on those "+
			"alone: pick another prompt", p.Nickname)
	}

	for _, k := range kinds {
		switch {
		case slices.Contains(p.Kinds, k):
		case len(kinds) == 1:
			return fmt.Sprintf("%q is written for %s, and this routine acts only on %s: pick a prompt for %s",
				p.Nickname, KindsWords(p.Kinds), k.Plural(), k.Plural())
		default:
			return fmt.Sprintf("%q is written for %s, and this routine acts on %s too: pick a prompt for both, "+
				"or watch only %s", p.Nickname, KindsWords(p.Kinds), k.Plural(), KindsWords(p.Kinds))
		}
	}

	return ""
}

// FindPrompt returns the library's prompt ref names (PromptIndex), and whether there is
// one.
func FindPrompt(library []Prompt, ref spec.PromptRef) (Prompt, bool) {
	if i := PromptIndex(library, ref); i >= 0 {
		return library[i], true
	}

	return Prompt{}, false
}

// PromptIndex returns the index in library of the prompt ref names, -1 for none: the one
// with its UUID, whatever it is called now; else, as a ref from before UUIDs or from
// another library names one, the user's copy of a starter, or the one with a nickname,
// ignoring case. The item's own default names no library prompt.
func PromptIndex(library []Prompt, ref spec.PromptRef) int {
	if ref.ItemDefault {
		return -1
	}

	if ref.UUID != "" {
		if i := slices.IndexFunc(library, func(p Prompt) bool { return p.UUID == ref.UUID }); i >= 0 {
			return i
		}
	}

	return slices.IndexFunc(library, func(p Prompt) bool {
		return (ref.Starter != "" && p.Starter == ref.Starter) ||
			(ref.Starter == "" && ref.Nickname != "" && strings.EqualFold(p.Nickname, ref.Nickname))
	})
}

// KindsWords names kinds of item: "pull requests", "issues", "pull requests and issues".
func KindsWords(kinds []item.Kind) string {
	words := make([]string, 0, len(kinds))
	for _, k := range kinds {
		words = append(words, k.Plural())
	}

	return Series(words)
}

// ActsOn returns the kinds of item a routine acts on: those both its watch and its trigger
// take.
func ActsOn(sp spec.Spec) []item.Kind {
	kinds := sp.Watch.Kinds()

	t, ok := catalog.TriggerByEvent(sp.Trigger.Event)
	if !ok {
		return kinds
	}

	return slices.DeleteFunc(slices.Clone(kinds), func(k item.Kind) bool { return !t.Applies(k) })
}
