package people

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/people/internal/queries"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/store"
)

// The directory's bounds, and how often a repository is read.
const (
	// maxPullsPerRepo is how many of a repository's pull request numbers, the newest, are
	// kept; maxSeen how many rows are kept in all, the latest seen; and keptFor how old a
	// row, a team no teams list names, and a read are kept.
	maxPullsPerRepo = 400
	maxSeen         = 100_000
	keptFor         = 365 * 24 * time.Hour

	// readEvery is how old a repository's last read is when it is read again, and
	// retryAfter how long after a failed read none is asked for.
	readEvery  = 7 * 24 * time.Hour
	retryAfter = 30 * time.Minute
)

// Role is what a person did on a pull request.
type Role string

// The roles, as the people_seen table names them.
const (
	// RoleAuthor opened the pull request.
	RoleAuthor Role = "author"

	// RoleReviewer left a review on it.
	RoleReviewer Role = "reviewer"

	// RoleCommitter authored one of its commits.
	RoleCommitter Role = "committer"

	// RoleRequested was asked to review it.
	RoleRequested Role = "requested"

	// RoleSuggested was suggested by GitHub as a reviewer of it.
	RoleSuggested Role = "suggested"

	// RolePicked was asked to review it by the user, from the app.
	RolePicked Role = "picked"
)

// ErrInvalid is returned for a repository name GitHub could not have issued.
var ErrInvalid = errors.New("invalid people directory entry")

// Seen is one thing a read saw a person do on one pull request of a repository.
type Seen struct {
	// Login is the person's, and Name their profile's display name, "" when the read gave
	// none.
	Login string
	Name  string

	// Number is the pull request's.
	Number int

	Role Role

	// Access reports an association of OWNER, MEMBER, or COLLABORATOR on the pull request
	// they opened or the review they left.
	Access bool

	// At is GitHub's time of what was seen; zero is now.
	At time.Time
}

// Team is one of a repository's teams: its slug, as a review request names it, and its
// display name, "" when none was read.
type Team struct {
	Slug string
	Name string
}

// Person is someone who works on a repository, as Ranked lists them.
type Person struct {
	Login string
	Name  string

	// Reviewed, Authored, and Committed count the pull requests they reviewed, opened, and
	// committed to, of those the directory keeps.
	Reviewed  int
	Authored  int
	Committed int

	// LastAt is the latest time they were seen there, and Where the repository they were
	// seen in then, as "owner/name", for OrgRanked; "" for Ranked, whose rows are all of one.
	LastAt time.Time
	Where  string

	// Score is what they rank by: what they did there, the latest counting most.
	Score float64
}

// Ranked is who works on a repository, as the directory knows it.
type Ranked struct {
	// People are those who work on it, the highest ranked first, never nil.
	People []Person

	// Teams are its teams, the most recently asked to review first, never nil.
	Teams []Team

	// ReadAt is when its recent pull requests were last read, zero before they were, Pulls
	// how many that read read, and Org whether an organization owns it, as that read found.
	ReadAt time.Time
	Pulls  int
	Org    bool

	// Stale reports that a read of its recent pull requests is due: there was none, or the
	// last is over a week old. It is false for 30 minutes after a read failed.
	Stale bool
}

// Config holds a Service's dependencies. Pool is required.
type Config struct {
	Pool  *sql.DB
	Clock clock.Clock
}

// Service is the people directory. It is safe for concurrent use.
type Service struct {
	db    *sql.DB
	q     *queries.Queries
	clock clock.Clock

	// maxSeen is how many rows of what was seen are kept in all, and maxAssignable how many
	// of who can be assigned where: maxSeen and maxAssignable, which a test lowers to prune a
	// small table.
	maxSeen       int64
	maxAssignable int64
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	return &Service{
		db: cfg.Pool, q: queries.New(cfg.Pool), clock: clock.Or(cfg.Clock), maxSeen: maxSeen, maxAssignable: maxAssignable,
	}
}

// Learn keeps what a read saw people do on the pull requests of the repository name, in one
// transaction that also prunes the directory to its bounds. An entry without a login GitHub
// could have issued, a pull request number, or a known role is skipped; one seen before
// keeps the access either saw, and the later time.
func (s *Service) Learn(ctx context.Context, name repo.Name, seen []Seen) error {
	key, err := repoKey(name)
	if err != nil {
		return err
	}

	now := s.now()

	return store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		for _, e := range seen {
			if !valid(e) {
				continue
			}

			if err := learnOne(ctx, q, key, e, now); err != nil {
				return err
			}
		}

		return prune(ctx, q, key, now, s.maxSeen)
	})
}

