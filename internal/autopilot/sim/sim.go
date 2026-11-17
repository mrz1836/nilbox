// Package sim is Autopilot's simulator: the real engine and ledger, over a real SQLite
// database (testutil.DB), driven through time by a scripted GitHub (World) the inbox's own
// service reads, a testing/synctest clock with sleep gaps, a fake agent runner, fake
// dispatchers for the other parts, and points where the app crashes and starts again.
//
// Each scenario is a timeline with its expected ledger at the end. It runs inside a
// synctest bubble: Advance lets time pass with the engine's ticks, Sleep jumps over a
// gap with none (the Mac asleep, or the app closed), and Settle waits for every goroutine
// to have done what it can. Only tests may import it.
package sim

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// Start is when a simulation starts: a Monday morning.
func Start() time.Time { return time.Date(2026, time.November, 16, 9, 0, 0, 0, time.UTC) }

// Clock is the simulation's clock: the synctest bubble's time, plus the gaps Sleep jumped
// over, starting at Start.
type Clock struct {
	offset atomic.Int64
	loc    atomic.Pointer[time.Location]
}

// newClock returns a clock at Start.
func newClock() *Clock {
	c := &Clock{}
	c.offset.Store(int64(time.Until(Start())))

	return c
}

// Now returns the simulated time.
func (c *Clock) Now() time.Time {
	loc := c.loc.Load()
	if loc == nil {
		loc = time.UTC
	}

	return time.Now().Add(time.Duration(c.offset.Load())).In(loc)
}

// Set sets the clock to at, in at's location, which every time it tells is in from then
// on, as a Mac's own clock tells local time. Call it before Begin.
func (c *Clock) Set(at time.Time) {
	c.offset.Store(int64(time.Until(at)))
	c.loc.Store(at.Location())
}

// Turn sets the clock d on, or back for a negative d, at once, as the user setting the
// Mac's clock does: the engine's ticks go on as they did.
func (c *Clock) Turn(d time.Duration) {
	c.jump(d)
}

// Travel has the clock tell local time in loc from now on, the moment unchanged, as a Mac
// does when its time zone changes.
func (c *Clock) Travel(loc *time.Location) {
	c.loc.Store(loc)
}

// jump moves the clock on by d, with no tick between.
func (c *Clock) jump(d time.Duration) {
	c.offset.Add(int64(d))
}

// Settings are the simulation's Autopilot settings, which a scenario changes.
type Settings struct {
	mu  sync.Mutex
	cfg autopilotprefs.Config
}

// Autopilot returns the settings.
func (s *Settings) Autopilot(context.Context) (autopilotprefs.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.cfg, nil
}

// Sim is one simulation.
type Sim struct {
	t *testing.T

	DB     *sql.DB
	World  *World
	Clock  *Clock
	Runner *Runner

	// Triage is the inbox's own triage, which the triage steps run through, as core
	// wires it, and GitHub the fake of the sheets the GitHub step goes through, under the
	// real GitHub dispatcher.
	Triage   *Triage
	GitHub   *GitHub
	Settings *Settings
	Lock     string

	// Notices hears what the real notices dispatcher, and the problems, tell the user, and
	// with TellWaits the matches that come to wait for the user too.
	Notices   *Notices
	TellWaits bool

	// Ready, Holds, Outside, and Library are the fakes of what holds an agent's start back
	// (agents.go), and Library a GitHub step's reply too, each left out of the app while
	// nil.
	Ready   *Ready
	Holds   *Holds
	Outside *Outside
	Library *Library

	// Scripts runs the script steps, as Runner runs the agents', and MyScripts are the
	// user's own scripts they pick from; each is left out of the app while nil.
	Scripts   *Runner
	MyScripts *MyScripts

	// Events hears what this copy's engine tells the window (autopilot:updated).
	Events *testutil.Events

	// Logs hears what the app logs.
	Logs *testutil.Logs

	// The app, as built by Start; a crash and a restart build it again over the same
	// database, world, runner, and settings.
	Inbox    *inbox.Service
	Store    *inbox.Store
	Routines *inbox.Routines
	Service  *autopilot.Service
	Engine   *autopilot.Engine

	stop func()
}

