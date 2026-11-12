package scriptrun

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// The errors the tests' fakes return.
var (
	errFacts       = errors.New("the facts changed")
	errGone        = errors.New("the script was deleted")
	errUnsupported = errors.New("unsupported operating system")
)

// wait bounds how long a test waits for a run to start or end.
const wait = 15 * time.Second

// poll is how often a test looks again.
const poll = 10 * time.Millisecond

// testRepo is the repository the tests' clones hold.
func testRepo() repo.Name {
	return repo.Name{Owner: "acme-org", Repo: "sdk"}
}

// harness is an engine over a fresh database, its log folder, and its own clone locks.
type harness struct {
	t       *testing.T
	e       *Engine
	s       *Service
	locks   *clonelock.Set
	logs    *testutil.Logs
	changes atomic.Int64
}

// newHarness returns a harness whose engine stops its runs quickly, by the user's Stop
// and as the app quits; edit changes the config first. The engine stops every run when
// the test ends.
func newHarness(t *testing.T, edit ...func(*Config)) *harness {
	t.Helper()

	h := &harness{t: t, locks: clonelock.New(), logs: &testutil.Logs{}}
	cfg := Config{
		DB: testutil.DB(t), Locks: h.locks, LogDir: filepath.Join(t.TempDir(), "script-runs"),
		Logger: h.logs.Logger(), Changed: func() { h.changes.Add(1) },
	}

	for _, fn := range edit {
		fn(&cfg)
	}

	h.e = NewEngine(cfg)
	h.e.ladder = fastLadder()
	h.e.quitKill, h.e.quitGrace = 300*time.Millisecond, 300*time.Millisecond
	h.s = NewService(h.e)

	t.Cleanup(h.e.StopAll)

	return h
}

// fastLadder stops a group as StopLadder does, with short waits.
func fastLadder() execpath.Ladder {
	return execpath.Ladder{
		{Signal: syscall.SIGINT, Wait: 200 * time.Millisecond},
		{Signal: syscall.SIGTERM, Wait: 200 * time.Millisecond},
		{Signal: syscall.SIGKILL},
	}
}

// clone returns a fresh folder for a run to hold.
func (h *harness) clone() string {
	h.t.Helper()

	return testutil.RealTempDir(h.t)
}

// spec returns a script run, in the app, of a fake tool that plays run in a fresh clone.
func (h *harness) spec(run testutil.FakeRun) Spec {
	h.t.Helper()

	path, env := testutil.FakeTool(h.t, "tool", run)

	return Spec{
		Kind: KindScript, Title: "Lint", Source: "magex", Repo: testRepo(), Dir: h.clone(), Argv: []string{path},
		Env: append(os.Environ(), env),
	}
}

// start starts spec and fails the test when it can't.
func (h *harness) start(spec Spec) int64 {
	h.t.Helper()

	id, err := h.e.Start(h.t.Context(), spec)
	require.NoError(h.t, err)
	require.Positive(h.t, id)

	return id
}

// ended waits for the run to end and let go of what it held, and returns its record.
func (h *harness) ended(id int64) Record {
	h.t.Helper()

	require.Eventually(h.t, func() bool { return h.e.liveRun(id) == nil }, wait, poll, "run %d ends", id)

	rec := h.record(id)
	require.False(h.t, rec.Status.Live(), "run %d is recorded as ended", id)

	return rec
}

// record returns the run's record.
func (h *harness) record(id int64) Record {
	h.t.Helper()

	rec, err := h.e.store.get(h.t.Context(), id)
	require.NoError(h.t, err)

	return rec
}

// logText returns the run's log, or "" when there is none.
func (h *harness) logText(id int64) string {
	h.t.Helper()

	text, err := h.readLog(id)
	require.NoError(h.t, err)

	return text
}

// readLog returns the run's log, or "" when there is none.
func (h *harness) readLog(id int64) (string, error) {
	body, err := os.ReadFile(h.e.files.log(id))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}

	return string(body), err
}

// readyLine is what a fake run prints once it is going, which waitReady waits for.
const readyLine = "ready\n"

// waitReady waits for the run's log to say "ready", which its fake tool prints once it
// has set itself up.
func (h *harness) waitReady(id int64) {
	h.t.Helper()

	require.EventuallyWithT(h.t, func(c *assert.CollectT) {
		text, err := h.readLog(id)
		require.NoError(c, err)
		assert.Contains(c, text, "ready")
	}, wait, poll, "run %d is ready", id)
}

// count returns how many runs the history holds.
func (h *harness) count() int {
	h.t.Helper()

	recs, err := h.e.store.list(h.t.Context())
	require.NoError(h.t, err)

	return len(recs)
}

// waitCount waits for the history to hold n runs.
func (h *harness) waitCount(n int, msgAndArgs ...any) {
	h.t.Helper()

	require.EventuallyWithT(h.t, func(c *assert.CollectT) {
		recs, err := h.e.store.list(h.t.Context())
		require.NoError(c, err)
		assert.Len(c, recs, n)
	}, wait, poll, msgAndArgs...)
}

// insertRow records a run as going, as an earlier start of the app did, with its process.
func (h *harness) insertRow(runsIn RunsIn, dir string, pid, pgid int, startedAt time.Time) int64 {
	h.t.Helper()

	ctx := h.t.Context()

	id, err := h.e.store.insert(ctx, started{
		spec:    Spec{Kind: KindScript, Title: "Lint", Repo: testRepo(), Dir: dir, Argv: []string{"/usr/bin/true"}},
		command: "true", runsIn: runsIn, at: time.Now(), result: Result{Tool: "mage-x", Intent: map[string]any{"a": 1}},
	})
	require.NoError(h.t, err)
	require.NoError(h.t, h.e.store.setProcess(ctx, id, pid, pgid, startedAt))

	return id
}

