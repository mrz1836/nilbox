package inbox

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/snooze"
)

// fakeSink records the arrivals it is told about, one call at a time.
type fakeSink struct {
	mu    sync.Mutex
	calls [][]Arrival
}

func (s *fakeSink) Arrived(_ context.Context, arrivals []Arrival) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls = append(s.calls, arrivals)
}

// last returns the arrivals of the last call, or nil when there was none.
func (s *fakeSink) last() []Arrival {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.calls) == 0 {
		return nil
	}

	return s.calls[len(s.calls)-1]
}

// arrivalHarness is a harness whose service tells a fakeSink about arrivals.
func arrivalHarness(t *testing.T) (*harness, *fakeSink) {
	t.Helper()

	h := newHarness(t)
	sink := &fakeSink{}
	h.svc = NewService(Config{
		Pool: h.pool, Tokens: h.tokens, Client: h.client, Clock: h.clock, Events: h.events,
		Arrivals: sink,
	})

	return h, sink
}

// byOther is an item someone other than the signed-in user wrote.
func byOther(it github.Item) github.Item {
	it.AuthorLogin = "octo-other"

	return it
}

// kindsAndNumbers lists arrivals as "kind #number", in order.
func kindsAndNumbers(arrivals []Arrival) []string {
	out := make([]string, 0, len(arrivals))
	for _, a := range arrivals {
		out = append(out, a.Kind+" #"+strconv.Itoa(a.Number))
	}

	return out
}

// tick moves the clock on an hour.
func (h *harness) tick() {
	h.clock.Advance(time.Hour)
}

func TestTheFirstRefreshTellsNoArrivals(t *testing.T) {
	h, sink := arrivalHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "MEMBER", false)}, nil, []github.Item{byOther(issue("example-repo", 2, "MEMBER"))})

	h.refresh(t)

	assert.Empty(t, sink.calls, "everything is new on the first refresh, so nothing is told")
}

func TestNewReviewRequestsAndAssignmentsArrive(t *testing.T) {
	h, sink := arrivalHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "MEMBER", false)}, nil, nil)
	h.refresh(t)

	h.tick()
	h.client.inbox = fetched(
		[]github.Item{pr(1, "MEMBER", false), byOther(pr(3, "MEMBER", false))},
		nil,
		[]github.Item{byOther(issue("example-repo", 4, "MEMBER"))},
	)
	h.refresh(t)

	got := sink.last()
	require.Len(t, sink.calls, 1)
	assert.Equal(t, []string{"review_requested #3", "assigned #4"}, kindsAndNumbers(got))

	first := got[0]
	assert.Equal(t, item.KindPR, first.ItemKind)
	assert.Equal(t, "acme-org", first.RepoOwner)
	assert.Equal(t, "example-repo", first.RepoName)
	assert.Equal(t, "Item 3", first.Title)
	assert.Equal(t, "https://github.com/acme-org/example-repo/pull/3", first.URL)
	assert.Equal(t, "octo-other", first.Author)
	assert.NotZero(t, first.ItemID)
	assert.Equal(t, item.KindIssue, got[1].ItemKind)

	// The same lists again tell nothing.
	h.tick()
	h.refresh(t)
	assert.Len(t, sink.calls, 1)
}

func TestAReviewRequestedAgainArrivesAgain(t *testing.T) {
	h, sink := arrivalHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "MEMBER", false)}, nil, nil)
	h.refresh(t)

	// Reviewed: it leaves the list.
	h.tick()
	h.client.inbox = fetched(nil, nil, nil)
	h.refresh(t)
	assert.Empty(t, sink.calls)

	// Asked again.
	h.tick()
	h.client.inbox = fetched([]github.Item{pr(1, "MEMBER", false)}, nil, nil)
	h.refresh(t)
	assert.Equal(t, []string{"review_requested #1"}, kindsAndNumbers(sink.last()))
}

func TestAnItemArrivesOnceForItsFirstReason(t *testing.T) {
	h, sink := arrivalHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	h.refresh(t)

	both := byOther(pr(5, "MEMBER", false))

	h.tick()
	h.client.inbox = fetched([]github.Item{both}, nil, []github.Item{both})
	h.refresh(t)

	assert.Equal(t, []string{"review_requested #5"}, kindsAndNumbers(sink.last()))
}

func TestAnAssignmentOnYourOwnItemDoesNotArrive(t *testing.T) {
	h, sink := arrivalHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	h.refresh(t)

	h.tick()
	h.client.inbox = fetched(nil, nil, []github.Item{issue("example-repo", 6, "OWNER")})
	h.refresh(t)

	assert.Empty(t, sink.calls, "the signed-in user wrote it")
}

func TestSetAsideItemsDoNotArrive(t *testing.T) {
	h, sink := arrivalHarness(t)
	one, two := byOther(pr(1, "MEMBER", false)), byOther(pr(2, "MEMBER", false))
	h.client.inbox = fetched(nil, nil, []github.Item{one, two})
	snap := h.refresh(t)

	_, err := h.svc.Dismiss(t.Context(), itemByNumber(t, snap.Assigned, 1).ID)
	require.NoError(t, err)
	_, err = h.svc.Snooze(t.Context(), itemByNumber(t, snap.Assigned, 2).ID, 3)
	require.NoError(t, err)

	// All three newly ask for a review; 1 is dismissed and 2 still snoozed.
	h.tick()
	h.client.inbox = fetched([]github.Item{one, two, byOther(pr(7, "MEMBER", false))}, nil, []github.Item{one, two})
	h.refresh(t)

	assert.Equal(t, []string{"review_requested #7"}, kindsAndNumbers(sink.last()))
}

