package people

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/people/internal/queries"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/store"
)

// The assignee pickers' part of the directory: who can be assigned in each repository, as
// GitHub listed them (its whole list, one page, or a search of it) and as the app saw people
// assigned there, which both sheets share, and how long a read of GitHub's list answers the
// next search without another.
const (
	// coversFor is how long a search that named everyone it matched answers every longer
	// search that starts with its words, and repeatFor how long any search answers the same
	// words again.
	coversFor = 7 * 24 * time.Hour
	repeatFor = 10 * time.Minute

	// maxAssignable is how many rows of who can be assigned where are kept in all, those
	// listed or seen assigned latest.
	maxAssignable = 100_000

	// orgRows is how many rows of what was seen on the owner's other repositories OrgRanked
	// ranks, the latest.
	orgRows = 2_000
)

// Found is someone a read of a repository's assignable users named: their login, and their
// profile's display name, "" when it gave none.
type Found struct {
	Login string
	Name  string
}

// Assignable is someone the directory knows can be assigned in a repository.
type Assignable struct {
	Login string
	Name  string

	// Listed reports that a read of GitHub's assignable users named them, and no read of
	// the whole list that named everyone left them out since.
	Listed bool

	// AssignedAt is when the app last saw them assigned there, zero for never.
	AssignedAt time.Time
}

// Assignables is who the directory knows can be assigned in a repository, and how complete
// its knowledge is.
type Assignables struct {
	// People are everyone it knows of, by login, never nil.
	People []Assignable

	// ReadAt is when GitHub's whole list was last read, zero before it was, Total how many
	// people that read said can be assigned, and Listed how many it named, at most one page;
	// Complete reports that it named everyone.
	ReadAt   time.Time
	Total    int
	Listed   int
	Complete bool

	// Stale reports that a read of the whole list is due: there was none, or the last is
	// over a week old. It is false for 30 minutes after a read failed.
	Stale bool
}

// Summary is how much the directory holds: the people it knows, and the repositories it
// knows anyone of.
type Summary struct {
	People int
	Repos  int
}

// KeepAssignable keeps what a read of the repository name's assignable users found, now:
// for query, the search's words ("" for the whole list), the people it named (found), of
// total GitHub said match, in one transaction that records the read and prunes the
// directory to its bounds. A person whose name is empty keeps the name kept; one without a
// login GitHub could have issued, or a bot's, is not kept. A read of the whole list that
// named everyone leaves out, from then on, everyone it didn't name.
func (s *Service) KeepAssignable(ctx context.Context, name repo.Name, query string, found []Found, total int) error {
	key, err := repoKey(name)
	if err != nil {
		return err
	}

	queryKey, now := QueryKey(query), s.now()

	return store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		if err := keepRead(ctx, q, key, queryKey, found, total, now); err != nil {
			return err
		}

		return pruneAssignable(ctx, q, now, s.maxAssignable)
	})
}

// keepRead keeps a read of the repository keyed key's assignable users for queryKey, at
// now, as KeepAssignable says, without pruning.
func keepRead(
	ctx context.Context, q *queries.Queries, key, queryKey string, found []Found, total int, now time.Time,
) error {
	listed := len(found)

	if queryKey == "" && listed >= total {
		if err := q.UnlistAssignable(ctx, key); err != nil {
			return fmt.Errorf("unlist who can be assigned: %w", err)
		}
	}

	for _, p := range found {
		if err := keepListed(ctx, q, key, p, now); err != nil {
			return err
		}
	}

	err := q.MarkAssignableRead(ctx, queries.MarkAssignableReadParams{
		Repo: key, QueryKey: queryKey, ReadAt: &now, Total: int64(max(total, listed, 0)), Listed: int64(listed),
	})
	if err != nil {
		return fmt.Errorf("record the read of who can be assigned: %w", err)
	}

	return nil
}

// keepListed keeps p, whom a read named, as listed in the repository keyed key at now,
// unless p is no person's login.
func keepListed(ctx context.Context, q *queries.Queries, key string, p Found, now time.Time) error {
	login := strings.TrimSpace(p.Login)
	if !person(login) {
		return nil
	}

	if err := keepPerson(ctx, q, login, p.Name, now); err != nil {
		return err
	}

	err := q.UpsertAssignableListed(ctx, queries.UpsertAssignableListedParams{
		Repo: key, LoginKey: strings.ToLower(login), ListedAt: &now,
	})
	if err != nil {
		return fmt.Errorf("keep %s as someone who can be assigned: %w", login, err)
	}

	return nil
}

