package inbox

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The routines' searches are read beside the inbox's own, in a request of their own, and
// what they watch is stored after the inbox, by the watch pass:
//
//   - A whole refresh hands the pass its searches as it starts, so their request goes out
//     alongside the inbox query, on the pass's own goroutine, outside the refresh's
//     errgroup: a search that fails, or a request that does, never fails the refresh or
//     cancels the inbox query, and the refresh stores and returns on the inbox's own
//     requests.
//   - Once the refresh has stored the inbox and told the window, the pass compares each
//     result with what is stored (a read, with no lock), reads the details of the watched
//     items that are new or changed (with no lock), and stores them, their searches' rows,
//     and the searches' state through the inbox's order (writeOrder.mu, held only for the
//     write), then says which items it stored (Config.Changed).
//   - One pass runs at a time: a refresh that finds one running sends no watch request of
//     its own.
//   - No pass is sent while the GraphQL budget is under a quarter of its size; the searches'
//     rows are kept, and the state says why.
const (
	// detailsStaleAfter is how old a watched item's details may be before a pass reads
	// them again whatever its search says, which catches what the search can't see, such
	// as its labels, or a base branch that moved on.
	detailsStaleAfter = 30 * time.Minute

	// staleTailFor is how long a truncated search's tail is kept, stale, after it last
	// returned an item: the item may only have dropped past the search's first page.
	staleTailFor = 7 * 24 * time.Hour

	// lowBudgetShare is the share of the GraphQL budget under which no pass is sent.
	lowBudgetShare = 4

	// passTimeout bounds one pass, its requests and its write.
	passTimeout = 2 * time.Minute

	// passJob names the passes in log entries.
	passJob = "read the routines' searches"

	// prefWatches keeps the state of each search the last passes read (see watchState).
	prefWatches = "inbox.watches"
)

// Why a refresh sent no watch request, as WatchStatus.Paused says.
const (
	// PausedLowBudget is a GraphQL budget under a quarter of its size.
	PausedLowBudget = "low_budget"
)

// Watch is one routine search the inbox reads beside its own lists: the key the routines
// that ask for it share, the query GitHub is sent, and whether the checks follower follows
// the checks of what it returns.
type Watch struct {
	Key    string
	Query  string
	Follow bool
}

// WatchSource gives the routines' searches for the account with login, deduplicated by
// key. It must answer at once, from what it keeps: a whole refresh asks as it starts.
type WatchSource interface {
	Watches(login string) []Watch
}

// ItemKeeper names the stored items something outside the inbox refers to, which the
// prune of old items keeps.
type ItemKeeper interface {
	KeptItems(ctx context.Context) ([]int64, error)
}

// WatchPass is what one watch pass did, for the log, the measurements, and the tests.
type WatchPass struct {
	// At is when its request went out.
	At time.Time

	// Took is how long it ran, from its request to its write; After is how long of that
	// came after the inbox was stored, which is what it adds to a refresh.
	Took  time.Duration
	After time.Duration

	// Searches is how many searches it sent, and Failed how many GitHub didn't answer.
	Searches int
	Failed   int

	// Requests and Points are what its searches and details cost.
	Requests int
	Points   int

	// Items is how many results its searches returned, Read how many of them it read the
	// details of, and Changed how many it stored as changed.
	Items   int
	Read    int
	Changed int

	// Paused says why it sent no request, "" when it did.
	Paused string
}

// watchJob is one refresh's watch request: its searches, the account, and when the inbox
// is stored.
type watchJob struct {
	login    string
	searches []Watch
	paused   string

	// stored closes once the refresh that sent it stored the inbox, or gave up.
	stored chan struct{}
	once   sync.Once
}

// release says the refresh is done with the inbox. It is safe on a nil job and called
// more than once.
func (j *watchJob) release() {
	if j == nil {
		return
	}

	j.once.Do(func() { close(j.stored) })
}

// watchPasses holds the pass worker's state.
type watchPasses struct {
	// jobs hands a refresh's job to the worker; it holds one.
	jobs chan *watchJob

	// busy is set from a job's hand-off until its pass ends.
	busy atomic.Bool

	// mu guards last and cleaned.
	mu   sync.Mutex
	last *WatchPass

	// cleaned is set once a pass with no searches has deleted every search's rows, so
	// refreshes with no routine's search send no job until one has a search again.
	cleaned bool
}

