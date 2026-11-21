// Package facts holds one item's evaluable values: every field a routine's conditions
// can read, and the moments its triggers turn on, each known, unknown, or not applying
// to the item.
//
// Build makes them from a plain Row, which the inbox's query fills from the items, their
// repositories, details, triage, check names, and changed files. Build is pure: it reads
// nothing and keeps no clock, so the moment it is built for comes with it. Every text
// value is made one line of plain text and capped, as a prompt's data is.
//
// A value that isn't known yet (check names not read, changed files not checked, an item's
// details not read) is unknown, with what the engine would read to know it, so a
// condition on it is unknown rather than no. A list read only in part, as GitHub lists
// some of a pull request's files, holds what was read and says it is partial. A field of
// another kind of item, such as a pull request's draft state on an issue, doesn't apply.
//
// It is a leaf: it imports only catalog, item, repo, trust, sensitive, bump, and
// textutil.
package facts

import (
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Need is data the engine can read to make an unknown value known.
type Need string

// The data an unknown value can need.
const (
	// NeedNothing is a value no read can make known, such as a title that doesn't read
	// as a dependency update.
	NeedNothing Need = ""

	// NeedDetails is the item's details: labels, counts, the head commit, the merge state.
	NeedDetails Need = "details"

	// NeedChecks is the head commit's check names, with their completion times.
	NeedChecks Need = "checks"

	// NeedFiles is the pull request's changed files.
	NeedFiles Need = "files"
)

// Status says whether a value is known.
type Status string

// The statuses of a value.
const (
	// StatusKnown is a value that was read.
	StatusKnown Status = "known"

	// StatusUnknown is a value not known yet, or that can't be told.
	StatusUnknown Status = "unknown"

	// StatusNotApplicable is a field of another kind of item.
	StatusNotApplicable Status = "notApplicable"
)

// Value is one field's value for one item.
type Value struct {
	Status Status `json:"status"`

	// Text is a text or a choice field's value; a choice as the catalog spells it.
	Text string `json:"text,omitempty"`

	// Number is a number field's value.
	Number int `json:"number,omitempty"`

	// Bool is a yes-or-no field's value.
	Bool bool `json:"bool,omitempty"`

	// List is a list field's values, without repeats.
	List []string `json:"list,omitempty"`

	// Partial reports a list read only in part: what it holds is so, and there may be
	// more.
	Partial bool `json:"partial,omitempty"`

	// Since is a duration field's moment, which the evaluator measures from.
	Since time.Time `json:"since,omitzero"`

	// Why says why the value isn't known, or why the field doesn't apply.
	Why string `json:"why,omitempty"`

	// Need is what the engine would read to know it.
	Need Need `json:"need,omitempty"`
}

// Known reports a value that was read.
func (v Value) Known() bool {
	return v.Status == StatusKnown
}

// Text returns a known text or choice value.
func Text(s string) Value {
	return Value{Status: StatusKnown, Text: s}
}

// Number returns a known number value.
func Number(n int) Value {
	return Value{Status: StatusKnown, Number: n}
}

// Bool returns a known yes-or-no value.
func Bool(b bool) Value {
	return Value{Status: StatusKnown, Bool: b}
}

// List returns a known list, partial when there may be more than it holds.
func List(values []string, partial bool) Value {
	return Value{Status: StatusKnown, List: textutil.NonNil(values), Partial: partial}
}

// Since returns a known duration field's moment.
func Since(t time.Time) Value {
	return Value{Status: StatusKnown, Since: t}
}

// Unknown returns a value not known yet, why, and what the engine would read to know it.
func Unknown(why string, need Need) Value {
	return Value{Status: StatusUnknown, Why: why, Need: need}
}

// NotApplicable returns a field's value on an item it doesn't apply to.
func NotApplicable(why string) Value {
	return Value{Status: StatusNotApplicable, Why: why}
}

// Item names the item the facts are about.
type Item struct {
	ID     int64     `json:"id"`
	Kind   item.Kind `json:"kind"`
	Repo   string    `json:"repo"`
	Number int       `json:"number"`
	Title  string    `json:"title"`
	URL    string    `json:"url"`
}

// Facts are one item's evaluable values.
type Facts struct {
	Item Item `json:"item"`

	// Values are the fields' values, by field.
	Values map[catalog.FieldID]Value `json:"values"`

	// Head is the pull request's head commit, which most triggers fire once per.
	Head Value `json:"head"`

	// ChecksDone is when the head commit's last check finished, known once its check
	// names are read and every check has finished.
	ChecksDone Value `json:"checksDone"`

	// ReviewAsked and Assigned are when the inbox last saw a request for the user's
	// review, and an assignment to the user, arrive; unknown when it doesn't say.
	ReviewAsked Value `json:"reviewAsked"`
	Assigned    Value `json:"assigned"`

	// Updated is the item's last activity.
	Updated time.Time `json:"updated"`

	// DetailsAt is when the item's details were last read, zero before they were.
	DetailsAt time.Time `json:"detailsAt,omitzero"`
}

// Value returns a field's value: unknown for a field Build didn't fill, such as the
// routine's own history, which the evaluator reads from the ledger.
func (f Facts) Value(id catalog.FieldID) Value {
	if v, ok := f.Values[id]; ok {
		return v
	}

	return Unknown("not read", NeedNothing)
}
