package inbox

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/progress"
)

// errInboxDown fails the inbox query, as a network outage would.
var errInboxDown = errors.New("the network is down")

// progressOf returns the refreshes' progress events heard, from the nth on.
func progressOf(events *inboxEvents, from int) []event.InboxProgress {
	var out []event.InboxProgress

	for _, e := range events.progress.All()[from:] {
		if p, ok := e.(event.InboxProgress); ok {
			out = append(out, p)
		}
	}

	return out
}

// phases returns the phases of told, each once, in the order they came.
func phases(told []event.InboxProgress) []event.InboxPhase {
	var out []event.InboxPhase

	for _, p := range told {
		if len(out) == 0 || out[len(out)-1] != p.Phase {
			out = append(out, p.Phase)
		}
	}

	return out
}

// lastOf returns the last of told in phase.
func lastOf(t *testing.T, told []event.InboxProgress, phase event.InboxPhase) event.InboxProgress {
	t.Helper()

	for i := len(told) - 1; i >= 0; i-- {
		if told[i].Phase == phase {
			return told[i]
		}
	}

	require.Failf(t, "no progress in the phase", "%s", phase)

	return event.InboxProgress{}
}

// lastProgressBeforeTheLastUpdate reports whether every progress event in all came before
// the last InboxUpdated.
func lastProgressBeforeTheLastUpdate(events *inboxEvents) bool {
	lastProgress, lastUpdate := -1, -1

	for i, e := range events.all.All() {
		switch e.(type) {
		case event.InboxProgress:
			lastProgress = i
		case event.InboxUpdated:
			lastUpdate = i
		}
	}

	return lastUpdate >= 0 && lastProgress < lastUpdate
}

func TestARefreshTellsHowFarItHasGot(t *testing.T) {
	t.Parallel()

	// An outside contributor's pull request is listed; the next refresh brings another,
	// and an issue, both new. Both pull requests' files are checked.
	listed, fresh := from(1, "outsider", "NONE", false), from(2, "forker", "CONTRIBUTOR", true)
	h := filesHarness(t, listed, fresh)
	h.svc.progressEvery = 0
	h.client.inbox = fetched([]github.Item{listed}, nil, nil)
	h.refresh(t)

	h.client.inbox = fetched([]github.Item{listed, fresh}, nil, []github.Item{byOther(issue("example-repo", 3, "MEMBER"))})
	h.client.details[nodeID(3)] = github.ItemDetails{}
	before := len(h.events.progress.All())

	h.refresh(t)

	told := progressOf(h.events, before)
	require.NotEmpty(t, told)
	assert.Equal(t, event.InboxProgress{Phase: event.InboxSearching}, told[0], "the search is told at once, counting nothing")
	assert.Equal(t, []event.InboxPhase{event.InboxSearching, event.InboxReadingDetails, event.InboxCheckingFiles},
		phases(told), "in order")
	assert.Equal(t, event.InboxProgress{Phase: event.InboxReadingDetails, Done: 2, Total: 2},
		lastOf(t, told, event.InboxReadingDetails), "only the items new since the last refresh are counted")
	assert.Equal(t, event.InboxProgress{Phase: event.InboxCheckingFiles, Done: 1, Total: 1},
		lastOf(t, told, event.InboxCheckingFiles), "only the pull request whose files are due")

	for _, p := range told {
		assert.LessOrEqual(t, p.Done, p.Total, "never past the total: %+v", p)
	}

	assert.True(t, lastProgressBeforeTheLastUpdate(h.events), "the update that ends the refresh comes last")
}

func TestARefreshWithNothingNewToReadTellsTheSearchAlone(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.svc.progressEvery = 0
	h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)
	before := len(h.events.progress.All())

	h.refresh(t)

	assert.Equal(t, []event.InboxPhase{event.InboxSearching}, phases(progressOf(h.events, before)),
		"the listed item's details come with the search, and a member's files aren't checked")
}

