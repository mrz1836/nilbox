package autopilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Export and import: routines leave the app as a bundle in the export format (Bundle),
// one routine or all of them, with the text of the prompts they name by nickname when the
// user asks for it, and come in from one. An import says first what would arrive (each
// routine's sentence, the prompts it brings, and the prompts, profiles, and agents its
// routines name that this Mac lacks, each for the user to map to one it has or to
// inherit), then brings the routines in switched off, after the prompts with no match by
// nickname. A bundle holds org and repository names, so it is the user's to share or not;
// the app never sends it anywhere.

var (
	// ErrNoPath is returned for a file to export to or import from that isn't an absolute
	// path.
	ErrNoPath = errors.New("choose a file")

	// ErrChanged is returned by Import for a file that changed since its preview.
	ErrChanged = errors.New("the file changed since it was read")

	// ErrNothingToExport is returned by Export when no routine is left to write.
	ErrNothingToExport = errors.New("no routine to export")
)

// PromptTransfer carries the text of prompts, in the prompts' own file format, for an
// export and an import. Core adapts the prompt library.
type PromptTransfer interface {
	// ExportPrompts returns the library's prompts with nicknames (ignoring case), in the
	// prompts' file format, and how many it holds; none found is nil and 0.
	ExportPrompts(ctx context.Context, nicknames []string) (json.RawMessage, int, error)

	// PreviewPrompts says what bringing in each of raw's prompts would do.
	PreviewPrompts(ctx context.Context, raw json.RawMessage) ([]PromptArrival, error)

	// ImportPrompts adds raw's prompts whose nickname no prompt of the library has, and
	// returns the nicknames it added.
	ImportPrompts(ctx context.Context, raw json.RawMessage) ([]string, error)
}

// Profiles lists what this Mac runs agents with. Core adapts the profiles.
type Profiles interface {
	// Profiles returns each agent whose program this Mac has, by ID, with the names of
	// its profiles.
	Profiles(ctx context.Context) (map[string][]string, error)
}

// Arrives says what bringing in one of a bundle's prompts would do.
type Arrives string

// What a bundle's prompt does on arrival.
const (
	// ArrivesNew: no prompt here has its nickname, so it is added.
	ArrivesNew Arrives = "new"

	// ArrivesSame: the prompt here with its nickname is the same, so nothing changes.
	ArrivesSame Arrives = "same"

	// ArrivesDiffers: the prompt here with its nickname differs, and the one here is kept.
	ArrivesDiffers Arrives = "differs"
)

// PromptArrival is what bringing in one of a bundle's prompts would do.
type PromptArrival struct {
	Nickname string  `json:"nickname"`
	Arrives  Arrives `json:"arrives"`
}

// Exported says what Export wrote.
type Exported struct {
	Routines int `json:"routines"`
	Prompts  int `json:"prompts"`
}

// Export writes the routines with ids, or every routine when ids is empty, in the export
// format to the file at path, replacing any file there, with the text of the prompts they
// name by nickname when withPrompts. A starter prompt travels by its key, as every Mac
// has it already.
func (s *Service) Export(ctx context.Context, path string, ids []int64, withPrompts bool) (Exported, error) {
	if path == "" || !filepath.IsAbs(path) {
		return Exported{}, uierr.New("Choose where to save the routines.", ErrNoPath)
	}

	rows, err := s.exportRows(ctx, ids)
	if err != nil {
		return Exported{}, err
	}

	b := bundleOf(rows, s.app, s.clock.Now())
	if len(b.Routines) == 0 {
		return Exported{}, uierr.New("There's no routine to export.", ErrNothingToExport)
	}

	out := Exported{Routines: len(b.Routines)}

	if b.Prompts, out.Prompts, err = s.exportPrompts(ctx, b, withPrompts); err != nil {
		return Exported{}, err
	}

	data, err := EncodeBundle(b)
	if err != nil {
		return Exported{}, err
	}

	if err = appdata.WriteFile(path, data); err != nil {
		return Exported{}, uierr.New("The routines couldn't be saved there.", fmt.Errorf("export the routines: %w", err))
	}

	return out, nil
}

// exportRows returns the routines with ids, in the user's order, or every routine for none,
// refusing an ID that is gone.
func (s *Service) exportRows(ctx context.Context, ids []int64) ([]queries.AutopilotRoutine, error) {
	rows, err := s.store.all(ctx)
	if err != nil || len(ids) == 0 {
		return rows, err
	}

	for _, id := range ids {
		if !slices.ContainsFunc(rows, func(r queries.AutopilotRoutine) bool { return r.ID == id }) {
			return nil, goneError(id)
		}
	}

	return slices.DeleteFunc(rows, func(r queries.AutopilotRoutine) bool { return !slices.Contains(ids, r.ID) }), nil
}

