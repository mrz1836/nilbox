package inbox

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/store"
)

// maxItemsByID caps the items one ItemsByID call reads.
const maxItemsByID = 500

// ItemsByID returns the stored items of the signed-in account among ids, listed or not,
// each as a snapshot's list shows it, newest activity first: the rows of a routine's
// Watching list, which Autopilot's Watching names. An id with no stored item, or one of
// another account, is left out; at most maxItemsByID are read. It reads the database
// alone, and the list is never nil.
func (s *Service) ItemsByID(ctx context.Context, ids []int64) ([]Item, error) {
	out := []Item{}

	if len(ids) == 0 {
		return out, nil
	}

	status, err := readStatus(ctx, s.prefs)
	if err != nil {
		return out, err
	}

	accountID, err := lookupAccount(ctx, s.q, status[prefViewer])
	if err != nil || accountID == 0 {
		return out, err
	}

	stored, err := s.q.ListItemsByID(ctx, queries.ListItemsByIDParams{AccountID: accountID, Ids: ids[:min(len(ids), maxItemsByID)]})
	if err != nil {
		return out, fmt.Errorf("read the items: %w", err)
	}

	rows, err := store.ConvertRows(stored, func(r queries.ListItemsByIDRow) (itemRow, error) {
		return itemRowOf(queries.ListItemsRow(r))
	})
	if err != nil {
		return out, fmt.Errorf("read the items: %w", err)
	}

	scope, rules := s.scopes.scope(status), reporules.Decode(status)

	for _, r := range rows {
		it := r.item(scope, status[prefViewer])
		it.RepoRule, _ = rules.For(repo.Name{Owner: r.RepoOwner, Repo: r.RepoName})
		out = append(out, it)
	}

	return out, nil
}

// ReadChecks reads the checks of the stored pull requests among ids on their head commits,
// with whether each is required, and stores them in item_checks, replacing what was
// stored: Autopilot asks for the pull requests whose conditions need check names. An id
// that isn't a stored pull request is left out. It costs one GraphQL point per batch of 25,
// counted for Autopilot, and tells the Changed seam the items it stored.
func (s *Service) ReadChecks(ctx context.Context, ids []int64) error {
	nodes, err := s.pullNodes(ctx, ids)
	if err != nil || len(nodes) == 0 {
		return err
	}

	ctx = github.WithPurpose(ctx, github.PurposeAutopilot)

	nodeIDs := make([]string, 0, len(nodes))
	for node := range nodes {
		nodeIDs = append(nodeIDs, node)
	}

	read, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (*github.ChecksRead, error) {
		return s.client.FetchChecks(ctx, tok, nodeIDs)
	})
	if err != nil {
		return fmt.Errorf("read the checks: %w", err)
	}

	stored, err := s.storeChecks(ctx, nodes, read)
	if err != nil {
		return err
	}

	s.tellChanged(stored)

	return nil
}

// pullNodes returns the node ID of each stored pull request among ids, with its id.
func (s *Service) pullNodes(ctx context.Context, ids []int64) (map[string]int64, error) {
	if len(ids) == 0 {
		return map[string]int64{}, nil
	}

	rows, err := s.q.ListNodeIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read the pull requests: %w", err)
	}

	out := make(map[string]int64, len(rows))

	for _, r := range rows {
		if r.NodeID != "" && item.Kind(r.Kind) == item.KindPR {
			out[r.NodeID] = r.ID
		}
	}

	return out, nil
}

// storeChecks stores what read brought of each pull request in nodes, under the store
// lock, and returns the ones it stored.
func (s *Service) storeChecks(ctx context.Context, nodes map[string]int64, read *github.ChecksRead) ([]int64, error) {
	s.order.mu.Lock()
	defer s.order.mu.Unlock()

	var stored []int64

	now := s.now()

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		for node, h := range read.Items {
			id, ok := nodes[node]
			if !ok {
				continue
			}

			if err := upsertChecks(ctx, q, id, h, now); err != nil {
				return err
			}

			stored = append(stored, id)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("save the checks: %w", err)
	}

	return stored, nil
}