// MarkAssignableFailed records a read of the repository name's assignable users, for query
// ("" for the whole list), that failed, now: no read of the whole list is due for 30
// minutes after one failed.
func (s *Service) MarkAssignableFailed(ctx context.Context, name repo.Name, query string) error {
	key, err := repoKey(name)
	if err != nil {
		return err
	}

	now := s.now()

	return store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		err := q.MarkAssignableFailed(ctx, queries.MarkAssignableFailedParams{
			Repo: key, QueryKey: QueryKey(query), FailedAt: &now,
		})
		if err != nil {
			return fmt.Errorf("record the failed read of who can be assigned: %w", err)
		}

		return pruneAssignable(ctx, q, now, s.maxAssignable)
	})
}

// SeenAssigned keeps the people logins name as assigned in the repository name, now: a read
// saw them assigned to one of its issues or pull requests, or the user assigned them from
// the app. A login GitHub could not have issued, or a bot's, is skipped.
func (s *Service) SeenAssigned(ctx context.Context, name repo.Name, logins []string) error {
	key, err := repoKey(name)
	if err != nil {
		return err
	}

	now := s.now()

	return store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		for _, login := range logins {
			if err := keepAssigned(ctx, q, key, login, now); err != nil {
				return err
			}
		}

		return pruneAssignable(ctx, q, now, s.maxAssignable)
	})
}

// keepAssigned keeps login as assigned in the repository keyed key at now, unless it is no
// person's login.
func keepAssigned(ctx context.Context, q *queries.Queries, key, login string, now time.Time) error {
	login = strings.TrimSpace(login)
	if !person(login) {
		return nil
	}

	if err := keepPerson(ctx, q, login, "", now); err != nil {
		return err
	}

	err := q.UpsertAssignableAssigned(ctx, queries.UpsertAssignableAssignedParams{
		Repo: key, LoginKey: strings.ToLower(login), AssignedAt: &now,
	})
	if err != nil {
		return fmt.Errorf("keep %s as assigned: %w", login, err)
	}

	return nil
}

// Assignable returns who the directory knows can be assigned in the repository name, and
// how complete that is, from the directory alone.
func (s *Service) Assignable(ctx context.Context, name repo.Name) (Assignables, error) {
	key, err := repoKey(name)
	if err != nil {
		return Assignables{}, err
	}

	rows, err := s.q.ListAssignable(ctx, key)
	if err != nil {
		return Assignables{}, fmt.Errorf("read who can be assigned in %s: %w", name, err)
	}

	people, err := store.ConvertRows(rows, func(r queries.ListAssignableRow) (Assignable, error) {
		return Assignable{
			Login: r.Login, Name: r.Name, Listed: r.ListedAt != nil, AssignedAt: store.Deref(r.AssignedAt).UTC(),
		}, nil
	})
	if err != nil {
		return Assignables{}, err
	}

	reads, err := s.q.ListAssignableReads(ctx, key)
	if err != nil {
		return Assignables{}, fmt.Errorf("read when who can be assigned in %s was read: %w", name, err)
	}

	out := Assignables{People: people, Stale: true}

	at := slices.IndexFunc(reads, func(r queries.ListAssignableReadsRow) bool { return r.QueryKey == "" })
	if at < 0 {
		return out, nil
	}

	whole := reads[at]
	out.ReadAt, out.Total, out.Listed = store.Deref(whole.ReadAt).UTC(), int(whole.Total), int(whole.Listed)
	out.Complete = !out.ReadAt.IsZero() && out.Listed >= out.Total
	out.Stale = stale(out.ReadAt, store.Deref(whole.FailedAt), s.now())

	return out, nil
}

// Covers reports that what the directory holds of the repository name's assignable users
// already answers a search for query, so GitHub needn't be asked: a read of the whole list
// named everyone; a search under a week old named everyone it matched, and query starts
// with its words; or a search for the same words is under ten minutes old. An empty query
// is always covered.
func (s *Service) Covers(ctx context.Context, name repo.Name, query string) (bool, error) {
	key, err := repoKey(name)
	if err != nil {
		return false, err
	}

	queryKey := QueryKey(query)
	if queryKey == "" {
		return true, nil
	}

	reads, err := s.q.ListAssignableReads(ctx, key)
	if err != nil {
		return false, fmt.Errorf("read when who can be assigned in %s was read: %w", name, err)
	}

	now := s.now()

	return slices.ContainsFunc(reads, func(r queries.ListAssignableReadsRow) bool {
		return covers(r, queryKey, now)
	}), nil
}

