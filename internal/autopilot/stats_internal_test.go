package autopilot

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// statsRig is a Service over a fresh database at now, with two routines of its own.
type statsRig struct {
	t   *testing.T
	svc *Service
	q   *queries.Queries
	now time.Time

	routines []int64
	matches  int
}

// newStatsRig returns a rig at now, with routines named first and second.
func newStatsRig(t *testing.T, now time.Time) *statsRig {
	t.Helper()

	db := testutil.DB(t)
	clk := testutil.NewClock(now)
	r := &statsRig{t: t, svc: NewService(Config{Pool: db, Clock: clk}), q: queries.New(db), now: now}

	for _, name := range []string{"first", "second"} {
		id, err := r.q.InsertRoutine(t.Context(), queries.InsertRoutineParams{
			AccountLogin: "octo-user", Name: name, NameKey: name, Mode: "auto", Spec: "{}", SpecVersion: 1,
			CreatedAt: now, UpdatedAt: now,
		})
		require.NoError(t, err)

		r.routines = append(r.routines, id)
	}

	return r
}

// weekRun is a run to record, ended as it says, started ago before now.
type weekRun struct {
	routine int
	ago     time.Duration
	runner  string
	step    int64
	outcome RunOutcome
	verdict string
	cost    *float64
	guessed bool
	in, out int64
	cache   int64
	going   bool
}

// match records a match of the rig's routine with steps, and returns it.
func (r *statsRig) match(routine int, steps ...stepRun) int64 {
	r.t.Helper()

	r.matches++
	m := &match{
		AutopilotMatch: queries.AutopilotMatch{
			RoutineID: r.routines[routine], AccountLogin: "octo-user", RepoOwner: "acme-org", RepoName: "example-repo",
			Kind: "pr", Number: int64(r.matches), FireKey: "appears:item:" + strconv.Itoa(r.matches),
			State: string(StateDone), CreatedAt: r.now, UpdatedAt: r.now, DecidedAt: r.now,
		},
		steps: steps,
	}

	inserted, err := newLedger(r.svc.store.db, r.svc.clock).insert(r.t.Context(), m)
	require.NoError(r.t, err)
	require.True(r.t, inserted)

	return m.ID
}

// run records run on the match id.
func (r *statsRig) run(id int64, run weekRun) {
	r.t.Helper()

	ctx := context.Background()
	started := r.now.Add(-run.ago)

	runID, err := r.q.InsertRun(ctx, queries.InsertRunParams{
		MatchID: id, RoutineID: r.routines[run.routine], Step: run.step, Runner: run.runner, Status: runStarting,
		Counted: true, QueuedAt: started, StartedAt: &started, UpdatedAt: started,
	})
	require.NoError(r.t, err)

	if run.going {
		return
	}

	_, err = r.q.EndRun(ctx, queries.EndRunParams{
		Outcome: string(run.outcome), Cost: run.cost, CostEstimated: run.guessed, TokensIn: run.in, TokensOut: run.out,
		CacheReadTokens: run.cache, FinishedAt: &started, ID: runID,
	})
	require.NoError(r.t, err)

	if run.verdict != "" {
		_, err = r.svc.store.db.ExecContext(ctx, `UPDATE autopilot_runs SET verdict = ? WHERE id = ?`, run.verdict, runID)
		require.NoError(r.t, err)
	}
}

// step is a step of kind, ended with outcome: any kind not listed is Notify me.
func step(kind catalog.StepKind, outcome catalog.Outcome) stepRun {
	steps := map[catalog.StepKind]spec.Step{
		catalog.StepAgent:         {Agent: &spec.AgentStep{}},
		catalog.StepWaitForChecks: {WaitForChecks: &spec.WaitStep{}},
		catalog.StepMerge:         {Merge: &spec.MergeStep{}},
		catalog.StepPostReview:    {PostReview: &spec.ReviewStep{}},
		catalog.StepGitHub:        {GitHub: []spec.GitHubAction{{Rebase: &spec.Empty{}}}},
		catalog.StepScript:        {Script: &spec.ScriptStep{ID: 1, Name: "Lint"}},
	}

	st, ok := steps[kind]
	if !ok {
		st = spec.Step{Notify: &spec.NotifyStep{}}
	}

	return stepRun{Step: st, Status: stepDone, Outcome: string(outcome)}
}

