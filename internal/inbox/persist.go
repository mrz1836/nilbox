package inbox

import (
	"context"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/trust"
)

// writer stores one refresh inside a transaction.
type writer struct {
	// q and prefs run in the transaction, so everything written commits or rolls back
	// together.
	q     *queries.Queries
	prefs *prefs.Store

	now       time.Time
	accountID int64
	repos     map[string]int64

	// details holds the items' details by node ID. An item without an entry keeps the
	// details stored before.
	details map[string]github.ItemDetails

	// files holds the outcome of each file check by node ID. An item without an entry
	// keeps the check stored before.
	files map[string]fileCheck

	// saved lists the items stored, in the order the lists returned them, and woke the
	// snoozes new activity ended early, with what ended them, for telling arrivals.
	saved []savedEntry
	woke  map[int64]snooze.Reasons

	// checked holds the items whose file check was stored.
	checked map[int64]bool

	// keep holds the items whose details and file check a newer write stored, which the
	// writer leaves as they are.
	keep map[int64]bool
}

// newWriter returns a writer that stores through q and p, which run in one transaction,
// and stamps what it stores with now.
func newWriter(q *queries.Queries, p *prefs.Store, now time.Time) *writer {
	return &writer{q: q, prefs: p, now: now, repos: make(map[string]int64)}
}

// saveAccount upserts the signed-in account and remembers its id.
func (w *writer) saveAccount(ctx context.Context, login string) error {
	id, err := w.q.UpsertAccount(ctx, queries.UpsertAccountParams{Login: login, CreatedAt: w.now, UpdatedAt: w.now})
	if err != nil {
		return fmt.Errorf("save the account: %w", err)
	}

	w.accountID = id

	return nil
}

// saveItems clears every reason flag for the account, then upserts each fetched item with
// the flags of the lists that returned it, its details, and its file check (an item to
// keep keeps the details and check stored). Items no longer returned keep their row
// (local state may reference it) with all three flags off, and lose their file check.
// Last, it ends the snoozes that the details stored give cause to, so no snapshot shows
// new activity on an item still snoozed.
func (w *writer) saveItems(ctx context.Context, inbox *github.Inbox) error {
	if err := w.q.ClearReasons(ctx, w.accountID); err != nil {
		return fmt.Errorf("clear the reason flags: %w", err)
	}

	for _, e := range mergeLists(inbox) {
		repoID, err := w.repoID(ctx, e.item)
		if err != nil {
			return err
		}

		if err = w.saveItem(ctx, repoID, e); err != nil {
			return err
		}
	}

	if err := w.pruneFiles(ctx); err != nil {
		return err
	}

	return w.wakeSnoozed(ctx)
}

// saveRepos records the repositories of items that were left out, so an archived
// repository is known as one.
func (w *writer) saveRepos(ctx context.Context, items []github.Item) error {
	for _, it := range items {
		if _, err := w.repoID(ctx, it); err != nil {
			return err
		}
	}

	return nil
}

// repoID upserts an item's repository, with what GitHub says about it, once per refresh
// and returns its id.
func (w *writer) repoID(ctx context.Context, it github.Item) (int64, error) {
	key := it.Ref().Repo.Key()
	if id, ok := w.repos[key]; ok {
		return id, nil
	}

	r := it.Repo

	id, err := w.q.UpsertRepo(ctx, queries.UpsertRepoParams{
		Owner: it.RepoOwner, Name: it.RepoName, NodeID: r.NodeID, Description: r.Description,
		Visibility: r.Visibility, IsArchived: r.IsArchived, ArchivedAt: timeOrNil(r.ArchivedAt),
		IsDisabled: r.IsDisabled, IsLocked: r.IsLocked, LockReason: r.LockReason, IsFork: r.IsFork,
		Parent: r.Parent, DefaultBranch: r.DefaultBranch, ViewerPermission: r.ViewerPermission,
		ViewerMergeMethod: r.ViewerMergeMethod, MergeCommitAllowed: r.MergeCommitAllowed,
		SquashMergeAllowed: r.SquashMergeAllowed, RebaseMergeAllowed: r.RebaseMergeAllowed,
		AutoMergeAllowed: r.AutoMergeAllowed, DeleteBranchOnMerge: r.DeleteBranchOnMerge,
		AllowUpdateBranch: r.AllowUpdateBranch, Language: r.Language, LanguageColor: r.LanguageColor,
		PushedAt: timeOrNil(r.PushedAt), FetchedAt: new(w.now), CreatedAt: w.now, UpdatedAt: w.now,
	})
	if err != nil {
		return 0, fmt.Errorf("save the repository %s: %w", it.Ref().Repo, err)
	}

	w.repos[key] = id

	return id, nil
}

