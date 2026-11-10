package scriptrun

import (
	"context"

	"github.com/mrz1836/nilbox/internal/clonelock"
)

// reconcile settles every run recorded as going that the engine doesn't follow: the runs
// the app left behind when it last quit or crashed. It also removes the files of runs the
// history no longer holds, and applies the retention.
//
// A run in the app whose process group is alive, led by the process that started when
// the run's did, is orphaned: followed again, under a placeholder lock on its clone, until
// the group is gone. Any other is interrupted. A run in the terminal whose exit file is
// there ended as it says; one whose shell still runs, or that is still young enough to
// record its shell, is followed again; any other is interrupted. A run whose kind has a
// Verifier is read and verified as it ends, as a run the engine started is.
//
// Every run is held under its placeholder lock and registered first. Only then are the
// ends of those that had ended worked out: at once for a run with nothing to verify, and
// as tracked work for one with a Finish, so that a release asking origin where its tag is
// holds up neither the others nor the app's quitting.
func (e *Engine) reconcile(ctx context.Context) {
	if e.isClosed() {
		return
	}

	recs, err := e.leftBehind(ctx)
	if err != nil {
		e.logger.WarnContext(ctx, "couldn't read the runs left from before the app started", "error", err)

		return
	}

	for _, s := range e.resumeAll(ctx, recs) {
		if s.l.finish == nil {
			e.end(ctx, s.l, s.end)
		} else {
			e.settle(context.WithoutCancel(ctx), s)
		}
	}

	e.prune(ctx)

	if len(recs) > 0 {
		e.notify()
	}
}

// resumeAll holds and registers every run in recs, following again those still going, and
// returns those that had ended, for reconcile to settle.
func (e *Engine) resumeAll(ctx context.Context, recs []Record) []settled {
	var ended []settled

	for _, rec := range recs {
		var s *settled

		if rec.RunsIn == RunsInTerminal {
			s = e.resumeTerminal(ctx, rec)
		} else {
			s = e.resumeApp(ctx, rec)
		}

		if s != nil {
			ended = append(ended, *s)
		}
	}

	return ended
}

// settled is a run from before the app restarted that had ended by then, and how.
type settled struct {
	l   *live
	end Ending
}

// endedRun registers a run from before the app restarted that had ended by then, while
// its end is worked out, and returns it with how it ended; nil once the engine has
// stopped, when its record stays as it is for the next start.
func (e *Engine) endedRun(l *live, end Ending) *settled {
	l.settling = true

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		l.holds.free()
		l.markDone()

		return nil
	}

	e.live[l.id] = l

	return &settled{l: l, end: end}
}

// settle works a registered run's end out as tracked work, which StopAll waits for once it
// has given up on the run. Once the engine has stopped, it lets the run go instead, its
// record left as going for the next start.
func (e *Engine) settle(ctx context.Context, s settled) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		if e.live[s.l.id] == s.l {
			delete(e.live, s.l.id)
		}

		s.l.holds.free()
		s.l.markDone()

		return
	}

	e.wg.Go(func() { e.end(ctx, s.l, s.end) })
}

// leftBehind returns the runs recorded as going that the engine doesn't follow, and
// meanwhile removes the files of runs the history no longer holds. It holds regMu, so no
// run is recorded or registered meanwhile.
func (e *Engine) leftBehind(ctx context.Context) ([]Record, error) {
	e.regMu.Lock()
	defer e.regMu.Unlock()

	recs, err := e.store.live(ctx)
	if err != nil {
		return nil, err
	}

	e.sweep(ctx)

	e.mu.Lock()
	defer e.mu.Unlock()

	out := make([]Record, 0, len(recs))

	for _, rec := range recs {
		if e.live[rec.ID] == nil {
			out = append(out, rec)
		}
	}

	return out, nil
}

