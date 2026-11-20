package draft_test

import (
	"context"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/item"
)

// setup is a Setup of what a test hands it, counting its reads: its library and its
// scripts, either of which may be one that can't be read, and the starter prompts' names.
type setup struct {
	library   []draft.Prompt
	noLibrary bool
	scripts   []draft.Script
	noScripts bool
	names     map[string]string

	libraryReads, scriptReads int
}

func (s *setup) Library(context.Context) ([]draft.Prompt, bool) {
	s.libraryReads++

	return s.library, !s.noLibrary
}

func (s *setup) Scripts(context.Context) ([]draft.Script, bool) {
	s.scriptReads++

	return s.scripts, !s.noScripts
}

func (s *setup) StarterNames(context.Context) spec.PromptNames {
	if s.names == nil {
		return nil
	}

	return func(p spec.PromptRef) string { return s.names[p.Starter] }
}

// library is a prompt library: Fix checks, a starter for pull requests; Triage, a prompt
// of the user's own for any item; and the replies: an approval's, a request for changes',
// and a comment's for any item.
func library() []draft.Prompt {
	return []draft.Prompt{
		{Starter: "fix-checks", Nickname: "Fix checks", Kinds: []item.Kind{item.KindPR}},
		{Nickname: "Triage", Kinds: item.Kinds()},
		{
			Starter: "reply-lgtm", Nickname: "LGTM", Reply: true, ReplyFor: draft.ReplyForApproval,
			Kinds: []item.Kind{item.KindPR},
		},
		{
			Starter: "reply-needs-changes", Nickname: "Needs changes", Reply: true,
			ReplyFor: draft.ReplyForRequestChanges, Kinds: []item.Kind{item.KindPR},
		},
		{
			Starter: "reply-question", Nickname: "Question", Reply: true, ReplyFor: draft.ReplyForComment,
			Kinds: item.Kinds(),
		},
	}
}

// notifySpec is a routine that tells the user when one of their pull requests' checks
// fail.
func notifySpec() spec.Spec {
	return spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{{Lists: []catalog.ListSource{catalog.SourceAuthored}}}},
		Trigger: spec.Trigger{Event: catalog.EventChecksFailed},
		Steps:   []spec.Step{{Notify: &spec.NotifyStep{Note: "Have a look"}}},
	}
}

// agentSpec is a routine that runs prompt where says on the user's failing pull requests.
func agentSpec(prompt spec.PromptRef, where catalog.Where) spec.Spec {
	s := notifySpec()
	s.Steps = []spec.Step{{Agent: &spec.AgentStep{Prompt: prompt, Where: where}}}

	return s
}

// lintSpec is a routine that tells the user of an item that appears, then runs the script
// picked as Lint, ID 7.
func lintSpec() spec.Spec {
	s := notifySpec()
	s.Trigger = spec.Trigger{Event: catalog.EventAppears}
	s.Steps = append(s.Steps, spec.Step{Script: &spec.ScriptStep{ID: 7, Name: "Lint"}})

	return s
}

// terminalOnly is a build that runs every step and action the catalog offers, and an
// agent in the terminal alone.
func terminalOnly() *draft.Offers {
	out := draft.CatalogOffers()
	out.Places = []catalog.Where{catalog.WhereTerminal}

	return out
}
