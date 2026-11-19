package inbox

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
)

// watchStatus reads what the inbox last read of each routine search.
func (h *watchHarness) watchStatus(t *testing.T) WatchStatus {
	t.Helper()

	status, err := h.store().WatchStatus(t.Context())
	require.NoError(t, err)

	return status
}

func TestAWatchPassThatCannotBeStoredKeepsWhatTheLastOneStored(t *testing.T) {
	t.Parallel()

	// newBump is #8, a Dependabot pull request in other-repo the search returns new, whose
	// details don't come back, so its repository is the little the search read.
	newBump := func(h *watchHarness) {
		it := dependabot(8)
		it.RepoName = "other-repo"
		h.client.watches[depQuery] = answer(watched(pr(1, "OWNER", false), headA, "SUCCESS"),
			watched(dependabot(7), headA, "SUCCESS"), watched(it, headA, "SUCCESS"))
	}

	tests := []struct {
		name string

		// step serves what the failing pass reads, after any passes of its own, and event
		// and when say which of its writes the database refuses.
		step        func(t *testing.T, h *watchHarness)
		event, when string

		want string
	}{
		{
			name: "a watch row", step: func(_ *testing.T, h *watchHarness) { newBump(h) },
			event: "BEFORE INSERT ON item_watches", want: "save the watch of acme-org/example-repo#1",
		},
		{
			name: "a new watched item", step: func(_ *testing.T, h *watchHarness) { newBump(h) },
			event: "BEFORE INSERT ON items", when: "NEW.number = 8",
			want: "save the watched item acme-org/other-repo#8",
		},
		{
			name: "a new item's repository", step: func(_ *testing.T, h *watchHarness) { newBump(h) },
			event: "BEFORE INSERT ON repos", when: "NEW.name = 'other-repo'",
			want: "save the repository acme-org/other-repo",
		},
		{
			name: "a merged item's watch rows",
			step: func(t *testing.T, h *watchHarness) {
				h.exec(t, `UPDATE items SET state = 'MERGED' WHERE number = 7`)
			},
			event: "BEFORE DELETE ON item_watches",
			want:  "take acme-org/example-repo#7 out of the routines' searches",
		},
		{
			name: "a closed result's state",
			step: func(_ *testing.T, h *watchHarness) {
				closed := watched(dependabot(7), headA, "SUCCESS")
				closed.State, closed.ClosedAt = github.StateClosed, h.clock.Now()
				h.client.watches[depQuery] = answer(watched(pr(1, "OWNER", false), headA, "SUCCESS"), closed)
			},
			event: "BEFORE UPDATE OF state ON items", when: "NEW.state = 'CLOSED'",
			want: "save the state of acme-org/example-repo#7",
		},
		{
			name:  "a failed search's rows",
			step:  func(_ *testing.T, h *watchHarness) { delete(h.client.watches, depQuery) },
			event: "BEFORE UPDATE ON item_watches", when: "NEW.stale = 1",
			want: "keep the rows of a search that failed",
		},
		{
			name: "the items that left a search",
			step: func(_ *testing.T, h *watchHarness) {
				h.client.watches[depQuery] = answer(watched(pr(1, "OWNER", false), headA, "SUCCESS"))
			},
			event: "BEFORE DELETE ON item_watches", want: "forget the items that left a search",
		},
		{
			name: "a truncated search's tail",
			step: func(_ *testing.T, h *watchHarness) {
				h.client.watches[depQuery] = github.WatchResult{
					IssueCount: 150, Items: []github.WatchItem{watched(dependabot(7), headA, "SUCCESS")},
				}
			},
			event: "BEFORE UPDATE ON item_watches", when: "NEW.stale = 1",
			want: "keep a search's tail",
		},
		{
			name: "a truncated search's old tail",
			step: func(t *testing.T, h *watchHarness) {
				// A pass keeps #1 past the page, stale, and it stays out that long.
				h.client.watches[depQuery] = github.WatchResult{
					IssueCount: 150, Items: []github.WatchItem{watched(dependabot(7), headA, "SUCCESS")},
				}
				h.refresh(t)
				synctest.Wait()
				h.clock.Advance(staleTailFor)
			},
			event: "BEFORE DELETE ON item_watches", want: "forget a search's old tail",
		},
		{
			name: "the rows of a search no routine has",
			step: func(_ *testing.T, h *watchHarness) {
				h.source.set(Watch{Key: otherQuery, Query: otherQuery})
				h.client.watches[otherQuery] = answer()
			},
			event: "BEFORE DELETE ON item_watches", want: "forget the searches no routine has",
		},
		{
			name:  "the searches' state",
			step:  func(*testing.T, *watchHarness) {},
			event: "BEFORE INSERT ON prefs", when: "NEW.key = '" + prefWatches + "'",
			want: "save the searches' state",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				h := newWatchHarness(t)
				defer h.stop()

				h.startWatching(t)
				h.refresh(t)
				synctest.Wait()

				h.next()
				tc.step(t, h)

				rows, status, stored, told := h.watchRows(t), h.watchStatus(t), len(h.storedItems(t)), len(h.changed.all())

				refuse(t, h.harness, tc.event, tc.when)

				h.refresh(t) // the pass failing never fails the refresh
				synctest.Wait()

				assert.Contains(t, h.logs.String(), passJob+" failed")
				assert.Contains(t, h.logs.String(), tc.want)
				assert.Equal(t, rows, h.watchRows(t), "no row the pass meant to write or drop moved")
				assert.Equal(t, status, h.watchStatus(t), "nor the searches' state")
				assert.Len(t, h.storedItems(t), stored, "nor any item it meant to store")
				assert.Len(t, h.changed.all(), told, "and nothing is told as changed")

				// Once the database takes the write again, the next pass stores.
				h.exec(t, "DROP TRIGGER refused_write")
				h.next()
				h.refresh(t)
				synctest.Wait()

				assert.NotEqual(t, status.At, h.watchStatus(t).At)
			})
		})
	}
}