// resumeApp holds a run in the app from before the app started, and follows it again
// when it still runs; one that had ended comes back, registered, for reconcile to settle.
func (e *Engine) resumeApp(ctx context.Context, rec Record) *settled {
	l := e.liveOf(rec)
	l.holds.lock = e.placeholder(ctx, rec)

	if !e.groupLive(l) {
		return e.endedRun(l, Ending{Interrupted: appClosed})
	}

	if err := e.store.setStatus(ctx, rec.ID, StatusOrphaned, orphanRunning); err != nil {
		e.logger.WarnContext(ctx, "couldn't record a run as orphaned", "run", rec.ID, "error", err)
	}

	e.follow(context.WithoutCancel(ctx), l, e.followOrphan)

	return nil
}

// resumeTerminal holds a run in the terminal from before the app started, and follows it
// again when its shell still runs, or may yet record itself; one that had ended comes
// back, registered, for reconcile to settle.
func (e *Engine) resumeTerminal(ctx context.Context, rec Record) *settled {
	l := e.liveOf(rec)
	l.holds.lock = e.placeholder(ctx, rec)

	if end, ok := e.terminalExit(l); ok {
		return e.endedRun(l, end)
	}

	if l.pid == 0 {
		e.notePID(ctx, l) // its script may have recorded its shell after the app quit
	}

	wait := e.pidWait - e.clock.Now().Sub(rec.StartedAt)

	running := l.pid != 0 && e.processAlive(l.pid, l.procStart)
	if !running && (l.pid != 0 || wait <= 0) {
		return e.endedRun(l, Ending{Interrupted: terminalClosed})
	}

	e.follow(context.WithoutCancel(ctx), l, func(ctx context.Context, l *live) {
		e.followTerminal(ctx, l, wait)
	})

	return nil
}

// liveOf returns the state of a run from before the app started. Its Spec's output reader
// and Finish were the app's before it restarted: its kind's Verifier, when it has one,
// gives them again, and the reader is fed the run's log from its start.
func (e *Engine) liveOf(rec Record) *live {
	l := &live{
		id: rec.ID, kind: rec.Kind, runsIn: rec.RunsIn, tool: rec.Result.Tool, dir: rec.Dir,
		repo: e.where(rec.Repo, rec.Dir), title: rec.Title, intent: rec.Result.Intent, pid: rec.PID, pgid: rec.PGID,
		procStart: rec.ProcStartedAt, started: rec.StartedAt, done: make(chan struct{}), abandon: make(chan struct{}),
	}

	if v := e.verifier(rec.Kind); v != nil {
		got := v.Verify(rec)
		l.output, l.finish = got.Output, got.Finish
	}

	return l
}

// placeholder takes a run's clone under a placeholder lock, a run from before the app
// restarted, and returns its release, or nil when another job holds the clone already.
func (e *Engine) placeholder(ctx context.Context, rec Record) func() {
	release, held, ok := e.locks.TryLock(rec.Dir, clonelock.Holder{
		Kind: clonelock.KindOrphan, Repo: e.where(rec.Repo, rec.Dir), Title: rec.Title, RunID: rec.ID,
	})
	if !ok {
		e.logger.InfoContext(ctx, "a run from before the app started found its clone busy", "run", rec.ID,
			"holder", string(held.Kind))

		return nil
	}

	return release
}

// sweep removes the files of the runs the history no longer holds, as after the database
// was set aside. The caller holds regMu, so no run starts meanwhile.
func (e *Engine) sweep(ctx context.Context) {
	ids, err := e.store.ids(ctx)
	if err != nil {
		e.logger.WarnContext(ctx, "couldn't read the runs to tidy their logs", "error", err)

		return
	}

	keep := make(map[int64]bool, len(ids))
	for _, id := range ids {
		keep[id] = true
	}

	stale, err := e.files.orphans(keep)
	if err != nil {
		e.logger.WarnContext(ctx, "couldn't tidy the runs' logs", "error", err)

		return
	}

	for _, id := range stale {
		if err = e.files.clear(id); err != nil {
			e.logger.WarnContext(ctx, "couldn't remove a forgotten run's log", "run", id, "error", err)
		}
	}
}
