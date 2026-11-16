package inbox

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/trust"
)

// The inbox's reads for the routines: the rows their facts are built from, the items each
// one watches, what the builder suggests, the check names of the pull requests they act
// on, and a draft's searches read now, for its test. Autopilot reaches them through the
// narrow interfaces it declares, which core adapts.

// FactsRows returns what a routine's facts are built from for each stored item among ids,
// by id: the item, its repository with the user's rules for it, its triage, and, once
// read, its details, its head commit's checks, and its changed files. An id with no
// stored item is left out. It reads the database alone.
func (s *Store) FactsRows(ctx context.Context, ids []int64) ([]facts.Row, error) {
	if len(ids) == 0 {
		return []facts.Row{}, nil
	}

	stored, err := s.prefs.GetMany(ctx, reporules.Keys()...)
	if err != nil {
		return nil, fmt.Errorf("read the repository rules: %w", err)
	}

	rules := reporules.Decode(stored)

	rows, err := s.q.ListFactsRows(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read the items' facts: %w", err)
	}

	out, err := store.ConvertRows(rows, func(r queries.ListFactsRowsRow) (facts.Row, error) { return factsRowOf(r, rules) })
	if err != nil {
		return nil, fmt.Errorf("read the items' facts: %w", err)
	}

	return out, nil
}

// FactsEnv returns what a routine's facts are built with beyond their rows, as of now:
// the signed-in account, the trusted bots, and the sensitive-file settings.
func (s *Store) FactsEnv(ctx context.Context, now time.Time) (facts.Env, error) {
	status, err := readStatus(ctx, s.prefs)
	if err != nil {
		return facts.Env{}, err
	}

	raw, stored := status[trust.PrefBots]

	return facts.Env{
		Now: now, Viewer: status[prefViewer], Trust: trust.NewPolicy(trust.DecodeBots(raw, stored)),
		Sensitive: sensitive.DecodeConfig(status),
	}, nil
}

// factsRowOf converts one ListFactsRows row.
func factsRowOf(r queries.ListFactsRowsRow, rules reporules.Rules) (facts.Row, error) {
	name := repo.Name{Owner: r.RepoOwner, Repo: r.RepoName}

	out := facts.Row{
		ID: r.ID, Kind: item.Kind(r.Kind), Repo: name, Number: int(r.Number), Title: r.Title, URL: r.URL,
		Author: r.AuthorLogin, AuthorType: r.AuthorType, Association: r.AuthorAssociation, Draft: r.IsDraft,
		HeadRef: r.HeadRef, CrossRepository: r.IsCrossRepository, CreatedAt: r.ItemCreatedAt.UTC(),
		UpdatedAt: r.ItemUpdatedAt.UTC(), InReviewRequested: r.ReasonReviewRequested, InAuthored: r.ReasonAuthored,
		InAssigned: r.ReasonAssigned, ReviewRequestedAt: utcOrNil(r.ReviewRequestedAt), AssignedAt: utcOrNil(r.AssignedAt),
		SnoozedUntil: utcOrNil(r.SnoozedUntil), DismissedAt: utcOrNil(r.DismissedAt), PinnedAt: utcOrNil(r.PinnedAt),
		Repository: facts.RepoRow{
			Read: r.RepoFetchedAt != nil, Visibility: r.Visibility, Permission: r.ViewerPermission, Fork: r.IsFork,
			AllowUpdateBranch: r.AllowUpdateBranch, Muted: rules.IsMuted(name), Pinned: rules.IsPinned(name),
		},
	}

	if r.RepoFetchedAt != nil {
		out.Repository.Language = new(r.Language)
	}

	if r.LocalPath != "" {
		out.Repository.HasClone = new(true)
	}

	var err error

	if out.Details, err = factsDetails(r); err != nil {
		return facts.Row{}, err
	}

	if out.Checks, err = factsChecks(r.ChecksItemID, r.ChecksHeadOID, r.Checks, r.ChecksComplete); err != nil {
		return facts.Row{}, fmt.Errorf("the checks of item %d: %w", r.ID, err)
	}

	if out.Files, err = factsFiles(r); err != nil {
		return facts.Row{}, fmt.Errorf("the files of item %d: %w", r.ID, err)
	}

	return out, nil
}