// covers reports that the read r answers a search for queryKey at now, as Covers says.
func covers(r queries.ListAssignableReadsRow, queryKey string, now time.Time) bool {
	if r.ReadAt == nil {
		return false
	}

	age, complete := now.Sub(*r.ReadAt), r.Listed >= r.Total

	switch {
	case r.QueryKey == "":
		return complete
	case r.QueryKey == queryKey && age < repeatFor:
		return true
	default:
		return complete && age < coversFor && strings.HasPrefix(queryKey, r.QueryKey)
	}
}

// OrgRanked returns the people who work on the other repositories of the owner of the
// repository name, ranked as Ranked ranks one repository's, from the latest of what was
// seen there, each with the repository they were last seen in (Person.Where).
func (s *Service) OrgRanked(ctx context.Context, name repo.Name) ([]Person, error) {
	key, err := repoKey(name)
	if err != nil {
		return nil, err
	}

	rows, err := s.q.ListSeenOfOwner(ctx, queries.ListSeenOfOwnerParams{
		OwnerPrefix: strings.ToLower(name.Owner) + "/%", Repo: key, RowLimit: orgRows,
	})
	if err != nil {
		return nil, fmt.Errorf("read who works on the other repositories of %s: %w", name.Owner, err)
	}

	seen, err := store.ConvertRows(rows, func(r queries.ListSeenOfOwnerRow) (seenRow, error) {
		return seenRow{
			repo: r.Repo, key: r.LoginKey, login: r.Login, name: r.Name, number: int(r.Number), role: Role(r.Role),
			access: r.Access, at: r.At.UTC(),
		}, nil
	})
	if err != nil {
		return nil, err
	}

	return rank(seen, s.now()), nil
}

// Summary counts the people the directory knows, and the repositories it knows anyone of.
func (s *Service) Summary(ctx context.Context) (Summary, error) {
	people, err := s.q.CountPeople(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("count the people the app knows: %w", err)
	}

	repos, err := s.q.CountRepos(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("count the repositories the app knows people of: %w", err)
	}

	return Summary{People: int(people), Repos: int(repos)}, nil
}

// Forget empties the directory, in one transaction: every person, what was seen of them,
// who can be assigned where, the teams, and every read, so the pickers learn again from the
// next read.
func (s *Service) Forget(ctx context.Context) error {
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		for _, forget := range []func(context.Context) error{
			q.ForgetPeople, q.ForgetTeams, q.ForgetReads, q.ForgetAssignableReads,
		} {
			if err := forget(ctx); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("forget the people the app learned: %w", err)
	}

	return nil
}

// QueryKey is the key a search for query is kept by: its words trimmed and lower-cased, as
// GitHub matches them; "" is the whole list.
func QueryKey(query string) string {
	return strings.ToLower(strings.TrimSpace(query))
}

// person reports a login the directory keeps as a person's: one GitHub could have issued,
// and not a bot's.
func person(login string) bool {
	return repo.ValidOwner(login) && !strings.HasSuffix(strings.ToLower(login), "[bot]")
}

// keepPerson keeps the person login names, at now, with name, or the name kept when it is
// empty.
func keepPerson(ctx context.Context, q *queries.Queries, login, name string, now time.Time) error {
	err := q.UpsertPerson(ctx, queries.UpsertPersonParams{
		LoginKey: strings.ToLower(login), Login: login, Name: strings.TrimSpace(name), UpdatedAt: now,
	})
	if err != nil {
		return fmt.Errorf("keep %s: %w", login, err)
	}

	return nil
}

// pruneAssignable keeps who can be assigned where to its bounds, at now: a year of rows, the
// latest kept rows in all, a week of searches and a year of the whole list's reads, and the
// people anything names.
func pruneAssignable(ctx context.Context, q *queries.Queries, now time.Time, kept int64) error {
	cutoff := now.Add(-keptFor)
	if _, err := q.PruneAssignableBefore(ctx, &cutoff); err != nil {
		return fmt.Errorf("prune who can be assigned a year ago: %w", err)
	}

	if _, err := q.PruneAssignablePast(ctx, kept); err != nil {
		return fmt.Errorf("prune who can be assigned past the directory's size: %w", err)
	}

	searches := now.Add(-coversFor)
	if _, err := q.PruneAssignableReads(ctx, queries.PruneAssignableReadsParams{
		SearchesBefore: &searches, ListsBefore: &cutoff,
	}); err != nil {
		return fmt.Errorf("prune the reads of who can be assigned: %w", err)
	}

	if _, err := q.PruneUnseenPeople(ctx); err != nil {
		return fmt.Errorf("prune the people nothing names: %w", err)
	}

	return nil
}
