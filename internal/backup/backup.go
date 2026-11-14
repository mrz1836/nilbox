// Package backup exports the user's whole setup to one file and imports it back: their
// settings, their repositories, the prompt library, the launch defaults, and their own
// scripts, each a category the user ticks on export and again on import (see File for the
// format). It owns no table: each category is an owner's, which core adapts to a Part.
//
// An import shows first what it would do (Preview), under either mode: Merge, which adds
// and changes but deletes nothing, and Replace, which makes each category exactly the
// file's. The import itself (Import) reads the file again and refuses one that changed
// since the preview, saves the setup as it stands into the app's own backups folder, and
// then applies every category in one transaction, so it is all or nothing.
//
// A backup never holds the GitHub sign-in, which the app never stores, nor anything the
// app reads again or keeps on its own: caches, mirrors, history, per-item triage, the
// agent profiles found on disk, script trust (a file from elsewhere could pre-approve a
// repository's command), the window's memory, and the tools' detection.
package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

const (
	// KeepBefore is how many of the copies an import saves first the app keeps.
	KeepBefore = 5

	// beforePrefix starts the name of each copy an import saves first.
	beforePrefix = brand.Slug + " before import "

	// stampLayout stamps the name of each copy an import saves first; it sorts as text in
	// time order.
	stampLayout = "20060102T150405Z"
)

var (
	// ErrNoPath is returned when the file to export to or import from is not an absolute
	// path.
	ErrNoPath = errors.New("choose a file")

	// ErrNoSections is returned for an export or an import that names no category.
	ErrNoSections = errors.New("choose what to include")

	// ErrUnknownSection is returned for a category this build doesn't know.
	ErrUnknownSection = errors.New("unknown category")

	// ErrNotInFile is returned for an import of a category the file doesn't hold.
	ErrNotInFile = errors.New("the backup doesn't hold that category")

	// ErrUnknownMode is returned for an import mode this build doesn't know.
	ErrUnknownMode = errors.New("unknown import mode")

	// ErrChanged is returned when the file changed between the preview and the import.
	ErrChanged = errors.New("the file changed since you opened it; open it again to see what it holds now")
)

// How is what a part's Prepare needs to know of the import beside its own section.
type How struct {
	// Mode is how the import treats what is here.
	Mode Mode

	// Sections are the categories the import takes.
	Sections []Section

	// Prompts names, by their nicknames in the file, the prompts the library will hold
	// for the file once its prompts section is applied, with what each is; nil when the
	// import doesn't take the prompts.
	Prompts map[string]PromptKind
}

// Takes reports whether the import takes the category s.
func (h How) Takes(s Section) bool {
	return slices.Contains(h.Sections, s)
}

// PromptKind is what a prompt the import will bring is: an agent prompt or a review reply,
// and what kinds of item it applies to, as "pr", "issue", or "any".
type PromptKind struct {
	Reply     bool
	AppliesTo string
}

// Links maps each prompt's nickname in the file to its ID here, once the prompts section
// is applied: the prompt added, saved over, or already holding it. It is empty when the
// import doesn't take the prompts.
type Links map[string]int64

// Prepared is what a part's Prepare worked out: its Plan, and what Apply needs.
type Prepared struct {
	Plan

	// Prompts are, for the prompts part alone, the prompts the library will hold for the
	// file, as How.Prompts names them.
	Prompts map[string]PromptKind

	// State is the part's own, which its Apply reads back.
	State any
}

// Part is one category's owner, as backup reaches it. Every method but Apply runs outside
// the import's transaction, and Apply only inside it.
type Part interface {
	// Export returns the category in portable form, and how many items it holds.
	Export(ctx context.Context) (json.RawMessage, int, error)

	// Prepare reads the category's section and works out what importing it does,
	// changing nothing: what it checks against the disk, the profiles, or the library is
	// checked here.
	Prepare(ctx context.Context, raw json.RawMessage, how How) (Prepared, error)

	// Apply makes the changes Prepare worked out inside tx, the import's transaction, and
	// returns what it did. The prompts part fills in links; the launch defaults read them.
	Apply(ctx context.Context, tx *sql.Tx, prepared Prepared, links Links) (Count, error)

	// Done runs once the transaction committed: the part's events, and the rewrite of the
	// copies the app keeps of it.
	Done(ctx context.Context, prepared Prepared)
}

