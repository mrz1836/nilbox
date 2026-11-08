package inbox

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/trust"
)

// waitTimeout bounds every wait in these tests.
const waitTimeout = 5 * time.Second

// gate holds the calls that reach it until it opens, and says when each one arrives.
type gate struct {
	arrived chan struct{}
	open    chan struct{}
	once    sync.Once
}

// newGate returns a closed gate that opens when the test ends, if not before.
func newGate(t *testing.T) *gate {
	t.Helper()

	g := &gate{arrived: make(chan struct{}, 16), open: make(chan struct{})}
	t.Cleanup(g.release)

	return g
}

// pass waits for the gate to open, or for ctx to end.
func (g *gate) pass(ctx context.Context) error {
	g.arrived <- struct{}{}

	select {
	case <-g.open:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// waitArrived waits for a call to reach the gate.
func (g *gate) waitArrived(t *testing.T) {
	t.Helper()

	select {
	case <-g.arrived:
	case <-time.After(waitTimeout):
		require.FailNow(t, "no call reached the gate")
	}
}

// release opens the gate.
func (g *gate) release() {
	g.once.Do(func() { close(g.open) })
}

// gatedClient is a fakeClient whose inbox query and single-item query can wait at a
// gate, whose pull request head read can wait at one once it has read the head, and
// whose inbox query can be refused as bad credentials a number of times.
type gatedClient struct {
	*fakeClient

	inboxGate *gate
	itemGate  *gate
	headGate  *gate

	inboxCalls atomic.Int32
	refusals   atomic.Int32
}

func (c *gatedClient) FetchInbox(ctx context.Context, tok github.Token) (*github.Inbox, error) {
	c.inboxCalls.Add(1)

	if c.refusals.Add(-1) >= 0 {
		return nil, &github.StatusError{StatusCode: http.StatusUnauthorized, Message: "Bad credentials"}
	}

	if c.inboxGate != nil {
		if err := c.inboxGate.pass(ctx); err != nil {
			return nil, err
		}
	}

	return c.fakeClient.FetchInbox(ctx, tok)
}

func (c *gatedClient) FetchItem(ctx context.Context, tok github.Token, id string) (*github.ItemUpdate, error) {
	if c.itemGate != nil {
		if err := c.itemGate.pass(ctx); err != nil {
			return nil, err
		}
	}

	return c.fakeClient.FetchItem(ctx, tok, id)
}

func (c *gatedClient) FetchPullHead(ctx context.Context, tok github.Token, ref item.Ref) (*github.PullHead, error) {
	head, err := c.fakeClient.FetchPullHead(ctx, tok, ref)

	if c.headGate != nil {
		if gateErr := c.headGate.pass(ctx); gateErr != nil {
			return nil, gateErr
		}
	}

	return head, err
}

// gated puts the harness's service behind a gatedClient over its fake client.
func (h *harness) gated() *gatedClient {
	return h.gatedTelling(nil)
}

// gatedTelling puts the harness's service behind a gatedClient over its fake client, and
// tells arrivals to sink.
func (h *harness) gatedTelling(sink ArrivalSink) *gatedClient {
	c := &gatedClient{fakeClient: h.client}
	h.svc = NewService(Config{
		Pool: h.pool, Tokens: h.tokens, Client: c, Clock: h.clock, Events: h.events, Arrivals: sink,
	})

	return c
}

// push moves the fake pull request with a number to head: its details and its files.
func (h *harness) push(number int, head string) {
	h.client.mu.Lock()
	defer h.client.mu.Unlock()

	d := h.client.details[nodeID(number)]
	d.HeadOID = head
	h.client.details[nodeID(number)] = d
	h.client.files[refKey(number)] = changed(head)
}

// storedHead returns the head commit of the details or the file check stored for an
// item: table is item_details or item_files.
func (h *harness) storedHead(t *testing.T, table string, id int64) string {
	t.Helper()

	var head string
	require.NoError(t, h.pool.QueryRowContext(t.Context(), "SELECT head_oid FROM "+table+" WHERE item_id = ?", id).Scan(&head))

	return head
}

// setInbox replaces what the fake inbox query answers while other calls may run.
func (h *harness) setInbox(inbox *github.Inbox) {
	h.client.mu.Lock()
	defer h.client.mu.Unlock()

	h.client.inbox = inbox
}

// outcome is what a call that returns a snapshot returned.
type outcome struct {
	snap Snapshot
	err  error
}

// async runs call in the background and returns where its outcome arrives.
func async(call func() (Snapshot, error)) <-chan outcome {
	out := make(chan outcome, 1)

	go func() {
		snap, err := call()
		out <- outcome{snap, err}
	}()

	return out
}

// await waits for an outcome.
func await(t *testing.T, ch <-chan outcome) outcome {
	t.Helper()

	select {
	case got := <-ch:
		return got
	case <-time.After(waitTimeout):
		require.FailNow(t, "the call never returned")

		return outcome{}
	}
}

// bounded returns a context that ends after waitTimeout, so a call that waits where it
// should not fails instead of hanging the test.
func bounded(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	t.Cleanup(cancel)

	return ctx
}

// refreshUntilGate starts a refresh that waits at the gate in its inbox query, and returns
// where its outcome arrives once the gate opens.
func refreshUntilGate(t *testing.T, h *harness, c *gatedClient) <-chan outcome {
	t.Helper()

	c.inboxGate = newGate(t)
	refresh := async(func() (Snapshot, error) { return h.svc.Refresh(t.Context()) })

	c.inboxGate.waitArrived(t)

	return refresh
}

func TestRefreshNowJoinsTheRefreshInFlight(t *testing.T) {
	t.Parallel()

	// In a synctest bubble, synctest.Wait returns once every goroutine in it is blocked,
	// which is when the second caller has joined the refresh in flight.
	synctest.Test(t, func(t *testing.T) {
		h, _ := itemHarness(t)
		c := h.gated()
		tokens := h.tokens.calls

		first := refreshUntilGate(t, h, c)
		second := async(func() (Snapshot, error) { return h.svc.Refresh(t.Context()) })

		synctest.Wait()
		c.inboxGate.release()

		a, b := await(t, first), await(t, second)
		require.NoError(t, a.err)
		require.NoError(t, b.err)
		assert.Equal(t, a.snap, b.snap, "both callers get the one refresh's snapshot")
		assert.Equal(t, int32(1), c.inboxCalls.Load(), "GitHub was asked once")
		assert.Equal(t, tokens+1, h.tokens.calls, "the token was borrowed once")
	})
}

func TestRefreshItemDoesNotWaitForARefresh(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	c := h.gated()

	retitled := pr(1, "OWNER", false)
	retitled.State, retitled.Title = github.StateOpen, "Item 1, retitled"
	h.client.items = map[string]*github.ItemUpdate{
		nodeID(1): {Found: true, Item: retitled, Details: prDetails(), RateLimit: itemRate()},
	}

	refresh := refreshUntilGate(t, h, c)

	snap, err := h.svc.RefreshItem(bounded(t), id)
	require.NoError(t, err, "the item's refresh finished while the whole refresh was still fetching")
	assert.Equal(t, "Item 1, retitled", itemByNumber(t, snap.ReviewRequested, 1).Title)

	c.inboxGate.release()
	require.NoError(t, await(t, refresh).err)
}

func TestCheckFilesDoesNotWaitForARefresh(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(1, "octo-outsider", "NONE", true))
	snap := h.refresh(t)
	c := h.gated()

	refresh := refreshUntilGate(t, h, c)

	check, err := h.svc.CheckFiles(bounded(t), itemByNumber(t, snap.ReviewRequested, 1).ID)
	require.NoError(t, err, "the check finished while the whole refresh was still fetching")
	assert.Equal(t, sensitive.StatusFlagged, check.Status)

	c.inboxGate.release()
	require.NoError(t, await(t, refresh).err)
}

func TestARefreshDoesNotBringBackAnItemMergedWhileItFetched(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	c := h.gated()

	merged := pr(1, "OWNER", false)
	merged.State = github.StateMerged
	h.client.items = map[string]*github.ItemUpdate{
		nodeID(1): {Found: true, Item: merged, Details: prDetails(), RateLimit: itemRate()},
	}

	// The refresh's inbox query was sent before the merge, so it still lists #1.
	refresh := refreshUntilGate(t, h, c)

	snap, err := h.svc.RefreshItem(bounded(t), id)
	require.NoError(t, err)
	assert.Equal(t, []int{2}, numbers(snap.ReviewRequested))

	c.inboxGate.release()

	got := await(t, refresh)
	require.NoError(t, got.err)
	assert.Equal(t, []int{2}, numbers(got.snap.ReviewRequested), "the merged item stays out of the lists")
	assert.Empty(t, got.snap.Assigned.Items)
	assert.Equal(t, [3]bool{false, false, false}, h.reasons(t, 1))
	assert.Equal(t, "2026-01-10T12:10:00Z", got.snap.LastRefreshedAt, "the refresh stored the rest")
}

func TestARefreshKeepsWhatAnItemRefreshStoredWhileItFetched(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	c := h.gated()

	retitled := pr(1, "OWNER", false)
	retitled.State, retitled.Title = github.StateOpen, "Item 1, retitled"

	details := prDetails()
	details.Checks = github.Checks{State: "FAILURE", Passed: 10, Failed: 2}
	h.client.items = map[string]*github.ItemUpdate{
		nodeID(1): {Found: true, Item: retitled, Details: details, RateLimit: itemRate()},
	}

	refresh := refreshUntilGate(t, h, c)

	_, err := h.svc.RefreshItem(bounded(t), id)
	require.NoError(t, err)

	c.inboxGate.release()

	got := await(t, refresh)
	require.NoError(t, got.err)

	it := itemByNumber(t, got.snap.ReviewRequested, 1)
	assert.Equal(t, "Item 1, retitled", it.Title, "the item's own refresh is the newer")
	require.NotNil(t, it.Details)
	assert.Equal(t, Checks{State: "FAILURE", Passed: 10, Failed: 2}, it.Details.Checks)
	assert.Equal(t, [3]bool{true, false, true}, h.reasons(t, 1), "the lists are the refresh's")
	assert.Equal(t, 4990, got.snap.Rate.GraphQL.Remaining, "the refresh's budget reading is the latest")
}

func TestAnItemRefreshOlderThanAStoredRefreshStoresNothing(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	c := h.gated()
	c.itemGate = newGate(t)

	stale := pr(1, "OWNER", false)
	stale.State, stale.Title = github.StateMerged, "Item 1, before"
	h.client.items = map[string]*github.ItemUpdate{
		nodeID(1): {Found: true, Item: stale, Details: prDetails(), RateLimit: itemRate()},
	}

	item := async(func() (Snapshot, error) { return h.svc.RefreshItem(t.Context(), id) })

	c.itemGate.waitArrived(t)

	// A whole refresh starts after the item's own and stores first.
	fresh := pr(1, "OWNER", false)
	fresh.Title = "Item 1, after"
	h.setInbox(fetched([]github.Item{fresh, pr(2, "OWNER", false)}, nil, []github.Item{fresh}))
	h.refresh(t)

	c.itemGate.release()

	got := await(t, item)
	require.NoError(t, got.err)
	assert.Equal(t, "Item 1, after", itemByNumber(t, got.snap.ReviewRequested, 1).Title)
	assert.Equal(t, [3]bool{true, false, true}, h.reasons(t, 1), "the older answer did not take it out")
}

func TestTriageDuringARefreshHolds(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	c := h.gated()

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)

	other := itemByNumber(t, snap.ReviewRequested, 2).ID

	refresh := refreshUntilGate(t, h, c)

	_, err = h.svc.Dismiss(bounded(t), id)
	require.NoError(t, err)

	_, err = h.svc.Pin(bounded(t), other)
	require.NoError(t, err)

	c.inboxGate.release()

	got := await(t, refresh)
	require.NoError(t, got.err)
	assert.True(t, itemByNumber(t, got.snap.ReviewRequested, 1).Dismissed())
	assert.NotEmpty(t, itemByNumber(t, got.snap.ReviewRequested, 2).PinnedAt)
	assert.Equal(t, []int{2, 1}, numbers(got.snap.ReviewRequested), "the pinned item comes first")
}