// timeOrNil stores a zero time as NULL.
func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}

	return new(t.UTC())
}

// saveItem upserts one item, then its details and file check when this refresh read
// them.
func (w *writer) saveItem(ctx context.Context, repoID int64, e *entry) error {
	it := e.item

	stored, err := w.q.UpsertItem(ctx, queries.UpsertItemParams{
		AccountID: w.accountID, RepoID: repoID, Kind: string(it.Ref().Kind), Number: int64(it.Number),
		NodeID: it.NodeID, Title: it.Title, URL: it.URL, AuthorLogin: it.AuthorLogin, AuthorType: it.AuthorType,
		AuthorAssociation: it.AuthorAssociation, IsCrossRepository: it.IsCrossRepository, IsDraft: it.IsDraft,
		HeadRef: it.HeadRef, ItemCreatedAt: it.CreatedAt.UTC(), ItemUpdatedAt: it.UpdatedAt.UTC(),
		ReasonReviewRequested: e.reviewRequested, ReasonAuthored: e.authored, ReasonAssigned: e.assigned,
		FetchedAt: w.now,
	})
	if err != nil {
		return fmt.Errorf("save the item %s: %w", it.Ref(), err)
	}

	// A list the user left from the inbox stays left while GitHub's search catches up, so
	// the entry takes the lists as stored, and the item does not arrive in one again.
	id := stored.ID
	e.reviewRequested, e.assigned = stored.ReasonReviewRequested, stored.ReasonAssigned
	w.saved = append(w.saved, savedEntry{id: id, entry: e})

	if err = w.saveDetails(ctx, id, it); err != nil {
		return err
	}

	return w.saveFiles(ctx, id, it)
}

// saveDetails stores an item's details when this refresh read them, unless the item is
// one to keep.
func (w *writer) saveDetails(ctx context.Context, id int64, it github.Item) error {
	d, ok := w.details[it.NodeID]
	if !ok || w.keep[id] {
		return nil
	}

	row, err := detailsRow(id, d, w.now)
	if err == nil {
		err = w.q.UpsertDetails(ctx, row)
	}

	if err != nil {
		return fmt.Errorf("save the details of %s: %w", it.Ref(), err)
	}

	return nil
}

// storedLabel is one label as the labels column of item_details stores it.
type storedLabel struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// storedProject is an issue's place in one project as the projects column of
// item_details stores it. Fields is absent from a row a build before them stored.
type storedProject struct {
	Title       string        `json:"title"`
	URL         string        `json:"url"`
	Status      string        `json:"status"`
	StatusColor string        `json:"statusColor"`
	Fields      []storedField `json:"fields"`
}

// storedField is an issue's value in one project field, as storedProject keeps it.
type storedField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Color string `json:"color"`
}

// storedProjectOf converts a fetched project for storing. Its fields are never nil, so
// the column always holds an array.
func storedProjectOf(p github.ProjectItem) storedProject {
	fields := make([]storedField, 0, len(p.Fields))
	for _, f := range p.Fields {
		fields = append(fields, storedField(f))
	}

	return storedProject{Title: p.Title, URL: p.URL, Status: p.Status, StatusColor: p.StatusColor, Fields: fields}
}

// detailsRow converts fetched details to their row.
func detailsRow(itemID int64, d github.ItemDetails, now time.Time) (queries.UpsertDetailsParams, error) {
	labels := make([]storedLabel, 0, len(d.Labels))
	for _, l := range d.Labels {
		labels = append(labels, storedLabel{Name: l.Name, Color: l.Color})
	}

	encodedLabels, err := store.EncodeJSON(labels)
	if err != nil {
		return queries.UpsertDetailsParams{}, err
	}

	projects := make([]storedProject, 0, len(d.Projects))
	for _, p := range d.Projects {
		projects = append(projects, storedProjectOf(p))
	}

	encodedProjects, err := store.EncodeJSON(projects)
	if err != nil {
		return queries.UpsertDetailsParams{}, err
	}

	reviewUsers, reviewTeams, err := encodeReviewRequests(d.ReviewRequests)
	if err != nil {
		return queries.UpsertDetailsParams{}, err
	}

	return queries.UpsertDetailsParams{
		ItemID:             itemID,
		Labels:             encodedLabels,
		Projects:           encodedProjects,
		Comments:           int64(d.Comments),
		Additions:          int64(d.Additions),
		Deletions:          int64(d.Deletions),
		ChangedFiles:       int64(d.ChangedFiles),
		Commits:            int64(d.Commits),
		HeadOID:            d.HeadOID,
		Mergeable:          d.Mergeable,
		ChecksState:        d.Checks.State,
		ChecksPassed:       int64(d.Checks.Passed),
		ChecksFailed:       int64(d.Checks.Failed),
		ChecksPending:      int64(d.Checks.Pending),
		ChecksSkipped:      int64(d.Checks.Skipped),
		ReviewDecision:     d.Reviews.Decision,
		Reviewers:          int64(d.Reviews.Reviewers),
		Reviewed:           int64(d.Reviews.Reviewed),
		Approvals:          int64(d.Reviews.Approvals),
		ChangesRequested:   int64(d.Reviews.ChangesRequested),
		SubIssuesTotal:     int64(d.SubIssues.Total),
		SubIssuesCompleted: int64(d.SubIssues.Completed),
		ReviewUsers:        reviewUsers,
		ReviewTeams:        reviewTeams,
		AutoMergeMethod:    d.AutoMerge.Method,
		AutoMergeAt:        autoMergeAt(d.AutoMerge),
		FetchedAt:          now,
	}, nil
}

