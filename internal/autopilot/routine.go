package autopilot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The caps of a routine's words, which the draft leaf checks (draft.CheckName,
// draft.CheckDescription).
const (
	// MaxNameRunes caps a routine's name.
	MaxNameRunes = draft.MaxNameRunes

	// MaxDescriptionRunes caps a routine's description.
	MaxDescriptionRunes = draft.MaxDescriptionRunes
)

// Routine is one routine as the window shows it.
type Routine struct {
	// ID names it on this Mac.
	ID int64 `json:"id"`

	// UUID names it for good: it never changes once given, and travels with the routine in
	// an export and a backup, so a backup brought back finds the routine it was, whatever
	// it is called now.
	UUID string `json:"uuid"`

	// Name and Description are the user's words for it.
	Name        string `json:"name"`
	Description string `json:"description"`

	// Enabled reports a routine switched on, and EnabledAt when it was, nil while it is
	// off.
	Enabled   bool       `json:"enabled"`
	EnabledAt *time.Time `json:"enabledAt"`

	// Mode is how it acts on a match: Dry run, Ask first, or Autopilot.
	Mode autopilotprefs.Mode `json:"mode"`

	// Spec is its definition, as far as it could be read: empty for a broken routine that
	// can't be read at all.
	Spec spec.Spec `json:"spec"`

	// Sentence says the spec in one sentence; "" for a broken routine.
	Sentence string `json:"sentence"`

	// TemplateKey names the template it was made from, "" for none.
	TemplateKey string `json:"templateKey"`

	// Account is the GitHub account it belongs to, "" for the account signed in, and
	// OtherAccount reports one that belongs to another account than the one signed in,
	// which doesn't run.
	Account      string `json:"account"`
	OtherAccount bool   `json:"otherAccount"`

	// BrokenReason says why its spec won't load, "" when it does, Broken says so in the
	// window's words, and Problems are a spec that no longer validates' problems.
	BrokenReason spec.Reason    `json:"brokenReason"`
	Broken       string         `json:"broken"`
	Problems     []spec.Problem `json:"problems"`

	// DryRunOnly are the kinds of step it has that this build can't run yet, so it runs
	// in Dry run alone; empty when it can run in any mode.
	DryRunOnly []catalog.StepKind `json:"dryRunOnly"`

	// LastError is the last error it met, in the window's words, "" for none.
	LastError string `json:"lastError"`

	// PastRules is how it stands on merging past the rules: never, accepted as it is, or
	// waiting for the user to accept it as it is now (pastrules.go).
	PastRules PastRules `json:"pastRules"`

	// Figures are what its ledger says of it, for its line in the list: filled in by List
	// alone, zero elsewhere.
	Figures RoutineFigures `json:"figures"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// RoutineFigures are what a routine's matches that did something or wait to (never a
// baseline, nor one that closed as its item or the routine moved on) say of it: how many
// it recorded today, from local midnight, real ones and a Dry run's apart, how many wait
// for the user, and when it last ran (or would have, in Dry run): when the match that ran
// last closed, as it ran, and whether that was a Dry run's, nil while none has.
type RoutineFigures struct {
	Today         int        `json:"today"`
	WouldRunToday int        `json:"wouldRunToday"`
	Waiting       int        `json:"waiting"`
	LastAt        *time.Time `json:"lastAt"`
	LastDryRun    bool       `json:"lastDryRun"`

	// Graduate reports a routine in Ask first whose last GraduateAfter approved runs all
	// finished cleanly, which offers to go on Autopilot: a suggestion, never a change.
	Graduate bool `json:"graduate"`
}

// RoutineName is a routine's name by its ID, a deleted one's included, for the window's
// marks and history that name it.
type RoutineName struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

// Draft is a routine as the builder saves it.
type Draft struct {
	// ID is the routine saved over, 0 (or left out) for a new one.
	ID int64 `json:"id,omitempty"`

	// Name and Description are the user's words for it.
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// Mode is how it acts on a match: left out, a new routine starts in the mode Settings
	// says new routines start in, and one saved before keeps its own.
	Mode autopilotprefs.Mode `json:"mode,omitempty"`

	// Spec is its definition; a version of 0 is this build's.
	Spec spec.Spec `json:"spec"`

	// TemplateKey names the template a new routine is made from, left out for none; a
	// routine saved before keeps its own.
	TemplateKey string `json:"templateKey,omitempty"`

	// Current is what to do with the items the routine matches now that it has no record
	// of, when it is on and its Watch, When, If, or Once per changed; left out, it runs on
	// them too.
	Current Current `json:"current,omitempty"`

	// AcceptPastRules is the user's confirmation that the routine's Merge it may go past
	// the rules, as its step allows: a spec that allows it is saved only with it, or as the
	// very spec text they accepted before (pastrules.go).
	AcceptPastRules bool `json:"acceptPastRules,omitempty"`
}

// loaded is a stored spec as a load read it.
type loaded struct {
	spec   spec.Spec
	broken *spec.BrokenError
}

// load reads a stored spec. One that won't load is kept as far as it was read, normalized
// so its lists are never nil, with why.
func load(data string) loaded {
	s, err := spec.Load([]byte(data))
	out := loaded{spec: spec.Normalize(s)}

	if b, ok := errors.AsType[*spec.BrokenError](err); ok {
		out.broken = b
	} else if err != nil {
		out.broken = &spec.BrokenError{Reason: spec.ReasonUnreadable, Err: err}
	}

	return out
}

// loadedSpec returns a stored spec, and whether it loads.
func loadedSpec(data string) (spec.Spec, bool) {
	l := load(data)

	return l.spec, l.broken == nil
}

// view is what a Routine is built with beside its row: the account signed in, and the
// starter prompts' names.
type view struct {
	viewer string
	names  spec.PromptNames
	offers *Offers
}

// routineOf builds the window's Routine from a stored row.
func routineOf(row queries.AutopilotRoutine, v view) Routine {
	l := load(row.Spec)

	out := Routine{
		ID: row.ID, UUID: row.UUID, Name: row.Name, Description: row.Description, Enabled: row.Enabled, EnabledAt: row.EnabledAt,
		Mode: storedMode(row.Mode), Spec: l.spec, TemplateKey: row.TemplateKey, Account: row.AccountLogin,
		OtherAccount: otherAccount(row.AccountLogin, v.viewer), Problems: []spec.Problem{},
		DryRunOnly: []catalog.StepKind{}, LastError: row.LastError, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}

	if l.broken != nil {
		out.BrokenReason = l.broken.Reason
		out.Broken = brokenWords(l.broken)

		if invalid, ok := errors.AsType[*spec.ValidationError](l.broken); ok {
			out.Problems = slices.Clone(invalid.Problems)
		}

		return out
	}

	out.Sentence = spec.Sentence(l.spec, v.names)
	out.DryRunOnly = v.offers.NotOffered(l.spec)
	out.PastRules = pastRulesOf(row, l.spec)

	return out
}

// storedMode reads a stored mode: as stored when it is one of the modes, and otherwise,
// as a mode a newer build added, Dry run.
func storedMode(stored string) autopilotprefs.Mode {
	if m := autopilotprefs.Mode(stored); m.Valid() {
		return m
	}

	return autopilotprefs.ModeDryRun
}

// otherAccount reports a routine of account that belongs to another account than viewer,
// the one signed in. A routine of no account, or with no account signed in, is no
// other's.
func otherAccount(account, viewer string) bool {
	return account != "" && viewer != "" && !strings.EqualFold(account, viewer)
}

// brokenWords says why a routine is broken, in the window's words.
func brokenWords(b *spec.BrokenError) string {
	switch b.Reason {
	case spec.ReasonNewer:
		return "Made by a newer " + brand.Name + ": update to edit it."
	case spec.ReasonUnreadable:
		return "Its definition can't be read."
	case spec.ReasonInvalid:
		return textutil.Sentence("It isn't valid: " + b.Err.Error())
	default:
		return textutil.Sentence(b.Err.Error())
	}
}

// Offers names the kinds of step, the GitHub actions, and the places of an agent a build
// runs: the draft leaf's own type, whose checks read it. Nil is the catalog's own.
type Offers = draft.Offers

// dryRunOnlyError says a routine with steps of kinds this build can't run yet runs in Dry
// run alone.
func dryRunOnlyError(kinds []catalog.StepKind) error {
	labels := make([]string, 0, len(kinds))

	for _, k := range kinds {
		if info, ok := catalog.StepByKind(k); ok {
			labels = append(labels, "“"+info.Label+"”")
		}
	}

	msg := fmt.Sprintf("This routine runs in Dry run for now: this version of %s can't yet run %s.", brand.Name,
		draft.Series(labels))

	return uierr.New(msg, fmt.Errorf("%w: %s", ErrDryRunOnly, textutil.Join(kinds, ", ")))
}

// checkMode refuses a mode that isn't one of the modes, "" among them.
func checkMode(mode autopilotprefs.Mode) error {
	if mode.Valid() {
		return nil
	}

	return uierr.New("That isn't one of the modes: choose Dry run, Ask first, or Autopilot.",
		fmt.Errorf("%w %q", autopilotprefs.ErrUnknownMode, mode))
}

// ProblemsError is a spec the builder saved that isn't valid, with every problem, which
// the window shows where each one is.
type ProblemsError struct {
	err *spec.ValidationError
}

// Error names the first problem.
func (e *ProblemsError) Error() string {
	return e.err.Error()
}

// Unwrap returns the validation error.
func (e *ProblemsError) Unwrap() error {
	return e.err
}

// Detail is what the window needs to show each problem where it is: {"problems": [...]}.
func (e *ProblemsError) Detail() any {
	return map[string][]spec.Problem{"problems": slices.Clone(e.err.Problems)}
}

// checkSpec returns s as stored, compact JSON in the format's order (draft.Encode, as
// Describe reads it), and s normalized, refusing one that won't load: the window is told
// each problem.
func checkSpec(s spec.Spec) ([]byte, spec.Spec, error) {
	data, _, err := draft.Encode(s)
	if err != nil {
		return nil, spec.Spec{}, err
	}

	loadedSpec, err := spec.Load(data)
	if err == nil {
		return data, loadedSpec, nil
	}

	if v, ok := errors.AsType[*spec.ValidationError](err); ok {
		msg := textutil.Sentence("The routine isn't valid yet: " + v.Problems[0].Message)

		return nil, spec.Spec{}, uierr.New(msg, &ProblemsError{err: v})
	}

	return nil, spec.Spec{}, uierr.New("The routine can't be saved: "+textutil.Sentence(err.Error()), err)
}

// storedSpec is a spec as a restore or an import stores it: normalized as Save stores one
// when it loads, else compact as it was, with its version and why it won't load.
type storedSpec struct {
	data    string
	version int64
	broken  spec.Reason
	spec    spec.Spec
	loads   bool
	refusal error
}

// keepSpec reads raw, a spec brought back or imported, for storing, refusing what is no
// JSON object: one that loads is stored normalized, encoded as Save encodes one, and one
// that doesn't (as a newer build's) compacted but as it was, never rewritten, with the
// version it says and why it won't load.
func keepSpec(raw json.RawMessage) (storedSpec, error) {
	var buf bytes.Buffer

	if err := json.Compact(&buf, raw); err != nil || buf.Len() == 0 || buf.Bytes()[0] != '{' {
		return storedSpec{}, fmt.Errorf("%w: its definition isn't a JSON object", spec.ErrUnreadable)
	}

	if buf.Len() > spec.MaxBytes {
		return storedSpec{}, fmt.Errorf("%w: larger than %d bytes", spec.ErrUnreadable, spec.MaxBytes)
	}

	l := load(buf.String())
	out := storedSpec{data: buf.String(), version: peekVersion(buf.Bytes()), spec: l.spec, loads: l.broken == nil}

	if out.loads {
		data, err := spec.Encode(l.spec)
		if err != nil {
			return storedSpec{}, err
		}

		out.data, out.version = string(data), int64(l.spec.Version)

		return out, nil
	}

	// A spec that won't load is the routine's all the same: it is kept as it was, broken.
	out.broken, out.refusal = l.broken.Reason, l.broken

	return out, nil
}

// peekVersion reads only a spec's version, 0 when it has none.
func peekVersion(data []byte) int64 {
	var head struct {
		Version json.RawMessage `json:"version"`
	}

	if json.Unmarshal(data, &head) != nil {
		return 0
	}

	v, err := strconv.ParseInt(string(head.Version), 10, 64)
	if err != nil || v < 0 {
		return 0
	}

	return v
}

// catalogMode returns mode, or Dry run for a spec with a step the catalog doesn't offer
// yet: what a restore or an import stores.
func catalogMode(mode autopilotprefs.Mode, s spec.Spec) autopilotprefs.Mode {
	var catalogs *Offers

	return catalogs.ModeFor(mode, s)
}
