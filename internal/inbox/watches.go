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

// The routines' searches are read after the inbox's own, in requests of their own, and
// what they watch is stored after the inbox, by the watch pass:
//
//   - A whole refresh hands the pass its searches as it starts, and the pass waits, on its
//     own goroutine and outside the refresh's errgroup, until the refresh has stored the
//     inbox and told the window. Only then do its requests go out, so they never slow the
//     inbox query down, and a search that fails, or a request that does, never fails the
//     refresh: the refresh stores and returns on the inbox's own requests.
//   - The pass compares each result with what is stored (a read, with no lock), reads the
//     details of the watched items that are new or changed (with no lock), and stores them,
//     their searches' rows, and the searches' state through the inbox's order
//     (writeOrder.mu, held only for the write), then says which items it stored
//     (Config.Changed).
//   - Under the lock, it reads again what is stored of its results first, and leaves alone
//     what was stored since its request went out or since it compared (see recheck): a
//     read that found an item merged or closed while the pass read GitHub stands, and its
//     rows are never brought back.
//   - One pass runs at a time: a refresh that finds one running sends no watch request of
//     its own.
//   - No pass is sent while the GraphQL budget, as the inbox's own query just left it, is
//     under a quarter of its size; the searches' rows are kept, and the state says why.
const (
	// detailsStaleAfter is how old a watched item's details may be before a pass reads
	// them again whatever its search says, which catches what the search can't see, such
	// as its labels, or a base branch that moved on. Such reads fill the room left in the
	// pass's last details batch, so they never add more than one request (fillStale).
	detailsStaleAfter = 30 * time.Minute

	// watchDetailsBatch is how many watched items one details request reads. GitHub reads
	// an item it hasn't lately more slowly, and four requests of 25 such pull requests side
	// by side ran past its 10-second limit (measured in November 2026), while four of 15
	// took 6 seconds.
	watchDetailsBatch = 15

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

// Keepers is an ItemKeeper over several: the items any of them names are kept.
type Keepers []ItemKeeper

// KeptItems returns the items every keeper names, each once. One that fails fails the
// whole, so the prune keeps everything rather than an item one of them refers to.
func (k Keepers) KeptItems(ctx context.Context) ([]int64, error) {
	var out []int64

	for _, keeper := range k {
		if keeper == nil {
			continue
		}

		ids, err := keeper.KeptItems(ctx)
		if err != nil {
			return nil, err
		}

		out = append(out, ids...)
	}

	slices.Sort(out)

	return slices.Compact(out), nil
}

// WatchPass is what one watch pass did, for the log, the measurements, and the tests.
type WatchPass struct {
	// At is when its request went out, once the inbox was stored; when it sent none, when
	// it began.
	At time.Time

	// Took is how long it ran, from its request to its write; After is how long it ran
	// after the inbox was stored, which is when its request goes out, so the two differ only
	// by the moment it takes to decide.
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
// is stored. paused is why the pass sends none, which the pass works out once the inbox
// is stored.
type watchJob struct {
	login    string
	searches []Watch
	paused   string

	// test marks a routine's test (searchNow), which judges the items no search watches
	// by their details' age alone (see judge).
	test bool

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

	// mu guards last, ended, and cleaned. ended is closed, and made again, as each pass
	// ends, for AwaitPass.
	mu    sync.Mutex
	last  *WatchPass
	ended chan struct{}

	// cleaned is set once a pass with no searches has deleted every search's rows, so
	// refreshes with no routine's search send no job until one has a search again.
	cleaned bool
}

// startWatchPass hands the worker this refresh's watch request, which goes out once the
// refresh has stored the inbox, and returns the job the refresh releases then; nil when
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

// runWatchPasses runs the watch passes the refreshes hand it, one at a time, until ctx
// ends, and returns once the pass in flight has. A pass that fails is logged and one that
// panics recovered, so the next still runs. Without it running, no refresh reads the
// routines' searches.
func (s *Service) runWatchPasses(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-s.passes.jobs:
			s.runPassJob(ctx, job)
		}
	}
}

