package ghstatus

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

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/testutil"
)

var errOffline = errors.New("dial tcp: lookup www.githubstatus.com: no such host")

// fakeFetcher answers each read with what it was given last, counting the reads.
type fakeFetcher struct {
	mu     sync.Mutex
	status *github.ServiceStatus
	err    error
	reads  atomic.Int32
}

func (f *fakeFetcher) FetchStatus(context.Context) (*github.ServiceStatus, error) {
	f.reads.Add(1)

	f.mu.Lock()
	defer f.mu.Unlock()

	return f.status, f.err
}

func (f *fakeFetcher) answer(status *github.ServiceStatus, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.status, f.err = status, err
}

// incident is the page during a major incident with Actions.
func incident() *github.ServiceStatus {
	return &github.ServiceStatus{
		Indicator: github.StatusMajor, Description: "Partial System Outage",
		Incidents: []github.StatusIncident{{
			Name: "Incident with Actions", Status: "investigating", Impact: "major",
			URL: "https://www.githubstatus.com/incidents/x1",
		}},
		Components: []github.StatusComponent{{Name: "Actions", Status: "partial_outage"}},
	}
}

// operational is the page with every system operational.
func operational() *github.ServiceStatus {
	return &github.ServiceStatus{
		Indicator: github.StatusNone, Description: "All Systems Operational",
		Incidents: []github.StatusIncident{}, Components: []github.StatusComponent{},
	}
}

// every says how often to check, from a value the test changes.
type every struct{ d atomic.Int64 }

func (e *every) set(d time.Duration) { e.d.Store(int64(d)) }

func (e *every) get(context.Context) time.Duration { return time.Duration(e.d.Load()) }

type rig struct {
	svc    *Service
	fetch  *fakeFetcher
	every  *every
	clock  *testutil.Clock
	events *testutil.Events
	logs   *testutil.Logs
}

func newRig(t *testing.T) *rig {
	t.Helper()

	r := &rig{
		fetch: &fakeFetcher{}, every: &every{}, events: &testutil.Events{}, logs: &testutil.Logs{},
		clock: testutil.NewClock(time.Date(2026, 11, 14, 17, 0, 0, 0, time.UTC)),
	}
	r.every.set(30 * time.Minute)
	r.fetch.answer(incident(), nil)
	r.svc = NewService(Config{
		Fetcher: r.fetch, Every: r.every.get, Clock: r.clock, Events: r.events, Logger: r.logs.Logger(),
	})

	return r
}

func TestStatusIsUnknownBeforeTheFirstCheck(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	assert.Equal(t, Status{
		State: StateUnknown, Incidents: []Incident{}, Components: []Component{}, URL: PageURL,
	}, r.svc.Status(t.Context()))
	assert.Zero(t, r.fetch.reads.Load(), "Status reads nothing from the network")
}

func TestCheckNowReadsThePageAndTellsTheWindow(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	got := r.svc.CheckNow(t.Context())

	want := Status{
		State: StateMajor, Description: "Partial System Outage",
		Incidents: []Incident{{
			Name: "Incident with Actions", Status: "investigating", Impact: "major",
			URL: "https://www.githubstatus.com/incidents/x1",
		}},
		Components: []Component{{Name: "Actions", Status: "partial_outage"}},
		CheckedAt:  "2026-11-14T17:00:00Z", URL: "https://www.githubstatus.com/",
	}
	assert.Equal(t, want, got)
	assert.Equal(t, want, r.svc.Status(t.Context()), "the reading is kept")
	assert.Equal(t, []event.Event{event.GitHubStatusUpdated{}}, r.events.All())
}

