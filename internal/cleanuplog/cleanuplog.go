// Package cleanuplog records the worktrees the app cleaned up, so the Worktrees view can
// show what was removed and when, across restarts.
//
// The worktrees service records a cleanup once the worktree is gone, whether the user
// asked for it or the automatic sweep did it. Only the newest Keep records are kept.
package cleanuplog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/cleanuplog/model"
)

// Keep is how many cleanups are kept; recording one more drops the oldest.
const Keep = 100

// pruneSQL drops every cleanup older than the newest Keep.
const pruneSQL = `DELETE FROM worktree_cleanups WHERE id NOT IN (
  SELECT id FROM worktree_cleanups ORDER BY cleaned_at DESC, id DESC LIMIT ?
)`

// ErrInvalidCleanup is returned for a cleanup without a repository or worktree name.
var ErrInvalidCleanup = errors.New("invalid worktree cleanup")

// Cleanup is one worktree the app cleaned up.
type Cleanup struct {
	// ID identifies the record; Record ignores it.
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

// Store reads and writes the cleanup records.
type Store struct {
	db *gorm.DB
}

// NewStore returns a Store over db.
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// Record stores a cleanup and drops the oldest records past Keep, in one transaction.
func (s *Store) Record(ctx context.Context, c Cleanup) error {
	if c.Repo == "" || c.Worktree == "" {
		return fmt.Errorf("%w: no repository or worktree name", ErrInvalidCleanup)
	}

	row := model.WorktreeCleanup{
		CleanedAt: c.CleanedAt.UTC(), Automatic: c.Automatic, Repo: c.Repo, Worktree: c.Worktree,
		DisplayPath: c.DisplayPath, DeletedBranch: c.DeletedBranch, Head: c.Head, ItemKind: c.ItemKind,
		ItemOwner: c.ItemOwner, ItemName: c.ItemName, ItemNumber: c.ItemNumber, ItemTitle: c.ItemTitle,
		ItemURL: c.ItemURL, ItemState: c.ItemState,
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}

		return tx.Exec(pruneSQL, Keep).Error
	})
	if err != nil {
		return fmt.Errorf("record the worktree cleanup: %w", err)
	}

	return nil
}

// Recent returns up to limit cleanups, newest first.
func (s *Store) Recent(ctx context.Context, limit int) ([]Cleanup, error) {
	var rows []model.WorktreeCleanup

	err := s.db.WithContext(ctx).Order("cleaned_at DESC, id DESC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("read the worktree cleanups: %w", err)
	}

	out := make([]Cleanup, 0, len(rows))
	for _, r := range rows {
		out = append(out, Cleanup{
			ID: r.ID, CleanedAt: r.CleanedAt.UTC(), Automatic: r.Automatic, Repo: r.Repo, Worktree: r.Worktree,
			DisplayPath: r.DisplayPath, DeletedBranch: r.DeletedBranch, Head: r.Head, ItemKind: r.ItemKind,
			ItemOwner: r.ItemOwner, ItemName: r.ItemName, ItemNumber: r.ItemNumber, ItemTitle: r.ItemTitle,
			ItemURL: r.ItemURL, ItemState: r.ItemState,
		})
	}

	return out, nil
}

// Clear forgets every cleanup.
func (s *Store) Clear(ctx context.Context) error {
	if err := s.db.WithContext(ctx).Where("1 = 1").Delete(&model.WorktreeCleanup{}).Error; err != nil {
		return fmt.Errorf("clear the worktree cleanups: %w", err)
	}

	return nil
}
