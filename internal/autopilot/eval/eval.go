// Package eval decides whether a routine matches an item: its trigger, with the fire key
// that makes it act once per occurrence, and its conditions, each with the reason it
// held or didn't.
//
// Evaluate is pure: no I/O and no clock of its own, so the moment it decides for comes
// with it. What it works out from the routine alone, Prepare works out once, for a cycle
// that decides one routine on many items. It decides in three values, as the builder
// promises:
//
//   - a condition on data not known yet (check names not read, files not checked) is
//     unknown, and so is its negation: "a failed check's name doesn't contain X" is never
//     yes before the names are read;
//   - "all of these" is yes only when every condition is yes, and no as soon as one is
//     no; "any of these" is yes as soon as one is yes;
//   - a routine acts only on a definite yes. An unknown decision says what is missing
//     (Needs), so the engine can read it and decide again.
//
// Conditions run cheapest first, and a list stops at its first decisive condition, but
// the order is worked out from what each condition says, never from where it sits, so
// shuffling a routine's conditions never changes its decision. Text compares ignore case,
// and a regex is Go's RE2, so matching can't run away.
//
// It is a leaf: it imports only spec, catalog, facts, sensitive, and textutil.
package eval

import (
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// Result is a three-valued decision: yes, no, or unknown.
type Result string

// The results.
const (
	Yes     Result = "yes"
	No      Result = "no"
	Unknown Result = "unknown"
)

// Not negates a result: unknown stays unknown.
func (r Result) Not() Result {
	switch r {
	case Yes:
		return No
	case No:
		return Yes
	case Unknown:
		return Unknown
	}

	return Unknown
}

// All is yes when every result is, no as soon as one is no, and else unknown; yes for
// none.
func All(results ...Result) Result {
	out := Yes

	for _, r := range results {
		switch r {
		case No:
			return No
		case Unknown:
			out = Unknown
		case Yes:
		}
	}

	return out
}

// Any is yes as soon as one result is, no when every one is no, and else unknown; no
// for none.
func Any(results ...Result) Result {
	out := No

	for _, r := range results {
		switch r {
		case Yes:
			return Yes
		case Unknown:
			out = Unknown
		case No:
		}
	}

	return out
}

// History is what the routine's ledger knows about the item.
type History struct {
	// Tries counts the routine's runs on the item since the last Try again.
	Tries int `json:"tries"`

	// LastOutcome is how its last run on the item ended, as the catalog's choices name
	// it; "" for none.
	LastOutcome string `json:"lastOutcome"`

	// FirstHead is the head commit the routine first saw the item at, "" before it saw
	// it.
	FirstHead string `json:"firstHead"`

	// OwnHeads are the head commits routines' own runs pushed to the item.
	OwnHeads []string `json:"ownHeads"`

	// Working reports a routine's agent working on the item now: a commit it pushes is
	// told apart from another's only once its run ends and records it.
	Working bool `json:"working,omitempty"`

	// CommentsSeen is the comment count the routine last fired at, or first saw the item
	// with; nil before it saw it.
	CommentsSeen *int `json:"commentsSeen"`

	// Manual is a click's token when the user runs the routine on the item by hand,
	// "" otherwise.
	Manual string `json:"manual"`
}

// Trace is one step of a decision: the trigger, or one condition.
type Trace struct {
	// Path is where it is in the spec: "trigger", "conditions.of[2]",
	// "conditions.of[1].group.of[0]".
	Path string `json:"path"`

	// Condition says what was checked, as the routine's sentence says it.
	Condition string `json:"condition"`

	// Result is what it came to.
	Result Result `json:"result"`

	// Actual says the value it compared, or why it isn't known.
	Actual string `json:"actual"`

	// Need is what the engine would read to know it, for an unknown result.
	Need facts.Need `json:"need,omitempty"`
}

// Decision is whether a routine matches an item.
type Decision struct {
	// Match is the decision: the trigger and the conditions together.
	Match Result `json:"match"`

	// FireKey is the occurrence the trigger fired for, which makes the routine act
	// once per it; "" while the trigger doesn't hold.
	FireKey string `json:"fireKey"`

	// Trace holds the trigger's step first, then each condition evaluated, in the order
	// they ran.
	Trace []Trace `json:"trace"`

	// Needs are the data still missing, for an unknown decision: reading them may
	// decide it.
	Needs []facts.Need `json:"needs"`

	// Pending is the trigger on its way, for a decision whose trigger doesn't hold yet but
	// will without anyone doing anything (checks still running, settling, a schedule due
	// later today); nil for any other, a trigger that waits on an event no one can foresee
	// (a comment, a label, an approval) among them. The conditions aren't decided with it:
	// Plan.Conditions decides them.
	Pending *Pending `json:"pending,omitempty"`
}

// Pending is a trigger on its way: when it holds, and the occurrence it fires for then.
type Pending struct {
	// When says when it holds, as a sentence ends: "when its checks pass", "once its
	// checks settle", "at 09:00".
	When string `json:"when"`

	// At is when it holds, when the clock alone decides it (settling, a schedule, a quiet
	// spell); nil while it waits on GitHub (checks still running).
	At *time.Time `json:"at"`

	// FireKey is the occurrence it fires for then, as the decision's FireKey will be, which
	// tells a routine whether it handled that occurrence already.
	FireKey string `json:"fireKey"`
}

// Evaluate decides whether s matches the item f describes at now, with what the
// routine's ledger knows of it. s is a normalized, valid spec, as spec.Load returns it.
// It prepares s for this one decision: a caller deciding s on many items prepares it once
// (Prepare) and decides with the plan.
func Evaluate(s spec.Spec, f facts.Facts, h History, now time.Time) Decision {
	return Prepare(s).Evaluate(f, h, now)
}

// ordered returns needs without repeats or nothing, in the order the facts name them.
func ordered(needs []facts.Need) []facts.Need {
	out := []facts.Need{}

	for _, need := range []facts.Need{facts.NeedDetails, facts.NeedChecks, facts.NeedFiles} {
		if slices.Contains(needs, need) {
			out = append(out, need)
		}
	}

	return out
}