// startWatchPass hands the worker this refresh's watch request, so it goes out beside the
// inbox query, and returns the job the refresh releases once it stored the inbox; nil when
// there is nothing to send: no source, no account, a pass still running, or no search and
// no rows to drop.
func (s *Service) startWatchPass(ctx context.Context) *watchJob {
	if s.watchSource == nil {
		return nil
	}

	login, _, err := s.prefs.Get(ctx, prefViewer)
	if err != nil || login == "" {
		return nil
	}

	searches := s.watchSource.Watches(login)

	s.passes.mu.Lock()
	idle := len(searches) == 0 && s.passes.cleaned
	s.passes.mu.Unlock()

	if idle || !s.passes.busy.CompareAndSwap(false, true) {
		return nil
	}

	job := &watchJob{login: login, searches: searches, stored: make(chan struct{})}
	if len(searches) > 0 {
		job.paused = s.budgetPause(ctx, login)
	}

	select {
	case s.passes.jobs <- job:
		return job
	default:
		s.passes.busy.Store(false)

		return nil
	}
}

// budgetPause returns PausedLowBudget while the GraphQL budget the last read left is
// under a quarter of its size and won't refill before now, and "" otherwise.
func (s *Service) budgetPause(ctx context.Context, login string) string {
	accountID, err := lookupAccount(ctx, s.q, login)
	if err != nil || accountID == 0 {
		return ""
	}

	rate, err := loadRate(ctx, s.q, accountID, resourceGraphQL)
	if err != nil || !rate.Known || rate.Limit <= 0 {
		return ""
	}

	if reset, parseErr := time.Parse(time.RFC3339, rate.ResetAt); parseErr == nil && !reset.After(s.now()) {
		return ""
	}

	if rate.Remaining*lowBudgetShare < rate.Limit {
		return PausedLowBudget
	}

	return ""
}

// RunWatchPasses runs the watch passes the refreshes hand it, one at a time, until ctx
// ends, and returns once the pass in flight has. A pass that fails is logged and one that
// panics recovered, so the next still runs. Without it running, no refresh reads the
// routines' searches.
func (s *Service) RunWatchPasses(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-s.passes.jobs:
			s.runPassJob(ctx, job)
		}
	}
}

// runPassJob runs one job's pass, logging a failure or a panic, and frees the worker.
func (s *Service) runPassJob(ctx context.Context, job *watchJob) {
	defer s.passes.busy.Store(false)

	defer func() {
		if p := recover(); p != nil {
			s.logger.ErrorContext(ctx, passJob+" panicked", "panic", p)
		}
	}()

	passCtx, cancel := context.WithTimeout(github.WithPurpose(ctx, github.PurposeWatches), passTimeout)
	defer cancel()

	report, err := s.watchPass(passCtx, job)

	switch {
	case err != nil && ctx.Err() == nil:
		s.logger.WarnContext(ctx, passJob+" failed", "error", err)
	case err == nil:
		s.logger.DebugContext(ctx, passJob, "searches", report.Searches, "failed", report.Failed,
			"items", report.Items, "read", report.Read, "changed", report.Changed, "points", report.Points,
			"took", report.Took, "after_inbox", report.After, "paused", report.Paused)
	}

	s.passes.mu.Lock()
	s.passes.last = &report
	s.passes.cleaned = len(job.searches) == 0 && err == nil
	s.passes.mu.Unlock()
}

// LastWatchPass returns what the last watch pass did, and false before one ran.
func (s *Service) LastWatchPass() (WatchPass, bool) {
	s.passes.mu.Lock()
	defer s.passes.mu.Unlock()

	if s.passes.last == nil {
		return WatchPass{}, false
	}

	return *s.passes.last, true
}

