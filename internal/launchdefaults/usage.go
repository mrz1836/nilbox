package launchdefaults

import (
	"context"

	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/repo"
)

// Store reads the stored launch defaults for the packages that depend on them, apart
// from the Service the window binds to.
type Store struct {
	db *gorm.DB
}

// NewStore returns a Store over db.
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// PromptUses lists every place a launch default names a prompt: each org's and each
// repository's default prompt for pull requests and for issues. It implements
// prompts.UsageSource.
func (s *Store) PromptUses(ctx context.Context) ([]prompts.Use, error) {
	all, err := All(ctx, s.db)
	if err != nil {
		return nil, err
	}

	var uses []prompts.Use

	for _, d := range all {
		target := repo.Name{Owner: d.Owner, Repo: d.Name}

		if d.PRPromptID != nil {
			uses = append(uses, prompts.Use{PromptID: *d.PRPromptID, Kind: item.KindPR, Target: target})
		}

		if d.IssuePromptID != nil {
			uses = append(uses, prompts.Use{PromptID: *d.IssuePromptID, Kind: item.KindIssue, Target: target})
		}
	}

	return uses, nil
}
