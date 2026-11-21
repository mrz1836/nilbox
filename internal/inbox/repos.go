package inbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/repo"
)

// RepoCount is a repository the inbox has seen, with how many of its items are listed
// now.
type RepoCount struct {
	Repo  repo.Name
	Items int
}

// RepoCounts returns every repository the inbox has seen that is not archived, by owner
// and then name ignoring case, each with how many of its items are listed now.
func (s *Store) RepoCounts(ctx context.Context) ([]RepoCount, error) {
	rows, err := s.q.ListRepoCounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("count the items of each repository: %w", err)
	}

	counts := make([]RepoCount, 0, len(rows))
	for _, row := range rows {
		counts = append(counts, RepoCount{Repo: repo.Name{Owner: row.Owner, Repo: row.Name}, Items: int(row.Items)})
	}

	return counts, nil
}

// DefaultBranch returns the default branch the last refresh stored for the repository
// name, which matches ignoring case, or "" when the inbox has never seen it. It reads
// only the database.
func (s *Store) DefaultBranch(ctx context.Context, name repo.Name) (string, error) {
	branch, err := s.q.GetRepoDefaultBranch(ctx, queries.GetRepoDefaultBranchParams{Owner: name.Owner, Name: name.Repo})

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("read the default branch of %s: %w", name, err)
	default:
		return branch, nil
	}
}

// RepoFacts is what the last refresh stored about a repository that says whether a branch
// can be pushed to it and a pull request opened there: all zero when the inbox never saw
// it.
type RepoFacts struct {
	// Found reports that the inbox has seen the repository, so the rest says something.
	Found bool

	// DefaultBranch is its default branch, or "" when not read.
	DefaultBranch string

	// ViewerPermission is the viewer's access to it as GitHub names it, ADMIN, MAINTAIN,
	// WRITE, TRIAGE, or READ, or "" when GitHub didn't say.
	ViewerPermission string

	// Archived, Disabled, and Locked are what GitHub says of it, and LockReason why it is
	// locked, as GitHub names that, or "".
	Archived   bool
	Disabled   bool
	Locked     bool
	LockReason string

	// Fork reports a fork, and Parent the repository it was forked from, "owner/name", or
	// "".
	Fork   bool
	Parent string
}

// RepoFacts returns what the last refresh stored about the repository name, which matches
// ignoring case, with Found false when the inbox has never seen it. It reads only the
// database.
func (s *Store) RepoFacts(ctx context.Context, name repo.Name) (RepoFacts, error) {
	row, err := s.q.GetRepoFacts(ctx, queries.GetRepoFactsParams{Owner: name.Owner, Name: name.Repo})

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return RepoFacts{}, nil
	case err != nil:
		return RepoFacts{}, fmt.Errorf("read what is stored about %s: %w", name, err)
	}

	return RepoFacts{
		Found: true, DefaultBranch: row.DefaultBranch, ViewerPermission: row.ViewerPermission, Archived: row.IsArchived,
		Disabled: row.IsDisabled, Locked: row.IsLocked, LockReason: row.LockReason, Fork: row.IsFork, Parent: row.Parent,
	}, nil
}
