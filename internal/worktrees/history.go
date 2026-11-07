package worktrees

import (
	"context"
	"path/filepath"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// recentLimit is how many cleanups RecentCleanups returns.
const recentLimit = 50

// RecentCleanups returns the worktrees the app cleaned up most recently, newest first,
// whether the user asked or the automatic sweep did it.
func (s *Service) RecentCleanups(ctx context.Context) ([]Cleanup, error) {
	if s.history == nil {
		return []Cleanup{}, nil
	}

	records, err := s.history.RecentCleanups(ctx, recentLimit)
	if err != nil {
		return nil, err
	}

	out := make([]Cleanup, 0, len(records))
	for _, r := range records {
		out = append(out, cleanupOf(r))
	}

	return out, nil
}

// ClearCleanups forgets the cleanups RecentCleanups lists. It removes nothing else.
func (s *Service) ClearCleanups(ctx context.Context) error {
	if s.history == nil {
		return nil
	}

	return s.history.ClearCleanups(ctx)
}

// recordCleanup adds a removed worktree to the history. The worktree is gone either way,
// so a failure is only logged.
func (s *Service) recordCleanup(ctx context.Context, repoPath string, wt Worktree, deletedBranch string, automatic bool) {
	if s.history == nil {
		return
	}

	record := worklog.Cleanup{
		CleanedAt:     s.clock.Now(),
		Automatic:     automatic,
		Repo:          filepath.Base(repoPath),
		Worktree:      filepath.Base(wt.Path),
		DisplayPath:   wt.DisplayPath,
		DeletedBranch: deletedBranch,
		Head:          wt.Head,
	}

	if it := wt.Item; it != nil {
		record.ItemKind, record.ItemOwner, record.ItemName, record.ItemNumber = it.Kind, it.Owner, it.Name, it.Number
		record.ItemTitle, record.ItemURL, record.ItemState = it.Title, it.URL, it.State
	}

	if err := s.history.RecordCleanup(ctx, record); err != nil {
		s.logger.WarnContext(ctx, "could not record a worktree cleanup", "error", err)
	}
}

// cleanupOf converts a stored cleanup for the view.
func cleanupOf(r worklog.Cleanup) Cleanup {
	c := Cleanup{
		ID:            r.ID,
		CleanedAt:     clock.Format(r.CleanedAt),
		Automatic:     r.Automatic,
		Repo:          r.Repo,
		Worktree:      r.Worktree,
		DisplayPath:   r.DisplayPath,
		DeletedBranch: r.DeletedBranch,
		Head:          r.Head,
	}

	if r.ItemKind != "" {
		c.Item = &LinkedItem{
			Kind: r.ItemKind, Owner: r.ItemOwner, Name: r.ItemName, Number: r.ItemNumber, Title: r.ItemTitle,
			URL: r.ItemURL, State: r.ItemState,
		}
	}

	return c
}