// New returns a simulation over a fresh database, not started, with Autopilot on, which a
// scenario turns off with Change. Call it inside a synctest bubble.
func New(t *testing.T) *Sim {
	t.Helper()

	clk := newClock()
	cfg := autopilotprefs.Defaults()
	cfg.Enabled = true

	s := &Sim{
		t: t, DB: testutil.DB(t), Clock: clk, World: NewWorld(clk), Runner: NewRunner(),
		Settings: &Settings{cfg: cfg}, Lock: filepath.Join(t.TempDir(), "autopilot.lock"),
		Logs: &testutil.Logs{}, Notices: &Notices{}, Events: &testutil.Events{},
	}
	s.Triage, s.GitHub = &Triage{sim: s}, newGitHub(s)

	// One connection: every use of the database waits its turn on a channel, which the
	// bubble counts as blocked, rather than in SQLite's busy wait, whose timer can't move
	// while another goroutine waits on a mutex the waiting one holds.
	s.DB.SetMaxOpenConns(1)

	t.Cleanup(s.Stop)

	return s
}

// Copy returns a second copy of the app on the same data folder: the same database,
// world, lock, and settings, with an engine of its own and a runner of its own.
func (s *Sim) Copy() *Sim {
	other := &Sim{
		t: s.t, DB: s.DB, Clock: s.Clock, World: s.World, Runner: NewRunner(),
		Settings: s.Settings, Lock: s.Lock, Logs: s.Logs, Notices: &Notices{},
		Events: &testutil.Events{},
	}
	other.Triage, other.GitHub = &Triage{sim: other}, newGitHub(other)

	s.t.Cleanup(other.Stop)

	return other
}

// Offers runs every kind of step and GitHub action, and an agent in every place, with the
// fake dispatchers and runner.
func Offers() *autopilot.Offers {
	out := &autopilot.Offers{
		Actions: []catalog.GitHubActionKind{
			catalog.GitHubRebase, catalog.GitHubUpdateBranch, catalog.GitHubReply, catalog.GitHubLabels,
		},
	}

	for _, st := range catalog.Steps() {
		out.Steps = append(out.Steps, st.Kind)
	}

	for _, p := range catalog.Places() {
		out.Places = append(out.Places, p.ID)
	}

	return out
}

// Begin builds the app and runs the inbox's watch passes and the engine.
func (s *Sim) Begin() {
	s.t.Helper()

	items := &items{sim: s}
	watches := &watchSource{}
	fan := &fanOut{}

	s.Store = inbox.NewStore(s.DB, s.Clock)
	s.Inbox = inbox.NewService(inbox.Config{
		Pool: s.DB, Tokens: s.World, Client: s.World, Clock: s.Clock, Watches: watches, Outcomes: fan, Events: fan,
		Changed: func(ids []int64) { fan.itemsChanged(ids) }, Keep: fan, Marks: fan, Store: s.Store,
		Logger: s.Logs.Logger(),
	})
	s.Routines = inbox.NewRoutines(s.Inbox)

	svc := autopilot.Config{
		Pool: s.DB, Prefs: s.Settings, Accounts: s.Store, Items: items, Offers: Offers(), GitHub: s.GitHub,
		Clock: s.Clock, Logger: s.Logs.Logger(),
	}
	eng := autopilot.EngineConfig{
		Items: items, Lock: autopilot.NewLock(s.Lock), Runner: s.Runner, Triage: s.Triage,
		Dispatchers: map[catalog.Dispatcher]autopilot.Dispatcher{catalog.DispatchNotices: autopilot.NewNotices(s.Notices)},
		Problems:    autopilot.NewProblemNotices(s.Notices), Finished: s.Notices, Events: s.Events,
	}
	s.config(&svc, &eng)

	s.Service = autopilot.NewService(svc)
	watches.searches = autopilot.NewSearches(s.Service)
	watches.searches.Rebuild(context.Background())

	s.Engine = autopilot.NewEngine(s.Service, eng)
	fan.engine.Store(s.Engine)
	s.Runner.attach(s.Engine)

	if s.Scripts != nil {
		s.Scripts.attach(s.Engine)
	}

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup

	wg.Go(func() { inbox.NewWatchPasses(s.Inbox, s.Logs.Logger()).Run(ctx) })
	wg.Go(func() { s.Engine.Run(ctx) })

	s.stop = func() {
		cancel()
		wg.Wait()
	}

	synctest.Wait()
}

