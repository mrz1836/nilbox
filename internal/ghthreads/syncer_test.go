package ghthreads

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// errFakeBroken is a pass that failed.
var errFakeBroken = errors.New("the fake pass broke")

// fakePasser records when each pass ran, and when the last ended, on the bubble's clock.
type fakePasser struct {
	mu     sync.Mutex
	passes []time.Time
	ended  time.Time
	poll   time.Duration

	// fail fails every pass, and panic panics on the first.
	fail  bool
	panic bool
}

func (p *fakePasser) runPass(context.Context, passRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.passes = append(p.passes, time.Now())
	p.ended = time.Now()

	if p.panic && len(p.passes) == 1 {
		panic("a broken pass")
	}

	if p.fail {
		return errFakeBroken
	}

	return nil
}

func (p *fakePasser) pollInterval() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.poll
}

func (p *fakePasser) lastEnded() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.ended
}

// count returns how many passes ran.
func (p *fakePasser) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.passes)
}

// endedNow records a pass someone else ran, as CheckNow does.
func (p *fakePasser) endedNow() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.ended = time.Now()
}

// runSyncer runs s until the returned stop is called, which waits for Run to return.
func runSyncer(t *testing.T, s *Syncer) (stop func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		s.Run(ctx)
	}()

	return func() {
		cancel()
		<-done
	}
}

func TestNewSyncerDefaults(t *testing.T) {
	t.Parallel()

	s := NewSyncer(&Service{}, nil, nil)

	assert.Equal(t, DefaultInterval, s.every(t.Context()))
	assert.Equal(t, 5*time.Minute, DefaultInterval)
	assert.NotNil(t, s.logger)
}

func TestSyncerPassesAtOnceThenEveryInterval(t *testing.T) {
	t.Parallel()

	// The bubble's clock moves only when every goroutine in it is blocked, so the real
	// interval takes no real time and the count of passes is exact.
	synctest.Test(t, func(t *testing.T) {
		p := &fakePasser{}
		logs := &testutil.Logs{}
		p.fail = true
		stop := runSyncer(t, newSyncer(p, nil, logs.Logger()))

		synctest.Wait()
		assert.Equal(t, 1, p.count(), "a pass at once")

		time.Sleep(2*DefaultInterval + time.Second)
		synctest.Wait()
		assert.Equal(t, 3, p.count(), "then one every interval, after a failure too")

		stop()

		assert.Contains(t, logs.String(), "GitHub notifications pass failed")
		assert.Contains(t, logs.String(), errFakeBroken.Error())
	})
}

func TestSyncerHonorsPollInterval(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		p := &fakePasser{poll: 10 * time.Minute}

		stop := runSyncer(t, newSyncer(p, func(context.Context) time.Duration { return 2 * time.Minute }, nil))
		defer stop()

		synctest.Wait()
		assert.Equal(t, 1, p.count())

		time.Sleep(9 * time.Minute)
		synctest.Wait()
		assert.Equal(t, 1, p.count(), "set to two minutes, but GitHub asks for ten")

		time.Sleep(time.Minute + time.Second)
		synctest.Wait()
		assert.Equal(t, 2, p.count())
	})
}

func TestSyncerWakeAppliesNewInterval(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var every atomic.Int64

		every.Store(int64(30 * time.Minute))

		p := &fakePasser{}
		s := newSyncer(p, func(context.Context) time.Duration { return time.Duration(every.Load()) }, nil)

		stop := runSyncer(t, s)
		defer stop()

		synctest.Wait()
		assert.Equal(t, 1, p.count())

		time.Sleep(5 * time.Minute)
		synctest.Wait()
		assert.Equal(t, 1, p.count(), "not yet: it is set to half an hour")

		every.Store(int64(2 * time.Minute))
		s.Wake()
		synctest.Wait()
		assert.Equal(t, 2, p.count(), "set shorter, a pass already due runs at once")

		s.Wake()
		s.Wake() // a second wake while one waits is dropped, never blocks
		synctest.Wait()
		assert.Equal(t, 2, p.count(), "a wake with nothing due runs nothing")

		time.Sleep(2*time.Minute + time.Second)
		synctest.Wait()
		assert.Equal(t, 3, p.count(), "and the new interval holds")
	})
}

func TestSyncerWaitsFromTheLastPassAnyoneRan(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		p := &fakePasser{}

		stop := runSyncer(t, newSyncer(p, nil, nil))
		defer stop()

		synctest.Wait()

		time.Sleep(3 * time.Minute)
		p.endedNow() // the user's Check again

		time.Sleep(2*time.Minute + time.Second)
		synctest.Wait()
		assert.Equal(t, 1, p.count(), "five minutes after the Syncer's own pass, but two after the user's")

		time.Sleep(3 * time.Minute)
		synctest.Wait()
		assert.Equal(t, 2, p.count(), "five minutes after the user's")
	})
}

