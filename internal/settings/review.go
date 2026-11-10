package settings

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// KeyMergeMethod is how the review sheet merges a pull request, stored as one of the
// MergeMethod choices.
const KeyMergeMethod = "review.mergeMethod"

// MergeMethod is how the review sheet merges a pull request: as the repository offers
// first, or with one method wherever the repository allows it.
type MergeMethod string

// Merge method choices.
const (
	// MergeMethodRepo merges with the method GitHub offers first in each repository.
	MergeMethodRepo MergeMethod = "repo"

	// MergeMethodSquash, MergeMethodMerge, and MergeMethodRebase merge with that method
	// (squash, a merge commit, or a rebase) where the repository allows it, and as the
	// repository offers first elsewhere.
	MergeMethodSquash MergeMethod = "squash"
	MergeMethodMerge  MergeMethod = "merge_commit"
	MergeMethodRebase MergeMethod = "rebase_merge"
)

// DefaultMergeMethod is the merge method of a fresh database.
const DefaultMergeMethod = MergeMethodRepo

// ErrInvalidMergeMethod is returned for a merge method that is not one of the choices.
var ErrInvalidMergeMethod = errors.New("unknown merge method")

// mergeMethods returns the merge method choices in the order Settings offers them.
func mergeMethods() []MergeMethod {
	return []MergeMethod{MergeMethodRepo, MergeMethodSquash, MergeMethodMerge, MergeMethodRebase}
}

// decodeMergeMethod reads a stored merge method, falling back to DefaultMergeMethod for
// one that is unset or no longer a choice.
func decodeMergeMethod(stored string) MergeMethod {
	if m := MergeMethod(stored); slices.Contains(mergeMethods(), m) {
		return m
	}

	return DefaultMergeMethod
}

// MergeMethod returns the stored merge method, or DefaultMergeMethod when none is stored
// or the stored value is no longer a choice.
func (s *Service) MergeMethod(ctx context.Context) (MergeMethod, error) {
	stored, _, err := s.prefs.Get(ctx, KeyMergeMethod)
	if err != nil {
		return DefaultMergeMethod, fmt.Errorf("read the merge method: %w", err)
	}

	return decodeMergeMethod(stored), nil
}

// SetMergeMethod stores how the review sheet merges: MergeMethodRepo, MergeMethodSquash,
// MergeMethodMerge, or MergeMethodRebase, and returns the settings with it.
func (s *Service) SetMergeMethod(ctx context.Context, method MergeMethod) (Settings, error) {
	if !slices.Contains(mergeMethods(), method) {
		return Settings{}, fmt.Errorf("%w %q: choose one of %s", ErrInvalidMergeMethod, method,
			textutil.Join(mergeMethods(), ", "))
	}

	return s.save(ctx, setting(KeyMergeMethod, string(method)))
}
