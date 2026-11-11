package scriptrun

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/execpath"
)

// The summaries of runs whose end the app couldn't see.
const (
	appClosed      = brand.Name + " closed while this was running."
	terminalClosed = "The terminal closed while this was running."
	terminalIdle   = "The terminal didn't start the run."
	orphanEnded    = "It ended after " + brand.Name + " restarted, so its exit status is unknown."
	orphanRunning  = "Still running from before " + brand.Name + " restarted."
	exitUnreadable = "Its exit status couldn't be read."
)

// live is a run the engine follows: one it started, or one from before the app restarted.
type live struct {
	id       int64
	kind     Kind
	runsIn   RunsIn
	tool     string
	dir      string
	repo     string
	title    string
	timeout  time.Duration
	output   io.Writer
	finish   func(ctx context.Context, end Ending, out Outcome) Outcome
	recorded func(ctx context.Context, id int64, out Outcome)
	released func(id int64)
	intent   any
	holds    holds

	// progress is its kind's Spec.Progress, or nil, and shown the progress last seen after
	// a read of its log, which only its follower touches.
	progress func() Progress
	shown    Progress

	// proc is the program of a run this engine started in the app, or nil. pid, pgid,
	// and procStart are its process, group, and start time, or a terminal run's shell's
	// once its script records it, or what a run from before the app restarted recorded.
	proc      *execpath.Proc
	pid       int
	pgid      int
	procStart time.Time

	// started is when the run started: a terminal run's shell starts after it.
	started time.Time

	// settling marks a run from before the app restarted that had ended by then: it is
	// held and registered only while its outcome is worked out. Nothing of it runs, so it
	// can't be stopped, holds no tool, and doesn't keep the app from quitting.
	settling bool

	// ended marks a run whose end is recorded. The engine holds it, and its clone, a moment
	// longer, until it lets go of it, but nothing of it goes any more: it can't be stopped,
	// shows no progress, uses no tool, and doesn't keep the app from quitting. The engine's
	// mu guards it.
	ended bool

	// stopped, timedOut, and quit say what ended it, when the engine did.
	stopped  atomic.Bool
	timedOut atomic.Bool
	quit     atomic.Bool

	// done is closed once its end is recorded, or the engine let it go; abandon is
	// closed when the engine gives up following it, as the app quits.
	done        chan struct{}
	abandon     chan struct{}
	doneOnce    sync.Once
	abandonOnce sync.Once
	stopOnce    sync.Once

	// reader, buf, and offset are where its follower has read its log to.
	reader *os.File
	buf    []byte
	offset int64
}

// progressNow returns how far the run has got, as its kind says, or no progress for a kind
// that says nothing.
func (l *live) progressNow() Progress {
	if l.progress == nil {
		return Progress{}
	}

	return l.progress()
}

// markDone closes done, once.
func (l *live) markDone() {
	l.doneOnce.Do(func() { close(l.done) })
}

// finishOutcome runs the run's Finish with a context that also ends once the engine gives
// up on the run as the app quits, so a slow Finish, one that checks GitHub for instance,
// can't hold up shutdown.
func (l *live) finishOutcome(ctx context.Context, end Ending, out Outcome) Outcome {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		select {
		case <-l.abandon:
			cancel()
		case <-ctx.Done():
		}
	}()

	return l.finish(ctx, end, out)
}

// giveUp closes abandon, once.
func (l *live) giveUp() {
	l.abandonOnce.Do(func() { close(l.abandon) })
}

// closeReader closes the log the follower read.
func (l *live) closeReader() {
	if l.reader != nil {
		_ = l.reader.Close()
		l.reader = nil
	}
}