func TestARefreshDoesNotAnnounceAnItemMergedWhileItFetched(t *testing.T) {
	t.Parallel()

	h, sink := arrivalHarness(t)
	h.client.inbox = fetched([]github.Item{byOther(pr(2, "OWNER", false))}, nil, []github.Item{byOther(pr(1, "OWNER", false))})
	snap := h.refresh(t)
	id := itemByNumber(t, snap.Assigned, 1).ID

	c := h.gatedTelling(sink)

	merged := byOther(pr(1, "OWNER", false))
	merged.State = github.StateMerged
	h.client.items = map[string]*github.ItemUpdate{nodeID(1): {Found: true, Item: merged, RateLimit: itemRate()}}

	// The refresh's answer, sent before the merge, newly asks for #1's review.
	h.setInbox(fetched([]github.Item{byOther(pr(1, "OWNER", false)), byOther(pr(2, "OWNER", false))}, nil,
		[]github.Item{byOther(pr(1, "OWNER", false))}))

	refresh := refreshUntilGate(t, h, c)

	_, err := h.svc.RefreshItem(bounded(t), id)
	require.NoError(t, err)

	c.inboxGate.release()
	require.NoError(t, await(t, refresh).err)
	assert.Nil(t, sink.last(), "no arrival is told for an item the refresh no longer lists")
}

