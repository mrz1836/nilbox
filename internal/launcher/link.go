package launcher

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// ErrNotTheClone is returned by LinkClone for a folder that isn't a clone of the item's
// repository.
var ErrNotTheClone = errors.New("not a clone of the repository")

// LinkedClone is a clone of an item's repository the user found outside the repository
// locations, ready for the window to add as a location.
type LinkedClone struct {
	// Dir is the clone, with ~ for home.
	Dir string `json:"dir"`

	// Location is what adding the clone as a repository location would add: the clone
	// alone, its folder when that holds other clones (Location.Parent), or nothing new
	// when the locations already hold it (Location.Covered).
	Location localrepos.Preview `json:"location"`
}

// LinkClone checks that the folder dir, which the user picked, is a clone of the item's
// repository, remembers it as the repository's clone, and says what adding it as a
// repository location would add, for the launch that found no clone. It refuses a folder
// that isn't a clone, a linked worktree, and a clone with no remote pointing at the
// repository, in words the window shows, and stores no location: the window adds it
// through the settings, which alone write the locations, then launches again.
func (s *Service) LinkClone(ctx context.Context, itemID int64, dir string) (LinkedClone, error) {
	prefs, err := s.settings.Get(ctx)
	if err != nil {
		return LinkedClone{}, err
	}

	row, err := s.loadItem(ctx, itemID)
	if err != nil {
		return LinkedClone{}, err
	}

	ref := row.repoRef()
	if _, err = repo.New(ref.name.Owner, ref.name.Repo); err != nil {
		return LinkedClone{}, err
	}

	clone, err := s.checkLinked(ctx, dir, ref.name)
	if err != nil {
		return LinkedClone{}, err
	}

	roots, err := prefs.roots(s.home)
	if err != nil {
		return LinkedClone{}, err
	}

	preview, err := localrepos.Survey(ctx, clone, roots, s.home, s.git)
	if err != nil {
		return LinkedClone{}, err
	}

	s.cacheClonePath(ctx, ref.id, clone)

	return LinkedClone{Dir: paths.Tilde(clone, s.home), Location: preview}, nil
}

// checkLinked checks that dir is a clone of name that can be a repository location, and
// returns it, absolute.
func (s *Service) checkLinked(ctx context.Context, dir string, name repo.Name) (string, error) {
	expanded, err := paths.ExpandHome(dir, s.home)
	if err != nil {
		return "", uierr.New("Choose the clone's folder.", fmt.Errorf("%w: %w", ErrNotTheClone, err))
	}

	shown := paths.Tilde(expanded, s.home)

	if err = localrepos.CheckAddable(expanded, s.home); err != nil {
		return "", err
	}

	local, err := localrepos.Inspect(ctx, expanded, s.git)

	switch {
	case errors.Is(err, localrepos.ErrNotARepo):
		return "", uierr.New(fmt.Sprintf("%s isn't a clone of %s.", shown, name), fmt.Errorf("%w: %w", ErrNotTheClone, err))
	case err != nil:
		return "", err
	}

	if _, ok := local.RemoteFor(name); !ok {
		return "", uierr.New(fmt.Sprintf("%s isn't a clone of %s; %s", shown, name, remotesWords(local)),
			fmt.Errorf("%w: %s", ErrNotTheClone, expanded))
	}

	return expanded, nil
}

// remotesWords says what a clone that isn't the repository's points at, ending a sentence:
// "its origin is acme-org/other.", or "it has no remote on GitHub."
func remotesWords(local localrepos.LocalRepo) string {
	if origin, ok := local.Remotes["origin"]; ok {
		return "its origin is " + origin.String() + "."
	}

	names := make([]string, 0, len(local.Remotes))
	for remote := range local.Remotes {
		names = append(names, remote)
	}

	if len(names) == 0 {
		return "it has no remote on GitHub."
	}

	slices.Sort(names)

	return "its remote " + names[0] + " is " + local.Remotes[names[0]].String() + "."
}
