package hooks

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// terminalScript names the script a terminal opened in a clone runs: no run owns it, so
// it takes run 0's prefix, which the launch folder's pruning knows.
const terminalScript = "run-0"

var (
	// ErrNoTerminal is returned where the user's terminal can't be opened.
	ErrNoTerminal = errors.New("the terminal can't be opened here")

	// ErrNoFinder is returned where Finder can't show a folder, as on Linux.
	ErrNoFinder = errors.New("finder can't show a folder here")
)

// FolderOpener opens the user's terminal in a folder, running nothing there. Core adapts
// the terminal the runs open in.
type FolderOpener interface {
	OpenFolder(ctx context.Context, name, dir string) error
}

// Revealer shows a file or a folder in Finder. Core adapts open -R, on macOS only.
type Revealer interface {
	Reveal(ctx context.Context, path string) error
}

// OpenTerminal opens the terminal chosen for launches in the clone the page lists at dir
// (with ~), for a row's Open in terminal: the user looks at the clone or fixes it there.
// The app runs nothing in it. Only a clone directly under the clone root is opened.
func (s *Service) OpenTerminal(ctx context.Context, dir string) error {
	c, err := s.cloneAt(ctx, dir)
	if err != nil {
		return err
	}

	if s.terminal == nil {
		return uierr.New("Your terminal can't be opened here.", ErrNoTerminal)
	}

	if err = s.terminal.OpenFolder(ctx, terminalScript, c.dir); err != nil {
		return fmt.Errorf("open the terminal in the clone: %w", err)
	}

	return nil
}

// ShowConfig shows the go-pre-commit config of the clone the page lists at dir (with ~) in
// Finder, for a row's Open the config: its .github/env folder, or the legacy
// .github/.env.base. Only a clone directly under the clone root, with a config, is shown.
func (s *Service) ShowConfig(ctx context.Context, dir string) error {
	c, err := s.cloneAt(ctx, dir)
	if err != nil {
		return err
	}

	path := configPath(c.dir)
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

// configPath is what shows the go-pre-commit config of the clone at dir: the modular
// folder when it holds a .env file, else the legacy .env.base, else "" for none.
func configPath(dir string) string {
	if folder := filepath.Join(dir, filepath.FromSlash(envDir)); len(modularFiles(folder)) > 0 {
		return folder
	}

	if base := filepath.Join(dir, filepath.FromSlash(envBase)); isRegular(base) {
		return base
	}

	return ""
}