// Clearer is a part whose rows Replace deletes before any part is applied, so a category
// applied before it is free of them: the launch defaults, which name prompts the prompts
// part may delete.
type Clearer interface {
	Clear(ctx context.Context, tx *sql.Tx) error
}

// Config holds a Service's dependencies. Pool and Parts are required.
type Config struct {
	// Pool is the database every part's Apply runs in, in one transaction.
	Pool *sql.DB

	// Parts are the categories' owners. A category without one is never exported or
	// imported.
	Parts map[Section]Part

	// Dir is where an import saves the setup as it stands before it changes anything.
	Dir string

	// App is the build's version, which every backup records.
	App string

	// Home shortens the paths the window is shown with ~.
	Home string

	// Clock stamps the backups. Nil is the system clock.
	Clock clock.Clock

	// Logger hears what an import could not tidy. Nil discards.
	Logger *slog.Logger
}

// Service exports and imports the user's setup.
type Service struct {
	pool   *sql.DB
	parts  map[Section]Part
	dir    string
	app    string
	home   string
	clock  clock.Clock
	logger *slog.Logger
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	return &Service{
		pool: cfg.Pool, parts: cfg.Parts, dir: cfg.Dir, app: cfg.App, home: cfg.Home, clock: clock.Or(cfg.Clock),
		logger: logging.Or(cfg.Logger),
	}
}

// SectionCount is how many items one category of a backup holds.
type SectionCount struct {
	Section Section `json:"section"`
	Items   int     `json:"items"`
}

// Exported says what Export wrote.
type Exported struct {
	// Path is the file.
	Path string `json:"path"`

	// Sections are the categories written, in order, with how many items each holds.
	Sections []SectionCount `json:"sections"`
}

// Export writes the categories in sections to a backup at path, readable only by the user,
// replacing any file there, and says how many items each holds.
func (s *Service) Export(ctx context.Context, path string, sections []Section) (Exported, error) {
	if path == "" || !filepath.IsAbs(path) {
		return Exported{}, ErrNoPath
	}

	chosen, err := s.chosen(sections)
	if err != nil {
		return Exported{}, err
	}

	data, counts, err := s.encode(ctx, chosen)
	if err != nil {
		return Exported{}, err
	}

	if err = appdata.WriteFile(path, data); err != nil {
		return Exported{}, fmt.Errorf("save the backup: %w", err)
	}

	return Exported{Path: path, Sections: counts}, nil
}

// SectionPreview is one category of a backup as the import dialog shows it.
type SectionPreview struct {
	// Section is the category.
	Section Section `json:"section"`

	// Present reports whether the file holds it.
	Present bool `json:"present"`

	// Merge and Replace are what importing it would do under each mode. Remove counts
	// only under Replace.
	Merge   Plan `json:"merge"`
	Replace Plan `json:"replace"`

	// Error says why the section can't be imported, or is "" when it can.
	Error string `json:"error"`
}

// Preview is a backup as the import dialog shows it, before anything is changed.
type Preview struct {
	// Path is the file, and Hash the SHA-256 of its bytes, which Import checks again.
	Path string `json:"path"`
	Hash string `json:"hash"`

	// ExportedAt is when the file was written, as RFC 3339, and App the version that wrote
	// it, each as the file says.
	ExportedAt string `json:"exportedAt"`
	App        string `json:"app"`

	// Sections are every category, in order, each present or not.
	Sections []SectionPreview `json:"sections"`
}

// Preview reads the backup at path and says, for each category it holds, what importing
// it would do under each mode, changing nothing. A section that can't be read is shown
// with why, and the others still are.
func (s *Service) Preview(ctx context.Context, path string) (Preview, error) {
	if path == "" || !filepath.IsAbs(path) {
		return Preview{}, ErrNoPath
	}

	f, hash, err := read(path)
	if err != nil {
		return Preview{}, err
	}

	out := Preview{Path: path, Hash: hash, ExportedAt: f.ExportedAt, App: f.App, Sections: []SectionPreview{}}
	present := s.present(f)

	plans := map[Mode]map[Section]Prepared{}
	errs := map[Section]error{}

	for _, mode := range []Mode{ModeMerge, ModeReplace} {
		plans[mode] = s.prepare(ctx, f, How{Mode: mode, Sections: present}, errs)
	}

	for _, section := range Sections() {
		p := SectionPreview{
			Section: section, Present: slices.Contains(present, section),
			Merge: normalized(plans[ModeMerge][section].Plan), Replace: normalized(plans[ModeReplace][section].Plan),
		}

		if err := errs[section]; err != nil {
			p.Error = uierr.Message(err)
		}

		out.Sections = append(out.Sections, p)
	}

	return out, nil
}

