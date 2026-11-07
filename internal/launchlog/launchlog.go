// Package launchlog records the worktrees the app started agent sessions in, and the pull
// request or issue each is for.
//
// The launcher records a launch when it asks the agent for a worktree; the worktrees
// service reads the records to link each worktree to its item exactly, and its automatic
// cleanup uses them to find the clones worth checking. A record is only a pointer: whether
// a worktree may be removed is always decided from git and GitHub at the time.
package launchlog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchlog/model"
)

// ErrInvalidLaunch is returned for a launch missing a field, or with an unknown kind or a
// number that is not positive.
var ErrInvalidLaunch = errors.New("invalid worktree launch")

// Launch is one worktree the app started a session in.
type Launch struct {
	// ClonePath is the clone the session was launched in.
	ClonePath string

	// Worktree is the worktree's directory name under the clone's .claude/worktrees.
	Worktree string

	// Owner and Name are the item's repository.
	Owner string
	Name  string

	// Kind is the item's kind.
	Kind item.Kind

	// Number is the item's number.
	Number int

	// LaunchedAt is when the session was launched.
	LaunchedAt time.Time
}

// Store reads and writes the launch records.
type Store struct {
	db *gorm.DB
}

// NewStore returns a Store over db.
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// Record stores a launch, replacing an earlier one for the same worktree of the same
// clone.
func (s *Store) Record(ctx context.Context, l Launch) error {
	if l.ClonePath == "" || l.Worktree == "" || l.Owner == "" || l.Name == "" ||
		!l.Kind.Valid() || l.Number <= 0 {
		return fmt.Errorf("%w: %+v", ErrInvalidLaunch, l)
	}

	row := model.WorktreeLaunch{
		ClonePath: l.ClonePath, Worktree: l.Worktree, Owner: l.Owner, Name: l.Name, Kind: l.Kind, Number: l.Number,
		LaunchedAt: l.LaunchedAt.UTC(),
	}

	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "clone_path"}, {Name: "worktree"}},
		DoUpdates: clause.AssignmentColumns([]string{"owner", "name", "kind", "number", "launched_at"}),
	}).Create(&row).Error
	if err != nil {
		return fmt.Errorf("record the worktree launch: %w", err)
	}

	return nil
}

// List returns every recorded launch, oldest first.
func (s *Store) List(ctx context.Context) ([]Launch, error) {
	var rows []model.WorktreeLaunch

	if err := s.db.WithContext(ctx).Order("launched_at, id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("read the worktree launches: %w", err)
	}

	out := make([]Launch, 0, len(rows))
	for _, r := range rows {
		out = append(out, Launch{
			ClonePath: r.ClonePath, Worktree: r.Worktree, Owner: r.Owner, Name: r.Name, Kind: r.Kind, Number: r.Number,
			LaunchedAt: r.LaunchedAt.UTC(),
		})
	}

	return out, nil
}

// Forget removes the record of a clone's worktree, if there is one.
func (s *Store) Forget(ctx context.Context, clonePath, worktree string) error {
	err := s.db.WithContext(ctx).Where("clone_path = ? AND worktree = ?", clonePath, worktree).
		Delete(&model.WorktreeLaunch{}).Error
	if err != nil {
		return fmt.Errorf("forget the worktree launch: %w", err)
	}

	return nil
}
