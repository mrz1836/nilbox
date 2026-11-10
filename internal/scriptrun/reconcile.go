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
// record its shell, is followed again; any other is interrupted.
func (e *Engine) reconcile(ctx context.Context) {
	if e.isClosed() {
		return
	}

	recs, err := e.leftBehind(ctx)
	if err != nil {
		e.logger.WarnContext(ctx, "couldn't read the runs left from before the app started", "error", err)

		return
	}

	for _, rec := range recs {
		if rec.RunsIn == RunsInTerminal {
			e.resumeTerminal(ctx, rec)
		} else {
			e.resumeApp(ctx, rec)
		}
	}

	e.prune(ctx)

	if len(recs) > 0 {
		e.notify()
	}
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

// resumeApp settles a run in the app from before the app started.
func (e *Engine) resumeApp(ctx context.Context, rec Record) {
	l := e.liveOf(rec)

	if !e.groupLive(l) {
		e.end(ctx, l, Ending{Interrupted: appClosed})

		return
	}

	if err := e.store.setStatus(ctx, rec.ID, StatusOrphaned, orphanRunning); err != nil {
		e.logger.WarnContext(ctx, "couldn't record a run as orphaned", "run", rec.ID, "error", err)
	}

	l.holds.lock = e.placeholder(ctx, rec)
	e.follow(context.WithoutCancel(ctx), l, e.followOrphan)
}

// resumeTerminal settles a run in the terminal from before the app started.
func (e *Engine) resumeTerminal(ctx context.Context, rec Record) {
	l := e.liveOf(rec)

	if end, ok := e.terminalExit(l); ok {
		e.end(ctx, l, end)

		return
	}

	if l.pid == 0 {
		e.notePID(ctx, l) // its script may have recorded its shell after the app quit
	}

	wait := e.pidWait - e.clock.Now().Sub(rec.StartedAt)

	running := l.pid != 0 && e.processAlive(l.pid, l.procStart)
	if !running && (l.pid != 0 || wait <= 0) {
		e.end(ctx, l, Ending{Interrupted: terminalClosed})

		return
	}

	l.holds.lock = e.placeholder(ctx, rec)
	e.follow(context.WithoutCancel(ctx), l, func(ctx context.Context, l *live) {
		e.followTerminal(ctx, l, wait)
	})
}

// liveOf returns the state of a run from before the app started. It has no output reader
// and no Finish: those were the app's before it restarted.
func (e *Engine) liveOf(rec Record) *live {
	return &live{
		id: rec.ID, kind: rec.Kind, runsIn: rec.RunsIn, tool: rec.Result.Tool, dir: rec.Dir,
		repo: e.where(rec.Repo, rec.Dir), title: rec.Title, intent: rec.Result.Intent, pid: rec.PID, pgid: rec.PGID,
		procStart: rec.ProcStartedAt, started: rec.StartedAt, done: make(chan struct{}), abandon: make(chan struct{}),
	}
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
