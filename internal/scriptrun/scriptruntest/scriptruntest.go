// Package scriptruntest is the runner the suites of scriptrun's kinds share (hooks,
// scripts, and release): a real scriptrun Engine and its Service over a fresh database,
// the app's next start over the same history, a fake terminal app that runs each run
// meant for the user's terminal under /bin/sh (Terminal), and waits for a run's end that
// look again only when the engine says the runs changed, never on a timer (AwaitShell
// alone polls, for a shell the engine notes without telling).
//
// It is test support only, built on internal/testutil: only _test.go files may import
// it, which a repoguard test enforces, so none of it reaches the app.
package scriptruntest

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// Wait bounds how long a test waits for a run to end, or for what it awaits.
const Wait = 30 * time.Second

// Harness is the runner as the app builds it, over a fresh database, a log folder and a
// set of clone locks of its own, and a home: the Engine the service under test starts
// its runs with, and the Service that reads them back as the window does. The engine
// stops every run when the test ends.
type Harness struct {
	// DB holds script_runs, LogDir the runs' logs, and Locks the clone locks, which the
	// service under test shares with the runner. The next start (Next) runs over the same
	// three.
	DB     *sql.DB
	LogDir string
	Locks  *clonelock.Set

	// Engine starts the runs, and Runs reads them back.
	Engine *scriptrun.Engine
	Runs   *scriptrun.Service

	t        testing.TB
	home     string
	logger   *slog.Logger
	terminal scriptrun.TerminalOpener
	changes  *changes
	wait     time.Duration
}

// Option sets up a Harness as New builds it.
type Option func(*Harness)

// WithTerminal has the harness's engines, the next start's included, open the runs in
// the terminal in term, such as a Terminal. Without it, a run in the terminal fails to
// start.
func WithTerminal(term scriptrun.TerminalOpener) Option {
	return func(h *Harness) { h.terminal = term }
}

// New returns a Harness whose runs' folders show under home with ~, and whose engine
// logs to logger (nil discards).
func New(t testing.TB, home string, logger *slog.Logger, opts ...Option) *Harness {
	t.Helper()

	h := &Harness{
		DB: testutil.DB(t), LogDir: filepath.Join(t.TempDir(), "script-runs"), Locks: clonelock.New(),
		t: t, home: home, logger: logger, changes: newChanges(), wait: Wait,
	}

	for _, opt := range opts {
		opt(h)
	}

	h.Engine = h.engine(nil)
	t.Cleanup(h.Engine.StopAll)

	h.Runs = scriptrun.NewService(h.Engine)

	return h
}

// Ended waits for the run id to end and let go of its clone, and returns it as the window
// reads it. It looks again each time the engine says the runs changed, which it does once
// a run's end is recorded and the run has let go of its clone, and fails the test when
// the run hasn't ended within Wait, saying where the run stands and why the wait most
// likely ran out.
func (h *Harness) Ended(id int64) scriptrun.Run {
	h.t.Helper()

	return h.ended(h.Runs, id)
}

// Await waits until cond holds, and fails the test with msgAndArgs, and why the wait most
// likely ran out, when it hasn't within Wait. It looks again each time the engine says
// the runs changed, so cond must hold once that is told: a run started, its end recorded
// or refused, its clone let go of, or what its kind's Released did, all of which come
// before the engine tells it.
func (h *Harness) Await(cond func() bool, msgAndArgs ...any) {
	h.t.Helper()

	h.await(cond, func() string { return "the condition never held" }, msgAndArgs...)
}

// shellPoll is how often AwaitShell looks again.
const shellPoll = 20 * time.Millisecond

// AwaitShell waits until the engine has seen the shell the terminal runs the run id in:
// its PID recorded, so the shell's end is read as the run's, with its exit status or
// without one. A test that ends such a shell, as a user closes the terminal, waits for
// this first, since a shell that comes and goes unseen reads as a terminal that never ran
// the command, once the engine's wait for it is up. The engine tells no change as it notes
// a shell (the window shows no PID, so telling it would only list the runs again), so this
// wait alone looks again on a timer, every shellPoll, up to Wait, reading the run's
// record as scriptrun's own tests do.
func (h *Harness) AwaitShell(id int64) {
	h.t.Helper()

	require.EventuallyWithT(h.t, func(c *assert.CollectT) {
		var pid int

		err := h.DB.QueryRowContext(h.t.Context(), `SELECT pid FROM script_runs WHERE id = ?`, id).Scan(&pid)
		require.NoError(c, err, "read run %d's record", id)
		assert.Positive(c, pid, "the engine has noted run %d's shell", id)
	}, h.wait, shellPoll)
}