func TestAnUnreadableSearchesStateIsStartedAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		require.True(t, h.watchStatus(t).Searches[depQuery].Answered)

		for _, raw := range []string{"{", `{"at": "2026-01-10T12:00:00Z"}`} {
			h.setPrefs(t, map[string]string{prefWatches: raw})

			assert.Equal(t, WatchStatus{Searches: map[string]SearchStatus{}}, h.watchStatus(t),
				"%s: a state that can't be read is none", raw)
		}

		h.client.mu.Lock()
		h.client.watchCalls = nil
		h.client.mu.Unlock()

		h.next()
		h.refresh(t)
		synctest.Wait()

		h.client.mu.Lock()
		calls := h.client.watchCalls
		h.client.mu.Unlock()

		require.Len(t, calls, 1)
		assert.Equal(t, github.NewSearch, calls[0][0].Last, "the search is sent as new, with no last count to pack by")
		assert.True(t, h.watchStatus(t).Searches[depQuery].Answered, "and the pass writes the state again")
	})
}

func TestAPassLeavesOutArchivedResultsAndKeepsAClose(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		id := h.storedItems(t)[1].ID

		archived := dependabot(9)
		archived.RepoName = "archived-repo"
		archivedResult := watched(archived, headA, "SUCCESS")
		archivedResult.Repo.IsArchived = true

		closed := watched(dependabot(7), headA, "SUCCESS")
		closed.State, closed.ClosedAt = github.StateClosed, h.clock.Now()

		// The Dependabot search still has #7 open; the other search, a moment apart, found
		// it closed.
		h.next()
		h.source.set(Watch{Key: depQuery, Query: depQuery, Follow: true}, Watch{Key: otherQuery, Query: otherQuery})
		h.client.watches[depQuery] = answer(watched(pr(1, "OWNER", false), headA, "SUCCESS"),
			watched(dependabot(7), headA, "SUCCESS"), archivedResult)
		h.client.watches[otherQuery] = answer(closed)
		h.client.details[nodeID(9)] = watchedPRDetails(headA, "SUCCESS")

		h.refresh(t)
		synctest.Wait()

		last, ok := h.svc.lastWatchPass()
		require.True(t, ok)
		assert.Equal(t, 1, last.Items, "only #1 is an open result: #9's repository is archived, and #7 closed")
		assert.Equal(t, github.StateClosed, h.itemState(t, id), "the close is the one kept")
		assert.Len(t, h.storedItems(t), 2, "the archived repository's result is never stored")
		assert.Equal(t, []int{1}, h.watchesOf(t), "and #7 left the searches")
	})
}

