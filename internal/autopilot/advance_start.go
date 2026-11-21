package autopilot

import (
	"cmp"
	"context"
	"errors"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// job is what a dispatcher is asked to run for m's step.
func (p *pass) job(r *routineRun, m *match, sr *stepRun) Job {
	j := Job{
		MatchID: m.ID, RoutineID: r.id(), RoutineName: r.row.Name, Head: m.HeadOID, Step: sr.Step,
		Index: int(m.Step), Action: sr.Action, Accept: sr.Accepted, Attempt: int(m.Attempt), Manual: m.Manual,
		Item: ItemRef{
			ID: store.Deref(m.ItemID), Kind: item.Kind(m.Kind), Repo: m.RepoOwner + "/" + m.RepoName,
			Number: int(m.Number), Title: m.Title, URL: m.URL,
		},
		MatchCreated: m.CreatedAt, Notices: r.spec.Notify, Reason: reasonOf(m.trace),
		Before: matchView(m, r.row.Name).Steps[:min(int(m.Step), len(m.steps))], Trigger: r.spec.Trigger.Event,
		Confirmed: append([]string{}, sr.Confirmed...),
	}

	if sr.Step.Agent != nil {
		amount, unit := runBudget(sr.Step.Agent)
		j.Budget, j.Unit = amount, string(unit)
		j.Resting = p.e.restingNow(p.now)
	}

	return j
}

// runnerGitHub is the runner of a GitHub action: the app's own write.
const runnerGitHub = "github"

// headNow returns the head commit of m's item as the cycle read it, which the read right
// before a start stored, else the head m was decided on: "" for an issue.
func (p *pass) headNow(m *match) string {
	if f, ok := p.facts[store.Deref(m.ItemID)]; ok && f.Head.Known() && f.Head.Text != "" {
		return f.Head.Text
	}

	return m.HeadOID
}

// runnerOf names who runs a step: an agent in the background or the terminal, one of the
// user's scripts, or the app's own GitHub write.
func runnerOf(st spec.Step) string {
	switch {
	case st.Agent != nil:
		return string(st.Agent.Place())
	case st.Script != nil:
		return runnerScript
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

	p.pin(ctx, r, m, sr, &job, &params)

	sr.Status = stepRunning
	if sr.Started == nil {
		sr.Started = new(p.now)
	}

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

// pin pins a GitHub write's run to its head, in job and in the run's record: each of a
// GitHub step's actions is a run of its own, pinned to the head the read right before it
// found, which this cycle decided the match on; Merge it and Post the agent's review to the
// commit their run's checks passed on, or the agent read (followUp).
func (p *pass) pin(ctx context.Context, r *routineRun, m *match, sr *stepRun, job *Job, params *queries.InsertRunParams) {
	if sr.Action < len(sr.Step.GitHub) && len(sr.Step.GitHub) > 0 {
		params.GithubAction = string(sr.Step.GitHub[sr.Action].Kind())
		job.Head = p.headNow(m)
		params.HeadBefore = job.Head

		return
	}

	if head, ok := p.followUp(ctx, r, m, sr, job); ok {
		params.HeadBefore = head
	}
}

// mustSteps decodes stored steps, empty when they can't be.
func mustSteps(raw string) []stepRun {
	steps, err := store.DecodeJSON[[]stepRun](raw)
	if err != nil {
		return []stepRun{}
	}

	return textutil.NonNil(steps)
}

// The statuses of a run.
const (
	runStarting = "starting"
	runRunning  = "running"
	runStopping = "stopping"
	runEnded    = "ended"
)

// dispatch hands job to the dispatcher d on a worker, and records how it went on the
// loop. Every start goes through it, and it asks Enabled right before the step is handed
// over: a step whose cycle came just before Autopilot was turned off isn't, and waits,
// as one that never started (ErrOff), for Autopilot to be on again.
func (e *Engine) dispatch(ctx context.Context, matchID, runID int64, d catalog.Dispatcher, job Job) {
	disp := e.dispatchers[d]
	e.inflight[matchID] = true

	e.goWork(ctx, func(ctx context.Context) {
		var (
			res Result
			err = ErrOff
		)

		if e.Enabled() {
			res, err = disp.Dispatch(ctx, job)
		}

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
// never started, and stays queued for the next start, as no run and no try, and so does
// one Autopilot was turned off before (ErrOff), a local step's too; any other fails its
// step.
func (e *Engine) dispatchFailed(ctx context.Context, q *queries.Queries, m *match, sr *stepRun, runID int64, err error,
	now time.Time,
) error {
	off := errors.Is(err, ErrOff)

	if off || (runID != 0 && errors.Is(err, ErrClosed)) {
		e.runnerClosed = e.runnerClosed || !off

		if runID != 0 {
			if _, endErr := e.endRunIn(ctx, q, runID, RunEnd{Outcome: RunNotStarted, Error: err.Error(), EndedAt: now}, false, false); endErr != nil {
				return endErr
			}

			m.unstarted()
		}

		sr.Status, sr.RunID, sr.Started = stepPending, 0, nil

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

	if err = e.recordLaunch(ctx, q, runID, res.Launched, now); err != nil {
		return false, err
	}

	switch {
	case res.Running:
		_, err = q.SetRunStatus(ctx, queries.SetRunStatusParams{Status: runRunning, UpdatedAt: now, ID: runID, Was: runStarting})

		return false, err
	case res.Wait != "" || res.Requeue:
		err = e.endRunIf(ctx, q, runID, RunEnd{Outcome: RunNotStarted, Summary: res.WaitWords, EndedAt: now}, false)
		e.heldAt(m, sr, res)
		e.holdBack(m, now, res.RetryAt)
	case res.unsent():
		err = e.unsent(ctx, q, m, sr, runID, res, now)
	default:
		outcome := RunDone
		if runID != 0 && !res.Sent {
			outcome = RunNotSent
		}

		err = e.endRunIf(ctx, q, runID, RunEnd{Outcome: outcome, Summary: res.Summary, EndedAt: now}, outcome == RunDone)

		stepEnded(m, sr, string(res.Outcome), res.Summary, now)
	}

	return true, err
}

// unsent records a GitHub action its sheet or the write gate kept from being sent: one no
// longer offered closes m; one offered with warnings waits for the user; after a push,
// nothing was sent and it is no try, so the item is read again and the cycle decides it
// again before the action starts once more; one the gate held waits until it may go.
func (e *Engine) unsent(ctx context.Context, q *queries.Queries, m *match, sr *stepRun, runID int64, res Result,
	now time.Time,
) error {
	words := cmp.Or(res.NotOffered, res.Warning, res.Held, res.Summary)
	err := e.endRunIf(ctx, q, runID, RunEnd{Outcome: RunNotSent, Summary: words, EndedAt: now}, false)

	if res.NotOffered != "" {
		m.close(StateClosed, ReasonNotOffered, res.NotOffered, now)

		return err
	}

	notSent(m, sr, runID)

	switch {
	case res.Warning != "":
		m.Approved = false

		m.wait(BlockWarning, res.Warning)
	case res.PushedSince:
		delete(e.freshReads, m.ID)
		m.set(StateQueued, "", "")
	default:
		e.holdWrites(now.Add(max(res.RetryIn, time.Second)), res.Held)
		m.wait(BlockWritesHeld, res.Held)
	}

	return err
}

// notSent puts back the step of m whose run runID sent nothing, to start again, and takes
// back the run its start counted, so it counts as no try: m is decided again like a match
// that started nothing, once nothing else of it went.
func notSent(m *match, sr *stepRun, runID int64) {
	sr.Status, sr.RunID = stepPending, 0

	if runID != 0 {
		m.unstarted()
	}
}

// stepEnded records that sr, m's step now, ended with outcome and note, and moves m on: to
// the step after it, or, for a GitHub step with another action to send, to that action,
// which starts as its own run once the limits let it.
func stepEnded(m *match, sr *stepRun, outcome, note string, now time.Time) {
	if len(sr.Step.GitHub) > 0 && sr.Action+1 < len(sr.Step.GitHub) {
		sr.Action++
		sr.Status, sr.RunID, sr.Accepted, sr.Note = stepPending, 0, "", note

		m.set(StateQueued, "", "")

		return
	}

	sr.Status, sr.Outcome, sr.Note, sr.Ended = stepDone, outcome, note, new(now)
	m.Step++

	m.set(StateQueued, "", "")
}

// holdWrites holds the GitHub actions until until, as the write gate asked, with words
// that say why; a hold already running past then stays as it is.
func (e *Engine) holdWrites(until time.Time, words string) {
	if until.After(e.writesHeld) {
		e.writesHeld, e.writesHeldWords = until, words
	}
}
