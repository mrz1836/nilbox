package autopilot

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/store"
)

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

	p.stopRuns(ctx, m, reason)
}

// stopRuns stops the runs m's steps have going, an agent's or a script's, each through
// its own runner, because its item is done with, as reason says.
func (p *pass) stopRuns(ctx context.Context, m *match, reason Reason) {
	for _, sr := range m.steps {
		if sr.Status == stepRunning && sr.RunID != 0 && (sr.Step.Agent != nil || sr.Step.Script != nil) {
			p.e.stopFor(ctx, p.e.runnerFor(runnerOf(sr.Step)), sr.RunID, reason)
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

// stopFor stops a run because its item is done with, through runner, its own, once at a
// time: a stop that fails leaves the run going, and a cycle a tick later or more stops it
// again. Its reason is kept, so a run that ends stopped anyway closes its match for it.
func (e *Engine) stopFor(ctx context.Context, runner Runner, runID int64, reason Reason) {
	if runner == nil {
		return
	}

	if ask, asked := e.stopping[runID]; asked && (ask.failedAt.IsZero() || time.Since(ask.failedAt) < e.tick) {
		return
	}

	e.stopping[runID] = stopAsk{reason: reason}
	e.setRunStatus(ctx, runID, runRunning, runStopping)

	e.goWork(ctx, func(ctx context.Context) {
		err := runner.Stop(ctx, runID)
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
	case run.Runner == runnerGitHub && end.Outcome == RunDone:
		stepEnded(m, &m.steps[m.Step], "", end.Summary, now)
	case run.Runner == runnerGitHub && end.Outcome == RunNotSent:
		// Nothing was sent, so sending it again sends it once.
		notSent(m, &m.steps[m.Step], run.ID)
		m.set(StateQueued, "", "")
	case end.Outcome == RunDone, end.Outcome == RunFailed, end.Outcome == RunInterrupted,
		end.Outcome == RunOutOfUsage:
		e.moveOn(m, m.Step, end, "", now)
	default:
		e.moveOn(m, m.Step, RunEnd{RunID: run.ID, Outcome: RunInterrupted}, "", now)
	}
}

// alive says what is known of a run the ledger records as going: a GitHub action is
// asked of no one (settleRun), a run with no runner to ask is gone, and a runner that
// can't say, or answers what the engine doesn't know, is unknown. A run in the terminal
// its runner never started is gone all the same: its launch may have opened the terminal
// before a quit or a crash cut it off, so it is never started again on its own.
func (e *Engine) alive(ctx context.Context, run queries.AutopilotRun) Liveness {
	runner := e.runnerFor(run.Runner)

	switch {
	case run.Runner == runnerGitHub:
		return LiveUnknown
	case runner == nil:
		return LiveGone
	}

	live, err := runner.Alive(ctx, run.ID)

	switch {
	case err != nil:
		return LiveUnknown
	case live == LiveNever && run.Runner == string(catalog.WhereTerminal):
		return LiveGone
	case live == LiveAlive, live == LiveGone, live == LiveNever:
		return live
	}

	return LiveUnknown
}
