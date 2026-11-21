package inbox

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
)

// The routine searches the watch tests send.
const (
	depQuery   = "is:pr is:open archived:false org:acme-org author:app/dependabot sort:updated-desc"
	otherQuery = "is:pr is:open archived:false org:acme-org label:\"chore\" sort:updated-desc"
)

var errWatchDown = errors.New("watch request failed")

// fakeWatches is a WatchSource that serves the searches set last, and says this copy reads
// them unless off is set, as with Autopilot off.
type fakeWatches struct {
	mu      sync.Mutex
	watches []Watch
	off     atomic.Bool
}

func (f *fakeWatches) Watches(context.Context, string) []Watch {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.watches)
}

func (f *fakeWatches) HoldsWatches() bool {
	return !f.off.Load()
}

// set serves watches from now on.
func (f *fakeWatches) set(watches ...Watch) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.watches = watches
}

// changedLog records what the Changed seam heard, and what the inbox showed then.
type changedLog struct {
	mu    sync.Mutex
	calls [][]int64

	// listedThen holds, for each call, how many items the snapshot listed and how many
	// inbox changes the window had been told of when the seam heard it.
	listedThen []int
	toldThen   []int
}

// all returns every id the seam heard, in order.
func (l *changedLog) all() []int64 {
	l.mu.Lock()
	defer l.mu.Unlock()

	var out []int64
	for _, c := range l.calls {
		out = append(out, c...)
	}

	return out
}

// watchHarness is a harness whose Service reads routine searches, with its watch pass
// worker running, and records what the Changed seam heard. It is built inside a synctest
// bubble, so synctest.Wait returns once a pass has stored.
type watchHarness struct {
	*harness

	source  *fakeWatches
	changed *changedLog

	// notes is the Store whose NoteStates shares the Service's write order, and halt stops
	// the running worker.
	notes *Store
	halt  func()
}

// newWatchHarness returns a watchHarness with its worker running; the test defers stop.
func newWatchHarness(t *testing.T) *watchHarness {
	t.Helper()

	h := &watchHarness{harness: newHarness(t), source: &fakeWatches{}, changed: &changedLog{}}
	h.client.details = map[string]github.ItemDetails{}
	h.notes = NewStore(h.pool, h.clock)
	h.serve(t, h.client)

	return h
}

// serve builds the harness's Service over client, its write order shared with notes, and
// runs its watch pass worker, stopping the one before.
func (h *watchHarness) serve(t *testing.T, client Client) {
	t.Helper()

	if h.halt != nil {
		h.halt()
	}

	h.svc = NewService(Config{
		Pool: h.pool, Tokens: h.tokens, Client: client, Clock: h.clock, Events: h.events, Logger: h.logs.Logger(),
		Watches: h.source, Changed: h.hear, Store: h.notes,
	})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		h.svc.runWatchPasses(ctx)
	}()

	h.halt = func() {
		cancel()
		<-done
	}
}

// gatedWatching puts the harness's Service behind a gatedClient over its fake client.
func (h *watchHarness) gatedWatching(t *testing.T) *gatedClient {
	t.Helper()

	c := &gatedClient{fakeClient: h.client}
	h.serve(t, c)

	return c
}

// stop stops the worker running now.
func (h *watchHarness) stop() {
	h.halt()
}

// hear records what the Changed seam heard, and what the inbox showed then.
func (h *watchHarness) hear(ids []int64) {
	snap, _ := h.svc.Snapshot(context.Background())

	h.changed.mu.Lock()
	defer h.changed.mu.Unlock()

	h.changed.calls = append(h.changed.calls, slices.Clone(ids))
	h.changed.listedThen = append(h.changed.listedThen, len(snap.ReviewRequested.Items))
	h.changed.toldThen = append(h.changed.toldThen, inboxUpdates(h.events))
}

// watched is a pull request a routine's search returns, open, at head with its checks in
// checks.
func watched(it github.Item, head, checks string) github.WatchItem {
	it.State = github.StateOpen
	it.Repo.NodeID = "R_example"

	return github.WatchItem{Item: it, HeadOID: head, BaseRef: "main", ChecksState: checks}
}

// dependabot is pull request number, opened by Dependabot, in no inbox list.
func dependabot(number int) github.Item {
	it := pr(number, "NONE", false)
	it.AuthorLogin, it.AuthorType = "dependabot", "Bot"

	return it
}

// answer is a search's answer of items, matching as many as it returns.
func answer(items ...github.WatchItem) github.WatchResult {
	return github.WatchResult{IssueCount: len(items), Items: items}
}

// watchedDetails reads the details the watch pass asked for, each request's IDs.
func (h *watchHarness) watchedDetails() [][]string {
	h.client.mu.Lock()
	defer h.client.mu.Unlock()

	var out [][]string

	for i, opts := range h.client.detailsOpts {
		if opts.Repository {
			out = append(out, h.client.detailsIDs[i])
		}
	}

	return out
}

// watchRows returns each stored watch row as "key #number stale follow".
type watchRow struct {
	key    string
	number int
	stale  bool
	follow bool
}

func (h *watchHarness) watchRows(t *testing.T) []watchRow {
	t.Helper()

	rows, err := h.pool.QueryContext(t.Context(), `SELECT w.watch_key, i.number, w.stale, w.follow
FROM item_watches w JOIN items i ON i.id = w.item_id ORDER BY w.watch_key, i.number`)
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	var out []watchRow

	for rows.Next() {
		var r watchRow
		require.NoError(t, rows.Scan(&r.key, &r.number, &r.stale, &r.follow))
		out = append(out, r)
	}

	require.NoError(t, rows.Err())

	return out
}

// watchedDetails is a watched pull request's details as GitHub reads them by ID: at head,
// on main, with its checks in checks, as its search said.
func watchedPRDetails(head, checks string) github.ItemDetails {
	d := prDetails()
	d.HeadOID, d.BaseRef, d.MergeState = head, "main", github.MergeStateClean
	d.Checks.State = checks

	return d
}

