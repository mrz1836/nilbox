package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// errOffline stands for GitHub out of reach, and errWrongRepos for a lookup of the wrong
// repositories.
var (
	errOffline    = errors.New("offline")
	errWrongRepos = errors.New("asked about the wrong repositories")
)

// fakeEnv is the user's shell's environment, read or not.
type fakeEnv struct {
	mu    sync.Mutex
	env   []string
	read  bool
	reads int
	ready chan struct{}
}

func newFakeEnv(read bool) *fakeEnv {
	return &fakeEnv{env: []string{"HOME=/Users/example"}, read: read, ready: make(chan struct{})}
}

func (f *fakeEnv) Env() ([]string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.env, f.read
}

func (f *fakeEnv) Ready() <-chan struct{} { return f.ready }

func (f *fakeEnv) Read(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.reads++

	return nil
}

// fakeReleases answers the latest-release lookups.
type fakeReleases struct {
	mu    sync.Mutex
	got   map[string]Release
	err   error
	calls int
}

func (f *fakeReleases) LatestReleases(_ context.Context, repos []repo.Name) (map[string]Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++

	if len(repos) != 2 {
		return nil, errWrongRepos
	}

	return f.got, f.err
}

func (f *fakeReleases) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

// harness is a Service over a fake machine, environment, and GitHub.
type harness struct {
	svc      *Service
	m        *machine
	env      *fakeEnv
	releases *fakeReleases
	events   *testutil.Events
	clock    *testutil.Clock
	prefs    *prefs.Store
	logs     *testutil.Logs
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	h := &harness{
		m: newMachine(t), env: newFakeEnv(true), events: &testutil.Events{}, logs: &testutil.Logs{},
		clock: testutil.NewClock(time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)),
		releases: &fakeReleases{got: map[string]Release{
			"mrz1836/mage-x": {Tag: "v1.30.2", URL: "https://github.com/mrz1836/mage-x/releases/tag/v1.30.2"},
		}},
	}

	h.prefs = prefs.New(testutil.DB(t), h.clock)
	h.svc = NewService(Config{
		Prefs: h.prefs, Env: h.env, Releases: h.releases, Home: h.m.home, Clock: h.clock, Events: h.events,
		Logger: h.logs.Logger(),
	})
	h.svc.run = h.m.run
	h.svc.dirsFor = func([]string, string) searchDirs { return h.m.dirs() }

	return h
}

// list lists the tools and returns them by ID.
func (h *harness) list(t *testing.T) (Tools, map[integrationprefs.ID]Tool) {
	t.Helper()

	got, err := h.svc.List(t.Context())
	require.NoError(t, err)

	byID := map[integrationprefs.ID]Tool{}
	for _, tl := range got.Tools {
		byID[tl.ID] = tl
	}

	return got, byID
}

func TestListServesTheLastDetectionUntilThisRunsEnds(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	kept := recordOf(detection{
		has: true, selected: Copy{Path: "/opt/tools/magex", DisplayPath: "/opt/tools/magex", Runs: true, Version: Version{Raw: recordedMagex}},
	}, h.clock.Now().Add(-time.Hour))
	raw, err := json.Marshal(kept)
	require.NoError(t, err)
	require.NoError(t, h.prefs.Set(t.Context(), "tools.mage-x.seen", string(raw)))

	h.m.put(h.m.pathA, magexSays(newerMagex))

	all, tools := h.list(t)
	assert.False(t, all.ShellChecked, "the shell hasn't been read yet")
	assert.True(t, tools["mage-x"].Checking)
	assert.Equal(t, StateFound, tools["mage-x"].State, "the last-known state, at once")
	assert.Equal(t, "/opt/tools/magex", tools["mage-x"].Copy.Path)
	assert.Equal(t, "2026-11-10T08:00:00Z", tools["mage-x"].CheckedAt)
	assert.True(t, tools["go-pre-commit"].Checking)
	assert.Equal(t, StateMissing, tools["go-pre-commit"].State, "never seen")
	assert.NotNil(t, tools["go-pre-commit"].Others, "never a nil slice")
	assert.Empty(t, h.events.All(), "List looks for nothing itself")

	require.NoError(t, h.svc.detect(t.Context()))
	assert.Equal(t, []event.Event{event.ToolsUpdated{}}, h.events.All(), "the window is told")

	close(h.env.ready)

	all, tools = h.list(t)
	assert.True(t, all.ShellChecked)
	assert.True(t, all.ShellRead)
	assert.False(t, tools["mage-x"].Checking)
	assert.Equal(t, "1.30.2", tools["mage-x"].Copy.Version.Raw)
	assert.Equal(t, "2026-11-10T09:00:00Z", tools["mage-x"].CheckedAt)

	stored, ok, err := h.prefs.Get(t.Context(), "tools.mage-x.seen")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "1.30.2", decodeSeen(stored).Copy.Version.Raw, "kept for the next launch")
}

