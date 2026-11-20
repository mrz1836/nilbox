package autopilot

import (
	"context"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// The builder's reading of a draft as it is edited: its sentence, its problems, which
// block saving, and its warnings, which don't, all without saving it or asking GitHub
// anything (Describe). What a draft finds on GitHub, and what its searches cost, is
// Test's. The checks are the draft leaf's, which Save's checks share, and so does
// whatever checks a routine outside the app's own process; the Service hands it what it
// reads beyond the draft as it reads it now (liveSetup).

// Library reads the prompt library for the builder's checks of the prompts a routine's
// steps name. Core adapts the prompt library.
type Library interface {
	Prompts(ctx context.Context) ([]LibraryPrompt, error)
}

// LibraryPrompt is one of the library's prompts as a routine names it, by the starter it
// came from, else by its nickname: the draft leaf's own type, which checks it.
type LibraryPrompt = draft.Prompt

// ReplyFor is the review a library reply is written for, as the prompt library names it:
// the draft leaf's own type.
type ReplyFor = draft.ReplyFor

// The reviews a reply can be written for.
const (
	ReplyForApproval       = draft.ReplyForApproval
	ReplyForRequestChanges = draft.ReplyForRequestChanges
	ReplyForComment        = draft.ReplyForComment
)

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
	r, err := draft.Read(ctx, d.Spec, d.Mode, s.liveSetup(), s.offers)

	return Described{Sentence: r.Sentence, Problems: r.Problems, Warnings: r.Warnings, DryRunOnly: r.DryRunOnly}, err
}

// loadDraft reads a draft's spec as Save stores it, normalized, with its problems: the
// spec's own, else those Save refuses it for beyond them (draft.Problems), such as the
// prompts its steps name that the library doesn't have or that don't fit. A spec that
// can't be read at all, as one past the size a spec may be, is an error.
func (s *Service) loadDraft(ctx context.Context, sp spec.Spec) (spec.Spec, []spec.Problem, error) {
	loaded, problems, err := draft.Load(sp)
	if err != nil || len(problems) > 0 {
		return loaded, problems, err
	}

	return loaded, draft.Problems(ctx, loaded, s.liveSetup(), s.offers), nil
}

// liveSetup returns what a draft's checks read beyond the draft, as the Service reads it
// now: the prompt library, the user's scripts, and the starter prompts' names.
func (s *Service) liveSetup() draft.Setup {
	return serviceSetup{s: s}
}

// serviceSetup is the Service's own reads for a draft's checks. One that can't be read
// is logged, and its checks find nothing: the engine checks a step's prompt again right
// before each start (promptHold), and holds a step whose script was renamed (scriptAsks).
type serviceSetup struct {
	s *Service
}

// Library reads the prompt library, none without one or when it can't be read (logged).
func (r serviceSetup) Library(ctx context.Context) ([]draft.Prompt, bool) {
	if r.s.library == nil {
		return nil, false
	}

	library, err := r.s.library.Prompts(ctx)
	if err != nil {
		r.s.logger.WarnContext(ctx, "could not read the prompt library to check a routine's prompts", "error", err)

		return nil, false
	}

	return library, true
}

// Scripts reads the user's own Repo scripts, none without them or when they can't be read
// (logged).
func (r serviceSetup) Scripts(ctx context.Context) ([]draft.Script, bool) {
	if r.s.myScripts == nil {
		return nil, false
	}

	mine, err := r.s.myScripts.MyScripts(ctx)
	if err != nil {
		r.s.logger.WarnContext(ctx, "could not read the user's scripts to check a routine's", "error", err)

		return nil, false
	}

	out := make([]draft.Script, 0, len(mine))
	for _, m := range mine {
		out = append(out, draft.Script{ID: m.ID, Name: m.Name})
	}

	return out, true
}

// StarterNames names the starter prompts as the routines are shown with them (view).
func (r serviceSetup) StarterNames(ctx context.Context) spec.PromptNames {
	return r.s.view(ctx).names
}
