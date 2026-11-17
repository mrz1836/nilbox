package autopilot

import (
	"database/sql"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// The ledger's keys in memory (ledgerindex.go): built once, kept in step by every write the
// engine makes, a transaction's only once it commits, and built again when the engine takes
// the lock or something else deleted rows. The simulator compares them with the database
// after every cycle, command, tick, and worker's result of every scenario (sim.drift).

// inStep fails t unless e's keys in memory hold exactly what the ledger does.
func inStep(t *testing.T, e *Engine) {
	t.Helper()

	require.True(t, e.ledger.index.valid.Load(), "the keys are built")
	require.NoError(t, e.ledger.index.diff(t.Context(), e.ledger.q))
}

// cycled runs one of e's cycles and waits for its workers.
func cycled(t *testing.T, e *Engine) {
	t.Helper()

	e.cycle(t.Context())
	e.work.Wait()
}

// routineIDs returns e's routines' IDs, in the user's order.
func routineIDs(t *testing.T, e *Engine) []int64 {
	t.Helper()

	rows, err := e.svc.store.all(t.Context())
	require.NoError(t, err)

	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}

	return out
}

// recordMatch records an open match of routine on item for the occurrence fire, waiting on b.
func recordMatch(t *testing.T, e *Engine, routine, item int64, fire string, b Blocker) *match {
	t.Helper()

	now := e.clock.Now()
	m := &match{
		AutopilotMatch: queries.AutopilotMatch{
			RoutineID: routine, ItemID: new(item), AccountLogin: "octo-user", RepoOwner: "acme-org",
			RepoName: "example-repo", Kind: "pr", Number: item, FireKey: fire, CreatedAt: now, UpdatedAt: now,
			DecidedAt: now,
		},
		steps: []stepRun{}, trace: []eval.Trace{},
	}
	m.wait(b, "")

	inserted, err := e.ledger.insert(t.Context(), m)
	require.NoError(t, err)
	require.True(t, inserted)

	return m
}

// errRolledBack rolls a transaction back.
var errRolledBack = errors.New("rolled back")

// keyOf returns what e's keys hold of m.
func keyOf(e *Engine, m *match) (keyRow, bool) {
	row, ok := e.ledger.index.matches[m.RoutineID][matchKey{item: *m.ItemID, fire: m.FireKey, dryRun: m.DryRun}]

	return row, ok
}

func TestTheLedgersKeysAreBuiltOnceAndKeptInStepCycleAfterCycle(t *testing.T) {
	t.Parallel()

	e, items := cycleEngine(t, 6, 24)

	cycled(t, e)
	require.Positive(t, e.state.LastCycle.Matches, "the first cycle records what each routine would do")
	inStep(t, e)
	assert.Equal(t, 1, e.ledger.index.builds, "the first cycle builds the keys")
	assert.NotEmpty(t, e.ledger.index.places)
	assert.Positive(t, e.ledger.index.rows)

	cycled(t, e)
	assert.Zero(t, e.state.LastCycle.Matches, "a steady cycle records nothing")

	// New heads are new occurrences, each recorded as the cycle decides it.
	for _, id := range []int64{2, 5, 9} {
		f := benchPull(int(id)+1000, items.now)
		f.Item.ID = id
		items.facts[id] = f
	}

	cycled(t, e)
	assert.Positive(t, e.state.LastCycle.Matches, "the new heads match again")
	inStep(t, e)

	cycled(t, e)
	inStep(t, e)
	assert.Equal(t, 1, e.ledger.index.builds, "no cycle after the first reads the keys from the database")
}

func TestATransactionsWritesReachTheKeysOnlyOnceItCommits(t *testing.T) {
	t.Parallel()

	e, _ := cycleEngine(t, 2, 4)
	ctx := t.Context()

	cycled(t, e)

	r := routineIDs(t, e)[0]
	m := recordMatch(t, e, r, 1, "test:1", BlockSlot)
	inStep(t, e)

	row, ok := keyOf(e, m)
	require.True(t, ok, "an insert reaches the keys at once")
	assert.Equal(t, StateQueued, row.State)

	err := e.ledger.inTx(ctx, func(q *queries.Queries) error {
		m.close(StateDismissed, ReasonYouSkipped, "", e.clock.Now())

		if updateErr := e.ledger.update(ctx, q, m); updateErr != nil {
			return updateErr
		}

		return errRolledBack
	})
	require.ErrorIs(t, err, errRolledBack)

	row, _ = keyOf(e, m)
	assert.Equal(t, StateQueued, row.State, "a write rolled back never reaches the keys")
	inStep(t, e)

	m, err = e.ledger.get(ctx, m.ID)
	require.NoError(t, err)

	err = e.ledger.inTx(ctx, func(q *queries.Queries) error {
		m.close(StateDismissed, ReasonYouSkipped, "", e.clock.Now())

		return e.ledger.update(ctx, q, m)
	})
	require.NoError(t, err)

	row, _ = keyOf(e, m)
	assert.Equal(t, StateDismissed, row.State, "a write committed reaches the keys")
	assert.Equal(t, ReasonYouSkipped, row.Reason)
	inStep(t, e)
	assert.Empty(t, e.ledger.index.txs, "no transaction's notes are left open")
	assert.Equal(t, 1, e.ledger.index.builds)
}

