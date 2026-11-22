package autopilot_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// The Dependabot searches the tests' routines watch: in the account signed in, and in
// acme-org.
var (
	mineSearch = watch.Search{Kind: item.KindPR, Authors: []string{"app/dependabot"}, Where: []string{watch.Me}}       //nolint:gochecknoglobals // fixed test values
	orgSearch  = watch.Search{Kind: item.KindPR, Authors: []string{"app/dependabot"}, Where: []string{"org:acme-org"}} //nolint:gochecknoglobals // fixed test values
)

// errLocked is an inbox that can't be read.
var errLocked = errors.New("the database is locked")

// searchSpec is a routine that notifies on its search's items when trigger fires.
func searchSpec(trigger catalog.Event, searches ...watch.Search) spec.Spec {
	s := notifySpec()
	s.Trigger = spec.Trigger{Event: trigger}
	s.Watch.Sources = nil

	for _, sr := range searches {
		s.Watch.Sources = append(s.Watch.Sources, spec.Source{Search: &sr})
	}

	return s
}

// query builds a search for the tests' viewer.
func query(t *testing.T, s watch.Search) watch.Query {
	t.Helper()

	q, err := watch.Build(s, viewer)
	require.NoError(t, err)

	return q
}

// on saves a routine named name over s and switches it on.
func (r *rig) on(t *testing.T, name string, s spec.Spec) autopilot.Routine {
	t.Helper()

	saved := r.save(t, draft(name, s))

	got, err := r.svc.SetEnabled(t.Context(), saved.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)

	return got
}

func TestTheWatchSetHoldsTheEnabledRoutinesSearchesOnce(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeDryRun)
	assert.Empty(t, autopilot.NewSearches(r.svc).Current().Searches, "nothing before a routine")

	fails := r.on(t, "Failing Dependabot", searchSpec(catalog.EventChecksFailed, mineSearch, orgSearch))
	appears := r.on(t, "New Dependabot", searchSpec(catalog.EventAppears, orgSearch))
	r.save(t, draft("Off", searchSpec(catalog.EventAppears, watch.Search{Where: []string{"org:other-org"}})))

	set := autopilot.NewSearches(r.svc).Current()
	assert.Equal(t, viewer, set.Login)
	require.Len(t, set.Searches, 2, "the shared search once, the routine that is off not at all")

	mine, org := set.Searches[0], set.Searches[1]
	assert.Equal(t, query(t, mineSearch).Key, mine.Key)
	assert.Contains(t, mine.Query, "user:"+viewer, "your account is the account signed in")
	assert.True(t, mine.Follow, "Checks fail reads the checks")
	assert.Equal(t, []int64{fails.ID}, mine.Routines)

	assert.Equal(t, query(t, orgSearch).Key, org.Key)
	assert.True(t, org.Follow, "one of its routines reads the checks")
	assert.Equal(t, []int64{fails.ID, appears.ID}, org.Routines)

	_, err := r.svc.SetEnabled(t.Context(), fails.ID, false, autopilot.CurrentRun)
	require.NoError(t, err)

	set = autopilot.NewSearches(r.svc).Current()
	require.Len(t, set.Searches, 1)
	assert.False(t, set.Searches[0].Follow, "Appears doesn't read the checks")
}

func TestARoutineThatWaitsForTheChecksFollowsWhatItsSearchesReturn(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAuto)

	sp := searchSpec(catalog.EventAppears, orgSearch)
	sp.Steps = verifySpec().Steps

	saved := r.on(t, "Fix and check", sp)
	assert.Equal(t, autopilotprefs.ModeAuto, saved.Mode, "this build follows an agent's checks, so it needs no Dry run")

	set := autopilot.NewSearches(r.svc).Current()
	require.Len(t, set.Searches, 1)
	assert.True(t, set.Searches[0].Follow, "the checks on its agent's push are read every round")
}

func TestTheWatchSetLeavesOutAnotherAccountsAndBrokenRoutines(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeDryRun)
	r.on(t, "Mine", searchSpec(catalog.EventAppears, mineSearch))

	r.accounts.signIn("octo-other")

	set := autopilot.NewSearches(r.svc).Rebuild(t.Context())
	assert.Equal(t, "octo-other", set.Login)
	assert.Empty(t, set.Searches, "a routine of another account doesn't run")

	r.accounts.signIn(viewer)
	r.insertRaw(t, "Newer", string(autopilotprefs.ModeDryRun), `{"version":99}`)
	testutil.Exec(t, r.db, `UPDATE autopilot_routines SET enabled = 1, enabled_at = ? WHERE name = 'Newer'`, startedAt())

	set = autopilot.NewSearches(r.svc).Rebuild(t.Context())
	assert.Len(t, set.Searches, 1, "a broken routine is left out")
}

