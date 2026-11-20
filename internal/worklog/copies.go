package worklog

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/worklog/internal/queries"
)

// ErrInvalidCopy is returned for a copy record missing a field, or with a relative path, a
// number that is not positive, or no commit.
var ErrInvalidCopy = errors.New("invalid worktree copy record")

// Copy is the pull request a worktree's commits went into as copies: each of its commits
// not on GitHub had an identical copy, as a cherry-pick or a rebase makes, on one pushed
// branch, whose pull request was known. The worktrees service takes it only while the
// worktree's HEAD is still Head.
type Copy struct {
	// ClonePath is the worktree's clone and WorktreePath its directory, both resolved.
	ClonePath    string
	WorktreePath string

	// Head is the commit the worktree had checked out.
	Head string

	// Ref is the remote-tracking branch the copies were on, as in
	// refs/remotes/origin/feature-x.
	Ref string

	// Owner, Name, and Number are the pull request that branch heads.
	Owner  string
	Name   string
	Number int

	// CopyTip is the branch's commit when last seen holding a copy of every one of Commits.
	CopyTip string

	// Commits are the worktree's commits that weren't on GitHub then, never empty.
	Commits []string

	// CreatedAt is when the record was first written for Head, and UpdatedAt when last;
	// RecordCopy stamps both.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// valid reports whether a copy record holds what storing it needs.
func (c Copy) valid() bool {
	return filepath.IsAbs(c.ClonePath) && filepath.IsAbs(c.WorktreePath) && c.Head != "" && c.Ref != "" &&
		c.Owner != "" && c.Name != "" && c.Number > 0 && c.CopyTip != "" && len(c.Commits) > 0
}

// RecordCopy stores the pull request a worktree's copied commits went into, replacing what
// was stored for the worktree before, stamped with the store's clock. A record for the same
// HEAD keeps when it was first written.
func (s *Store) RecordCopy(ctx context.Context, c Copy) error {
	if !c.valid() {
		return fmt.Errorf("%w: %+v", ErrInvalidCopy, c)
	}

	commits, err := store.EncodeJSON(c.Commits)
	if err != nil {
		return fmt.Errorf("record the worktree's copies: %w", err)
	}

	now := s.clock.Now().UTC()

	err = s.q.RecordCopy(ctx, queries.RecordCopyParams{
		ClonePath: c.ClonePath, WorktreePath: c.WorktreePath, Head: c.Head, Ref: c.Ref, RepoOwner: c.Owner,
		RepoName: c.Name, PRNumber: int64(c.Number), CopyTip: c.CopyTip, Commits: commits, CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		return fmt.Errorf("record the worktree's copies: %w", err)
	}

	return nil
}

// Copies returns every worktree's copy record, in the order they were first written.
func (s *Store) Copies(ctx context.Context) ([]Copy, error) {
	rows, err := s.q.ListCopies(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the worktrees' copy records: %w", err)
	}

	return store.ConvertRows(rows, func(r queries.ListCopiesRow) (Copy, error) {
		commits, err := store.DecodeJSON[[]string](r.Commits)
		if err != nil {
			return Copy{}, fmt.Errorf("read the copies of %s: %w", r.WorktreePath, err)
		}

		return Copy{
			ClonePath: r.ClonePath, WorktreePath: r.WorktreePath, Head: r.Head, Ref: r.Ref, Owner: r.RepoOwner,
			Name: r.RepoName, Number: int(r.PRNumber), CopyTip: r.CopyTip, Commits: commits,
			CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
		}, nil
	})
}

// ForgetCopy removes the copy record of the worktree at worktreePath, if there is one.
func (s *Store) ForgetCopy(ctx context.Context, worktreePath string) error {
	if err := s.q.DeleteCopy(ctx, worktreePath); err != nil {
		return fmt.Errorf("forget the worktree's copies: %w", err)
	}

	return nil
}