// watchPass sends a job's searches, waits for the inbox to be stored, and stores what the
// searches watch (see the notes at the top of this file).
func (s *Service) watchPass(ctx context.Context, job *watchJob) (WatchPass, error) {
	started := s.now()
	report := WatchPass{At: started, Searches: len(job.searches), Paused: job.paused}
	seq := s.startFetch()

	var (
		got *github.Watches
		err error
	)

	if len(job.searches) > 0 && job.paused == "" {
		got, err = s.fetchWatches(ctx, job)
		if err != nil {
			return report, err
		}
	}

	waited := s.now()

	select {
	case <-job.stored:
	case <-ctx.Done():
		return report, ctx.Err()
	}

	released := s.now()

	plan, err := s.planPass(ctx, job, got, started)
	if err != nil {
		return report, err
	}

	if err = s.readPassDetails(ctx, &plan); err != nil {
		s.logger.WarnContext(ctx, passJob+": could not read the watched items' details", "error", err)
	}

	changed, err := s.storePass(ctx, seq, &plan)
	if err != nil {
		return report, err
	}

	s.tellChanged(changed)

	done := s.now()
	report.Took, report.After = done.Sub(started), done.Sub(maxTime(released, waited))
	report.Failed, report.Items, report.Read, report.Changed = plan.failed(), plan.count(), len(plan.details), len(changed)

	if got != nil {
		report.Requests, report.Points = got.Requests+plan.detailRequests, got.RateLimit.Cost+plan.detailPoints
	}

	return report, nil
}

// maxTime returns the later of a and b.
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}

	return b
}

// fetchWatches sends the job's searches with a token, each packed as it returned last
// time. A token GitHub refuses is borrowed again once.
func (s *Service) fetchWatches(ctx context.Context, job *watchJob) (*github.Watches, error) {
	state, err := readWatchState(ctx, s.prefs)
	if err != nil {
		return nil, err
	}

	searches := make([]github.WatchSearch, 0, len(job.searches))

	for _, w := range job.searches {
		last := github.NewSearch
		if st, ok := state.Searches[w.Key]; ok && st.Answered {
			last = min(st.Count, watchPage)
		}

		searches = append(searches, github.WatchSearch{Key: w.Key, Query: w.Query, Last: last})
	}

	return github.WithToken(ctx, s.tokens, func(tok github.Token) (*github.Watches, error) {
		return s.client.FetchWatches(ctx, tok, searches)
	})
}

// watchPage is the most results one search returns.
const watchPage = 100

// passPlan is what a pass works out before it stores anything: each search's answer, the
// open results by node ID with what is stored of them, and the details to read.
type passPlan struct {
	job     *watchJob
	answers map[string]github.WatchResult
	results map[string]*passItem
	order   []string
	closed  []github.WatchItem

	accountID int64
	options   github.DetailOptions

	// details holds the details read, by node ID, and detailRequests and detailPoints what
	// reading them cost.
	details        map[string]github.ItemDetails
	detailRequests int
	detailPoints   int

	// started is when the pass's request went out: an item a later read stored is left as
	// that read stored it.
	started time.Time
}

// passItem is one open result of a pass, and what is stored of it.
type passItem struct {
	item     github.WatchItem
	stored   *queries.ListWatchBaselineRow
	follow   bool
	read     bool
	newer    bool
	changed  bool
	listed   bool
	searches []string
}

// failed counts the searches GitHub didn't answer; none while the pass was paused.
func (p *passPlan) failed() int {
	if p.job.paused != "" {
		return 0
	}

	n := 0

	for _, w := range p.job.searches {
		if a, ok := p.answers[w.Key]; !ok || a.Err != nil {
			n++
		}
	}

	return n
}

// count counts the open results.
func (p *passPlan) count() int {
	return len(p.results)
}

// planPass gathers the answers' open results, compares them with what is stored, and
// picks the items whose details it reads: new to the watches, or changed since the last
// read (see needsDetails). It reads the database, and holds no lock.
func (s *Service) planPass(ctx context.Context, job *watchJob, got *github.Watches, started time.Time) (passPlan, error) {
	plan := passPlan{job: job, answers: map[string]github.WatchResult{}, results: map[string]*passItem{}, started: started}
	if got != nil {
		plan.answers = got.Results
	}

	accountID, err := lookupAccount(ctx, s.q, job.login)
	if err != nil {
		return plan, err
	}

	last, err := s.projectAccess(ctx)
	if err != nil {
		return plan, err
	}

	plan.accountID = accountID
	plan.options = github.DetailOptions{Projects: readOptions(last).Projects, Repository: true}

	for _, w := range job.searches {
		for _, it := range plan.answers[w.Key].Items {
			plan.add(w, it)
		}
	}

	if accountID == 0 || len(plan.order) == 0 {
		return plan, nil
	}

	return plan, s.compareStored(ctx, &plan)
}