func TestARebuildThatCantReadKeepsTheSearchesAsTheyWere(t *testing.T) {
	t.Parallel()

	t.Run("the routines", func(t *testing.T) {
		t.Parallel()

		r := newRig(t, autopilotprefs.ModeDryRun)
		r.on(t, "New Dependabot", searchSpec(catalog.EventAppears, orgSearch))

		searches := autopilot.NewSearches(r.svc)
		before := searches.Current()
		require.Len(t, before.Searches, 1)

		testutil.Exec(t, r.db, `ALTER TABLE autopilot_routines RENAME TO autopilot_routines_away`)

		assert.Equal(t, before, searches.Rebuild(t.Context()), "the inbox goes on reading the searches it had")
		assert.Equal(t, before, searches.Current())
		assert.Contains(t, r.logs.String(), "could not read the routines for their searches")
	})

	t.Run("whether Autopilot is on", func(t *testing.T) {
		t.Parallel()

		logs := &testutil.Logs{}
		svc := autopilot.NewService(autopilot.Config{
			Pool: testutil.DB(t), Prefs: prefs{err: errLocked}, Accounts: &accounts{login: viewer}, Logger: logs.Logger(),
		})

		set := autopilot.NewSearches(svc).Rebuild(t.Context())

		assert.Equal(t, autopilot.WatchSet{Searches: []autopilot.WatchSearch{}}, set, "none before a first build")
		assert.Contains(t, logs.String(), "could not read whether Autopilot is on for the routines' searches")
	})
}

// fakeItems is a fake inbox: suggestions, watched items, facts, and searches it answers.
type fakeItems struct {
	mu sync.Mutex

	suggestions autopilot.Suggestions
	watched     map[string][]int64
	listed      map[catalog.ListSource][]int64
	rows        map[int64]facts.Row
	owners      map[string]bool
	searches    map[string]autopilot.SearchResult
	checks      map[int64]*facts.ChecksRow

	searched [][]string
	read     [][]int64
	err      error

	// searchErr fails SearchNow alone, and checksErr ReadChecks, as the inbox does while
	// the budget is low.
	searchErr error
	checksErr error
}

func (f *fakeItems) Suggestions(context.Context) (autopilot.Suggestions, error) {
	return f.suggestions, f.err
}

func (f *fakeItems) WatchedItems(_ context.Context, keys []string, lists []catalog.ListSource) ([]int64, error) {
	var out []int64

	for _, l := range lists {
		out = append(out, f.listed[l]...)
	}

	for _, k := range keys {
		out = append(out, f.watched[k]...)
	}

	return out, f.err
}

func (f *fakeItems) Facts(_ context.Context, ids []int64, now time.Time) ([]facts.Facts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]facts.Facts, 0, len(ids))

	for _, id := range ids {
		row, ok := f.rows[id]
		if !ok {
			continue
		}

		if c, read := f.checks[id]; read && slices.Contains(f.readIDs(), id) {
			row.Checks = c
		}

		out = append(out, facts.Build(row, facts.Env{Now: now, Viewer: viewer}))
	}

	return out, nil
}

func (f *fakeItems) KnownOwners(context.Context) (map[string]bool, error) {
	return f.owners, nil
}

func (f *fakeItems) SearchNow(_ context.Context, queries []string) (map[string]autopilot.SearchResult, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.searched = append(f.searched, slices.Clone(queries))

	out := map[string]autopilot.SearchResult{}
	for _, q := range queries {
		out[q] = f.searches[q]
	}

	if f.searchErr != nil {
		return nil, 0, f.searchErr
	}

	return out, 2, f.err
}

func (f *fakeItems) ReadChecks(_ context.Context, ids []int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.checksErr != nil {
		return 0, f.checksErr
	}

	f.read = append(f.read, slices.Clone(ids))

	return 1, nil
}

// readIDs is every ID whose checks were read. The caller holds mu.
func (f *fakeItems) readIDs() []int64 {
	var out []int64
	for _, r := range f.read {
		out = append(out, r...)
	}

	return out
}

