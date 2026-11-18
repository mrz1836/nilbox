package inbox

import (
	"context"
	"fmt"

	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/store"
)

// PushTarget is an open pull request whose head branch the push watch follows in the
// user's clones (internal/pushwatch), so a push made on this Mac reaches its card at once.
type PushTarget struct {
	// ID is the item's.
	ID int64

	// Repo is the pull request's repository, which holds its head branch too.
	Repo repo.Name

	// Branch is the head branch.
	Branch string

	// Head is the head commit as the details last read it, "" before they did.
	Head string
}

// PushTargets returns the pull requests the push watch follows: the open pull requests
// listed for the signed-in account, neither dismissed nor snoozed, whose head branch is in
// their own repository, the ones the user opened first, then the most recently active. A
// muted repository's or org's are left out, as the checks follow leaves them out. It reads
// what is stored alone, never GitHub, and returns none before an account is known. It is
// the Store's, which the window never calls, so it isn't bound.
func (s *Store) PushTargets(ctx context.Context) ([]PushTarget, error) {
	stored, err := s.prefs.GetMany(ctx, append([]string{prefViewer}, reporules.Keys()...)...)
	if err != nil {
		return nil, fmt.Errorf("read the pull requests to watch for pushes: %w", err)
	}

	login := stored[prefViewer]
	if login == "" {
		return []PushTarget{}, nil
	}

	now := s.clock.Now().UTC()

	rows, err := s.q.ListPushTargets(ctx, queries.ListPushTargetsParams{Login: login, Now: &now})
	if err != nil {
		return nil, fmt.Errorf("read the pull requests to watch for pushes: %w", err)
	}

	rules := reporules.Decode(stored)
	out := make([]PushTarget, 0, len(rows))

	for _, r := range rows {
		name := repo.Name{Owner: r.RepoOwner, Repo: r.RepoName}
		if rules.IsMuted(name) {
			continue
		}

		out = append(out, PushTarget{ID: r.ID, Repo: name, Branch: r.HeadRef, Head: store.Deref(r.HeadOID)})
	}

	return out, nil
}