// Stop stops the app, as a quit does; it is safe to call when stopped.
func (s *Sim) Stop() {
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
}

// Crash stops the app where it is: the engine lets go of its lock, as the system does for
// a process that ends, and a run the runner holds goes on.
func (s *Sim) Crash() {
	s.Stop()
}

// Restart crashes the app and starts it again over the same data.
func (s *Sim) Restart() {
	s.Crash()
	s.Begin()
}

// Refresh refreshes the inbox, as the scheduler does, and lets everything settle: the watch
// pass, the engine's cycle, and the workers it starts.
func (s *Sim) Refresh() {
	s.t.Helper()

	_, err := s.Inbox.Refresh(context.Background())
	if err != nil && !strings.Contains(err.Error(), ErrOffline.Error()) && !errors.Is(err, github.ErrNotLoggedIn) {
		require.NoError(s.t, err)
	}

	s.Settle()
}

// Settle waits for every goroutine to have done what it can, and for the engine to have
// applied what its workers brought.
func (s *Sim) Settle() {
	for range 3 {
		synctest.Wait()
		time.Sleep(time.Millisecond)
	}

	synctest.Wait()
}

// Advance lets d pass, the engine's ticks with it, refreshing every five minutes as the
// scheduler does when refresh says so.
func (s *Sim) Advance(d time.Duration, refresh bool) {
	const every = 5 * time.Minute

	for d > 0 {
		step := min(d, every)
		time.Sleep(step)
		d -= step

		if refresh {
			s.Refresh()
		} else {
			s.Settle()
		}
	}
}

// Sleep jumps over d with no tick, as a Mac asleep or an app closed does, then wakes the
// engine, as the next tick would.
func (s *Sim) Sleep(d time.Duration) {
	s.Clock.jump(d)

	if s.Engine != nil {
		s.Engine.Wake()
	}

	s.Settle()
}

// Change changes the settings, as a setter does, and tells the engine.
func (s *Sim) Change(fn func(c *autopilotprefs.Config)) {
	s.Settings.mu.Lock()
	fn(&s.Settings.cfg)
	s.Settings.mu.Unlock()

	if s.Engine != nil {
		s.Engine.Wake()
	}

	s.Settle()
}

// Save saves a routine named name over sp in mode, and returns it.
func (s *Sim) Save(name string, sp spec.Spec, mode autopilotprefs.Mode) autopilot.Routine {
	s.t.Helper()

	r, err := s.Service.Save(context.Background(), autopilot.Draft{Name: name, Spec: sp, Mode: mode})
	require.NoError(s.t, err)

	return r
}

// Enable switches the routine id on, answering the question about its current matches
// with current, and lets the engine settle.
func (s *Sim) Enable(id int64, current autopilot.Current) {
	s.t.Helper()

	_, err := s.Service.SetEnabled(context.Background(), id, true, current)
	require.NoError(s.t, err)
	s.Settle()
}

// Row is one ledger row, as a scenario expects it.
type Row struct {
	Number  int
	State   autopilot.State
	Blocker autopilot.Blocker
	Reason  autopilot.Reason
	DryRun  bool
	Manual  bool
	Key     string
}

// Ledger returns every match recorded, oldest first.
func (s *Sim) Ledger() []Row {
	s.t.Helper()

	page, err := s.Service.Activity(context.Background(), 0, 500)
	require.NoError(s.t, err)

	var out []Row

	for _, day := range page.Days {
		for _, m := range day.Matches {
			out = append(out, Row{
				Number: m.Item.Number, State: m.State, Blocker: m.BlockedBy, Reason: m.Reason, DryRun: m.DryRun,
				Manual: m.Manual, Key: m.FireKey,
			})
		}
	}

	slices.Reverse(out)

	return out
}

// Matches returns every match recorded, as the window shows them, oldest first.
func (s *Sim) Matches() []autopilot.Match {
	s.t.Helper()

	page, err := s.Service.Activity(context.Background(), 0, 500)
	require.NoError(s.t, err)

	var out []autopilot.Match
	for _, day := range page.Days {
		out = append(out, day.Matches...)
	}

	slices.Reverse(out)

	return out
}

// Search is the scenarios' search: Dependabot's pull requests in acme-org.
func Search() spec.Source {
	return spec.Source{Search: &watch.Search{Kind: item.KindPR, Authors: []string{"app/dependabot"}, Where: []string{"org:" + Owner}}}
}