// startWatching lists #1 and refreshes once, so the account is known, then serves the
// Dependabot search, following its checks, with #1 and #7 in it, and #7's details.
func (h *watchHarness) startWatching(t *testing.T) {
	t.Helper()

	h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)
	synctest.Wait()

	h.source.set(Watch{Key: depQuery, Query: depQuery, Follow: true})
	h.client.details[nodeID(1)] = prDetails()
	h.client.details[nodeID(7)] = watchedPRDetails(headA, "SUCCESS")
	h.client.watches = map[string]github.WatchResult{
		depQuery: answer(watched(pr(1, "OWNER", false), headA, "SUCCESS"), watched(dependabot(7), headA, "SUCCESS")),
	}
	h.clock.Advance(time.Minute)
}

// next moves the clock on to the next refresh.
func (h *watchHarness) next() {
	h.clock.Advance(5 * time.Minute)
}

func TestAWatchPassStoresWatchedItemsOutOfTheLists(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)

		snap := h.refresh(t)

		synctest.Wait()

		assert.Equal(t, []int{1}, numbers(snap.ReviewRequested), "a watched item never shows in the lists")
		assert.Equal(t, [][]string{{nodeID(7)}}, h.watchedDetails(),
			"the watched item's details are read; the listed one has the refresh's")
		assert.Equal(t, []watchRow{{key: depQuery, number: 1, follow: true}, {key: depQuery, number: 7, follow: true}},
			h.watchRows(t))

		ids := h.changed.all()
		require.Len(t, ids, 1, "only the watched item changed")

		stored := h.storedItems(t)
		require.Len(t, stored, 2)
		assert.Equal(t, 7, stored[1].Number)
		assert.False(t, stored[1].ReasonReviewRequested || stored[1].ReasonAuthored || stored[1].ReasonAssigned)

		again, err := h.svc.Snapshot(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []int{1}, numbers(again.ReviewRequested))
		assert.Empty(t, again.Yours.Items)
		assert.Equal(t, 1, again.ReviewRequested.Total, "a watched item is in no list's count")

		last, ok := h.svc.lastWatchPass()
		require.True(t, ok)
		assert.Equal(t, 1, last.Searches)
		assert.Equal(t, 2, last.Items)
		assert.Equal(t, 1, last.Read)
		assert.Zero(t, last.Failed)
	})
}

func TestASecondPassWithNothingChangedReadsNoDetails(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		forget := func() {
			h.client.mu.Lock()
			h.client.detailsIDs, h.client.detailsOpts = nil, nil
			h.client.mu.Unlock()
		}

		step := func(head, checks string) {
			h.next()
			forget()

			h.client.watches[depQuery] = answer(watched(pr(1, "OWNER", false), headA, "SUCCESS"),
				watched(dependabot(7), head, checks))
			h.client.details[nodeID(7)] = watchedPRDetails(head, checks)

			h.refresh(t)
			synctest.Wait()
		}

		step(headA, "SUCCESS")
		assert.Empty(t, h.watchedDetails(), "nothing changed, so no watched item's details are read")
		assert.Len(t, h.changed.all(), 1, "and nothing is told as changed")

		step(headB, "PENDING")
		assert.Equal(t, [][]string{{nodeID(7)}}, h.watchedDetails(), "a new head commit is read again")

		step(headB, "PENDING")
		assert.Equal(t, [][]string{{nodeID(7)}}, h.watchedDetails(), "checks still running are read again")

		step(headB, "SUCCESS")
		assert.Equal(t, [][]string{{nodeID(7)}}, h.watchedDetails(), "checks that finished are read again")

		step(headB, "SUCCESS")
		assert.Empty(t, h.watchedDetails(), "and then left alone")

		h.clock.Advance(detailsStaleAfter)
		step(headB, "SUCCESS")
		assert.Equal(t, [][]string{{nodeID(7)}}, h.watchedDetails(), "until its details are half an hour old")
	})
}

func TestARefreshStoresTheInboxWhenTheWatchRequestFails(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		h.next()
		h.client.watchErr = errWatchDown
		h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false), pr(2, "OWNER", false)}, nil, nil)
		h.client.details[nodeID(2)] = prDetails()

		snap := h.refresh(t)

		synctest.Wait()

		assert.ElementsMatch(t, []int{1, 2}, numbers(snap.ReviewRequested), "the inbox is stored whatever the watches do")
		assert.Empty(t, snap.LastError)
		assert.Len(t, h.watchRows(t), 2, "the rows stay as they were")
		assert.Contains(t, h.logs.String(), "read the routines' searches failed")
	})
}

func TestTheInboxIsStoredAndToldBeforeTheWatchPassStores(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)

		told := inboxUpdates(h.events)
		h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false), pr(2, "OWNER", false)}, nil, nil)
		h.client.details[nodeID(2)] = prDetails()

		h.refresh(t)
		synctest.Wait()

		h.changed.mu.Lock()
		defer h.changed.mu.Unlock()

		require.Len(t, h.changed.calls, 1)
		assert.Equal(t, 2, h.changed.listedThen[0], "the inbox was stored before the pass")
		assert.Greater(t, h.changed.toldThen[0], told, "and the window was told")
	})
}

func TestAClicksFileCheckIsNotHeldUpWhileThePassReadsDetails(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		outsider := from(3, "outsider", "NONE", false)
		h.client.files = map[string]*github.ChangedFiles{refKey(3): changed(headA)}

		d := prDetails()
		d.HeadOID = headA
		h.client.details[nodeID(3)] = d
		h.client.details[nodeID(8)] = prDetails()

		snap := h.seed(t, []github.Item{outsider}, nil, nil)

		synctest.Wait()

		id := itemByNumber(t, snap.ReviewRequested, 3).ID

		h.source.set(Watch{Key: depQuery, Query: depQuery})
		h.client.watches = map[string]github.WatchResult{depQuery: answer(watched(dependabot(8), headA, "SUCCESS"))}
		h.client.watchDetailsGate = newGate(t)

		h.refresh(t)
		h.client.watchDetailsGate.waitArrived(t)

		check, err := h.svc.CheckFiles(bounded(t), id)
		require.NoError(t, err, "the file check ran while the pass waited on GitHub")
		assert.NotNil(t, check)

		h.client.watchDetailsGate.release()
		synctest.Wait()

		assert.Len(t, h.changed.all(), 1, "the watched item, once the pass could read it")
	})
}

