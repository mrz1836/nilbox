package spec

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Caps on a spec's lists and texts.
const (
	MaxSources    = 10
	MaxSearches   = 5
	MaxConditions = 50
	MaxValues     = 50
	MaxSteps      = 10
	MaxActions    = 4
	MaxLabels     = 20
	MaxLaunchRows = 50
	MaxCommands   = 50
	MaxRequires   = 20

	// MaxTextRunes caps a condition's value, a command, a note, and a setting's name.
	MaxTextRunes = 200

	// MaxNicknameRunes is the longest nickname a prompt can have.
	MaxNicknameRunes = 20

	// maxNumber caps a number condition's value.
	maxNumber = 1_000_000_000
)

// ErrInvalid is what every *ValidationError wraps.
var ErrInvalid = errors.New("the routine isn't valid")

// Part is one of a spec's parts, as a problem names it.
type Part string

// The parts of a spec.
const (
	PartVersion    Part = "version"
	PartWatch      Part = "watch"
	PartTrigger    Part = "trigger"
	PartConditions Part = "conditions"
	PartSteps      Part = "steps"
	PartLimits     Part = "limits"
	PartNotify     Part = "notify"
	PartRequires   Part = "requires"
)

// Problem is one thing wrong with a spec, for the builder to show where it is.
type Problem struct {
	// Part is the part it is in.
	Part Part `json:"part"`

	// Field is where in the part, as the spec spells it: "sources[0].search.where",
	// "of[2].values", "[1].onlyIf"; "" for the part as a whole.
	Field string `json:"field"`

	// Message says what is wrong, and what to do.
	Message string `json:"message"`
}

// Path is where the problem is in the whole spec: "watch.sources[0].search.where".
func (p Problem) Path() string {
	switch {
	case p.Field == "":
		return string(p.Part)
	case strings.HasPrefix(p.Field, "["):
		return string(p.Part) + p.Field
	default:
		return string(p.Part) + "." + p.Field
	}
}

// ValidationError is every problem Validate found.
type ValidationError struct {
	Problems []Problem
}

// Error names the first problem, and how many more there are.
func (e *ValidationError) Error() string {
	first := e.Problems[0]
	msg := first.Path() + ": " + first.Message

	if more := len(e.Problems) - 1; more > 0 {
		msg += " (and " + textutil.Counted(more, "more problem", "more problems") + ")"
	}

	return msg
}

// Unwrap returns ErrInvalid.
func (e *ValidationError) Unwrap() error {
	return ErrInvalid
}

// Validate checks a normalized spec, returning a *ValidationError with every problem it
// finds, or nil. It holds every value to its range, every name to its closed set
// (ClosedSets) or the catalog, every search to its rules (scoped, quoted, short), and the
// steps to their order:
//
//   - one agent step at most;
//   - Wait for the checks only after an agent step that may push, and never only if it
//     didn't push;
//   - Merge it only after a Wait for the checks that runs only if the agent pushed, so
//     the pass it follows is on the agent's own commit, and only if the checks passed;
//   - Post the agent's review only after a read-only agent step;
//   - Allow approving only when the review is posted as its verdict;
//   - only a notice after an agent step in the terminal, whose end the app can't see;
//   - a schedule's digest only with Notify me as the first step.
func Validate(s Spec) error {
	v := &validator{}

	if s.Version != Version {
		v.add(PartVersion, "", "this build reads version %d", Version)
	}

	kinds := v.watch(s.Watch, s.Trigger.Event)
	v.kinds = kinds
	v.trigger(s.Trigger, kinds)
	v.conditions(s.Conditions, kinds)
	v.steps(s.Steps, kinds)
	v.digest(s.Trigger, s.Steps)
	v.limits(s.Limits)
	v.requires(s.Requires)

	if len(v.problems) > 0 {
		return &ValidationError{Problems: v.problems}
	}

	return nil
}

// validator collects a spec's problems.
type validator struct {
	problems []Problem

	// kinds are the kinds of item the watch holds.
	kinds []item.Kind
}

// add records a problem.
func (v *validator) add(part Part, field, format string, args ...any) {
	v.problems = append(v.problems, Problem{Part: part, Field: field, Message: fmt.Sprintf(format, args...)})
}

// watch checks the sources and returns the kinds of item they hold.
func (v *validator) watch(w Watch, event catalog.Event) []item.Kind {
	switch {
	case len(w.Sources) == 0 && event != catalog.EventManual:
		v.add(PartWatch, "sources", "watch your inbox or a search, so the routine has items to look at")
	case len(w.Sources) > MaxSources:
		v.add(PartWatch, "sources", "watch at most %d sources", MaxSources)
	}

	searches := 0

	for i, src := range w.Sources {
		field := fmt.Sprintf("sources[%d]", i)

		switch {
		case src.Search != nil && src.Lists != nil:
			v.add(PartWatch, field, "a source is your inbox or a search, not both")
		case src.Search != nil:
			searches++

			v.search(field+".search", *src.Search)
		default:
			v.lists(field+".lists", src.Lists)
		}
	}

	if searches > MaxSearches {
		v.add(PartWatch, "sources", "use at most %d searches in one routine", MaxSearches)
	}

	return w.Kinds()
}

// lists checks a source's inbox reasons.
func (v *validator) lists(field string, lists []catalog.ListSource) {
	if len(lists) == 0 {
		v.add(PartWatch, field, "pick at least one reason an item is in your inbox")
	}

	for i, l := range lists {
		if !member(l) {
			v.add(PartWatch, fmt.Sprintf("%s[%d]", field, i), "%q isn't a reason an item is in your inbox", l)
		}
	}
}

// search checks a source's search against the search's rules.
func (v *validator) search(field string, s watch.Search) {
	for _, fe := range watch.Check(s) {
		v.add(PartWatch, field+"."+fe.Field, "%s", fe.Err.Error())
	}
}

// texts checks a list of free texts: at most max, each one line of at most
// MaxTextRunes.
func (v *validator) texts(part Part, field string, values []string, limit int) {
	if len(values) > limit {
		v.add(part, field, "at most %d", limit)
	}

	for i, s := range values {
		if msg := checkText(s, MaxTextRunes); msg != "" {
			v.add(part, fmt.Sprintf("%s[%d]", field, i), "%s", msg)
		}
	}
}

// checkText says what is wrong with a one-line text, or "".
func checkText(s string, limit int) string {
	switch {
	case s == "":
		return "it's empty"
	case utf8.RuneCountInString(s) > limit:
		return fmt.Sprintf("keep it to %d characters", limit)
	case strings.ContainsFunc(s, unicode.IsControl) || !utf8.ValidString(s):
		return "keep it to one line of plain text"
	default:
		return ""
	}
}

// requires checks the tools the routine needs.
func (v *validator) requires(tools []string) {
	if len(tools) > MaxRequires {
		v.add(PartRequires, "", "list at most %d tools", MaxRequires)
	}

	for i, t := range tools {
		if !toolName.MatchString(t) {
			v.add(PartRequires, fmt.Sprintf("[%d]", i), "%q isn't a program's name", t)
		}
	}
}

// fits reports whether kinds holds one the applies func takes, or kinds is empty.
func fits(kinds []item.Kind, applies func(item.Kind) bool) bool {
	if len(kinds) == 0 {
		return true
	}

	return slices.ContainsFunc(kinds, applies)
}

// kindWords names the kinds a watch holds: "issues", "pull requests".
func kindWords(kinds []item.Kind) string {
	if len(kinds) == 1 {
		return kinds[0].Plural()
	}

	return "pull requests and issues"
}
