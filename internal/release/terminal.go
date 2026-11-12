package release

import (
	"context"

	"github.com/mrz1836/nilbox/internal/scriptrun"
)

// OpenTerminal opens the terminal chosen for launches in the clone of repoText, the one
// the page shows at dir (with ~ for home), for a check's Open in terminal: the user
// commits, pushes, or deletes a tag there. The app runs nothing in it
// (scriptrun.OpenFolder). Only a clone the Release page lists, or the clone root holds,
// with that GitHub origin, is opened.
func (s *Service) OpenTerminal(ctx context.Context, repoText, dir string) error {
	name, err := s.checkRequest(repoText, "", "")
	if err != nil {
		return err
	}

	clone, err := s.cloneFor(ctx, name, dir)
	if err != nil {
		return err
	}

	return scriptrun.OpenFolder(ctx, s.terminal, clone)
}