// collector is an output reader that keeps every byte it is fed.
type collector struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write keeps p.
func (c *collector) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.buf.Write(p)
}

// String returns every byte fed so far.
func (c *collector) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.buf.String()
}

// fakeProcs stands in for the process table: which processes and groups are alive, and
// when each process started. It records the signals sent.
type fakeProcs struct {
	mu      sync.Mutex
	alive   map[int]bool
	groups  map[int]bool
	starts  map[int]time.Time
	signals []syscall.Signal
}

// newFakeProcs returns a process table with nothing running.
func newFakeProcs() *fakeProcs {
	return &fakeProcs{alive: map[int]bool{}, groups: map[int]bool{}, starts: map[int]time.Time{}}
}

// install makes e read the process table from f and signal into it.
func (f *fakeProcs) install(e *Engine) {
	e.alive = f.isAlive
	e.groupAlive = f.groupIsAlive
	e.startedAt = f.startOf
	e.signal = f.send
}

// run records a process pid, leading its own group, started at at.
func (f *fakeProcs) run(pid int, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.alive[pid], f.groups[pid], f.starts[pid] = true, true, at
}

// end ends the process pid and its group.
func (f *fakeProcs) end(pid int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.alive, pid)
	delete(f.groups, pid)
	delete(f.starts, pid)
}

func (f *fakeProcs) isAlive(pid int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.alive[pid]
}

func (f *fakeProcs) groupIsAlive(pgid int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.groups[pgid]
}

func (f *fakeProcs) startOf(pid int) time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.starts[pid]
}

func (f *fakeProcs) send(_ int, sig syscall.Signal) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.signals = append(f.signals, sig)

	return nil
}

// sent returns the signals sent so far.
func (f *fakeProcs) sent() []syscall.Signal {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]syscall.Signal(nil), f.signals...)
}

// shellScript is what a terminal's script does, without the pseudo-terminal: record the
// shell's PID, run the command into the log, and write its exit status under a temporary
// name and rename it. With NO_EXIT_FILE set, the shell ends without one, as when the
// terminal window closes mid-run.
const shellScript = `printf '%s\n' "$$" > "$PID_FILE"
"$@" >> "$LOG_FILE" 2>&1
status=$?
if [ -n "${NO_EXIT_FILE:-}" ]; then exit "$status"; fi
printf '%s\n' "$status" > "$EXIT_FILE.tmp" && mv "$EXIT_FILE.tmp" "$EXIT_FILE"
exit "$status"`

// fakeTerminal opens a run as a terminal would, with /bin/sh running shellScript, and no
// window. It records each run it was asked to open.
type fakeTerminal struct {
	t       *testing.T
	err     error
	noExit  bool
	nothing bool
	mu      sync.Mutex
	opened  []TerminalRun
	folders []string
	procs   []*execpath.Proc
}

// newFakeTerminal returns a fake terminal whose shells are killed when the test ends.
func newFakeTerminal(t *testing.T) *fakeTerminal {
	t.Helper()

	f := &fakeTerminal{t: t}

	t.Cleanup(func() {
		f.mu.Lock()
		defer f.mu.Unlock()

		for _, p := range f.procs {
			p.Stop(fastLadder())
			_, _ = p.Wait()
		}
	})

	return f
}

// OpenRun starts the run's script with /bin/sh, unless told to fail or do nothing. The
// shell outlives the call, as a terminal's does; the test's cleanup stops it.
func (f *fakeTerminal) OpenRun(ctx context.Context, run TerminalRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.opened = append(f.opened, run)

	if f.err != nil || f.nothing {
		return f.err
	}

	env := append(os.Environ(), run.Env...)
	env = append(env, "PID_FILE="+run.PIDFile, "LOG_FILE="+run.Log, "EXIT_FILE="+run.ExitFile)

	if f.noExit {
		env = append(env, "NO_EXIT_FILE=1")
	}

	proc, err := execpath.Start(context.WithoutCancel(ctx), execpath.Cmd{
		Path: "/bin/sh", Args: append([]string{"-c", shellScript, "sh"}, run.Argv...), Dir: run.Dir, Env: env,
	})
	if err != nil {
		return err
	}

	f.procs = append(f.procs, proc)

	return nil
}

// OpenFolder records the folder it was asked to open a terminal in, unless told to fail.
func (f *fakeTerminal) OpenFolder(_ context.Context, name, dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.folders = append(f.folders, name+" "+dir)

	return f.err
}

// opensIn returns the folders it was asked to open a terminal in, each after its name.
func (f *fakeTerminal) opensIn() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.folders...)
}

// fakeReveal records the files it was asked to show.
type fakeReveal struct {
	mu    sync.Mutex
	shown []string
	err   error
}

func (f *fakeReveal) Reveal(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.shown = append(f.shown, path)

	return f.err
}

func (f *fakeReveal) paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.shown...)
}

// runs returns the runs it was asked to open.
func (f *fakeTerminal) runs() []TerminalRun {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]TerminalRun(nil), f.opened...)
}

// fakeRerunner works a run out again as the spec it holds now, and records what it was
// asked.
type fakeRerunner struct {
	mu    sync.Mutex
	spec  Spec
	err   error
	asked []Record
}

// Rerun returns the spec it holds.
func (f *fakeRerunner) Rerun(_ context.Context, run Record) (Spec, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.asked = append(f.asked, run)

	return f.spec, f.err
}