// compareStored reads what is stored of plan's results, and judges each against it.
func (s *Service) compareStored(ctx context.Context, plan *passPlan) error {
	stored, err := s.q.ListWatchBaseline(ctx, queries.ListWatchBaselineParams{AccountID: plan.accountID, NodeIds: plan.order})
	if err != nil {
		return fmt.Errorf("read the watched items: %w", err)
	}

	for i := range stored {
		if p := plan.results[stored[i].NodeID]; p != nil {
			p.stored = &stored[i]
		}
	}

	now := s.now()
	for _, id := range plan.order {
		plan.results[id].judge(plan.started, now)
	}

	return nil
}

// add takes one result of the search w: a closed or merged one, which the search index can
// trail, is kept apart, and an archived repository's is left out, as the inbox leaves them
// out of its lists.
func (p *passPlan) add(w Watch, it github.WatchItem) {
	switch {
	case it.Repo.IsArchived:
		return
	case it.Closed():
		p.closed = append(p.closed, it)

		return
	}

	item := p.results[it.NodeID]
	if item == nil {
		item = &passItem{item: it}
		p.results[it.NodeID] = item
		p.order = append(p.order, it.NodeID)
	}

	item.follow = item.follow || w.Follow
	item.searches = append(item.searches, w.Key)
}

// judge works out what a pass does with the item: a listed one has its details from the
// refresh, and only its watch rows are stored; one a later read stored is left as it is;
// any other is stored, and its details read when needsDetails says so.
func (p *passItem) judge(started, now time.Time) {
	st := p.stored
	if st == nil {
		p.read, p.changed = true, true

		return
	}

	p.listed = st.ReasonReviewRequested || st.ReasonAuthored || st.ReasonAssigned
	p.newer = st.FetchedAt.After(started) || (st.DetailsFetchedAt != nil && st.DetailsFetchedAt.After(started))

	if p.listed || p.newer {
		return
	}

	p.read = needsDetails(p.item, st, now)
	p.changed = p.read || moved(p.item, st)
}

// moved reports whether the item's card moved since it was stored: new to the watches, or
// another update time, state, or draft flag.
func moved(it github.WatchItem, st *queries.ListWatchBaselineRow) bool {
	return !st.Watched || !st.ItemUpdatedAt.Equal(it.UpdatedAt.UTC()) || st.State != it.State || st.IsDraft != it.IsDraft
}

// needsDetails reports whether a watched item's details are read again: it is new to the
// watches or has none; its update time, head commit, base branch, checks' state, or draft
// flag moved; its checks were still running at the last read, or GitHub hadn't worked out
// whether it merges; or its details are older than detailsStaleAfter.
func needsDetails(it github.WatchItem, st *queries.ListWatchBaselineRow, now time.Time) bool {
	switch {
	case st.DetailsItemID == nil || st.DetailsFetchedAt == nil || moved(it, st):
		return true
	case now.Sub(*st.DetailsFetchedAt) >= detailsStaleAfter:
		return true
	case !it.IsPR:
		return false
	default:
		return pullMoved(it, st)
	}
}

// pullMoved reports whether a watched pull request's search says more than its stored
// details: another head commit, base branch, or checks' state, checks still running at
// the last read, or a merge state GitHub hadn't worked out.
func pullMoved(it github.WatchItem, st *queries.ListWatchBaselineRow) bool {
	head, checks := store.Deref(st.HeadOID), store.Deref(st.ChecksState)

	switch {
	case !strings.EqualFold(head, it.HeadOID), store.Deref(st.BaseRef) != it.BaseRef, checks != it.ChecksState:
		return true
	case checks == "PENDING" || checks == "EXPECTED":
		return true
	default:
		return store.Deref(st.Mergeable) == "UNKNOWN" || store.Deref(st.MergeState) == github.MergeStateUnknown
	}
}

// readPassDetails reads the details of the items plan picked, with their repositories, in
// the details' own batches, with no lock held.
func (s *Service) readPassDetails(ctx context.Context, plan *passPlan) error {
	var ids []string

	for _, id := range plan.order {
		if plan.results[id].read {
			ids = append(ids, id)
		}
	}

	if len(ids) == 0 {
		return nil
	}

	details, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (*github.Details, error) {
		return s.client.FetchDetails(ctx, tok, ids, plan.options)
	})
	if err != nil {
		return fmt.Errorf("read the details: %w", err)
	}

	plan.details = details.Items
	plan.detailRequests, plan.detailPoints = details.Requests, details.RateLimit.Cost

	return nil
}