func TestARefreshBorrowsTheTokenAgainWhenGitHubRefusesIt(t *testing.T) {
	t.Parallel()

	h, _ := itemHarness(t)
	c := h.gated()
	c.refusals.Store(1)

	tokens := h.tokens.calls
	snap := h.refresh(t)

	assert.Empty(t, snap.LastError)
	assert.Equal(t, []string{fakeToken}, h.tokens.forgot, "the refused token was given back")
	assert.Equal(t, tokens+2, h.tokens.calls, "a fresh token was borrowed for the one retry")
	assert.Equal(t, int32(2), c.inboxCalls.Load())
}

func TestARefreshRefusedTwiceFails(t *testing.T) {
	t.Parallel()

	h, _ := itemHarness(t)
	c := h.gated()
	c.refusals.Store(2)

	snap, err := h.svc.Refresh(t.Context())
	require.ErrorIs(t, err, github.ErrUnexpectedStatus)
	assert.True(t, github.IsBadCredentials(err))
	assert.Contains(t, snap.LastError, "HTTP 401")
	assert.Equal(t, []string{fakeToken}, h.tokens.forgot, "the token is given back once, and retried once")
	assert.Equal(t, int32(2), c.inboxCalls.Load())
}

func TestRefreshItemBorrowsTheTokenAgainWhenGitHubRefusesIt(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	h.client.itemErr = &github.StatusError{StatusCode: http.StatusUnauthorized, Message: "Bad credentials"}

	_, err := h.svc.RefreshItem(t.Context(), id)
	require.ErrorIs(t, err, github.ErrUnexpectedStatus)
	assert.Equal(t, []string{fakeToken}, h.tokens.forgot)
	assert.Len(t, h.client.itemIDs, 2, "the item was asked about once more with a fresh token")
}

