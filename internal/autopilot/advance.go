package autopilot

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/trust"
)

// A match moves through the steps it started with, one at a time, each starting when the
// one before it ends. A local step (a notice, the inbox's triage, a notification marked
// done) runs at once, held back only by Pause Autopilot and the routine's active hours. An
// agent or GitHub step follows the routine's mode, and goes through every limit, in Go,
// right before it starts (gate): the user's OK, Pause, the active hours, the tries, the
// day's counts, the agents at once, the daily cap, the outside contributor rule, a fork's
// branch, and one run per item; then the item is read fresh from GitHub, on a worker, and
// the next cycle, which evaluates it on what that read stored, starts it. A worker only
// reads and dispatches; the loop alone decides and records.

// freshFor is how long an item's read right before a start stands.
const freshFor = 2 * time.Minute

// fresh is an item read right before a start, with what holds the start back.
type fresh struct {
	at     time.Time
	hold   Blocker
	detail string
}

// verdict is what the limits say of a start: go, wait on a blocker, or close.
type verdict struct {
	closeAs State
	reason  Reason
	block   Blocker
	words   string
}

// wait is a verdict that waits on b.
func wait(b Blocker, words string) verdict { return verdict{block: b, words: words} }

// pass is a verdict that lets a start through.
func (v verdict) through() bool { return v.closeAs == "" && v.block == "" }

// advance moves m through its steps as far as it can go now: Only if steps that don't
// hold are skipped, local steps run, and the first agent or GitHub step starts once the
// limits let it. A match with every step ended closes as done.
func (p *pass) advance(ctx context.Context, r *routineRun, m *match) {
	defer p.save(ctx, m)

	for int(m.Step) < len(m.steps) {
		sr := &m.steps[m.Step]

		if p.stepUnderWay(m, sr) {
			return
		}

		if ended(sr) || p.skipStep(m, sr) {
			m.Step++

			continue
		}

		p.runStep(ctx, r, m, sr)

		return
	}

	reason, words := ReasonRan, "It ran."
	if failedAny(m.steps) {
		reason, words = ReasonFailed, "It ran, and a step failed."
	}

	m.close(StateDone, reason, words, p.now)
}

// stepUnderWay reports a step running now. A step left running by a restart, with nothing
// under way for it, runs again.
func (p *pass) stepUnderWay(m *match, sr *stepRun) bool {
	if sr.Status != stepRunning {
		return false
	}

	if m.state().active() || p.e.inflight[m.ID] {
		return true
	}

	sr.Status = stepPending

	return false
}

// ended reports a step that ended: done, skipped, or failed.
func ended(sr *stepRun) bool {
	return sr.Status == stepDone || sr.Status == stepSkipped || sr.Status == stepFailed
}

// skipStep skips a step whose Only if doesn't hold, and reports whether it did.
func (p *pass) skipStep(m *match, sr *stepRun) bool {
	if onlyIfHolds(m.steps[:m.Step], sr.Step.OnlyIf) {
		return false
	}

	sr.Status, sr.Note = stepSkipped, "Its Only if didn't hold."

	return true
}

// runStep runs, or starts, m's step sr: a local step at once, Wait for the checks by
// verifying, and an agent or GitHub step once the limits let it. A step with no part to
// run it closes the match.
func (p *pass) runStep(ctx context.Context, r *routineRun, m *match, sr *stepRun) {
	info, ok := catalog.StepByKind(sr.Step.Kind())
	if !ok || p.e.dispatchers[info.Dispatcher] == nil && sr.Step.Kind() != catalog.StepWaitForChecks {
		m.close(StateClosed, ReasonNotOffered, "This version can't run one of its steps yet.", p.now)

		return
	}

	switch {
	case info.Local:
		p.runLocal(ctx, r, m, sr, info)
	case sr.Step.Kind() == catalog.StepWaitForChecks:
		p.verify(ctx, r, m, sr)
	default:
		p.tryStart(ctx, r, m, sr, info)
	}
}

// failedAny reports a step that failed.
func failedAny(steps []stepRun) bool {
	for _, s := range steps {
		if s.Status == stepFailed {
			return true
		}
	}

	return false
}

// onlyIfHolds reports whether a step's Only if holds on how the steps before it ended:
// the latest step of the kind the outcome comes from decides.
func onlyIfHolds(before []stepRun, want catalog.Outcome) bool {
	if want == "" {
		return true
	}

	var from catalog.StepKind

	for _, o := range catalog.Outcomes() {
		if o.ID == want {
			from = o.From
		}
	}

	for i := len(before) - 1; i >= 0; i-- {
		if before[i].Step.Kind() == from {
			return before[i].Outcome == string(want)
		}
	}

	return false
}

// runLocal runs a local step on a worker: held back only by Pause Autopilot and the
// routine's active hours.
func (p *pass) runLocal(ctx context.Context, r *routineRun, m *match, sr *stepRun, info catalog.Step) {
	if v := p.held(r, m); !v.through() {
		m.wait(v.block, v.words)

		return
	}

	job := p.job(r, m, sr)
	sr.Status, sr.Started = stepRunning, new(p.now)

	m.wait(BlockStarting, "")
	p.e.dispatch(ctx, m.ID, 0, info.Dispatcher, job)
}

// verify follows the checks on the commit the agent pushed, through the verify part
// once it is there; until then the match stays verifying.
func (p *pass) verify(ctx context.Context, r *routineRun, m *match, sr *stepRun) {
	m.set(StateVerifying, "", "The checks on its commit haven't finished.")

	sr.Status, sr.Started = stepRunning, new(p.now)

	if p.e.dispatchers[catalog.DispatchVerify] != nil {
		p.e.dispatch(ctx, m.ID, 0, catalog.DispatchVerify, p.job(r, m, sr))
	}
}

