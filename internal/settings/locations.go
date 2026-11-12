package settings

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// KeyCloneLocations is the user's other repository locations, beside the clone root: a
// JSON list of {path, kind}, each path with ~ for home, in the order they were added.
const KeyCloneLocations = "clones.locations"

// ErrInvalidCloneLocation is returned for a repository location that can't be added: a
// path that isn't one, a folder that doesn't exist or can't be a location, or one the
// locations already cover.
var ErrInvalidCloneLocation = errors.New("invalid repository location")

// AddCloneLocations adds each of dirs to the other repository locations, all or none,
// and returns the settings with them; one call serves a single add, the suggestions the
// preview offers, and a launch's clone found elsewhere. Each is trimmed, expanded, and
// stored with ~, as a folder (looked into one level deep) or a single clone, by what it is
// now (localrepos.Classify); a hidden folder, one inside the app's worktrees folder or a
// git folder, a linked worktree, the clone root, a clone the locations already hold, and
// a folder that doesn't answer within the Reader's limit (WithReader) are refused, in
// words the window shows. A location already listed is classified again, which is what
// Check again does. A folder added drops the clone locations directly inside it, which it
// now covers.
func (s *Service) AddCloneLocations(ctx context.Context, dirs []string) (Settings, error) {
	adds, err := s.checkLocations(ctx, dirs)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, func(ctx context.Context, p *prefs.Store) error {
		stored, err := p.GetMany(ctx, KeyCloneRoot, KeyCloneLocations)
		if err != nil {
			return fmt.Errorf("read the repository locations: %w", err)
		}

		root, list := storedCloneRoot(stored), decodeCloneLocations(stored[KeyCloneLocations])

		for _, add := range adds {
			if list, err = s.addLocation(root, list, add); err != nil {
				return err
			}
		}

		encoded, err := store.EncodeJSON(list)
		if err != nil {
			return err
		}

		return p.Set(ctx, KeyCloneLocations, encoded)
	})
}

// RemoveCloneLocation drops dir from the other repository locations, by its stored text
// or as the same folder, and returns the settings without it. A location that isn't
// listed is no error. Its clones and their worktrees are left as they are, but no listing
// or cleanup reaches them until the location is added again.
func (s *Service) RemoveCloneLocation(ctx context.Context, dir string) (Settings, error) {
	text := strings.TrimSpace(dir)
	expanded, expandErr := paths.ExpandHome(text, s.home)

	// Resolved once, before the transaction: the location may be on a share that hangs.
	var target folder
	if expandErr == nil {
		target = s.folderOf(expanded)
	}

	return s.save(ctx, func(ctx context.Context, p *prefs.Store) error {
		err := p.Update(ctx, KeyCloneLocations, func(stored string, _ bool) (string, error) {
			list := slices.DeleteFunc(decodeCloneLocations(stored), func(loc localrepos.Location) bool {
				return loc.Path == text || (expandErr == nil && s.sameLocation(loc.Path, target))
			})

			return store.EncodeJSON(list)
		})
		if err != nil {
			return fmt.Errorf("save the repository locations: %w", err)
		}

		return nil
	})
}

// PreviewCloneLocation says what adding dir as a repository location would add, and what
// the app suggests instead where it finds a better choice, before anything is stored (see
// localrepos.Survey). It reads the folder and its subfolders with stat alone, and a
// clone's origin with one read-only git command when the Service has git (WithGit).
func (s *Service) PreviewCloneLocation(ctx context.Context, dir string) (localrepos.Preview, error) {
	expanded, err := expandLocation(dir, s.home)
	if err != nil {
		return localrepos.Preview{}, err
	}

	roots, err := s.storedRoots(ctx)
	if err != nil {
		return localrepos.Preview{}, err
	}

	return localrepos.Survey(ctx, expanded, roots, s.home, s.git)
}

// CheckCloneLocations reads how the clone root and each of the other repository
// locations read now, the clone root first: whether each is there, a folder's clones, and
// a clone's origin (see localrepos.Check).
func (s *Service) CheckCloneLocations(ctx context.Context) ([]localrepos.LocationState, error) {
	stored, err := s.prefs.GetMany(ctx, KeyCloneRoot, KeyCloneLocations)
	if err != nil {
		return nil, fmt.Errorf("read the repository locations: %w", err)
	}

	return localrepos.Check(ctx, storedCloneRoot(stored), decodeCloneLocations(stored[KeyCloneLocations]), s.home,
		s.git, s.reader), nil
}

// locationAdd is a location to add: its folder, absolute and resolved through its links,
// and what it is now.
type locationAdd struct {
	dir    string
	folder folder
	kind   localrepos.Kind
}

// checkLocations checks each of dirs as checkLocation does, refusing none at all.
func (s *Service) checkLocations(ctx context.Context, dirs []string) ([]locationAdd, error) {
	if len(dirs) == 0 {
		return nil, uierr.New("Choose a folder to add.", ErrInvalidCloneLocation)
	}

	adds := make([]locationAdd, 0, len(dirs))

	for _, dir := range dirs {
		add, err := s.checkLocation(ctx, dir)
		if err != nil {
			return nil, err
		}

		adds = append(adds, add)
	}

	return adds, nil
}

// checkLocation checks a location to add as it is typed, before anything is read from
// the database: a path to a folder that exists and can be a location, which answers within
// the Reader's limit (WithReader).
func (s *Service) checkLocation(ctx context.Context, dir string) (locationAdd, error) {
	expanded, err := expandLocation(dir, s.home)
	if err != nil {
		return locationAdd{}, err
	}

	kind, err := s.reader.Addable(ctx, expanded, s.home)
	if err != nil {
		return locationAdd{}, fmt.Errorf("%w: %w", ErrInvalidCloneLocation, err)
	}

	return locationAdd{dir: expanded, folder: s.folderOf(expanded), kind: kind}, nil
}