// FixFailing is the flagship's shape: when a watched pull request's checks fail, run an
// agent, then wait for the checks on what it pushed.
func FixFailing() spec.Spec {
	return spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{Search()}},
		Trigger: spec.Trigger{Event: catalog.EventChecksFailed, WaitForChecks: new(false)},
		Steps: []spec.Step{
			{Agent: &spec.AgentStep{Prompt: spec.PromptRef{Starter: "fix-checks"}}},
			{WaitForChecks: &spec.WaitStep{}, OnlyIf: catalog.OutcomePushed},
		},
	}
}

// Reviews is a routine with a Read only agent in the background: review each watched pull
// request as it appears.
func Reviews() spec.Spec {
	return spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{Search()}},
		Trigger: spec.Trigger{Event: catalog.EventAppears},
		Steps: []spec.Step{{Agent: &spec.AgentStep{
			Prompt: spec.PromptRef{Starter: "review"}, Fence: &spec.Fence{Tools: catalog.PresetReadOnly},
		}}},
	}
}

// Pins is a local routine: pin each watched pull request as it appears.
func Pins() spec.Spec {
	return spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{Search()}},
		Trigger: spec.Trigger{Event: catalog.EventAppears},
		Steps:   []spec.Step{{Pin: &spec.Empty{}}},
	}
}

// Rebases is a routine with a GitHub action: ask Dependabot to rebase a pull request that
// falls behind, as each appears.
func Rebases() spec.Spec {
	return spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{Search()}},
		Trigger: spec.Trigger{Event: catalog.EventAppears},
		Steps:   []spec.Step{{GitHub: []spec.GitHubAction{{Rebase: &spec.Empty{}}}}},
	}
}

// items reads the inbox for the routines, as core's adapters do.
type items struct {
	sim *Sim
}

func (a *items) Suggestions(ctx context.Context) (autopilot.Suggestions, error) {
	got, err := a.sim.Store.Suggest(ctx)

	return autopilot.Suggestions{Authors: got.Authors, Bots: got.Bots, Owners: got.Owners, Labels: got.Labels, Checks: got.Checks}, err
}

func (a *items) WatchedItems(ctx context.Context, keys []string, lists []catalog.ListSource) ([]int64, error) {
	return a.sim.Store.WatchedItems(ctx, keys, inbox.Lists{
		ReviewRequested: slices.Contains(lists, catalog.SourceReviewRequested),
		Authored:        slices.Contains(lists, catalog.SourceAuthored),
		Assigned:        slices.Contains(lists, catalog.SourceAssigned),
	})
}

func (a *items) Facts(ctx context.Context, ids []int64, now time.Time) ([]facts.Facts, error) {
	rows, err := a.sim.Store.FactsRows(ctx, ids)
	if err != nil {
		return nil, err
	}

	env, err := a.sim.Store.FactsEnv(ctx, now)
	if err != nil {
		return nil, err
	}

	out := make([]facts.Facts, 0, len(rows))
	for _, r := range rows {
		out = append(out, facts.Build(r, env))
	}

	return out, nil
}

func (a *items) KnownOwners(ctx context.Context) (map[string]bool, error) {
	return a.sim.Store.KnownOwners(ctx)
}

func (a *items) SearchNow(ctx context.Context, queries []string) (map[string]autopilot.SearchResult, int, error) {
	got, points, err := a.sim.Routines.SearchNow(ctx, queries)
	if err != nil {
		return nil, 0, err
	}

	out := make(map[string]autopilot.SearchResult, len(got))
	for q, o := range got {
		out[q] = autopilot.SearchResult{IDs: o.IDs, Count: o.Count, Truncated: o.Truncated, Err: o.Err}
	}

	return out, points, nil
}

func (a *items) ReadChecks(ctx context.Context, ids []int64) (int, error) {
	return a.sim.Routines.ReadChecks(ctx, ids)
}

func (a *items) ItemStates(ctx context.Context, ids []int64) (map[int64]autopilot.ItemState, error) {
	got, err := a.sim.Store.ItemStates(ctx, ids)
	if err != nil {
		return nil, err
	}

	out := make(map[int64]autopilot.ItemState, len(got))
	for id, st := range got {
		out[id] = autopilot.ItemState{State: st.State, FetchedAt: st.FetchedAt, Listed: st.Listed, Watched: st.Watched}
	}

	return out, nil
}

