package autopilot

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/trust"
)

// The engine's cycle time: one cycle of 20 routines over 500 watched pull requests, as
// docs/performance.md records it. The cycle is the engine's own: the routines and the
// ledger read from a real database, the items' facts handed over ready (the inbox's read of
// them is its own, measured apart), every routine decided on every item, and what changed
// written. The routines are the template gallery's, in Dry run, so the first cycle records
// what each would do and every cycle after it, the steady one each tick runs, finds each
// occurrence recorded.

// benchItems is the inbox as the benchmark's engine reads it: every routine watches every
// item, each open and listed but those closed names, every search answered now.
type benchItems struct {
	now    time.Time
	ids    []int64
	facts  map[int64]facts.Facts
	closed map[int64]bool
}

func (b *benchItems) WatchedItems(context.Context, []string, []catalog.ListSource) ([]int64, error) {
	return b.ids, nil
}

func (b *benchItems) Facts(_ context.Context, ids []int64, _ time.Time) ([]facts.Facts, error) {
	out := make([]facts.Facts, 0, len(ids))
	for _, id := range ids {
		if f, ok := b.facts[id]; ok {
			out = append(out, f)
		}
	}

	return out, nil
}

func (b *benchItems) ItemStates(_ context.Context, ids []int64) (map[int64]ItemState, error) {
	out := make(map[int64]ItemState, len(ids))
	for _, id := range ids {
		out[id] = ItemState{State: "OPEN", FetchedAt: b.now, Listed: true, Watched: true}

		if b.closed[id] {
			out[id] = ItemState{State: "CLOSED", FetchedAt: b.now}
		}
	}

	return out, nil
}

func (b *benchItems) InboxStatus(context.Context) (InboxStatus, error) {
	return InboxStatus{Viewer: "octo-user", LastRefresh: b.now, WatchedAt: b.now, Searches: map[string]SearchState{}}, nil
}

func (b *benchItems) ReadChecks(context.Context, []int64) (int, error) { return 0, nil }
func (b *benchItems) ReadStates(context.Context, []int64) error        { return nil }
func (b *benchItems) ReadItem(context.Context, int64) error            { return nil }
func (b *benchItems) WatchNow(context.Context) error                   { return nil }

func (b *benchItems) LocalStates(context.Context, []int64) (map[int64]string, error) {
	return map[int64]string{}, nil
}

// benchPrefs is Autopilot's settings with Autopilot (beta) on.
type benchPrefs struct{}

func (benchPrefs) Autopilot(context.Context) (autopilotprefs.Config, error) {
	c := autopilotprefs.Defaults()
	c.Enabled = true

	return c, nil
}

// benchAccounts signs octo-user in.
type benchAccounts struct{}

func (benchAccounts) ViewerLogin(context.Context) (string, error) { return "octo-user", nil }

// benchPull is pull request n's facts: a Dependabot update whose checks failed.
func benchPull(n int, now time.Time) facts.Facts {
	head := strconv.FormatInt(int64(n)<<20, 16) + "0000000000000000000000000000000000"
	head = head[:40]

	row := facts.Row{
		ID: int64(n), Kind: item.KindPR, Repo: repo.Name{Owner: "acme-org", Repo: "example-repo"}, Number: n,
		Title:  "build(deps): bump golang.org/x/net from 0.30.0 to 0.31.0",
		Author: "dependabot", AuthorType: trust.TypeBot, Association: "NONE",
		HeadRef: "dependabot/go_modules/golang.org/x/net-0.31.0", CreatedAt: now.Add(-72 * time.Hour),
		UpdatedAt: now.Add(-90 * time.Minute), InReviewRequested: true,
		Repository: facts.RepoRow{Read: true, Visibility: "PUBLIC", Permission: "ADMIN"},
		Details: &facts.DetailsRow{
			HeadOID: head, BaseRef: "main", Mergeable: "MERGEABLE", MergeState: "CLEAN", ReviewDecision: "REVIEW_REQUIRED",
			ChecksState: "FAILURE", ChecksPassed: 10, ChecksFailed: 2, Additions: 12, Deletions: 3, Comments: 4,
			Labels: []string{"dependencies"},
		},
		Checks: &facts.ChecksRow{Head: head, Complete: true, Checks: []facts.CheckRow{
			{Name: "Analyze (go)", Workflow: "CodeQL", Outcome: facts.CheckFailed, CompletedAt: now.Add(-10 * time.Minute)},
			{Name: "test", Workflow: "CI", Outcome: facts.CheckPassed, CompletedAt: now.Add(-20 * time.Minute)},
		}},
		Files: &facts.FilesRow{Head: head, Complete: true, Files: []sensitive.File{{Path: "go.mod"}, {Path: "go.sum"}}},
	}

	return facts.Build(row, facts.Env{
		Now: now, Viewer: "octo-user", Trust: trust.NewPolicy(trust.DefaultBots()), Sensitive: sensitive.DefaultConfig(),
	})
}

// benchEngine is an engine over a fresh database holding 20 of the gallery's routines, in
// Dry run, switched on, over 500 pull requests, its first cycle run when first says so.
func benchEngine(b testing.TB, first bool) *Engine {
	b.Helper()

	e, _ := cycleEngine(b, 20, 500)

	if first {
		e.cycle(b.Context())
		e.work.Wait()
	}

	return e
}