func TestARoutinesWeekCountsWhatItsRunsDid(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 16, 9, 0, 0, 0, time.UTC)
	r := newStatsRig(t, now)
	agent := func(outcome catalog.Outcome) stepRun { return step(catalog.StepAgent, outcome) }
	wait := func(outcome catalog.Outcome) stepRun { return step(catalog.StepWaitForChecks, outcome) }

	// A fix: the agent pushed, and the checks then passed; then a merge, sent.
	fix := r.match(0, agent(catalog.OutcomePushed), wait(catalog.OutcomePassedChecks), step(catalog.StepMerge, ""))
	r.run(fix, weekRun{
		ago: time.Hour, runner: runnerBackground, outcome: RunDone, cost: new(1.25), guessed: true, in: 1000,
		out: 200, cache: 5000,
	})
	r.run(fix, weekRun{ago: time.Hour, runner: "github", step: 2, outcome: RunDone})

	// An agent that pushed, whose checks still fail: no fix.
	failing := r.match(0, agent(catalog.OutcomePushed), wait(catalog.OutcomeFailingChecks))
	r.run(failing, weekRun{ago: 2 * time.Hour, runner: runnerBackground, outcome: RunDone, cost: new(0.5), in: 10, out: 5})

	// A fix the verdict alone says, as the follow of the checks records it.
	verdict := r.match(0, agent(catalog.OutcomePushed))
	r.run(verdict, weekRun{ago: 3 * time.Hour, runner: runnerBackground, outcome: RunDone, verdict: verdictPassed})

	// Twice on one match, an interruption and its run again: one fix, two runs.
	again := r.match(0, agent(catalog.OutcomePushed), wait(catalog.OutcomePassedChecks))
	r.run(again, weekRun{ago: 4 * time.Hour, runner: runnerBackground, outcome: RunInterrupted})
	r.run(again, weekRun{ago: 3 * time.Hour, runner: runnerBackground, outcome: RunDone})

	// A review posted, a GitHub action sent and one GitHub didn't take, a script, and a run
	// in the terminal, which reports neither cost nor tokens.
	review := r.match(0, agent(catalog.OutcomeApprove), step(catalog.StepPostReview, ""))
	r.run(review, weekRun{ago: time.Hour, runner: runnerBackground, outcome: RunDone, in: 7, out: 3})
	r.run(review, weekRun{ago: time.Hour, runner: "github", step: 1, outcome: RunDone})

	actions := r.match(0, step(catalog.StepGitHub, ""))
	r.run(actions, weekRun{ago: time.Hour, runner: "github", outcome: RunDone})
	r.run(actions, weekRun{ago: time.Hour, runner: "github", outcome: RunNotSent})

	script := r.match(0, step(catalog.StepScript, ""))
	r.run(script, weekRun{ago: time.Hour, runner: "script", outcome: RunDone})

	terminal := r.match(0, agent(""))
	r.run(terminal, weekRun{ago: time.Hour, runner: "terminal", outcome: RunDone})

	// What counts nowhere: a start that never happened, a run older than the week, a run
	// going (counted, with nothing reported yet), and another routine's.
	never := r.match(0, agent(""))
	r.run(never, weekRun{ago: time.Hour, runner: runnerBackground, outcome: RunNotStarted})

	old := r.match(0, agent(catalog.OutcomePushed), wait(catalog.OutcomePassedChecks))
	r.run(old, weekRun{ago: 8 * 24 * time.Hour, runner: runnerBackground, outcome: RunDone, cost: new(9.0)})

	going := r.match(0, agent(""))
	r.run(going, weekRun{ago: time.Minute, runner: runnerBackground, going: true})

	other := r.match(1, agent(catalog.OutcomePushed), wait(catalog.OutcomePassedChecks))
	r.run(other, weekRun{routine: 1, ago: time.Hour, runner: runnerBackground, outcome: RunDone, cost: new(4.0)})

	got, err := r.svc.Stats(t.Context(), r.routines[0])
	require.NoError(t, err)

	assert.Equal(t, now.Add(-statsWeek), got.Since)
	assert.Equal(t, 8, got.Runs, "every agent's run that started this week, the interrupted and the one going too")
	assert.Equal(t, 1, got.Scripts)
	assert.Equal(t, 3, got.Fixes, "a pushed commit whose checks passed, once per match")
	assert.Equal(t, 1, got.Merges)
	assert.Equal(t, 1, got.Reviews)
	assert.Equal(t, 1, got.Actions, "a GitHub action that sent nothing isn't one")
	require.NotNil(t, got.CostUSD)
	assert.InDelta(t, 1.75, *got.CostUSD, 1e-9)
	assert.Equal(t, 2, got.Costed)
	assert.Equal(t, 1, got.Estimated, "one run's cost was the agent's estimate, the other's a charge")
	assert.Equal(t, RoutineStats{
		Since: got.Since, Runs: 8, Scripts: 1, Fixes: 3, Merges: 1, Reviews: 1, Actions: 1, CostUSD: got.CostUSD,
		Costed: 2, Estimated: 1, TokensIn: 1017, TokensOut: 208, CacheRead: 5000, Counted: 3,
	}, got)
}

func TestAWeekWithNoRunsCountsNothing(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 16, 9, 0, 0, 0, time.UTC)
	r := newStatsRig(t, now)

	got, err := r.svc.Stats(t.Context(), r.routines[0])
	require.NoError(t, err)
	assert.Equal(t, RoutineStats{Since: now.Add(-statsWeek)}, got)
	assert.Nil(t, got.CostUSD, "no run reported a cost, so there is no estimate")
}

func TestABackgroundRunsTokensAreWhatItsSessionCounts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 16, 9, 0, 0, 0, time.UTC)
	r := newStatsRig(t, now)
	id := r.match(0, step(catalog.StepAgent, ""))

	r.run(id, weekRun{ago: time.Hour, runner: runnerBackground, outcome: RunDone, in: 1200, out: 30, cache: 9000})
	r.run(id, weekRun{ago: time.Hour, runner: "terminal", outcome: RunDone, in: 5})

	_, err := r.svc.store.db.ExecContext(t.Context(), `UPDATE autopilot_runs SET script_run_id = id + 40`)
	require.NoError(t, err)

	e := &Engine{ledger: newLedger(r.svc.store.db, r.svc.clock)}

	got, ok, err := e.BackgroundTokens(t.Context(), 41)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, RunUsage{TokensIn: 1200, TokensOut: 30, CacheRead: 9000}, got)

	for _, runID := range []int64{42, 99} {
		_, ok, err = e.BackgroundTokens(t.Context(), runID)
		require.NoError(t, err)
		assert.False(t, ok, "run %d is no background agent's run", runID)
	}
}
