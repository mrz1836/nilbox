package autopilot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

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
		CostEstimated: end.CostEstimated, TokensIn: end.TokensIn, TokensOut: end.TokensOut, CacheReadTokens: end.CacheRead, Turns: end.Turns,
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
	taken := e.takenOver[end.RunID]

	if told.Outcome == RunOutOfUsage {
		e.rest(ctx, run, told.Limit, e.clock.Now())
	}

	err := e.endAndMove(ctx, run, told, endCounts(run, told) && !taken, late, func(m *match) {
		e.moveEnded(m, run, told, forItem, late, taken)
	})
	if err != nil {
		e.logger.WarnContext(ctx, "Autopilot could not record a run's end; it tries again", "run", end.RunID, "error", err)
		e.unsaved[end.RunID] = end

		return
	}

	delete(e.stopping, end.RunID)
	delete(e.gone, end.RunID)
	delete(e.takenOver, end.RunID)

	// A run the user took over carries on in their terminal: nothing tells them it ended.
	if !taken {
		e.tellFinished(ctx, run, told)
	}

	e.Wake()
}

// moveEnded moves m on from run, which ended as end says: a late end only while m still
// waits on the run's interruption; a run the user took over closes m as taken over; any
// other moves on as moveOn says.
func (e *Engine) moveEnded(m *match, run queries.AutopilotRun, end RunEnd, forItem Reason, late, taken bool) {
	if late {
		if !waitsOn(m, run) {
			return
		}

		m.steps[run.Step].RunID = run.ID
	}

	if taken {
		tookOver(m, run.Step, e.clock.Now())

		return
	}

	e.moveOn(m, run.Step, end, forItem, e.clock.Now())
}

// takenOverWords is what a match whose run the user took over says.
const takenOverWords = "You took it over: it carries on in your terminal."

// tookOver closes m, done, at its step at index, whose run the user took over.
func tookOver(m *match, index int64, now time.Time) {
	if int(index) < len(m.steps) {
		sr := &m.steps[index]
		sr.Status, sr.Note, sr.Ended = stepDone, takenOverWords, new(now)
	}

	m.close(StateDone, ReasonTakenOver, takenOverWords, now)
}

// takeOver records that the user takes the run with runID over: a run still working is
// marked, so its end, which its Stop brings, closes its match as taken over, counting as no
// try and telling nothing; a run that ended already closes its match so when the match
// still waits on it (interrupted) or closed as the user stopped it. Any other match is left
// as it is: it moved on from the run already.
func (e *Engine) takeOver(ctx context.Context, runID int64) error {
	run, err := e.ledger.q.GetRun(ctx, runID)
	if err != nil {
		return uierr.New("That run isn't in Autopilot's history.", errors.Join(ErrNoRun, err))
	}

	if run.Status != runEnded {
		e.takenOver[runID] = true

		return nil
	}

	return e.ledger.inTx(ctx, func(q *queries.Queries) error {
		m, getErr := e.ledger.getIn(ctx, q, run.MatchID)

		switch {
		case errors.Is(getErr, ErrNoMatch):
			return nil
		case getErr != nil:
			return getErr
		case !waitsOnTakeOver(m, run):
			return nil
		}

		tookOver(m, run.Step, e.clock.Now())

		return e.saveIn(ctx, q, m)
	})
}

// waitsOnTakeOver reports a match a take over of run closes: one that waits on the run's
// interruption, or one the user closed by stopping the run.
func waitsOnTakeOver(m *match, run queries.AutopilotRun) bool {
	if m.Step != run.Step {
		return false
	}

	if m.state() == StateWaiting && Blocker(m.BlockedBy) == BlockInterrupted {
		return true
	}

	return m.state() == StateDismissed && Reason(m.Reason) == ReasonYouStopped
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
		end.Outcome != RunNotSent && end.Outcome != RunOutOfUsage
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
	case RunOutOfUsage:
		// No try: its step starts again on the next profile its chain may use.
		sr.Status, sr.RunID, sr.Started, sr.Ended, sr.Note = stepPending, 0, nil, nil, outOfUsageWords(end)

		m.unstarted()
		m.set(StateQueued, "", sr.Note)
	case RunFailed, RunNotSent:
		sr.Status, sr.Outcome, sr.Note = stepFailed, string(catalog.OutcomeNotPushed), textutil.Sentence(end.Error)
		m.Step = index + 1

		m.set(StateQueued, "", "")
	case RunDone:
		ranTo(sr, end)

		m.Step = index + 1

		m.set(StateQueued, "", "")
		e.verifyNext(m, now)
	}

	// Whether it pushed is an agent's outcome alone: a script's says nothing a later step
	// reads.
	if sr.Step.Agent == nil {
		sr.Outcome = ""
	}
}

// ranTo records on sr that its run ended done as end says: its outcome is whether it
// pushed, and a read-only agent's is its review's verdict, its review kept for the Post
// the agent's review after it.
func ranTo(sr *stepRun, end RunEnd) {
	sr.Status, sr.Outcome, sr.Note = stepDone, string(catalog.OutcomeNotPushed), end.Summary
	if end.Commit != "" {
		sr.Outcome = string(catalog.OutcomePushed)
	}

	if sr.Step.Agent != nil && sr.Step.Agent.ReadOnly() {
		sr.Review, sr.Outcome = end.Review, string(reviewOutcome(end.Review))
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