func TestAFailedSearchKeepsItsRowsStale(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		h.next()
		delete(h.client.watches, depQuery) // the fake fails a search it has no answer for

		h.refresh(t)
		synctest.Wait()

		assert.Equal(t, []watchRow{
			{key: depQuery, number: 1, stale: true, follow: true}, {key: depQuery, number: 7, stale: true, follow: true},
		}, h.watchRows(t), "the items don't look as if they left the watch")

		status, err := NewStore(h.pool, h.clock).WatchStatus(t.Context())
		require.NoError(t, err)
		assert.NotEmpty(t, status.Searches[depQuery].Error)
		assert.NotEmpty(t, status.Searches[depQuery].FailedAt)
		assert.Equal(t, 2, status.Searches[depQuery].Count, "the last count is kept, for packing")

		last, _ := h.svc.lastWatchPass()
		assert.Equal(t, 1, last.Failed)
	})
}

func TestATruncatedSearchKeepsItsTail(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		h.clock.Advance(5 * time.Minute)
		h.client.watches[depQuery] = github.WatchResult{
			IssueCount: 150, Items: []github.WatchItem{watched(dependabot(7), headA, "SUCCESS")},
		}

		h.refresh(t)
		synctest.Wait()

		assert.Equal(t, []watchRow{
			{key: depQuery, number: 1, stale: true, follow: true}, {key: depQuery, number: 7, follow: true},
		}, h.watchRows(t), "the item past the page is kept, stale")

		h.clock.Advance(staleTailFor)
		h.refresh(t)
		synctest.Wait()

		assert.Equal(t, []watchRow{{key: depQuery, number: 7, follow: true}}, h.watchRows(t),
			"until it has stayed out of the page that long")

		h.next()
		h.client.watches[depQuery] = answer()
		h.refresh(t)
		synctest.Wait()

		assert.Empty(t, h.watchRows(t), "a search answered in full replaces its rows")
	})
}

func TestTheRowsOfASearchNoRoutineHasAreDeleted(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		h.source.set(Watch{Key: otherQuery, Query: otherQuery})
		h.client.watches[otherQuery] = answer()

		h.refresh(t)
		synctest.Wait()
		assert.Empty(t, h.watchRows(t))

		h.source.set()
		h.client.mu.Lock()
		calls := len(h.client.watchCalls)
		h.client.mu.Unlock()

		h.refresh(t)
		synctest.Wait()
		h.refresh(t)
		synctest.Wait()

		h.client.mu.Lock()
		defer h.client.mu.Unlock()

		assert.Len(t, h.client.watchCalls, calls, "no search, no request")
	})
}

func TestWatchedItemsKeepTheirFileChecks(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		id := h.storedItems(t)[1].ID
		require.NoError(t, upsertFiles(t.Context(), h.svc.q, newFilesRow(id, fileCheck{files: changed(headA)}, h.clock.Now())))

		h.refresh(t)
		synctest.Wait()

		got, err := h.svc.q.GetCheckItem(t.Context(), id)
		require.NoError(t, err)
		assert.NotNil(t, got.FilesItemID, "a watched item keeps its file check when the refresh prunes the unlisted ones")
	})
}

func TestNoWatchRequestWhileTheBudgetIsLow(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		calls := func() int {
			h.client.mu.Lock()
			defer h.client.mu.Unlock()

			return len(h.client.watchCalls)
		}

		paused := func() string {
			status, err := NewStore(h.pool, h.clock).WatchStatus(t.Context())
			require.NoError(t, err)

			return status.Paused
		}

		sent := calls()
		require.Equal(t, 1, sent)

		h.next()

		low := fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
		low.RateLimit.Remaining, low.RateLimit.Used = 1000, 4000
		h.client.inbox = low
		h.refresh(t) // the pass reads the budget the inbox query just left
		synctest.Wait()

		assert.Equal(t, sent, calls(), "the budget is under a quarter")
		assert.Equal(t, PausedLowBudget, paused())
		assert.Len(t, h.watchRows(t), 2, "the rows are kept")

		_, _, err := NewRoutines(h.svc).SearchNow(t.Context(), []string{otherQuery})
		require.ErrorIs(t, err, ErrLowBudget, "a routine's test waits for the budget too")
		assert.Equal(t, sent, calls())

		h.next()
		h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
		h.refresh(t)
		synctest.Wait()

		assert.Equal(t, sent+1, calls(), "the passes go on once the budget is back over a quarter")
		assert.Empty(t, paused())
	})
}

func TestTheFollowerTakesTheWatchedPullRequestsARoutineFollows(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.client.details[nodeID(7)] = withChecks("PENDING")
		h.client.watches[depQuery] = answer(watched(dependabot(7), headA, "PENDING"))
		setRules(t, h.harness, []string{}, []string{"acme-org/example-repo"})

		h.refresh(t)
		synctest.Wait()

		h.clock.Advance(FollowInterval)
		h.client.items = map[string]*github.ItemUpdate{nodeID(7): open(h.harness, 7, withChecks("SUCCESS"))}

		require.NoError(t, h.svc.refreshRunning(t.Context()))
		assert.Equal(t, [][]string{{nodeID(7)}}, h.client.itemsIDs,
			"a routine follows a muted repository's pull request on purpose")
		assert.Len(t, h.changed.all(), 2, "the pass's, then the follower's")
	})
}