// utcOrNil returns t in UTC, or nil.
func utcOrNil(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}

	return new(t.UTC())
}

// factsDetails converts a row's details, or returns nil when none were read.
func factsDetails(r queries.ListFactsRowsRow) (*facts.DetailsRow, error) {
	if r.DetailsItemID == nil {
		return nil, nil //nolint:nilnil // no details is not an error, and there are none
	}

	labels, err := decodeColumn[storedLabel](r.Labels)
	if err != nil {
		return nil, fmt.Errorf("the labels of item %d: %w", r.ID, err)
	}

	users, err := decodeColumn[string](r.ReviewUsers)
	if err != nil {
		return nil, fmt.Errorf("the review requests of item %d: %w", r.ID, err)
	}

	names := make([]string, 0, len(labels))
	for _, l := range labels {
		names = append(names, l.Name)
	}

	return &facts.DetailsRow{
		HeadOID: store.Deref(r.HeadOID), BaseRef: store.Deref(r.BaseRef), Mergeable: store.Deref(r.Mergeable),
		MergeState: store.Deref(r.MergeState), ReviewDecision: store.Deref(r.ReviewDecision),
		ChecksState: store.Deref(r.ChecksState), ChecksPassed: int(store.Deref(r.ChecksPassed)),
		ChecksFailed: int(store.Deref(r.ChecksFailed)), ChecksPending: int(store.Deref(r.ChecksPending)),
		ChecksAwaiting: int(store.Deref(r.ChecksAwaiting)), Additions: int(store.Deref(r.Additions)),
		Deletions: int(store.Deref(r.Deletions)), ChangedFiles: int(store.Deref(r.ChangedFiles)),
		Commits: int(store.Deref(r.Commits)), Comments: int(store.Deref(r.Comments)), Labels: names,
		Approvals: int(store.Deref(r.Approvals)), ChangesRequested: int(store.Deref(r.ChangesRequested)),
		ReviewUsers: users, AutoMerge: store.Deref(r.AutoMergeMethod) != "",
	}, nil
}

// factsFiles converts a row's file check, or returns nil when none was stored.
func factsFiles(r queries.ListFactsRowsRow) (*facts.FilesRow, error) {
	if r.FilesItemID == nil {
		return nil, nil //nolint:nilnil // no check is not an error, and there is none
	}

	files, err := store.DecodeJSON[[]storedFile](store.Deref(r.Files))
	if err != nil {
		return nil, err
	}

	out := make([]sensitive.File, 0, len(files))
	for _, f := range files {
		out = append(out, sensitive.File{Path: f.Path, PreviousPath: f.Previous, Change: f.Change})
	}

	return &facts.FilesRow{
		Head: store.Deref(r.FilesHeadOID), Files: out, Complete: store.Deref(r.FilesComplete), Error: store.Deref(r.FilesError),
	}, nil
}

// storedCheck is one check run or commit status as the checks column of item_checks
// stores it.
type storedCheck struct {
	Kind        string     `json:"kind"`
	Name        string     `json:"name"`
	Workflow    string     `json:"workflow"`
	App         string     `json:"app"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	Outcome     string     `json:"outcome"`
	URL         string     `json:"url"`
	Required    *bool      `json:"required"`
	StartedAt   *time.Time `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt"`
}

// factsChecks converts a stored read of a head commit's checks, or returns nil when none
// was stored (itemID is NULL).
func factsChecks(itemID *int64, head, raw *string, complete *bool) (*facts.ChecksRow, error) {
	if itemID == nil {
		return nil, nil //nolint:nilnil // no read is not an error, and there is none
	}

	stored, err := store.DecodeJSON[[]storedCheck](store.Deref(raw))
	if err != nil {
		return nil, err
	}

	checks := make([]facts.CheckRow, 0, len(stored))

	for _, c := range stored {
		row := facts.CheckRow{
			Name: c.Name, Workflow: c.Workflow, Outcome: facts.CheckOutcome(c.Outcome), Required: c.Required,
		}

		if c.CompletedAt != nil {
			row.CompletedAt = c.CompletedAt.UTC()
		}

		checks = append(checks, row)
	}

	return &facts.ChecksRow{Head: store.Deref(head), Checks: checks, Complete: store.Deref(complete)}, nil
}

