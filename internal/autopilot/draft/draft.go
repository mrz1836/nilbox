// Package draft reads a routine's draft as the builder edits it, before it is saved: its
// spec as Save stores it, the problems that keep it from being saved, its warnings, the
// steps this build runs only in Dry run, and its sentence. The autopilot package's
// Describe and Save read a draft here, and so does whatever checks a routine outside the
// app's own process, so a routine reads the same wherever it is checked.
//
// It is a pure leaf. What a draft's checks read beyond the draft (the prompt library, the
// user's scripts, and the starter prompts' names) comes through Setup, which the caller
// adapts, and what this build runs through Offers. It reads no database, starts nothing,
// and never asks GitHub.
package draft

import (
	"context"
	"errors"
	"slices"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Setup is what a draft's checks read beyond the draft itself. A read can fail, as a
// library that can't be read does: the checks it answers are then skipped, and the engine
// checks a step's prompt, and its script, again right before each start.
type Setup interface {
	// Library returns the prompt library, false when there is none or it can't be read,
	// which skips the prompt checks.
	Library(ctx context.Context) ([]Prompt, bool)

	// Scripts returns the user's own Repo scripts, read only for a spec with a script
	// step: false when there are none to read or they can't be read, which skips the
	// script checks.
	Scripts(ctx context.Context) ([]Script, bool)

	// StarterNames names the starter prompts in the sentence, as the library names them;
	// nil writes a starter's key as words.
	StarterNames(ctx context.Context) spec.PromptNames

	// PastRules reports Let routines merge your own pull requests past the rules switched
	// on in Autopilot's settings; false when it is off or can't be read, which warns of a
	// Merge it that allows it.
	PastRules(ctx context.Context) bool
}

// Prompt is one of the library's prompts as a routine names it: by the starter it came
// from, else by its nickname.
type Prompt struct {
	Starter  string
	Nickname string

	// Reply reports a review reply, which a GitHub step comments, rather than a prompt an
	// agent runs.
	Reply bool

	// ReplyFor is the review a reply is written for: an approval, a request for changes,
	// or a comment. A GitHub step only comments (D5), so it posts a comment's reply alone,
	// never an approving one such as LGTM, whose words would read as an approval.
	ReplyFor ReplyFor

	// Kinds are the kinds of item it applies to.
	Kinds []item.Kind
}

// ReplyFor is the review a library reply is written for, as the prompt library names it.
type ReplyFor string

// The reviews a reply can be written for.
const (
	ReplyForApproval       ReplyFor = "approve"
	ReplyForRequestChanges ReplyFor = "request_changes"
	ReplyForComment        ReplyFor = "comment"
)

// Script is one of the user's own Repo scripts as a script step names it: its ID on this
// Mac, and the name it is saved under now.
type Script struct {
	ID   int64
	Name string
}

// Reading is a draft as the builder shows it before it is saved, with its spec as Save
// stores it.
type Reading struct {
	// Spec is the draft's spec as Save stores it: normalized, and as it loads back once it
	// loads.
	Spec spec.Spec

	// Sentence says the draft in one sentence, as the routine list will; "" while it has
	// problems, so the builder keeps the last one that read.
	Sentence string

	// Problems are what keeps it from being saved, each where it is in the spec; never nil.
	Problems []spec.Problem

	// Warnings are what may make it do less than meant, or more than is wise, each where
	// it is; none keeps it from being saved, and the list is never nil.
	Warnings []spec.Problem

	// DryRunOnly are the kinds of step it has that this build can't run yet, so it runs
	// in Dry run alone, whatever its mode; never nil.
	DryRunOnly []catalog.StepKind
}

// Read reads the draft spec sp, whose routine acts in mode, as the builder shows it while
// it is edited: its spec as Save stores it (Load), the problems Save would refuse it for
// (the spec's own, else Problems'), the steps this build runs only in Dry run (o's), its
// warnings, and, only while it has no problem, its sentence. It saves nothing and never
// asks GitHub: beyond the draft it reads s's library, its scripts for a script step, for a
// Merge it past the rules whether the user's switch allows it, and, for the sentence, its
// starter prompts' names. A spec that can't be read at all, as one past the size a spec
// may be, is an error.
func Read(ctx context.Context, sp spec.Spec, mode autopilotprefs.Mode, s Setup, o *Offers) (Reading, error) {
	out := Reading{Problems: []spec.Problem{}, Warnings: []spec.Problem{}, DryRunOnly: []catalog.StepKind{}}

	loaded, problems, err := Load(sp)
	if err != nil {
		return out, err
	}

	if len(problems) == 0 {
		problems = Problems(ctx, loaded, s, o)
	}

	out.Spec = loaded
	out.Problems = append(out.Problems, problems...)
	out.DryRunOnly = o.NotOffered(loaded)
	out.Warnings = append(warnings(loaded, mode, len(out.DryRunOnly) > 0), pastRulesWarnings(ctx, loaded, s)...)

	if len(out.Problems) == 0 {
		out.Sentence = spec.Sentence(loaded, s.StarterNames(ctx))
	}

	return out, nil
}

// Encode returns the draft spec sp as Save stores it, compact JSON in the format's order,
// and the spec it encodes: normalized, at this build's version when it names none. Load,
// and the autopilot package's Save, read a draft's spec as it encodes here.
func Encode(sp spec.Spec) ([]byte, spec.Spec, error) {
	if sp.Version == 0 {
		sp.Version = spec.Version
	}

	normal := spec.Normalize(sp)

	data, err := spec.Encode(normal)

	return data, normal, err
}

// Load reads the draft spec sp as Save stores it (Encode), loaded back as every load
// checks a stored spec: the spec loaded, or, with the spec's own problems, normalized. A
// spec that can't be read at all, as one past the size a spec may be, is an error.
func Load(sp spec.Spec) (spec.Spec, []spec.Problem, error) {
	data, normal, err := Encode(sp)
	if err != nil {
		return normal, nil, err
	}

	loaded, err := spec.Load(data)
	if v, ok := errors.AsType[*spec.ValidationError](err); ok {
		return normal, slices.Clone(v.Problems), nil
	} else if err != nil {
		return normal, nil, uierr.New("The routine can't be read: "+textutil.Sentence(err.Error()), err)
	}

	return loaded, nil, nil
}

// Problems returns the problems Save refuses the loaded spec sp for beyond the spec's own:
// an agent step set to run where this build can't run one (o's places), a background
// agent step whose own agent can't keep its tools preset, the prompts its steps name that
// s's library doesn't have or that don't fit, and the scripts they name that the user no
// longer has under that name. Without a library, or scripts, to read, it finds none of
// theirs. The list is never nil.
func Problems(ctx context.Context, sp spec.Spec, s Setup, o *Offers) []spec.Problem {
	out := []spec.Problem{}
	out = append(out, o.placeProblems(sp)...)
	out = append(out, fenceProblems(sp)...)
	out = append(out, promptProblems(ctx, sp, s)...)

	return append(out, scriptProblems(ctx, sp, s)...)
}
