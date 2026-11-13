package inbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
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

	items, err := store.ConvertRows(rows, launchItemOf)
	if err != nil {
		return nil, fmt.Errorf("read the items: %w", err)
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
		Labels: labels, Additions: int(store.Deref(r.Additions)), Deletions: int(store.Deref(r.Deletions)),
		ChangedFiles: int(store.Deref(r.ChangedFiles)), Commits: int(store.Deref(r.Commits)),
		Mergeable: store.Deref(r.Mergeable), ChecksState: store.Deref(r.ChecksState),
		ReviewDecision: store.Deref(r.ReviewDecision), HeadOID: store.Deref(r.HeadOID),
	}, nil
}

// ReviewItem is a stored pull request as the review sheet shows it before asking GitHub:
// the item, its repository's merge settings and the viewer's access, the lists it is in,
// and the details a refresh read, which are zero until one has.
type ReviewItem struct {
	ID     int64
	Kind   item.Kind
	Number int
	NodeID string
	Title  string
	URL    string

	AuthorLogin       string
	AuthorType        string
	AuthorAssociation string
	IsCrossRepository bool
	IsDraft           bool

	// HeadRef is the head branch.
	HeadRef string

	CreatedAt time.Time
	UpdatedAt time.Time

	ReviewRequested bool
	Authored        bool
	Assigned        bool

	// Owner and Name are the repository, and DefaultBranch its default branch.
	Owner         string
	Name          string
	DefaultBranch string

	// ViewerPermission is the viewer's access to the repository, and ViewerMergeMethod
	// the merge method GitHub offers the viewer first.
	ViewerPermission  string
	ViewerMergeMethod string

	MergeCommitAllowed  bool
	SquashMergeAllowed  bool
	RebaseMergeAllowed  bool
	AutoMergeAllowed    bool
	DeleteBranchOnMerge bool
	AllowUpdateBranch   bool

	// Viewer is the account whose inbox lists the pull request.
	Viewer string

	// HasDetails reports that a refresh has read the details below.
	HasDetails bool

	// Labels names the labels, in GitHub's order.
	Labels []string

	Comments     int
	Additions    int
	Deletions    int
	ChangedFiles int
	Commits      int

	// Mergeable, ChecksState, and ReviewDecision are GitHub's words.
	Mergeable      string
	ChecksState    string
	ChecksPassed   int
	ChecksFailed   int
	ChecksPending  int
	ChecksSkipped  int
	ReviewDecision string

	// ChecksAwaiting counts the workflow runs held for a maintainer's approval.
	ChecksAwaiting int

	Reviewers        int
	Reviewed         int
	Approvals        int
	ChangesRequested int

	// HeadOID is the head commit, and AutoMerge the method auto-merge merges with, ""
	// while it is off.
	HeadOID   string
	AutoMerge string

	// DetailsAt is when a refresh read the details; zero before one has.
	DetailsAt time.Time
}

// ReviewItem returns the stored item with id as the review sheet reads it, or an error
// matching item.ErrNotFound when no item has id.
func (s *Store) ReviewItem(ctx context.Context, id int64) (ReviewItem, error) {
	r, err := s.q.GetReviewItem(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ReviewItem{}, fmt.Errorf("%w (id %d)", item.ErrNotFound, id)
	}

	if err != nil {
		return ReviewItem{}, fmt.Errorf("read the item: %w", err)
	}

	labels := []string{}

	if r.Labels != nil {
		stored, decodeErr := store.DecodeJSON[[]storedLabel](*r.Labels)
		if decodeErr != nil {
			return ReviewItem{}, fmt.Errorf("the labels of item %d: %w", r.ID, decodeErr)
		}

		for _, l := range stored {
			labels = append(labels, l.Name)
		}
	}

	return ReviewItem{
		ID: r.ID, Kind: item.Kind(r.Kind), Number: int(r.Number), NodeID: r.NodeID, Title: r.Title, URL: r.URL,
		AuthorLogin: r.AuthorLogin, AuthorType: r.AuthorType, AuthorAssociation: r.AuthorAssociation,
		IsCrossRepository: r.IsCrossRepository, IsDraft: r.IsDraft, HeadRef: r.HeadRef,
		CreatedAt: r.ItemCreatedAt.UTC(), UpdatedAt: r.ItemUpdatedAt.UTC(),
		ReviewRequested: r.ReasonReviewRequested, Authored: r.ReasonAuthored, Assigned: r.ReasonAssigned,
		Owner: r.RepoOwner, Name: r.RepoName, DefaultBranch: r.DefaultBranch, ViewerPermission: r.ViewerPermission,
		ViewerMergeMethod: r.ViewerMergeMethod, MergeCommitAllowed: r.MergeCommitAllowed,
		SquashMergeAllowed: r.SquashMergeAllowed, RebaseMergeAllowed: r.RebaseMergeAllowed,
		AutoMergeAllowed: r.AutoMergeAllowed, DeleteBranchOnMerge: r.DeleteBranchOnMerge,
		AllowUpdateBranch: r.AllowUpdateBranch, Viewer: r.AccountLogin,
		HasDetails: r.DetailsItemID != nil, Labels: labels, Comments: int(store.Deref(r.Comments)),
		Additions: int(store.Deref(r.Additions)), Deletions: int(store.Deref(r.Deletions)),
		ChangedFiles: int(store.Deref(r.ChangedFiles)), Commits: int(store.Deref(r.Commits)),
		Mergeable: store.Deref(r.Mergeable), ChecksState: store.Deref(r.ChecksState),
		ChecksPassed: int(store.Deref(r.ChecksPassed)), ChecksFailed: int(store.Deref(r.ChecksFailed)),
		ChecksPending: int(store.Deref(r.ChecksPending)), ChecksSkipped: int(store.Deref(r.ChecksSkipped)),
		ChecksAwaiting: int(store.Deref(r.ChecksAwaiting)), ReviewDecision: store.Deref(r.ReviewDecision), Reviewers: int(store.Deref(r.Reviewers)),
		Reviewed: int(store.Deref(r.Reviewed)), Approvals: int(store.Deref(r.Approvals)),
		ChangesRequested: int(store.Deref(r.ChangesRequested)), HeadOID: store.Deref(r.HeadOID),
		AutoMerge: store.Deref(r.AutoMergeMethod), DetailsAt: store.Deref(r.DetailsFetchedAt).UTC(),
	}, nil
}