// tryStart starts an agent or GitHub step once the limits let it: first the item is read
// fresh, on a worker; the cycle after that read, which evaluated the item on it, starts it.
func (p *pass) tryStart(ctx context.Context, r *routineRun, m *match, sr *stepRun, info catalog.Step) {
	v := p.gate(r, m, sr, info)

	switch {
	case v.closeAs != "":
		m.close(v.closeAs, v.reason, v.words, p.now)

		return
	case v.block != "":
		m.wait(v.block, v.words)

		return
	}

	got, read := p.e.freshReads[m.ID]
	if !read || p.now.Sub(got.at) > freshFor {
		p.readFresh(ctx, r, m, sr)

		return
	}

	delete(p.e.freshReads, m.ID)

	if got.hold != "" {
		m.wait(got.hold, got.detail)

		return
	}

	p.start(ctx, r, m, sr, info)
}

// readFresh reads m's item again from GitHub on a worker, and asks what else holds the
// start back; the next cycle evaluates it on what the read stored.
func (p *pass) readFresh(ctx context.Context, r *routineRun, m *match, sr *stepRun) {
	id := store.Deref(m.ItemID)
	job := p.job(r, m, sr)

	m.wait(BlockStarting, "Reading it again from GitHub before it starts.")

	p.e.inflight[m.ID] = true
	p.cycle.Reads++

	p.e.goWork(ctx, func(ctx context.Context) {
		got := fresh{at: p.e.clock.Now()}

		err := p.e.items.ReadItem(ctx, id)
		if err == nil && p.e.holds != nil {
			got.hold, got.detail, err = p.e.holds.Hold(ctx, job)
		}

		p.e.mail.post(func(ctx context.Context) {
			delete(p.e.inflight, m.ID)

			if err != nil {
				p.e.logger.InfoContext(ctx, "Autopilot could not read an item before a start", "match", m.ID, "error", err)
				p.e.requeue(ctx, m.ID, BlockGitHubBudget, "It couldn't be read from GitHub; it tries again soon.")

				return
			}

			p.e.freshReads[m.ID] = got
			p.e.requeue(ctx, m.ID, "", "")
		})
	})
}

// requeue puts a match still waiting on its start back in the queue, waiting on b.
func (e *Engine) requeue(ctx context.Context, id int64, b Blocker, words string) {
	m, err := e.ledger.get(ctx, id)
	if err != nil || m.state() != StateQueued {
		return
	}

	if b == "" {
		m.set(StateQueued, "", "")
	} else {
		m.wait(b, words)
	}

	e.save(ctx, m)
}

// gate checks every limit on a start, in Go, cheapest first, and says whether it goes,
// waits, or closes. A manual run counts toward no cap or try, and keeps to the active
// hours of no routine, but waits for a free slot and the daily cap like any other.
func (p *pass) gate(r *routineRun, m *match, sr *stepRun, info catalog.Step) verdict {
	agent, github := info.Kind == catalog.StepAgent, info.Kind == catalog.StepGitHub
	id := store.Deref(m.ItemID)

	for _, check := range []func() verdict{
		func() verdict { return p.askFirst(r, m) },
		func() verdict { return p.held(r, m) },
		func() verdict { return p.limits(r, m, id, agent, github) },
		func() verdict { return p.room(r, sr, agent) },
		func() verdict { return p.itemRules(m, id, sr, agent) },
	} {
		if v := check(); !v.through() {
			return v
		}
	}

	return verdict{}
}

// held holds every start, a local step's too, while Autopilot is paused, and outside the
// routine's active hours, which a manual run keeps to no routine's.
func (p *pass) held(r *routineRun, m *match) verdict {
	switch {
	case p.inbox.SignedOut:
		return wait(BlockSignedOut, "No one is signed in to GitHub; it starts once you sign in again.")
	case p.cfg.Paused:
		return wait(BlockPaused, "Autopilot is paused.")
	case !m.Manual && !inActiveHours(r.spec.Limits.ActiveHours, p.now):
		return wait(BlockActiveHours, "It waits for the routine's active hours.")
	}

	return verdict{}
}

// room checks there is room for an agent's run: a free slot, and the daily cap.
func (p *pass) room(r *routineRun, sr *stepRun, agent bool) verdict {
	switch {
	case !agent:
		return verdict{}
	case p.e.runnerClosed:
		return wait(BlockSlot, "The runner is closing, as the app quits; it starts on the next start.")
	case p.counts.activeAgents >= p.cfg.AgentsAtOnce:
		return wait(BlockSlot, textutil.Counted(p.cfg.AgentsAtOnce, "agent is", "agents are")+" working already.")
	}

	return p.spend(r, sr.Step.Agent)
}

// askFirst holds a start for the user's OK: a routine that asks first, and an Autopilot
// routine's start after a long gap.
func (p *pass) askFirst(r *routineRun, m *match) verdict {
	switch {
	case m.Manual || m.Approved:
		return verdict{}
	case r.mode == autopilotprefs.ModeAsk:
		return wait(BlockAsk, "Waiting for your OK.")
	case p.catchingUp(r) && p.searchFailing(r):
		return wait(BlockCatchUp, "Its search is failing, so what it matches may be out of date: it waits for your OK.")
	case p.catchingUp(r):
		return wait(BlockCatchUp, "Nothing was read for a while: it waits for your OK before it catches up.")
	}

	return verdict{}
}

// limits checks the routine's tries, the day's counts, and the wait between tries, none of
// which a manual run counts toward.
func (p *pass) limits(r *routineRun, m *match, id int64, agent, github bool) verdict {
	if m.Manual {
		return verdict{}
	}

	h := p.historyOf(r, id, "")
	lim := r.spec.Limits

	switch {
	case h.Tries >= lim.Tries():
		return verdict{
			closeAs: StateSkipped, reason: ReasonTries,
			words: "It gave up: " + textutil.Counted(lim.Tries(), "try", "tries") + " used.",
		}
	case p.counts.routineRuns[r.id()] >= lim.Runs():
		return wait(BlockCapRuns, "The routine ran "+textutil.Counted(lim.Runs(), "time", "times")+" today.")
	case agent && p.counts.agentRuns >= p.cfg.RunsPerDay:
		return wait(BlockCapRuns, "Autopilot ran "+textutil.Counted(p.cfg.RunsPerDay, "agent", "agents")+" today.")
	case github && p.counts.actions >= p.cfg.ActionsPerDay:
		return wait(BlockCapActions, "Autopilot sent "+
			textutil.Counted(p.cfg.ActionsPerDay, "GitHub action", "GitHub actions")+" today.")
	}

	if last := p.lastTry(r.id(), id); !last.IsZero() && p.now.Sub(last) < lim.Wait() {
		return wait(BlockWait, "It waits "+lim.Wait().String()+" between tries.")
	}

	return verdict{}
}