// storePass stores what plan read, in one transaction under the store lock: each watched
// item and its details, the closed results' state, each search's rows and state, and no
// row of a search no routine has. It returns the items it stored as changed.
func (s *Service) storePass(ctx context.Context, seq uint64, plan *passPlan) ([]int64, error) {
	s.order.mu.Lock()
	defer s.order.mu.Unlock()

	var changed []int64

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		w := s.newWriter(tx)
		w.accountID, w.details = plan.accountID, plan.details

		var err error

		if plan.accountID != 0 {
			if changed, err = w.storeWatched(ctx, plan); err != nil {
				return err
			}
		}

		return w.storeSearches(ctx, plan)
	})
	if err != nil {
		return nil, fmt.Errorf("save the watched items: %w", err)
	}

	// A refresh fetching now that started before this pass's request takes what it stored
	// again, as it takes an item's own read (see order.go).
	if r := s.order.refresh; r != nil && seq > r.seq {
		r.replays = append(r.replays, plan.replays(seq)...)
	}

	return changed, nil
}

// replays returns the item writes a refresh still fetching applies again: one for each item
// whose details the pass stored.
func (p *passPlan) replays(seq uint64) []itemWrite {
	var out []itemWrite

	for _, id := range p.order {
		item := p.results[id]

		d, ok := p.details[id]
		if !ok || item.stored == nil || item.listed || item.newer {
			continue
		}

		out = append(out, itemWrite{id: item.stored.ID, seq: seq, update: &itemUpdate{
			accountID: p.accountID, update: &github.ItemUpdate{Found: true, Item: item.item.Item, Details: d},
			files: map[string]fileCheck{},
		}})
	}

	return out
}

// storeWatched stores the pass's watched items, their details and rows, and the closed
// results' state, and returns the items it stored as changed.
func (w *writer) storeWatched(ctx context.Context, plan *passPlan) ([]int64, error) {
	var changed []int64

	for _, nodeID := range plan.order {
		item := plan.results[nodeID]

		id, err := w.storeWatchedItem(ctx, item)
		if err != nil {
			return nil, err
		}

		if item.changed {
			changed = append(changed, id)
		}

		if err = w.storeWatchRows(ctx, id, item); err != nil {
			return nil, err
		}
	}

	closed := make([]github.Item, 0, len(plan.closed))
	for _, it := range plan.closed {
		closed = append(closed, it.Item)
	}

	if err := w.saveClosedStates(ctx, closed); err != nil {
		return nil, err
	}

	return changed, w.wakeSnoozed(ctx)
}

// storeWatchRows records that each of the item's searches returned it now.
func (w *writer) storeWatchRows(ctx context.Context, id int64, item *passItem) error {
	for _, key := range item.searches {
		err := w.q.UpsertWatch(ctx, queries.UpsertWatchParams{WatchKey: key, ItemID: id, Follow: item.follow, SeenAt: w.now})
		if err != nil {
			return fmt.Errorf("save the watch of %s: %w", item.item.Ref(), err)
		}
	}

	return nil
}

// storeWatchedItem stores one watched item: a listed one, or one a later read stored, is
// left as it is; any other is stored with its repository, and its details when the pass
// read them. It returns the item's id.
func (w *writer) storeWatchedItem(ctx context.Context, item *passItem) (int64, error) {
	if item.stored != nil && (item.listed || item.newer) {
		return item.stored.ID, nil
	}

	it := item.item

	repoID, err := w.watchedRepoID(ctx, it)
	if err != nil {
		return 0, err
	}

	id, err := w.q.UpsertWatchedItem(ctx, queries.UpsertWatchedItemParams{
		AccountID: w.accountID, RepoID: repoID, Kind: string(it.Ref().Kind), Number: int64(it.Number),
		NodeID: it.NodeID, Title: it.Title, URL: it.URL, AuthorLogin: it.AuthorLogin, AuthorType: it.AuthorType,
		AuthorAssociation: it.AuthorAssociation, IsCrossRepository: it.IsCrossRepository, IsDraft: it.IsDraft,
		HeadRef: it.HeadRef, ItemCreatedAt: it.CreatedAt.UTC(), ItemUpdatedAt: it.UpdatedAt.UTC(), FetchedAt: w.now,
		State: it.State, ClosedAt: timeOrNil(it.ClosedAt),
	})
	if err != nil {
		return 0, fmt.Errorf("save the watched item %s: %w", it.Ref(), err)
	}

	return id, w.saveDetails(ctx, id, it.Item)
}