// upsertChecks stores one pull request's checks on a head commit.
func upsertChecks(ctx context.Context, q *queries.Queries, id int64, h github.HeadChecks, now time.Time) error {
	encoded, err := store.EncodeJSON(storedChecksOf(h))
	if err != nil {
		return err
	}

	var settled *time.Time
	if at, ok := h.Settled(); ok {
		settled = new(at.UTC())
	}

	err = q.UpsertChecks(ctx, queries.UpsertChecksParams{
		ItemID: id, HeadOID: h.Head, Checks: encoded, Complete: h.Complete, SettledAt: settled, FetchedAt: now,
	})
	if err != nil {
		return fmt.Errorf("save the checks of item %d: %w", id, err)
	}

	return nil
}

// SearchOutcome is what one search read now found, for a routine's test.
type SearchOutcome struct {
	// Query is the search, as GitHub was sent it.
	Query string

	// IDs are the stored items it returned that are open, in its order.
	IDs []int64

	// Count is how many items GitHub says it matches, and Truncated reports that it
	// returned only its first 100.
	Count     int
	Truncated bool

	// Err is why GitHub didn't answer it.
	Err error
}

// SearchNow reads searches from GitHub now, for a routine's test, and stores what they
// return as the watch pass does, without any search's rows, so nothing watches the items
// on the test's account: the prune of old items takes them in time unless a routine comes
// to watch them. The details of the items that are new or changed are read too. It
// returns each search's outcome, by query, and what it cost in GraphQL points, counted for
// Autopilot.
func (s *Service) SearchNow(ctx context.Context, texts []string) (map[string]SearchOutcome, int, error) {
	ctx = github.WithPurpose(ctx, github.PurposeAutopilot)

	login, _, err := s.prefs.Get(ctx, prefViewer)
	if err != nil {
		return nil, 0, fmt.Errorf("read the signed-in account: %w", err)
	}

	job := &watchJob{login: login, stored: make(chan struct{})}
	for _, q := range texts {
		job.searches = append(job.searches, Watch{Key: q, Query: q})
	}

	started, seq := s.now(), s.startFetch()

	got, err := s.fetchWatches(ctx, job)
	if err != nil {
		return nil, 0, err
	}

	plan, err := s.planPass(ctx, job, got, started)
	if err != nil {
		return nil, 0, err
	}

	if err = s.readPassDetails(ctx, &plan); err != nil {
		return nil, 0, err
	}

	ids, err := s.storeTested(ctx, seq, plan)
	if err != nil {
		return nil, 0, err
	}

	out := make(map[string]SearchOutcome, len(texts))
	for _, q := range texts {
		out[q] = outcomeOf(q, got.Results[q], ids)
	}

	return out, got.RateLimit.Cost + plan.detailPoints, nil
}

// outcomeOf is the search q's outcome, from its answer and the ids its results were
// stored under, by node ID.
func outcomeOf(q string, answer github.WatchResult, ids map[string]int64) SearchOutcome {
	o := SearchOutcome{Query: q, IDs: []int64{}, Count: answer.IssueCount, Truncated: answer.Truncated(), Err: answer.Err}

	for _, it := range answer.Items {
		if id, ok := ids[it.NodeID]; ok {
			o.IDs = append(o.IDs, id)
		}
	}

	return o
}

// storeTested stores a test's open results and their details, as a pass does, and none of
// its searches' rows, and returns each result's id by node ID.
func (s *Service) storeTested(ctx context.Context, seq uint64, plan passPlan) (map[string]int64, error) {
	if plan.accountID == 0 {
		return map[string]int64{}, nil
	}

	s.order.mu.Lock()
	defer s.order.mu.Unlock()

	ids := make(map[string]int64, len(plan.order))

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		w := s.newWriter(tx)
		w.accountID, w.details = plan.accountID, plan.details

		for _, node := range plan.order {
			id, err := w.storeWatchedItem(ctx, plan.results[node])
			if err != nil {
				return err
			}

			ids[node] = id
		}

		return w.wakeSnoozed(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("save the searched items: %w", err)
	}

	if r := s.order.refresh; r != nil && seq > r.seq {
		r.replays = append(r.replays, plan.replays(seq)...)
	}

	return ids, nil
}