// followApp follows a run the engine started in the app until its program exits, tailing
// its log and stopping it at its time limit.
func (e *Engine) followApp(ctx context.Context, l *live) {
	ticker := time.NewTicker(e.tailEvery)
	defer ticker.Stop()

	var limit <-chan time.Time

	if l.timeout > 0 {
		timer := time.NewTimer(l.timeout)
		defer timer.Stop()

		limit = timer.C
	}

	for {
		select {
		case <-l.proc.Done():
			e.end(ctx, l, e.appEnding(l))

			return
		case <-limit:
			limit = nil

			l.timedOut.Store(true)
			l.proc.Stop(e.ladder)
		case <-l.abandon:
			e.drop(l)

			return
		case <-ticker.C:
			e.tail(l)
		}
	}
}

// appEnding returns how a run's program ended.
func (e *Engine) appEnding(l *live) Ending {
	code, _ := l.proc.Wait() // the code says it all; the error only wraps it

	end := Ending{Stopped: l.stopped.Load(), TimedOut: l.timedOut.Load(), Quit: l.quit.Load()}
	if code >= 0 {
		end.ExitCode = &code
	}

	return end
}

// followTerminal follows a run in the terminal until its exit file appears or its shell
// is gone, tailing its log. A run whose script hasn't recorded its shell within pidWait
// never started. As the app quits, it lets the run go, still recorded as running.
func (e *Engine) followTerminal(ctx context.Context, l *live, pidWait time.Duration) {
	ticker := time.NewTicker(e.tailEvery)
	defer ticker.Stop()

	timer := time.NewTimer(max(pidWait, 0))
	defer timer.Stop()

	noPID := timer.C
	if l.pid != 0 {
		noPID = nil
	}

	for {
		select {
		case <-e.closing:
			e.drop(l)

			return
		case <-noPID:
			noPID = nil

			if e.checkTerminal(ctx, l, true) {
				return
			}
		case <-ticker.C:
			if e.checkTerminal(ctx, l, false) {
				return
			}
		}
	}
}

// checkTerminal tails a run in the terminal and ends it once its exit file is there, its
// shell is gone, or, when late, it never recorded its shell. It reports whether the run
// ended.
func (e *Engine) checkTerminal(ctx context.Context, l *live, late bool) bool {
	e.tail(l)

	if end, ok := e.terminalExit(l); ok {
		e.end(ctx, l, end)

		return true
	}

	if l.pid == 0 {
		e.notePID(ctx, l)
	}

	switch {
	case l.pid == 0 && late:
		e.end(ctx, l, Ending{Interrupted: terminalIdle})

		return true
	case l.pid != 0 && !e.processAlive(l.pid, l.procStart):
		end, ok := e.terminalExit(l) // written just as the shell exited
		if !ok {
			end = Ending{Interrupted: terminalClosed}
		}

		e.end(ctx, l, end)

		return true
	default:
		return false
	}
}

// terminalExit reads a run's exit file, and reports whether it was there.
func (e *Engine) terminalExit(l *live) (Ending, bool) {
	code, known, ok := e.files.readExit(l.id)

	switch {
	case !ok:
		return Ending{}, false
	case !known:
		return Ending{Interrupted: exitUnreadable}, true
	default:
		return Ending{ExitCode: &code}, true
	}
}

// notePID reads the PID a run's script recorded for its shell, if it has, and records it
// with the shell's start time.
func (e *Engine) notePID(ctx context.Context, l *live) {
	pid := e.files.readPID(l.id)
	if pid == 0 {
		return
	}

	start := e.startedAt(pid)
	if !e.shellStart(l.started, start) {
		return // the PID is another process's now: the shell came and went unseen
	}

	l.pid, l.procStart = pid, start

	if err := e.store.setProcess(ctx, l.id, pid, 0, l.procStart); err != nil {
		e.logger.WarnContext(ctx, "couldn't record a run's process", "run", l.id, "error", err)
	}
}

// shellStart reports whether a process that started at start can be the shell of a
// terminal run that started at started: one the terminal began after the run did, within
// the time the run waits for it. A process the PID file names that started at another
// time took over the PID of a shell that ended unseen, while the app was away.
func (e *Engine) shellStart(started, start time.Time) bool {
	if start.IsZero() || started.IsZero() {
		return !start.IsZero()
	}

	return !start.Before(started.Add(-startSlack)) && !start.After(started.Add(e.pidWait+startSlack))
}