// lastTry returns when the routine's last counted run on the item ended, zero for none.
func (p *pass) lastTry(routineID, id int64) time.Time {
	var last time.Time

	for _, run := range p.runs[id] {
		if run.RoutineID == routineID && countsTry(run) && run.FinishedAt != nil && run.FinishedAt.After(last) {
			last = *run.FinishedAt
		}
	}

	return last
}

// spend checks the daily cap, and the routine's own, against the run's budget: today's use,
// plus the budgets of the runs still working, plus this one's, must fit under each. A
// budget in another unit than a cap's can't be counted against it.
func (p *pass) spend(r *routineRun, a *spec.AgentStep) verdict {
	amount, unit := runBudget(a)

	if c := p.cfg.DailyCap; !c.Off() && c.Unit == unit && p.counts.spend[unit]+amount > float64(c.Amount) {
		return wait(BlockCapSpend, "It would go over today's cap.")
	}

	if b := r.spec.Limits.BudgetPerDay; b != nil {
		limit, limitUnit := budgetOf(b)
		if limitUnit == unit && p.counts.routineSpend[r.id()][unit]+amount > limit {
			return wait(BlockCapSpend, "It would go over the routine's budget for today.")
		}
	}

	return verdict{}
}

// itemRules checks what the item itself allows: an outside contributor's item, a fork's
// branch for a run that pushes, and one run on the item at a time. A fork's pull request
// is an outside contributor's whoever opened it, so the fork's check can't hold anything
// until an org or a repository can allow agents on those; it is here for then.
func (p *pass) itemRules(m *match, id int64, sr *stepRun, agent bool) verdict {
	if f, ok := p.facts[id]; ok {
		if v := outsideRule(f, agent, m.Approved); !v.through() {
			return v
		}

		fork := f.Value(catalog.FieldFromFork)
		if agent && sr.Step.Agent.MayPush() && fork.Known() && fork.Bool {
			return wait(BlockCrossRepo, "Its branch is in a fork, which a run can't push to.")
		}
	}

	if p.counts.activeItems[id] {
		return wait(BlockBusyWorktree, "Something else runs on it; it starts once that ends.")
	}

	return verdict{}
}

// outsideRule holds an outside contributor's item: an agent never runs on one, which
// closes its match, and a GitHub action waits for the user's OK.
func outsideRule(f facts.Facts, agent, approved bool) verdict {
	t := f.Value(catalog.FieldTrust)

	switch {
	case !t.Known() || t.Text != string(trust.ClassOutside) || approved && !agent:
		return verdict{}
	case agent:
		return verdict{
			closeAs: StateSkipped, reason: ReasonOutside,
			words: "An outside contributor's item: its org or repository doesn't allow agents on these.",
		}
	}

	return wait(BlockOutside, "An outside contributor's item: it waits for your OK.")
}

// runBudget returns an agent step's budget per run and its unit.
func runBudget(a *spec.AgentStep) (float64, autopilotprefs.Unit) {
	if a != nil && a.Fence != nil && a.Fence.Budget != nil {
		return budgetOf(a.Fence.Budget)
	}

	return float64(catalog.DefaultRunCents) / 100, autopilotprefs.UnitUSD
}

// budgetOf returns a budget's amount and unit.
func budgetOf(b *spec.Budget) (float64, autopilotprefs.Unit) {
	if b.Tokens != nil {
		return float64(*b.Tokens), autopilotprefs.UnitTokens
	}

	if b.USD != nil {
		return *b.USD, autopilotprefs.UnitUSD
	}

	return 0, autopilotprefs.UnitUSD
}

// inActiveHours reports whether now is within a routine's active hours: always with none.
// Hours whose end comes before their start run past midnight, from the start's day.
func inActiveHours(h *spec.ActiveHours, now time.Time) bool {
	if h == nil {
		return true
	}

	from, okFrom := minutesOf(h.From)
	to, okTo := minutesOf(h.To)

	if !okFrom || !okTo {
		return true
	}

	t := now.Hour()*60 + now.Minute()
	day := weekdayOf(now)

	switch {
	case from <= to:
		return onDay(h.Days, day) && t >= from && t < to
	case t >= from:
		return onDay(h.Days, day)
	case t < to:
		return onDay(h.Days, weekdayOf(now.AddDate(0, 0, -1)))
	}

	return false
}

// minutesOf reads "HH:MM" as minutes past midnight.
func minutesOf(s string) (int, bool) {
	hh, mm, ok := strings.Cut(s, ":")
	if !ok {
		return 0, false
	}

	h, errH := strconv.Atoi(hh)
	m, errM := strconv.Atoi(mm)

	return h*60 + m, errH == nil && errM == nil
}

// weekdayOf names t's day as the catalog does.
func weekdayOf(t time.Time) catalog.Weekday {
	return catalog.Weekdays()[(int(t.Weekday())+6)%7]
}

// onDay reports whether days holds day; no days is every day.
func onDay(days []catalog.Weekday, day catalog.Weekday) bool {
	if len(days) == 0 {
		return true
	}

	for _, d := range days {
		if d == day {
			return true
		}
	}

	return false
}

// job is what a dispatcher is asked to run for m's step.
func (p *pass) job(r *routineRun, m *match, sr *stepRun) Job {
	j := Job{
		MatchID: m.ID, RoutineID: r.id(), RoutineName: r.row.Name, Head: m.HeadOID, Step: sr.Step,
		Index: int(m.Step), Attempt: int(m.Attempt), Manual: m.Manual,
		Item: ItemRef{
			ID: store.Deref(m.ItemID), Kind: item.Kind(m.Kind), Repo: m.RepoOwner + "/" + m.RepoName,
			Number: int(m.Number), Title: m.Title, URL: m.URL,
		},
	}

	if sr.Step.Agent != nil {
		amount, unit := runBudget(sr.Step.Agent)
		j.Budget, j.Unit = amount, string(unit)
	}

	return j
}

