package localrepos

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/paths"
)

// The bounds of Survey's look one level down, inside a folder with no clones directly
// inside it: the most subfolders it looks into, the most entries it reads across them,
// and the most suggestions it keeps.
const (
	insideFolders = 100
	insideEntries = 5000
	insideKept    = 8
)

// Suggestion is a folder Survey suggests adding in place of the one asked about, with ~
// for home, and how many clones sit directly inside it.
type Suggestion struct {
	Path   string `json:"path"`
	Clones int    `json:"clones"`
}

// Preview is what adding a folder as a repository location would add, worked out before
// anything is stored, with what the app suggests instead where it finds a better choice.
// No slice it holds is nil.
type Preview struct {
	// Path is the folder, with ~ for home.
	Path string `json:"path"`

	// Kind is what the folder is now.
	Kind Kind `json:"kind"`

	// Clones is how many clones sit directly inside a folder; 0 for a clone.
	Clones int `json:"clones"`

	// Repo is a clone's origin on GitHub, "owner/name", or "" when it isn't on GitHub or
	// wasn't read.
	Repo string `json:"repo"`

	// Covered says, in a sentence, why the locations already hold the folder: it is the
	// clone root, already listed, or a clone directly inside a location. "" otherwise.
	// CoveredBy is the location that holds it, with ~.
	Covered   string `json:"covered"`
	CoveredBy string `json:"coveredBy"`

	// Inside are the subfolders of a folder with no clones directly inside it that have
	// clones directly inside them, the most clones first, at most eight. MoreInside says
	// more had some, and Partial that the look stopped at its bounds before it was done.
	Inside     []Suggestion `json:"inside"`
	MoreInside bool         `json:"moreInside"`
	Partial    bool         `json:"partial"`

	// Parent is the folder of a clone whose folder holds other clones, which Clones
	// counts, the clone aside; its Path is "" when there's none to suggest. It is never /
	// and never a folder the locations already cover.
	Parent Suggestion `json:"parent"`

	// Replaces are the clone locations, with ~, that adding a folder makes redundant,
	// since they sit directly inside it. Adding the folder drops them.
	Replaces []string `json:"replaces"`
}

// Survey works out what adding the folder at dir to roots would add, reading the folder
// and its subfolders with stat and ReadDir alone. git, when set, reads a clone's origin,
// one read-only git config; nil leaves Repo "". A folder that can't be a location is
// refused in words the window shows (Classify, CheckAddable).
func Survey(ctx context.Context, dir string, roots Roots, home string, git execpath.Runner) (Preview, error) {
	dir = filepath.Clean(dir)

	kind, err := Classify(dir, home)
	if err != nil {
		return Preview{}, err
	}

	if err = CheckAddable(dir, home); err != nil {
		return Preview{}, err
	}

	p := Preview{Path: paths.Tilde(dir, home), Kind: kind, Inside: []Suggestion{}, Replaces: []string{}}

	if p.Covered, p.CoveredBy = roots.covering(dir, kind, home); p.Covered != "" {
		return p, nil
	}

	if kind == KindClone {
		p.Repo = originName(ctx, dir, git)
		p.Parent = roots.parentOf(dir, home)

		return p, nil
	}

	clones, _ := folderClones(dir) // Classify read it as a folder; one it can't list has none
	p.Clones = len(clones)

	if p.Clones == 0 {
		p.Inside, p.MoreInside, p.Partial = roots.inside(dir, home)
	}

	resolved := paths.Resolved(dir)

	for _, place := range roots.places {
		if place.Kind == KindClone && paths.Resolved(filepath.Dir(place.Path)) == resolved {
			p.Replaces = append(p.Replaces, paths.Tilde(place.Path, home))
		}
	}

	return p, nil
}

// covering says why the locations already hold dir, of kind, and which location does,
// with ~, or returns "" twice.
func (r Roots) covering(dir string, kind Kind, home string) (why, by string) {
	shown := paths.Tilde(dir, home)
	resolved := paths.Resolved(dir)

	for _, p := range r.places {
		if p.resolved != resolved {
			continue
		}

		if p.Primary {
			return shown + " is your clone root.", shown
		}

		return shown + " is already one of your repository locations.", paths.Tilde(p.Path, home)
	}

	if kind != KindClone {
		return "", ""
	}

	parent := paths.Resolved(filepath.Dir(dir))

	for _, p := range r.places {
		if p.Kind == KindFolder && p.resolved == parent {
			where := paths.Tilde(p.Path, home)

			return shown + " is already in " + where + ".", where
		}
	}

	return "", ""
}