// followOrphan follows a run in the app from before the app restarted until its process
// group is gone, tailing its log into the reader its kind's Verifier gave it, if any. Its
// exit status can't be known.
func (e *Engine) followOrphan(ctx context.Context, l *live) {
	ticker := time.NewTicker(e.tailEvery)
	defer ticker.Stop()

	for {
		select {
		case <-l.abandon:
			e.drop(l)

			return
		case <-ticker.C:
			e.tail(l)

			if !e.groupLive(l) {
				e.end(ctx, l, Ending{Stopped: l.stopped.Load(), Quit: l.quit.Load(), Interrupted: orphanEnded})

				return
			}
		}
	}
}

// tail feeds the run's output reader what its log holds past what it has read, and then
// notes whether its progress changed.
func (e *Engine) tail(l *live) {
	if l.output == nil || !e.openLog(l) {
		return
	}

	if l.feed() {
		e.noteProgress(l)
	}
}

// openLog opens the run's log for its follower to read, once, and reports whether it is
// open: a log not written yet isn't.
func (e *Engine) openLog(l *live) bool {
	if l.reader != nil {
		return true
	}

	f, err := os.Open(e.files.log(l.id))
	if err != nil {
		return false
	}

	l.reader, l.buf = f, make([]byte, readBlock)

	return true
}

// feed feeds the run's output reader what its open log holds past what it has read, and
// reports whether there was anything.
func (l *live) feed() bool {
	fed := false

	for {
		n, err := l.reader.ReadAt(l.buf, l.offset)
		if n > 0 {
			_, _ = l.output.Write(l.buf[:n])
			l.offset += int64(n)
			fed = true
		}

		if err != nil || n < len(l.buf) {
			return fed
		}
	}
}

// end records how a run ended: it feeds the reader the rest of the log, works out the
// outcome, cuts a long log down, and records the run as ended.
func (e *Engine) end(ctx context.Context, l *live, end Ending) {
	e.tail(l)
	l.closeReader()

	out := defaultOutcome(end, l.timeout)
	if l.finish != nil {
		got := l.finishOutcome(ctx, end, out)
		if got.Unsettled {
			e.leave(ctx, l)

			return
		}

		out = checkedOutcome(got, out)
	}

	size, err := e.compaction.compact(e.files.log(l.id))
	if err != nil {
		e.logger.WarnContext(ctx, "couldn't cut a long run log down", "run", l.id, "error", err)

		size = e.files.size(l.id)
	}

	e.record(ctx, l, end.ExitCode, out, size)
}

// failStart records a run that couldn't start as failed, saying why.
func (e *Engine) failStart(ctx context.Context, l *live, summary string) {
	e.record(ctx, l, nil, Outcome{Status: StatusFailed, Summary: summary}, e.files.size(l.id))
}

// record records a run's end and marks the run ended once it is recorded, tells its kind's
// Recorded, lets go of it and then tells its Released, logs it, applies the retention, and
// tells Changed. A run whose end couldn't be recorded isn't marked: its record still says
// it goes, and the next start settles it.
func (e *Engine) record(ctx context.Context, l *live, exitCode *int, out Outcome, size int64) {
	unrecognized := out.Unrecognized
	if len(unrecognized) > MaxUnrecognized {
		unrecognized = unrecognized[:MaxUnrecognized]
	}

	result := withLists(Result{
		Tool: l.tool, Intent: l.intent, Warnings: out.Warnings, Unrecognized: unrecognized,
		UnrecognizedCount: max(out.UnrecognizedCount, len(out.Unrecognized)), Outcome: out.Detail,
	})

	err := e.store.finish(ctx, finished{
		id: l.id, status: out.Status, exitCode: exitCode, summary: out.Summary, result: result, logBytes: size,
		at: e.clock.Now(),
	})
	if err == nil {
		e.markEnded(l)
	}

	switch {
	case err != nil:
		e.logger.WarnContext(ctx, "couldn't record how a run ended", "run", l.id, "error", err)
	case l.recorded != nil:
		l.recorded(ctx, l.id, out)
	}

	e.release(l)

	if l.released != nil {
		l.released(l.id)
	}

	e.logEnd(ctx, l, exitCode, out.Status, result)
	e.prune(ctx)
	e.notify()
}