// exportPrompts returns the text of the prompts b's routines name by nickname, and how many,
// when asked and the prompts can be carried; none otherwise.
func (s *Service) exportPrompts(ctx context.Context, b Bundle, asked bool) (json.RawMessage, int, error) {
	if !asked || s.transfer == nil {
		return nil, 0, nil
	}

	raw, n, err := s.transfer.ExportPrompts(ctx, bundleNicknames(b))
	if err != nil {
		return nil, 0, uierr.New("The prompts the routines run couldn't be read.", err)
	}

	return raw, n, nil
}

// bundleNicknames returns the nicknames of the prompts b's routines name, each once,
// ignoring case, in order.
func bundleNicknames(b Bundle) []string {
	var out []string

	for _, r := range b.Routines {
		sp, err := spec.Decode(r.Spec)
		if err != nil {
			continue
		}

		for _, ref := range promptRefs(sp) {
			if ref.Nickname != "" && !textutil.ContainsFold(out, ref.Nickname) {
				out = append(out, ref.Nickname)
			}
		}
	}

	return out
}

// RefKind is what a routine names that this Mac may lack.
type RefKind string

// The kinds of reference.
const (
	RefPrompt  RefKind = "prompt"
	RefProfile RefKind = "profile"
	RefAgent   RefKind = "agent"
)

// MissingRef is something a bundle's routines name that this Mac lacks, which the user
// maps to something it has, or to inherit.
type MissingRef struct {
	Kind RefKind `json:"kind"`

	// Agent is a profile's agent (RefProfile), or the agent (RefAgent); Name is a prompt's
	// nickname or a starter's words (RefPrompt), or a profile's name (RefProfile).
	Agent string `json:"agent"`
	Name  string `json:"name"`

	// Starter is a starter prompt's key (RefPrompt), "" for one named by nickname.
	Starter string `json:"starter"`

	// Reply reports a prompt a GitHub step comments, which can't inherit.
	Reply bool `json:"reply"`

	// Routines name the routines that name it.
	Routines []string `json:"routines"`

	// Choices are what it may be mapped to: the library's prompts of its purpose, or the
	// agent's profiles. "" (inherit) is offered beside them but for a reply.
	Choices []string `json:"choices"`

	// Inherits reports one the import maps to inherit by itself, with no choice: an
	// agent whose program this Mac lacks.
	Inherits bool `json:"inherits"`
}

// RefChoice maps one MissingRef to what this Mac has: To is a prompt's nickname or a
// profile's name, "" to inherit.
type RefChoice struct {
	Kind    RefKind `json:"kind"`
	Agent   string  `json:"agent"`
	Name    string  `json:"name"`
	Starter string  `json:"starter"`
	To      string  `json:"to"`
}

// ImportRoutine is one of a bundle's routines as it would arrive.
type ImportRoutine struct {
	// Name is its name in the file, and ArrivesAs the name it takes here, another when a
	// routine here has it.
	Name      string `json:"name"`
	ArrivesAs string `json:"arrivesAs"`

	// Sentence says it in one sentence, "" when it won't load, and Broken why it won't.
	Sentence string `json:"sentence"`
	Broken   string `json:"broken"`
}

// ImportPreview is what importing a bundle would do.
type ImportPreview struct {
	// SHA256 names the file as read, which Import takes back, so a file that changed
	// since is read again rather than imported unseen.
	SHA256 string `json:"sha256"`

	// Routines are its routines, each arriving switched off.
	Routines []ImportRoutine `json:"routines"`

	// Prompts are the prompts it brings, each with what bringing it in would do.
	Prompts []PromptArrival `json:"prompts"`

	// Missing are what its routines name that this Mac lacks, after the prompts it brings.
	Missing []MissingRef `json:"missing"`
}

// ImportRequest is an import as the user confirmed it: the file and its SHA-256 as the
// preview read it, and what each missing reference maps to (left out, it inherits).
type ImportRequest struct {
	Path   string      `json:"path"`
	SHA256 string      `json:"sha256"`
	Map    []RefChoice `json:"map"`
}