// runPassJob runs one job's pass, logging a failure or a panic, records what it did, and
// frees the worker: the record is kept and AwaitPass told even when the pass panicked.
func (s *Service) runPassJob(ctx context.Context, job *watchJob) {
	report := WatchPass{At: s.now(), Searches: len(job.searches)}
	failed := true

	defer func() {
		if p := recover(); p != nil {
			s.logger.ErrorContext(ctx, passJob+" panicked", "panic", p)
		}

		s.endPass(report, len(job.searches) == 0 && !failed)
	}()

	passCtx, cancel := context.WithTimeout(github.WithPurpose(ctx, github.PurposeWatches), passTimeout)
	defer cancel()

	got, err := s.watchPass(passCtx, job)
	report, failed = got, err != nil

	switch {
	case err != nil && ctx.Err() == nil:
		s.logger.WarnContext(ctx, passJob+" failed", "error", err)
	case err == nil:
		s.logger.DebugContext(ctx, passJob, "searches", report.Searches, "failed", report.Failed,
			"items", report.Items, "read", report.Read, "changed", report.Changed, "points", report.Points,
			"took", report.Took, "after_inbox", report.After, "paused", report.Paused)
	}
}

// endPass records what a pass did, tells AwaitPass, and frees the worker. cleaned says
// the pass had no search and deleted every search's rows.
func (s *Service) endPass(report WatchPass, cleaned bool) {
	s.passes.mu.Lock()
	s.passes.last = &report
	s.passes.cleaned = cleaned
	close(s.passes.ended)
	s.passes.ended = make(chan struct{})
	s.passes.mu.Unlock()

	s.passes.busy.Store(false)
}

// awaitPass waits for a watch pass whose request went out at since or later to end, or
// for ctx to end, and returns what it did.
func (s *Service) awaitPass(ctx context.Context, since time.Time) (WatchPass, error) {
	for {
		s.passes.mu.Lock()
		last, ended := s.passes.last, s.passes.ended
		s.passes.mu.Unlock()

		if last != nil && !last.At.Before(since) {
			return *last, nil
		}

		select {
		case <-ended:
		case <-ctx.Done():
			return WatchPass{}, ctx.Err()
		}
	}
}

// lastWatchPass returns what the last watch pass did, and false before one ran.
func (s *Service) lastWatchPass() (WatchPass, bool) {
	s.passes.mu.Lock()
	defer s.passes.mu.Unlock()

	if s.passes.last == nil {
		return WatchPass{}, false
	}

	return *s.passes.last, true
}

// watchPass waits for the inbox to be stored, sends a job's searches, and stores what they
// watch (see the notes at the top of this file).
func (s *Service) watchPass(ctx context.Context, job *watchJob) (WatchPass, error) {
	report := WatchPass{At: s.now(), Searches: len(job.searches)}

	select {
	case <-job.stored:
	case <-ctx.Done():
		return report, ctx.Err()
	}

	released := s.now()

	started, seq, got, err := s.sendPass(ctx, job)
	report.At, report.Paused = started, job.paused

	if err != nil {
		return report, err
	}

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
	report.Took, report.After = done.Sub(started), done.Sub(released)
	report.Failed, report.Items, report.Read, report.Changed = plan.failed(), plan.count(), len(plan.details), len(changed)

	if got != nil {
		report.Requests, report.Points = got.Requests+plan.detailRequests, got.RateLimit.Cost+plan.detailPoints
	}

	return report, nil
}

