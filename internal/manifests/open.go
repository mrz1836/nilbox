package manifests

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// FolderOpener opens the user's terminal in a folder, running nothing there. Core adapts
// the terminal the runs open in, under the name of its script that runs nothing, as it
// does for the worktrees.
type FolderOpener interface {
	OpenFolder(ctx context.Context, dir string) error
}

// Revealer shows a file or a folder in Finder. Core adapts open -R, on macOS only.
type Revealer interface {
	Reveal(ctx context.Context, path string) error
}

// OpenTerminal opens the terminal chosen for launches in the clone the page lists at dir
// (with ~), for a row's Open in terminal: the user looks at the clone, or upgrades it by
// hand, there, and the app runs nothing in it. Only a clone the repository locations hold
// is opened; where no terminal can be opened, the error is ErrNoTerminal, in words the
// window shows.
func (s *Service) OpenTerminal(ctx context.Context, dir string) error {
	c, err := s.folders().At(ctx, dir)
	if err != nil {
		return err
	}

	if s.terminal == nil {
		return uierr.New("Your terminal can't be opened here.", ErrNoTerminal)
	}

	if err = s.terminal.OpenFolder(ctx, c.Dir); err != nil {
		return fmt.Errorf("open the terminal in the clone: %w", err)
	}

	return nil
}

// Reveal shows a go.mod, package.json, or lockfile of the clone the page lists at dir
// (with ~) in Finder, for a module's Reveal in Finder, path being the file's path from
// the clone's top. path must be one gitread passes to git (ValidManifestPath or
// ValidLockfilePath: clean, relative, and naming no other file), the clone one the
// repository locations hold, and the file in its checkout now, inside the clone once its
// links are resolved. Finder only shows it: nothing reads it or runs it. Where Finder
// can't show it, the error is ErrNoFinder, in words the window shows.
func (s *Service) Reveal(ctx context.Context, dir, path string) error {
	if !gitread.ValidManifestPath(path) && !gitread.ValidLockfilePath(path) {
		return uierr.New("That isn't a go.mod, a package.json, or a lockfile in this clone.",
			fmt.Errorf("%w: the file %q", ErrInvalidRequest, path))
	}

	c, err := s.folders().At(ctx, dir)
	if err != nil {
		return err
	}

	file, err := fileIn(c.Dir, path)
	if err != nil {
		return err
	}

	if s.reveal == nil {
		return uierr.New(brand.Name+" can't open Finder here.", ErrNoFinder)
	}

	if err = s.reveal.Reveal(ctx, file); err != nil {
		return fmt.Errorf("show the file in Finder: %w", err)
	}

	return nil
}

// folders finds the clone a request names by its folder, as the page lists it, with ~:
// only a clone the repository locations hold is one.
func (s *Service) folders() localrepos.ByFolder {
	return localrepos.ByFolder{Clones: s.clones, Home: s.home, Invalid: ErrInvalidRequest, NoClone: ErrNoClone}
}

// fileIn returns the file at rel, a path from the top of the clone at dir, refusing one
// that isn't in the clone's checkout now, or that leads outside the clone once its links
// are resolved.
func fileIn(dir, rel string) (string, error) {
	file := filepath.Join(dir, filepath.FromSlash(rel))

	if _, err := os.Lstat(file); err != nil {
		return "", uierr.New(rel+" isn't in this clone's checkout: the branch it's on may not have it.",
			fmt.Errorf("%w: %w", ErrInvalidRequest, err))
	}

	if root := paths.Resolved(dir); !strings.HasPrefix(paths.Resolved(file), root+string(filepath.Separator)) {
		return "", uierr.New(rel+" leads outside this clone.", fmt.Errorf("%w: the file %q", ErrInvalidRequest, rel))
	}

	return file, nil
}