// fakeCosts estimates a point a request, packed one search a request.
type fakeCosts struct{}

func (fakeCosts) Estimate(_ context.Context, counts []int) (autopilot.Estimate, error) {
	return autopilot.Estimate{
		Requests: len(counts), PointsPerRefresh: len(counts), PointsPerHour: 12 * len(counts),
		Percent: float64(12*len(counts)) * 100 / 5000,
	}, nil
}

// itemsRig is a rig whose Service reads a fake inbox.
func itemsRig(t *testing.T, items *fakeItems) *rig {
	t.Helper()

	r := newRig(t, autopilotprefs.ModeDryRun)
	r.svc = autopilot.NewService(autopilot.Config{
		Pool: r.db, Prefs: prefs{cfg: onSettings()}, Accounts: r.accounts, Names: names{},
		File: autopilot.NewFile(filepath.Join(t.TempDir(), "autopilot.json"), "v0.0.0-test", r.clock), Clock: r.clock,
		Logger: r.logs.Logger(), Items: items, Costs: fakeCosts{},
	})

	return r
}

// failingPR is a pull request of Dependabot's whose checks failed and settled, with its
// check names not read yet.
func failingPR(id int64, number int) facts.Row {
	done := startedAt().Add(-time.Hour)

	return facts.Row{
		ID: id, Kind: item.KindPR, Repo: repo.Name{Owner: "acme-org", Repo: "example-repo"}, Number: number,
		Title: "Bump example.com/lib from 1.0.0 to 1.0.1", Author: "dependabot", AuthorType: "Bot",
		CreatedAt: done, UpdatedAt: done,
		Details: &facts.DetailsRow{
			HeadOID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ChecksState: "FAILURE", ChecksFailed: 1, ChecksPassed: 3,
		},
	}
}

func TestSuggestReturnsWhatTheInboxHolds(t *testing.T) {
	t.Parallel()

	items := &fakeItems{suggestions: autopilot.Suggestions{
		Authors: []string{"octo-reviewer"}, Bots: []string{"dependabot"}, Owners: []string{"acme-org"},
	}}
	r := itemsRig(t, items)

	got, err := r.svc.Suggest(t.Context())
	require.NoError(t, err)
	assert.Equal(t, autopilot.Suggestions{
		Authors: []string{"octo-reviewer"}, Bots: []string{"dependabot"}, Owners: []string{"acme-org"},
		Labels: []string{}, Checks: []string{},
	}, got, "no list is nil")

	none, err := newRig(t, autopilotprefs.ModeDryRun).svc.Suggest(t.Context())
	require.NoError(t, err)
	assert.NotNil(t, none.Labels, "a Service with no inbox suggests nothing")
}

func TestWatchingReturnsTheRoutinesItems(t *testing.T) {
	t.Parallel()

	items := &fakeItems{
		watched: map[string][]int64{query(t, orgSearch).Key: {7, 8}},
		listed:  map[catalog.ListSource][]int64{catalog.SourceAuthored: {1}},
		rows:    map[int64]facts.Row{1: failingPR(1, 10), 7: failingPR(7, 70), 8: failingPR(8, 80)},
	}
	r := itemsRig(t, items)

	both := searchSpec(catalog.EventAppears, orgSearch)
	both.Watch.Sources = append(both.Watch.Sources, spec.Source{Lists: []catalog.ListSource{catalog.SourceAuthored}})
	routine := r.save(t, draft("Both", both))

	got, err := r.svc.Watching(t.Context(), routine.ID)
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 7, 8}, got)

	_, err = r.svc.Watching(t.Context(), 999)
	require.ErrorIs(t, err, autopilot.ErrNotFound)
}