// learnOne keeps one entry of what was seen on the repository keyed key.
func learnOne(ctx context.Context, q *queries.Queries, key string, e Seen, now time.Time) error {
	login := strings.TrimSpace(e.Login)
	loginKey := strings.ToLower(login)

	err := q.UpsertPerson(ctx, queries.UpsertPersonParams{
		LoginKey: loginKey, Login: login, Name: strings.TrimSpace(e.Name), UpdatedAt: now,
	})
	if err != nil {
		return fmt.Errorf("keep %s: %w", login, err)
	}

	at := e.At.UTC()
	if e.At.IsZero() {
		at = now
	}

	err = q.UpsertSeen(ctx, queries.UpsertSeenParams{
		Repo: key, LoginKey: loginKey, Number: int64(e.Number), Role: string(e.Role), Access: e.Access, At: at,
	})
	if err != nil {
		return fmt.Errorf("keep what %s did on #%d: %w", login, e.Number, err)
	}

	return nil
}

// prune keeps the directory to its bounds, at now: the repository keyed key's newest
// pull request numbers, a year of rows, the latest kept rows in all, and the people they
// name.
func prune(ctx context.Context, q *queries.Queries, key string, now time.Time, kept int64) error {
	if _, err := q.PruneSeenOfRepo(ctx, queries.PruneSeenOfRepoParams{Repo: key, RowLimit: maxPullsPerRepo}); err != nil {
		return fmt.Errorf("prune the repository's pull requests: %w", err)
	}

	if _, err := q.PruneSeenBefore(ctx, now.Add(-keptFor)); err != nil {
		return fmt.Errorf("prune what was seen a year ago: %w", err)
	}

	if _, err := q.PruneSeenPast(ctx, kept); err != nil {
		return fmt.Errorf("prune what was seen past the directory's size: %w", err)
	}

	if _, err := q.PruneUnseenPeople(ctx); err != nil {
		return fmt.Errorf("prune the people nothing names: %w", err)
	}

	return nil
}

// SetTeams keeps teams of the repository name. When listed, they are its teams list as
// GitHub gave it, which replaces the list kept: a team it no longer names stays only while
// a pull request asked it to review in the last year. Otherwise they are teams a pull
// request asked to review, now. A slug that is empty is skipped. Either way the teams kept
// are pruned in the same transaction.
func (s *Service) SetTeams(ctx context.Context, name repo.Name, teams []Team, listed bool) error {
	key, err := repoKey(name)
	if err != nil {
		return err
	}

	now := s.now()

	return store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		return setTeams(ctx, s.q.WithTx(tx), key, teams, listed, now)
	})
}

// setTeams keeps teams of the repository keyed key, as SetTeams says, at now, and prunes
// the teams kept.
func setTeams(ctx context.Context, q *queries.Queries, key string, teams []Team, listed bool, now time.Time) error {
	if listed {
		if err := q.UnlistTeams(ctx, key); err != nil {
			return fmt.Errorf("unlist the teams: %w", err)
		}
	}

	for _, t := range teams {
		if err := keepTeam(ctx, q, key, t, listed, now); err != nil {
			return err
		}
	}

	cutoff := now.Add(-keptFor)
	if _, err := q.PruneTeams(ctx, &cutoff); err != nil {
		return fmt.Errorf("prune the teams: %w", err)
	}

	return nil
}

// keepTeam keeps one team of the repository keyed key: listed, or asked to review at now.
func keepTeam(ctx context.Context, q *queries.Queries, key string, t Team, listed bool, now time.Time) error {
	slug := strings.TrimSpace(t.Slug)
	if slug == "" {
		return nil
	}

	var err error

	if listed {
		err = q.UpsertListedTeam(ctx, queries.UpsertListedTeamParams{
			Repo: key, SlugKey: strings.ToLower(slug), Slug: slug, Name: strings.TrimSpace(t.Name),
		})
	} else {
		err = q.UpsertRequestedTeam(ctx, queries.UpsertRequestedTeamParams{
			Repo: key, SlugKey: strings.ToLower(slug), Slug: slug, Name: strings.TrimSpace(t.Name), RequestedAt: &now,
		})
	}

	if err != nil {
		return fmt.Errorf("keep the team %s: %w", slug, err)
	}

	return nil
}

