// Package worklog records the worktrees the app started agent sessions in, and the pull
// request or issue each is for, and the worktrees it cleaned up. It owns the
// worktree_launches and worktree_cleanups tables.
//
// The launcher records a launch when it asks the agent for a worktree; the worktrees
// service reads the records to link each worktree to its item exactly, and its automatic
// cleanup uses them to find the clones worth checking. A record is only a pointer: whether
// a worktree may be removed is always decided from git and GitHub at the time.
//
// The worktrees service records a cleanup once the worktree is gone, whether the user
// asked for it or the automatic sweep did it, so the Worktrees view can show what was
// removed and when, across restarts. Only the newest KeepCleanups records are kept.
package worklog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/worklog/internal/queries"
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

// Store reads and writes the launch and cleanup records.
type Store struct {
	db *sql.DB
	q  *queries.Queries

	// keepCleanups is how many cleanups are kept: KeepCleanups, or fewer in tests that
	// need to run past the cap with few writes.
	keepCleanups int64
}

// New returns a Store over the pool db.
func New(db *sql.DB) *Store {
	return &Store{db: db, q: queries.New(db), keepCleanups: KeepCleanups}
}

// RecordLaunch stores a launch, replacing an earlier one for the same worktree of the same
// clone.
func (s *Store) RecordLaunch(ctx context.Context, l Launch) error {
	if l.ClonePath == "" || l.Worktree == "" || l.Owner == "" || l.Name == "" ||
		!l.Kind.Valid() || l.Number <= 0 {
		return fmt.Errorf("%w: %+v", ErrInvalidLaunch, l)
	}

	err := s.q.UpsertLaunch(ctx, queries.UpsertLaunchParams{
		ClonePath: l.ClonePath, Worktree: l.Worktree, Owner: l.Owner, Name: l.Name, Kind: string(l.Kind),
		Number: int64(l.Number), LaunchedAt: l.LaunchedAt.UTC(),
	})
	if err != nil {
		return fmt.Errorf("record the worktree launch: %w", err)
	}

	return nil
}

// Launches returns every recorded launch, oldest first.
func (s *Store) Launches(ctx context.Context) ([]Launch, error) {
	rows, err := s.q.ListLaunches(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the worktree launches: %w", err)
	}

	out := make([]Launch, 0, len(rows))
	for _, r := range rows {
		out = append(out, Launch{
			ClonePath: r.ClonePath, Worktree: r.Worktree, Owner: r.Owner, Name: r.Name, Kind: item.Kind(r.Kind),
			Number: int(r.Number), LaunchedAt: r.LaunchedAt.UTC(),
		})
	}

	return out, nil
}

// ForgetLaunch removes the record of a clone's worktree, if there is one.
func (s *Store) ForgetLaunch(ctx context.Context, clonePath, worktree string) error {
	err := s.q.DeleteLaunch(ctx, queries.DeleteLaunchParams{ClonePath: clonePath, Worktree: worktree})
	if err != nil {
		return fmt.Errorf("forget the worktree launch: %w", err)
	}

	return nil
}