// Request is an import as the window asks for it.
type Request struct {
	// Path is the backup, and Hash the hash its preview gave.
	Path string `json:"path"`
	Hash string `json:"hash"`

	// Sections are the categories to import, each one the file holds.
	Sections []Section `json:"sections"`

	// Mode is how to treat what is here.
	Mode Mode `json:"mode"`
}

// SectionResult is what an import did to one category.
type SectionResult struct {
	Section Section  `json:"section"`
	Count   Count    `json:"count"`
	Notes   []string `json:"notes"`
}

// Imported says what Import did.
type Imported struct {
	// SavedTo is the copy of the setup as it stood before the import, with ~ for home.
	SavedTo string `json:"savedTo"`

	// Sections are the categories imported, in order.
	Sections []SectionResult `json:"sections"`
}

// Import applies the categories req names from the backup at its path, all of them or,
// when anything fails, none. It refuses a file whose bytes changed since its preview (its
// hash differs), saves the whole setup as it stands into the backups folder first (keeping
// the newest KeepBefore such copies), works each category out again, then applies every
// one in one transaction, in Sections' order, the rows Replace deletes from a Clearer
// first. Once it commits, each part's Done tells the app and rewrites the copies it keeps.
func (s *Service) Import(ctx context.Context, req Request) (Imported, error) {
	f, chosen, err := s.load(req)
	if err != nil {
		return Imported{}, err
	}

	errs := map[Section]error{}
	prepared := s.prepare(ctx, f, How{Mode: req.Mode, Sections: chosen}, errs)

	for _, section := range chosen {
		if err = errs[section]; err != nil {
			return Imported{}, err
		}
	}

	saved, err := s.saveBefore(ctx)
	if err != nil {
		return Imported{}, err
	}

	counts, err := s.apply(ctx, chosen, req.Mode, prepared)
	if err != nil {
		return Imported{}, fmt.Errorf("import the backup: %w", err)
	}

	out := Imported{SavedTo: paths.Tilde(saved, s.home), Sections: make([]SectionResult, 0, len(chosen))}

	for _, section := range chosen {
		s.parts[section].Done(ctx, prepared[section])

		out.Sections = append(out.Sections, SectionResult{
			Section: section, Count: counts[section], Notes: textutil.NonNil(prepared[section].Notes),
		})
	}

	return out, nil
}

// load checks req and reads its file: an absolute path, a mode this build knows, categories
// it has parts for, the bytes the preview read, and each category in the file. It returns
// the file and the categories, in order.
func (s *Service) load(req Request) (File, []Section, error) {
	if req.Path == "" || !filepath.IsAbs(req.Path) {
		return File{}, nil, ErrNoPath
	}

	if !req.Mode.Valid() {
		return File{}, nil, fmt.Errorf("%w: %q", ErrUnknownMode, req.Mode)
	}

	chosen, err := s.chosen(req.Sections)
	if err != nil {
		return File{}, nil, err
	}

	f, hash, err := read(req.Path)
	if err != nil {
		return File{}, nil, err
	}

	if hash != req.Hash {
		return File{}, nil, ErrChanged
	}

	for _, section := range chosen {
		if f.Section(section) == nil {
			return File{}, nil, fmt.Errorf("%w: %s", ErrNotInFile, section)
		}
	}

	return f, chosen, nil
}

// apply applies each of chosen's prepared parts in one transaction, and returns what each
// did.
func (s *Service) apply(ctx context.Context, chosen []Section, mode Mode, prepared map[Section]Prepared,
) (map[Section]Count, error) {
	counts := map[Section]Count{}

	err := store.InTx(ctx, s.pool, func(tx *sql.Tx) error {
		if mode == ModeReplace {
			if err := s.clear(ctx, tx, chosen); err != nil {
				return err
			}
		}

		links := Links{}

		for _, section := range chosen {
			count, err := s.parts[section].Apply(ctx, tx, prepared[section], links)
			if err != nil {
				return fmt.Errorf("%s: %w", section, err)
			}

			counts[section] = count
		}

		return nil
	})

	return counts, err
}