// storedChecksOf converts a read of a pull request's checks for storing. The list is never
// nil, so the column always holds an array.
func storedChecksOf(h github.HeadChecks) []storedCheck {
	out := make([]storedCheck, 0, len(h.Checks))

	for _, c := range h.Checks {
		out = append(out, storedCheck{
			Kind: c.Kind, Name: c.Name, Workflow: c.Workflow, App: c.App, Status: c.Status, Conclusion: c.Conclusion,
			Outcome: c.Outcome, URL: c.URL, Required: c.Required, StartedAt: timeOrNil(c.StartedAt),
			CompletedAt: timeOrNil(c.CompletedAt),
		})
	}

	return out
}

// Lists names the inbox lists a routine watches.
type Lists struct {
	ReviewRequested bool
	Authored        bool
	Assigned        bool
}

// WatchedItems returns the stored items of the signed-in account a routine watches,
// newest activity first: those in the lists it names, and those its searches, by key,
// return, a stale row included, since a search that failed still watches its items.
func (s *Store) WatchedItems(ctx context.Context, keys []string, lists Lists) ([]int64, error) {
	login, err := s.ViewerLogin(ctx)
	if err != nil || login == "" {
		return []int64{}, err
	}

	ids, err := s.q.ListWatchedItems(ctx, queries.ListWatchedItemsParams{
		Login: login, ReviewRequested: lists.ReviewRequested, Authored: lists.Authored, Assigned: lists.Assigned,
		Keys: append([]string{""}, keys...),
	})
	if err != nil {
		return nil, fmt.Errorf("read the watched items: %w", err)
	}

	if ids == nil {
		return []int64{}, nil
	}

	return ids, nil
}

// Suggestions is what the stored items hold that a routine's builder suggests: the people
// and bots who opened them, the owners of their repositories, their labels, and the names
// of their checks, each most common first.
type Suggestions struct {
	// Authors are the people who opened stored items, and Bots the bots, by login.
	Authors []string
	Bots    []string

	// Owners are the owners of the stored repositories: users and orgs alike.
	Owners []string

	// Labels are the stored items' label names.
	Labels []string

	// Checks are the stored pull requests' check names, as GitHub shows them: "workflow /
	// job", or a status's context, or an app's run's name.
	Checks []string
}

// maxSuggestions caps each list of Suggestions.
const maxSuggestions = 100

// Suggest returns what the stored items hold that a routine's builder suggests. It reads
// the database alone, and every list is never nil.
func (s *Store) Suggest(ctx context.Context) (Suggestions, error) {
	out := Suggestions{Authors: []string{}, Bots: []string{}, Owners: []string{}, Labels: []string{}, Checks: []string{}}

	var err error

	if out.Authors, out.Bots, err = s.suggestAuthors(ctx); err != nil {
		return out, err
	}

	owners, err := s.q.ListOwners(ctx)
	if err != nil {
		return out, fmt.Errorf("read the owners: %w", err)
	}

	for _, o := range owners[:min(len(owners), maxSuggestions)] {
		out.Owners = append(out.Owners, o.Owner)
	}

	if out.Labels, err = s.suggestLabels(ctx); err != nil {
		return out, err
	}

	if out.Checks, err = s.suggestChecks(ctx); err != nil {
		return out, err
	}

	return out, nil
}

// suggestAuthors returns the people and the bots who opened stored items, the most items
// first, each list never nil.
func (s *Store) suggestAuthors(ctx context.Context) (people, bots []string, err error) {
	people, bots = []string{}, []string{}

	authors, err := s.q.ListAuthors(ctx)
	if err != nil {
		return people, bots, fmt.Errorf("read the authors: %w", err)
	}

	for _, a := range authors {
		if (trust.Author{Login: a.AuthorLogin, Type: a.AuthorType}).IsBot() {
			bots = append(bots, a.AuthorLogin)
		} else {
			people = append(people, a.AuthorLogin)
		}
	}

	return people[:min(len(people), maxSuggestions)], bots[:min(len(bots), maxSuggestions)], nil
}

