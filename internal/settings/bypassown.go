package settings

import (
	"context"
	"fmt"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
)

// KeyBypassOwn is whether the review sheet's button merges the user's own pull request past
// the branch's rules when an approval from someone else is all it lacks and GitHub lets the
// user merge past them. The sheet still asks the user to accept each rule it skips.
const KeyBypassOwn = "review.bypassOwn"

// DefaultBypassOwn is whether a fresh database makes merging past the rules the button on
// the user's own pull request: it doesn't.
const DefaultBypassOwn = false

// BypassOwn returns whether the review sheet's button merges the user's own pull request
// past the rules when it waits only on an approval: the stored setting, or
// DefaultBypassOwn when none is stored or it cannot be read.
func (s *Service) BypassOwn(ctx context.Context) (bool, error) {
	stored, _, err := s.prefs.Get(ctx, KeyBypassOwn)
	if err != nil {
		return DefaultBypassOwn, fmt.Errorf("read whether to merge your own pull requests past the rules: %w", err)
	}

	return prefval.Bool(stored, DefaultBypassOwn), nil
}

// SetBypassOwn stores whether the review sheet's button merges the user's own pull request
// past the rules when an approval from someone else is all it lacks, and returns the
// settings with it.
func (s *Service) SetBypassOwn(ctx context.Context, on bool) (Settings, error) {
	return s.save(ctx, setting(KeyBypassOwn, prefval.EncodeBool(on)))
}