// sendPass works out, once the inbox is stored, whether the budget lets the job's searches
// go (job.paused), numbers the pass's fetch, and sends them. It returns when the request
// went out, its number, and what GitHub answered: nil when nothing was sent.
func (s *Service) sendPass(ctx context.Context, job *watchJob) (time.Time, uint64, *github.Watches, error) {
	if len(job.searches) > 0 {
		job.paused = s.budgetPause(ctx, job.login)
	}

	started, seq := s.now(), s.startFetch()

	if len(job.searches) == 0 || job.paused != "" {
		return started, seq, nil, nil
	}

	got, err := s.fetchWatches(ctx, job)

	return started, seq, got, err
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

// passItem is one open result of a pass, and what is stored of it. stored is what the
// pass compared it with, and then what recheck read again under the store lock.
type passItem struct {
	item     github.WatchItem
	stored   *queries.ListWatchBaselineRow
	follow   bool
	searches []string

	// read says its details are read; stale that they are only old (fillStale); changed
	// that it is told as changed once stored.
	read    bool
	stale   bool
	changed bool

	// listed is an item in an inbox list, whose card and details are the refresh's; newer
	// one a read stored after the pass's request went out, which stands; gone one stored
	// merged, or closed or out of every search since, which keeps no row (see judge).
	listed bool
	newer  bool
	gone   bool
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
	plan.options = github.DetailOptions{
		Projects: readOptions(last).Projects, Repository: true, Batch: watchDetailsBatch, Partial: true,
	}

	for _, w := range job.searches {
		for _, it := range plan.answers[w.Key].Items {
			plan.add(w, it)
		}
	}

	plan.dropClosed()

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
		plan.results[id].judge(plan.started, now, plan.job.test)
	}

	plan.fillStale()

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

// dropClosed takes out of the open results an item another search returned merged or
// closed: the requests read it a moment apart, and the close is the one kept.
func (p *passPlan) dropClosed() {
	for _, it := range p.closed {
		if _, ok := p.results[it.NodeID]; ok {
			delete(p.results, it.NodeID)
			p.order = slices.DeleteFunc(p.order, func(id string) bool { return id == it.NodeID })
		}
	}
}

// judge works out what a pass does with the item: a listed one has its details from the
// refresh, and only its watch rows are stored; one a later read stored is left as it is;
// one stored merged, or closed by such a read, keeps no row, since the search's open is
// older; any other is stored, and its details read when needsDetails says so, or, new to
// the watches, when they are missing or older than detailsStaleAfter. A test (test) reads
// the details of an item no search watches only then: the passes keep the watched ones
// current, and a test only looks.
func (p *passItem) judge(started, now time.Time, test bool) {
	st := p.stored
	if st == nil {
		p.read, p.changed = true, true

		return
	}

	p.listed = inAList(st)
	p.newer = storedAfter(st, started)
	p.gone = heldClosed(st, p.newer)

	if p.gone || p.listed || p.newer {
		return
	}

	p.read, p.stale = readsOf(p.item, st, now, test)
	p.changed = p.read || moved(p.item, st) || !st.Watched
}

// readsOf works out whether a pass reads a stored item's details (read), or only would as
// room allows since they are old (stale): see judge.
func readsOf(it github.WatchItem, st *queries.ListWatchBaselineRow, now time.Time, test bool) (read, stale bool) {
	old := !readAfter(st.DetailsFetchedAt, now.Add(-detailsStaleAfter))

	switch {
	case !st.Watched && test:
		return old, false
	case !st.Watched:
		return old || needsDetails(it, st), false
	default:
		read = needsDetails(it, st)

		return read, !read && old
	}
}

// inAList reports whether what is stored of an item has it in an inbox list.
func inAList(st *queries.ListWatchBaselineRow) bool {
	return st.ReasonReviewRequested || st.ReasonAuthored || st.ReasonAssigned
}

// storedAfter reports whether an item's card or details were stored after since.
func storedAfter(st *queries.ListWatchBaselineRow, since time.Time) bool {
	return st.FetchedAt.After(since) || readAfter(st.DetailsFetchedAt, since)
}

// heldClosed reports whether what is stored of an item the search returned open stands
// against it: merged, which no pull request comes back from, or closed by a read stored
// after the search went out (newer). A close stored before is older than the search,
// which says the item opened again.
func heldClosed(st *queries.ListWatchBaselineRow, newer bool) bool {
	switch st.State {
	case github.StateMerged:
		return true
	case github.StateClosed:
		return newer
	default:
		return false
	}
}

// readAfter reports whether details read at at were read after since; never when they
// weren't read.
func readAfter(at *time.Time, since time.Time) bool {
	return at != nil && at.After(since)
}

// moved reports whether the item's card moved since it was stored: another update time,
// state, or draft flag.
func moved(it github.WatchItem, st *queries.ListWatchBaselineRow) bool {
	return !st.ItemUpdatedAt.Equal(it.UpdatedAt.UTC()) || st.State != it.State || st.IsDraft != it.IsDraft
}

// recheck judges the item again on what recheck read of it under the store lock (now,
// nil when nothing is stored), against what the pass compared it with: an item stored
// since then, or since the pass's request went out, is newer, and keeps its card and
// details; one whose rows went since, as a read that found it merged, closed, or gone
// takes them away, is gone too; one deleted since is stored again as new.
func (p *passItem) recheck(now *queries.ListWatchBaselineRow, started time.Time) {
	before := p.stored

	if now == nil {
		if before != nil {
			p.stored, p.listed, p.newer, p.gone, p.changed = nil, false, false, false, true
		}

		return
	}

	since := before == nil || storedSince(before, now)

	p.stored = now
	p.listed = inAList(now)
	p.newer = since || storedAfter(now, started)
	p.gone = heldClosed(now, p.newer) || unwatchedSince(before, now)

	if p.listed || p.newer || p.gone {
		p.changed = false
	}
}

// unwatchedSince reports whether an item that had watch rows when the pass compared it has
// none now: a read that found it merged, closed, or gone took them away.
func unwatchedSince(before, now *queries.ListWatchBaselineRow) bool {
	return before != nil && before.Watched && !now.Watched
}

// storedSince reports whether a write changed what is stored of an item between two reads
// of it: its card, its details, its state, or its lists.
func storedSince(a, b *queries.ListWatchBaselineRow) bool {
	return !a.FetchedAt.Equal(b.FetchedAt) || !sameStamp(a.DetailsFetchedAt, b.DetailsFetchedAt) || a.State != b.State ||
		a.ReasonReviewRequested != b.ReasonReviewRequested || a.ReasonAuthored != b.ReasonAuthored ||
		a.ReasonAssigned != b.ReasonAssigned
}

// sameStamp reports whether two optional times are both missing or equal.
func sameStamp(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}

	return a.Equal(*b)
}

