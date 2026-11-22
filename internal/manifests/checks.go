package manifests

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/manifests/internal/queries"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// checkStore keeps the repositories the user marked up to date, in deps_checks, the one
// table the package owns. A store without a database keeps nothing, so no clone is ever
// marked.
type checkStore struct {
	q *queries.Queries
}

// newCheckStore returns the store over db, which may be nil.
func newCheckStore(db *sql.DB) checkStore {
	if db == nil {
		return checkStore{}
	}

	return checkStore{q: queries.New(db)}
}

// all returns when the user marked each repository up to date, by its name folded
// (repo.Name.Key).
func (c checkStore) all(ctx context.Context) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	if c.q == nil {
		return out, nil
	}

	rows, err := c.q.ListChecks(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the repositories marked up to date: %w", err)
	}

	for _, r := range rows {
		out[repo.Name{Owner: r.RepoOwner, Repo: r.RepoName}.Key()] = r.CheckedAt
	}

	return out, nil
}

// set records that the user marked the repository name up to date at at.
func (c checkStore) set(ctx context.Context, name repo.Name, at time.Time) error {
	if c.q == nil {
		return nil
	}

	err := c.q.SetCheck(ctx, queries.SetCheckParams{RepoOwner: name.Owner, RepoName: name.Repo, CheckedAt: at})
	if err != nil {
		return fmt.Errorf("mark %s up to date: %w", name, err)
	}

	return nil
}

// clear forgets that the user marked the repository name up to date.
func (c checkStore) clear(ctx context.Context, name repo.Name) error {
	if c.q == nil {
		return nil
	}

	if err := c.q.ClearCheck(ctx, queries.ClearCheckParams{RepoOwner: name.Owner, RepoName: name.Repo}); err != nil {
		return fmt.Errorf("unmark %s: %w", name, err)
	}

	return nil
}

// MarkChecked records that the user checked the dependencies of the repository named
// "owner/name" now and found them up to date, for a row's Mark up to date, a triage of
// their own kept on this Mac alone: every clone of it then ages from now, until one of its
// counted manifests changes later (Clone.AgeFrom). Marking it again moves the check to
// now. It reads no clone, and returns the page as last read with the check on it.
func (s *Service) MarkChecked(ctx context.Context, name string) (Page, error) {
	n, err := checkedRepo(name)
	if err != nil {
		return Page{}, err
	}

	if err = s.checks.set(ctx, n, s.clock.Now()); err != nil {
		return Page{}, uierr.New("Couldn't mark "+n.String()+" up to date.", err)
	}

	return s.servePage(ctx), nil
}

// ClearChecked forgets that the user marked the repository named "owner/name" up to date,
// for a row's Unmark, so its clones age from their manifests alone again; one never marked
// is left as it is. It reads no clone, and returns the page as last read without the check.
func (s *Service) ClearChecked(ctx context.Context, name string) (Page, error) {
	n, err := checkedRepo(name)
	if err != nil {
		return Page{}, err
	}

	if err = s.checks.clear(ctx, n); err != nil {
		return Page{}, uierr.New("Couldn't unmark "+n.String()+".", err)
	}

	return s.servePage(ctx), nil
}

// checkedRepo reads the repository a check names, "owner/name", refusing anything else.
func checkedRepo(name string) (repo.Name, error) {
	n, err := repo.Parse(name)
	if err != nil {
		return repo.Name{}, uierr.New("That isn't a repository.", fmt.Errorf("%w: the repository: %w", ErrInvalidRequest, err))
	}

	return n, nil
}

// servePage returns the last read's page with the checks as they are now on it, sorted
// (withChecks). Checks that can't be read are logged, and the last ones read stand.
func (s *Service) servePage(ctx context.Context) Page {
	return withChecks(s.lastPage(), s.readChecks(ctx))
}

// readChecks reads when the user marked each repository up to date, by its name folded.
// Checks that can't be read are logged, and the last ones read stand.
func (s *Service) readChecks(ctx context.Context) map[string]time.Time {
	checks, err := s.checks.all(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.WarnContext(ctx, "couldn't read the repositories marked up to date; using the last ones read",
				"error", err)
		}

		s.mu.Lock()
		defer s.mu.Unlock()

		return maps.Clone(s.lastChecks)
	}

	s.mu.Lock()
	s.lastChecks = checks
	s.mu.Unlock()

	return checks
}

// withChecks puts checks, when the user marked each repository up to date by its name
// folded, on page's clones, which are its own, each one's age counting from then on
// (ageFrom), and sorts them by it.
func withChecks(page Page, checks map[string]time.Time) Page {
	for i := range page.Clones {
		c := &page.Clones[i]
		checked := checks[c.key]

		c.CheckedAt = stamp(checked)
		c.aged = ageFrom(c.oldest, checked, c.Error != "")
		c.AgeFrom = stamp(c.aged)
	}

	sortClones(page.Clones)

	return page
}

// ageFrom returns when a clone's age counts from: the later of oldest, when its stalest
// counted module last changed, and checked, when the user marked it up to date, either
// the zero time when it isn't known; or the zero time for a clone whose read failed,
// checked or not, so it is never taken for a fresh one.
func ageFrom(oldest, checked time.Time, failed bool) time.Time {
	switch {
	case failed:
		return time.Time{}
	case checked.After(oldest):
		return checked
	default:
		return oldest
	}
}

// sortClones sorts clones as the page lists them: stalest first by when each one's age
// counts from, those with none last, then by repository, ignoring case, and folder.
func sortClones(clones []Clone) {
	slices.SortStableFunc(clones, func(a, b Clone) int {
		return cmp.Or(compareAges(a.aged.IsZero(), b.aged.IsZero(), a.aged.Compare(b.aged)),
			cmp.Compare(strings.ToLower(a.Repo), strings.ToLower(b.Repo)), cmp.Compare(a.Dir, b.Dir))
	})
}