func TestTheSnapshotIsKeptUntilTheInboxChanges(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)

	before, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)

	// A write past the service shows that the snapshot is kept rather than read again.
	_, err = h.pool.ExecContext(t.Context(), "UPDATE items SET title = 'Retitled behind its back'")
	require.NoError(t, err)

	kept, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, before, kept)

	// The service's own write empties the cache, and the snapshot is read again.
	after, err := h.svc.Dismiss(t.Context(), id)
	require.NoError(t, err)
	assert.True(t, itemByNumber(t, after.ReviewRequested, 1).Dismissed())
	assert.Equal(t, "Retitled behind its back", itemByNumber(t, after.ReviewRequested, 2).Title)

	again, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, after, again)
}

func TestTheSnapshotFollowsTheTrustedBots(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	bot := pr(1, "NONE", false)
	bot.AuthorLogin, bot.AuthorType = "helper-bot", "Bot"
	h.client.inbox = fetched([]github.Item{bot}, nil, nil)

	snap := h.refresh(t)
	assert.Equal(t, trust.ClassOutside, itemByNumber(t, snap.ReviewRequested, 1).Trust)

	h.setPrefs(t, map[string]string{trust.PrefBots: trust.EncodeBots([]string{"helper-bot"})})

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, trust.ClassBot, itemByNumber(t, snap.ReviewRequested, 1).Trust,
		"a change of the settings the snapshot depends on reads it again")
}