// Imported says what Import brought in.
type Imported struct {
	// Routines are the names the routines arrived under, each switched off, and Prompts
	// the nicknames of the prompts added.
	Routines []string `json:"routines"`
	Prompts  []string `json:"prompts"`
}

// readBundle reads the bundle at path, and its SHA-256.
func readBundle(path string) (Bundle, string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return Bundle{}, "", uierr.New("Choose a file of routines.", ErrNoPath)
	}

	data, err := appdata.ReadFile(path, MaxBundleBytes)
	if err != nil {
		return Bundle{}, "", uierr.New("The file couldn't be read.", err)
	}

	b, err := DecodeBundle(data)
	if err != nil {
		return Bundle{}, "", uierr.New(textutil.Sentence(err.Error()), err)
	}

	sum := sha256.Sum256(data)

	return b, hex.EncodeToString(sum[:]), nil
}

// PreviewImport reads the bundle at path and says what importing it would do, changing
// nothing: each routine's sentence and the name it arrives under, the prompts it brings,
// and what its routines name that this Mac lacks.
func (s *Service) PreviewImport(ctx context.Context, path string) (ImportPreview, error) {
	out := ImportPreview{Routines: []ImportRoutine{}, Prompts: []PromptArrival{}, Missing: []MissingRef{}}

	b, sum, err := readBundle(path)
	if err != nil {
		return out, err
	}

	out.SHA256 = sum

	rows, err := s.store.all(ctx)
	if err != nil {
		return out, err
	}

	out.Routines = arrivingRoutines(b, rows, s.view(ctx).names)

	if out.Prompts, err = s.previewPrompts(ctx, b); err != nil {
		return out, err
	}

	out.Missing, err = s.missingRefs(ctx, b, out.Prompts)

	return out, err
}

// arrivingRoutines says how each of b's routines would arrive beside rows: the name it takes
// and its sentence, or why it won't load. One whose definition isn't JSON at all is left out,
// as an import leaves it out.
func arrivingRoutines(b Bundle, rows []queries.AutopilotRoutine, names spec.PromptNames) []ImportRoutine {
	out := make([]ImportRoutine, 0, len(b.Routines))

	for _, r := range b.Routines {
		k, ok := keptFrom(r.Name, r.Description, r.Template, r.Mode, r.Spec)
		if !ok {
			continue
		}

		in := ImportRoutine{Name: r.Name, ArrivesAs: freeName(k.name, rows)}
		rows = append(rows, queries.AutopilotRoutine{Name: in.ArrivesAs})

		if k.spec.loads {
			in.Sentence = spec.Sentence(k.spec.spec, names)
		} else if be, isBroken := errors.AsType[*spec.BrokenError](k.spec.refusal); isBroken {
			in.Broken = brokenWords(be)
		}

		out = append(out, in)
	}

	return out
}

// previewPrompts says what bringing in each of b's prompts would do, none when it brings
// none or they can't be carried.
func (s *Service) previewPrompts(ctx context.Context, b Bundle) ([]PromptArrival, error) {
	if len(b.Prompts) == 0 || s.transfer == nil {
		return []PromptArrival{}, nil
	}

	arrivals, err := s.transfer.PreviewPrompts(ctx, b.Prompts)
	if err != nil {
		return []PromptArrival{}, uierr.New("The prompts in the file couldn't be read.", err)
	}

	return textutil.NonNil(arrivals), nil
}

// Import brings in the bundle at req.Path, as its preview read it: first the prompts it
// brings that no prompt here has the nickname of, then every routine, switched off, at the
// end of the list, each its own name or another when a routine here has it, the
// account signed in's, and with each missing reference mapped as req says. A routine a
// newer build wrote arrives as it was, broken. A file that changed since its preview is
// refused (ErrChanged).
func (s *Service) Import(ctx context.Context, req ImportRequest) (Imported, error) {
	out := Imported{Routines: []string{}, Prompts: []string{}}

	b, sum, err := readBundle(req.Path)
	if err != nil {
		return out, err
	}

	if req.SHA256 != "" && req.SHA256 != sum {
		return out, uierr.New("The file changed since it was read: read it again before you import it.", ErrChanged)
	}

	if out.Prompts, err = s.importPrompts(ctx, b); err != nil {
		return out, err
	}

	// The prompts just added are the library's now, however soon its next read sees them.
	added := make([]PromptArrival, 0, len(out.Prompts))
	for _, n := range out.Prompts {
		added = append(added, PromptArrival{Nickname: n, Arrives: ArrivesNew})
	}

	missing, err := s.missingRefs(ctx, b, added)
	if err != nil {
		return out, err
	}

	if out.Routines, err = s.storeImported(ctx, b, newRefMapper(missing, req.Map)); err != nil {
		return Imported{Routines: []string{}, Prompts: out.Prompts}, uierr.New("The routines couldn't be imported.", err)
	}

	if len(out.Routines) > 0 {
		s.changed(ctx)
	}

	return out, nil
}

