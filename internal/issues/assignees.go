package issues

import (
	"context"

	"github.com/mrz1836/nilbox/internal/assignees"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
)

// The issue sheet's assignee picker offers people through the assignee Finder the review
// sheet shares: from the People directory, with no request, and from GitHub only when
// nothing the directory holds answers a search.

// AssigneeCandidates returns whom the picker offers to assign to the issue with id, from the
// sheet's cache and the People directory alone, with no request.
func (s *Service) AssigneeCandidates(ctx context.Context, id int64) (assignees.Candidates, error) {
	it, err := s.issueItem(ctx, id)
	if err != nil {
		return assignees.Candidates{People: []assignees.Candidate{}}, err
	}

	return s.finder.Candidates(ctx, it)
}

// LearnAssignees reads who can be assigned in the issue's repository, when a read is due
// (Candidates.Stale), and returns AssigneeCandidates with what it learned: one GraphQL
// request of one point, at most weekly per repository, whichever sheet asks.
func (s *Service) LearnAssignees(ctx context.Context, id int64) (assignees.Candidates, error) {
	it, err := s.issueItem(ctx, id)
	if err != nil {
		return assignees.Candidates{People: []assignees.Candidate{}}, err
	}

	return s.finder.Learn(ctx, it, s.readAssignable(id))
}

// SearchAssignees returns whom the picker offers to assign to the issue with id whose login
// or name matches query: from what the app holds when that answers the search, else after
// one GraphQL request of one point.
func (s *Service) SearchAssignees(ctx context.Context, id int64, query string) (assignees.Candidates, error) {
	it, err := s.issueItem(ctx, id)
	if err != nil {
		return assignees.Candidates{People: []assignees.Candidate{}}, err
	}

	return s.finder.Search(ctx, it, query, s.readAssignable(id))
}

// issueItem is the issue with id as the assignee picker sees it, from the inbox and the
// sheet's cache: its participants as the sheet last read them.
func (s *Service) issueItem(ctx context.Context, id int64) (assignees.Item, error) {
	card, c, err := s.load(ctx, id)
	if err != nil {
		return assignees.Item{}, err
	}

	it := assignees.Item{
		Repo: card.ref().Repo, Viewer: card.Viewer, Author: card.AuthorLogin, AuthorIsPerson: card.AuthorType == "User",
	}

	if c != nil {
		it.Participants = c.view.Participants
	}

	return it, nil
}

// readAssignable is the read of who can be assigned in the repository of the issue with id,
// through the inbox's footing, which stores nothing.
func (s *Service) readAssignable(id int64) assignees.Read {
	return func(ctx context.Context, query string) (assignees.Found, error) {
		var got *github.AssignableUsers

		err := s.inbox.Peek(ctx, id, func(ctx context.Context, tok github.Token, t Target) error {
			read, readErr := s.github.FetchAssignable(ctx, tok, item.KindIssue, t.NodeID, query)
			got = read

			return readErr
		})
		if err != nil {
			return assignees.Found{}, err
		}

		out := assignees.Found{People: make([]assignees.Person, 0, len(got.People)), Total: got.Total}
		for _, p := range got.People {
			out.People = append(out.People, assignees.Person(p))
		}

		return out, nil
	}
}

// InboxEdits follows, in the sheet's cache, what the inbox's own edits change of an issue:
// Unassign me takes the user off its assignees on GitHub, and the sheet that opens next
// reads it again. It is apart from the Service, which the window binds, since only core
// calls it.
type InboxEdits struct {
	cache *cacheStore
}

// NewInboxEdits returns the InboxEdits of the sheet s.
func NewInboxEdits(s *Service) InboxEdits {
	return InboxEdits{cache: s.cache}
}

// Unassigned marks the cached view of the issue with id out of date, as the inbox's
// Unassign me changed its assignees on GitHub, so the sheet's next refresh reads it, as
// after the sheet's own edits, however recently it was read. An issue the sheet holds no
// cache of is left as it is.
func (e InboxEdits) Unassigned(ctx context.Context, id int64) error {
	return e.cache.expire(ctx, id)
}