// runnerGitHub is the runner of a GitHub action: the app's own write.
const runnerGitHub = "github"

// runnerOf names who runs a step: an agent in the background or the terminal, or the
// app's own GitHub write.
func runnerOf(st spec.Step) string {
	if st.Agent != nil {
		return string(st.Agent.Place())
	}

	return runnerGitHub
}

// start records the run, moves m to running, and hands the step to its dispatcher on a
// worker. The one-active-run index refuses a second active run on the item, which waits.
func (p *pass) start(ctx context.Context, r *routineRun, m *match, sr *stepRun, info catalog.Step) {
	job := p.job(r, m, sr)
	runner := runnerOf(sr.Step)

	params := queries.InsertRunParams{
		MatchID: m.ID, RoutineID: r.id(), ItemID: m.ItemID, RepoOwner: m.RepoOwner, RepoName: m.RepoName, Kind: m.Kind,
		Number: m.Number, Title: m.Title, URL: m.URL, Step: m.Step, Runner: runner, Status: runStarting,
		HeadBefore: m.HeadOID, BudgetReserved: job.Budget, BudgetUnit: job.Unit, Manual: m.Manual,
		Counted: !m.Manual, QueuedAt: p.now, StartedAt: new(p.now), UpdatedAt: p.now,
	}

	if sr.Step.Agent != nil {
		params.Agent, params.Preset = sr.Step.Agent.AgentID(), string(sr.Step.Agent.Tools())
	}

	if len(sr.Step.GitHub) > 0 {
		params.GithubAction = string(sr.Step.GitHub[0].Kind())
	}

	sr.Status, sr.Started = stepRunning, new(p.now)

	m.set(StateRunning, "", "")
	m.Attempt++

	err := p.e.ledger.inTx(ctx, func(q *queries.Queries) error {
		id, insertErr := q.InsertRun(ctx, params)
		if insertErr != nil {
			return insertErr
		}

		job.RunID, sr.RunID = id, id

		return p.e.ledger.update(ctx, q, m)
	})
	if err != nil {
		m.AutopilotMatch, m.steps = m.orig, mustSteps(m.origSteps)

		if store.IsUniqueViolation(err) {
			m.wait(BlockBusyWorktree, "Something else runs on it; it starts once that ends.")
		} else {
			p.e.logger.WarnContext(ctx, "Autopilot could not record a run", "match", m.ID, "error", err)
		}

		return
	}

	p.cycle.Starts++
	p.counts.started(r.id(), store.Deref(m.ItemID), runner, job, m.Manual)
	p.e.dispatch(ctx, m.ID, job.RunID, info.Dispatcher, job)
}

// mustSteps decodes stored steps, empty when they can't be.
func mustSteps(raw string) []stepRun {
	steps, err := store.DecodeJSON[[]stepRun](raw)
	if err != nil {
		return []stepRun{}
	}

	return nonNil(steps)
}

// The statuses of a run.
const (
	runStarting = "starting"
	runRunning  = "running"
	runStopping = "stopping"
	runEnded    = "ended"
)

// dispatch hands job to the dispatcher d on a worker, and records how it went on the
// loop.
func (e *Engine) dispatch(ctx context.Context, matchID, runID int64, d catalog.Dispatcher, job Job) {
	disp := e.dispatchers[d]
	e.inflight[matchID] = true

	e.goWork(ctx, func(ctx context.Context) {
		res, err := disp.Dispatch(ctx, job)

		e.mail.post(func(ctx context.Context) {
			delete(e.inflight, matchID)
			e.applyDispatch(ctx, matchID, runID, res, err)
			e.Wake()
		})
	})
}

// applyDispatch records how a dispatch went: a local step's end, a run that goes on, or a
// GitHub action sent, not sent, or held for a warning. The run's end and the match's move
// are written in one transaction, so neither is ever recorded without the other.
func (e *Engine) applyDispatch(ctx context.Context, matchID, runID int64, res Result, err error) {
	now := e.clock.Now()

	txErr := e.ledger.inTx(ctx, func(q *queries.Queries) error {
		m, getErr := e.ledger.getIn(ctx, q, matchID)

		switch {
		case errors.Is(getErr, ErrNoMatch):
			return nil
		case getErr != nil:
			return getErr
		case !m.state().Open() || int(m.Step) >= len(m.steps):
			return nil
		}

		sr := &m.steps[m.Step]
		moved := true

		var writeErr error

		if err != nil {
			writeErr = e.dispatchFailed(ctx, q, m, sr, runID, err, now)
		} else {
			moved, writeErr = e.dispatchAnswered(ctx, q, m, sr, runID, res, now)
		}

		if writeErr != nil || !moved {
			return writeErr
		}

		return e.saveIn(ctx, q, m)
	})
	if txErr != nil {
		e.logger.WarnContext(ctx, "Autopilot could not record how a step went", "match", matchID, "error", txErr)
	}
}

// dispatchFailed records a dispatch that failed: one the runner refused as it closed
// never started, and stays queued for the next start, as no run and no try; any other
// fails its step.
func (e *Engine) dispatchFailed(ctx context.Context, q *queries.Queries, m *match, sr *stepRun, runID int64, err error,
	now time.Time,
) error {
	if runID != 0 && errors.Is(err, ErrClosed) {
		e.runnerClosed = true

		if _, endErr := e.endRunIn(ctx, q, runID, RunEnd{Outcome: RunNotStarted, Error: err.Error(), EndedAt: now}, false, false); endErr != nil {
			return endErr
		}

		sr.Status, sr.RunID, sr.Started = stepPending, 0, nil

		m.unstarted()
		m.set(StateQueued, "", "")

		return nil
	}

	if endErr := e.endRunIf(ctx, q, runID, RunEnd{Outcome: RunFailed, Error: err.Error(), EndedAt: now}, true); endErr != nil {
		return endErr
	}

	sr.Status, sr.Note, sr.Ended = stepFailed, textutil.Sentence(err.Error()), new(now)
	m.Step++

	m.set(StateQueued, "", "")

	return nil
}

