package hooks

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

// Revealer shows a file or a folder in Finder. Core adapts open -R, on macOS only.
type Revealer interface {
	Reveal(ctx context.Context, path string) error
}

// OpenTerminal opens the terminal chosen for launches in the clone the page lists at dir
// (with ~), for a row's Open in terminal: the user looks at the clone or fixes it there.
// The app runs nothing in it (scriptrun.OpenFolder). Only a clone directly under the clone
// root is opened.
func (s *Service) OpenTerminal(ctx context.Context, dir string) error {
	c, err := s.cloneAt(ctx, dir)
	if err != nil {
		return err
	}

	return scriptrun.OpenFolder(ctx, s.terminal, c.dir)
}

// ShowConfig shows the go-pre-commit config of the clone the page lists at dir (with ~) in
// Finder, for a row's Open the config: its .github/env folder, or the legacy
// .github/.env.base. Only a clone directly under the clone root, with a config, is shown.
func (s *Service) ShowConfig(ctx context.Context, dir string) error {
	c, err := s.cloneAt(ctx, dir)
	if err != nil {
		return err
	}

	path, _ := configSource(c.dir)
	if path == "" {
		return notSetUpError(c, s.home)
	}

	if s.reveal == nil {
		return uierr.New(brand.Name+" can't open Finder here.", ErrNoFinder)
	}

	if err = s.reveal.Reveal(ctx, path); err != nil {
		return fmt.Errorf("show the config: %w", err)
	}

	return nil
}