// Next returns the runner as the app's next start builds it, over the same history, logs,
// locks, and home, with verifiers registered for their kinds. It isn't running yet.
func (h *Harness) Next(verifiers map[scriptrun.Kind]scriptrun.Verifier) *scriptrun.Engine {
	return h.engine(verifiers)
}

// Restart runs the next start's runner (Next) until the run id is settled and has let go
// of its clone, stops it, and returns the run as that runner reads it.
func (h *Harness) Restart(id int64, verifiers map[scriptrun.Kind]scriptrun.Verifier) scriptrun.Run {
	h.t.Helper()

	next := h.Next(verifiers)

	ctx, cancel := context.WithCancel(h.t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		next.Run(ctx)
	}()

	// Stopped however the wait ends, a failure included, so the runner never outlives it.
	defer func() {
		cancel()
		<-done
	}()

	return h.ended(scriptrun.NewService(next), id)
}

// engine builds an Engine over the harness's history, logs, locks, and home that tells
// the harness when the runs change, with verifiers registered.
func (h *Harness) engine(verifiers map[scriptrun.Kind]scriptrun.Verifier) *scriptrun.Engine {
	e := scriptrun.NewEngine(scriptrun.Config{
		DB: h.DB, Locks: h.Locks, LogDir: h.LogDir, Home: h.home, Logger: h.logger, Changed: h.changes.tell,
		Terminal: h.terminal,
	})

	for kind, v := range verifiers {
		e.RegisterVerifier(kind, v)
	}

	return e
}

// ended waits for the run id, as runs reads it, to end and let go of its clone.
func (h *Harness) ended(runs *scriptrun.Service, id int64) scriptrun.Run {
	h.t.Helper()

	var (
		run   scriptrun.Run
		state = "it was never read"
	)

	h.await(func() bool {
		got, err := runs.Run(h.t.Context(), id)

		switch {
		case err != nil:
			state = "it can't be read: " + err.Error()

			return false
		case got.Status.Live():
			state = "its record says it is " + string(got.Status)

			return false
		}

		run = got

		dir, err := paths.ExpandHome(got.Dir, h.home)
		if err != nil {
			state = "its folder can't be told: " + err.Error()

			return false
		}

		if holder, held := h.Locks.Holder(dir); held {
			state = fmt.Sprintf("it is recorded as %s, but a %s run still holds %s", got.Status, holder.Kind, got.Dir)

			return false
		}

		return true
	}, func() string { return fmt.Sprintf("run %d never ended: %s", id, state) })

	return run
}

// await waits until cond holds, looking again each time the runs change, and fails the
// test once the harness's wait is up with what failure says, why the wait most likely ran
// out (stalled), and msgAndArgs.
func (h *Harness) await(cond func() bool, failure func() string, msgAndArgs ...any) {
	h.t.Helper()

	began := time.Now()

	timer := time.NewTimer(h.wait)
	defer timer.Stop()

	var (
		told       int           // the changes the engine told while the wait went on
		lastChange time.Duration // how far into the wait the last one came
	)

	for {
		// Taken before cond looks, so a change while it looks still wakes the wait.
		changed := h.changes.next()

		if cond() {
			return
		}

		select {
		case <-changed:
			told++
			lastChange = time.Since(began)
		case <-timer.C:
			why := stalled(cond, told, lastChange) // first, as its look updates what failure says

			require.FailNow(h.t, failure()+" within "+h.wait.String()+"; "+why, msgAndArgs...)

			return
		}
	}
}

// stalled says why a wait that ran out most likely did, from one last look at cond and
// the changes the engine told while it waited (told, the last lastChange into the wait):
// cond holds now, so the engine told no change once it did; the engine told no change at
// all, so the wait looked only once; or cond still didn't hold after the last change.
func stalled(cond func() bool, told int, lastChange time.Duration) string {
	switch {
	case cond():
		return "it holds now, but the engine told no change once it did, so the wait never looked again: " +
			"what makes it hold must come before a change the engine tells"
	case told == 0:
		return "the engine told no change while it waited, so it looked only once: " +
			"the run never moved on, or what it waits for is nothing the engine tells"
	default:
		return fmt.Sprintf("it still didn't hold after the last of the %d changes the engine told, %s into the wait",
			told, lastChange.Round(time.Millisecond))
	}
}

// changes tells every wait that the runs changed: a channel closed, and replaced, at
// each change.
type changes struct {
	mu sync.Mutex
	ch chan struct{}
}

func newChanges() *changes {
	return &changes{ch: make(chan struct{})}
}

// tell is the engines' Changed: it wakes every wait, and returns at once.
func (c *changes) tell() {
	c.mu.Lock()
	defer c.mu.Unlock()

	close(c.ch)
	c.ch = make(chan struct{})
}

// next returns the channel the next change closes.
func (c *changes) next() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.ch
}
