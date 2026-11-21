package launcher

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

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

	roots, err := prefs.roots(s.home, s.reader)
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

	if err = localrepos.CheckAddable(expanded, s.home); err != nil {
		return "", err
	}

	if _, err = s.cloneOf(ctx, expanded, name); err != nil {
		return "", err
	}

	return expanded, nil
}

// cloneOf reads the folder dir, absolute, as a clone of name, and returns it with its
// remote for name, or says in words the window shows why it isn't one: it isn't a clone,
// or no remote of its points at name.
func (s *Service) cloneOf(ctx context.Context, dir string, name repo.Name) (Clone, error) {
	shown := paths.Tilde(dir, s.home)

	local, err := localrepos.Inspect(ctx, dir, s.git)

	switch {
	case errors.Is(err, localrepos.ErrNotARepo):
		return Clone{}, uierr.New(fmt.Sprintf("%s isn't a clone of %s.", shown, name), fmt.Errorf("%w: %w", ErrNotTheClone, err))
	case err != nil:
		return Clone{}, err
	}

	remote, ok := local.RemoteFor(name)
	if !ok {
		return Clone{}, uierr.New(fmt.Sprintf("%s isn't a clone of %s; %s", shown, name, remotesWords(local, name)),
			fmt.Errorf("%w: %s", ErrNotTheClone, dir))
	}

	return Clone{Dir: local.Path, Remote: remote}, nil
}

// remotesWords says what a clone that isn't name's points at, ending a sentence: "its
// origin is acme-org/other.", "its remote mine is octo-user/other.", or "it has no remote
// on GitHub." A remote whose name starts with "-" is one RemoteFor never takes, since git
// would read the name as an option: one that points at name says so, and any other is
// left out.
func remotesWords(local localrepos.LocalRepo, name repo.Name) string {
	if origin, ok := local.Remotes["origin"]; ok {
		return "its origin is " + origin.String() + "."
	}

	names := make([]string, 0, len(local.Remotes))
	for remote := range local.Remotes {
		names = append(names, remote)
	}

	slices.Sort(names)

	for _, remote := range names {
		if strings.HasPrefix(remote, "-") && local.Remotes[remote].Key() == name.Key() {
			return fmt.Sprintf("its remote for it is named %q, which git would read as an option: rename the remote.",
				remote)
		}
	}

	names = slices.DeleteFunc(names, func(remote string) bool { return strings.HasPrefix(remote, "-") })

	if len(names) == 0 {
		return "it has no remote on GitHub."
	}

	return "its remote " + names[0] + " is " + local.Remotes[names[0]].String() + "."
}