// dispatchAnswered records a dispatch's answer, and reports whether m changed: a run that
// goes on changes only the run.
func (e *Engine) dispatchAnswered(ctx context.Context, q *queries.Queries, m *match, sr *stepRun, runID int64, res Result,
	now time.Time,
) (bool, error) {
	var err error

	switch {
	case res.Running:
		_, err = q.SetRunStatus(ctx, queries.SetRunStatusParams{Status: runRunning, UpdatedAt: now, ID: runID, Was: runStarting})

		return false, err
	case res.NotOffered != "":
		err = e.endRunIf(ctx, q, runID, RunEnd{Outcome: RunNotSent, Summary: res.NotOffered, EndedAt: now}, false)
		m.close(StateClosed, ReasonNotOffered, res.NotOffered, now)
	case res.Warning != "":
		err = e.endRunIf(ctx, q, runID, RunEnd{Outcome: RunNotSent, Summary: res.Warning, EndedAt: now}, false)

		sr.Status, sr.RunID = stepPending, 0
		m.Approved = false

		m.wait(BlockWarning, res.Warning)
	default:
		outcome := RunDone
		if runID != 0 && !res.Sent {
			outcome = RunNotSent
		}

		err = e.endRunIf(ctx, q, runID, RunEnd{Outcome: outcome, Summary: res.Summary, EndedAt: now}, outcome == RunDone)

		sr.Status, sr.Outcome, sr.Note, sr.Ended = stepDone, string(res.Outcome), res.Summary, new(now)
		m.Step++

		m.set(StateQueued, "", "")
	}

	return true, err
}

// endRunIf ends the run runID, when there is one, through q.
func (e *Engine) endRunIf(ctx context.Context, q *queries.Queries, runID int64, end RunEnd, counted bool) error {
	if runID == 0 {
		return nil
	}

	_, err := e.endRunIn(ctx, q, runID, end, counted, false)

	return err
}

// endRunIn records through q how a run ended, and reports whether it did: while it hadn't
// ended, or with late, over the interrupted end the engine recorded for it.
func (e *Engine) endRunIn(ctx context.Context, q *queries.Queries, runID int64, end RunEnd, counted, late bool,
) (bool, error) {
	if end.EndedAt.IsZero() {
		end.EndedAt = e.clock.Now()
	}

	params := queries.EndRunParams{
		Outcome: string(end.Outcome), RunCommit: end.Commit, HeadAfter: end.Head, Cost: end.Cost,
		TokensIn: end.TokensIn, TokensOut: end.TokensOut, CacheReadTokens: end.CacheRead, Turns: end.Turns,
		Summary: end.Summary, Error: end.Error, Counted: counted, FinishedAt: new(end.EndedAt), ID: runID,
	}

	var (
		n   int64
		err error
	)

	if late {
		n, err = q.ReviseRunEnd(ctx, queries.ReviseRunEndParams(params))
	} else {
		n, err = q.EndRun(ctx, params)
	}

	if err != nil {
		return false, fmt.Errorf("record the end of run %d: %w", runID, err)
	}

	return n > 0, nil
}

// endAndMove records how run ended and, in the same transaction, moves its match as move
// says, while the match is open with the run's step, so a run's end and its match's move
// are written together or not at all.
func (e *Engine) endAndMove(ctx context.Context, run queries.AutopilotRun, end RunEnd, counted, late bool,
	move func(m *match),
) error {
	return e.ledger.inTx(ctx, func(q *queries.Queries) error {
		ended, err := e.endRunIn(ctx, q, run.ID, end, counted, late)
		if err != nil || !ended {
			return err
		}

		m, err := e.ledger.getIn(ctx, q, run.MatchID)

		switch {
		case errors.Is(err, ErrNoMatch):
			return nil
		case err != nil:
			return err
		case !m.state().Open() || int(run.Step) >= len(m.steps):
			return nil
		}

		move(m)

		return e.saveIn(ctx, q, m)
	})
}

// setRunStatus moves a run from one status to the next.
func (e *Engine) setRunStatus(ctx context.Context, runID int64, was, status string) {
	_, err := e.ledger.q.SetRunStatus(ctx, queries.SetRunStatusParams{
		Status: status, UpdatedAt: e.clock.Now(), ID: runID, Was: was,
	})
	if err != nil {
		e.logger.WarnContext(ctx, "Autopilot could not record a run's status", "run", runID, "error", err)
	}
}

// applyRunEnd records how a run its runner told of ended, and moves its match on, in one
// transaction: an interrupted run waits for the user, one stopped closes its match, and one
// that ended goes on to the next step, verifying when it pushed and the checks are next. A
// stop the engine asked for the run's item names why it was stopped, even when that stop
// failed and the run ended stopped anyway. A runner's word that comes once the engine
// recorded the run interrupted, having found it gone, is kept over that record, its cost
// and its commit with it, and moves the match on while it still waits on that
// interruption. An end that couldn't be written is tried again on the next tick.
func (e *Engine) applyRunEnd(ctx context.Context, end RunEnd) {
	run, late, ok := e.endable(ctx, end)
	if !ok {
		return
	}

	// Only the engine knows a start never happened; a runner that says so of a run it was
	// handed is taken as having been interrupted.
	if end.Outcome == RunNotStarted {
		end.Outcome = RunInterrupted
	}

	told, forItem := e.stoppedFor(end)

	err := e.endAndMove(ctx, run, told, endCounts(run, told), late, func(m *match) {
		if late {
			if !waitsOn(m, run) {
				return
			}

			m.steps[run.Step].RunID = run.ID
		}

		e.moveOn(m, run.Step, told, forItem, e.clock.Now())
	})
	if err != nil {
		e.logger.WarnContext(ctx, "Autopilot could not record a run's end; it tries again", "run", end.RunID, "error", err)
		e.unsaved[end.RunID] = end

		return
	}

	delete(e.stopping, end.RunID)
	delete(e.gone, end.RunID)
	e.Wake()
}

// retryEnds applies again the runs' ends that couldn't be written.
func (e *Engine) retryEnds(ctx context.Context) {
	for id, end := range e.unsaved {
		delete(e.unsaved, id)
		e.applyRunEnd(ctx, end)
	}
}

