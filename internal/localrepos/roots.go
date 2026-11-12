package localrepos

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/mrz1836/nilbox/internal/paths"
)

// otherLocations is how a message names the user's other repository locations, after the
// clone root.
const otherLocations = " or your other repository locations"

// State is how a repository location reads now.
type State string

const (
	// StateOK is a location that reads as it did when it was added.
	StateOK State = "ok"

	// StateMissing is a location that isn't there, as on a drive that is unplugged.
	StateMissing State = "missing"

	// StateNotFolder is a location that is now a file.
	StateNotFolder State = "notFolder"

	// StateUnreadable is a location that couldn't be read.
	StateUnreadable State = "unreadable"

	// StateNotAClone is a clone location that no longer holds a .git.
	StateNotAClone State = "notAClone"

	// StateNowAClone is a folder location that holds a .git now. It is still looked into.
	StateNowAClone State = "nowAClone"

	// StateCovered is a location another one already covers: the clone root, a clone
	// directly inside the clone root or a folder location, or one listed twice.
	StateCovered State = "covered"

	// StateNotAnswering is a location whose read took longer than its Reader's limit, as on
	// a network share that hangs. It is skipped, and read again next time.
	StateNotAnswering State = "notAnswering"
)

// Place is one location Roots reads: its path, absolute and with ~ expanded, its kind,
// and whether it is the clone root.
type Place struct {
	Path    string
	Kind    Kind
	Primary bool
}

// place is a Place with its path resolved through links once, and whether that took
// longer than the Reader's limit, which leaves its path as it is.
type place struct {
	Place

	resolved string
	stalled  bool
}

// Roots is where the user's clones are: the clone root, the primary, which new clones go
// in, and the other locations the user added, each expanded and its links resolved once.
// The zero Roots has no primary; NewRoots builds one.
type Roots struct {
	places []place

	// reader bounds the reads of the other locations; nil reads them directly.
	reader *Reader
}

// RootsOption configures the Roots NewRoots builds.
type RootsOption func(*Roots)

// WithReader reads the other locations through reader, which bounds how long each may
// take, the resolution of their links included; the clone root is still read directly. It
// changes neither Key nor Equal.
func WithReader(reader *Reader) RootsOption {
	return func(r *Roots) { r.reader = reader }
}

// NewRoots builds the Roots of the clone root primary and the other locations extra, both
// as stored, with ~ for home. Only the primary can fail; an extra that can't be expanded,
// has no known kind, is the primary, or repeats another is dropped. With a Reader
// (WithReader), the other locations' links are resolved through it, at once: one that
// doesn't answer is kept as it is written, and Clones skips it.
func NewRoots(primary string, extra []Location, home string, opts ...RootsOption) (Roots, error) {
	root, err := paths.ExpandHome(primary, home)
	if err != nil {
		return Roots{}, fmt.Errorf("clone root: %w", err)
	}

	var r Roots

	for _, opt := range opts {
		opt(&r)
	}

	r.places = make([]place, 0, 1+len(extra))
	r.places = append(r.places, place{Place: Place{Path: root, Kind: KindFolder, Primary: true}, resolved: paths.Resolved(root)})

	for _, p := range r.resolveAll(extraPlaces(extra, home)) {
		if !r.has(p.resolved) {
			r.places = append(r.places, p)
		}
	}

	return r, nil
}

// extraPlaces expands the other locations, dropping one that can't be expanded or has no
// known kind.
func extraPlaces(extra []Location, home string) []place {
	out := make([]place, 0, len(extra))

	for _, loc := range extra {
		if dir, err := paths.ExpandHome(loc.Path, home); err == nil && loc.Kind.Valid() {
			out = append(out, place{Place: Place{Path: dir, Kind: loc.Kind}})
		}
	}

	return out
}

// Primary returns the clone root, absolute, or "" for the zero Roots.
func (r Roots) Primary() string {
	if len(r.places) == 0 {
		return ""
	}

	return r.places[0].Path
}

// Places returns every location, the clone root first, then the others in the order
// they are stored.
func (r Roots) Places() []Place {
	out := make([]Place, 0, len(r.places))
	for _, p := range r.places {
		out = append(out, p.Place)
	}

	return out
}

// Several reports whether there are other locations beside the clone root.
func (r Roots) Several() bool {
	return len(r.places) > 1
}

