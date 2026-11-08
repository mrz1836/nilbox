package worklog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/worklog/internal/queries"
)

// KeepCleanups is how many cleanups are kept; recording one more drops the oldest.
const KeepCleanups = 100

// ErrInvalidCleanup is returned for a cleanup without a repository or worktree name.
var ErrInvalidCleanup = errors.New("invalid worktree cleanup")

// Cleanup is one worktree the app cleaned up.
type Cleanup struct {
	// ID identifies the record; RecordCleanup ignores it.
	ID int64

	// CleanedAt is when the worktree was removed.
	CleanedAt time.Time

	// Automatic reports a cleanup by the sweep rather than by the user.
	Automatic bool

	// Repo is the clone's directory name, and Worktree the worktree's, e.g. "pr-184".
	Repo     string
	Worktree string

	// DisplayPath is the worktree's path relative to home with ~.
	DisplayPath string

	// DeletedBranch is the branch deleted with it, or "" when it was kept.
	DeletedBranch string

	// Head is the commit the worktree had checked out.
	Head string

	// The pull request or issue it was for: kind ("pr" or "issue"), repository, number,
	// title, URL, and state ("merged" or "closed").
	ItemKind   string
	ItemOwner  string
	ItemName   string
	ItemNumber int
	ItemTitle  string
	ItemURL    string
	ItemState  string
}

// RecordCleanup stores a cleanup and drops the oldest records past KeepCleanups, in one
// transaction.
func (s *Store) RecordCleanup(ctx context.Context, c Cleanup) error {
	if c.Repo == "" || c.Worktree == "" {
		return fmt.Errorf("%w: no repository or worktree name", ErrInvalidCleanup)
	}

	row := queries.InsertCleanupParams{
		CleanedAt: c.CleanedAt.UTC(), Automatic: c.Automatic, Repo: c.Repo, Worktree: c.Worktree,
		DisplayPath: c.DisplayPath, DeletedBranch: c.DeletedBranch, Head: c.Head, ItemKind: c.ItemKind,
		ItemOwner: c.ItemOwner, ItemName: c.ItemName, ItemNumber: int64(c.ItemNumber), ItemTitle: c.ItemTitle,
		ItemURL: c.ItemURL, ItemState: c.ItemState,
	}

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.InsertCleanup(ctx, row); err != nil {
			return err
		}

		return q.TrimCleanups(ctx, s.keepCleanups)
	})
	if err != nil {
		return fmt.Errorf("record the worktree cleanup: %w", err)
	}

	return nil
}

// RecentCleanups returns up to limit cleanups, newest first. A negative limit returns
// every one kept.
func (s *Store) RecentCleanups(ctx context.Context, limit int) ([]Cleanup, error) {
	rows, err := s.q.RecentCleanups(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("read the worktree cleanups: %w", err)
	}

	out := make([]Cleanup, 0, len(rows))
	for _, r := range rows {
		out = append(out, Cleanup{
			ID: r.ID, CleanedAt: r.CleanedAt.UTC(), Automatic: r.Automatic, Repo: r.Repo, Worktree: r.Worktree,
			DisplayPath: r.DisplayPath, DeletedBranch: r.DeletedBranch, Head: r.Head, ItemKind: r.ItemKind,
			ItemOwner: r.ItemOwner, ItemName: r.ItemName, ItemNumber: int(r.ItemNumber), ItemTitle: r.ItemTitle,
			ItemURL: r.ItemURL, ItemState: r.ItemState,
		})
	}

	return out, nil
}

// ClearCleanups forgets every cleanup.
func (s *Store) ClearCleanups(ctx context.Context) error {
	if err := s.q.ClearCleanups(ctx); err != nil {
		return fmt.Errorf("clear the worktree cleanups: %w", err)
	}

	return nil
}