// needsDetails reports whether a watched item's details are read again because it moved:
// it has none; its update time, state, head commit, base branch, checks' state, or draft
// flag moved; or its checks were still running at the last read, or GitHub hadn't worked
// out whether it merges. Details older than detailsStaleAfter are read too, as room allows
// (see fillStale).
func needsDetails(it github.WatchItem, st *queries.ListWatchBaselineRow) bool {
	switch {
	case st.DetailsItemID == nil || st.DetailsFetchedAt == nil || moved(it, st):
		return true
	case !it.IsPR:
		return false
	default:
		return pullMoved(it, st)
	}
}

// fillStale picks, of the items whose details are only old, the oldest first, as many as
// the pass reads without a request more: the room left in its last details batch, or one
// batch when it reads none else. Each such item is read once in the time its turn takes to
// come round, which is detailsStaleAfter while the watches hold a few batches' worth.
func (p *passPlan) fillStale() {
	moving, stale := 0, make([]*passItem, 0, len(p.order))

	for _, id := range p.order {
		switch item := p.results[id]; {
		case item.read:
			moving++
		case item.stale:
			stale = append(stale, item)
		}
	}

	room := watchDetailsBatch
	if moving > 0 {
		room = (watchDetailsBatch - moving%watchDetailsBatch) % watchDetailsBatch
	}

	slices.SortStableFunc(stale, func(a, b *passItem) int {
		return a.stored.DetailsFetchedAt.Compare(*b.stored.DetailsFetchedAt)
	})

	for _, item := range stale[:min(room, len(stale))] {
		item.read, item.changed = true, true
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

	// A batch that failed leaves its items for the next pass: they are stored without their
	// card moving (see storeResult), so it finds them changed again. The batches that
	// succeeded are kept.
	if details != nil {
		plan.details = details.Items
		plan.detailRequests, plan.detailPoints = details.Requests, details.RateLimit.Cost
	}

	if err != nil {
		return fmt.Errorf("read the details: %w", err)
	}

	return nil
}

// storePass stores what plan read, in one transaction under the store lock: what is
// stored of its results is read again first (recheck), then each watched item and its
// details, the closed results' state, each search's rows and state, and no row of a search
// no routine has. It returns the items it stored as changed.
func (s *Service) storePass(ctx context.Context, seq uint64, plan *passPlan) ([]int64, error) {
	s.order.mu.Lock()
	defer s.order.mu.Unlock()

	var changed []int64

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		w := s.newWriter(tx)
		w.accountID, w.details = plan.accountID, plan.details

		if plan.accountID != 0 {
			stored, err := w.storeResults(ctx, plan, true)
			if err != nil {
				return err
			}

			changed = plan.changedOf(stored)
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
// whose card and details the pass stored.
func (p *passPlan) replays(seq uint64) []itemWrite {
	var out []itemWrite

	for _, id := range p.order {
		item := p.results[id]

		d, ok := p.details[id]
		if !ok || item.stored == nil || item.listed || item.newer || item.gone {
			continue
		}

		out = append(out, itemWrite{id: item.stored.ID, seq: seq, update: &itemUpdate{
			accountID: p.accountID, update: &github.ItemUpdate{Found: true, Item: item.item.Item, Details: d},
			files: map[string]fileCheck{},
		}})
	}

	return out
}

// changedOf returns the ids of the items stored, by node ID, that the pass tells as
// changed.
func (p *passPlan) changedOf(stored map[string]int64) []int64 {
	var out []int64

	for _, node := range p.order {
		if id, ok := stored[node]; ok && p.results[node].changed {
			out = append(out, id)
		}
	}

	return out
}

// storeResults reads again what is stored of the plan's results (recheck), then stores
// each one, with its watch rows when rows is set (a pass's, never a test's), the closed
// results' state, and the snoozes woken. It returns the id of every result stored or left
// as stored, by node ID; a gone one is left out.
func (w *writer) storeResults(ctx context.Context, plan *passPlan, rows bool) (map[string]int64, error) {
	if err := w.recheck(ctx, plan); err != nil {
		return nil, err
	}

	ids := make(map[string]int64, len(plan.order))

	for _, node := range plan.order {
		id, kept, err := w.storeOne(ctx, plan.results[node], rows)
		if err != nil {
			return nil, err
		}

		if kept {
			ids[node] = id
		}
	}

	closed := make([]github.Item, 0, len(plan.closed))
	for _, it := range plan.closed {
		closed = append(closed, it.Item)
	}

	if err := w.saveClosedStates(ctx, closed, &plan.started); err != nil {
		return nil, err
	}

	return ids, w.wakeSnoozed(ctx)
}

// storeOne stores one result (storeResult), and its watch rows when rows is set, or takes
// a gone one out of every search. It returns the result's id, and false for a gone one.
func (w *writer) storeOne(ctx context.Context, item *passItem, rows bool) (int64, bool, error) {
	if item.gone {
		return 0, false, w.forgetGone(ctx, item, rows)
	}

	id, err := w.storeResult(ctx, item)
	if err != nil || !rows {
		return id, err == nil, err
	}

	return id, true, w.storeWatchRows(ctx, id, item)
}

// recheck reads again, under the store lock, what is stored of the plan's results, and
// judges each against what the pass compared it with (see passItem.recheck), so a read
// stored while the pass read GitHub stands.
func (w *writer) recheck(ctx context.Context, plan *passPlan) error {
	if len(plan.order) == 0 {
		return nil
	}

	rows, err := w.q.ListWatchBaseline(ctx, queries.ListWatchBaselineParams{AccountID: plan.accountID, NodeIds: plan.order})
	if err != nil {
		return fmt.Errorf("read the watched items again: %w", err)
	}

	now := make(map[string]*queries.ListWatchBaselineRow, len(rows))
	for i := range rows {
		now[rows[i].NodeID] = &rows[i]
	}

	for _, node := range plan.order {
		plan.results[node].recheck(now[node], plan.started)
	}

	return nil
}

// forgetGone takes a gone item out of every routine's search, when the pass stores rows:
// what is stored says it no longer matches one.
func (w *writer) forgetGone(ctx context.Context, item *passItem, rows bool) error {
	if !rows || item.stored == nil {
		return nil
	}

	if err := w.q.DeleteItemWatches(ctx, item.stored.ID); err != nil {
		return fmt.Errorf("take %s out of the routines' searches: %w", item.item.Ref(), err)
	}

	return nil
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

// storeResult stores one result and returns its id: a listed one, or one a later read
// stored, is left as it is; a stored one whose details the pass meant to read, or whose
// card moved, but whose details didn't come back, is left as it is too, so the next pass
// finds it changed and reads it again, and it isn't told as changed; any other is stored
// with its repository, and its details when the pass read them. A new one is stored
// without its details when they didn't come back, and isn't told as changed until a pass
// reads them.
func (w *writer) storeResult(ctx context.Context, item *passItem) (int64, error) {
	_, read := w.details[item.item.NodeID]

	switch st := item.stored; {
	case st != nil && (item.listed || item.newer):
		return st.ID, nil
	case st != nil && !read && (item.read || moved(item.item, st)):
		item.changed = false

		return st.ID, nil
	case st == nil && !read:
		item.changed = false
	}

	return w.storeWatchedItem(ctx, item.item)
}

// storeWatchedItem stores what a search said of an item, with its repository, and its
// details when the pass read them, and returns its id.
func (w *writer) storeWatchedItem(ctx context.Context, it github.WatchItem) (int64, error) {
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
// its rows; a truncated one, or one GitHub couldn't read in full (Partial), keeps the
// rows it didn't return, stale, for staleTailFor; one that failed keeps every row, stale.
// The rows of a search no routine has are deleted.
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
	case answer.Truncated() || answer.Partial:
		if err := w.keepTail(ctx, key); err != nil {
			return st, err
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

// keepTail keeps the rows of the search with key that its answer didn't return, stale,
// and forgets those that have stayed out of its answers for staleTailFor.
func (w *writer) keepTail(ctx context.Context, key string) error {
	if err := w.q.DeleteStaleWatches(ctx, queries.DeleteStaleWatchesParams{
		WatchKey: key, SeenAt: w.now.Add(-staleTailFor),
	}); err != nil {
		return fmt.Errorf("forget a search's old tail: %w", err)
	}

	if err := w.q.MarkWatchesStale(ctx, queries.MarkWatchesStaleParams{WatchKey: key, SeenAt: w.now}); err != nil {
		return fmt.Errorf("keep a search's tail: %w", err)
	}

	return nil
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

// WatchPasses runs a Service's watch passes as a desktop worker.
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
	p.svc.runWatchPasses(ctx)
}

// Routines is the inbox's face for Autopilot's own reads, which the window never reaches
// (the Service is bound, so these are not its methods): a draft's searches read now, the
// check names of the pull requests a routine acts on, and what the last watch pass did.
type Routines struct {
	svc *Service
}

// NewRoutines returns the inbox's face for Autopilot over s.
func NewRoutines(s *Service) *Routines {
	return &Routines{svc: s}
}

// SearchNow reads searches from GitHub now, for a routine's test, or returns ErrLowBudget
// while the GraphQL budget is low (see searchNow).
func (r *Routines) SearchNow(ctx context.Context, texts []string) (map[string]SearchOutcome, int, error) {
	return r.svc.searchNow(ctx, texts)
}

// ReadChecks reads and stores the checks of the stored pull requests among ids, and
// returns what that cost in GraphQL points (see readChecks).
func (r *Routines) ReadChecks(ctx context.Context, ids []int64) (int, error) {
	return r.svc.readChecks(ctx, ids)
}

// LastWatchPass returns what the last watch pass did, and false before one ran.
func (r *Routines) LastWatchPass() (WatchPass, bool) {
	return r.svc.lastWatchPass()
}

// AwaitPass waits for a watch pass whose request went out at since or later to end, or for
// ctx to end, and returns what it did: what the refresh measurement times.
func (r *Routines) AwaitPass(ctx context.Context, since time.Time) (WatchPass, error) {
	return r.svc.awaitPass(ctx, since)
}