// parentOf suggests the folder of the clone at dir when it holds other clones and could
// be a location the locations don't already cover; otherwise its Path is "".
func (r Roots) parentOf(dir, home string) Suggestion {
	parent := filepath.Dir(dir)
	if filepath.Dir(parent) == parent || CheckAddable(parent, home) != nil || r.has(paths.Resolved(parent)) {
		return Suggestion{}
	}

	clones, err := folderClones(parent)
	if err != nil {
		return Suggestion{}
	}

	others := 0

	for _, c := range clones {
		if !paths.Same(c, dir) {
			others++
		}
	}

	if others == 0 {
		return Suggestion{}
	}

	return Suggestion{Path: paths.Tilde(parent, home), Clones: others}
}

// inside looks one level down in dir, a folder with no clones directly inside it, for
// subfolders that have some: at most insideFolders subfolders and insideEntries entries
// across them. It returns the insideKept with the most, whether more had some, and whether
// the look stopped at its bounds.
func (r Roots) inside(dir, home string) (kept []Suggestion, more, partial bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []Suggestion{}, false, false
	}

	look := insideLook{roots: r, home: home, budget: insideEntries, found: []Suggestion{}}

	for _, entry := range entries {
		if !look.next(filepath.Join(dir, entry.Name())) {
			break
		}
	}

	found := look.found

	slices.SortStableFunc(found, func(a, b Suggestion) int {
		return cmp.Or(b.Clones-a.Clones, strings.Compare(a.Path, b.Path))
	})

	if len(found) > insideKept {
		return found[:insideKept], true, look.partial
	}

	return found, false, look.partial
}

// insideLook is inside's look one level down, as it goes: the subfolders it looked into,
// the entries it may still read, what it found, and whether it stopped at its bounds.
type insideLook struct {
	roots   Roots
	home    string
	folders int
	budget  int
	found   []Suggestion
	partial bool
}

// next looks into sub, a subfolder's path, unless it is hidden or not a folder, and
// reports whether the look goes on: not once it reaches its bounds.
func (l *insideLook) next(sub string) bool {
	if hidden(sub) || !isFolder(sub) {
		return true
	}

	if l.folders == insideFolders || l.budget <= 0 {
		l.partial = true

		return false
	}

	l.folders++

	count, read, complete := countClones(sub, l.budget)
	l.budget -= read
	l.partial = l.partial || !complete

	if count > 0 && !l.roots.has(paths.Resolved(sub)) && CheckAddable(sub, l.home) == nil {
		l.found = append(l.found, Suggestion{Path: paths.Tilde(sub, l.home), Clones: count})
	}

	return true
}

// countClones counts the clones directly inside dir, reading at most budget entries, and
// returns how many it read and whether it read them all.
func countClones(dir string, budget int) (count, read int, complete bool) {
	f, err := os.Open(filepath.Clean(dir))
	if err != nil {
		return 0, 0, true
	}

	defer func() { _ = f.Close() }()

	entries, err := f.ReadDir(budget)

	for _, entry := range entries {
		if name := entry.Name(); !strings.HasPrefix(name, ".") && isRepo(filepath.Join(dir, name)) {
			count++
		}
	}

	if err != nil || len(entries) < budget {
		return count, len(entries), true
	}

	// Exactly budget entries were read: complete only when nothing is left.
	rest, _ := f.ReadDir(1)

	return count, len(entries), len(rest) == 0
}

// isFolder reports a folder, or a link to one.
func isFolder(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.IsDir()
}

// originName returns the GitHub repository the clone at dir's origin is, "owner/name",
// or "" when it isn't on GitHub, can't be read, or git is nil.
func originName(ctx context.Context, dir string, git execpath.Runner) string {
	if git == nil {
		return ""
	}

	clone, err := Inspect(ctx, dir, git)
	if err != nil {
		return ""
	}

	if origin, ok := clone.Remotes[originRemote]; ok {
		return origin.String()
	}

	return ""
}

