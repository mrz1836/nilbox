package gitread

import "context"

// OriginHeadReader reads origin's default branch as a clone last learned it, from its own
// refs/remotes/origin/HEAD, asking no one: *Git.
type OriginHeadReader interface {
	OriginHead(ctx context.Context, dir string) (string, error)
}

// DefaultBranch works out the default branch of the repository the clone at dir is a
// clone of, as the app knows it without asking anyone: stored, the branch the app stored
// for the repository (the inbox's), when it stored one; else the branch the clone's
// refs/remotes/origin/HEAD names, read through git (OriginHead) when git isn't nil; else
// the first of main and master that guess accepts, when guess isn't nil, as Repo scripts
// takes the one the clone is on and Deps the one it has of origin's. It is "" when none
// of them names a branch, and a read that fails names none. Repo scripts and Deps share
// the rule.
func DefaultBranch(ctx context.Context, git OriginHeadReader, dir, stored string, guess func(branch string) bool) string {
	if stored != "" {
		return stored
	}

	if git != nil {
		if head, err := git.OriginHead(ctx, dir); err == nil && head != "" {
			return head
		}
	}

	if guess == nil {
		return ""
	}

	for _, branch := range guessedBranches() {
		if guess(branch) {
			return branch
		}
	}

	return ""
}

// guessedBranches are the names a default branch most often has, in the order
// DefaultBranch tries them.
func guessedBranches() []string {
	return []string{"main", "master"}
}