// endCounts reports whether a run that ended as end says counts as a try: not a manual
// run, an interrupted one, one the engine stopped for its item, or a GitHub action that sent
// nothing.
func endCounts(run queries.AutopilotRun, end RunEnd) bool {
	return !run.Manual && end.Outcome != RunInterrupted && end.Outcome != RunNotStarted && !end.StoppedForMe &&
		end.Outcome != RunNotSent
}

// endable reads the run end is for, and reports whether end applies to it, and late when
// it does over the interrupted end the engine recorded: a runner's end applies to a run not
// ended yet, or to one so recorded, which it tells more of.
func (e *Engine) endable(ctx context.Context, end RunEnd) (queries.AutopilotRun, bool, bool) {
	run, err := e.ledger.q.GetRun(ctx, end.RunID)
	if err != nil {
		return run, false, false
	}

	if run.Status != runEnded {
		return run, false, true
	}

	return run, true, run.Outcome == string(RunInterrupted) && end.Outcome != RunInterrupted
}

// stoppedFor returns end as the engine records it, and why the engine stopped the run, ""
// when it didn't: a run the engine asked to stop ended stopped for its item, as did one
// that ended stopped after a stop that failed.
func (e *Engine) stoppedFor(end RunEnd) (RunEnd, Reason) {
	ask, asked := e.stopping[end.RunID]
	if !asked || !ask.failedAt.IsZero() && end.Outcome != RunStopped {
		return end, ""
	}

	end.Outcome, end.StoppedForMe = RunStopped, true

	return end, ask.reason
}

// waitsOn reports a match that still waits on run's interruption: at the run's step, with
// nothing started since.
func waitsOn(m *match, run queries.AutopilotRun) bool {
	sr := m.steps[run.Step]

	return m.state() == StateWaiting && Blocker(m.BlockedBy) == BlockInterrupted && m.Step == run.Step &&
		sr.Status == stepPending && sr.RunID == 0
}

// moveOn moves m on from its step at index, whose run ended as end says: an interrupted
// run waits for the user, one stopped closes m, for its item when forItem says why, and
// one that ended goes on to the next step, verifying when it pushed and the checks are
// next.
func (e *Engine) moveOn(m *match, index int64, end RunEnd, forItem Reason, now time.Time) {
	sr := &m.steps[index]
	sr.Ended = new(now)

	switch end.Outcome {
	case RunInterrupted, RunNotStarted:
		sr.Status, sr.RunID, sr.Ended = stepPending, 0, nil
		m.Approved = false

		m.wait(BlockInterrupted, interruptedWords(m, sr))
	case RunStopped:
		if end.StoppedForMe {
			m.close(StateClosed, forItem, "Stopped: "+stoppedWords(forItem), now)
		} else {
			m.close(StateDismissed, ReasonYouStopped, "You stopped it.", now)
		}
	case RunFailed, RunNotSent:
		sr.Status, sr.Outcome, sr.Note = stepFailed, string(catalog.OutcomeNotPushed), textutil.Sentence(end.Error)
		m.Step = index + 1

		m.set(StateQueued, "", "")
	case RunDone:
		sr.Status, sr.Outcome, sr.Note = stepDone, string(catalog.OutcomeNotPushed), end.Summary
		if end.Commit != "" {
			sr.Outcome = string(catalog.OutcomePushed)
		}

		m.Step = index + 1

		m.set(StateQueued, "", "")
		e.verifyNext(m, now)
	}
}

// verifyNext moves a match whose next step to run follows the checks straight to
// verifying, so the push its run made, which moves its item's head, never closes it as
// having moved on.
func (e *Engine) verifyNext(m *match, now time.Time) {
	for i := int(m.Step); i < len(m.steps); i++ {
		sr := &m.steps[i]
		if !onlyIfHolds(m.steps[:i], sr.Step.OnlyIf) {
			continue
		}

		if sr.Step.Kind() == catalog.StepWaitForChecks {
			for j := int(m.Step); j < i; j++ {
				m.steps[j].Status, m.steps[j].Note = stepSkipped, "Its Only if didn't hold."
			}

			m.Step = int64(i)
			sr.Status, sr.Started = stepRunning, new(now)

			m.set(StateVerifying, "", "The checks on its commit haven't finished.")
		}

		return
	}
}

// interruptedWords says what an interrupted step's match waits for: a GitHub action the
// quit cut off may have been sent, so the user looks at the item before sending it again.
func interruptedWords(m *match, sr *stepRun) string {
	if len(sr.Step.GitHub) > 0 {
		return brand.Name + " quit while it sent this: check the " + item.Kind(m.Kind).Noun() +
			" on GitHub before you send it again."
	}

	return "It was interrupted: run it again?"
}

// stoppedWords says why the engine stopped a run.
func stoppedWords(r Reason) string {
	if words, ok := map[Reason]string{ReasonMerged: "it was merged.", ReasonItemClosed: "it was closed."}[r]; ok {
		return words
	}

	return "the item is gone."
}

// watchActive stops a running match's run once its item merged, closed, or is gone, and
// closes a verifying one. An item that left its routine's watch (watched, nil for a
// routine that no longer runs, says) has its state read first: a search drops what
// merged.
func (p *pass) watchActive(ctx context.Context, m *match, watched map[int64]bool) {
	reason, words, done := p.itemDone(m)
	if !done {
		if id := store.Deref(m.ItemID); watched != nil && !watched[id] && !m.Manual {
			p.readStateOnce(id)
		}

		return
	}

	if m.state() == StateVerifying {
		m.close(StateClosed, reason, words, p.now)
		p.save(ctx, m)

		return
	}

	for _, sr := range m.steps {
		if sr.Status == stepRunning && sr.RunID != 0 && sr.Step.Agent != nil {
			p.e.stopFor(ctx, sr.RunID, reason)
		}
	}
}

// readStateOnce asks for the state of an item that left a watch, unless it was read
// lately.
func (p *pass) readStateOnce(id int64) {
	if at, read := p.e.stateReads[id]; !read || p.now.Sub(at) > stateReadFor {
		p.stateReads = append(p.stateReads, id)
	}
}

