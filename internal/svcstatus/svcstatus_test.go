package svcstatus

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/statusprefs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

var errOffline = errors.New("dial tcp: lookup status.claude.com: no such host")

// The hosts of the three pages.
const (
	githubHost = "www.githubstatus.com"
	claudeHost = "status.claude.com"
	openaiHost = "status.openai.com"
)

// answer is what the fake page at a host answers.
type answer struct {
	body  string
	etag  string
	err   error
	panic bool
}

// fakeFetcher answers each read with what its host was given last, answering a 304 to a
// matching ETag, and counts the reads per host.
type fakeFetcher struct {
	mu      sync.Mutex
	answers map[string]answer
	reads   map[string]int
	etags   map[string]string // the ETag each host was last sent
}

func newFakeFetcher() *fakeFetcher {
	return &fakeFetcher{answers: map[string]answer{}, reads: map[string]int{}, etags: map[string]string{}}
}

func (f *fakeFetcher) FetchStatus(_ context.Context, req github.StatusRequest) (github.StatusAnswer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.reads[req.Host]++
	f.etags[req.Host] = req.ETag
	a := f.answers[req.Host]

	switch {
	case a.panic:
		panic("the page broke the fetcher")
	case a.err != nil:
		return github.StatusAnswer{}, a.err
	case a.etag != "" && req.ETag == a.etag:
		return github.StatusAnswer{ETag: a.etag, NotModified: true}, nil
	default:
		return github.StatusAnswer{Body: []byte(a.body), ETag: a.etag}, nil
	}
}

func (f *fakeFetcher) answer(host string, a answer) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.answers[host] = a
}

// count returns how many reads each host had, hosts never read left out.
func (f *fakeFetcher) count() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := map[string]int{}

	for host, n := range f.reads {
		if n > 0 {
			out[host] = n
		}
	}

	return out
}

// sent returns the ETag host was last sent.
func (f *fakeFetcher) sent(host string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.etags[host]
}

// rulesBox holds the rules a test changes.
type rulesBox struct {
	mu    sync.Mutex
	rules Rules
}

func (b *rulesBox) get(context.Context) Rules {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.rules
}

func (b *rulesBox) every(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.rules.Every = d
}

func (b *rulesBox) change(t testing.TB, change func(statusprefs.Config) (statusprefs.Config, error)) {
	t.Helper()

	b.mu.Lock()
	defer b.mu.Unlock()

	cfg, err := change(b.rules.Watch)
	require.NoError(t, err)

	b.rules.Watch = cfg
}

// alerts records the alerts it hears.
type alerts struct {
	mu  sync.Mutex
	all []Alert
}

func (a *alerts) Alert(_ context.Context, alert Alert) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.all = append(a.all, alert)
}

func (a *alerts) heard() []Alert {
	a.mu.Lock()
	defer a.mu.Unlock()

	return append([]Alert(nil), a.all...)
}

type rig struct {
	svc    *Service
	fetch  *fakeFetcher
	rules  *rulesBox
	alerts *alerts
	clock  *testutil.Clock
	events *testutil.Events
	logs   *testutil.Logs
}

// newRig builds a Service over the recorded pages (GitHub operational, Claude with an
// incident on claude.ai, OpenAI with one on the Compliance API), every 30 minutes, the
// defaults watched. A nil c is the system clock, for the Checker's tests.
func newRig(t *testing.T, c *testutil.Clock) *rig {
	t.Helper()

	r := &rig{
		fetch: newFakeFetcher(), rules: &rulesBox{rules: Rules{Every: 30 * time.Minute, Watch: statusprefs.Defaults()}},
		alerts: &alerts{}, events: &testutil.Events{}, logs: &testutil.Logs{}, clock: c,
	}
	r.fetch.answer(githubHost, answer{body: string(fixture(t, "github-operational.json")), etag: `W/"gh1"`})
	r.fetch.answer(claudeHost, answer{body: string(fixture(t, "claude-incident.json")), etag: `W/"cl1"`})
	r.fetch.answer(openaiHost, answer{body: string(fixture(t, "openai-incident.json"))})

	cfg := Config{
		Fetcher: r.fetch, Rules: r.rules.get, Notifier: r.alerts, Events: r.events, Logger: r.logs.Logger(),
	}
	if c != nil {
		cfg.Clock = c
	}

	r.svc = NewService(cfg)

	return r
}

