package release

import (
	"context"
	"errors"
	"fmt"

	"github.com/mrz1836/nilbox/internal/uierr"
)

// terminalScript names the script a terminal opened in a clone runs: no run owns it, so
// it takes run 0's prefix, which the launch folder's pruning knows.
const terminalScript = "run-0"

// ErrNoTerminal is returned where the user's terminal can't be opened.
var ErrNoTerminal = errors.New("the terminal can't be opened here")

// FolderOpener opens the user's terminal in a folder, running nothing there. Core adapts
// the terminal the runs open in.
type FolderOpener interface {
	OpenFolder(ctx context.Context, name, dir string) error
}

// OpenTerminal opens the terminal chosen for launches in the clone of repoText, the one
// the page shows at dir (with ~ for home), for a check's Open in terminal: the user
// commits, pushes, or deletes a tag there. The app runs nothing in it. Only a clone the
// Release page lists, or the clone root holds, with that GitHub origin, is opened.
func (s *Service) OpenTerminal(ctx context.Context, repoText, dir string) error {
	name, err := s.checkRequest(repoText, "", "")
	if err != nil {
		return err
	}

	clone, err := s.cloneFor(ctx, name, dir)
	if err != nil {
		return err
	}

	if s.terminal == nil {
		return uierr.New("Your terminal can't be opened here.", ErrNoTerminal)
	}

	if err = s.terminal.OpenFolder(ctx, terminalScript, clone); err != nil {
		return fmt.Errorf("open the terminal in the clone: %w", err)
	}

	return nil
}