// suggestLabels counts the stored labels by name, ignoring case, the most common first.
func (s *Store) suggestLabels(ctx context.Context) ([]string, error) {
	columns, err := s.q.ListLabelColumns(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the labels: %w", err)
	}

	counts := newTally()

	for _, raw := range columns {
		labels, decodeErr := store.DecodeJSON[[]storedLabel](raw)
		if decodeErr != nil {
			continue
		}

		for _, l := range labels {
			counts.add(l.Name)
		}
	}

	return counts.top(maxSuggestions), nil
}

// suggestChecks counts the stored check names, ignoring case, the most common first.
func (s *Store) suggestChecks(ctx context.Context) ([]string, error) {
	columns, err := s.q.ListCheckColumns(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the checks: %w", err)
	}

	counts := newTally()

	for _, raw := range columns {
		checks, decodeErr := store.DecodeJSON[[]storedCheck](raw)
		if decodeErr != nil {
			continue
		}

		for _, c := range checks {
			counts.add(checkName(c.Workflow, c.Name))
		}
	}

	return counts.top(maxSuggestions), nil
}

// checkName is a check's name as GitHub shows it: "workflow / job", or the run's or the
// status's name alone when it has no workflow.
func checkName(workflow, name string) string {
	if workflow == "" {
		return name
	}

	return workflow + " / " + name
}

// tally counts names ignoring case, keeping the first spelling of each.
type tally struct {
	counts map[string]int
	names  map[string]string
}

// newTally returns an empty tally.
func newTally() *tally {
	return &tally{counts: map[string]int{}, names: map[string]string{}}
}

// add counts one name; "" is not counted.
func (t *tally) add(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}

	key := strings.ToLower(name)
	if _, ok := t.names[key]; !ok {
		t.names[key] = name
	}

	t.counts[key]++
}

// top returns the n most counted names, ties by name.
func (t *tally) top(n int) []string {
	keys := make([]string, 0, len(t.counts))
	for k := range t.counts {
		keys = append(keys, k)
	}

	sort.Slice(keys, func(i, j int) bool {
		if t.counts[keys[i]] != t.counts[keys[j]] {
			return t.counts[keys[i]] > t.counts[keys[j]]
		}

		return keys[i] < keys[j]
	})

	out := make([]string, 0, min(n, len(keys)))
	for _, k := range keys[:min(n, len(keys))] {
		out = append(out, t.names[k])
	}

	return out
}

// KnownOwners returns every owner of a stored repository, in lower case: those GitHub
// can answer a search about, since the inbox has seen them. GitHub answers a search for an
// owner it doesn't know with nothing, and no error.
func (s *Store) KnownOwners(ctx context.Context) (map[string]bool, error) {
	owners, err := s.q.ListOwners(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the owners: %w", err)
	}

	out := make(map[string]bool, len(owners))
	for _, o := range owners {
		out[strings.ToLower(o.Owner)] = true
	}

	return out, nil
}

// NoteStates records the state another service read of each item in states, by its ref's
// key, as a read of the inbox's own would: every stored item of that repository and number
// takes it, and is stamped as read at readAt, when that read went out. It runs outside the
// inbox's order, so a merged item keeps its state, and an item a read of the inbox's
// stored at or after readAt keeps what that newer read stored (see SetRefState). It never
// touches the lists.
func (s *Store) NoteStates(ctx context.Context, readAt time.Time, refs []item.Ref, states map[string]github.ItemState,
) error {
	if len(states) == 0 {
		return nil
	}

	readAt = readAt.UTC()

	return store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		for _, ref := range refs {
			st, ok := states[ref.Key()]
			if !ok || st.State == "" {
				continue
			}

			if err := setRefState(ctx, q, ref, st.State, st.ClosedAt, readAt, &readAt); err != nil {
				return err
			}
		}

		return nil
	})
}