func TestSyncerOffRunsNothingUntilWoken(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var on atomic.Bool

		p := &fakePasser{}
		s := newSyncer(p, func(context.Context) time.Duration {
			if on.Load() {
				return time.Minute
			}

			return 0
		}, nil)

		stop := runSyncer(t, s)
		defer stop()

		time.Sleep(time.Hour)
		synctest.Wait()
		assert.Zero(t, p.count(), "nothing runs while the interval is zero")

		on.Store(true)
		s.Wake()
		synctest.Wait()
		assert.Equal(t, 1, p.count())
	})
}

func TestSyncerRecoversAPanickingPass(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		p := &fakePasser{panic: true}
		logs := &testutil.Logs{}

		stop := runSyncer(t, newSyncer(p, nil, logs.Logger()))
		defer stop()

		synctest.Wait()
		assert.Contains(t, logs.String(), "GitHub notifications pass panicked")

		time.Sleep(DefaultInterval + time.Second)
		synctest.Wait()
		assert.Equal(t, 2, p.count(), "and keeps going")
	})
}

// gatedClient is GitHub with no notifications, whose listing number gateAt (from 1) waits
// for gate, and which counts the listings answering at once.
type gatedClient struct {
	gate   chan struct{}
	gateAt int

	mu       sync.Mutex
	listings int
	inFlight int
	most     int
}

// newGatedClient returns a gatedClient whose listing number at waits for its gate.
func newGatedClient(at int) *gatedClient {
	return &gatedClient{gate: make(chan struct{}), gateAt: at}
}

func (*gatedClient) FetchViewer(context.Context, github.Token) (string, error) {
	return viewerLogin, nil
}

func (c *gatedClient) ListThreads(context.Context, github.Token, github.ThreadQuery) (*github.ThreadPage, error) {
	c.mu.Lock()
	c.listings++
	gated := c.listings == c.gateAt
	c.inFlight++
	c.most = max(c.most, c.inFlight)
	c.mu.Unlock()

	if gated {
		<-c.gate
	}

	c.mu.Lock()
	c.inFlight--
	c.mu.Unlock()

	return &github.ThreadPage{Threads: []github.Thread{}}, nil
}

func (*gatedClient) FetchItemStates(context.Context, github.Token, []item.Ref) (map[string]github.ItemState, error) {
	return map[string]github.ItemState{}, nil
}

// counts returns how many listings were sent, and the most answering at once.
func (c *gatedClient) counts() (listed, most int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.listings, c.most
}

func TestCheckNowNeverOverlapsAPass(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)

	synctest.Test(t, func(t *testing.T) {
		client := newGatedClient(1)
		svc := NewService(Config{
			Pool: db, Tokens: &fakeTokens{value: "test-token-not-real"}, Client: client, Rules: &fakeRules{},
		})

		stop := runSyncer(t, NewSyncer(svc, nil, nil))
		defer stop()

		synctest.Wait()

		listed, _ := client.counts()
		require.Equal(t, 1, listed, "the Syncer's pass is listing")

		checked := make(chan error, 1)

		go func() {
			_, err := svc.CheckNow(t.Context())
			checked <- err
		}()

		synctest.Wait()

		listed, _ = client.counts()
		assert.Equal(t, 1, listed, "CheckNow waits for the pass running")

		close(client.gate)
		require.NoError(t, <-checked)

		listed, most := client.counts()
		assert.Equal(t, 2, listed, "then runs its own")
		assert.Equal(t, 1, most, "never two at once")
	})
}

func TestSyncerSkipsATimerThatFiresDuringCheckNow(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)

	synctest.Test(t, func(t *testing.T) {
		// The Syncer's first listing goes through; the second, CheckNow's, waits.
		client := newGatedClient(2)
		svc := NewService(Config{
			Pool: db, Tokens: &fakeTokens{value: "test-token-not-real"}, Client: client, Rules: &fakeRules{},
		})

		stop := runSyncer(t, NewSyncer(svc, nil, nil))
		defer stop()

		synctest.Wait()

		time.Sleep(DefaultInterval - time.Minute)

		checked := make(chan error, 1)

		go func() {
			_, err := svc.CheckNow(t.Context())
			checked <- err
		}()

		// The Syncer's timer fires while CheckNow is still listing; its pass waits.
		time.Sleep(2 * time.Minute)
		synctest.Wait()

		listed, _ := client.counts()
		require.Equal(t, 2, listed, "CheckNow is listing, the Syncer's pass waiting")

		close(client.gate)
		require.NoError(t, <-checked)
		synctest.Wait()

		listed, most := client.counts()
		assert.Equal(t, 2, listed, "a pass ended while the Syncer's waited: it lists nothing")
		assert.Equal(t, 1, most)

		time.Sleep(DefaultInterval - time.Second)
		synctest.Wait()

		listed, _ = client.counts()
		assert.Equal(t, 2, listed, "the Syncer waits the interval from CheckNow's end")

		time.Sleep(2 * time.Second)
		synctest.Wait()

		listed, _ = client.counts()
		assert.Equal(t, 3, listed, "then lists")
	})
}