func TestAPassStoresAgainAnItemDeletedWhileItReadDetails(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		// #7 has a new head, so the pass reads its details, and waits on GitHub there.
		h.next()
		h.client.watches[depQuery] = answer(watched(pr(1, "OWNER", false), headA, "SUCCESS"),
			watched(dependabot(7), headB, "PENDING"))
		h.client.details[nodeID(7)] = watchedPRDetails(headB, "PENDING")
		h.client.watchDetailsGate = newGate(t)

		h.refresh(t)
		h.client.watchDetailsGate.waitArrived(t)

		// Meanwhile #7 is deleted, as the prune of old items deletes one.
		h.exec(t, `DELETE FROM items WHERE number = 7`)

		told := len(h.changed.all())

		h.client.watchDetailsGate.release()
		synctest.Wait()

		require.Len(t, h.storedItems(t), 2, "the pass stores it again, as new")
		assert.Equal(t, []int{1, 7}, h.watchesOf(t))

		again := h.storedItems(t)[1]
		assert.Equal(t, 7, again.Number)
		assert.Equal(t, []int64{again.ID}, h.changed.all()[told:], "and tells it as changed")

		rows, err := h.store().FactsRows(t.Context(), []int64{again.ID})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.NotNil(t, rows[0].Details)
		assert.Equal(t, headB, rows[0].Details.HeadOID, "with the details it read")
	})
}

func TestARefreshStillFetchingTakesATestsItemsAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		id := h.storedItems(t)[1].ID
		c := h.gatedWatching(t)
		c.inboxGate = newGate(t)

		// A whole refresh goes out, and will list #7 with what GitHub said of it then.
		h.next()

		listed := dependabot(7)
		listed.Title = "Bump as the slower refresh listed it"
		h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, []github.Item{listed}, nil)
		h.client.details[nodeID(7)] = watchedPRDetails(headA, "SUCCESS")
		refresh := async(func() (Snapshot, error) { return h.svc.Refresh(t.Context()) })

		c.inboxGate.waitArrived(t)

		// A routine's test, sent after it, reads #7 at a new head, with a new title.
		h.clock.Advance(time.Second)

		searched := dependabot(7)
		searched.Title = "Bump as the test read it"

		h.client.mu.Lock()
		h.client.watches[otherQuery] = answer(watched(searched, headB, "SUCCESS"))
		h.client.details[nodeID(7)] = watchedPRDetails(headB, "SUCCESS")
		h.client.mu.Unlock()

		got, _, err := NewRoutines(h.svc).SearchNow(t.Context(), []string{otherQuery})
		require.NoError(t, err)
		require.Equal(t, []int64{id}, got[otherQuery].IDs)

		c.inboxGate.release()
		require.NoError(t, await(t, refresh).err)

		var title, head string

		require.NoError(t, h.pool.QueryRowContext(t.Context(), `SELECT i.title, d.head_oid FROM items i
JOIN item_details d ON d.item_id = i.id WHERE i.id = ?`, id).Scan(&title, &head))
		assert.Equal(t, "Bump as the test read it", title, "the refresh takes the test's newer card again, over its own")
		assert.Equal(t, headB, head, "and its details")

		snap, err := h.svc.Snapshot(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []int{7}, numbers(snap.Yours), "the refresh's lists are its own")
	})
}