func TestTheWatchedItemsArePrunedOnlyWhenNothingRefersToThem(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, []github.Item{pr(1, "OWNER", false), pr(2, "OWNER", false), pr(3, "OWNER", false)}, nil, nil)

	stored := h.storedItems(t)
	ids := map[int]int64{}

	for _, s := range stored {
		ids[s.Number] = s.ID
	}

	_, err := h.svc.Pin(t.Context(), ids[3])
	require.NoError(t, err)

	h.exec(t, `INSERT INTO item_watches (watch_key, item_id, follow, seen_at) VALUES ('k', ?, 0, ?)`, ids[2], h.clock.Now())

	h.client.inbox = fetched(nil, nil, nil)
	h.refresh(t)

	h.clock.Advance(pruneAfter + time.Hour)
	h.refresh(t)

	left := map[int]bool{}
	for _, s := range h.storedItems(t) {
		left[s.Number] = true
	}

	assert.Equal(t, map[int]bool{2: true, 3: true}, left, "the watched and the pinned items stay; the other goes")

	kept := newHarness(t)
	kept.svc = NewService(Config{
		Pool: kept.pool, Tokens: kept.tokens, Client: kept.client, Clock: kept.clock, Keep: fixedKeep{ids: []int64{1}},
	})
	kept.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)
	kept.client.inbox = fetched(nil, nil, nil)
	kept.refresh(t)
	kept.clock.Advance(pruneAfter + time.Hour)
	kept.refresh(t)
	assert.Len(t, kept.storedItems(t), 1, "an item the Keep seam names stays")
}

// fixedKeep is an ItemKeeper that names fixed items.
type fixedKeep struct {
	ids []int64
}

func (k fixedKeep) KeptItems(context.Context) ([]int64, error) {
	return k.ids, nil
}