// Key names the locations as one string, for a cache keyed by where it read: the clone
// root alone when there are no others, so a cache keyed by the clone root stays valid.
func (r Roots) Key() string {
	if len(r.places) == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString(r.places[0].Path)

	for _, p := range r.places[1:] {
		b.WriteString("\n" + string(p.Kind) + ":" + p.Path)
	}

	return b.String()
}

// Equal reports whether r and o are the same locations, in the same order.
func (r Roots) Equal(o Roots) bool {
	return r.Key() == o.Key()
}

// Shown returns every location's path with ~ for home, the clone root first; never nil.
func (r Roots) Shown(home string) []string {
	out := make([]string, 0, len(r.places))
	for _, p := range r.places {
		out = append(out, paths.Tilde(p.Path, home))
	}

	return out
}

// Where names the locations in a sentence: "~/projects", or "~/projects or your other
// repository locations" when there are others.
func (r Roots) Where(home string) string {
	where := paths.Tilde(r.Primary(), home)
	if r.Several() {
		where += otherLocations
	}

	return where
}

// Holds reports whether dir, absolute and not hidden, is a clone the locations hold: its
// parent is the clone root or a folder location, or it is itself a clone location. It
// reads no git, and only resolves dir's links.
func (r Roots) Holds(dir string) bool {
	clean := filepath.Clean(dir)
	if !filepath.IsAbs(clean) || hidden(clean) {
		return false
	}

	if r.folderAt(paths.Resolved(filepath.Dir(clean))) {
		return true
	}

	if !slices.ContainsFunc(r.places, func(p place) bool { return p.Kind == KindClone }) {
		return false
	}

	self := paths.Resolved(clean)

	return slices.ContainsFunc(r.places, func(p place) bool { return p.Kind == KindClone && p.resolved == self })
}

// CloneAt reports whether dir is one of the clone locations, as its path is stored.
func (r Roots) CloneAt(dir string) bool {
	clean := filepath.Clean(dir)

	for _, p := range r.places {
		if p.Kind == KindClone && p.Path == clean {
			return true
		}
	}

	return false
}

// Unread is a location Clones couldn't read, and why.
type Unread struct {
	// Dir is the location, absolute.
	Dir string

	// State is why it wasn't read: StateMissing, StateNotFolder, StateUnreadable,
	// StateNotAClone, or StateNotAnswering.
	State State
}

// Words says why the location wasn't read, with ~ for home: "~/x wasn't found".
func (u Unread) Words(home string) string {
	shown := paths.Tilde(u.Dir, home)

	switch u.State {
	case StateMissing:
		return shown + " wasn't found"
	case StateNotFolder:
		return shown + " isn't a folder"
	case StateNotAClone:
		return shown + " is no longer a clone"
	case StateNotAnswering:
		return shown + " didn't answer in time"
	case StateUnreadable, StateOK, StateNowAClone, StateCovered:
		return shown + " couldn't be read"
	}

	return shown + " couldn't be read"
}

// Found is what Clones found: the clones, and the other locations it couldn't read.
type Found struct {
	// Dirs are the clones: the clone root's by name, then each other location's in the
	// order they are stored, a folder's by name. A clone two locations reach is listed
	// once, where it is reached first.
	Dirs []string

	// Skipped are the other locations that couldn't be read, in the order they are stored.
	Skipped []Unread
}

// Clones lists the clones in every location, reading no git: the clones directly inside
// the clone root and each folder location (see folderClones), and each clone location
// that still holds a .git. The locations are read at once, the others through the Roots'
// Reader, if any. A clone root that can't be read is an error, as it always was; another
// location that can't be read, or doesn't answer in time, is skipped and named in
// Found.Skipped.
func (r Roots) Clones(ctx context.Context) (Found, error) {
	if len(r.places) == 0 {
		return Found{}, fmt.Errorf("read clone root: %w", paths.ErrEmpty)
	}

	reads := make([]placeRead, len(r.places))

	var wg sync.WaitGroup

	for i, p := range r.places {
		switch {
		case p.Primary:
			wg.Go(func() { reads[i] = readPlace(p.Place) })
		case p.stalled:
			reads[i] = placeRead{state: StateNotAnswering}
		default:
			wg.Go(func() { reads[i] = r.reader.within(ctx, p.Place) })
		}
	}

	wg.Wait()

	if err := ctx.Err(); err != nil {
		return Found{}, err
	}

	if err := reads[0].err; err != nil {
		return Found{}, fmt.Errorf("read clone root: %w", err)
	}

	found := Found{Dirs: append([]string{}, reads[0].dirs...), Skipped: []Unread{}}
	if !r.Several() {
		return found, nil
	}

	seen := resolvedSet(found.Dirs)

	for i, read := range reads[1:] {
		if !read.state.listed() {
			found.Skipped = append(found.Skipped, Unread{Dir: r.places[i+1].Path, State: read.state})

			continue
		}

		fresh := slices.DeleteFunc(slices.Clone(read.dirs), func(dir string) bool { return seen[paths.Resolved(dir)] })
		found.Dirs = append(found.Dirs, fresh...)

		maps.Copy(seen, resolvedSet(fresh))
	}

	return found, nil
}