// watchedRepoID records a watched item's repository and returns its id: whole from the
// details the pass read, which carry it, or else from the little the search read, which
// keeps what is stored of it.
func (w *writer) watchedRepoID(ctx context.Context, it github.WatchItem) (int64, error) {
	key := it.Ref().Repo.Key()
	if id, ok := w.repos[key]; ok {
		return id, nil
	}

	if d, ok := w.details[it.NodeID]; ok && d.Repo != nil && strings.EqualFold(d.Repo.Owner, it.RepoOwner) &&
		strings.EqualFold(d.Repo.Name, it.RepoName) {
		whole := it.Item
		whole.Repo = d.Repo.Repo

		return w.repoID(ctx, whole)
	}

	id, err := w.q.EnsureRepo(ctx, queries.EnsureRepoParams{
		Owner: it.RepoOwner, Name: it.RepoName, NodeID: it.Repo.NodeID, IsArchived: it.Repo.IsArchived,
		CreatedAt: w.now, UpdatedAt: w.now,
	})
	if err != nil {
		return 0, fmt.Errorf("save the repository %s: %w", it.Ref().Repo, err)
	}

	w.repos[key] = id

	return id, nil
}

// storeSearches stores each search's rows and state: a search answered in full replaces
// its rows; a truncated one keeps the rows past its page, stale, for staleTailFor; one
// that failed keeps every row, stale. The rows of a search no routine has are deleted.
func (w *writer) storeSearches(ctx context.Context, plan *passPlan) error {
	state, err := readWatchState(ctx, w.prefs)
	if err != nil {
		return err
	}

	keys := []string{""}
	next := watchState{Searches: make(map[string]searchState, len(plan.job.searches)), Paused: plan.job.paused}

	for _, search := range plan.job.searches {
		keys = append(keys, search.Key)

		st, storeErr := w.storeSearch(ctx, plan, search.Key, state.Searches[search.Key])
		if storeErr != nil {
			return storeErr
		}

		next.Searches[search.Key] = st
	}

	if err = w.q.DeleteWatchesNotIn(ctx, keys); err != nil {
		return fmt.Errorf("forget the searches no routine has: %w", err)
	}

	next.At = w.now

	return writeWatchState(ctx, w.prefs, next)
}

// storeSearch stores one search's rows, and returns its state.
func (w *writer) storeSearch(ctx context.Context, plan *passPlan, key string, st searchState) (searchState, error) {
	answer, ok := plan.answers[key]

	switch {
	case plan.job.paused != "" || !ok:
		return st, nil
	case answer.Err != nil:
		if err := w.q.MarkSearchStale(ctx, key); err != nil {
			return st, fmt.Errorf("keep the rows of a search that failed: %w", err)
		}

		st.Error, st.FailedAt = uierr.Message(answer.Err), new(w.now)

		return st, nil
	case answer.Truncated():
		if err := w.q.DeleteStaleWatches(ctx, queries.DeleteStaleWatchesParams{
			WatchKey: key, SeenAt: w.now.Add(-staleTailFor),
		}); err != nil {
			return st, fmt.Errorf("forget a search's old tail: %w", err)
		}

		if err := w.q.MarkWatchesStale(ctx, queries.MarkWatchesStaleParams{WatchKey: key, SeenAt: w.now}); err != nil {
			return st, fmt.Errorf("keep a search's tail: %w", err)
		}
	default:
		if err := w.q.DeleteUnseenWatches(ctx, queries.DeleteUnseenWatchesParams{WatchKey: key, SeenAt: w.now}); err != nil {
			return st, fmt.Errorf("forget the items that left a search: %w", err)
		}
	}

	return searchState{
		Answered: true, Count: answer.IssueCount, Returned: len(answer.Items), Truncated: answer.Truncated(),
		At: new(w.now),
	}, nil
}

// watchState is what the inbox keeps of the routines' searches between passes, under
// prefWatches: each search's last answer, by key, and why the last refresh sent none.
type watchState struct {
	Searches map[string]searchState `json:"searches"`
	Paused   string                 `json:"paused,omitempty"`
	At       time.Time              `json:"at"`
}

