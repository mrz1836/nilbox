package issues

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
)

// The pickers' memory: what each read found is kept for keptFor, so opening a picker again,
// or searching again for the same words, costs nothing. The labels are kept for labelRepos
// repositories at most, and the searches for keptSearches.
const (
	keptFor      = 10 * time.Minute
	labelRepos   = 32
	keptSearches = 64
)

// memo keeps what reads found, by key, each for keptFor, and size keys at most: the oldest
// read gives way. It is safe for concurrent use.
type memo[T any] struct {
	size int

	mu   sync.Mutex
	kept map[string]memoed[T]
}

// memoed is what a read found at at.
type memoed[T any] struct {
	value T
	at    time.Time
}

// get returns what was kept for key, while it is under keptFor old at now.
func (m *memo[T]) get(key string, now time.Time) (T, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	k, ok := m.kept[key]
	if !ok || now.Sub(k.at) >= keptFor || now.Before(k.at) {
		var zero T

		return zero, false
	}

	return k.value, true
}

// put keeps value for key as read at now, letting go of the oldest read once size keys are
// kept.
func (m *memo[T]) put(key string, value T, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.kept == nil {
		m.kept = map[string]memoed[T]{}
	}

	if _, ok := m.kept[key]; !ok && len(m.kept) >= m.size {
		oldest := ""

		for k, v := range m.kept {
			if oldest == "" || v.at.Before(m.kept[oldest].at) {
				oldest = k
			}
		}

		delete(m.kept, oldest)
	}

	m.kept[key] = memoed[T]{value: value, at: now}
}

// Labels returns the labels of the issue's repository the picker offers, the first hundred
// in name order: read with one GraphQL point, one read per repository at a time, then kept
// ten minutes.
func (s *Service) Labels(ctx context.Context, id int64) (RepoLabels, error) {
	card, err := s.issue(ctx, id)
	if err != nil {
		return RepoLabels{Labels: []RepoLabel{}}, err
	}

	repo := strings.ToLower(card.ref().Repo.String())

	if kept, ok := s.labels.get(repo, s.now()); ok {
		return kept, nil
	}

	labels, err := s.labelReads.Do(ctx, repo, func(ctx context.Context) (RepoLabels, error) {
		return s.readLabels(ctx, id, repo)
	})
	if err != nil {
		return RepoLabels{Labels: []RepoLabel{}}, err
	}

	return labels, nil
}

// readLabels reads the labels of the issue with id's repository, repo, and keeps them.
func (s *Service) readLabels(ctx context.Context, id int64, repo string) (RepoLabels, error) {
	var read *github.RepoLabels

	err := s.inbox.Peek(ctx, id, func(ctx context.Context, tok github.Token, t Target) error {
		var readErr error

		read, readErr = s.github.FetchRepoLabels(ctx, tok, t.NodeID)

		return readErr
	})
	if err != nil {
		return RepoLabels{}, err
	}

	out := RepoLabels{Labels: make([]RepoLabel, 0, len(read.Labels)), Total: read.Total}
	for _, l := range read.Labels {
		out.Labels = append(out.Labels, RepoLabel{Name: l.Name, Color: l.Color, Description: l.Description})
	}

	s.labels.put(repo, out, s.now())

	return out, nil
}

// Assignable returns the people the issue can be assigned to whose login or name matches
// query, the first twenty: read with one GraphQL point a search, one at a time for the
// same words in the same repository, then kept ten minutes. The user is marked.
func (s *Service) Assignable(ctx context.Context, id int64, query string) ([]Candidate, error) {
	card, err := s.issue(ctx, id)
	if err != nil {
		return []Candidate{}, err
	}

	query = strings.TrimSpace(query)
	key := strings.ToLower(card.ref().Repo.String()) + "\x00" + strings.ToLower(query)

	found, ok := s.searches.get(key, s.now())
	if !ok {
		found, err = s.searchReads.Do(ctx, key, func(ctx context.Context) ([]github.Assignable, error) {
			return s.search(ctx, id, key, query)
		})
		if err != nil {
			return []Candidate{}, err
		}
	}

	out := make([]Candidate, 0, len(found))
	for _, p := range found {
		out = append(out, Candidate{Login: p.Login, Name: p.Name, Mine: isViewer(p.Login, card.Viewer)})
	}

	return out, nil
}

// search reads who the issue with id can be assigned to that query matches, and keeps them
// under key.
func (s *Service) search(ctx context.Context, id int64, key, query string) ([]github.Assignable, error) {
	var found []github.Assignable

	err := s.inbox.Peek(ctx, id, func(ctx context.Context, tok github.Token, t Target) error {
		var readErr error

		found, readErr = s.github.FetchAssignable(ctx, tok, t.NodeID, query)

		return readErr
	})
	if err != nil {
		return nil, err
	}

	s.searches.put(key, found, s.now())

	return found, nil
}