// importPrompts adds b's prompts no prompt here has the nickname of, and returns their
// nicknames, none when it brings none or they can't be carried.
func (s *Service) importPrompts(ctx context.Context, b Bundle) ([]string, error) {
	if len(b.Prompts) == 0 || s.transfer == nil {
		return []string{}, nil
	}

	added, err := s.transfer.ImportPrompts(ctx, b.Prompts)
	if err != nil {
		return []string{}, uierr.New("The prompts in the file couldn't be added.", err)
	}

	return textutil.NonNil(added), nil
}

// storeImported stores b's routines, mapped, switched off, the account signed in's, at the
// end of the list, each under a free name, in one transaction, and returns their names.
func (s *Service) storeImported(ctx context.Context, b Bundle, mapper refMapper) ([]string, error) {
	viewer := s.viewer(ctx)
	names := []string{}

	err := s.reorder(ctx, func(q *queries.Queries, rows []queries.AutopilotRoutine) ([]queries.AutopilotRoutine, error) {
		for _, r := range b.Routines {
			k, ok := keptFrom(r.Name, r.Description, r.Template, r.Mode, mapper.apply(r.Spec))
			if !ok {
				continue
			}

			k.name, k.account = freeName(k.name, rows), viewer

			row, err := insertKept(ctx, q, s.clock, k, int64(len(rows)))
			if err != nil {
				return nil, err
			}

			rows = append(rows, row)
			names = append(names, row.Name)
		}

		return rows, nil
	})
	if err != nil {
		return nil, err
	}

	return names, nil
}

// missingRefs returns what b's routines name that this Mac lacks, after the prompts that
// arrive with it: each prompt named by nickname or by starter that the library lacks and
// no new prompt brings, each agent whose program this Mac lacks, and each profile none of
// its agents has, in the order the routines name them.
func (s *Service) missingRefs(ctx context.Context, b Bundle, arriving []PromptArrival) ([]MissingRef, error) {
	c, err := s.refCollector(ctx, arriving)
	if err != nil {
		return nil, err
	}

	for _, r := range b.Routines {
		if sp, decodeErr := spec.Decode(r.Spec); decodeErr == nil {
			c.routine(r.Name, sp)
		}
	}

	return textutil.NonNil(c.out), nil
}

// refCollector returns a collector over the library, this Mac's agents and profiles, and
// the prompts arriving new.
func (s *Service) refCollector(ctx context.Context, arriving []PromptArrival) (*refCollector, error) {
	c := &refCollector{
		library: s.libraryPrompts(ctx), names: s.view(ctx).names, noLibrary: s.library == nil,
		noProfiles: s.profiles == nil,
	}

	if s.profiles != nil {
		agents, err := s.profiles.Profiles(ctx)
		if err != nil {
			return nil, uierr.New("The agents' profiles on this Mac couldn't be read.", err)
		}

		c.agents = agents
	}

	for _, a := range arriving {
		if a.Arrives == ArrivesNew {
			c.arriving = append(c.arriving, a.Nickname)
		}
	}

	return c, nil
}

// refCollector gathers what routines name that this Mac lacks.
type refCollector struct {
	library  []LibraryPrompt
	agents   map[string][]string
	names    spec.PromptNames
	arriving []string

	// noLibrary and noProfiles report nothing to check prompts or profiles against.
	noLibrary, noProfiles bool

	out []MissingRef
}

// routine gathers what the routine name names.
func (c *refCollector) routine(name string, sp spec.Spec) {
	for _, st := range sp.Steps {
		if st.Agent != nil {
			c.agentStep(name, *st.Agent)
		}

		for _, a := range st.GitHub {
			if a.Reply != nil {
				c.prompt(name, a.Reply.Prompt, true)
			}
		}
	}
}

