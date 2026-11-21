package autopilot

import (
	"context"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// dayCounts are today's counts against the limits, from local midnight, and the pace of
// the GitHub actions over the last minute and the last hour, which may reach back past it.
type dayCounts struct {
	routineRuns  map[int64]int
	agentRuns    int
	actions      int
	activeAgents int
	activeItems  map[int64]bool
	spend        map[autopilotprefs.Unit]float64
	routineSpend map[int64]map[autopilotprefs.Unit]float64

	// paceMinute and paceHour count the GitHub actions started in the last minute and the
	// last hour, manual ones too, as of now.
	paceMinute int
	paceHour   int
	now        time.Time

	// counted are the matches a routine's runs today were counted for, by routine: a
	// match is one of its routine's runs however many runs it starts.
	counted map[int64]map[int64]bool
}

// dayCounts reads today's counts, and the last hour's GitHub actions.
func (e *Engine) dayCounts(ctx context.Context, now time.Time) (dayCounts, error) {
	out := dayCounts{
		routineRuns: map[int64]int{}, activeItems: map[int64]bool{}, spend: map[autopilotprefs.Unit]float64{},
		routineSpend: map[int64]map[autopilotprefs.Unit]float64{}, counted: map[int64]map[int64]bool{}, now: now,
	}

	day := dayStart(now)

	since := day
	if hour := now.Add(-time.Hour); hour.Before(since) {
		since = hour
	}

	rows, err := e.ledger.q.ListRunsSince(ctx, since)
	if err != nil {
		return out, fmt.Errorf("read today's runs: %w", err)
	}

	for _, r := range rows {
		out.add(r, day)
	}

	return out, nil
}

// add counts one of the runs read: today's toward the day's counts, and the last hour's
// GitHub actions toward their pace. One that never started, or a GitHub action that sent
// nothing, counts toward nothing.
func (d *dayCounts) add(r queries.ListRunsSinceRow, day time.Time) {
	switch RunOutcome(r.Outcome) {
	case RunNotStarted, RunNotSent, RunOutOfUsage:
		return
	case RunDone, RunFailed, RunStopped, RunInterrupted:
	}

	active := r.Status == runStarting || r.Status == runRunning || r.Status == runStopping
	today := !r.QueuedAt.Before(day)

	if active {
		d.going(r)
	}

	if r.Runner == runnerGitHub && r.StartedAt != nil {
		d.paced(*r.StartedAt)
	}

	if today && !r.Manual {
		d.countRun(r.RoutineID, r.MatchID, r.Runner)
	}

	d.addSpend(r, active, today)
}

// going counts a run still going: its item busy, and, for an agent's, an agent's slot
// taken.
func (d *dayCounts) going(r queries.ListRunsSinceRow) {
	if r.ItemID != nil {
		d.activeItems[*r.ItemID] = true
	}

	if isAgentRunner(r.Runner) {
		d.activeAgents++
	}
}

// paced counts a GitHub action started at toward the pace, while it is within the last
// minute or the last hour.
func (d *dayCounts) paced(at time.Time) {
	if age := d.now.Sub(at); age < time.Hour {
		d.paceHour++

		if age < time.Minute {
			d.paceMinute++
		}
	}
}

// countRun counts a run of the match matchID toward the day's counts: its routine's, once
// for the match, and the agents' or the GitHub actions', once for each run; a script's
// counts toward its routine's alone.
func (d *dayCounts) countRun(routineID, matchID int64, runner string) {
	if d.counted[routineID] == nil {
		d.counted[routineID] = map[int64]bool{}
	}

	if !d.counted[routineID][matchID] {
		d.counted[routineID][matchID] = true
		d.routineRuns[routineID]++
	}

	switch {
	case runner == runnerGitHub:
		d.actions++
	case isAgentRunner(runner):
		d.agentRuns++
	}
}

// addSpend adds a run's use to the day's: a run still working by what it reserved, and
// one ended today by what it reported.
func (d *dayCounts) addSpend(r queries.ListRunsSinceRow, active, today bool) {
	add := func(unit autopilotprefs.Unit, amount float64) {
		d.spend[unit] += amount

		if d.routineSpend[r.RoutineID] == nil {
			d.routineSpend[r.RoutineID] = map[autopilotprefs.Unit]float64{}
		}

		d.routineSpend[r.RoutineID][unit] += amount
	}

	switch {
	case active:
		add(autopilotprefs.Unit(r.BudgetUnit), r.BudgetReserved)
	case today:
		if r.Cost != nil {
			add(autopilotprefs.UnitUSD, *r.Cost)
		}

		add(autopilotprefs.UnitTokens, float64(r.TokensIn+r.TokensOut))
	}
}

// started counts a run the cycle just started.
func (d *dayCounts) started(routineID, itemID int64, runner string, job Job, manual bool) {
	d.activeItems[itemID] = true

	switch {
	case isAgentRunner(runner):
		d.activeAgents++
	case runner == runnerGitHub:
		d.paced(d.now)
	}

	if !manual {
		d.countRun(routineID, job.MatchID, runner)
	}

	if job.Unit != "" {
		unit := autopilotprefs.Unit(job.Unit)
		d.spend[unit] += job.Budget

		if d.routineSpend[routineID] == nil {
			d.routineSpend[routineID] = map[autopilotprefs.Unit]float64{}
		}

		d.routineSpend[routineID][unit] += job.Budget
	}
}

// dayStart returns local midnight of now's day, in now's location.
func dayStart(now time.Time) time.Time {
	y, mo, d := now.Date()

	return time.Date(y, mo, d, 0, 0, 0, 0, now.Location())
}