func TestASnapshotReadAcrossAWriteIsNotKept(t *testing.T) {
	t.Parallel()

	var c snapshotCache

	seen := c.load()
	c.empty()
	c.keep(seen, Snapshot{LastError: "read before the write"}, settingsKey{})
	assert.Nil(t, c.load().snap, "a snapshot read across a write is not kept")

	seen = c.load()
	c.keep(seen, Snapshot{LastError: "read after the write"}, settingsKey{})
	require.NotNil(t, c.load().snap)
	assert.Equal(t, "read after the write", c.load().snap.LastError)
}

func TestTheCategoriesAreCompiledOncePerChange(t *testing.T) {
	t.Parallel()

	var c scopeCache

	status := map[string]string{prefViewer: "octo-user", sensitive.PrefPatterns: `["deploy/*.tf"]`}
	first := c.scope(status)

	status[prefViewer] = "octo-other"
	second := c.scope(status)
	assert.Same(t, first.matcher, second.matcher, "the same settings reuse the compiled categories")
	assert.Equal(t, "octo-other", second.viewer, "the viewer is the status's own")

	status[sensitive.PrefPatterns] = `["deploy/*.sh"]`
	third := c.scope(status)
	assert.NotSame(t, first.matcher, third.matcher, "new patterns are compiled")

	_, _, matched := third.matcher.Match("deploy/run.sh")
	assert.True(t, matched)
}

func TestItemLocksHoldOneItemAtATime(t *testing.T) {
	t.Parallel()

	var l itemLocks

	unlockFirst, err := l.lock(t.Context(), 1)
	require.NoError(t, err)

	unlockOther, err := l.lock(t.Context(), 2)
	require.NoError(t, err, "another item is not held up")
	unlockOther()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	_, err = l.lock(ctx, 1)
	require.ErrorIs(t, err, context.DeadlineExceeded, "the same item waits until it is released")

	unlockFirst()

	unlockAgain, err := l.lock(t.Context(), 1)
	require.NoError(t, err)
	unlockAgain()

	assert.Empty(t, l.locks, "a lock no one holds or waits for is dropped")
}

func TestARefreshDoesNotWakeASnoozeSetAfterANewerItemRefresh(t *testing.T) {
	t.Parallel()

	h, sink := arrivalHarness(t)

	before := prDetails()
	before.HeadOID, before.Commits = headA, 3
	h.client.inbox = fetched([]github.Item{byOther(pr(1, "OWNER", false))}, nil, nil)
	h.client.details = map[string]github.ItemDetails{nodeID(1): before}

	snap := h.refresh(t)
	id := itemByNumber(t, snap.ReviewRequested, 1).ID
	c := h.gatedTelling(sink)

	// A scheduled refresh asks GitHub before a push, and so reads head A.
	refresh := refreshUntilGate(t, h, c)

	// The push lands; the user refreshes the item, which reads head B, then snoozes it.
	after := before
	after.HeadOID, after.Commits = headB, 5
	pushed := byOther(pr(1, "OWNER", false))
	pushed.State = github.StateOpen
	h.client.items = map[string]*github.ItemUpdate{
		nodeID(1): {Found: true, Item: pushed, Details: after, RateLimit: itemRate()},
	}

	_, err := h.svc.RefreshItem(bounded(t), id)
	require.NoError(t, err)

	_, err = h.svc.Snooze(bounded(t), id, 3)
	require.NoError(t, err)

	c.inboxGate.release()

	got := await(t, refresh)
	require.NoError(t, got.err)

	it := itemByNumber(t, got.snap.ReviewRequested, 1)
	assert.True(t, it.Snoozed(h.clock.Now()), "the snooze holds: the refresh's older head is not new activity")
	assert.Nil(t, it.WokeBy)
	assert.Equal(t, headB, h.storedHead(t, "item_details", id), "the item keeps the newer details")
	assert.Nil(t, sink.last(), "no woke arrival is told")
}