// agentStep gathers what one agent step names: its prompt, its agents, and its profiles.
func (c *refCollector) agentStep(name string, a spec.AgentStep) {
	c.prompt(name, a.Prompt, false)
	c.agent(name, a.AgentID())
	c.profile(name, a.AgentID(), a.Profile)

	for _, row := range a.LaunchFor {
		agentID := firstOf(row.Agent, a.AgentID())
		c.agent(name, agentID)
		c.profile(name, agentID, row.Profile)
	}

	for _, f := range a.Fallbacks {
		c.agent(name, f.Agent)
		c.profile(name, f.Agent, f.Profile)
	}
}

// add records ref for the routine name, once.
func (c *refCollector) add(name string, ref MissingRef) {
	for i, have := range c.out {
		if have.Kind == ref.Kind && strings.EqualFold(have.Agent, ref.Agent) && strings.EqualFold(have.Name, ref.Name) &&
			have.Starter == ref.Starter {
			if !slices.Contains(have.Routines, name) {
				c.out[i].Routines = append(c.out[i].Routines, name)
			}

			return
		}
	}

	ref.Routines = []string{name}
	ref.Choices = textutil.NonNil(ref.Choices)
	c.out = append(c.out, ref)
}

// prompt records a prompt the library lacks.
func (c *refCollector) prompt(name string, ref spec.PromptRef, reply bool) {
	if c.noLibrary || ref.ItemDefault || c.has(ref) {
		return
	}

	m := MissingRef{Kind: RefPrompt, Name: ref.Nickname, Starter: ref.Starter, Reply: reply}
	if ref.Starter != "" {
		m.Name = starterWords(ref.Starter)
		if c.names != nil && c.names(ref) != "" {
			m.Name = c.names(ref)
		}
	}

	m.Choices = c.promptChoices(reply)
	c.add(name, m)
}

// has reports a prompt the library holds, or one that arrives with the bundle by nickname.
func (c *refCollector) has(ref spec.PromptRef) bool {
	if _, ok := draft.FindPrompt(c.library, ref); ok {
		return true
	}

	return ref.Nickname != "" && textutil.ContainsFold(c.arriving, ref.Nickname)
}

// promptChoices are the library's prompts a missing prompt may map to: its replies for a
// reply, its agent prompts otherwise, by nickname.
func (c *refCollector) promptChoices(reply bool) []string {
	var out []string

	for _, p := range c.library {
		if p.Reply == reply {
			out = append(out, p.Nickname)
		}
	}

	slices.Sort(out)

	return out
}

// agent records an agent whose program this Mac lacks, which inherits.
func (c *refCollector) agent(name, agentID string) {
	if c.noProfiles || agentID == "" || agentID == catalog.Inherit {
		return
	}

	if _, ok := c.agents[agentID]; ok {
		return
	}

	c.add(name, MissingRef{Kind: RefAgent, Agent: agentID, Name: agentID, Inherits: true})
}

// profile records a profile none of the agents it may run on has. An agent this Mac
// lacks inherits its profile with it.
func (c *refCollector) profile(name, agentID, profile string) {
	if c.noProfiles || profile == "" {
		return
	}

	if agentID != "" && agentID != catalog.Inherit {
		names, ok := c.agents[agentID]
		if ok && !slices.Contains(names, profile) {
			c.add(name, MissingRef{Kind: RefProfile, Agent: agentID, Name: profile, Choices: slices.Sorted(slices.Values(names))})
		}

		return
	}

	if choices, missing := c.inheritedProfile(profile); missing {
		c.add(name, MissingRef{Kind: RefProfile, Agent: catalog.Inherit, Name: profile, Choices: choices})
	}
}

// inheritedProfile reports a profile of an agent step that inherits its agent which no agent
// here has, with every agent's profiles as the choices.
func (c *refCollector) inheritedProfile(profile string) ([]string, bool) {
	var choices []string

	for _, names := range c.agents {
		if slices.Contains(names, profile) {
			return nil, false
		}

		choices = append(choices, names...)
	}

	slices.Sort(choices)

	return slices.Compact(choices), true
}

// refMapper rewrites a routine's spec as an import maps what this Mac lacks.
type refMapper struct {
	missing []MissingRef
	choices []RefChoice
}

// newRefMapper maps each of missing as choices say, else to inherit.
func newRefMapper(missing []MissingRef, choices []RefChoice) refMapper {
	return refMapper{missing: missing, choices: choices}
}

// to returns what ref maps to, and whether it is missing at all.
func (m refMapper) to(ref MissingRef) (string, bool) {
	found := slices.ContainsFunc(m.missing, func(have MissingRef) bool { return sameRef(have, ref) })
	if !found {
		return "", false
	}

	if ref.Kind == RefAgent {
		return "", true
	}

	for _, c := range m.choices {
		if sameRef(MissingRef{Kind: c.Kind, Agent: c.Agent, Name: c.Name, Starter: c.Starter}, ref) {
			return strings.TrimSpace(c.To), true
		}
	}

	return "", true
}

