// Package worklog records the worktrees the app made for pull requests and issues, which
// item each is for and the sessions started in it, and the worktrees it cleaned up. It
// owns the worktree_records and worktree_cleanups tables.
//
// The worktrees service records a worktree when it makes one for a launch, or finds one on
// an item's branch, and the launcher records each session started in it. The worktrees
// service reads the records to link each worktree to its item exactly, to resume it on the
// next launch, and to find the clones its automatic cleanup checks, and it checks every
// record against git each time, dropping one whose worktree is gone. A record is only a
// pointer: whether a worktree may be removed is always decided from git and GitHub at the
// time.
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
	"path/filepath"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/worklog/internal/queries"
)

// LegacyDir is where, under a clone, Claude Code made the worktrees of launches before
// the app made them itself: <clone>/.claude/worktrees/<name>.
const LegacyDir = ".claude/worktrees"

var (
	// ErrInvalidRecord is returned for a worktree record missing a field, or with an
	// unknown kind, a number that is not positive, or a relative path.
	ErrInvalidRecord = errors.New("invalid worktree record")

	// ErrNotRecorded is returned when touching a worktree that has no record.
	ErrNotRecorded = errors.New("the worktree is not recorded")
)

// Record is one worktree the app made for a pull request or issue, or found on its
// branch: where it is, the item it is for, and the sessions started in it.
type Record struct {
	// ClonePath is the clone the worktree belongs to.
	ClonePath string

	// Worktree is the item's worktree name, pr-<n> or issue-<n>.
	Worktree string

	// Path is the worktree's absolute directory, or "" for a record kept before the app
	// made worktrees itself (see Dir).
	Path string

	// Branch is the branch the worktree was made on, worktree-<name>.
	Branch string

	// Owner and Name are the item's repository.
	Owner string
	Name  string

	// Kind is the item's kind.
	Kind item.Kind

	// Number is the item's number.
	Number int

	// Agent is the agent of the latest session started there, or agent.Inherit when none
	// has started.
	Agent agent.ID

	// CreatedAt is when the worktree was made or first recorded.
	CreatedAt time.Time

	// LastAgentAt is when the latest session started, or zero when none has.
	LastAgentAt time.Time

	// Launches counts the sessions started there.
	Launches int
}

// Dir returns the worktree's directory: Path, or for a record kept before the app made
// worktrees itself, the directory Claude Code made under the clone.
func (r Record) Dir() string {
	if r.Path != "" {
		return r.Path
	}

	return filepath.Join(r.ClonePath, filepath.FromSlash(LegacyDir), r.Worktree)
}

// valid reports whether a record holds what storing it needs.
func (r Record) valid() bool {
	return r.ClonePath != "" && r.Worktree != "" && r.Owner != "" && r.Name != "" && r.Kind.Valid() &&
		r.Number > 0 && (r.Path == "" || filepath.IsAbs(r.Path)) && !r.CreatedAt.IsZero()
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

// Record stores where a clone's worktree for an item is, and the item it is for,
// replacing the place an earlier record of the same worktree named. A worktree recorded
// already keeps when it was made and its sessions.
func (s *Store) Record(ctx context.Context, r Record) error {
	if !r.valid() {
		return fmt.Errorf("%w: %+v", ErrInvalidRecord, r)
	}

	err := s.q.RecordWorktree(ctx, queries.RecordWorktreeParams{
		ClonePath: r.ClonePath, Worktree: r.Worktree, Path: r.Path, Branch: r.Branch, Owner: r.Owner, Name: r.Name,
		Kind: string(r.Kind), Number: int64(r.Number), CreatedAt: r.CreatedAt.UTC(),
	})
	if err != nil {
		return fmt.Errorf("record the worktree: %w", err)
	}

	return nil
}

// Touch records a session the agent with id started at at in a clone's recorded
// worktree. It returns ErrNotRecorded when the worktree has no record.
func (s *Store) Touch(ctx context.Context, clonePath, worktree string, id agent.ID, at time.Time) error {
	at = at.UTC()

	n, err := s.q.TouchWorktree(ctx, queries.TouchWorktreeParams{
		Agent: string(id), LastAgentAt: &at, ClonePath: clonePath, Worktree: worktree,
	})

	switch {
	case err != nil:
		return fmt.Errorf("record the session: %w", err)
	case n == 0:
		return fmt.Errorf("%w: %s", ErrNotRecorded, worktree)
	default:
		return nil
	}
}

// Worktrees returns every recorded worktree, oldest first.
func (s *Store) Worktrees(ctx context.Context) ([]Record, error) {
	rows, err := s.q.ListWorktrees(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the worktree records: %w", err)
	}

	out := make([]Record, 0, len(rows))
	for _, r := range rows {
		rec := Record{
			ClonePath: r.ClonePath, Worktree: r.Worktree, Path: r.Path, Branch: r.Branch, Owner: r.Owner, Name: r.Name,
			Kind: item.Kind(r.Kind), Number: int(r.Number), Agent: agent.ID(r.Agent), CreatedAt: r.CreatedAt.UTC(),
			Launches: int(r.Launches),
		}

		if r.LastAgentAt != nil {
			rec.LastAgentAt = r.LastAgentAt.UTC()
		}

		out = append(out, rec)
	}

	return out, nil
}

// Forget removes the record of a clone's worktree, if there is one.
func (s *Store) Forget(ctx context.Context, clonePath, worktree string) error {
	err := s.q.DeleteWorktree(ctx, queries.DeleteWorktreeParams{ClonePath: clonePath, Worktree: worktree})
	if err != nil {
		return fmt.Errorf("forget the worktree: %w", err)
	}

	return nil
}
