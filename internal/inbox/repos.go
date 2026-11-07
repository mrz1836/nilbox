package inbox

import (
	"context"
	"fmt"

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