func TestReadsRecordTheItemsState(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)

	merged := pr(1, "OWNER", false)
	merged.State = github.StateMerged
	h.client.items = map[string]*github.ItemUpdate{
		nodeID(1): {Found: true, Item: merged, Details: prDetails(), RateLimit: itemRate()},
	}

	h.exec(t, `INSERT INTO item_watches (watch_key, item_id, follow, seen_at) VALUES ('k', ?, 0, ?)`, id, h.clock.Now())

	_, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)

	var state string

	require.NoError(t, h.pool.QueryRowContext(t.Context(), `SELECT state FROM items WHERE id = ?`, id).Scan(&state))
	assert.Equal(t, github.StateMerged, state)

	var rows int

	require.NoError(t, h.pool.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM item_watches`).Scan(&rows))
	assert.Zero(t, rows, "a merged item leaves every routine's search")

	second := pr(2, "OWNER", false)
	second.State = github.StateOpen
	h.client.inbox = fetched([]github.Item{second}, nil, nil)
	h.refresh(t)
	require.NoError(t, h.pool.QueryRowContext(t.Context(), `SELECT state FROM items WHERE number = 2`).Scan(&state))
	assert.Equal(t, github.StateOpen, state, "the inbox's search says it is open")
}

func TestTheInboxStampsWhenAnItemArrivedInAList(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)

	arrived := func() *time.Time {
		var at *time.Time

		require.NoError(t, h.pool.QueryRowContext(t.Context(),
			`SELECT review_requested_at FROM items WHERE number = 1`).Scan(&at))

		return at
	}

	first := arrived()
	require.NotNil(t, first)

	h.clock.Advance(time.Hour)
	h.refresh(t)
	assert.True(t, first.Equal(*arrived()), "the stamp keeps the arrival while the item stays")

	h.client.inbox = fetched(nil, nil, nil)
	h.refresh(t)
	assert.Nil(t, arrived(), "it goes once the item leaves the list")
}

func TestTheActorActsOnAWatchedItem(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)

	id := h.storedItems(t)[0].ID
	h.client.inbox = fetched(nil, nil, nil)
	h.refresh(t)

	actor := NewActor(h.svc)
	act := func(context.Context, github.Token, PullTarget) (Acted, error) { return Acted{}, nil }

	_, err := actor.Act(t.Context(), id, act)
	require.Error(t, err, "an item in no list and no watch is no longer the inbox's")

	h.exec(t, `INSERT INTO item_watches (watch_key, item_id, follow, seen_at) VALUES ('k', ?, 0, ?)`, id, h.clock.Now())

	_, err = actor.Act(t.Context(), id, act)
	require.NoError(t, err, "a watched item's sheet acts as a listed one's")
}

func TestOldDetailsAreReadOnlyInTheRoomLeft(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)

	plan := func(moving, stale int) *passPlan {
		p := &passPlan{results: map[string]*passItem{}}

		for i := range moving + stale {
			id := nodeID(i)
			read := new(now.Add(-time.Duration(i) * time.Minute))
			p.results[id] = &passItem{
				read: i < moving, stale: i >= moving,
				stored: &queries.ListWatchBaselineRow{DetailsFetchedAt: read},
			}
			p.order = append(p.order, id)
		}

		return p
	}

	reads := func(p *passPlan) (stale []int) {
		for i, id := range p.order {
			if item := p.results[id]; item.read && item.stale {
				stale = append(stale, i)
			}
		}

		return stale
	}

	alone := plan(0, 40)
	alone.fillStale()
	assert.Len(t, reads(alone), watchDetailsBatch, "with nothing else to read, one batch of the oldest")
	assert.Equal(t, 39, reads(alone)[len(reads(alone))-1], "the oldest first")

	beside := plan(4, 40)
	beside.fillStale()
	assert.Len(t, reads(beside), watchDetailsBatch-4, "they fill the room the moved items leave")

	full := plan(watchDetailsBatch, 40)
	full.fillStale()
	assert.Empty(t, reads(full), "never a request more")
}

// itemState reads the stored state of the item with id.
func (h *watchHarness) itemState(t *testing.T, id int64) string {
	t.Helper()

	var state string

	require.NoError(t, h.pool.QueryRowContext(t.Context(), `SELECT state FROM items WHERE id = ?`, id).Scan(&state))

	return state
}

// watchesOf returns the numbers of the items the watch rows hold.
func (h *watchHarness) watchesOf(t *testing.T) []int {
	t.Helper()

	rows := h.watchRows(t)
	out := make([]int, 0, len(rows))

	for _, r := range rows {
		out = append(out, r.number)
	}

	return out
}

// merged is what an item's own read says of #number once it merged.
func (h *watchHarness) merged(number int) *github.ItemUpdate {
	it := dependabot(number)
	it.State, it.ClosedAt = github.StateMerged, h.clock.Now()

	return &github.ItemUpdate{Found: true, Item: it, Details: watchedPRDetails(headB, "SUCCESS"), RateLimit: itemRate()}
}

func TestAMergeReadWhileThePassReadsDetailsStands(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		id := h.storedItems(t)[1].ID
		require.Equal(t, []int{1, 7}, h.watchesOf(t))

		// #7 has a new head, so the pass reads its details, and waits on GitHub there.
		h.next()
		h.client.watches[depQuery] = answer(watched(pr(1, "OWNER", false), headA, "SUCCESS"),
			watched(dependabot(7), headB, "PENDING"))
		h.client.details[nodeID(7)] = watchedPRDetails(headB, "PENDING")
		h.client.watchDetailsGate = newGate(t)

		h.refresh(t)
		h.client.watchDetailsGate.waitArrived(t)

		// Meanwhile a read of #7's own finds it merged, and takes it out of the searches.
		h.clock.Advance(time.Second)
		h.client.items = map[string]*github.ItemUpdate{nodeID(7): h.merged(7)}
		_, err := h.svc.RefreshItem(t.Context(), id)
		require.NoError(t, err)
		require.Equal(t, []int{1}, h.watchesOf(t))

		told := len(h.changed.all())

		h.client.watchDetailsGate.release()
		synctest.Wait()

		assert.Equal(t, github.StateMerged, h.itemState(t, id), "the pass never stores its older open over the merge")
		assert.Equal(t, []int{1}, h.watchesOf(t), "and never brings its rows back")
		assert.NotContains(t, h.changed.all()[told:], id, "nor tells it as changed")
	})
}

func TestAListingStoredWhileThePassReadsDetailsStands(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		h.next()

		searched := dependabot(7)
		searched.Title = "Bump as the search read it"
		h.client.watches[depQuery] = answer(watched(pr(1, "OWNER", false), headA, "SUCCESS"),
			watched(searched, headB, "PENDING"))
		h.client.details[nodeID(7)] = watchedPRDetails(headB, "PENDING")
		h.client.watchDetailsGate = newGate(t)

		h.refresh(t)
		h.client.watchDetailsGate.waitArrived(t)

		// A whole refresh lists #7 while the pass waits, with a newer title.
		h.clock.Advance(time.Second)

		listed := dependabot(7)
		listed.Title = "Bump as the inbox listed it"
		h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, []github.Item{listed}, nil)
		h.refresh(t)

		h.client.watchDetailsGate.release()
		synctest.Wait()

		var title string

		require.NoError(t, h.pool.QueryRowContext(t.Context(), `SELECT title FROM items WHERE number = 7`).Scan(&title))
		assert.Equal(t, "Bump as the inbox listed it", title, "the refresh's newer card stands")
		assert.Equal(t, []int{1, 7}, h.watchesOf(t), "while the search still returns it")
	})
}

func TestAMergeReadWhileATestReadsDetailsStands(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)
		synctest.Wait()

		h.client.watches = map[string]github.WatchResult{otherQuery: answer(watched(dependabot(9), headA, "FAILURE"))}
		h.client.details[nodeID(9)] = watchedPRDetails(headA, "FAILURE")

		got, _, err := h.svc.searchNow(t.Context(), []string{otherQuery})
		require.NoError(t, err)
		require.Len(t, got[otherQuery].IDs, 1)

		id := got[otherQuery].IDs[0]

		// Half an hour on, a test reads #9's details again, and waits on GitHub there.
		h.clock.Advance(detailsStaleAfter + time.Minute)
		h.client.watchDetailsGate = newGate(t)

		var again map[string]SearchOutcome

		tested := make(chan error, 1)

		go func() {
			var searchErr error

			again, _, searchErr = h.svc.searchNow(t.Context(), []string{otherQuery})
			tested <- searchErr
		}()

		h.client.watchDetailsGate.waitArrived(t)
		h.clock.Advance(time.Second)
		h.client.items = map[string]*github.ItemUpdate{nodeID(9): h.merged(9)}
		_, err = h.svc.RefreshItem(t.Context(), id)
		require.NoError(t, err)

		h.client.watchDetailsGate.release()

		require.NoError(t, <-tested)
		assert.Empty(t, again[otherQuery].IDs, "the test leaves out what was merged since")
		assert.Equal(t, github.StateMerged, h.itemState(t, id), "and never stores its older open over it")
	})
}

// olderReadLandsAfterAPass has #7's own read go out and read it open, then a pass find it
// in state, and then the read land, and returns #7's id.
func (h *watchHarness) olderReadLandsAfterAPass(t *testing.T, state string) int64 {
	t.Helper()

	h.startWatching(t)
	h.refresh(t)
	synctest.Wait()

	id := h.storedItems(t)[1].ID
	c := h.gatedWatching(t)
	c.itemGate = newGate(t)

	h.client.items = map[string]*github.ItemUpdate{nodeID(7): open(h.harness, 7, watchedPRDetails(headA, "SUCCESS"))}
	read := async(func() (Snapshot, error) { return h.svc.RefreshItem(t.Context(), id) })

	c.itemGate.waitArrived(t)

	// The pass's searches go out after the read, and find #7 closed or merged.
	h.next()

	closed := watched(dependabot(7), headA, "SUCCESS")
	closed.State, closed.ClosedAt = state, h.clock.Now()
	h.client.watches[depQuery] = answer(watched(pr(1, "OWNER", false), headA, "SUCCESS"), closed)

	h.refresh(t)
	synctest.Wait()
	require.Equal(t, state, h.itemState(t, id), "the pass records the state it found")

	c.itemGate.release()
	require.NoError(t, await(t, read).err)
	synctest.Wait()

	return id
}

func TestAnOlderItemReadNeverReopensAMergeAPassStored(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		id := h.olderReadLandsAfterAPass(t, github.StateMerged)
		assert.Equal(t, github.StateMerged, h.itemState(t, id), "the read that went out first stores nothing over it")
	})
}

func TestAnOlderItemReadNeverReopensACloseAPassStored(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		id := h.olderReadLandsAfterAPass(t, github.StateClosed)
		assert.Equal(t, github.StateClosed, h.itemState(t, id), "a close can reopen, but only by a newer read")
	})
}

func TestANewerReadNeverReopensAMerge(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	h.exec(t, `UPDATE items SET state = 'MERGED' WHERE id = ?`, id)

	// GitHub never answers a merged pull request as open; a read that does is wrong, and so
	// is a stale listing.
	h.client.items = map[string]*github.ItemUpdate{nodeID(1): open(h, 1, prDetails())}
	_, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)

	var state string

	require.NoError(t, h.pool.QueryRowContext(t.Context(), `SELECT state FROM items WHERE id = ?`, id).Scan(&state))
	assert.Equal(t, github.StateMerged, state, "an item's own read never writes over a merge")

	listed := pr(1, "OWNER", false)
	listed.State = github.StateOpen
	h.client.inbox = fetched([]github.Item{listed}, nil, nil)
	h.refresh(t)

	require.NoError(t, h.pool.QueryRowContext(t.Context(), `SELECT state FROM items WHERE id = ?`, id).Scan(&state))
	assert.Equal(t, github.StateMerged, state, "nor does a whole refresh's listing")
}

func TestAnOlderItemReadNeverReopensACloseAnotherServiceRead(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		id := h.storedItems(t)[1].ID
		c := h.gatedWatching(t)
		c.itemGate = newGate(t)

		h.client.items = map[string]*github.ItemUpdate{nodeID(7): open(h.harness, 7, watchedPRDetails(headA, "SUCCESS"))}
		read := async(func() (Snapshot, error) { return h.svc.RefreshItem(t.Context(), id) })

		c.itemGate.waitArrived(t)

		// The worktree sweep's read goes out after it, and finds #7 closed.
		h.clock.Advance(time.Second)

		ref := dependabot(7).Ref()
		require.NoError(t, h.notes.NoteStates(t.Context(), h.notes.StartRead(), []item.Ref{ref},
			map[string]github.ItemState{ref.Key(): {State: github.StateClosed, ClosedAt: h.clock.Now()}}))

		c.itemGate.release()
		require.NoError(t, await(t, read).err)

		assert.Equal(t, github.StateClosed, h.itemState(t, id), "the read that went out first stores nothing over it")
	})
}

func TestARefreshStillFetchingTakesAStateReadAfterIt(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)
		synctest.Wait()

		id := h.storedItems(t)[0].ID
		c := h.gatedWatching(t)
		c.inboxGate = newGate(t)

		listed := pr(1, "OWNER", false)
		listed.State = github.StateOpen
		h.client.inbox = fetched([]github.Item{listed}, nil, nil)
		refresh := async(func() (Snapshot, error) { return h.svc.Refresh(t.Context()) })

		c.inboxGate.waitArrived(t)

		// Another service reads #1 after the refresh went out, and finds it closed.
		h.clock.Advance(time.Second)

		ref := listed.Ref()
		require.NoError(t, h.notes.NoteStates(t.Context(), h.notes.StartRead(), []item.Ref{ref},
			map[string]github.ItemState{ref.Key(): {State: github.StateClosed, ClosedAt: h.clock.Now()}}))

		c.inboxGate.release()
		require.NoError(t, await(t, refresh).err)

		assert.Equal(t, github.StateClosed, h.itemState(t, id), "the refresh applies the newer read again, over its own")
	})
}

func TestDetailsThatDidntComeBackAreReadAgainByTheNextPass(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		id := h.storedItems(t)[1].ID
		told := len(h.changed.all())

		updatedAt := func() time.Time {
			var at time.Time

			require.NoError(t, h.pool.QueryRowContext(t.Context(), `SELECT item_updated_at FROM items WHERE id = ?`, id).
				Scan(&at))

			return at.UTC()
		}

		before := updatedAt()

		// #7 has a new comment: only its update time moves, and its details don't come back.
		h.next()

		commented := dependabot(7)
		commented.UpdatedAt = commented.UpdatedAt.Add(time.Hour)
		h.client.watches[depQuery] = answer(watched(pr(1, "OWNER", false), headA, "SUCCESS"),
			watched(commented, headA, "SUCCESS"))
		h.client.watchDetailsErr = errFakeNetwork

		h.refresh(t)
		synctest.Wait()

		assert.True(t, before.Equal(updatedAt()), "its card keeps the update time its details were read at")
		assert.Len(t, h.changed.all(), told, "and it isn't told as changed")
		assert.Equal(t, []int{1, 7}, h.watchesOf(t), "while it stays watched")

		h.next()
		h.client.watchDetailsErr = nil
		h.client.mu.Lock()
		h.client.detailsIDs, h.client.detailsOpts = nil, nil
		h.client.mu.Unlock()

		h.refresh(t)
		synctest.Wait()

		assert.Equal(t, [][]string{{nodeID(7)}}, h.watchedDetails(), "the next pass reads it again")
		assert.True(t, commented.UpdatedAt.UTC().Equal(updatedAt()))
		assert.Contains(t, h.changed.all()[told:], id)
	})
}

func TestATestReadsAnUnwatchedItemsDetailsOnlyWhenOld(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)
		synctest.Wait()

		h.client.watches = map[string]github.WatchResult{otherQuery: answer(watched(dependabot(9), headA, "FAILURE"))}
		h.client.details[nodeID(9)] = watchedPRDetails(headA, "FAILURE")

		test := func() {
			h.client.mu.Lock()
			h.client.detailsIDs, h.client.detailsOpts = nil, nil
			h.client.mu.Unlock()

			_, _, err := h.svc.searchNow(t.Context(), []string{otherQuery})
			require.NoError(t, err)
		}

		test()
		assert.Equal(t, [][]string{{nodeID(9)}}, h.watchedDetails(), "new, so its details are read")

		h.clock.Advance(10 * time.Minute)

		commented := dependabot(9)
		commented.UpdatedAt = commented.UpdatedAt.Add(time.Hour)
		h.client.watches[otherQuery] = answer(watched(commented, headA, "FAILURE"))

		test()
		assert.Empty(t, h.watchedDetails(), "an item no search watches is read again only once its details are old")

		h.clock.Advance(detailsStaleAfter)
		test()
		assert.Equal(t, [][]string{{nodeID(9)}}, h.watchedDetails(), "as they are half an hour on")
	})
}

func TestATestStoresTheCardOfAnUnwatchedItemThatMoved(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)
		synctest.Wait()

		h.client.watches = map[string]github.WatchResult{otherQuery: answer(watched(dependabot(9), headA, "FAILURE"))}
		h.client.details[nodeID(9)] = watchedPRDetails(headA, "FAILURE")

		test := func() SearchOutcome {
			h.client.mu.Lock()
			h.client.detailsIDs, h.client.detailsOpts = nil, nil
			h.client.mu.Unlock()

			got, _, err := h.svc.searchNow(t.Context(), []string{otherQuery})
			require.NoError(t, err)

			return got[otherQuery]
		}

		id := test().IDs[0]

		card := func() (string, time.Time) {
			var (
				state string
				at    time.Time
			)

			require.NoError(t, h.pool.QueryRowContext(t.Context(), `SELECT state, item_updated_at FROM items WHERE id = ?`, id).
				Scan(&state, &at))

			return state, at.UTC()
		}

		// A new comment moves only its update time: its details stay unread, as they are
		// fresh, but its card moves.
		h.clock.Advance(10 * time.Minute)

		commented := dependabot(9)
		commented.UpdatedAt = commented.UpdatedAt.Add(time.Hour)
		h.client.watches[otherQuery] = answer(watched(commented, headA, "FAILURE"))

		require.Equal(t, []int64{id}, test().IDs)
		assert.Empty(t, h.watchedDetails())

		_, at := card()
		assert.True(t, commented.UpdatedAt.UTC().Equal(at), "the card of a test's result moves with its search")

		// Closed since, and open again: its details are read for the new state.
		h.exec(t, `UPDATE items SET state = 'CLOSED' WHERE id = ?`, id)
		h.clock.Advance(time.Minute)

		reopened := commented
		reopened.UpdatedAt = reopened.UpdatedAt.Add(time.Hour)
		h.client.watches[otherQuery] = answer(watched(reopened, headA, "FAILURE"))

		require.Equal(t, []int64{id}, test().IDs)
		assert.Equal(t, [][]string{{nodeID(9)}}, h.watchedDetails(), "a state that moved is read")

		state, _ := card()
		assert.Equal(t, github.StateOpen, state, "and its card is open again")
	})
}

func TestATestKeepsAResultAPassStoppedWatching(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		id := h.storedItems(t)[1].ID

		// A test whose search returns #7 at a new head reads its details, and waits there.
		h.next()
		h.client.watches[otherQuery] = answer(watched(dependabot(7), headB, "SUCCESS"))
		h.client.details[nodeID(7)] = watchedPRDetails(headB, "SUCCESS")
		h.client.watchDetailsGate = newGate(t)

		var tested map[string]SearchOutcome

		done := make(chan error, 1)

		go func() {
			var err error

			tested, _, err = h.svc.searchNow(t.Context(), []string{otherQuery})
			done <- err
		}()

		h.client.watchDetailsGate.waitArrived(t)

		// Meanwhile the routine is edited away, and a pass drops its search's rows.
		h.source.set()
		h.refresh(t)
		synctest.Wait()
		require.Empty(t, h.watchRows(t))

		h.client.watchDetailsGate.release()
		require.NoError(t, <-done)
		assert.Equal(t, []int64{id}, tested[otherQuery].IDs, "an open result stays the test's, whatever a pass dropped")
	})
}

func TestAPartialSearchDropsNothingFromTheWatch(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		h.next()
		h.client.watches[depQuery] = github.WatchResult{
			IssueCount: 1, Items: []github.WatchItem{watched(dependabot(7), headA, "SUCCESS")}, Partial: true,
		}

		h.refresh(t)
		synctest.Wait()

		assert.Equal(t, []watchRow{
			{key: depQuery, number: 1, stale: true, follow: true}, {key: depQuery, number: 7, follow: true},
		}, h.watchRows(t), "an item GitHub may have failed alone is kept, stale")
	})
}

func TestTheWatchRequestGoesOutOnlyOnceTheInboxIsStored(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)

		c := h.gatedWatching(t)
		c.inboxGate = newGate(t)
		h.client.watchErr = errWatchDown

		calls := func() int {
			h.client.mu.Lock()
			defer h.client.mu.Unlock()

			return len(h.client.watchCalls)
		}

		refresh := async(func() (Snapshot, error) { return h.svc.Refresh(t.Context()) })

		c.inboxGate.waitArrived(t)
		synctest.Wait()
		assert.Zero(t, calls(), "nothing is sent beside the inbox query")
		c.inboxGate.release()

		got := await(t, refresh)
		require.NoError(t, got.err, "a watch request that fails never fails the refresh")
		assert.Equal(t, []int{1}, numbers(got.snap.ReviewRequested))

		synctest.Wait()
		assert.Equal(t, 1, calls(), "the request went out once the inbox was stored")
	})
}

func TestAPassThatPanicsStillEnds(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.client.watchPanic = true

		since := h.clock.Now()
		h.refresh(t)

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		_, err := h.svc.awaitPass(ctx, since)
		require.NoError(t, err, "what waits for the pass hears it end")
		assert.Contains(t, h.logs.String(), "panicked")

		h.client.mu.Lock()
		h.client.watchPanic = false
		h.client.mu.Unlock()

		h.next()
		h.refresh(t)
		synctest.Wait()
		assert.Equal(t, []int{1, 7}, h.watchesOf(t), "and the next refresh's pass runs")
	})
}

// runningChecks lists #1 to #listed and watches #101 on, following them, all with their
// checks running, each answering a read with its checks passed, the watched ones read
// longest ago, which alone would put them first; it returns the listed ones' node IDs.
func (h *watchHarness) runningChecks(t *testing.T, listed, followed int) map[string]bool {
	t.Helper()

	var (
		items   []github.Item
		results []github.WatchItem
		nodes   = map[string]bool{}
	)

	h.client.items = map[string]*github.ItemUpdate{}

	for n := 1; n <= listed; n++ {
		items = append(items, pr(n, "OWNER", false))
		h.client.details[nodeID(n)] = withChecks("PENDING")
		h.client.items[nodeID(n)] = open(h.harness, n, withChecks("SUCCESS"))
		nodes[nodeID(n)] = true
	}

	for n := 101; n < 101+followed; n++ {
		results = append(results, watched(dependabot(n), headA, "PENDING"))
		h.client.details[nodeID(n)] = watchedPRDetails(headA, "PENDING")
		h.client.items[nodeID(n)] = open(h.harness, n, withChecks("SUCCESS"))
	}

	h.seed(t, items, nil, nil)
	synctest.Wait()

	h.source.set(Watch{Key: depQuery, Query: depQuery, Follow: true})
	h.client.watches = map[string]github.WatchResult{depQuery: answer(results...)}
	h.clock.Advance(time.Minute)
	h.refresh(t)
	synctest.Wait()

	h.exec(t, `UPDATE item_details SET fetched_at = ? WHERE item_id IN (SELECT item_id FROM item_watches)
  AND item_id NOT IN (SELECT id FROM items WHERE reason_review_requested = 1)`, h.clock.Now().Add(-time.Hour))
	h.clock.Advance(FollowInterval)

	return nodes
}

func TestTheFollowerReadsInBatchesTheListedFirst(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		nodes := h.runningChecks(t, 40, 20)

		require.NoError(t, h.svc.refreshRunning(t.Context()))

		require.Len(t, h.client.itemsIDs, 2, "50 pull requests, in two requests")

		read := map[string]bool{}

		for _, ids := range h.client.itemsIDs {
			assert.LessOrEqual(t, len(ids), followBatch)

			for _, id := range ids {
				read[id] = true
			}
		}

		assert.Len(t, read, followMax)

		for node := range nodes {
			assert.True(t, read[node], "every listed pull request is read before a watched one: %s", node)
		}
	})
}

func TestTheFollowerLeavesTheWatchedPullRequestsWhileThisCopyReadsNoSearch(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		nodes := h.runningChecks(t, 2, 3)

		// read returns the pull requests a round read.
		read := func() map[string]bool {
			out := map[string]bool{}

			for _, ids := range h.client.itemsIDs {
				for _, id := range ids {
					out[id] = true
				}
			}

			h.client.itemsIDs = nil

			return out
		}

		// Autopilot off: no copy reads the routines' searches, and what is stored of them
		// is stale, so the follower follows the inbox's own cards alone.
		h.source.off.Store(true)
		require.NoError(t, h.svc.refreshRunning(t.Context()))
		assert.Equal(t, nodes, read(), "only the listed pull requests are read")

		// The searches the copy reads again, their pull requests are followed again.
		h.source.off.Store(false)
		require.NoError(t, h.svc.refreshRunning(t.Context()))
		assert.Equal(t, map[string]bool{nodeID(101): true, nodeID(102): true, nodeID(103): true}, read())
	})
}

func TestAFollowerBatchThatFailsLeavesItsPullRequestsForTheNextRound(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.runningChecks(t, 40, 0)
		h.client.itemsFail = nodeID(1) // the batch that holds #1, of #1 to #25

		err := h.svc.refreshRunning(t.Context())
		require.ErrorIs(t, err, errFakeNetwork, "the failed request is reported")
		require.Len(t, h.client.itemsIDs, 2)

		checks := func(number int) string {
			var state string

			require.NoError(t, h.pool.QueryRowContext(t.Context(), `SELECT d.checks_state FROM item_details d
JOIN items i ON i.id = d.item_id WHERE i.number = ?`, number).Scan(&state))

			return state
		}

		for n := 1; n <= 25; n++ {
			assert.Equal(t, "PENDING", checks(n), "#%d's request failed, so it is read again next round", n)
		}

		for n := 26; n <= 40; n++ {
			assert.Equal(t, "SUCCESS", checks(n), "#%d's request was answered, and is stored", n)
		}
	})
}

func TestAKeeperThatCantBeReadOnlySkipsThePrune(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.svc = NewService(Config{
		Pool: h.pool, Tokens: h.tokens, Client: h.client, Clock: h.clock, Logger: h.logs.Logger(),
		Keep: Keepers{failingKeep{}},
	})
	h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)
	h.client.inbox = fetched(nil, nil, nil)
	h.refresh(t)

	h.clock.Advance(pruneAfter + time.Hour)
	h.refresh(t) // must succeed

	assert.Len(t, h.storedItems(t), 1, "an item something may refer to is kept")
	assert.Contains(t, h.logs.String(), "prunes no old item")
}

func TestReadChecksWaitsForTheBudget(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		id := h.storedItems(t)[1].ID

		h.next()

		low := fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
		low.RateLimit.Remaining, low.RateLimit.Used = 1000, 4000
		h.client.inbox = low
		h.refresh(t)
		synctest.Wait()

		points, err := NewRoutines(h.svc).ReadChecks(t.Context(), []int64{id})
		require.ErrorIs(t, err, ErrLowBudget)
		assert.Zero(t, points)
		assert.Empty(t, h.client.checksIDs, "nothing is sent under a quarter of the budget")
	})
}

func TestKeepersKeepWhatAnyOfThemNames(t *testing.T) {
	t.Parallel()

	got, err := Keepers{fixedKeep{ids: []int64{3, 1}}, nil, fixedKeep{ids: []int64{1, 2}}}.KeptItems(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 2, 3}, got)

	_, err = Keepers{fixedKeep{ids: []int64{1}}, failingKeep{}}.KeptItems(t.Context())
	require.ErrorIs(t, err, errWatchDown, "one that fails fails the whole, so the prune keeps everything")
}

// failingKeep is an ItemKeeper that can't say what it keeps.
type failingKeep struct{}

func (failingKeep) KeptItems(context.Context) ([]int64, error) {
	return nil, errWatchDown
}

// inboxUpdates counts the inbox changes events heard.
func inboxUpdates(events *inboxEvents) int {
	n := 0

	for _, e := range events.All() {
		if e.Name() == event.NameInboxUpdated {
			n++
		}
	}

	return n
}
