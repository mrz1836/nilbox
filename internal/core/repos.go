package core

import (
	"context"

	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
)

// inboxRepos lists the repositories the inbox has seen, for the launch defaults editor.
// It implements launchdefaults.RepoSource.
type inboxRepos struct {
	db *gorm.DB
}

// Repos returns every repository the inbox has seen that is not archived, by owner and
// then name ignoring case, each with how many of its items the inbox lists now.
func (r inboxRepos) Repos(ctx context.Context) ([]launchdefaults.Target, error) {
	counts, err := inbox.RepoCounts(ctx, r.db)
	if err != nil {
		return nil, err
	}

	targets := make([]launchdefaults.Target, 0, len(counts))
	for _, c := range counts {
		targets = append(targets, launchdefaults.Target{Owner: c.Repo.Owner, Name: c.Repo.Repo, Items: c.Items})
	}

	return targets, nil
}