func TestAWriteThroughATransactionTheLedgerDidntOpenBuildsTheKeysAgain(t *testing.T) {
	t.Parallel()

	e, _ := cycleEngine(t, 2, 4)
	ctx := t.Context()

	cycled(t, e)

	m := recordMatch(t, e, routineIDs(t, e)[0], 2, "test:2", BlockSlot)

	err := store.InTx(ctx, e.ledger.db, func(tx *sql.Tx) error {
		m.wait(BlockAsk, "")

		return e.ledger.update(ctx, e.ledger.q.WithTx(tx), m)
	})
	require.NoError(t, err)
	assert.False(t, e.ledger.index.valid.Load(), "what it wrote can't be told, so the keys are built again")

	require.NoError(t, e.ledger.index.ensure(ctx, e.ledger.q))
	inStep(t, e)
	assert.Equal(t, 2, e.ledger.index.builds)

	row, _ := keyOf(e, m)
	assert.Equal(t, StateWaiting, row.State)
}

func TestTheKeysAreBuiltAgainOnceSomethingElseDeletesTheirRows(t *testing.T) {
	t.Parallel()

	e, items := cycleEngine(t, 4, 8)

	cycled(t, e)
	inStep(t, e)

	r := routineIDs(t, e)[0]
	_, seen := e.ledger.index.seen[r][3]
	require.True(t, seen, "the routine saw the item")

	// The inbox prunes an old item: its baselines go with it, and its matches lose it.
	testutil.Exec(t, e.ledger.db, `DELETE FROM items WHERE id = 3`)
	delete(items.facts, 3)
	items.ids = slices.DeleteFunc(items.ids, func(id int64) bool { return id == 3 })

	require.Error(t, e.ledger.index.diff(t.Context(), e.ledger.q), "the keys hold what is gone")

	cycled(t, e)
	inStep(t, e)
	assert.Equal(t, 2, e.ledger.index.builds, "the counts tell the keys to be built again")

	_, seen = e.ledger.index.seen[r][3]
	assert.False(t, seen)

	for _, at := range e.ledger.index.places {
		assert.NotEqual(t, int64(3), at.key.item)
	}
}

func TestTakingTheLockBuildsTheKeysAgain(t *testing.T) {
	t.Parallel()

	e, _ := cycleEngine(t, 2, 4)
	ctx := t.Context()

	cycled(t, e)

	m := recordMatch(t, e, routineIDs(t, e)[0], 1, "test:1", BlockAsk)

	// Another copy of the app held the engine meanwhile, and the user skipped the match
	// there: a change no count tells.
	e.setHolds(false, false)
	testutil.Exec(t, e.ledger.db, `UPDATE autopilot_matches SET state = 'dismissed', reason = 'you_skipped' WHERE id = ?`,
		m.ID)
	e.takeLock(ctx)
	assert.False(t, e.ledger.index.valid.Load())

	cycled(t, e)
	inStep(t, e)
	assert.Equal(t, 2, e.ledger.index.builds)

	row, _ := keyOf(e, m)
	assert.Equal(t, StateDismissed, row.State)
}

func TestThePrunesDeletionsReachTheKeys(t *testing.T) {
	t.Parallel()

	e, items := cycleEngine(t, 4, 8)
	ctx := t.Context()

	cycled(t, e)

	// The routines' Dry run records on item 4 closed when they were recorded; once the item
	// is closed, Clear history removes them.
	items.closed[4] = true
	now := e.clock.Now()

	cleared, err := e.clear(ctx, now.Add(time.Second), now, true)
	require.NoError(t, err)
	require.Positive(t, cleared.Removed)

	for _, at := range e.ledger.index.places {
		assert.NotEqual(t, int64(4), at.key.item, "a deleted match leaves the keys")
	}

	require.NoError(t, e.ledger.index.ensure(ctx, e.ledger.q))
	inStep(t, e)
	assert.Equal(t, 1, e.ledger.index.builds, "the keys knew of the deletions without a read")
}

func TestForgettingADeletedRoutinesBaselinesBuildsTheKeysAgain(t *testing.T) {
	t.Parallel()

	e, _ := cycleEngine(t, 3, 6)
	ctx := t.Context()

	cycled(t, e)

	gone := routineIDs(t, e)[1]
	require.NotEmpty(t, e.ledger.index.seen[gone])
	require.NoError(t, e.svc.Delete(ctx, gone))

	now := e.clock.Now()
	_, err := e.clear(ctx, now, now, false)
	require.NoError(t, err)
	assert.False(t, e.ledger.index.valid.Load(), "the baselines forgotten have the keys built again")

	cycled(t, e)
	inStep(t, e)
	assert.Equal(t, 2, e.ledger.index.builds)
	assert.Empty(t, e.ledger.index.seen[gone])
}