func TestWatchingListsOnlyWhatItsConditionsCount(t *testing.T) {
	t.Parallel()

	// passingPR is a pull request of the user's titled title, its checks passing, or still
	// running when running.
	passingPR := func(id int64, title string, running bool) facts.Row {
		row := failingPR(id, int(id)*10)
		row.Title, row.Author, row.AuthorType = title, viewer, "User"
		row.Details.ChecksState, row.Details.ChecksFailed, row.Details.ChecksPassed = "SUCCESS", 0, 4

		if running {
			row.Details.ChecksState, row.Details.ChecksPending = "PENDING", 1
		}

		return row
	}

	items := &fakeItems{
		listed: map[catalog.ListSource][]int64{catalog.SourceAuthored: {1, 2, 3, 4}},
		rows: map[int64]facts.Row{
			1: passingPR(1, "[Sync] Update project files from source repository (6207e6a)", false),
			2: passingPR(2, "[Deps] Upgrade Go dependencies within major versions", false),
			3: passingPR(3, "[Sync] Update project files from source repository (91c0d2f)", true),
		},
	}
	r := itemsRig(t, items)

	sp := notifySpec()
	sp.Trigger = spec.Trigger{Event: catalog.EventChecksPassed}
	sp.Conditions = spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{
		{Field: catalog.FieldTitle, Op: catalog.OpStartsWith, Value: "[Sync] Update project files from source"},
	}}
	routine := r.save(t, draft("Sync", sp))

	got, err := r.svc.Watching(t.Context(), routine.ID)
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 3}, got, "the title rules #2 out; #3 waits on its checks, which its trigger watches "+
		"for; #4 isn't stored")

	// A condition that waits on a read can't rule an item out.
	names := notifySpec()
	names.Conditions = spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{
		{Field: catalog.FieldID("checks.failedNames"), Op: catalog.OpAnyContains, Values: []string{"security"}},
	}}
	items.rows[5] = failingPR(5, 50)
	items.listed[catalog.SourceAuthored] = []int64{5}
	unread := r.save(t, draft("Security", names))

	got, err = r.svc.Watching(t.Context(), unread.ID)
	require.NoError(t, err)
	assert.Equal(t, []int64{5}, got, "its check names aren't read yet")
}

func TestWatchingDecidesWithTheRoutinesHistory(t *testing.T) {
	t.Parallel()

	items := &fakeItems{
		listed: map[catalog.ListSource][]int64{catalog.SourceAuthored: {7, 8}},
		rows:   map[int64]facts.Row{7: failingPR(7, 70), 8: failingPR(8, 80)},
	}
	r := itemsRig(t, items)

	sp := notifySpec()
	sp.Conditions = spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{
		{Field: catalog.FieldLastOutcome, Op: catalog.OpNoneOf, Values: []string{catalog.OutcomeDone}},
	}}
	routine := r.save(t, draft("Once", sp))

	now := startedAt()
	testutil.Exec(t, r.db, `INSERT INTO accounts (id, login, created_at, updated_at) VALUES (1, ?, ?, ?)`, viewer, now, now)
	testutil.Exec(t, r.db, `INSERT INTO repos (id, owner, name, created_at, updated_at)
		VALUES (1, 'acme-org', 'example-repo', ?, ?)`, now, now)
	testutil.Exec(t, r.db, `INSERT INTO items (id, account_id, repo_id, kind, number, item_created_at, item_updated_at,
		fetched_at) VALUES (7, 1, 1, 'pr', 70, ?, ?, ?)`, now, now, now)
	testutil.Exec(t, r.db, `INSERT INTO autopilot_matches (routine_id, item_id, repo_owner, repo_name, kind, number,
		fire_key, state, reason, created_at, updated_at, decided_at, closed_at)
		VALUES (?, 7, 'acme-org', 'example-repo', 'pr', 70, 'checks_failed:commit:old', 'done', 'ran', ?, ?, ?, ?)`,
		routine.ID, now, now, now, now)

	got, err := r.svc.Watching(t.Context(), routine.ID)
	require.NoError(t, err)
	assert.Equal(t, []int64{8}, got, "its last run on #7 is done, as the ledger records it")
}