func TestAnItemBackFromASnoozeArrives(t *testing.T) {
	h, sink := arrivalHarness(t)
	one, two, three := byOther(pr(1, "MEMBER", false)), byOther(pr(2, "MEMBER", false)),
		byOther(pr(3, "MEMBER", false))
	h.client.inbox = fetched(nil, nil, []github.Item{one, two, three})
	snap := h.refresh(t)

	_, err := h.svc.Dismiss(t.Context(), itemByNumber(t, snap.Assigned, 1).ID)
	require.NoError(t, err)
	_, err = h.svc.Snooze(t.Context(), itemByNumber(t, snap.Assigned, 3).ID, 1)
	require.NoError(t, err)

	// Two days on, 3's snooze has run its course; all three newly ask for a review, and
	// only the dismissed 1 is still set aside.
	h.clock.Advance(48 * time.Hour)
	h.client.inbox = fetched([]github.Item{one, two, three}, nil, []github.Item{one, two, three})
	h.refresh(t)

	assert.Equal(t, []string{"review_requested #2", "review_requested #3"}, kindsAndNumbers(sink.last()))
}

func TestASnoozeEndedEarlyArrives(t *testing.T) {
	h, id, details := wakeHarness(t)
	sink := &fakeSink{}
	h.svc = NewService(Config{
		Pool: h.pool, Tokens: h.tokens, Client: h.client, Clock: h.clock, Events: h.events,
		Arrivals: sink,
	})

	_, err := h.svc.Snooze(t.Context(), id, 3)
	require.NoError(t, err)

	h.later(t, details)
	assert.Empty(t, sink.calls, "nothing new")

	details.Commits = 5
	details.HeadOID = "2222222222222222222222222222222222222222"
	h.later(t, details)

	got := sink.last()
	require.Len(t, got, 1)
	assert.Equal(t, ArrivalWoke, got[0].Kind)
	assert.Equal(t, id, got[0].ItemID)
	assert.Equal(t, snooze.Reasons{Commits: 2}, got[0].Woke)
}

func TestATruncatedListNeedsTheItemToHaveMoved(t *testing.T) {
	h, sink := arrivalHarness(t)
	h.clock.Set(time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC))
	h.client.inbox = fetched([]github.Item{pr(1, "MEMBER", false)}, nil, nil)
	h.client.inbox.ReviewRequested.IssueCount = 150
	h.refresh(t)

	// Another item fell out of the truncated list, so an old one slid in: not new.
	old := byOther(pr(2, "MEMBER", false))
	moved := byOther(pr(3, "MEMBER", false))
	moved.UpdatedAt = h.clock.Now().Add(30 * time.Minute)

	h.tick()
	h.client.inbox = fetched([]github.Item{pr(1, "MEMBER", false), old, moved}, nil, nil)
	h.client.inbox.ReviewRequested.IssueCount = 150
	h.refresh(t)

	assert.Equal(t, []string{"review_requested #3"}, kindsAndNumbers(sink.last()))
}

func TestAnotherAccountTellsNoArrivals(t *testing.T) {
	h, sink := arrivalHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	h.refresh(t)

	h.tick()
	h.client.inbox = fetched([]github.Item{byOther(pr(1, "MEMBER", false))}, nil, nil)
	h.client.inbox.Viewer.Login = "octo-second"
	h.refresh(t)

	assert.Empty(t, sink.calls, "the lists are new to this account")
}

func TestAFailedRefreshTellsNoArrivals(t *testing.T) {
	h, sink := arrivalHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	h.refresh(t)

	h.tick()
	h.client.inbox = fetched([]github.Item{byOther(pr(1, "MEMBER", false))}, nil, nil)
	h.client.limitsErr = errFakeNetwork

	_, err := h.svc.Refresh(t.Context())
	require.Error(t, err)
	assert.Empty(t, sink.calls)
}

func TestTheSinkIsToldOutsideTheLock(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched(nil, nil, nil)

	sink := &lockingSink{}
	h.svc = NewService(Config{
		Pool: h.pool, Tokens: h.tokens, Client: h.client, Clock: h.clock, Arrivals: sink,
	})
	sink.svc = h.svc
	h.refresh(t)

	h.tick()
	h.client.inbox = fetched([]github.Item{byOther(pr(1, "MEMBER", false))}, nil, nil)

	done := make(chan struct{})

	go func() {
		defer close(done)

		_, _ = h.svc.Refresh(context.Background())
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the sink was told while the refresh held the lock")
	}

	assert.True(t, sink.told)
}

// lockingSink takes the inbox's lock when told, which deadlocks if the refresh holds it.
type lockingSink struct {
	svc  *Service
	told bool
}

func (s *lockingSink) Arrived(context.Context, []Arrival) {
	s.svc.order.mu.Lock()
	s.told = true
	s.svc.order.mu.Unlock()
}