// at is the rig's clock's start.
func at() time.Time {
	return time.Date(2026, 11, 14, 17, 0, 0, 0, time.UTC)
}

// claudeDown is Claude's page with the Claude API in a partial outage.
const claudeDown = `{
	"status": {"indicator": "major"},
	"components": [
		{"id": "k8w3r06qmzrp", "name": "Claude API (api.anthropic.com)", "status": "partial_outage"},
		{"id": "yyzkbfz2thpt", "name": "Claude Code", "status": "operational"},
		{"id": "rwppv331jlwc", "name": "claude.ai", "status": "operational"},
		{"id": "0qbwn08sd68x", "name": "Claude Console (platform.claude.com)", "status": "operational"},
		{"id": "bpp5gb3hpjcl", "name": "Claude Cowork", "status": "operational"},
		{"id": "w14q5j51spbm", "name": "Claude Platform on AWS", "status": "operational"},
		{"id": "csth13wtqbd6", "name": "Claude Models on Google Cloud (Anthropic managed)", "status": "operational"},
		{"id": "69zh8pryjjgp", "name": "Claude in Microsoft Foundry", "status": "operational"},
		{"id": "0scnb50nvy53", "name": "Claude for Government", "status": "operational"}
	],
	"incidents": [{"id": "x1", "name": "Elevated errors on Claude API", "status": "identified", "impact": "major",
		"components": [{"id": "k8w3r06qmzrp", "name": "Claude API (api.anthropic.com)"}]}],
	"scheduled_maintenances": []
}`

// claudeUp is Claude's page with everything operational.
const claudeUp = `{
	"status": {"indicator": "none"},
	"components": [
		{"id": "k8w3r06qmzrp", "name": "Claude API (api.anthropic.com)", "status": "operational"},
		{"id": "yyzkbfz2thpt", "name": "Claude Code", "status": "operational"},
		{"id": "rwppv331jlwc", "name": "claude.ai", "status": "operational"}
	],
	"incidents": [], "scheduled_maintenances": []
}`

func TestStatusIsUnknownBeforeTheFirstCheck(t *testing.T) {
	t.Parallel()

	r := newRig(t, testutil.NewClock(at()))

	got := r.svc.Status(t.Context())

	assert.Equal(t, StateUnknown, got.State)
	assert.Empty(t, got.CheckedAt)
	require.Len(t, got.Providers, 3)

	for _, p := range got.Providers {
		assert.Equal(t, StateUnknown, p.State, p.ID)
		assert.NotNil(t, p.Affected, "the window gets [], never null")
		assert.NotNil(t, p.Incidents)
		assert.NotNil(t, p.Unlisted)
	}

	assert.Empty(t, r.fetch.count(), "Status reads nothing from the network")
}

func TestCheckNowReadsEveryPageWatchedAndTellsTheWindowOnce(t *testing.T) {
	t.Parallel()

	r := newRig(t, testutil.NewClock(at()))

	got := r.svc.CheckNow(t.Context())

	assert.Equal(t, map[string]int{githubHost: 1, claudeHost: 1, openaiHost: 1}, r.fetch.count())
	assert.Equal(t, []event.Event{event.ServiceStatusUpdated{}}, r.events.All())

	assert.Equal(t, StateOperational, got.State, "neither incident touches what is watched")
	assert.Equal(t, "2026-11-14T17:00:00Z", got.CheckedAt)
	assert.Equal(t, Provider{
		ID: "claude", Name: "Claude", URL: "https://status.claude.com/", State: StateOperational,
		Affected: []Part{}, Incidents: []Incident{}, Others: 1, Unlisted: []statusprefs.PartID{},
		CheckedAt: "2026-11-14T17:00:00Z",
	}, got.Providers[1])
	assert.Equal(t, 1, got.Providers[2].Others, "OpenAI's incident is another")
	assert.Equal(t, got, r.svc.Status(t.Context()), "the readings are kept")
	assert.Empty(t, r.alerts.heard(), "the first readings only set the baselines")
}