func TestTestTriesTheDraftOnItsItemsNow(t *testing.T) {
	t.Parallel()

	q := query(t, orgSearch)
	items := &fakeItems{
		searches: map[string]autopilot.SearchResult{q.Text: {IDs: []int64{7, 8, 9}, Count: 3}},
		rows:     map[int64]facts.Row{7: failingPR(7, 70), 8: failingPR(8, 80), 9: failingPR(9, 90)},
		owners:   map[string]bool{"acme-org": true},
		checks: map[int64]*facts.ChecksRow{
			7: {Head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Complete: true, Checks: []facts.CheckRow{
				{Name: "security", Outcome: facts.CheckFailed, CompletedAt: startedAt().Add(-time.Hour)},
			}},
			9: {Head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Complete: true, Checks: []facts.CheckRow{
				{Name: "lint", Workflow: "ci", Outcome: facts.CheckFailed, CompletedAt: startedAt().Add(-time.Hour)},
			}},
		},
	}
	r := itemsRig(t, items)

	d := draft("Security", searchSpec(catalog.EventChecksFailed, orgSearch))
	d.Spec.Conditions = spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{
		{Field: catalog.FieldID("checks.failedNames"), Op: catalog.Op("anyContains"), Values: []string{"security"}},
	}}

	got, err := r.svc.Test(t.Context(), d)
	require.NoError(t, err)

	require.Len(t, got.Searches, 1)
	assert.Equal(t, q.Text, got.Searches[0].Query)
	assert.Equal(t, 3, got.Searches[0].Count)
	assert.Empty(t, got.Searches[0].Warnings, "the inbox has seen acme-org")
	assert.Equal(t, 3, got.Points, "the searches' two points and the check names' one")
	assert.False(t, got.Cached)
	assert.Equal(t, 3, got.Watched)
	assert.Equal(t, [][]int64{{7, 8, 9}}, items.read, "the check names its decision needs are read, once")

	require.Len(t, got.Items, 3)
	assert.Equal(t, int64(7), got.Items[0].ID, "the match first")
	assert.Equal(t, eval.Yes, got.Items[0].Match)
	assert.NotEmpty(t, got.Items[0].FireKey)
	assert.Equal(t, 1, got.Matched)
	assert.Equal(t, autopilot.Estimate{Requests: 1, PointsPerRefresh: 1, PointsPerHour: 12, Percent: 0.24}, got.Estimate)

	// Every row says why: the match and the miss alike carry each step of the decision,
	// the condition with the value it compared.
	condition := func(it autopilot.TestItem) eval.Trace {
		t.Helper()

		for _, tr := range it.Trace {
			if tr.Path == "conditions.of[0]" {
				return tr
			}
		}

		require.FailNow(t, "no trace of the condition", "#%d: %+v", it.Number, it.Trace)

		return eval.Trace{}
	}

	assert.Equal(t, "trigger", got.Items[0].Trace[0].Path, "the trigger's step first")
	matched := condition(got.Items[0])
	assert.Equal(t, eval.Yes, matched.Result)
	assert.Contains(t, matched.Actual, "security")

	rows := map[int64]autopilot.TestItem{}
	for _, it := range got.Items {
		rows[it.ID] = it
	}

	assert.Equal(t, eval.No, rows[9].Match, "the miss")
	missed := condition(rows[9])
	assert.Equal(t, eval.No, missed.Result, "the condition that failed")
	assert.Contains(t, missed.Condition, "security", "names what it checked")
	assert.Contains(t, missed.Actual, "lint", "and the value it compared")

	assert.Equal(t, eval.Unknown, rows[8].Match, "#8's check names never came")
	unknown := condition(rows[8])
	assert.Equal(t, eval.Unknown, unknown.Result)
	assert.NotEmpty(t, unknown.Actual, "says why it can't tell")
	assert.Equal(t, []facts.Need{facts.NeedChecks}, rows[8].Needs, "and what it waits for")

	again, err := r.svc.Test(t.Context(), d)
	require.NoError(t, err)
	assert.True(t, again.Cached, "a search read within the minute isn't read again")
	assert.Equal(t, 1, again.Points, "only the check names it reads again")
	assert.Len(t, items.searched, 1)

	r.clock.Advance(time.Minute)

	_, err = r.svc.Test(t.Context(), d)
	require.NoError(t, err)
	assert.Len(t, items.searched, 2, "after a minute it is read again")
}

func TestTestWarnsOfAnOwnerTheInboxHasNeverSeen(t *testing.T) {
	t.Parallel()

	typo := watch.Search{Kind: item.KindPR, Where: []string{"org:acme-orgg", "repo:octo-user/example-repo"}}
	q := query(t, typo)
	r := itemsRig(t, &fakeItems{
		searches: map[string]autopilot.SearchResult{q.Text: {IDs: []int64{}}},
		owners:   map[string]bool{"octo-user": true},
	})

	got, err := r.svc.Test(t.Context(), draft("Typo", searchSpec(catalog.EventAppears, typo)))
	require.NoError(t, err)
	require.Len(t, got.Searches, 1)
	require.Len(t, got.Searches[0].Warnings, 1)
	assert.Contains(t, got.Searches[0].Warnings[0], "acme-orgg")
}

