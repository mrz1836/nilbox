package integrations

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/uierr"
)

const (
	// osascript and openProgram are macOS's own programs, run by their paths.
	osascript   = "/usr/bin/osascript"
	openProgram = "/usr/bin/open"

	// automationDenied is the AppleScript error macOS returns when the user hasn't let
	// the app control Finder.
	automationDenied = "-1743"
)

// ErrAutomationDenied is returned by Trash when the user hasn't let the app control
// Finder; the window offers Reveal in Finder instead.
var ErrAutomationDenied = errors.New("not allowed to control Finder")

// Trash moves a copy of a tool to the Trash through Finder, so Finder's Put Back works:
// one the app installed, or another copy detection found, never anything else. The path
// is passed to the script as an argument, never written into it. When the user hasn't let
// the app control Finder, it returns ErrAutomationDenied.
func (s *Service) Trash(ctx context.Context, id integrationprefs.ID, path string) (Tools, error) {
	c, err := s.knownCopy(id, path)
	if err != nil {
		return Tools{}, err
	}

	// Finder would follow a link and trash what it points to, such as Homebrew's copy in
	// its Cellar, so a link, and a Homebrew copy, are left for the user to move.
	if c.Link || c.Source == SourceHomebrew {
		return Tools{}, uierr.New(fmt.Sprintf("%s is a link, so %s won't move it. Reveal it in Finder to move it yourself.",
			c.DisplayPath, brand.Name), fmt.Errorf("%w: %s is a link", ErrNotACopy, c.Path))
	}

	if s.commands == nil {
		return Tools{}, uierr.New(brand.Name+" can't move files to the Trash here.", ErrNotACopy)
	}

	_, stderr, err := s.commands.Run(ctx, osascript,
		"-e", "on run argv",
		"-e", `tell application "Finder" to delete (POSIX file (item 1 of argv) as alias)`,
		"-e", "end run",
		c.Path)
	if err != nil {
		if strings.Contains(string(stderr), automationDenied) {
			return Tools{}, uierr.New(brand.Name+" isn't allowed to control Finder, so it can't move this to the Trash.",
				fmt.Errorf("%w: %w", ErrAutomationDenied, err))
		}

		return Tools{}, uierr.New(fmt.Sprintf("Finder couldn't move %s to the Trash: %s", c.DisplayPath,
			firstLine(stderr, nil, err.Error())), err)
	}

	if err = s.detect(ctx); err != nil {
		return Tools{}, err
	}

	return s.List(ctx)
}

// Reveal shows a copy of a tool in Finder: one detection found, never anything else.
func (s *Service) Reveal(ctx context.Context, id integrationprefs.ID, path string) error {
	c, err := s.knownCopy(id, path)
	if err != nil {
		return err
	}

	if s.commands == nil {
		return uierr.New(brand.Name+" can't open Finder here.", ErrNotACopy)
	}

	if _, stderr, err := s.commands.Run(ctx, openProgram, "-R", c.Path); err != nil {
		return uierr.New(fmt.Sprintf("Finder couldn't show %s: %s", c.DisplayPath, firstLine(stderr, nil, err.Error())), err)
	}

	return nil
}

// knownCopy returns the copy of the tool at path that the last detection found: the one in
// use when the app installed it, or any other copy.
func (s *Service) knownCopy(id integrationprefs.ID, path string) (Copy, error) {
	s.mu.Lock()
	d, ok := s.detected[string(id)]
	s.mu.Unlock()

	refused := uierr.New("That isn't a copy of the tool "+brand.Name+" found.", fmt.Errorf("%w: %s", ErrNotACopy, path))

	if !ok || !d.has || path == "" {
		return Copy{}, refused
	}

	if paths.Same(d.selected.Path, path) && d.selected.Source == SourceApp {
		return d.selected, nil
	}

	for _, c := range d.others {
		if paths.Same(c.Path, path) {
			return c, nil
		}
	}

	return Copy{}, refused
}
