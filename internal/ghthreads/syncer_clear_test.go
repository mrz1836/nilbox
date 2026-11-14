package ghthreads

import (
	"context"
	"database/sql"
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
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

func TestSyncerNudgeRunsSoon(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		p := newFakePasser()
		s := newSyncer(p, nil, nil)

		stop := runSyncer(t, s)
		defer stop()

		synctest.Wait()
		require.Len(t, p.ran(), 1, "the first pass")

		ref := item.Ref{Repo: repo.Name{Owner: owner, Repo: exampleRepo}, Number: 1, Kind: item.KindPR}
		begun := time.Now()

		p.nudges.add([]item.Ref{ref}, nil, false)
		synctest.Wait()

		runs := p.ran()
		require.Len(t, runs, 2, "a nudge runs at once, not at the interval")
		assert.True(t, runs[1].req.nudged)
		assert.Equal(t, []item.Ref{ref}, runs[1].req.refs)
		assert.True(t, runs[1].at.Equal(begun), "no time passed")

		s.RulesChanged()
		synctest.Wait()

		runs = p.ran()
		require.Len(t, runs, 3)
		assert.True(t, runs[2].req.all, "the rules changed: every thread is decided again")

		time.Sleep(DefaultInterval + time.Second)
		synctest.Wait()
		assert.Equal(t, 2, p.count(), "a nudged run never stands in for the timed pass")
	})
}

func TestSyncerSenderTimerWakesAtSendAfter(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		p := newFakePasser()

		stop := runSyncer(t, newSyncer(p, nil, nil))
		defer stop()

		synctest.Wait()

		due := time.Now().Add(MinHold)
		p.sendBy(due)

		time.Sleep(MinHold - time.Second)
		synctest.Wait()
		assert.Len(t, p.ran(), 1, "not before send_after")

		time.Sleep(time.Second)
		synctest.Wait()

		runs := p.ran()
		require.Len(t, runs, 2, "the hold's end wakes the sender, not the five-minute wait")
		assert.True(t, runs[1].req.send)
		assert.True(t, runs[1].at.Equal(due))

		time.Sleep(time.Minute)
		synctest.Wait()
		assert.Len(t, p.ran(), 2, "nothing more is due")
	})
}

// clearingClient is GitHub with count unread pull request threads (one when count is 0),
// from firstID on, each about its own pull request from 1 on, updated at updated, which
// records when a thread is marked done, and panics as told.
type clearingClient struct {
	*gatedClient

	updated time.Time
	count   int

	// panicList panics every listing, and panicDone every thread marked done.
	panicList bool
	panicDone bool

	mu        sync.Mutex
	done      []time.Time
	doneIDs   []int64
	listings  int
	doneTries int
}

func (c *clearingClient) ListThreads(context.Context, github.Token, github.ThreadQuery) (*github.ThreadPage, error) {
	c.mu.Lock()
	c.listings++
	c.mu.Unlock()

	if c.panicList {
		panic("a broken listing")
	}

	name := repo.Name{Owner: owner, Repo: exampleRepo}
	threads := make([]github.Thread, 0, max(c.count, 1))

	for i := range max(c.count, 1) {
		threads = append(threads, github.Thread{
			ID: firstID + int64(i), Unread: true, Reason: "review_requested", UpdatedAt: c.updated,
			SubjectType: "PullRequest", Title: "Retry the sync worker", Repo: name,
			Ref: item.Ref{Repo: name, Number: i + 1, Kind: item.KindPR},
		})
	}

	return &github.ThreadPage{Threads: threads}, nil
}

func (c *clearingClient) MarkThreadDone(_ context.Context, _ github.Token, id int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.doneTries++

	if c.panicDone {
		panic("a broken clear")
	}

	c.done = append(c.done, time.Now())
	c.doneIDs = append(c.doneIDs, id)

	return nil
}

// marked returns when a thread was marked done.
func (c *clearingClient) marked() []time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return slices.Clone(c.done)
}

// tries returns how many listings were tried, and how many threads were tried to mark done.
func (c *clearingClient) tries() (listings, done int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.listings, c.doneTries
}

