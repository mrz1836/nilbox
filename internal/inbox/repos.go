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