func TestTestRefusesADraftWithProblems(t *testing.T) {
	t.Parallel()

	r := itemsRig(t, &fakeItems{})

	bad := searchSpec(catalog.EventAppears, watch.Search{Kind: item.KindPR})

	_, err := r.svc.Test(t.Context(), draft("No scope", bad))

	var problems *autopilot.ProblemsError

	require.ErrorAs(t, err, &problems, "a search that names no scope is refused as Save refuses it")
}

func TestTestSaysWhenTheInboxCantBeRead(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeDryRun)

	_, err := r.svc.Test(t.Context(), draft("Any", searchSpec(catalog.EventAppears, orgSearch)))
	require.ErrorIs(t, err, autopilot.ErrNoItems)

	failing := itemsRig(t, &fakeItems{err: errLocked})

	_, err = failing.svc.Test(t.Context(), draft("Any", searchSpec(catalog.EventAppears, orgSearch)))
	require.Error(t, err)
}

func TestRebuildsRunOneAtATimeSoTheLastReadStoresLast(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, autopilotprefs.ModeDryRun)
		off := r.save(t, draft("Off", searchSpec(catalog.EventAppears, orgSearch)))
		searches := autopilot.NewSearches(r.svc)

		arrived, release := r.accounts.holdNext()
		first := make(chan struct{})

		go func() {
			defer close(first)

			searches.Rebuild(t.Context()) // reads the routine off, then waits on the account
		}()

		<-arrived

		// The routine comes on, and its own rebuild runs while the first still waits.
		testutil.Exec(t, r.db, `UPDATE autopilot_routines SET enabled = 1, enabled_at = ? WHERE id = ?`, startedAt(), off.ID)

		second := make(chan struct{})

		go func() {
			defer close(second)

			searches.Rebuild(t.Context())
		}()

		synctest.Wait()
		release()
		<-first
		<-second

		assert.Len(t, searches.Current().Searches, 1, "the rebuild that read the routine on stores last")
	})
}

func TestTestSendsNothingWhileTheBudgetIsLow(t *testing.T) {
	t.Parallel()

	q := query(t, orgSearch)
	items := &fakeItems{
		rows:      map[int64]facts.Row{7: failingPR(7, 70)},
		owners:    map[string]bool{"acme-org": true},
		listed:    map[catalog.ListSource][]int64{catalog.SourceAuthored: {7}},
		searchErr: autopilot.ErrLowBudget,
	}
	r := itemsRig(t, items)

	s := searchSpec(catalog.EventChecksFailed, orgSearch)
	s.Watch.Sources = append(s.Watch.Sources, spec.Source{Lists: []catalog.ListSource{catalog.SourceAuthored}})
	s.Conditions = spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{
		{Field: catalog.FieldID("checks.failedNames"), Op: catalog.Op("anyContains"), Values: []string{"security"}},
	}}

	got, err := r.svc.Test(t.Context(), draft("Low", s))
	require.NoError(t, err, "a low budget is said, not failed")
	assert.True(t, got.LowBudget)
	require.Len(t, got.Searches, 1)
	assert.Equal(t, q.Text, got.Searches[0].Query)
	assert.Contains(t, got.Searches[0].Error, "budget")
	assert.Zero(t, got.Points)
	assert.Empty(t, items.read, "no check name is read either")
	require.Len(t, got.Items, 1, "the lists' items are still tried")
	assert.Equal(t, eval.Unknown, got.Items[0].Match)
}