// cycleEngine is an engine over a fresh database holding routines of the gallery's
// routines, in Dry run, switched on, over n pull requests, with the inbox it reads.
func cycleEngine(b testing.TB, routines, n int) (*Engine, *benchItems) {
	b.Helper()

	now := time.Date(2026, time.November, 16, 10, 0, 0, 0, time.UTC)
	db := testutil.DB(b)
	svc := NewService(Config{
		Pool: db, Prefs: benchPrefs{}, Accounts: benchAccounts{}, Clock: testutil.NewClock(now),
		Offers: &Offers{Steps: allSteps(), Places: []catalog.Where{catalog.WhereBackground, catalog.WhereTerminal}},
	})

	// The matches name their items, which the inbox stores: one account, one repository,
	// and n pull requests.
	stamp := "'2026-11-16 10:00:00+00:00'"
	seed := []string{
		`INSERT INTO accounts (id, login, created_at, updated_at) VALUES (1, 'octo-user', ` + stamp + `, ` + stamp + `)`,
		`INSERT INTO repos (id, owner, name, created_at, updated_at) VALUES (1, 'acme-org', 'example-repo', ` + stamp +
			`, ` + stamp + `)`,
	}

	items := &benchItems{now: now, facts: map[int64]facts.Facts{}, closed: map[int64]bool{}}
	for i := 1; i <= n; i++ {
		items.ids = append(items.ids, int64(i))
		items.facts[int64(i)] = benchPull(i, now)
		seed = append(seed, `INSERT INTO items (id, account_id, repo_id, kind, number, item_created_at, item_updated_at,
			fetched_at) VALUES (`+strconv.Itoa(i)+`, 1, 1, 'pr', `+strconv.Itoa(i)+`, `+stamp+`, `+stamp+`, `+stamp+`)`)
	}

	for _, stmt := range seed {
		_, err := db.ExecContext(b.Context(), stmt)
		require.NoError(b, err)
	}

	for i, sp := range benchRoutines(b, routines) {
		data, err := spec.Encode(sp)
		require.NoError(b, err)

		name := "Routine " + strconv.Itoa(i+1)
		_, err = queries.New(db).InsertRoutine(b.Context(), queries.InsertRoutineParams{
			AccountLogin: "octo-user", Name: name, NameKey: name, Enabled: true, Mode: string(autopilotprefs.ModeDryRun),
			Spec: string(data), SpecVersion: spec.Version, SortOrder: int64(i), EnabledAt: &now, CreatedAt: now,
			UpdatedAt: now,
		})
		require.NoError(b, err)
	}

	e := NewEngine(svc, EngineConfig{Items: items})
	e.on.Store(true)
	e.takeLock(b.Context())
	e.primed = true

	return e, items
}

// allSteps is every kind of step.
func allSteps() []catalog.StepKind {
	steps := catalog.Steps()
	out := make([]catalog.StepKind, 0, len(steps))

	for _, s := range steps {
		out = append(out, s.Kind)
	}

	return out
}

// benchRoutines are the gallery's templates, repeated to n.
func benchRoutines(b testing.TB, n int) []spec.Spec {
	b.Helper()

	dir := filepath.Join("spec", "testdata", "templates")

	entries, err := os.ReadDir(dir)
	require.NoError(b, err)

	var out []spec.Spec

	for len(out) < n {
		for _, e := range entries {
			data, readErr := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // a fixture
			require.NoError(b, readErr)

			sp, loadErr := spec.Load(data)
			require.NoError(b, loadErr, e.Name())

			if out = append(out, sp); len(out) == n {
				break
			}
		}
	}

	return out
}

// BenchmarkEngineCycle runs the steady cycle of 20 routines over 500 pull requests, the
// one each tick runs once every occurrence is recorded: the target is under 20 ms
// (docs/performance.md, The engine's cycle).
func BenchmarkEngineCycle(b *testing.B) {
	e := benchEngine(b, true)
	ctx := b.Context()

	for b.Loop() {
		e.cycle(ctx)
	}

	b.StopTimer()
	e.work.Wait()

	c := e.state.LastCycle
	require.NotNil(b, c)
	require.Equal(b, 20, c.Routines)
	require.Equal(b, 500, c.Items)
	require.Zero(b, c.Matches, "a steady cycle records nothing new")
	require.Equal(b, 1, e.ledger.index.builds, "a steady cycle reads none of the ledger's keys from the database")
	require.NoError(b, e.ledger.index.diff(b.Context(), e.ledger.q))
}

// BenchmarkEngineFirstCycle runs the first cycle of 20 routines over 500 pull requests,
// which records what each would do on each.
func BenchmarkEngineFirstCycle(b *testing.B) {
	for b.Loop() {
		b.StopTimer()

		e := benchEngine(b, false)

		b.StartTimer()
		e.cycle(b.Context())
		b.StopTimer()

		e.work.Wait()
		require.Positive(b, e.state.LastCycle.Matches)
		b.StartTimer()
	}
}

// BenchmarkLedgerKeysBuild builds the ledger's keys in memory from the database, as the
// engine does when it takes the lock, over what the first cycle of 20 routines on 500 pull
// requests records: 3,500 matches and 10,000 baselines.
func BenchmarkLedgerKeysBuild(b *testing.B) {
	e := benchEngine(b, true)

	for b.Loop() {
		require.NoError(b, newLedgerIndex().build(b.Context(), e.ledger.q))
	}
}

// BenchmarkLedgerKeysCheck is the one read a steady cycle makes of the ledger's keys: the
// counts of the rows, which find them current.
func BenchmarkLedgerKeysCheck(b *testing.B) {
	e := benchEngine(b, true)

	for b.Loop() {
		require.NoError(b, e.ledger.index.ensure(b.Context(), e.ledger.q))
	}

	b.StopTimer()
	require.Equal(b, 1, e.ledger.index.builds)
}