// sameRef reports two references to the same thing.
func sameRef(a, b MissingRef) bool {
	if a.Kind != b.Kind || a.Starter != b.Starter {
		return false
	}

	return strings.EqualFold(a.Name, b.Name) && (a.Kind == RefPrompt || strings.EqualFold(a.Agent, b.Agent))
}

// apply returns raw with every missing reference mapped, or raw as it is when it doesn't
// decode, as a newer build's spec, which arrives broken and unrewritten.
func (m refMapper) apply(raw json.RawMessage) json.RawMessage {
	sp, err := spec.Decode(raw)
	if err != nil || len(m.missing) == 0 {
		return raw
	}

	for i := range sp.Steps {
		m.step(&sp.Steps[i])
	}

	data, err := spec.Encode(sp)
	if err != nil {
		return raw
	}

	return data
}

// step maps one step: an agent step's prompt, agents, and profiles, and a GitHub step's
// replies.
func (m refMapper) step(st *spec.Step) {
	if st.Agent != nil {
		m.agentStep(st.Agent)
	}

	for j := range st.GitHub {
		if r := st.GitHub[j].Reply; r != nil {
			r.Prompt = m.prompt(r.Prompt, true)
		}
	}
}

// prompt maps a prompt the library lacks: to the prompt chosen by its nickname, or, for an
// agent's prompt, to the item's own default prompt; a reply with no choice stays as it is.
func (m refMapper) prompt(ref spec.PromptRef, reply bool) spec.PromptRef {
	name := ref.Nickname
	if ref.Starter != "" {
		name = ""
	}

	to, missing := m.toPrompt(ref, name)

	switch {
	case !missing:
		return ref
	case to != "":
		return spec.PromptRef{Nickname: to}
	case reply:
		return ref
	default:
		return spec.PromptRef{ItemDefault: true}
	}
}

// toPrompt finds what the prompt ref maps to among the missing references.
func (m refMapper) toPrompt(ref spec.PromptRef, nickname string) (string, bool) {
	for _, have := range m.missing {
		if have.Kind != RefPrompt || have.Starter != ref.Starter {
			continue
		}

		if ref.Starter == "" && !strings.EqualFold(have.Name, nickname) {
			continue
		}

		return m.to(have)
	}

	return "", false
}

// agentStep maps an agent step's agents and profiles.
func (m refMapper) agentStep(a *spec.AgentStep) {
	a.Prompt = m.prompt(a.Prompt, false)

	agentID := a.AgentID()
	a.Profile = m.profile(agentID, a.Profile)

	if _, missing := m.to(MissingRef{Kind: RefAgent, Agent: agentID, Name: agentID}); missing {
		a.Agent, a.Profile = catalog.Inherit, ""
	}

	for i := range a.LaunchFor {
		row := &a.LaunchFor[i]
		rowAgent := firstOf(row.Agent, agentID)
		row.Profile = m.profile(rowAgent, row.Profile)

		if _, missing := m.to(MissingRef{Kind: RefAgent, Agent: rowAgent, Name: rowAgent}); missing {
			row.Agent, row.Profile = "", ""
		}
	}

	a.Fallbacks = slices.DeleteFunc(a.Fallbacks, func(f spec.ProfileRef) bool {
		_, agentMissing := m.to(MissingRef{Kind: RefAgent, Agent: f.Agent, Name: f.Agent})

		return agentMissing
	})

	for i := range a.Fallbacks {
		f := &a.Fallbacks[i]
		f.Profile = m.profile(f.Agent, f.Profile)
	}

	a.Fallbacks = slices.DeleteFunc(a.Fallbacks, func(f spec.ProfileRef) bool { return f.Profile == "" })

	if len(a.Fallbacks) == 0 {
		a.Fallbacks = nil
	}
}

// profile maps a profile of agentID that this Mac lacks: to the profile chosen, or to
// inherit ("").
func (m refMapper) profile(agentID, profile string) string {
	if profile == "" {
		return ""
	}

	agent := agentID
	if agent == "" {
		agent = catalog.Inherit
	}

	to, missing := m.to(MissingRef{Kind: RefProfile, Agent: agent, Name: profile})
	if !missing {
		return profile
	}

	return to
}