func (a *items) InboxStatus(ctx context.Context) (autopilot.InboxStatus, error) {
	st, err := a.sim.Store.ReadStatus(ctx)
	if err != nil {
		return autopilot.InboxStatus{}, err
	}

	out := autopilot.InboxStatus{
		Viewer: st.Viewer, LastRefresh: st.LastRefresh, WatchedAt: st.WatchedAt, WatchPaused: st.Paused,
		SignedOut: st.SignedOut,
		Searches:  map[string]autopilot.SearchState{},
	}

	for key, ss := range st.Searches {
		out.Searches[key] = autopilot.SearchState{
			Answered: ss.Answered, At: ss.At, Error: ss.Error, Truncated: ss.Truncated, Partial: ss.Partial,
		}
	}

	return out, nil
}

func (a *items) ReadStates(ctx context.Context, ids []int64) error {
	got, err := a.sim.Store.ItemStates(ctx, ids)
	if err != nil {
		return err
	}

	refs := make([]item.Ref, 0, len(got))
	for _, st := range got {
		refs = append(refs, st.Ref)
	}

	read := a.sim.Store.StartRead()

	states, err := a.sim.World.FetchItemStates(ctx, github.Token{}, refs)
	if err != nil {
		return err
	}

	return a.sim.Store.NoteStates(ctx, read, refs, states)
}

func (a *items) LocalStates(ctx context.Context, ids []int64) (map[int64]string, error) {
	return a.sim.Routines.LocalStates(ctx, ids)
}

func (a *items) ReadItem(ctx context.Context, id int64) error {
	_, err := a.sim.Inbox.RefreshItem(ctx, id)

	return err
}

func (a *items) WatchNow(ctx context.Context) error {
	return a.sim.Routines.WatchNow(ctx)
}

// watchSource gives the inbox the routines' searches, as core's adapter does.
type watchSource struct {
	searches *autopilot.Searches
}

func (w *watchSource) HoldsWatches() bool {
	return w.searches.HoldsWatches()
}

func (w *watchSource) Watches(login string) []inbox.Watch {
	set := w.searches.Current()
	if !strings.EqualFold(set.Login, login) {
		set = w.searches.Rebuild(context.Background())
	}

	out := make([]inbox.Watch, 0, len(set.Searches))
	for _, sr := range set.Searches {
		out = append(out, inbox.Watch{Key: sr.Key, Query: sr.Query, Follow: sr.Follow})
	}

	return out
}

// fanOut tells the engine what the inbox stored and saw, and names the items its ledger
// keeps, as core's adapters do.
type fanOut struct {
	engine atomic.Pointer[autopilot.Engine]
}

// Emit wakes the engine when the inbox stored a change, as core's relay does.
func (f *fanOut) Emit(e event.Event) {
	if _, ok := e.(event.InboxUpdated); ok {
		if eng := f.engine.Load(); eng != nil {
			eng.Wake()
		}
	}
}

func (f *fanOut) Settle(ctx context.Context, outcomes []inbox.Outcome) {
	e := f.engine.Load()
	if e == nil {
		return
	}

	told := make([]autopilot.Outcome, 0, len(outcomes))
	for _, o := range outcomes {
		told = append(told, autopilot.Outcome{
			Kind: string(o.Kind), Ref: o.Ref, At: o.At,
			Reversed: autopilot.Reversal{
				RoutineID: o.Reversed.Routine, ItemID: o.Reversed.Item, Action: catalog.StepKind(o.Reversed.Did),
			},
		})
	}

	e.Outcomes(ctx, told)
}

func (f *fanOut) KeptItems(ctx context.Context) ([]int64, error) {
	if e := f.engine.Load(); e != nil {
		return e.KeptItems(ctx)
	}

	return nil, nil
}

func (f *fanOut) KeptMarks(ctx context.Context) ([]int64, error) {
	if e := f.engine.Load(); e != nil {
		return e.KeptMarks(ctx)
	}

	return nil, nil
}

func (f *fanOut) itemsChanged(ids []int64) {
	if e := f.engine.Load(); e != nil {
		e.ItemsChanged(ids)
	}
}
