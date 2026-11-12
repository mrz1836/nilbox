package sessions_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// fakeObserver answers each session with the state the test set for it, and counts its
// calls.
type fakeObserver struct {
	mu     sync.Mutex
	listed bool
	states map[string]sessions.State
	calls  int
	closes int
	seen   [][]string
}

func newFakeObserver() *fakeObserver {
	return &fakeObserver{states: map[string]sessions.State{}}
}

func (f *fakeObserver) Observe(_ context.Context, _ time.Time, live []sessions.Record) []sessions.Observation {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++

	ids := make([]string, 0, len(live))
	out := make([]sessions.Observation, 0, len(live))

	for _, r := range live {
		ids = append(ids, r.ID)

		if s, ok := f.states[r.ID]; ok {
			out = append(out, sessions.Observation{ID: r.ID, State: s, PID: 4100, Seen: f.listed})
		}
	}

	f.seen = append(f.seen, ids)

	return out
}

func (f *fakeObserver) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.closes++
}

// list makes the observer report every session as listed by its agent.
func (f *fakeObserver) list() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.listed = true
}

func (f *fakeObserver) set(id string, s sessions.State) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.states[id] = s
}

func (f *fakeObserver) counts() (calls, closes int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls, f.closes
}

// fakeAttention records what the AttentionSink heard.
type fakeAttention struct {
	mu       sync.Mutex
	needs    []string
	finished []string
}

func (f *fakeAttention) NeedsYou(_ context.Context, s sessions.Session) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.needs = append(f.needs, s.ID+":"+string(s.State))
}

func (f *fakeAttention) Finished(_ context.Context, s sessions.Session) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.finished = append(f.finished, s.ID)
}

func (f *fakeAttention) heard() (needs, finished []string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.needs), slices.Clone(f.finished)
}

// errUnreadable is what an unreadable master switch fails with.
var errUnreadable = errors.New("prefs are unreadable")

// toggle is the master switch.
type toggle struct {
	mu  sync.Mutex
	on  bool
	err error
}

func (s *toggle) FollowSessions(context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.on, s.err
}

func (s *toggle) set(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.on = on
}

// watched is a Watcher over a fresh ledger, with a fake Claude observer.
type watched struct {
	store     *sessions.Store
	ledger    *sessions.Ledger
	watcher   *sessions.Watcher
	claude    *fakeObserver
	codex     *fakeObserver
	attention *fakeAttention
	events    *testutil.Events
	toggle    *toggle
}

func newWatched(t *testing.T, tweaks ...func(*sessions.WatcherConfig)) *watched {
	t.Helper()

	w := &watched{
		store: sessions.NewStore(testutil.DB(t)), claude: newFakeObserver(), codex: newFakeObserver(),
		attention: &fakeAttention{}, events: &testutil.Events{}, toggle: &toggle{on: true},
	}

	cfg := sessions.WatcherConfig{
		Store: w.store, Observers: map[agent.ID]sessions.Observer{agent.Claude: w.claude, agent.Codex: w.codex},
		Switch: w.toggle, Attention: w.attention, Events: w.events,
	}
	for _, tweak := range tweaks {
		tweak(&cfg)
	}

	w.watcher = sessions.NewWatcher(cfg)
	w.ledger = sessions.NewLedger(sessions.Config{Store: w.store, Watcher: w.watcher, Switch: w.toggle, Events: w.events})

	return w
}

// start records a session launched now.
func (w *watched) start(t *testing.T, id string) {
	t.Helper()

	r := record(id, 12)
	r.LaunchedAt = time.Now()
	require.NoError(t, w.ledger.Start(t.Context(), r))
}

// state returns a session's stored state.
func (w *watched) state(t *testing.T, id string) sessions.State {
	t.Helper()

	r, err := w.store.Get(t.Context(), id)
	require.NoError(t, err)

	return r.State
}