// clear deletes, inside tx, the rows of each of chosen's parts that is a Clearer.
func (s *Service) clear(ctx context.Context, tx *sql.Tx, chosen []Section) error {
	for _, section := range chosen {
		c, ok := s.parts[section].(Clearer)
		if !ok {
			continue
		}

		if err := c.Clear(ctx, tx); err != nil {
			return fmt.Errorf("%s: %w", section, err)
		}
	}

	return nil
}

// prepare runs each part's Prepare over its section of f, in order, for the categories how
// takes that f holds, handing the prompts part's prompts to the parts after it. A part
// that fails is left out, with its error in errs.
func (s *Service) prepare(ctx context.Context, f File, how How, errs map[Section]error) map[Section]Prepared {
	out := map[Section]Prepared{}

	for _, section := range Sections() {
		raw := f.Section(section)
		if raw == nil || !how.Takes(section) {
			continue
		}

		p, err := s.parts[section].Prepare(ctx, raw, how)
		if err != nil {
			errs[section] = err

			continue
		}

		if section == SectionPrompts {
			how.Prompts = p.Prompts
		}

		out[section] = p
	}

	return out
}

// present lists the categories f holds that this build has a part for.
func (s *Service) present(f File) []Section {
	out := []Section{}

	for _, section := range Sections() {
		if _, ok := s.parts[section]; ok && f.Section(section) != nil {
			out = append(out, section)
		}
	}

	return out
}

// chosen returns sections in Sections' order, each once, refusing none and one this
// build has no part for.
func (s *Service) chosen(sections []Section) ([]Section, error) {
	out := []Section{}

	for _, section := range sections {
		if _, ok := s.parts[section]; !ok || !section.Valid() {
			return nil, fmt.Errorf("%w: %q", ErrUnknownSection, section)
		}
	}

	for _, section := range Sections() {
		if slices.Contains(sections, section) {
			out = append(out, section)
		}
	}

	if len(out) == 0 {
		return nil, ErrNoSections
	}

	return out, nil
}

// encode returns a backup of the categories in sections, and how many items each holds.
func (s *Service) encode(ctx context.Context, sections []Section) ([]byte, []SectionCount, error) {
	f := File{Format: Format, Version: Version, ExportedAt: clock.Format(s.clock.Now()), App: s.app}
	counts := make([]SectionCount, 0, len(sections))

	for _, section := range sections {
		raw, n, err := s.parts[section].Export(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("back up the %s: %w", section, err)
		}

		f = f.withSection(section, raw)
		counts = append(counts, SectionCount{Section: section, Items: n})
	}

	data, err := Encode(f)
	if err != nil {
		return nil, nil, err
	}

	return data, counts, nil
}

// saveBefore saves every category the Service has a part for into the backups folder, as
// "<slug> before import <stamp>.json", and keeps the newest KeepBefore such copies. It
// returns the copy's path.
func (s *Service) saveBefore(ctx context.Context) (string, error) {
	if s.dir == "" {
		return "", nil
	}

	all := []Section{}

	for _, section := range Sections() {
		if _, ok := s.parts[section]; ok {
			all = append(all, section)
		}
	}

	data, _, err := s.encode(ctx, all)
	if err != nil {
		return "", fmt.Errorf("save the setup as it is before the import: %w", err)
	}

	if err = os.MkdirAll(s.dir, appdata.PrivateDirPerm); err != nil {
		return "", fmt.Errorf("save the setup as it is before the import: %w", err)
	}

	path := filepath.Join(s.dir, beforePrefix+s.clock.Now().UTC().Format(stampLayout)+".json")

	if err = appdata.WriteFile(path, data); err != nil {
		return "", fmt.Errorf("save the setup as it is before the import: %w", err)
	}

	s.prune(ctx)

	return path, nil
}

// prune deletes the copies saved before an import but the newest KeepBefore. A copy it
// can't delete is logged.
func (s *Service) prune(ctx context.Context) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		s.logger.WarnContext(ctx, "could not list the backups saved before imports", "error", err)

		return
	}

	var names []string

	for _, e := range entries {
		if name := e.Name(); e.Type().IsRegular() && strings.HasPrefix(name, beforePrefix) &&
			strings.HasSuffix(name, ".json") {
			names = append(names, name)
		}
	}

	slices.Sort(names)

	for len(names) > KeepBefore {
		if err = os.Remove(filepath.Join(s.dir, names[0])); err != nil {
			s.logger.WarnContext(ctx, "could not delete an old backup saved before an import", "name", names[0],
				"error", err)
		}

		names = names[1:]
	}
}