func TestSkipBacklogSkipsWhatTheCardShowedInOneTransaction(t *testing.T) {
	t.Parallel()

	e, _ := cycleEngine(t, 2, 8)
	ctx := t.Context()

	cycled(t, e)

	ids := routineIDs(t, e)
	mine, theirs := ids[0], ids[1]

	var backlog []int64
	for item := int64(1); item <= 4; item++ {
		backlog = append(backlog, recordMatch(t, e, mine, item, "backlog:"+strconv.FormatInt(item, 10), BlockBacklog).ID)
	}

	other := recordMatch(t, e, theirs, 5, "backlog:5", BlockBacklog).ID
	letGo := recordMatch(t, e, mine, 6, "backlog:6", BlockSlot).ID

	// The card showed the first three, another routine's, and one let go since; the fourth
	// was recorded after it.
	skipped, err := e.skipBacklog(ctx, mine, []int64{backlog[0], backlog[1], backlog[2], backlog[1], other, letGo, 999})
	require.NoError(t, err)
	assert.Equal(t, backlog[:3], skipped)
	inStep(t, e)

	for _, id := range backlog[:3] {
		m, getErr := e.ledger.get(ctx, id)
		require.NoError(t, getErr)
		assert.Equal(t, StateDismissed, m.state())
		assert.Equal(t, ReasonYouSkipped, Reason(m.Reason))
		assert.NotNil(t, m.ClosedAt)
	}

	for id, want := range map[int64]State{backlog[3]: StateWaiting, other: StateWaiting, letGo: StateQueued} {
		m, getErr := e.ledger.get(ctx, id)
		require.NoError(t, getErr)
		assert.Equal(t, want, m.state(), "match %d is left as it is", id)
	}

	// One that can't be skipped skips none.
	more := recordMatch(t, e, mine, 7, "backlog:7", BlockBacklog).ID
	testutil.Exec(t, e.ledger.db, `CREATE TRIGGER refuse_skip BEFORE UPDATE ON autopilot_matches WHEN OLD.id = `+
		strconv.FormatInt(more, 10)+` BEGIN SELECT RAISE(ABORT, 'refused'); END`)

	skipped, err = e.skipBacklog(ctx, mine, []int64{backlog[3], more})
	require.Error(t, err)
	assert.Empty(t, skipped)

	m, err := e.ledger.get(ctx, backlog[3])
	require.NoError(t, err)
	assert.Equal(t, StateWaiting, m.state(), "the skip before the one refused is rolled back")
	inStep(t, e)
	assert.Equal(t, 1, e.ledger.index.builds)
}

// keyWrites are the queries that write what the ledger's keys hold, each with the one
// function that may call it, which notes the write.
func keyWrites() map[string]string {
	return map[string]string{
		"InsertMatch":    "ledger.go: insert",
		"UpdateMatch":    "ledger.go: update",
		"InsertBaseline": "ledger.go: saveBaseline",
		"UpdateBaseline": "ledger.go: saveBaseline",
		"DeleteMatches":  "upkeep.go: deleteMatches",
	}
}

func TestOnlyTheLedgerWritesWhatItsKeysHold(t *testing.T) {
	t.Parallel()

	want := keyWrites()
	found := map[string][]string{}

	for _, path := range sourceFiles(t, ".", "sim") {
		for _, call := range queryCalls(t, path) {
			if _, ok := want[call.query]; ok {
				found[call.query] = append(found[call.query], call.at)
			}
		}
	}

	for query, at := range want {
		assert.Equal(t, []string{at}, found[query], query)
	}
}

// sourceFiles returns the Go files of dirs that aren't tests.
func sourceFiles(t *testing.T, dirs ...string) []string {
	t.Helper()

	out := make([]string, 0, len(dirs))

	for _, dir := range dirs {
		paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
		require.NoError(t, err)

		out = append(out, slices.DeleteFunc(paths, func(p string) bool { return strings.HasSuffix(p, "_test.go") })...)
	}

	return out
}

// queryCall is a call of a method by its name, and where: "file: function".
type queryCall struct {
	query string
	at    string
}

// queryCalls returns every call of a method by name in the file at path, with the
// function it is in.
func queryCalls(t *testing.T, path string) []queryCall {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	require.NoError(t, err)

	var out []queryCall

	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			for _, name := range methodCalls(fn) {
				out = append(out, queryCall{query: name, at: path + ": " + fn.Name.Name})
			}
		}
	}

	return out
}

// methodCalls returns the name of every method fn calls.
func methodCalls(fn *ast.FuncDecl) []string {
	var out []string

	ast.Inspect(fn, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel {
				out = append(out, sel.Sel.Name)
			}
		}

		return true
	})

	return out
}