// run runs the Watcher until the test ends.
func (w *watched) run(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		w.watcher.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func TestWatcherSleepsUntilASessionStarts(t *testing.T) {
	t.Parallel()

	// A short revive window, so the window ends after a few looks rather than a day's
	// worth of them; TestWatcherRevivesWithinTheDefaultWindow covers the default.
	const (
		reviveTick   = 30 * time.Second
		reviveWindow = 4 * reviveTick
	)

	synctest.Test(t, func(t *testing.T) {
		w := newWatched(t, func(cfg *sessions.WatcherConfig) {
			cfg.ReviveTick, cfg.ReviveWindow = reviveTick, reviveWindow
		})
		w.run(t)

		time.Sleep(time.Hour)
		synctest.Wait()

		calls, _ := w.claude.counts()
		assert.Zero(t, calls, "nothing runs, so nothing is asked")

		w.claude.set("s1", sessions.StateWorking)
		w.start(t, "s1")
		synctest.Wait()

		calls, _ = w.claude.counts()
		assert.Equal(t, 1, calls, "a new session wakes it at once")
		assert.Equal(t, sessions.StateWorking, w.state(t, "s1"))
		assert.Equal(t, 2, w.events.Count(), "the launch, then the change")

		time.Sleep(3*sessions.DefaultTick + time.Millisecond)
		synctest.Wait()

		calls, closes := w.claude.counts()
		assert.Equal(t, 4, calls, "once a tick while it runs")

		_, codexCloses := w.codex.counts()
		assert.Positive(t, codexCloses, "an agent with nothing to follow lets go of what it keeps")
		assert.Zero(t, closes)

		w.claude.set("s1", sessions.StateEnded)
		time.Sleep(sessions.DefaultTick)
		synctest.Wait()

		assert.Equal(t, sessions.StateEnded, w.state(t, "s1"))

		time.Sleep(3 * reviveTick)
		synctest.Wait()

		calls, _ = w.claude.counts()
		assert.Equal(t, 8, calls, "nothing runs, so only a slow look for the session resumed elsewhere")

		time.Sleep(reviveWindow)
		synctest.Wait()

		calls, closes = w.claude.counts()
		assert.Equal(t, 9, calls, "looked for until the window ends")

		time.Sleep(time.Hour)
		synctest.Wait()

		again, _ := w.claude.counts()
		assert.Equal(t, calls, again, "once the window after it ended passed, nothing is asked")
		assert.Equal(t, 1, closes, "and the observer let go")
	})
}

func TestWatcherRevivesWithinTheDefaultWindow(t *testing.T) {
	t.Parallel()

	w := newWatched(t)
	clk := testutil.NewClock(launched())
	w.watcher = sessions.NewWatcher(sessions.WatcherConfig{
		Store: w.store, Observers: map[agent.ID]sessions.Observer{agent.Claude: w.claude}, Clock: clk,
	})

	require.NoError(t, w.store.Insert(t.Context(), record("s1", 12)))
	_, err := w.store.Apply(t.Context(), launched(), []sessions.Observation{{ID: "s1", State: sessions.StateEnded}})
	require.NoError(t, err)

	clk.Set(launched().Add(sessions.ReviveWindow))
	assert.Equal(t, sessions.ReviveTick, w.watcher.Reconcile(t.Context()), "an ended session is looked for once a revive tick")

	calls, _ := w.claude.counts()
	assert.Equal(t, 1, calls, "until the default window after it ended passed")

	clk.Set(launched().Add(sessions.ReviveWindow + time.Second))
	assert.Zero(t, w.watcher.Reconcile(t.Context()), "then it waits to be woken")

	calls, closes := w.claude.counts()
	assert.Equal(t, 1, calls, "and asks nothing")
	assert.Equal(t, 1, closes, "the observer let go")
}

func TestWatcherFollowsASessionResumedOutsideTheApp(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		w := newWatched(t)
		w.watcher = sessions.NewWatcher(sessions.WatcherConfig{
			Store: w.store, Observers: map[agent.ID]sessions.Observer{agent.Claude: w.claude}, Events: w.events,
			NewID: func() string { return "resumed" },
		})
		w.ledger = sessions.NewLedger(sessions.Config{Store: w.store, Watcher: w.watcher, Events: w.events})
		w.run(t)

		w.claude.set("s1", sessions.StateEnded)
		w.start(t, "s1")
		synctest.Wait()
		require.Equal(t, sessions.StateEnded, w.state(t, "s1"))

		w.claude.set("s1", sessions.StateWorking)
		w.claude.list()
		time.Sleep(sessions.ReviveTick)
		synctest.Wait()

		assert.Equal(t, sessions.StateEnded, w.state(t, "s1"), "the ended session stays as it ended")

		got, err := w.store.Get(t.Context(), "resumed")
		require.NoError(t, err)
		assert.Equal(t, "s1", got.ResumedFrom)
		assert.Equal(t, "s1", got.AgentSessionID)
		assert.Equal(t, sessions.StateWorking, got.State)
		assert.Equal(t, 4100, got.PID)

		w.claude.set("resumed", sessions.StateIdle)
		time.Sleep(sessions.DefaultTick)
		synctest.Wait()
		assert.Equal(t, sessions.StateIdle, w.state(t, "resumed"), "and followed from then on")
	})
}

func TestWatcherReconcilesWhatRanBeforeItStarted(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		w := newWatched(t)
		require.NoError(t, w.store.Insert(t.Context(), record("before", 12)))
		w.claude.set("before", sessions.StateEnded)

		w.run(t)
		synctest.Wait()

		assert.Equal(t, sessions.StateEnded, w.state(t, "before"), "a session left running at the last quit is reconciled")
	})
}