// searchState is one search's last answer: how many it matched and returned, whether
// that was all of them, when it was answered, and, once one failed, why and when.
type searchState struct {
	Answered  bool       `json:"answered"`
	Count     int        `json:"count"`
	Returned  int        `json:"returned"`
	Truncated bool       `json:"truncated,omitempty"`
	At        *time.Time `json:"at,omitempty"`
	Error     string     `json:"error,omitempty"`
	FailedAt  *time.Time `json:"failedAt,omitempty"`
}

// readWatchState reads the kept state; one never written, or that can't be read, is empty.
func readWatchState(ctx context.Context, p interface {
	Get(ctx context.Context, key string) (string, bool, error)
},
) (watchState, error) {
	raw, ok, err := p.Get(ctx, prefWatches)
	if err != nil {
		return watchState{}, fmt.Errorf("read the searches' state: %w", err)
	}

	empty := watchState{Searches: map[string]searchState{}}
	if !ok || raw == "" {
		return empty, nil
	}

	st, err := store.DecodeJSON[watchState](raw)
	if err != nil || st.Searches == nil {
		return empty, nil //nolint:nilerr // a state that can't be read is started again
	}

	return st, nil
}

// writeWatchState keeps st.
func writeWatchState(ctx context.Context, p interface {
	Set(ctx context.Context, key, value string) error
}, st watchState,
) error {
	raw, err := store.EncodeJSON(st)
	if err != nil {
		return err
	}

	if err = p.Set(ctx, prefWatches, raw); err != nil {
		return fmt.Errorf("save the searches' state: %w", err)
	}

	return nil
}

// tellChanged says which items were stored outside a whole refresh, outside every lock.
func (s *Service) tellChanged(ids []int64) {
	if s.changedSink == nil || len(ids) == 0 {
		return
	}

	out := slices.Clone(ids)
	slices.Sort(out)
	s.changedSink(slices.Compact(out))
}

// WatchStatus is what the inbox last read of each routine search, for the routines that
// use it.
type WatchStatus struct {
	// Searches holds each search's last answer, by key.
	Searches map[string]SearchStatus `json:"searches"`

	// Paused says why the last refresh sent no watch request (PausedLowBudget), "" when it
	// sent one.
	Paused string `json:"paused"`

	// At is when the last pass stored, as RFC 3339, "" before one did.
	At string `json:"at"`
}

// SearchStatus is one routine search's last answer.
type SearchStatus struct {
	// Answered reports that GitHub has answered the search.
	Answered bool `json:"answered"`

	// Count is how many items it matched, and Returned how many it returned: the 100 most
	// recently updated at most (Truncated).
	Count     int  `json:"count"`
	Returned  int  `json:"returned"`
	Truncated bool `json:"truncated"`

	// Error is why the last pass couldn't read it, in the window's words, "" when it
	// could; its rows are kept, stale, until it can.
	Error string `json:"error"`

	// At is when it was last answered, and FailedAt when it last failed, as RFC 3339.
	At       string `json:"at"`
	FailedAt string `json:"failedAt"`
}

// WatchStatus returns what the inbox last read of each routine search.
func (s *Store) WatchStatus(ctx context.Context) (WatchStatus, error) {
	st, err := readWatchState(ctx, s.prefs)
	if err != nil {
		return WatchStatus{}, err
	}

	out := WatchStatus{Searches: make(map[string]SearchStatus, len(st.Searches)), Paused: st.Paused}
	if !st.At.IsZero() {
		out.At = clock.Format(st.At)
	}

	for key, ss := range st.Searches {
		out.Searches[key] = SearchStatus{
			Answered: ss.Answered, Count: ss.Count, Returned: ss.Returned, Truncated: ss.Truncated, Error: ss.Error,
			At: formatOptionalTime(ss.At), FailedAt: formatOptionalTime(ss.FailedAt),
		}
	}

	return out, nil
}

// WatchPasses runs a Service's watch passes as a desktop worker (Service.RunWatchPasses).
type WatchPasses struct {
	svc    *Service
	logger *slog.Logger
}

// NewWatchPasses returns the worker of s's watch passes.
func NewWatchPasses(s *Service, logger *slog.Logger) *WatchPasses {
	return &WatchPasses{svc: s, logger: logging.Or(logger)}
}

// Run runs the passes until ctx ends.
func (p *WatchPasses) Run(ctx context.Context) {
	p.svc.RunWatchPasses(ctx)
}
