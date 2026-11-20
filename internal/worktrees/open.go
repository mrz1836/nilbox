package worktrees

import (
	"context"
	"errors"
	"fmt"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// ErrNoFinder is returned where Finder can't show a folder, as on Linux.
var ErrNoFinder = errors.New("finder can't show a folder here")

// FolderOpener opens the user's terminal in a folder, running nothing there, under the
// script name it is given (scriptrun.FolderOpener). Core adapts the terminal the runs open
// in.
type FolderOpener interface {
	OpenFolder(ctx context.Context, name, dir string) error
}

// Revealer shows a file or a folder in Finder. Core adapts open -R, on macOS only.
type Revealer interface {
	Reveal(ctx context.Context, path string) error
}

// OpenTerminal opens the terminal chosen for launches in the worktree at path, one of the
// linked worktrees of the clone at repoPath, for its Open in Terminal: the user looks at
// the worktree or works in it there. The app runs nothing in it (scriptrun.OpenFolder).
// The request is checked as Details checks it, so a worktree whose folder is gone is
// refused with ErrPrunable.
func (s *Service) OpenTerminal(ctx context.Context, repoPath, path string) error {
	_, e, err := s.linkedEntry(ctx, repoPath, path)
	if err != nil {
		return err
	}

	return scriptrun.OpenFolder(ctx, s.terminal, e.Path)
}

// Reveal shows the worktree at path, one of the linked worktrees of the clone at repoPath,
// in Finder, for its Reveal in Finder. The request is checked as Details checks it, so a
// worktree whose folder is gone is refused with ErrPrunable; where Finder can't show it,
// the error is ErrNoFinder, in words the window shows.
func (s *Service) Reveal(ctx context.Context, repoPath, path string) error {
	_, e, err := s.linkedEntry(ctx, repoPath, path)
	if err != nil {
		return err
	}

	if s.reveal == nil {
		return uierr.New(brand.Name+" can't open Finder here.", ErrNoFinder)
	}

	if err = s.reveal.Reveal(ctx, e.Path); err != nil {
		return fmt.Errorf("show the worktree in Finder: %w", err)
	}

	return nil
}
