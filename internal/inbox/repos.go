package inbox

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/repo"
)

// repoCountsSQL lists the repositories that are not archived, with how many of their
// items are listed now.
const repoCountsSQL = `SELECT r.owner, r.name, COUNT(i.id) AS items
FROM repos r
LEFT JOIN items i ON i.repo_id = r.id
  AND (i.reason_review_requested = 1 OR i.reason_authored = 1 OR i.reason_assigned = 1)
WHERE r.is_archived = 0
GROUP BY r.id
ORDER BY r.owner COLLATE NOCASE, r.name COLLATE NOCASE`

// RepoCount is a repository the inbox has seen, with how many of its items are listed
// now.
type RepoCount struct {
	Repo  repo.Name
	Items int
}

// RepoCounts returns every repository the inbox has seen that is not archived, by owner
// and then name ignoring case, each with how many of its items are listed now. db is a
// GORM handle outside a transaction.
func RepoCounts(ctx context.Context, db *gorm.DB) ([]RepoCount, error) {
	var rows []struct {
		Owner string
		Name  string
		Items int
	}

	if err := db.WithContext(ctx).Raw(repoCountsSQL).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("count the items of each repository: %w", err)
	}

	counts := make([]RepoCount, 0, len(rows))
	for _, row := range rows {
		counts = append(counts, RepoCount{Repo: repo.Name{Owner: row.Owner, Repo: row.Name}, Items: row.Items})
	}

	return counts, nil
}