func TestListShowsEachTool(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.m.put(h.m.pathA, magexSays(recordedMagex))
	require.NoError(t, h.svc.detect(t.Context()))

	_, tools := h.list(t)
	assert.Equal(t, Tool{
		ID: "mage-x", Name: "mage-x", Binary: "magex", Purpose: "Release new versions of your repositories in two clicks.",
		Repo: "mrz1836/mage-x", ReleasesURL: "https://github.com/mrz1836/mage-x/releases", MinVersion: recordedMagex,
		Switch: integrationprefs.SwitchAuto, On: true, State: StateFound,
		Copy: Copy{
			Path: filepath.Join(h.m.pathA, "magex"), DisplayPath: filepath.Join(h.m.pathA, "magex"), Runs: true,
			Version: Version{Raw: recordedMagex, Commit: "9a61f25"}, Source: SourceRelease, OnPath: true,
		},
		Others: []Copy{}, CheckedAt: "2026-11-10T09:00:00Z",
	}, tools["mage-x"])
}

func TestListAppliesTheSwitches(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.m.put(h.m.pathA, magexSays(recordedMagex))
	require.NoError(t, h.prefs.SetMany(t.Context(), map[string]string{
		"integrations.mage-x.enabled": "off", "integrations.go-pre-commit.enabled": "on",
		"integrations.go-pre-commit.path": "/opt/gone/go-pre-commit",
	}))
	require.NoError(t, h.svc.detect(t.Context()))

	_, tools := h.list(t)
	assert.False(t, tools["mage-x"].On, "off, though found")
	assert.Equal(t, StateFound, tools["mage-x"].State)
	assert.True(t, tools["go-pre-commit"].On, "on stays on while missing, to show the problem")
	assert.Equal(t, StateMissing, tools["go-pre-commit"].State)
	assert.Equal(t, "/opt/gone/go-pre-commit", tools["go-pre-commit"].Chosen)
	assert.True(t, tools["go-pre-commit"].ChosenMissing)
}

func TestEffectiveState(t *testing.T) {
	t.Parallel()

	// D3: on and off are what the user chose; automatic is on while the tool is usable.
	tests := []struct {
		sw     integrationprefs.Switch
		usable bool
		want   bool
	}{
		{integrationprefs.SwitchAuto, true, true},
		{integrationprefs.SwitchAuto, false, false},
		{integrationprefs.SwitchOn, true, true},
		{integrationprefs.SwitchOn, false, true},
		{integrationprefs.SwitchOff, true, false},
		{integrationprefs.SwitchOff, false, false},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, Effective(tt.sw, tt.usable), "%s, usable %v", tt.sw, tt.usable)
	}

	for state, usable := range map[State]bool{
		StateFound: true, StateUpdate: true, StateTooOld: false, StateWontRun: false, StateMissing: false,
	} {
		assert.Equal(t, usable, state.Usable(), state)
	}

	// The scripts' automatic is on while either tool is on.
	assert.True(t, ScriptsOn(integrationprefs.SwitchAuto, true, false))
	assert.True(t, ScriptsOn(integrationprefs.SwitchAuto, false, true))
	assert.False(t, ScriptsOn(integrationprefs.SwitchAuto, false, false))
	assert.True(t, ScriptsOn(integrationprefs.SwitchOn, false, false))
	assert.False(t, ScriptsOn(integrationprefs.SwitchOff, true, true))
}

func TestUpdatesAreCheckedOnceADay(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.m.put(h.m.pathA, magexSays(recordedMagex))
	require.NoError(t, h.svc.detect(t.Context()))

	require.NoError(t, h.svc.checkUpdates(t.Context(), updateEvery))
	assert.Equal(t, 1, h.releases.count())

	_, tools := h.list(t)
	assert.Equal(t, StateUpdate, tools["mage-x"].State)
	assert.Equal(t, Latest{
		Version: "1.30.2", URL: "https://github.com/mrz1836/mage-x/releases/tag/v1.30.2", CheckedAt: "2026-11-10T09:00:00Z",
	}, tools["mage-x"].Latest)
	assert.Equal(t, Latest{CheckedAt: "2026-11-10T09:00:00Z"}, tools["go-pre-commit"].Latest, "no release, but checked")

	h.clock.Advance(23 * time.Hour)
	require.NoError(t, h.svc.checkUpdates(t.Context(), updateEvery))
	assert.Equal(t, 1, h.releases.count(), "not again within a day")

	h.clock.Advance(time.Hour)
	require.NoError(t, h.svc.checkUpdates(t.Context(), updateEvery))
	assert.Equal(t, 2, h.releases.count(), "once a day")

	_, err := h.svc.CheckAgain(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, h.releases.count(), "Check again doesn't ask again within ten minutes")

	h.clock.Advance(recheckAfter)
	_, err = h.svc.CheckAgain(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 3, h.releases.count(), "but does after them")
}