// stopAsk is a stop the engine asked of a run for its item: the reason it was for, kept
// until the run ends, and when the last try failed, zero while one is under way or went
// through.
type stopAsk struct {
	reason   Reason
	failedAt time.Time
}

// stopFor stops a run because its item is done with, once at a time: a stop that fails
// leaves the run going, and a cycle a tick later or more stops it again. Its reason is
// kept, so a run that ends stopped anyway closes its match for it.
func (e *Engine) stopFor(ctx context.Context, runID int64, reason Reason) {
	if e.runner == nil {
		return
	}

	if ask, asked := e.stopping[runID]; asked && (ask.failedAt.IsZero() || time.Since(ask.failedAt) < e.tick) {
		return
	}

	e.stopping[runID] = stopAsk{reason: reason}
	e.setRunStatus(ctx, runID, runRunning, runStopping)

	e.goWork(ctx, func(ctx context.Context) {
		err := e.runner.Stop(ctx, runID)
		if err == nil || ctx.Err() != nil {
			return
		}

		e.logger.WarnContext(ctx, "Autopilot could not stop a run; it tries again", "run", runID, "error", err)

		e.mail.post(func(ctx context.Context) {
			if ask, asked := e.stopping[runID]; asked {
				ask.failedAt = time.Now()
				e.stopping[runID] = ask
			}

			e.setRunStatus(ctx, runID, runStopping, runRunning)
		})
	})
}

// unknownFor is how long a run its runner can't report on may go unknown before it ends
// interrupted: longer than the longest time limit a run may have, by which its runner has
// ended it, if it ran at all.
const unknownFor = time.Duration(catalog.MaxTimeoutMinutes)*time.Minute + 30*time.Minute

// reconcile checks the runs the ledger records as going, once this copy holds the engine
// and on every tick after, leaving out those a worker of this copy is starting or
// dispatching now, and those whose end is on its way (settleRun).
func (e *Engine) reconcile(ctx context.Context) {
	if e.checking {
		return
	}

	runs, err := e.ledger.q.ListActiveRuns(ctx)
	if err != nil {
		e.logger.WarnContext(ctx, "Autopilot could not read the runs going", "error", err)

		return
	}

	e.forgetGone(runs)

	runs = slices.DeleteFunc(runs, func(r queries.AutopilotRun) bool { return e.inflight[r.MatchID] })
	if len(runs) == 0 {
		return
	}

	e.checking = true

	e.goWork(ctx, func(ctx context.Context) {
		found := make([]Liveness, len(runs))
		for i, run := range runs {
			found[i] = e.alive(ctx, run)
		}

		e.mail.post(func(ctx context.Context) {
			e.checking = false
			e.checks++

			for i, run := range runs {
				e.settleRun(ctx, run, found[i])
			}
		})
	})
}

// forgetGone forgets what the checks found of runs no longer going.
func (e *Engine) forgetGone(runs []queries.AutopilotRun) {
	going := make(map[int64]bool, len(runs))
	for _, r := range runs {
		going[r.ID] = true
	}

	maps.DeleteFunc(e.gone, func(id int64, _ sighting) bool { return !going[id] })
}

// settleRun settles a run the ledger records as going that no worker of this copy is
// starting, and whose RunEnded isn't on its way.
//
// A GitHub action is never sent again on its own: one still recorded as going was cut
// off by a quit or a crash, maybe after GitHub took it, so its match waits for the user,
// who checks the item first. An agent's run its runner never started (still starting, as
// a start the quit cut off before it reached the runner) goes back to the queue, its match
// with it, as no try; one that ended unseen ends interrupted. Either is taken as so only
// once two checks a tick apart found it so, as a runner may let go of a run a moment before
// its end is delivered. A run the runner can't say of is asked again on the next tick, and
// one still unknown unknownFor after it started ends interrupted, so no run is taken as
// going for good.
func (e *Engine) settleRun(ctx context.Context, run queries.AutopilotRun, found Liveness) {
	if _, unsaved := e.unsaved[run.ID]; unsaved || e.inflight[run.MatchID] || e.endPending(run.ID) {
		return
	}

	if run.Runner == runnerGitHub {
		e.applyRunEnd(ctx, RunEnd{RunID: run.ID, Outcome: RunInterrupted, Error: "the app quit while it sent this"})

		return
	}

	e.settleAgentRun(ctx, run, found)
}

// settleAgentRun settles an agent's run by what its runner said of it (settleRun).
func (e *Engine) settleAgentRun(ctx context.Context, run queries.AutopilotRun, found Liveness) {
	switch found {
	case LiveAlive:
		delete(e.gone, run.ID)
	case LiveNever, LiveGone:
		confirmed, never := e.confirmGone(run.ID, found)
		if !confirmed {
			return
		}

		if never && run.Status == runStarting {
			e.unstart(ctx, run)
		} else {
			e.applyRunEnd(ctx, RunEnd{RunID: run.ID, Outcome: RunInterrupted, Error: "the app stopped while it ran"})
		}
	case LiveUnknown:
		if run.StartedAt != nil && e.clock.Now().Sub(*run.StartedAt) > unknownFor {
			e.applyRunEnd(ctx, RunEnd{RunID: run.ID, Outcome: RunInterrupted, Error: "its runner couldn't say how it went"})
		}
	}
}

// sighting is the first check that found a run gone or never started, and whether it
// said never.
type sighting struct {
	check int
	never bool
}

// confirmGone reports whether an earlier check found the run gone too, or never started,
// and never when both said the runner never started it; the first check to find it so is
// only noted.
func (e *Engine) confirmGone(runID int64, found Liveness) (bool, bool) {
	first, seen := e.gone[runID]
	if !seen {
		e.gone[runID] = sighting{check: e.checks, never: found == LiveNever}

		return false, false
	}

	if first.check >= e.checks {
		return false, false
	}

	delete(e.gone, runID)

	return true, first.never && found == LiveNever
}

