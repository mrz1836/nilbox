package autopilot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

const (
	// MaxNameRunes caps a routine's name.
	MaxNameRunes = 80

	// MaxDescriptionRunes caps a routine's description.
	MaxDescriptionRunes = 500

	// maxTemplateKey caps a template's key.
	maxTemplateKey = 64
)

// templateKey is what a template's key looks like: "fix-failing-checks-on-my-prs".
var templateKey = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Routine is one routine as the window shows it.
type Routine struct {
	// ID names it.
	ID int64 `json:"id"`

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
		ID: row.ID, Name: row.Name, Description: row.Description, Enabled: row.Enabled, EnabledAt: row.EnabledAt,
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
	out.DryRunOnly = v.offers.notOffered(l.spec)

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

// Offers names the kinds of step, and the GitHub actions, a build runs: by default the
// catalog's own (Step.Offered, GitHubAction.Offered), which each part's phase turns on
// once its dispatcher is built. A test hands in its own, with fakes for the dispatchers,
// never by changing the catalog.
type Offers struct {
	Steps   []catalog.StepKind
	Actions []catalog.GitHubActionKind

	// Places are where this build runs an agent step (catalog.PlaceInfo.Offered). A step
	// set to run anywhere else is a problem the builder shows (placeProblems), so it is
	// never saved; and one a routine holds from before keeps its runs from starting, as
	// this Mac isn't ready for it (readiness).
	Places []catalog.Where
}

// catalogOffers returns the catalog's own offers.
func catalogOffers() *Offers {
	out := &Offers{}

	for _, info := range catalog.Steps() {
		if info.Offered {
			out.Steps = append(out.Steps, info.Kind)
		}
	}

	for _, a := range catalog.GitHubActions() {
		if a.Offered {
			out.Actions = append(out.Actions, a.Kind)
		}
	}

	for _, p := range catalog.Places() {
		if p.Offered {
			out.Places = append(out.Places, p.ID)
		}
	}

	return out
}

// runsAt reports whether this build runs an agent step where place says.
func (o *Offers) runsAt(place catalog.Where) bool {
	return slices.Contains(o.orCatalog().Places, place)
}

// placeProblems returns a problem for each agent step of s set to run where this build
// can't run an agent, at its where field: plain words in the builder, which keep the
// routine from being saved as it is.
func (o *Offers) placeProblems(s spec.Spec) []spec.Problem {
	var out []spec.Problem

	for i, st := range s.Steps {
		if st.Agent == nil || o.runsAt(st.Agent.Place()) {
			continue
		}

		info, _ := catalog.PlaceByID(st.Agent.Place())
		out = append(out, spec.Problem{
			Part: spec.PartSteps, Field: fmt.Sprintf("[%d].agent.where", i),
			Message: fmt.Sprintf("this version of %s can't run an agent %s yet: choose %s", brand.Name,
				strings.ToLower(info.Label), o.placeWords()),
		})
	}

	return out
}

// placeWords names the places this build runs an agent in: "In my terminal".
func (o *Offers) placeWords() string {
	var words []string

	for _, p := range catalog.Places() {
		if o.runsAt(p.ID) {
			words = append(words, p.Label)
		}
	}

	return series(words)
}

// orCatalog returns o, or the catalog's offers for nil.
func (o *Offers) orCatalog() *Offers {
	if o == nil {
		return catalogOffers()
	}

	return o
}

// notOffered returns the kinds of step s has that this build can't run yet, in the
// catalog's order: a step whose kind isn't offered, and a GitHub step with an action
// that isn't.
func (o *Offers) notOffered(s spec.Spec) []catalog.StepKind {
	out := []catalog.StepKind{}

	for _, info := range catalog.Steps() {
		for _, st := range s.Steps {
			if st.Kind() == info.Kind && !o.runs(st) {
				out = append(out, info.Kind)

				break
			}
		}
	}

	return out
}

// runs reports whether this build runs st.
func (o *Offers) runs(st spec.Step) bool {
	off := o.orCatalog()

	if !slices.Contains(off.Steps, st.Kind()) {
		return false
	}

	for _, a := range st.GitHub {
		if !slices.Contains(off.Actions, a.Kind()) {
			return false
		}
	}

	return true
}

// modeFor returns mode, or Dry run for a spec with a step this build can't run yet.
func (o *Offers) modeFor(mode autopilotprefs.Mode, s spec.Spec) autopilotprefs.Mode {
	if len(o.notOffered(s)) > 0 {
		return autopilotprefs.ModeDryRun
	}

	return mode
}

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
		series(labels))

	return uierr.New(msg, fmt.Errorf("%w: %s", ErrDryRunOnly, textutil.Join(kinds, ", ")))
}

// series joins words as a sentence lists them: "a", "a and b", "a, b, and c".
func series(words []string) string {
	switch len(words) {
	case 0:
		return "its steps"
	case 1:
		return words[0]
	case 2:
		return words[0] + " and " + words[1]
	default:
		return strings.Join(words[:len(words)-1], ", ") + ", and " + words[len(words)-1]
	}
}

// checkName returns a routine's name, trimmed, refusing one that is empty, longer than
// MaxNameRunes, or not one line of plain text.
func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)

	switch {
	case name == "":
		return "", uierr.New("Give the routine a name.", ErrInvalidName)
	case utf8.RuneCountInString(name) > MaxNameRunes:
		return "", uierr.New(fmt.Sprintf("Keep the name to %d characters.", MaxNameRunes),
			fmt.Errorf("%w: a long name", ErrInvalidName))
	case !utf8.ValidString(name) || strings.ContainsFunc(name, hidden):
		return "", uierr.New("Keep the name to one line of plain text.",
			fmt.Errorf("%w: a hidden character", ErrInvalidName))
	}

	return name, nil
}

// checkDescription returns a routine's description, trimmed, refusing one longer than
// MaxDescriptionRunes or holding a control character but a line break.
func checkDescription(desc string) (string, error) {
	desc = strings.TrimSpace(desc)

	switch {
	case utf8.RuneCountInString(desc) > MaxDescriptionRunes:
		return "", uierr.New(fmt.Sprintf("Keep the description to %d characters.", MaxDescriptionRunes),
			fmt.Errorf("%w: a long description", ErrInvalidDescription))
	case !utf8.ValidString(desc) || strings.ContainsFunc(desc, func(r rune) bool { return r != '\n' && hidden(r) }):
		return "", uierr.New("Keep the description to plain text.",
			fmt.Errorf("%w: a hidden character", ErrInvalidDescription))
	}

	return desc, nil
}

// checkTemplate returns a template's key, trimmed, refusing one that isn't one.
func checkTemplate(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || (len(key) <= maxTemplateKey && templateKey.MatchString(key)) {
		return key, nil
	}

	return "", uierr.New("That isn't one of the templates.", fmt.Errorf("%w %q", ErrInvalidTemplate, key))
}

// hidden reports a rune that can't be shown in a line of text: a control character, or
// one that changes the text's direction.
func hidden(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r)
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

// checkSpec returns s as stored, compact JSON in the format's order, and s normalized,
// refusing one that won't load: the window is told each problem.
func checkSpec(s spec.Spec) ([]byte, spec.Spec, error) {
	if s.Version == 0 {
		s.Version = spec.Version
	}

	data, err := spec.Encode(spec.Normalize(s))
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

	return catalogs.modeFor(mode, s)
}