func TestTheSnapshotSaysARefreshIsUnderWay(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
	c := h.gated()
	c.inboxGate = newGate(t)

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.False(t, snap.Refreshing)

	refreshed := async(func() (Snapshot, error) { return h.svc.Refresh(t.Context()) })

	c.inboxGate.waitArrived(t)

	snap, err = h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.True(t, snap.Refreshing, "whoever started it")
	assert.Zero(t, h.events.Count(), "a refresh starting tells no update, so Autopilot's engine doesn't wake")
	assert.Equal(t, []event.InboxProgress{{Phase: event.InboxSearching}}, progressOf(h.events, 0),
		"its progress tells the window it started")

	c.inboxGate.release()

	got := await(t, refreshed)
	require.NoError(t, got.err)
	assert.False(t, got.snap.Refreshing, "the snapshot the ending update brings says it ended")

	snap, err = h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.False(t, snap.Refreshing)
	assert.True(t, lastProgressBeforeTheLastUpdate(h.events))
}

func TestAFailedRefreshStopsSayingItRefreshes(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	h.client.inboxErr = errInboxDown

	snap, err := h.svc.Refresh(t.Context())
	require.Error(t, err)
	assert.NotEmpty(t, snap.LastError)
	assert.False(t, snap.Refreshing, "the snapshot the failure's update brings says it ended")

	again, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.False(t, again.Refreshing)
	assert.Equal(t, 1, h.events.Count(), "the failure is told")
	assert.True(t, lastProgressBeforeTheLastUpdate(h.events))
}

func TestACanceledRefreshStopsSayingItRefreshes(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		c := h.gated()
		c.inboxGate = newGate(t)

		ctx, cancel := context.WithCancel(t.Context())
		refreshed := async(func() (Snapshot, error) { return h.svc.Refresh(ctx) })

		c.inboxGate.waitArrived(t)
		cancel()

		got := await(t, refreshed)
		require.ErrorIs(t, got.err, context.Canceled)

		// The caller stops waiting as soon as its ctx ends, while the refresh still unwinds:
		// what it does as it ends is read once it has.
		synctest.Wait()

		snap, err := h.svc.Snapshot(t.Context())
		require.NoError(t, err)
		assert.False(t, snap.Refreshing)
		assert.Empty(t, snap.LastError, "being cut short is no failure")
		assert.Equal(t, 1, h.events.Count(), "the window, told it started, is told it ended, so it reads it again")
		assert.True(t, lastProgressBeforeTheLastUpdate(h.events))
	})
}

// slowFiles is a fakeClient whose file checks each take a while.
type slowFiles struct {
	*fakeClient

	each time.Duration
}

func (c *slowFiles) FetchChangedFiles(
	ctx context.Context, tok github.Token, ref item.Ref, head string,
) (*github.ChangedFiles, error) {
	time.Sleep(c.each)

	return c.fakeClient.FetchChangedFiles(ctx, tok, ref, head)
}

func TestARefreshsProgressComesAFewTimesASecond(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		items := make([]github.Item, 0, maxFileChecks)
		for n := 1; n <= maxFileChecks; n++ {
			items = append(items, from(n, fmt.Sprintf("outsider-%d", n), "NONE", false))
		}

		h := filesHarness(t, items...)
		h.svc = NewService(Config{
			Pool: h.pool, Tokens: h.tokens, Client: &slowFiles{fakeClient: h.client, each: 100 * time.Millisecond},
			Clock: h.clock, Events: h.events,
		})

		start := time.Now()

		h.refresh(t)

		took := time.Since(start)

		// Whatever was held back when it ended is never told after it.
		time.Sleep(2 * progress.Every)
		synctest.Wait()

		files := 0

		for _, p := range progressOf(h.events, 0) {
			if p.Phase == event.InboxCheckingFiles {
				files++
			}
		}

		assert.Equal(t, maxFileChecks/fileChecksParallel*100*time.Millisecond, took, "the checks ran four at a time")
		assert.Positive(t, files)
		assert.LessOrEqual(t, files, 1+int(took/progress.Every)+1, "a few times a second, not once a check")
		assert.True(t, lastProgressBeforeTheLastUpdate(h.events), "nothing held back is told after the update")
	})
}
