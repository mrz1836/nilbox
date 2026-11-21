package autopilot

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/store"
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

		if ended(sr) || p.skipStep(m, sr) || p.noReview(ctx, m, sr) {
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

	for _, b := range slices.Backward(before) {
		if b.Step.Kind() == from {
			return b.Outcome == string(want)
		}
	}

	return false
}

// runLocal runs a local step on a worker: held back only by Pause Autopilot and the
// routine's active hours. A notice waits for the cycle's end, which tells them all at once
// (tellNotices), so a burst is gathered.
func (p *pass) runLocal(ctx context.Context, r *routineRun, m *match, sr *stepRun, info catalog.Step) {
	if v := p.held(r, m); !v.through() {
		m.wait(v.block, v.words)

		return
	}

	job := p.job(r, m, sr)
	job.Local = p.local[store.Deref(m.ItemID)]
	sr.Status, sr.Started = stepRunning, new(p.now)

	m.wait(BlockStarting, "")

	if info.Dispatcher == catalog.DispatchNotices {
		// A routine catching up makes the cycle's notices a burst; it is noted now, as the
		// cycle may end the catch-up before the notices go.
		job.Burst = p.catchingUp(r)
		p.e.inflight[m.ID] = true
		p.notices = append(p.notices, job)

		return
	}

	p.e.dispatch(ctx, m.ID, 0, info.Dispatcher, job)
}

// tellNotices hands the cycle's notices to the notices dispatcher, each on a worker, as a
// burst when there are more than one, or when any comes from a routine catching up: those
// are gathered, so more than three at once show as one banner. A scheduled routine's
// Notify me steps that tell once with the list are one digest per routine (digests).
func (p *pass) tellNotices(ctx context.Context) {
	jobs, digests := p.digests(p.notices)
	burst := len(jobs)+len(digests) > 1 || slices.ContainsFunc(jobs, func(j Job) bool { return j.Burst })

	for _, job := range jobs {
		job.Burst = burst
		p.e.dispatch(ctx, job.MatchID, 0, catalog.DispatchNotices, job)
	}

	for _, dj := range digests {
		dj.job.Burst = burst
		p.e.dispatchDigest(ctx, dj)
	}

	p.notices = nil
}

// verify follows the checks on the commit the agent pushed: the match verifies, and the
// engine judges it from this cycle on (verifyMatch), as a run that pushed does once it
// ends.
func (p *pass) verify(ctx context.Context, r *routineRun, m *match, sr *stepRun) {
	m.set(StateVerifying, "", "The checks on its commit haven't finished.")

	sr.Status, sr.Started = stepRunning, new(p.now)

	if p.verifyMatch(ctx, r, m) {
		p.advance(ctx, r, m)
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

	if p.backOff(m) {
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

// readFresh asks, on a worker, what holds the start back on this Mac (the step's prompt,
// or a GitHub step's replies, then the app's other parts, Holds), and only when nothing
// does reads m's item again from GitHub, so a start held back here spends nothing; the
// next cycle evaluates it on what the read stored.
func (p *pass) readFresh(ctx context.Context, r *routineRun, m *match, sr *stepRun) {
	job := p.job(r, m, sr)
	sp := r.spec

	m.wait(BlockStarting, "Reading it again from GitHub before it starts.")

	p.e.inflight[m.ID] = true
	p.cycle.Reads++

	p.e.goWork(ctx, func(ctx context.Context) {
		got, err := p.e.freshRead(ctx, sp, job)

		p.e.mail.post(func(ctx context.Context) {
			delete(p.e.inflight, m.ID)

			if errors.Is(err, ErrOff) {
				p.e.requeue(ctx, m.ID, "", "")

				return
			}

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

// freshRead is readFresh's work, on the worker: the step's prompt, then Holds, and only
// when neither holds the start back, the item's read. Turned off since the cycle,
// Autopilot asks GitHub nothing: it returns ErrOff, and the match stays queued, as at
// dispatch.
func (e *Engine) freshRead(ctx context.Context, sp spec.Spec, job Job) (fresh, error) {
	got := fresh{at: e.clock.Now()}

	var err error

	got.hold, got.detail = e.promptHold(ctx, sp, job.Step)
	if got.hold == "" && e.holds != nil {
		got.hold, got.detail, err = e.holds.Hold(ctx, job)
	}

	switch {
	case err != nil || got.hold != "":
	case !e.Enabled():
		err = ErrOff
	default:
		err = e.readItem(ctx, job)
	}

	return got, err
}

// readItem reads job's item right before its step starts: through the step's own part
// when it reads the item (itemReader: a GitHub step's sheet, whose read rewrites the
// inbox's card too, so an action costs one point, not two), else through the inbox's
// single-item read.
func (e *Engine) readItem(ctx context.Context, job Job) error {
	if info, ok := catalog.StepByKind(job.Step.Kind()); ok {
		if reader, reads := e.dispatchers[info.Dispatcher].(itemReader); reads {
			return reader.ReadItem(ctx, job)
		}
	}

	return e.items.ReadItem(ctx, job.Item.ID)
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