func TestCheckNowReadsAgainOnlyOnceTheReadingIsOlderThanFreshFor(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	r.svc.CheckNow(t.Context())
	r.fetch.answer(operational(), nil)
	r.clock.Advance(FreshFor - time.Second)

	assert.Equal(t, StateMajor, r.svc.CheckNow(t.Context()).State, "a reading moments old stands")
	assert.Equal(t, int32(1), r.fetch.reads.Load())

	r.clock.Advance(time.Second)

	got := r.svc.CheckNow(t.Context())
	assert.Equal(t, StateOperational, got.State)
	assert.Empty(t, got.Incidents)
	assert.NotNil(t, got.Incidents, "the window gets [], never null")
	assert.Equal(t, int32(2), r.fetch.reads.Load())
}

func TestAFailedCheckKeepsTheReadingAndSaysWhy(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	r.svc.CheckNow(t.Context())
	r.fetch.answer(nil, errOffline)
	r.clock.Advance(time.Minute)

	got := r.svc.CheckNow(t.Context())

	assert.Equal(t, StateMajor, got.State, "the last reading stands")
	assert.Equal(t, "2026-11-14T17:00:00Z", got.CheckedAt, "taken when it was")
	assert.Equal(t, "Couldn't reach GitHub's status page.", got.Error)
	assert.Contains(t, r.logs.String(), "no such host")
	assert.Len(t, r.events.All(), 2, "the window hears the failure too")

	r.fetch.answer(incident(), nil)
	r.clock.Advance(time.Minute)

	assert.Empty(t, r.svc.CheckNow(t.Context()).Error, "a check that works clears it")
}

func TestOffReadsNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	r.svc.CheckNow(t.Context())
	r.every.set(0)

	assert.Equal(t, Status{
		State: StateOff, Incidents: []Incident{}, Components: []Component{}, URL: PageURL,
	}, r.svc.CheckNow(t.Context()))
	assert.Equal(t, StateOff, r.svc.Status(t.Context()).State)
	assert.Equal(t, int32(1), r.fetch.reads.Load())

	r.every.set(time.Hour)
	assert.Equal(t, StateMajor, r.svc.Status(t.Context()).State, "turned on again, the last reading shows")
}

func TestStatesFollowThePagesIndicator(t *testing.T) {
	t.Parallel()

	for indicator, want := range map[string]State{
		github.StatusNone: StateOperational, github.StatusMinor: StateMinor, github.StatusMajor: StateMajor,
		github.StatusCritical: StateCritical, github.StatusMaintenance: StateMaintenance, "on fire": StateMinor,
	} {
		assert.Equal(t, want, stateOf(indicator), indicator)
	}
}

func TestStatusSharesNoListWithTheReading(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	got := r.svc.CheckNow(t.Context())
	got.Incidents[0].Name = "changed"

	assert.Equal(t, "Incident with Actions", r.svc.Status(t.Context()).Incidents[0].Name)
}

func TestACheckWhoseContextEndsChangesNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	assert.Equal(t, StateUnknown, r.svc.CheckNow(ctx).State)
	assert.Empty(t, r.events.All())
}

func TestTheCheckerChecksAtOnceThenAsOftenAsSet(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		checker := NewChecker(r.svc)

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			checker.Run(ctx)
		}()

		synctest.Wait()
		assert.Equal(t, int32(1), r.fetch.reads.Load(), "a check at once")

		time.Sleep(30*time.Minute + time.Second)
		synctest.Wait()
		assert.Equal(t, int32(2), r.fetch.reads.Load(), "and one each interval")

		// Off, it checks nothing however long it waits; on again, it checks at once once a
		// check is due.
		r.every.set(0)
		checker.Wake()
		time.Sleep(2 * time.Hour)
		synctest.Wait()
		assert.Equal(t, int32(2), r.fetch.reads.Load())

		r.every.set(5 * time.Minute)
		checker.Wake()
		synctest.Wait()
		assert.Equal(t, int32(3), r.fetch.reads.Load())

		cancel()
		<-done
	})
}

func TestTheCheckerWakesWithoutBlocking(t *testing.T) {
	t.Parallel()

	checker := NewChecker(newRig(t).svc)

	checker.Wake()
	checker.Wake()

	require.Len(t, checker.wake, 1, "a second wake while one waits is dropped")
}