// LocationState is how one of the user's repository locations reads now, for the
// settings' list of them.
type LocationState struct {
	// Path is the location as stored, with ~ for home.
	Path string `json:"path"`

	// Kind is what it was when it was added; the clone root is a folder.
	Kind Kind `json:"kind"`

	// Primary marks the clone root.
	Primary bool `json:"primary"`

	// State is how it reads now.
	State State `json:"state"`

	// Clones is how many clones sit directly inside a folder.
	Clones int `json:"clones"`

	// Repo is a clone location's origin on GitHub, "owner/name", or "".
	Repo string `json:"repo"`

	// CoveredBy is, for StateCovered, the location that already holds it, with ~.
	CoveredBy string `json:"coveredBy"`
}

// checked is one location Check reads: its folder, absolute, and resolved through links,
// both "" for one that can't be read as a path, and how it reads.
type checked struct {
	dir      string
	resolved string
	state    LocationState
}

// Check reads how the clone root primary and each of the locations extra, both as stored,
// read now: the clone root first, then each stored entry in order, every one of them, so
// the settings can show what NewRoots drops. git, when set, reads a clone location's
// origin; nil leaves Repo "". The locations are read at once.
func Check(ctx context.Context, primary string, extra []Location, home string, git execpath.Runner) []LocationState {
	all := checkedOf(primary, extra, home)

	markCovered(all)

	var wg sync.WaitGroup

	for i := range all {
		c := &all[i]
		if c.dir != "" && c.state.State != StateCovered {
			wg.Go(func() { c.read(ctx, git) })
		}
	}

	wg.Wait()

	out := make([]LocationState, 0, len(all))
	for _, c := range all {
		out = append(out, c.state)
	}

	return out
}

// checkedOf lists the locations Check reads, each expanded, with no state read yet: one
// that can't be read as a path, or has no known kind, is unreadable.
func checkedOf(primary string, extra []Location, home string) []checked {
	all := make([]checked, 0, 1+len(extra))

	root, err := paths.ExpandHome(primary, home)
	if err != nil {
		all = append(all, checked{state: LocationState{Path: primary, Kind: KindFolder, Primary: true, State: StateUnreadable}})
	} else {
		all = append(all, checked{
			dir: root, resolved: paths.Resolved(root),
			state: LocationState{Path: paths.Tilde(root, home), Kind: KindFolder, Primary: true},
		})
	}

	for _, loc := range extra {
		c := checked{state: LocationState{Path: loc.Path, Kind: loc.Kind, State: StateUnreadable}}

		if dir, expandErr := paths.ExpandHome(loc.Path, home); expandErr == nil && loc.Kind.Valid() {
			c.dir, c.resolved = dir, paths.Resolved(dir)
		}

		all = append(all, c)
	}

	return all
}

// markCovered marks each location another already covers, from the paths alone: one the
// clone root or an earlier entry already is, or a clone directly inside a folder location.
func markCovered(all []checked) {
	for i := range all {
		c := &all[i]
		if c.dir == "" || c.state.Primary {
			continue
		}

		c.state.CoveredBy = coveredBy(all[:i], c.resolved, false)
		if c.state.CoveredBy == "" && c.state.Kind == KindClone {
			c.state.CoveredBy = coveredBy(all, paths.Resolved(filepath.Dir(c.dir)), true)
		}

		if c.state.CoveredBy != "" {
			c.state.State = StateCovered
		}
	}
}

// read reads how the location reads now: its state, a folder's clones, and a clone's
// origin.
func (c *checked) read(ctx context.Context, git execpath.Runner) {
	read := readPlace(Place{Path: c.dir, Kind: c.state.Kind, Primary: c.state.Primary})
	c.state.State = read.state

	switch {
	case c.state.Kind == KindFolder:
		c.state.Clones = len(read.dirs)
	case read.state == StateOK:
		c.state.Repo = originName(ctx, c.dir, git)
	}
}

// coveredBy returns the shown path of the first location in all whose resolved path is
// resolved, only a folder one when folders, or "".
func coveredBy(all []checked, resolved string, folders bool) string {
	for _, c := range all {
		if c.dir != "" && c.resolved == resolved && (!folders || c.state.Kind == KindFolder) {
			return c.state.Path
		}
	}

	return ""
}