func TestAChangeOfWhatIsWatchedCostsNoRequest(t *testing.T) {
	t.Parallel()

	r := newRig(t, testutil.NewClock(at()))
	r.svc.CheckNow(t.Context())

	r.rules.change(t, func(c statusprefs.Config) (statusprefs.Config, error) { return c.SetPart("claude.ai", true) })

	got := r.svc.Status(t.Context())
	assert.Equal(t, StateMajor, got.State, "claude.ai's incident counts at once")
	assert.Equal(t, StateMajor, got.Providers[1].State)
	assert.Equal(t, []Part{{ID: "claude.ai", Name: "claude.ai", State: StateMinor}}, got.Providers[1].Affected)
	assert.Equal(t, 0, got.Providers[1].Others)

	r.rules.change(t, func(c statusprefs.Config) (statusprefs.Config, error) { return c.SetPart("claude.ai", false) })
	assert.Equal(t, StateOperational, r.svc.Status(t.Context()).State, "and goes again")

	assert.Equal(t, map[string]int{githubHost: 1, claudeHost: 1, openaiHost: 1}, r.fetch.count(), "no request")
}

func TestCheckNowReadsEachProviderAgainOnlyOnceItIsOlderThanFreshFor(t *testing.T) {
	t.Parallel()

	r := newRig(t, testutil.NewClock(at()))

	r.svc.CheckNow(t.Context())
	r.clock.Advance(FreshFor - time.Second)
	r.svc.CheckNow(t.Context())

	assert.Equal(t, map[string]int{githubHost: 1, claudeHost: 1, openaiHost: 1}, r.fetch.count(),
		"readings moments old stand")
	assert.Len(t, r.events.All(), 1, "nothing read, nothing told")

	// Claude turned on later: only it is read, while the others stand.
	r.rules.change(t, func(c statusprefs.Config) (statusprefs.Config, error) { return c.SetProvider("claude", false) })
	r.svc.applyRules(t.Context())
	r.rules.change(t, func(c statusprefs.Config) (statusprefs.Config, error) { return c.SetProvider("claude", true) })

	r.svc.CheckNow(t.Context())
	assert.Equal(t, map[string]int{githubHost: 1, claudeHost: 2, openaiHost: 1}, r.fetch.count())

	r.clock.Advance(time.Second)
	r.svc.CheckNow(t.Context())
	assert.Equal(t, map[string]int{githubHost: 2, claudeHost: 2, openaiHost: 2}, r.fetch.count())
}

func TestA304KeepsTheReading(t *testing.T) {
	t.Parallel()

	r := newRig(t, testutil.NewClock(at()))

	r.svc.CheckNow(t.Context())
	r.clock.Advance(time.Minute)

	got := r.svc.CheckNow(t.Context())

	assert.Equal(t, `W/"cl1"`, r.fetch.sent(claudeHost), "the last ETag is sent")
	assert.Empty(t, r.fetch.sent(openaiHost), "a page that gives none is asked plainly")
	assert.Equal(t, 1, got.Providers[1].Others, "the reading stands")
	assert.Equal(t, "2026-11-14T17:01:00Z", got.Providers[1].CheckedAt, "confirmed now")
	assert.Equal(t, "2026-11-14T17:01:00Z", got.CheckedAt)
}