// autoMergeAt is when a pull request's auto-merge was turned on, or nil while it is off or
// GitHub did not say.
func autoMergeAt(a github.AutoMerge) *time.Time {
	if !a.On() || a.EnabledAt.IsZero() {
		return nil
	}

	at := a.EnabledAt.UTC()

	return &at
}

// saveRates records one snapshot per resource and prunes snapshots past the retention
// window. The GraphQL row comes from the query itself, so it carries the query's cost.
func (w *writer) saveRates(ctx context.Context, query github.QueryRate, limits *github.RateLimits) error {
	for _, row := range []queries.InsertRateParams{
		w.graphQLSnapshot(query),
		w.restSnapshot(resourceCore, limits.Core),
		w.restSnapshot(resourceSearch, limits.Search),
	} {
		if err := w.q.InsertRate(ctx, row); err != nil {
			return fmt.Errorf("save the rate limits: %w", err)
		}
	}

	// The cutoff is in UTC, as every stored time is, so it compares with taken_at as
	// text in the same format.
	if err := w.q.PruneRates(ctx, w.now.Add(-snapshotRetention)); err != nil {
		return fmt.Errorf("prune old rate limits: %w", err)
	}

	return nil
}

// saveGraphQLRate records the GraphQL budget a single query left, with its cost. The REST
// budgets keep their last readings.
func (w *writer) saveGraphQLRate(ctx context.Context, query github.QueryRate) error {
	if err := w.q.InsertRate(ctx, w.graphQLSnapshot(query)); err != nil {
		return fmt.Errorf("save the rate limit: %w", err)
	}

	return nil
}

// graphQLSnapshot builds a snapshot row from a query's own budget reading.
func (w *writer) graphQLSnapshot(query github.QueryRate) queries.InsertRateParams {
	return queries.InsertRateParams{
		AccountID: w.accountID, Resource: resourceGraphQL, LimitTotal: int64(query.Limit),
		Remaining: int64(query.Remaining), Used: int64(query.Used), Cost: int64(query.Cost),
		ResetAt: query.ResetAt.UTC(), TakenAt: w.now,
	}
}

// restSnapshot builds a snapshot row from a REST budget.
func (w *writer) restSnapshot(resource string, r github.Rate) queries.InsertRateParams {
	return queries.InsertRateParams{
		AccountID: w.accountID, Resource: resource, LimitTotal: int64(r.Limit), Remaining: int64(r.Remaining),
		Used: int64(r.Used), ResetAt: r.ResetAt.UTC(), TakenAt: w.now,
	}
}

// listedNodeIDs returns the node IDs of the items the last refresh listed.
func (s *Service) listedNodeIDs(ctx context.Context) ([]string, error) {
	viewer, _, err := s.prefs.Get(ctx, prefViewer)
	if err != nil {
		return nil, fmt.Errorf("read the listed items: %w", err)
	}

	ids, err := s.q.ListListedNodeIDs(ctx, viewer)
	if err != nil {
		return nil, fmt.Errorf("read the listed items: %w", err)
	}

	return ids, nil
}

// readStatus returns the stored inbox status, and the settings the inbox reads with it,
// that are set.
func readStatus(ctx context.Context, p *prefs.Store) (map[string]string, error) {
	status, err := p.GetMany(ctx,
		prefViewer, prefViewerName, prefViewerURL, prefViewerAvatarURL, prefAvatar, prefAvatarSource,
		prefTotalReviewRequested, prefTotalAuthored, prefTotalAssigned,
		prefLastRefreshAt, prefLastError, prefScopeWarning, prefProjects, trust.PrefBots,
		sensitive.PrefScope, sensitive.PrefOff, sensitive.PrefPatterns,
	)
	if err != nil {
		return nil, fmt.Errorf("read the inbox status: %w", err)
	}

	return status, nil
}