// unstart puts a run that never started back in the queue, in one transaction: the run
// ends as not started, counting as no run and no try, and its match, still at that run,
// waits to start it again, its OK kept, and is decided again like any queued match.
func (e *Engine) unstart(ctx context.Context, run queries.AutopilotRun) {
	err := e.endAndMove(ctx, run, RunEnd{Outcome: RunNotStarted, Error: "it never started"}, false, false,
		func(m *match) { putBack(m, run) })
	if err != nil {
		e.logger.WarnContext(ctx, "Autopilot could not put a start back in the queue", "run", run.ID, "error", err)

		return
	}

	e.Wake()
}

// putBack puts m, while it is still at run, back in the queue to start run's step again,
// taking back the run its start counted.
func putBack(m *match, run queries.AutopilotRun) {
	sr := &m.steps[run.Step]
	if m.state() != StateRunning || m.Step != run.Step || sr.RunID != run.ID {
		return
	}

	sr.Status, sr.RunID, sr.Started = stepPending, 0, nil

	m.unstarted()
	m.set(StateQueued, "", "")
}

// unstrand finds the running matches whose run at their step has ended, as when a match's
// move was lost once its run's end was written, and moves each on by that end, so no match
// is left running for good: a start that never happened goes back to the queue, an end the
// ledger keeps moves it on as it would have, and one it can't tell (a stop, whoever asked
// for it) waits for the user as interrupted.
func (e *Engine) unstrand(ctx context.Context) {
	open, err := e.ledger.open(ctx)
	if err != nil {
		e.logger.WarnContext(ctx, "Autopilot could not read the open matches", "error", err)

		return
	}

	for _, m := range open {
		if run, stranded := e.stranded(ctx, m); stranded {
			e.moveByRecord(m, run)
			e.save(ctx, m)
		}
	}
}

// stranded returns the run m is running at its step, and reports whether the ledger
// records it as ended with no end of it on its way.
func (e *Engine) stranded(ctx context.Context, m *match) (queries.AutopilotRun, bool) {
	if m.state() != StateRunning || e.inflight[m.ID] || int(m.Step) >= len(m.steps) {
		return queries.AutopilotRun{}, false
	}

	sr := m.steps[m.Step]
	if sr.Status != stepRunning || sr.RunID == 0 {
		return queries.AutopilotRun{}, false
	}

	run, err := e.ledger.q.GetRun(ctx, sr.RunID)
	_, unsaved := e.unsaved[run.ID]

	return run, err == nil && run.Status == runEnded && !unsaved && !e.endPending(run.ID)
}

// moveByRecord moves m on by how the ledger recorded run ended.
func (e *Engine) moveByRecord(m *match, run queries.AutopilotRun) {
	end := RunEnd{
		RunID: run.ID, Outcome: RunOutcome(run.Outcome), Commit: run.RunCommit, Head: run.HeadAfter,
		Summary: run.Summary, Error: run.Error,
	}
	now := e.clock.Now()

	switch {
	case end.Outcome == RunNotStarted:
		putBack(m, run)
	case run.Runner == runnerGitHub && (end.Outcome == RunDone || end.Outcome == RunNotSent):
		cur := &m.steps[m.Step]
		cur.Status, cur.Note, cur.Ended = stepDone, end.Summary, new(now)
		m.Step++

		m.set(StateQueued, "", "")
	case end.Outcome == RunDone, end.Outcome == RunFailed, end.Outcome == RunInterrupted:
		e.moveOn(m, m.Step, end, "", now)
	default:
		e.moveOn(m, m.Step, RunEnd{RunID: run.ID, Outcome: RunInterrupted}, "", now)
	}
}

// alive says what is known of a run the ledger records as going: a GitHub action is
// asked of no one (settleRun), a run with no runner to ask is gone, and a runner that
// can't say, or answers what the engine doesn't know, is unknown.
func (e *Engine) alive(ctx context.Context, run queries.AutopilotRun) Liveness {
	switch {
	case run.Runner == runnerGitHub:
		return LiveUnknown
	case e.runner == nil:
		return LiveGone
	}

	live, err := e.runner.Alive(ctx, run.ID)

	switch {
	case err != nil:
		return LiveUnknown
	case live == LiveAlive, live == LiveGone, live == LiveNever:
		return live
	}

	return LiveUnknown
}

// dayCounts are today's counts against the limits, from local midnight.
type dayCounts struct {
	routineRuns  map[int64]int
	agentRuns    int
	actions      int
	activeAgents int
	activeItems  map[int64]bool
	spend        map[autopilotprefs.Unit]float64
	routineSpend map[int64]map[autopilotprefs.Unit]float64
}

// dayCounts reads today's counts.
func (e *Engine) dayCounts(ctx context.Context, now time.Time) (dayCounts, error) {
	out := dayCounts{
		routineRuns: map[int64]int{}, activeItems: map[int64]bool{}, spend: map[autopilotprefs.Unit]float64{},
		routineSpend: map[int64]map[autopilotprefs.Unit]float64{},
	}

	day := dayStart(now)

	rows, err := e.ledger.q.ListRunsSince(ctx, day)
	if err != nil {
		return out, fmt.Errorf("read today's runs: %w", err)
	}

	for _, r := range rows {
		out.add(r, day)
	}

	return out, nil
}

// add counts one of the day's runs; one that never started counts toward nothing.
func (d *dayCounts) add(r queries.ListRunsSinceRow, day time.Time) {
	if RunOutcome(r.Outcome) == RunNotStarted {
		return
	}

	active := r.Status == runStarting || r.Status == runRunning || r.Status == runStopping
	today := !r.QueuedAt.Before(day)

	if active && r.ItemID != nil {
		d.activeItems[*r.ItemID] = true
	}

	if active && r.Runner != runnerGitHub {
		d.activeAgents++
	}

	if today && !r.Manual {
		d.countRun(r.RoutineID, r.Runner)
	}

	d.addSpend(r, active, today)
}

// countRun counts a run toward the day's counts: its routine's, and the agents' or the
// GitHub actions'.
func (d *dayCounts) countRun(routineID int64, runner string) {
	d.routineRuns[routineID]++

	if runner == runnerGitHub {
		d.actions++
	} else {
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

	if runner != runnerGitHub {
		d.activeAgents++
	}

	if !manual {
		d.countRun(routineID, runner)
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