func TestAFileCheckOlderThanAnEarlierRefreshIsNotStored(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(1, "octo-outsider", "NONE", true))
	snap := h.refresh(t)
	id := itemByNumber(t, snap.ReviewRequested, 1).ID

	c := h.gated()
	c.headGate = newGate(t)

	type checked struct {
		check *sensitive.Check
		err   error
	}

	done := make(chan checked, 1)

	go func() {
		check, err := h.svc.CheckFiles(t.Context(), id)
		done <- checked{check, err}
	}()

	// The check reads head A, and stalls there.
	c.headGate.waitArrived(t)

	// The pull request is pushed to head B; one refresh checks its files there, and the
	// next has no check due.
	h.push(1, headB)
	h.refresh(t)
	h.refresh(t)
	assert.Equal(t, headB, h.storedHead(t, "item_files", id))

	c.headGate.release()

	var got checked

	select {
	case got = <-done:
	case <-time.After(waitTimeout):
		require.FailNow(t, "the check never returned")
	}

	require.NoError(t, got.err)
	assert.Equal(t, headA, got.check.Head, "the caller gets the check it ran")
	assert.Equal(t, headB, h.storedHead(t, "item_files", id), "the older check did not replace the newer one")
}

func TestARefreshSupersedesOnlyOlderWritesOfWhatItStored(t *testing.T) {
	t.Parallel()

	order := &writeOrder{refreshed: map[int64]uint64{1: 5}, checked: map[int64]uint64{2: 5}}
	update, files := &itemUpdate{}, &filesRow{}

	tests := []struct {
		name  string
		write itemWrite
		want  bool
	}{
		{name: "an older refresh of an item it stored", write: itemWrite{id: 1, seq: 4, update: update}, want: true},
		{name: "a newer refresh of an item it stored", write: itemWrite{id: 1, seq: 6, update: update}},
		{name: "an older refresh of an item it did not store", write: itemWrite{id: 3, seq: 4, update: update}},
		{name: "an older check of files it checked", write: itemWrite{id: 2, seq: 4, files: files}, want: true},
		{name: "an older check of files it did not check", write: itemWrite{id: 1, seq: 4, files: files}},
		{name: "a newer check of files it checked", write: itemWrite{id: 2, seq: 6, files: files}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, order.supersedes(tt.write))
		})
	}

	assert.False(t, (&writeOrder{}).supersedes(itemWrite{id: 1, seq: 1, update: update}), "before any refresh")
}

func TestARefreshKeepsTheLastCheckOfEachItemStillListed(t *testing.T) {
	t.Parallel()

	var s Service

	s.order.checked = map[int64]uint64{1: 3, 2: 3}
	s.storedBy(5, &writer{saved: []savedEntry{{id: 1}, {id: 3}}, checked: map[int64]bool{3: true}})

	assert.Equal(t, map[int64]uint64{1: 5, 3: 5}, s.order.refreshed)
	assert.Equal(t, map[int64]uint64{1: 3, 3: 5}, s.order.checked,
		"#1 keeps the earlier refresh that checked it, #3 is checked now, and #2, no longer listed, is forgotten")
}

func TestRewrittenNamesTheItemsWhoseOwnRefreshIsReplayed(t *testing.T) {
	t.Parallel()

	got := rewritten([]itemWrite{{id: 1, update: &itemUpdate{}}, {id: 2, files: &filesRow{}}})
	assert.Equal(t, map[int64]bool{1: true}, got)
}