func TestSyncerSendsAHeldClearWhenItsHoldEnds(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)

	synctest.Test(t, func(t *testing.T) {
		client := &clearingClient{gatedClient: newGatedClient(0), updated: time.Now().UTC()}
		rules := &fakeRules{}
		rules.change(func(r *threadrules.Rules) { r.Auto = true })

		svc := NewService(Config{
			Pool: db, Tokens: &fakeTokens{value: "test-token-not-real"}, Client: client, Rules: rules,
			Holds: fakeHolds{seconds: 30},
		})

		stop := runSyncer(t, NewSyncer(svc, nil, nil))
		defer stop()

		synctest.Wait()

		dismissed := time.Now()
		NewSettler(svc).Settle(t.Context(), []Outcome{outcome(OutcomeDismissed, item.KindPR, 1, dismissed)})
		synctest.Wait()

		snap, err := svc.Snapshot(t.Context())
		require.NoError(t, err)
		require.Len(t, snap.Queued, 1, "the nudge queued it at once")
		assert.Empty(t, client.marked(), "held for the Undo")

		time.Sleep(33*time.Second - time.Millisecond)
		synctest.Wait()
		assert.Empty(t, client.marked(), "toasts of 30 seconds hold it 33")

		time.Sleep(time.Millisecond)
		synctest.Wait()

		marked := client.marked()
		require.Len(t, marked, 1, "sent as the hold ends")
		assert.True(t, marked[0].Equal(dismissed.Add(33*time.Second)))
	})
}

// clearingService returns a Service over db and client, with clearing on when auto, and
// logs, as the desktop shell builds it.
func clearingService(db *sql.DB, client Client, auto bool, events event.Sink, logs *testutil.Logs) *Service {
	rules := &fakeRules{}
	rules.change(func(r *threadrules.Rules) { r.Auto = auto })

	return NewService(Config{
		Pool: db, Tokens: &fakeTokens{value: "test-token-not-real"}, Client: client, Rules: rules, Events: events,
		Logger: logs.Logger(),
	})
}

func TestSyncerNeverSpinsOnAPanickingRun(t *testing.T) {
	t.Parallel()

	t.Run("a pass", func(t *testing.T) {
		t.Parallel()

		db := testutil.DB(t)

		synctest.Test(t, func(t *testing.T) {
			client := &clearingClient{gatedClient: newGatedClient(0), updated: time.Now().UTC(), panicList: true}
			logs := &testutil.Logs{}

			stop := runSyncer(t, NewSyncer(clearingService(db, client, true, nil, logs), nil, nil))
			defer stop()

			time.Sleep(30*time.Minute + time.Second)
			synctest.Wait()

			listings, _ := client.tries()
			assert.Equal(t, 7, listings, "one every interval, a panic counting as a pass that ended")
		})
	})

	t.Run("a send", func(t *testing.T) {
		t.Parallel()

		db := testutil.DB(t)

		synctest.Test(t, func(t *testing.T) {
			client := &clearingClient{gatedClient: newGatedClient(0), updated: time.Now().UTC(), panicDone: true}
			svc := clearingService(db, client, true, nil, &testutil.Logs{})

			stop := runSyncer(t, NewSyncer(svc, nil, nil))
			defer stop()

			synctest.Wait()
			NewSettler(svc).Settle(t.Context(), []Outcome{outcome(OutcomeDismissed, item.KindPR, 1, time.Now())})

			time.Sleep(10 * time.Minute)
			synctest.Wait()

			_, tries := client.tries()
			assert.GreaterOrEqual(t, tries, 5, "tried again, a minute apart")
			assert.LessOrEqual(t, tries, 15, "never one right after another that panicked")
		})
	})
}

// gateSink holds the first event emitted once block is set until gate closes, telling
// entered when it does.
type gateSink struct {
	block   atomic.Bool
	entered chan struct{}
	gate    chan struct{}
}

func (g *gateSink) Emit(event.Event) {
	if g.block.CompareAndSwap(true, false) {
		g.entered <- struct{}{}

		<-g.gate
	}
}

func TestSyncerKeepsAClickMadeAsARunEnds(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)

	synctest.Test(t, func(t *testing.T) {
		client := &clearingClient{gatedClient: newGatedClient(0), updated: time.Now().UTC(), count: 2}
		sink := &gateSink{entered: make(chan struct{}), gate: make(chan struct{})}
		svc := clearingService(db, client, false, sink, &testutil.Logs{})

		stop := runSyncer(t, NewSyncer(svc, nil, nil))
		defer stop()

		synctest.Wait()

		// The user clears one by hand; the run that sends it stops, as it ends, at telling
		// the window.
		sink.block.Store(true)

		_, err := svc.Clear(t.Context(), []int64{firstID})
		require.NoError(t, err)

		<-sink.entered

		// Then the other, while that run is still ending.
		_, err = svc.Clear(t.Context(), []int64{firstID + 1})
		require.NoError(t, err)

		begun := time.Now()

		close(sink.gate)
		synctest.Wait()

		marked := client.marked()
		require.Len(t, marked, 2, "the second click is sent at once, not at the next pass")
		assert.True(t, marked[1].Equal(begun), "no time passed")
		assert.Equal(t, []int64{firstID, firstID + 1}, client.doneIDs)
	})
}