func TestWatcherDebouncesWhatNeedsTheUser(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		w := newWatched(t)
		w.run(t)

		w.claude.set("quick", sessions.StateWorking)
		w.claude.set("slow", sessions.StateWorking)
		w.start(t, "quick")
		w.start(t, "slow")
		synctest.Wait()

		w.claude.set("quick", sessions.StateNeedsApproval)
		w.claude.set("slow", sessions.StateNeedsInput)
		time.Sleep(sessions.DefaultTick)
		synctest.Wait()

		w.claude.set("quick", sessions.StateWorking)
		time.Sleep(sessions.DefaultTick)
		synctest.Wait()

		needs, _ := w.attention.heard()
		assert.Empty(t, needs, "nothing yet: a prompt answered at once never shows")

		time.Sleep(sessions.DefaultDebounce)
		synctest.Wait()

		needs, _ = w.attention.heard()
		assert.Equal(t, []string{"slow:needs_input"}, needs, "once, after the debounce")

		w.claude.set("quick", sessions.StateIdle)
		time.Sleep(sessions.DefaultTick)
		synctest.Wait()

		_, finished := w.attention.heard()
		assert.Equal(t, []string{"quick"}, finished, "a turn finished")

		time.Sleep(10 * sessions.DefaultDebounce)
		synctest.Wait()

		needs, _ = w.attention.heard()
		assert.Len(t, needs, 1, "never twice for one wait")
	})
}

func TestWatcherFollowsNothingWhileSwitchedOff(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		w := newWatched(t)
		w.toggle.set(false)
		w.run(t)

		w.claude.set("s1", sessions.StateWorking)
		w.start(t, "s1")
		synctest.Wait()

		time.Sleep(time.Minute)
		synctest.Wait()

		calls, _ := w.claude.counts()
		assert.Zero(t, calls, "the agents are never asked")
		assert.Equal(t, sessions.StateStarting, w.state(t, "s1"), "the session is still recorded")

		waiting, err := w.ledger.Waiting(t.Context())
		require.NoError(t, err)
		assert.Empty(t, waiting)

		w.toggle.set(true)
		time.Sleep(time.Minute)
		synctest.Wait()

		assert.Equal(t, sessions.StateWorking, w.state(t, "s1"), "followed once switched on")
	})
}

func TestWatcherFollowsWhenTheSwitchCannotBeRead(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		w := newWatched(t)
		w.toggle.err = errUnreadable
		w.run(t)

		w.claude.set("s1", sessions.StateIdle)
		w.start(t, "s1")
		synctest.Wait()

		assert.Equal(t, sessions.StateIdle, w.state(t, "s1"))
	})
}

func TestLedgerStartTellsTheWindow(t *testing.T) {
	t.Parallel()

	w := newWatched(t)
	w.start(t, "s1")

	assert.Equal(t, []event.Event{event.SessionsUpdated{}}, w.events.All())

	got, err := w.ledger.Get(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, sessions.StateStarting, got.State)
}

func TestLedgerWaiting(t *testing.T) {
	t.Parallel()

	w := newWatched(t)
	w.start(t, "a")
	w.start(t, "b")

	_, err := w.store.Apply(t.Context(), launched(), []sessions.Observation{
		{ID: "a", State: sessions.StateNeedsApproval}, {ID: "b", State: sessions.StateWorking},
	})
	require.NoError(t, err)

	got, err := w.ledger.Waiting(t.Context())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "a", got[0].ID)
}

func TestLedgerRunningAndAgentProcesses(t *testing.T) {
	t.Parallel()

	w := newWatched(t)
	w.start(t, "older")
	w.start(t, "newer")

	other := record("other", 13)
	other.LaunchedAt = time.Now()
	require.NoError(t, w.ledger.Start(t.Context(), other))

	_, err := w.store.Apply(t.Context(), launched(), []sessions.Observation{
		{ID: "older", State: sessions.StateWorking, PID: 4100}, {ID: "other", State: sessions.StateEnded, PID: 4200},
	})
	require.NoError(t, err)

	running, err := w.ledger.Running(t.Context(), "/srv/wt/example-repo/pr-12/")
	require.NoError(t, err)
	require.Len(t, running, 2, "the sessions running in the folder; the one that ended is not")
	assert.Equal(t, "newer", running[0].ID, "newest first")

	none, err := w.ledger.Running(t.Context(), "/srv/wt/example-repo/pr-13")
	require.NoError(t, err)
	assert.Empty(t, none, "nothing runs in another folder")
	assert.NotNil(t, none)

	pids, err := w.ledger.AgentProcesses(t.Context())
	require.NoError(t, err)
	assert.Equal(t, map[int]agent.ID{4100: agent.Claude}, pids, "only running sessions with a known process")

	w.toggle.set(false)

	pids, err = w.ledger.AgentProcesses(t.Context())
	require.NoError(t, err)
	assert.Empty(t, pids, "not followed, the processes it knows may be stale")

	running, err = w.ledger.Running(t.Context(), "/srv/wt/example-repo/pr-12")
	require.NoError(t, err)
	assert.Empty(t, running)
}