func TestAFailedUpdateCheckKeepsTheLastAnswer(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NoError(t, h.svc.checkUpdates(t.Context(), updateEvery))

	h.releases.mu.Lock()
	h.releases.err = errOffline
	h.releases.mu.Unlock()
	h.clock.Advance(25 * time.Hour)

	require.ErrorIs(t, h.svc.checkUpdates(t.Context(), updateEvery), errOffline)
	assert.Contains(t, h.logs.String(), "couldn't check the tools for updates")

	_, tools := h.list(t)
	assert.Equal(t, "1.30.2", tools["mage-x"].Latest.Version, "the last answer stays")

	_, err := h.svc.CheckAgain(t.Context())
	require.NoError(t, err, "a failed check isn't the settings' failure")
}

func TestNoUpdateCheckWithoutGitHub(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.svc.releases = nil

	require.NoError(t, h.svc.checkUpdates(t.Context(), 0))
	assert.Empty(t, h.events.All())
}

func TestCheckAgainReadsTheShellAgain(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.m.put(h.m.pathA, magexSays(recordedMagex))

	got, err := h.svc.CheckAgain(t.Context())
	require.NoError(t, err)

	h.env.mu.Lock()
	reads := h.env.reads
	h.env.mu.Unlock()

	assert.Equal(t, 1, reads)
	assert.Equal(t, 1, h.releases.count(), "and checks for updates")
	require.Len(t, got.Tools, 2)
	assert.False(t, got.Tools[0].Checking)
	assert.Equal(t, StateUpdate, got.Tools[0].State)
}

func TestListAsksForANewDetectionWhenStale(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NoError(t, h.svc.detect(t.Context()))

	h.list(t)
	assert.Empty(t, h.svc.wake, "a fresh detection is served as it is")

	h.clock.Advance(11 * time.Minute)
	h.list(t)
	assert.Len(t, h.svc.wake, 1, "an old one asks the worker to look again")
}

// signalSink passes on each event's name.
type signalSink struct {
	names chan string
}

func (s signalSink) Emit(e event.Event) {
	s.names <- e.Name()
}

func TestWorkerLooksAfterTheShellIsReadAndWhenWoken(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	sink := signalSink{names: make(chan string, 16)}
	h.svc.events = sink
	h.m.put(h.m.pathA, magexSays(recordedMagex))

	w := NewWorker(h.svc)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		w.Run(ctx)
		close(done)
	}()

	// It waits for the shell's environment (see TestWorkerStopsBeforeTheShellIsRead).
	close(h.env.ready)
	assert.Equal(t, event.NameToolsUpdated, <-sink.names, "it looks for the tools")
	assert.Equal(t, event.NameToolsUpdated, <-sink.names, "and checks for updates")

	w.Wake()
	assert.Equal(t, event.NameToolsUpdated, <-sink.names, "woken, it looks again")
	assert.Equal(t, 1, h.releases.count(), "but checks for updates only once a day")

	cancel()
	<-done
}

func TestWorkerStopsBeforeTheShellIsRead(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	NewWorker(h.svc).Run(ctx)
	assert.Empty(t, h.events.All())
}

func TestANewlyChosenCopyIsCheckingUntilItIsLookedFor(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.m.put(h.m.pathA, magexSays(recordedMagex))
	require.NoError(t, h.prefs.Set(t.Context(), integrationprefs.KeyPath(integrationprefs.MageX), "/opt/gone/magex"))
	require.NoError(t, h.svc.detect(t.Context()))

	_, tools := h.list(t)
	assert.True(t, tools["mage-x"].ChosenMissing)
	assert.False(t, tools["mage-x"].Checking)

	// The user goes back to the copy their shell finds; the finder hasn't looked yet.
	require.NoError(t, h.prefs.Set(t.Context(), integrationprefs.KeyPath(integrationprefs.MageX), ""))

	_, tools = h.list(t)
	assert.True(t, tools["mage-x"].Checking, "what was found is from before the change")
	assert.False(t, tools["mage-x"].ChosenMissing, "and says nothing about the copy chosen before")
	assert.Empty(t, tools["mage-x"].Chosen)

	require.NoError(t, h.svc.detect(t.Context()))

	_, tools = h.list(t)
	assert.False(t, tools["mage-x"].Checking)
}

func TestAWokenDetectionIsChecking(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NoError(t, h.svc.detect(t.Context()))

	_, tools := h.list(t)
	assert.False(t, tools["mage-x"].Checking)

	NewWorker(h.svc).Wake()

	_, tools = h.list(t)
	assert.True(t, tools["mage-x"].Checking, "a detection is asked for")
	assert.True(t, tools["go-pre-commit"].Checking)

	require.NoError(t, h.svc.detect(t.Context()))

	_, tools = h.list(t)
	assert.False(t, tools["mage-x"].Checking, "and done")
}
