package inbox

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/store"
)

// Store gives the packages around the inbox what they need of its tables, without the
// refreshes, triage, and GitHub client of a Service: the items a launch is for, the clone
// each repository resolved to, the repositories the inbox has seen, and the signed-in
// account. The tables (accounts, repos, items, item_details, item_files,
// item_local_state, and rate_snapshots) belong to this package; no other touches them.
type Store struct {
	q     *queries.Queries
	prefs *prefs.Store
	clock clock.Clock
}

// NewStore returns a Store over the pool db. RememberClone stamps the repository from c;
// a nil c is the system clock.
func NewStore(db *sql.DB, c clock.Clock) *Store {
	c = clock.Or(c)

	return &Store{q: queries.New(db), prefs: prefs.New(db, c), clock: c}
}

// ViewerLogin returns the signed-in account's login as the last refresh recorded it, or ""
// before the first refresh.
func (s *Store) ViewerLogin(ctx context.Context) (string, error) {
	login, _, err := s.prefs.Get(ctx, prefViewer)
	if err != nil {
		return "", fmt.Errorf("read the signed-in account: %w", err)
	}

	return login, nil
}

// RememberClone records path as the local clone of the repository with repoID, so the
// next launch in it can skip the scan of the clone root. Nothing changes when no
// repository has repoID.
func (s *Store) RememberClone(ctx context.Context, repoID int64, path string) error {
	err := s.q.SetRepoLocalPath(ctx, queries.SetRepoLocalPathParams{
		LocalPath: path, UpdatedAt: s.clock.Now().UTC(), ID: repoID,
	})
	if err != nil {
		return fmt.Errorf("remember the repository's clone: %w", err)
	}

	return nil
}

// LaunchItem is a stored item as a launch reads it: the item, its repository with the
// clone last remembered for it, and the details a prompt can name, which are zero until
// a refresh has read them. The pull request fields are zero, or "", for an issue.
type LaunchItem struct {
	ID     int64
	Kind   item.Kind
	Number int
	URL    string
	Title  string

	// HeadRef is a pull request's head branch.
	HeadRef string

	AuthorLogin       string
	AuthorType        string
	AuthorAssociation string
	IsCrossRepository bool
	IsDraft           bool

	// RepoID, Owner, and Name are the item's repository.
	RepoID int64
	Owner  string
	Name   string

	// LocalPath is the clone RememberClone last recorded for the repository, or "".
	LocalPath     string
	DefaultBranch string

	// Labels names the item's labels, in GitHub's order.
	Labels []string

	Additions    int
	Deletions    int
	ChangedFiles int
	Commits      int

	// Mergeable, ChecksState, and ReviewDecision are GitHub's words, as a refresh read
	// them.
	Mergeable      string
	ChecksState    string
	ReviewDecision string

	// HeadOID is a pull request's head commit as the last refresh read it, or "".
	HeadOID string
}

// LaunchItems returns the stored items among ids, by id. An id with no stored item is
// left out.
func (s *Store) LaunchItems(ctx context.Context, ids []int64) ([]LaunchItem, error) {
	if len(ids) == 0 {
		return []LaunchItem{}, nil
	}

	rows, err := s.q.ListLaunchItems(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read the items: %w", err)
	}

	items := make([]LaunchItem, 0, len(rows))

	for _, r := range rows {
		it, convErr := launchItemOf(r)
		if convErr != nil {
			return nil, fmt.Errorf("read the items: %w", convErr)
		}

		items = append(items, it)
	}

	return items, nil
}

// launchItemOf converts a ListLaunchItems row, giving missing details their zero values.
func launchItemOf(r queries.ListLaunchItemsRow) (LaunchItem, error) {
	labels := []string{}

	if r.Labels != nil {
		stored, err := store.DecodeJSON[[]storedLabel](*r.Labels)
		if err != nil {
			return LaunchItem{}, fmt.Errorf("the labels of item %d: %w", r.ID, err)
		}

		for _, l := range stored {
			labels = append(labels, l.Name)
		}
	}

	return LaunchItem{
		ID: r.ID, Kind: item.Kind(r.Kind), Number: int(r.Number), URL: r.URL, Title: r.Title, HeadRef: r.HeadRef,
		AuthorLogin: r.AuthorLogin, AuthorType: r.AuthorType, AuthorAssociation: r.AuthorAssociation,
		IsCrossRepository: r.IsCrossRepository, IsDraft: r.IsDraft,
		RepoID: r.RepoID, Owner: r.Owner, Name: r.Name, LocalPath: r.LocalPath, DefaultBranch: r.DefaultBranch,
		Labels: labels, Additions: int(value(r.Additions)), Deletions: int(value(r.Deletions)),
		ChangedFiles: int(value(r.ChangedFiles)), Commits: int(value(r.Commits)), Mergeable: value(r.Mergeable),
		ChecksState: value(r.ChecksState), ReviewDecision: value(r.ReviewDecision), HeadOID: value(r.HeadOID),
	}, nil
}
