package hooks

import (
	"context"

	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
)

// clone is a clone a request names: its folder, absolute, and its origin's repository on
// GitHub, or the zero Name.
type clone struct {
	dir  string
	name repo.Name
}

// label names the clone for the app's log: its repository, or its folder with ~.
func (c clone) label(home string) string {
	if c.name.Valid() {
		return c.name.String()
	}

	return paths.Tilde(c.dir, home)
}

// folders finds the clone a request names by its folder, as the page lists it, with ~:
// only a clone the repository locations hold is one. Its repository is the one the last
// scan read for it, else its origin's.
func (s *Service) folders() localrepos.ByFolder {
	return localrepos.ByFolder{
		Clones: s.clones, Home: s.home, Invalid: ErrInvalidRequest, NoClone: ErrNoClone, Known: s.scannedName,
	}
}

// cloneAt finds the clone a request names by its folder (see folders).
func (s *Service) cloneAt(ctx context.Context, shown string) (clone, error) {
	l, err := s.folders().List(ctx)
	if err != nil {
		return clone{}, err
	}

	return s.cloneIn(ctx, shown, l)
}

// cloneIn finds the clone shown names among l's (see folders).
func (s *Service) cloneIn(ctx context.Context, shown string, l localrepos.Listing) (clone, error) {
	p, err := s.folders().Pick(ctx, shown, l)
	if err != nil {
		return clone{}, err
	}

	return clone{dir: p.Dir, name: p.Name}, nil
}

// scannedName returns the repository the last scan read for the clone at dir, the zero
// Name for one whose origin isn't on GitHub, and whether the scan listed the clone.
func (s *Service) scannedName(dir string) (repo.Name, bool) {
	key := paths.Resolved(dir)

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.repos {
		if r.key != key {
			continue
		}

		if r.row.Repo == "" {
			return repo.Name{}, true
		}

		name, err := repo.Parse(r.row.Repo)

		return name, err == nil
	}

	return repo.Name{}, false
}