// MarkRead records a read of the recent pull requests of the repository name that went
// through, now: it read pulls of them, and found an organization owns it (org) or not. It
// clears a failure, and prunes the reads kept.
func (s *Service) MarkRead(ctx context.Context, name repo.Name, pulls int, org bool) error {
	key, err := repoKey(name)
	if err != nil {
		return err
	}

	now := s.now()

	return store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		if err := q.MarkRead(ctx, queries.MarkReadParams{Repo: key, ReadAt: &now, Pulls: int64(max(pulls, 0)), Org: org}); err != nil {
			return fmt.Errorf("record the read: %w", err)
		}

		return pruneReads(ctx, q, now)
	})
}

// MarkFailed records a read of the recent pull requests of the repository name that
// failed, now, so none is due for 30 minutes, and prunes the reads kept.
func (s *Service) MarkFailed(ctx context.Context, name repo.Name) error {
	key, err := repoKey(name)
	if err != nil {
		return err
	}

	now := s.now()

	return store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		if err := q.MarkFailed(ctx, queries.MarkFailedParams{Repo: key, FailedAt: &now}); err != nil {
			return fmt.Errorf("record the failed read: %w", err)
		}

		return pruneReads(ctx, q, now)
	})
}

// pruneReads drops the reads of the repositories neither read nor failed in a year.
func pruneReads(ctx context.Context, q *queries.Queries, now time.Time) error {
	cutoff := now.Add(-keptFor)
	if _, err := q.PruneReads(ctx, &cutoff); err != nil {
		return fmt.Errorf("prune the reads: %w", err)
	}

	return nil
}

// Ranked returns who works on the repository name, with its teams and when it was last
// read, from the directory alone.
func (s *Service) Ranked(ctx context.Context, name repo.Name) (Ranked, error) {
	key, err := repoKey(name)
	if err != nil {
		return Ranked{}, err
	}

	now := s.now()

	rows, err := s.q.ListSeen(ctx, key)
	if err != nil {
		return Ranked{}, fmt.Errorf("read who works on %s: %w", name, err)
	}

	seen, err := store.ConvertRows(rows, func(r queries.ListSeenRow) (seenRow, error) {
		return seenRow{
			key: r.LoginKey, login: r.Login, name: r.Name, number: int(r.Number), role: Role(r.Role), access: r.Access,
			at: r.At.UTC(),
		}, nil
	})
	if err != nil {
		return Ranked{}, err
	}

	teamRows, err := s.q.ListTeams(ctx, key)
	if err != nil {
		return Ranked{}, fmt.Errorf("read the teams of %s: %w", name, err)
	}

	teams, err := store.ConvertRows(teamRows, func(r queries.ListTeamsRow) (Team, error) {
		return Team{Slug: r.Slug, Name: r.Name}, nil
	})
	if err != nil {
		return Ranked{}, err
	}

	out := Ranked{People: rank(seen, now), Teams: teams, Stale: true}

	read, err := s.q.GetRead(ctx, key)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return out, nil
	case err != nil:
		return Ranked{}, fmt.Errorf("read when %s was read: %w", name, err)
	}

	out.ReadAt, out.Pulls, out.Org = store.Deref(read.ReadAt).UTC(), int(read.Pulls), read.Org
	out.Stale = stale(out.ReadAt, store.Deref(read.FailedAt), now)

	return out, nil
}

// stale reports a read due at now, for a repository last read at readAt and whose last
// read failed at failedAt (zero for never): one read over readEvery ago, or never, unless
// one failed in the last retryAfter.
func stale(readAt, failedAt, now time.Time) bool {
	if !failedAt.IsZero() && now.Sub(failedAt) < retryAfter {
		return false
	}

	return readAt.IsZero() || now.Sub(readAt) >= readEvery
}

// valid reports an entry the directory keeps: a login GitHub could have issued, a pull
// request number, and a known role.
func valid(e Seen) bool {
	if e.Number <= 0 || !repo.ValidOwner(strings.TrimSpace(e.Login)) {
		return false
	}

	switch e.Role {
	case RoleAuthor, RoleReviewer, RoleCommitter, RoleRequested, RoleSuggested, RolePicked:
		return true
	default:
		return false
	}
}

// repoKey is the key the directory keeps the repository name by, or ErrInvalid.
func repoKey(name repo.Name) (string, error) {
	if !name.Valid() {
		return "", fmt.Errorf("%w: repository %q", ErrInvalid, name.String())
	}

	return name.Key(), nil
}

// now returns the current time in UTC.
func (s *Service) now() time.Time {
	return s.clock.Now().UTC()
}