// resolveAll resolves each place's links: directly, one after another, without a Reader,
// or through it, all at once.
func (r Roots) resolveAll(places []place) []place {
	if r.reader == nil {
		for i := range places {
			places[i].resolved = paths.Resolved(places[i].Path)
		}

		return places
	}

	var wg sync.WaitGroup

	for i := range places {
		p := &places[i]

		wg.Go(func() {
			var answered bool

			p.resolved, answered = r.reader.Resolved(p.Path)
			p.stalled = !answered
		})
	}

	wg.Wait()

	return places
}

// folderAt reports a folder location, the clone root among them, whose resolved path is
// resolved.
func (r Roots) folderAt(resolved string) bool {
	return slices.ContainsFunc(r.places, func(p place) bool { return p.Kind == KindFolder && p.resolved == resolved })
}

// has reports a place whose resolved path is resolved.
func (r Roots) has(resolved string) bool {
	return slices.ContainsFunc(r.places, func(p place) bool { return p.resolved == resolved })
}

// resolvedSet holds each of dirs resolved through links.
func resolvedSet(dirs []string) map[string]bool {
	set := make(map[string]bool, len(dirs))
	for _, dir := range dirs {
		set[paths.Resolved(dir)] = true
	}

	return set
}

// listed reports a state whose location's clones are listed: one that reads as it did,
// or a folder that is now a clone itself, which is still looked into.
func (s State) listed() bool {
	return s == StateOK || s == StateNowAClone
}

// placeRead is what reading one location found: its clones, how it reads, and, for a
// folder that couldn't be listed, the error.
type placeRead struct {
	dirs  []string
	state State
	err   error
}

// readPlace reads one location: a folder's clones, or a clone location itself.
func readPlace(p Place) placeRead {
	if p.Kind == KindClone {
		if isRepo(p.Path) {
			return placeRead{dirs: []string{p.Path}, state: StateOK}
		}

		return placeRead{state: notAClone(p.Path)}
	}

	dirs, err := folderClones(p.Path)
	if err != nil {
		return placeRead{state: unlisted(p.Path), err: err}
	}

	if !p.Primary && hasGit(p.Path) {
		return placeRead{dirs: dirs, state: StateNowAClone}
	}

	return placeRead{dirs: dirs, state: StateOK}
}

// notAClone says why the clone location at dir, which holds no .git, isn't one: it is
// missing, a file, unreadable, or a folder that is no longer a clone.
func notAClone(dir string) State {
	if state := statState(dir); state != StateOK {
		return state
	}

	return StateNotAClone
}

// unlisted says why the folder location at dir couldn't be listed.
func unlisted(dir string) State {
	if state := statState(dir); state != StateOK {
		return state
	}

	return StateUnreadable
}

// statState reads how the folder at dir stands with stat alone.
func statState(dir string) State {
	info, err := os.Stat(dir)

	switch {
	case errors.Is(err, fs.ErrNotExist):
		return StateMissing
	case err != nil:
		return StateUnreadable
	case !info.IsDir():
		return StateNotFolder
	default:
		return StateOK
	}
}

// folderClones lists the git working trees directly inside dir: each entry, not hidden
// (its name does not start with a dot), that is a folder or a link to one and holds a .git
// folder or file, in the order of their names. Nested repositories are not searched. The
// error is reading dir itself.
func folderClones(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var out []string

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		if clone := filepath.Join(dir, entry.Name()); isRepo(clone) {
			out = append(out, clone)
		}
	}

	return out, nil
}