// addLocation adds add to list, the locations stored beside the clone root root: it
// classifies one already listed again, refuses one the locations cover, and otherwise
// appends it with ~, dropping the clone locations a folder covers.
func (s *Service) addLocation(root string, list []localrepos.Location, add locationAdd) ([]localrepos.Location, error) {
	if i := slices.IndexFunc(list, func(loc localrepos.Location) bool { return s.sameLocation(loc.Path, add.folder) }); i >= 0 {
		list[i].Kind = add.kind

		return s.pruneCovered(list, add), nil
	}

	roots, err := localrepos.NewRoots(root, list, s.home, localrepos.WithReader(s.reader))
	if err != nil {
		return nil, err
	}

	if why, _ := roots.Covering(add.dir, add.kind, s.home); why != "" {
		return nil, uierr.New(why, fmt.Errorf("%w: %s", ErrInvalidCloneLocation, add.dir))
	}

	list = append(list, localrepos.Location{Path: paths.Tilde(add.dir, s.home), Kind: add.kind})

	return s.pruneCovered(list, add), nil
}

// pruneCovered drops the clone locations directly inside add, when it is a folder.
func (s *Service) pruneCovered(list []localrepos.Location, add locationAdd) []localrepos.Location {
	if add.kind != localrepos.KindFolder {
		return list
	}

	return slices.DeleteFunc(list, func(loc localrepos.Location) bool {
		dir, err := paths.ExpandHome(loc.Path, s.home)

		return err == nil && loc.Kind == localrepos.KindClone && s.same(filepath.Dir(dir), add.folder)
	})
}

// folder is a folder a change names: its path, cleaned, and resolved through its links
// once, through the Reader, which answered unless it took longer than its limit.
type folder struct {
	clean    string
	resolved string
	answered bool
}

// folderOf resolves the folder dir once, through the Reader.
func (s *Service) folderOf(dir string) folder {
	resolved, answered := s.reader.Resolved(dir)

	return folder{clean: filepath.Clean(dir), resolved: resolved, answered: answered}
}

// sameLocation reports whether stored, a location as stored, names the folder f.
func (s *Service) sameLocation(stored string, f folder) bool {
	expanded, err := paths.ExpandHome(stored, s.home)

	return err == nil && s.same(expanded, f)
}

// same reports whether path names the folder f: the same path once cleaned, or once both
// are resolved through their links, through the Reader. A folder whose links don't resolve
// in time is the same only by its path.
func (s *Service) same(path string, f folder) bool {
	if filepath.Clean(path) == f.clean {
		return true
	}

	if !f.answered {
		return false
	}

	resolved, answered := s.reader.Resolved(path)

	return answered && resolved == f.resolved
}

// storedRoots reads the clone root and the other locations as stored.
func (s *Service) storedRoots(ctx context.Context) (localrepos.Roots, error) {
	stored, err := s.prefs.GetMany(ctx, KeyCloneRoot, KeyCloneLocations)
	if err != nil {
		return localrepos.Roots{}, fmt.Errorf("read the repository locations: %w", err)
	}

	return localrepos.NewRoots(storedCloneRoot(stored), decodeCloneLocations(stored[KeyCloneLocations]), s.home,
		localrepos.WithReader(s.reader))
}

// expandLocation expands a location as it is typed, refusing one that isn't a path in
// words the window shows.
func expandLocation(dir, home string) (string, error) {
	expanded, err := paths.ExpandHome(dir, home)

	switch {
	case errors.Is(err, paths.ErrEmpty):
		return "", uierr.New("Choose a folder, or type its path.", fmt.Errorf("%w: %w", ErrInvalidCloneLocation, err))
	case err != nil:
		return "", uierr.New("Type the folder's full path, or one starting with ~/.",
			fmt.Errorf("%w: %w", ErrInvalidCloneLocation, err))
	}

	return expanded, nil
}

// storedCloneRoot returns the clone root as stored, or DefaultCloneRoot.
func storedCloneRoot(stored map[string]string) string {
	if v := strings.TrimSpace(stored[KeyCloneRoot]); v != "" {
		return v
	}

	return DefaultCloneRoot
}

// decodeCloneLocations reads the stored locations, leniently: text that isn't a list of
// them reads as none, and an entry with an unknown kind, or a path that is neither
// absolute nor ~-relative, is dropped. The list is never nil.
func decodeCloneLocations(raw string) []localrepos.Location {
	out := []localrepos.Location{}

	if strings.TrimSpace(raw) == "" {
		return out
	}

	stored, err := store.DecodeJSON[[]localrepos.Location](raw)
	if err != nil {
		return out
	}

	for _, loc := range stored {
		path := cleanLocation(loc.Path)
		if !loc.Kind.Valid() || (path != "~" && !strings.HasPrefix(path, "~/") && !filepath.IsAbs(path)) {
			continue
		}

		out = append(out, localrepos.Location{Path: path, Kind: loc.Kind})
	}

	return out
}

// cleanLocation trims and cleans a stored path until neither changes it, so a path
// decoded once decodes to itself again.
func cleanLocation(path string) string {
	for {
		next := strings.TrimSpace(filepath.Clean(strings.TrimSpace(path)))
		if next == path {
			return path
		}

		path = next
	}
}