func TestMatchingSaysWhenItsCountMayBeShort(t *testing.T) {
	t.Parallel()

	q := query(t, orgSearch)
	s := searchSpec(catalog.EventChecksFailed, orgSearch)
	s.Trigger.WaitForChecks = new(false)

	many := make([]int64, 0, 201)
	manyRows := map[int64]facts.Row{}

	for id := int64(1); id <= 201; id++ {
		many = append(many, id)
		manyRows[id] = failingPR(id, int(id))
	}

	tests := []struct {
		name      string
		result    autopilot.SearchResult
		rows      map[int64]facts.Row
		searchErr error
		want      autopilot.Matching
	}{
		{
			name:   "everything read",
			result: autopilot.SearchResult{IDs: []int64{7}, Count: 1},
			want:   autopilot.Matching{Count: 1, PullRequests: 1},
		},
		{
			name:   "a search GitHub didn't answer",
			result: autopilot.SearchResult{IDs: []int64{}, Err: errLocked},
			want:   autopilot.Matching{Incomplete: true},
		},
		{
			name:   "a search past a page",
			result: autopilot.SearchResult{IDs: []int64{7}, Count: 340, Truncated: true},
			want:   autopilot.Matching{Count: 1, PullRequests: 1, Incomplete: true},
		},
		{
			name:      "a budget too low to read the search",
			searchErr: autopilot.ErrLowBudget,
			want:      autopilot.Matching{Incomplete: true},
		},
		{
			name:   "more items than a test tries",
			result: autopilot.SearchResult{IDs: many, Count: len(many)},
			rows:   manyRows,
			want:   autopilot.Matching{Count: 200, PullRequests: 200, Incomplete: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rows := tt.rows
			if rows == nil {
				rows = map[int64]facts.Row{7: failingPR(7, 70)}
			}

			r := itemsRig(t, &fakeItems{
				searches:  map[string]autopilot.SearchResult{q.Text: tt.result},
				rows:      rows,
				owners:    map[string]bool{"acme-org": true},
				searchErr: tt.searchErr,
			})

			got, err := r.svc.Matching(t.Context(), draft("Failing", s))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestATestFromTheCacheReadsNoCheckNameWhileTheBudgetIsLow(t *testing.T) {
	t.Parallel()

	q := query(t, orgSearch)
	items := &fakeItems{
		searches: map[string]autopilot.SearchResult{q.Text: {IDs: []int64{7}, Count: 1}},
		rows:     map[int64]facts.Row{7: failingPR(7, 70)},
		owners:   map[string]bool{"acme-org": true},
	}
	r := itemsRig(t, items)

	d := draft("Security", searchSpec(catalog.EventChecksFailed, orgSearch))
	d.Spec.Conditions = spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{
		{Field: catalog.FieldID("checks.failedNames"), Op: catalog.Op("anyContains"), Values: []string{"security"}},
	}}

	_, err := r.svc.Test(t.Context(), d)
	require.NoError(t, err)

	items.mu.Lock()
	items.checksErr = autopilot.ErrLowBudget
	items.mu.Unlock()

	got, err := r.svc.Test(t.Context(), d)
	require.NoError(t, err)
	assert.True(t, got.Cached, "the searches come from the last minute's cache")
	assert.True(t, got.LowBudget, "and the check names, which would go out, wait for the budget")
	assert.NotContains(t, r.logs.String(), "could not read the check names", "a low budget is said, not logged as a failure")
}

func TestTheEstimatePacksBySearchesMatchCounts(t *testing.T) {
	t.Parallel()

	matched, failed := query(t, orgSearch), query(t, mineSearch)
	costs := &recordingCosts{}
	items := &fakeItems{
		searches: map[string]autopilot.SearchResult{
			matched.Text: {IDs: []int64{7}, Count: 40},
			failed.Text:  {IDs: []int64{}, Err: errLocked},
		},
		rows:   map[int64]facts.Row{7: failingPR(7, 70)},
		owners: map[string]bool{"acme-org": true},
	}
	r := itemsRig(t, items)
	r.svc = autopilot.NewService(autopilot.Config{
		Pool: r.db, Prefs: prefs{cfg: onSettings()}, Accounts: r.accounts, Names: names{},
		File: autopilot.NewFile(filepath.Join(t.TempDir(), "autopilot.json"), "v0.0.0-test", r.clock), Clock: r.clock,
		Logger: r.logs.Logger(), Items: items, Costs: costs,
	})

	_, err := r.svc.Test(t.Context(), draft("Two", searchSpec(catalog.EventAppears, orgSearch, mineSearch)))
	require.NoError(t, err)

	costs.mu.Lock()
	defer costs.mu.Unlock()

	assert.Equal(t, [][]int{{40, -1}}, costs.counts,
		"a search is packed by what it matched, and one that failed as a full page, never as nothing")
}

// recordingCosts estimates as fakeCosts does, recording the counts it was asked about.
type recordingCosts struct {
	mu     sync.Mutex
	counts [][]int
}

func (c *recordingCosts) Estimate(ctx context.Context, counts []int) (autopilot.Estimate, error) {
	c.mu.Lock()
	c.counts = append(c.counts, slices.Clone(counts))
	c.mu.Unlock()

	return fakeCosts{}.Estimate(ctx, counts)
}