// IssueItem is a stored issue as the issue sheet shows it before asking GitHub: the item,
// its repository's default branch and the viewer's access to it, the lists it is in, and
// its card's details, which are zero until a refresh has read them. Its labels, projects,
// and sub-issue counts are the GitHub client's own types, as the card stores them.
type IssueItem struct {
	ID     int64
	Kind   item.Kind
	Number int
	NodeID string
	Title  string
	URL    string

	AuthorLogin       string
	AuthorType        string
	AuthorAssociation string

	CreatedAt time.Time
	UpdatedAt time.Time

	// Assigned reports that the issue is in the Assigned list, the only list an issue is in.
	Assigned bool

	// Owner and Name are the repository, and DefaultBranch its default branch.
	Owner         string
	Name          string
	DefaultBranch string

	// ViewerPermission is the viewer's access to the repository: ADMIN, MAINTAIN, WRITE,
	// TRIAGE, or READ.
	ViewerPermission string

	// Viewer is the account whose inbox lists the issue.
	Viewer string

	// HasDetails reports that a refresh has read the details below.
	HasDetails bool

	// Labels and Projects are in GitHub's order; never nil.
	Labels   []github.Label
	Comments int
	Projects []github.ProjectItem

	SubIssues github.SubIssues

	// DetailsAt is when a refresh read the details; zero before one has.
	DetailsAt time.Time
}

// IssueItem returns the stored item with id as the issue sheet reads it, or an error
// matching item.ErrNotFound when no item has id.
func (s *Store) IssueItem(ctx context.Context, id int64) (IssueItem, error) {
	r, err := s.q.GetIssueItem(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return IssueItem{}, fmt.Errorf("%w (id %d)", item.ErrNotFound, id)
	}

	if err != nil {
		return IssueItem{}, fmt.Errorf("read the item: %w", err)
	}

	labels, err := decodeColumn[storedLabel](r.Labels)
	if err != nil {
		return IssueItem{}, fmt.Errorf("the labels of item %d: %w", r.ID, err)
	}

	projects, err := decodeColumn[storedProject](r.Projects)
	if err != nil {
		return IssueItem{}, fmt.Errorf("the projects of item %d: %w", r.ID, err)
	}

	return IssueItem{
		ID: r.ID, Kind: item.Kind(r.Kind), Number: int(r.Number), NodeID: r.NodeID, Title: r.Title, URL: r.URL,
		AuthorLogin: r.AuthorLogin, AuthorType: r.AuthorType, AuthorAssociation: r.AuthorAssociation,
		CreatedAt: r.ItemCreatedAt.UTC(), UpdatedAt: r.ItemUpdatedAt.UTC(), Assigned: r.ReasonAssigned,
		Owner: r.RepoOwner, Name: r.RepoName, DefaultBranch: r.DefaultBranch, ViewerPermission: r.ViewerPermission,
		Viewer: r.AccountLogin, HasDetails: r.DetailsItemID != nil, Labels: githubLabels(labels),
		Comments: int(store.Deref(r.Comments)), Projects: githubProjects(projects),
		SubIssues: github.SubIssues{
			Total: int(store.Deref(r.SubIssuesTotal)), Completed: int(store.Deref(r.SubIssuesCompleted)),
		},
		DetailsAt: store.Deref(r.DetailsFetchedAt).UTC(),
	}, nil
}

// githubLabels converts stored labels, never returning nil.
func githubLabels(stored []storedLabel) []github.Label {
	out := make([]github.Label, 0, len(stored))
	for _, l := range stored {
		out = append(out, github.Label(l))
	}

	return out
}

// githubProjects converts stored projects, never returning nil.
func githubProjects(stored []storedProject) []github.ProjectItem {
	out := make([]github.ProjectItem, 0, len(stored))
	for _, p := range stored {
		out = append(out, p.githubProject())
	}

	return out
}