func TestAFailedCheckKeepsTheReadingAndSaysWhy(t *testing.T) {
	t.Parallel()

	r := newRig(t, testutil.NewClock(at()))

	r.svc.CheckNow(t.Context())
	r.fetch.answer(claudeHost, answer{err: errOffline})
	r.fetch.answer(githubHost, answer{body: `<html>down</html>`})
	r.clock.Advance(time.Minute)

	got := r.svc.CheckNow(t.Context())

	claude := got.Providers[1]
	assert.Equal(t, StateOperational, claude.State, "the last reading stands")
	assert.Equal(t, 1, claude.Others)
	assert.Equal(t, "2026-11-14T17:00:00Z", claude.CheckedAt, "taken when it was")
	assert.Equal(t, "Couldn't reach Claude's status page.", claude.Error)
	assert.Equal(t, "GitHub's status page answered with something the app can't read.", got.Providers[0].Error)
	assert.Equal(t, "2026-11-14T17:00:00Z", got.CheckedAt, "the oldest reading")
	assert.Contains(t, r.logs.String(), "no such host")
	assert.Len(t, r.events.All(), 2, "the window hears the failure too")

	r.fetch.answer(claudeHost, answer{body: claudeUp})
	r.clock.Advance(time.Minute)

	assert.Empty(t, r.svc.CheckNow(t.Context()).Providers[1].Error, "a check that works clears it")
}

func TestAFailedFirstCheckIsUnknownWithWhy(t *testing.T) {
	t.Parallel()

	r := newRig(t, testutil.NewClock(at()))
	r.fetch.answer(claudeHost, answer{err: errOffline})

	got := r.svc.CheckNow(t.Context()).Providers[1]

	assert.Equal(t, StateUnknown, got.State)
	assert.Equal(t, "Couldn't reach Claude's status page.", got.Error)
	assert.Empty(t, got.CheckedAt)
}

func TestOffReadsNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t, testutil.NewClock(at()))

	r.svc.CheckNow(t.Context())
	r.rules.every(0)
	r.clock.Advance(time.Hour)

	got := r.svc.CheckNow(t.Context())
	assert.Equal(t, StateOff, got.State)
	assert.Empty(t, got.CheckedAt)

	for _, p := range got.Providers {
		assert.Equal(t, StateOff, p.State, p.ID)
	}

	assert.Equal(t, map[string]int{githubHost: 1, claudeHost: 1, openaiHost: 1}, r.fetch.count())

	r.rules.every(time.Hour)
	assert.Equal(t, StateOperational, r.svc.Status(t.Context()).State, "turned on again, the last readings show")
}

func TestNothingWatchedMeansNoRequest(t *testing.T) {
	t.Parallel()

	r := newRig(t, testutil.NewClock(at()))
	r.rules.change(t, func(c statusprefs.Config) (statusprefs.Config, error) {
		c, _ = c.SetProvider("github", false)
		c, _ = c.SetProvider("openai", false)

		for _, id := range []statusprefs.PartID{"claude.api", "claude.code"} {
			c, _ = c.SetPart(id, false)
		}

		return c, nil
	})

	got := r.svc.CheckNow(t.Context())

	assert.Equal(t, StateOff, got.State)
	assert.Empty(t, r.fetch.count())
	assert.Empty(t, r.events.All())
}

func TestAProviderTurnedOffIsForgotten(t *testing.T) {
	t.Parallel()

	r := newRig(t, testutil.NewClock(at()))
	r.svc.CheckNow(t.Context())

	r.rules.change(t, func(c statusprefs.Config) (statusprefs.Config, error) { return c.SetProvider("claude", false) })
	r.svc.applyRules(t.Context())

	assert.Equal(t, StateOff, r.svc.Status(t.Context()).Providers[1].State)
	assert.Equal(t, []event.Event{event.ServiceStatusUpdated{}, event.ServiceStatusUpdated{}}, r.events.All())

	r.rules.change(t, func(c statusprefs.Config) (statusprefs.Config, error) { return c.SetProvider("claude", true) })

	assert.Equal(t, StateUnknown, r.svc.Status(t.Context()).Providers[1].State, "its reading went with it")

	r.svc.CheckNow(t.Context())
	assert.Empty(t, r.fetch.sent(claudeHost), "and its ETag")
}

func TestACheckWhoseContextEndsChangesNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t, testutil.NewClock(at()))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	assert.Equal(t, StateUnknown, r.svc.CheckNow(ctx).State)
	assert.Empty(t, r.events.All())
}

func TestAPartGoingDownAndComingBackIsAlerted(t *testing.T) {
	t.Parallel()

	r := newRig(t, testutil.NewClock(at()))
	r.svc.CheckNow(t.Context())

	r.fetch.answer(claudeHost, answer{body: claudeDown})
	r.clock.Advance(time.Minute)
	r.svc.CheckNow(t.Context())

	assert.Equal(t, []Alert{{
		Provider: "claude", State: StateMajor, Title: "Claude: Partial outage",
		Body: "Claude API · Elevated errors on Claude API", URL: "https://status.claude.com/incidents/x1",
	}}, r.alerts.heard())

	r.clock.Advance(time.Minute)
	r.svc.CheckNow(t.Context())
	assert.Len(t, r.alerts.heard(), 1, "told once")

	r.fetch.answer(claudeHost, answer{err: errOffline})
	r.clock.Advance(time.Minute)
	r.svc.CheckNow(t.Context())
	assert.Len(t, r.alerts.heard(), 1, "a failed check tells nothing")

	r.fetch.answer(claudeHost, answer{body: claudeUp})
	r.clock.Advance(time.Minute)
	r.svc.CheckNow(t.Context())

	assert.Equal(t, Alert{
		Provider: "claude", State: StateOperational, Title: "Claude: Operational again", URL: "https://status.claude.com/",
	}, r.alerts.heard()[1])
}

func TestAChangeOfWhatIsWatchedSetsTheBaselineQuietly(t *testing.T) {
	t.Parallel()

	r := newRig(t, testutil.NewClock(at()))
	r.svc.CheckNow(t.Context())

	r.rules.change(t, func(c statusprefs.Config) (statusprefs.Config, error) { return c.SetPart("claude.ai", true) })
	r.svc.applyRules(t.Context())

	r.clock.Advance(time.Minute)
	r.svc.CheckNow(t.Context())
	assert.Empty(t, r.alerts.heard(), "claude.ai's incident was there when it was ticked")

	r.fetch.answer(claudeHost, answer{body: claudeUp})
	r.clock.Advance(time.Minute)
	r.svc.CheckNow(t.Context())

	require.Len(t, r.alerts.heard(), 1, "but its recovery is told")
	assert.Equal(t, "Claude: Operational again", r.alerts.heard()[0].Title)
}

func TestUnlistedPartsAreLoggedOnce(t *testing.T) {
	t.Parallel()

	r := newRig(t, testutil.NewClock(at()))
	r.fetch.answer(claudeHost, answer{body: claudeUp})

	got := r.svc.CheckNow(t.Context())
	assert.Empty(t, got.Providers[1].Unlisted)

	r.rules.change(t, func(c statusprefs.Config) (statusprefs.Config, error) { return c.SetPart("claude.gov", true) })

	for range 2 {
		r.clock.Advance(time.Minute)
		got = r.svc.CheckNow(t.Context())
	}

	assert.Equal(t, []statusprefs.PartID{"claude.gov"}, got.Providers[1].Unlisted)
	assert.Equal(t, 1, strings.Count(r.logs.String(), "no longer lists a part"), "logged once")
}

func TestAPanickingFetchIsRecoveredAsAFailure(t *testing.T) {
	t.Parallel()

	r := newRig(t, testutil.NewClock(at()))
	r.fetch.answer(claudeHost, answer{panic: true})

	got := r.svc.CheckNow(t.Context())

	assert.Equal(t, StateUnknown, got.Providers[1].State)
	assert.Equal(t, "Claude's status page answered with something the app can't read.", got.Providers[1].Error)
	assert.Equal(t, StateOperational, got.Providers[0].State, "the others were read")
	assert.Contains(t, r.logs.String(), "panicked")
}