// markEnded marks a run whose end is recorded as ended.
func (e *Engine) markEnded(l *live) {
	e.mu.Lock()
	defer e.mu.Unlock()

	l.ended = true
}

// leave lets go of a run whose Finish couldn't settle it before its context ended, as the
// app quit: its record stays as going, and the next start settles it again.
func (e *Engine) leave(ctx context.Context, l *live) {
	e.logger.InfoContext(ctx, "a run's end is left for the next start to settle", "run", l.id, "kind", string(l.kind))
	e.drop(l)
}

// drop lets go of a run without recording its end, which stays for the next start.
func (e *Engine) drop(l *live) {
	l.closeReader()
	e.release(l)
}

// release frees a run's clone and its slot, unregisters it, and marks it done, in that
// order: a run no longer live holds nothing.
func (e *Engine) release(l *live) {
	l.holds.free()

	e.mu.Lock()
	if e.live[l.id] == l {
		delete(e.live, l.id)
	}
	e.mu.Unlock()

	l.markDone()
}

// logEnd writes the app log's one line about a run that ended: never its output, nor
// anything of its environment. A run that didn't succeed, or whose program exited
// non-zero, is a warning.
func (e *Engine) logEnd(ctx context.Context, l *live, exitCode *int, status Status, result Result) {
	level, exit := slog.LevelInfo, "unknown"

	if exitCode != nil {
		exit = strconv.Itoa(*exitCode)
	}

	if (status != StatusSucceeded && status != StatusStopped) || (exitCode != nil && *exitCode != 0) {
		level = slog.LevelWarn
	}

	e.logger.Log(ctx, level, "a run ended", "run", l.id, "kind", string(l.kind), "repo", l.repo,
		"in", string(l.runsIn), "status", string(status), "exit", exit, "warnings", len(result.Warnings),
		"unrecognized", result.UnrecognizedCount)
}

// defaultOutcome works out a run's outcome from how it ended: the app quitting, the user
// stopping it, its time limit, an end the app couldn't see, or its exit status.
func defaultOutcome(end Ending, timeout time.Duration) Outcome {
	switch {
	case end.Quit:
		return Outcome{Status: StatusStopped, Summary: "Stopped as " + brand.Name + " quit."}
	case end.Stopped:
		return Outcome{Status: StatusStopped, Summary: "Stopped."}
	case end.TimedOut:
		return Outcome{Status: StatusFailed, Summary: "It ran past its limit of " + durationText(timeout) + ", so it was stopped."}
	case end.Interrupted != "":
		return Outcome{Status: StatusInterrupted, Summary: end.Interrupted}
	case end.ExitCode == nil:
		return Outcome{Status: StatusInterrupted, Summary: "Its exit status is unknown."}
	case *end.ExitCode == 0:
		return Outcome{Status: StatusSucceeded, Summary: "Finished."}
	default:
		return Outcome{Status: StatusFailed, Summary: fmt.Sprintf("It exited with status %d.", *end.ExitCode)}
	}
}

// checkedOutcome returns out, with fallback's status where out's isn't one a run ends
// with, and fallback's summary where out has none.
func checkedOutcome(out, fallback Outcome) Outcome {
	if !out.Status.Valid() || out.Status.Live() {
		out.Status = fallback.Status
	}

	if out.Summary == "" {
		out.Summary = fallback.Summary
	}

	return out
}

// durationText writes a time limit the way a sentence says it: "5 minutes", "1 minute".
func durationText(d time.Duration) string {
	switch {
	case d == time.Minute:
		return "1 minute"
	case d > 0 && d%time.Minute == 0:
		return strconv.FormatInt(int64(d/time.Minute), 10) + " minutes"
	default:
		return d.String()
	}
}
