package autopilot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// A routine's week, for This week beneath its builder: what its runs of the last seven
// days did, read from the ledger alone. A run counts in the week it started; one that
// never started counts nowhere. The dollars are what each run's agent reported, which
// under a subscription's sign-in is what the same work would cost through the API, so the
// window says "about"; the tokens are what each run's stream reported, the real measure.
// A run in the user's terminal reports neither, since its session is the user's: the
// session panel counts its tokens from the agent's own files.

// statsWeek is how far back This week reaches.
const statsWeek = 7 * 24 * time.Hour

// verdictPassed is a run's verdict when the checks on its commit passed, as the follow of
// an agent's checks records it: the Only if outcome it ends Wait for the checks with.
const verdictPassed = string(catalog.OutcomePassedChecks)

// RoutineStats is what a routine's runs did since Since.
type RoutineStats struct {
	// Since is when the week began: seven days before now.
	Since time.Time `json:"since"`

	// Runs counts its agents' runs, and Scripts its scripts' runs.
	Runs    int `json:"runs"`
	Scripts int `json:"scripts"`

	// Fixes counts its matches whose agent pushed and whose checks then passed; Merges its
	// Merge it steps that merged; Reviews its agents' reviews posted; and Actions the
	// GitHub actions GitHub took.
	Fixes   int `json:"fixes"`
	Merges  int `json:"merges"`
	Reviews int `json:"reviews"`
	Actions int `json:"actions"`

	// CostUSD is what the runs that reported a cost said they spent, in dollars, nil while
	// none reported one; Costed is how many did, and Estimated how many of those reported
	// the agent's estimate of what the API would have charged, as one signed in with the
	// user's plan does.
	CostUSD   *float64 `json:"costUsd"`
	Costed    int      `json:"costed"`
	Estimated int      `json:"estimated"`

	// TokensIn, TokensOut, and CacheRead are the runs' tokens, as their streams reported
	// them, and Counted how many runs reported any.
	TokensIn  int64 `json:"tokensIn"`
	TokensOut int64 `json:"tokensOut"`
	CacheRead int64 `json:"cacheRead"`
	Counted   int   `json:"counted"`
}

// Stats returns what the routine id's runs did over the last seven days, for its This week.
// It reads the database alone.
func (s *Service) Stats(ctx context.Context, id int64) (RoutineStats, error) {
	since := s.clock.Now().Add(-statsWeek)
	out := RoutineStats{Since: since}

	if s.store.q == nil {
		return out, nil
	}

	rows, err := s.ledger().q.ListRoutineWeekRuns(ctx, queries.ListRoutineWeekRunsParams{
		RoutineID: id, Since: new(since.UTC()),
	})
	if err != nil {
		return out, uierr.New("The routine's week couldn't be read.", err)
	}

	return weekStats(since, rows), nil
}

// weekStats counts a routine's runs of the week into its stats (see the notes at the top
// of this file).
func weekStats(since time.Time, rows []queries.ListRoutineWeekRunsRow) RoutineStats {
	out := RoutineStats{Since: since}
	fixed := map[int64]bool{}

	var cost float64

	for _, r := range rows {
		if RunOutcome(r.Outcome) == RunNotStarted {
			continue
		}

		steps, err := store.DecodeJSON[[]stepRun](r.MatchSteps)
		if err != nil {
			steps = nil
		}

		out.count(r, stepKindAt(steps, r.Step))
		cost += out.spent(r)

		if isAgentRunner(r.Runner) && (r.Verdict == verdictPassed || fixedIn(steps, r.Step)) {
			fixed[r.MatchID] = true
		}
	}

	out.Fixes = len(fixed)

	if out.Costed > 0 {
		out.CostUSD = &cost
	}

	return out
}

// spent counts what a run reported spending, its tokens and its cost, and returns the
// cost, 0 when it reported none.
func (st *RoutineStats) spent(r queries.ListRoutineWeekRunsRow) float64 {
	if r.TokensIn+r.TokensOut+r.CacheReadTokens > 0 {
		st.TokensIn += r.TokensIn
		st.TokensOut += r.TokensOut
		st.CacheRead += r.CacheReadTokens
		st.Counted++
	}

	if r.Cost == nil {
		return 0
	}

	st.Costed++

	if r.CostEstimated {
		st.Estimated++
	}

	return *r.Cost
}

// count counts a run of a step of kind: an agent's or a script's run, or, once GitHub took
// it, a GitHub action, a merge, or a review posted.
func (st *RoutineStats) count(r queries.ListRoutineWeekRunsRow, kind catalog.StepKind) {
	sent := r.Status == runEnded && RunOutcome(r.Outcome) == RunDone

	switch {
	case r.Runner == runnerScript:
		st.Scripts++
	case isAgentRunner(r.Runner):
		st.Runs++
	case !sent:
	case kind == catalog.StepMerge:
		st.Merges++
	case kind == catalog.StepPostReview:
		st.Reviews++
	default:
		st.Actions++
	}
}

// stepKindAt returns the kind of the step at index among steps, "" when there is none.
func stepKindAt(steps []stepRun, index int64) catalog.StepKind {
	if index < 0 || int(index) >= len(steps) {
		return ""
	}

	return steps[index].Step.Kind()
}

// fixedIn reports a match whose agent step at index pushed, and whose Wait for the checks
// after it passed: a fix.
func fixedIn(steps []stepRun, index int64) bool {
	if int(index) >= len(steps) || steps[index].Outcome != string(catalog.OutcomePushed) {
		return false
	}

	for _, sr := range steps[index+1:] {
		if sr.Step.Kind() == catalog.StepWaitForChecks {
			return sr.Outcome == string(catalog.OutcomePassedChecks)
		}
	}

	return false
}

// BackgroundTokens returns the tokens the agent's run in the background that started the
// run engine's run scriptRunID reported so far, as the ledger records them, for its
// session's panel; false when no such run is recorded.
func (e *Engine) BackgroundTokens(ctx context.Context, scriptRunID int64) (RunUsage, bool, error) {
	row, err := e.ledger.q.GetBackgroundRunTokens(ctx, scriptRunID)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return RunUsage{}, false, nil
	case err != nil:
		return RunUsage{}, false, fmt.Errorf("read the tokens of run %d: %w", scriptRunID, err)
	}

	return RunUsage{TokensIn: row.TokensIn, TokensOut: row.TokensOut, CacheRead: row.CacheReadTokens}, true, nil
}