func TestWatcherTakesAMomentItCouldNotAskForNoChange(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		w := newWatched(t)
		w.run(t)

		w.claude.set("s1", sessions.StateNeedsApproval)
		w.claude.set("s2", sessions.StateWorking)
		w.start(t, "s1")
		w.start(t, "s2")
		synctest.Wait()

		time.Sleep(sessions.DefaultDebounce + sessions.DefaultTick)
		synctest.Wait()

		needs, _ := w.attention.heard()
		require.Equal(t, []string{"s1:needs_approval"}, needs)

		w.claude.set("s1", sessions.StateUnknown)
		w.claude.set("s2", sessions.StateUnknown)
		time.Sleep(sessions.DefaultTick)
		synctest.Wait()

		w.claude.set("s1", sessions.StateNeedsApproval)
		w.claude.set("s2", sessions.StateIdle)
		time.Sleep(sessions.DefaultTick)
		synctest.Wait()

		time.Sleep(10 * sessions.DefaultDebounce)
		synctest.Wait()

		needs, finished := w.attention.heard()
		assert.Equal(t, []string{"s1:needs_approval"}, needs, "the same prompt is not announced again")
		assert.Equal(t, []string{"s2"}, finished, "a turn that finished while it could not be asked is announced")
	})
}

func TestWatcherRemindsOnceAPromptItCouldNotAskAboutIsKnownAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		w := newWatched(t)
		w.run(t)

		w.claude.set("s1", sessions.StateWorking)
		w.start(t, "s1")
		synctest.Wait()

		w.claude.set("s1", sessions.StateNeedsInput)
		time.Sleep(sessions.DefaultTick)
		synctest.Wait()

		w.claude.set("s1", sessions.StateUnknown)
		time.Sleep(sessions.DefaultDebounce + sessions.DefaultTick)
		synctest.Wait()

		needs, _ := w.attention.heard()
		assert.Empty(t, needs, "nothing while the agent cannot be asked")

		w.claude.set("s1", sessions.StateNeedsInput)
		time.Sleep(sessions.DefaultTick)
		synctest.Wait()

		needs, _ = w.attention.heard()
		assert.Equal(t, []string{"s1:needs_input"}, needs, "the wait is announced once it is known again")
	})
}

func TestWatcherRevivesASessionLaunchedLongAgoThatEndedRecently(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		w := newWatched(t)
		w.watcher = sessions.NewWatcher(sessions.WatcherConfig{
			Store: w.store, Observers: map[agent.ID]sessions.Observer{agent.Claude: w.claude}, Events: w.events,
			NewID: func() string { return "resumed" },
		})

		old := record("s1", 12)
		old.LaunchedAt = time.Now().Add(-3 * 24 * time.Hour)
		require.NoError(t, w.store.Insert(t.Context(), old))

		_, err := w.store.Apply(t.Context(), time.Now().Add(-time.Hour), []sessions.Observation{
			{ID: "s1", State: sessions.StateEnded},
		})
		require.NoError(t, err)

		w.claude.set("s1", sessions.StateWorking)
		w.claude.list()
		w.run(t)
		synctest.Wait()

		got, err := w.store.Get(t.Context(), "resumed")
		require.NoError(t, err, "watched for a day after it ended, however long ago it was launched")
		assert.Equal(t, "s1", got.ResumedFrom)
	})
}

func TestLedgerWaitingListsTheLongestWaitingFirst(t *testing.T) {
	t.Parallel()

	w := newWatched(t)
	w.start(t, "launched-first")
	w.start(t, "launched-second")

	_, err := w.store.Apply(t.Context(), launched().Add(time.Hour), []sessions.Observation{
		{ID: "launched-second", State: sessions.StateNeedsApproval},
	})
	require.NoError(t, err)

	_, err = w.store.Apply(t.Context(), launched().Add(2*time.Hour), []sessions.Observation{
		{ID: "launched-first", State: sessions.StateNeedsInput},
	})
	require.NoError(t, err)

	got, err := w.ledger.Waiting(t.Context())
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "launched-second", got[0].ID, "it has waited an hour longer")
}
