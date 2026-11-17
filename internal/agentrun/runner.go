package agentrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/textutil"
)

const (
	// UsageEvery is how often, at most, a run's tokens so far are written to its record.
	UsageEvery = 30 * time.Second

	// promptHeadRunes is how much of the prompt the session's record keeps.
	promptHeadRunes = 200
)

// ErrNoRun is returned for a run the runner doesn't know.
var ErrNoRun = errors.New("no such agent run")

// Git reads what a run left in its worktree, read-only: *gitread.Git.
type Git interface {
	Head(ctx context.Context, dir string) (string, error)
	ChangedFiles(ctx context.Context, dir, from, to string) ([]string, error)
	WorktreeDiff(ctx context.Context, dir, from string) (gitread.WorktreeDiff, error)
}

// SessionState is what a run's session does, as its run says.
type SessionState string

// The states a run gives its session.
const (
	SessionWorking SessionState = "working"
	SessionEnded   SessionState = "ended"
	SessionFailed  SessionState = "failed"
)

// StartedSession is a run's session, as it is recorded once the run starts.
type StartedSession struct {
	Session

	// ID is the session's ID, and RunID the run's in the history.
	ID    string
	RunID int64

	// Dir is the worktree it runs in, PromptHead the start of its prompt, and LaunchedAt
	// when it started.
	Dir        string
	PromptHead string
	LaunchedAt time.Time
}

// SessionUpdate is what a run says of its session.
type SessionUpdate struct {
	State       SessionState
	LastMessage string
}

// Sessions records each run as a session, which the item's row follows (core adapts the
// sessions ledger).
type Sessions interface {
	Started(ctx context.Context, s StartedSession) error
	State(ctx context.Context, sessionID string, u SessionUpdate) error
}

// Sweeper ends what a run left running in its worktree after its group got SIGKILL: an
// agent's background shell leaves the group, and SIGKILL leaves it alive.
type Sweeper interface {
	Sweep(ctx context.Context, dir string)
}

// End is how a run ended, once its record holds it.
type End struct {
	// ScriptRunID is the run in the history, and Intent what it set out to do.
	ScriptRunID int64
	Intent      Intent

	Outcome Outcome
	EndedAt time.Time
}

// Usage is a run's tokens so far.
type Usage struct {
	RunID     int64
	TokensIn  int64
	TokensOut int64
	CacheRead int64
}

// Config holds a Runner's dependencies. Engine and Runs are required.
type Config struct {
	// Engine runs the runs, and Runs reads their history and logs.
	Engine *scriptrun.Engine
	Runs   *scriptrun.Service

	// Git reads the worktree's commit as a run ends, and the files its commits changed.
	// Without it, neither is read.
	Git Git

	// Sessions records each run as a session. Optional.
	Sessions Sessions

	// Ended hears each run's end once its record holds it, once, on the goroutine that
	// recorded it: it must return promptly. Autopilot's engine hears it (RunEnded).
	Ended func(ctx context.Context, end End)

	// Changed hears, with the ledger's run, that a run's events changed, as its stream
	// grows; core tells the window, a few times a second at most. It must return promptly.
	Changed func(runID int64)

	// Usage hears a run's tokens so far, at most every UsageEvery. Optional.
	Usage func(ctx context.Context, u Usage)

	// KeepAwake reports whether to keep the Mac from idle sleep while a run goes, through
	// Caffeinate (/usr/bin/caffeinate), started beside each run. Either nil or "" keeps
	// nothing awake.
	KeepAwake  func(ctx context.Context) bool
	Caffeinate string

	// Sweeper ends what a run left in its worktree after SIGKILL. Optional.
	Sweeper Sweeper

	Clock  clock.Clock
	Logger *slog.Logger
}

// Runner starts agents' background runs, follows their streams, and tells how each ended.
// It is the runs' Verifier too, registered for KindAgent.
type Runner struct {
	engine     *scriptrun.Engine
	runs       *scriptrun.Service
	git        Git
	sessions   Sessions
	ended      func(context.Context, End)
	changed    func(int64)
	usage      func(context.Context, Usage)
	keepAwake  func(context.Context) bool
	caffeinate string
	sweeper    Sweeper
	clock      clock.Clock
	logger     *slog.Logger

	mu       sync.Mutex
	byLedger map[int64]*run
	byScript map[int64]*run
	starting map[int64]*run
}

// New returns a Runner over cfg, and registers it as the agents' runs' Verifier.
func New(cfg Config) *Runner {
	r := &Runner{
		engine: cfg.Engine, runs: cfg.Runs, git: cfg.Git, sessions: cfg.Sessions, ended: cfg.Ended,
		changed: cfg.Changed, usage: cfg.Usage, keepAwake: cfg.KeepAwake, caffeinate: cfg.Caffeinate,
		sweeper: cfg.Sweeper, clock: clock.Or(cfg.Clock), logger: logging.Or(cfg.Logger),
		byLedger: map[int64]*run{}, byScript: map[int64]*run{}, starting: map[int64]*run{},
	}

	if cfg.Engine != nil {
		cfg.Engine.RegisterVerifier(scriptrun.KindAgent, r)
	}

	return r
}

// run is one run the runner follows.
type run struct {
	intent Intent
	dir    string
	parser *stream

	mu        sync.Mutex
	scriptID  int64
	recorded  bool
	pending   *SessionUpdate
	delivered bool
	lastUsage time.Time
	awake     *execpath.Proc
	cancel    context.CancelFunc

	// done is closed once the run's end is told, or its start failed (Wait).
	done     chan struct{}
	doneOnce sync.Once
}

// newRun returns a run of intent in dir, read by parser.
func newRun(intent Intent, dir string, parser *stream) *run {
	return &run{intent: intent, dir: dir, parser: parser, done: make(chan struct{})}
}

// finished marks the run's end told, or its start failed, once.
func (rn *run) finished() {
	rn.doneOnce.Do(func() { close(rn.done) })
}

// Start starts plan's run and returns its ID in the history. In order, under the
// worktree's lock and a slot: the plan's own Before (the worktree is still clean, and no
// session runs there), then the run's folder, 0700, with its prompt and settings, 0600;
// then it is recorded and started, its prompt on stdin. Once it starts, it is recorded as a
// session. An error from Before comes back as it is; one once the engine stopped matches
// scriptrun.ErrClosed.
func (r *Runner) Start(ctx context.Context, plan Plan) (int64, error) {
	if err := plan.check(); err != nil {
		return 0, err
	}

	if _, err := agent.HeadlessOf(plan.Agent); err != nil {
		return 0, err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	rn := newRun(intentOf(plan), plan.Dir, parserFor(plan.Agent, plan.Dir))
	rn.cancel = cancel

	r.mu.Lock()
	r.starting[plan.RunID] = rn
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.starting, plan.RunID)
		r.mu.Unlock()
	}()

	id, err := r.engine.Start(ctx, r.spec(plan, rn))
	if err != nil {
		rn.finished()

		return id, err
	}

	r.register(id, rn)
	r.recordSession(ctx, plan, rn, id)
	r.stayAwake(ctx, rn, id)

	return id, nil
}

// Verify gives a run from before the app restarted its callbacks again, from its intent:
// its log is read from the start, and its end judged as a run the runner started.
func (r *Runner) Verify(rec scriptrun.Record) scriptrun.Verification {
	intent := intentIn(rec.Result.Intent)
	rn := newRun(intent, rec.Dir, parserFor(intent.Agent, rec.Dir))
	rn.recorded = true

	r.register(rec.ID, rn)

	return scriptrun.Verification{
		Callbacks: r.callbacks(rn), Timeout: intent.Fence.Timeout, OutlivesQuit: outlives(intent),
	}
}

// Alive says what the runner knows of the ledger's run: alive while it starts, goes, or
// its end hasn't been told; gone once it ended; never when the history holds no run of it.
func (r *Runner) Alive(ctx context.Context, runID int64) (Liveness, error) {
	r.mu.Lock()
	rn, followed := r.byLedger[runID]
	_, starting := r.starting[runID]
	r.mu.Unlock()

	switch {
	case starting:
		return LiveAlive, nil
	case followed:
		rn.mu.Lock()
		defer rn.mu.Unlock()

		if rn.delivered {
			return LiveGone, nil
		}

		return LiveAlive, nil
	}

	rec, err := r.record(ctx, runID)

	switch {
	case errors.Is(err, ErrNoRun):
		return LiveNever, nil
	case err != nil:
		return LiveUnknown, err
	case rec.Status.Live():
		return LiveAlive, nil
	default:
		return LiveGone, nil
	}
}

// ScriptRun returns the history's ID of the ledger's run, if the history holds it.
func (r *Runner) ScriptRun(ctx context.Context, runID int64) (int64, bool) {
	r.mu.Lock()
	rn, followed := r.byLedger[runID]
	r.mu.Unlock()

	if followed {
		rn.mu.Lock()
		defer rn.mu.Unlock()

		return rn.scriptID, rn.scriptID != 0
	}

	rec, err := r.record(ctx, runID)

	return rec.ID, err == nil
}

// Stop stops the ledger's run, as the run's Stop does; one still starting is called off.
func (r *Runner) Stop(ctx context.Context, runID int64) error {
	r.mu.Lock()
	rn, starting := r.starting[runID]
	r.mu.Unlock()

	if starting {
		rn.cancel()

		return nil
	}

	id, ok := r.ScriptRun(ctx, runID)
	if !ok {
		return fmt.Errorf("%w: run %d", ErrNoRun, runID)
	}

	return r.runs.Stop(ctx, id)
}

// StopAll stops every agent's run, and calls off every one still starting, leaving the
// engine open for later starts; no script's run is touched.
func (r *Runner) StopAll(ctx context.Context) error {
	r.mu.Lock()
	for _, rn := range r.starting {
		rn.cancel()
	}
	r.mu.Unlock()

	_, err := r.engine.StopKind(ctx, scriptrun.KindAgent)

	return err
}

// Progress returns how far the ledger's run has got, while the runner follows it.
func (r *Runner) Progress(runID int64) (runevent.Progress, bool) {
	r.mu.Lock()
	rn, ok := r.byLedger[runID]
	r.mu.Unlock()

	if !ok {
		return runevent.Progress{}, false
	}

	return rn.parser.Progress(), true
}

// Folders returns the worktrees the agents' runs work in now, those still starting and
// those from before the app restarted included: the process list leaves both the app's
// own children and an orphan out, so the worktrees, the launcher, and the engine read
// these.
func (r *Runner) Folders() []Folder {
	live := r.engine.Live(scriptrun.KindAgent)

	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]Folder, 0, len(live)+len(r.starting))

	for _, l := range live {
		f := Folder{RunID: l.ID, Title: l.Title, Dir: l.Dir, PID: l.PID}
		if rn := r.byScript[l.ID]; rn != nil {
			f.LedgerRunID, f.Agent = rn.intent.RunID, rn.intent.Agent
		}

		out = append(out, f)
	}

	for _, rn := range r.starting {
		out = append(out, Folder{LedgerRunID: rn.intent.RunID, Agent: rn.intent.Agent, Dir: rn.dir})
	}

	return out
}

// spec is plan's run.
func (r *Runner) spec(plan Plan, rn *run) scriptrun.Spec {
	return scriptrun.Spec{
		Callbacks: r.callbacks(rn), Kind: scriptrun.KindAgent, Title: plan.Title, Source: plan.Source,
		Repo: plan.Repo, Dir: plan.Dir, Argv: plan.Argv, Env: plan.Env, Command: plan.Command,
		Timeout: plan.Fence.Timeout, Intent: rn.intent, Stdin: plan.PromptPath(), Folder: plan.Folder,
		OutlivesQuit: outlives(rn.intent),
		Before: func(ctx context.Context) ([]string, error) {
			if plan.Before != nil {
				if err := plan.Before(ctx); err != nil {
					return nil, err
				}
			}

			return nil, writeFolder(plan)
		},
	}
}

// register makes the runner follow rn as the run id.
func (r *Runner) register(id int64, rn *run) {
	rn.mu.Lock()
	rn.scriptID = id
	rn.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	r.byScript[id] = rn
	r.byLedger[rn.intent.RunID] = rn
}

// recordSession records the run as a session, working, and then whatever its end said
// before the record was there.
func (r *Runner) recordSession(ctx context.Context, plan Plan, rn *run, id int64) {
	if r.sessions == nil {
		return
	}

	err := r.sessions.Started(ctx, StartedSession{
		Session: plan.Session, ID: plan.SessionID, RunID: id, Dir: plan.Dir,
		PromptHead: textutil.Clip(plan.Prompt, promptHeadRunes), LaunchedAt: r.clock.Now(),
	})
	if err != nil {
		r.logger.WarnContext(ctx, "could not record an agent's run as a session", "run", id, "error", err)
	}

	rn.mu.Lock()
	rn.recorded = true
	pending := rn.pending
	rn.pending = nil
	rn.mu.Unlock()

	if pending != nil {
		r.setSession(ctx, rn, *pending)
	}
}

// setSession tells the run's session what it does now, once the session is recorded.
func (r *Runner) setSession(ctx context.Context, rn *run, u SessionUpdate) {
	if r.sessions == nil {
		return
	}

	rn.mu.Lock()
	if !rn.recorded {
		rn.pending = &u
		rn.mu.Unlock()

		return
	}
	rn.mu.Unlock()

	if err := r.sessions.State(ctx, rn.intent.SessionID, u); err != nil {
		r.logger.WarnContext(ctx, "could not record what an agent's run does", "error", err)
	}
}

// stayAwake keeps the Mac from idle sleep while the run id goes, when the user asks it to:
// caffeinate -i -w <pid> ends by itself with the run's program.
func (r *Runner) stayAwake(ctx context.Context, rn *run, id int64) {
	if r.keepAwake == nil || r.caffeinate == "" || !r.keepAwake(ctx) {
		return
	}

	pid := 0

	for _, l := range r.engine.Live(scriptrun.KindAgent) {
		if l.ID == id {
			pid = l.PID
		}
	}

	if pid <= 0 {
		return
	}

	proc, err := execpath.Start(context.WithoutCancel(ctx), execpath.Cmd{
		Path: r.caffeinate, Args: []string{"-i", "-w", strconv.Itoa(pid)},
	})
	if err != nil {
		r.logger.WarnContext(ctx, "could not keep the Mac awake for an agent's run", "run", id, "error", err)

		return
	}

	rn.mu.Lock()
	rn.awake = proc
	rn.mu.Unlock()
}

// callbacks are a run's: its parser reads the log, its todo list is its progress, its
// result line ends it, and Finish judges it.
func (r *Runner) callbacks(rn *run) scriptrun.Callbacks {
	return scriptrun.Callbacks{
		Output: feed{runner: r, rn: rn},
		Progress: func() scriptrun.Progress {
			return scriptrun.Progress{Working: rn.parser.Progress().Step}
		},
		Over: func() bool {
			if rn.parser.Over() {
				return true
			}

			p := rn.parser.Progress()

			return rn.intent.Fence.BudgetTokens > 0 && p.TokensIn+p.TokensOut >= rn.intent.Fence.BudgetTokens
		},
		Finish: func(ctx context.Context, end scriptrun.Ending, _ scriptrun.Outcome) scriptrun.Outcome {
			return r.finish(ctx, rn, end)
		},
		Recorded: func(ctx context.Context, id int64, out scriptrun.Outcome) {
			r.recorded(ctx, rn, id, out)
		},
	}
}

// finish judges how the run came out, reads what it left in its worktree, and, after its
// group got SIGKILL, ends what it left running there.
func (r *Runner) finish(ctx context.Context, rn *run, end scriptrun.Ending) scriptrun.Outcome {
	result, hasResult := rn.parser.Result()
	progress := rn.parser.Progress()

	j := judgment{
		end: end, fence: rn.intent.Fence, limit: rn.parser.Limit(), words: rn.parser.lastWords(),
		tokens: progress.TokensIn + progress.TokensOut, unknown: progress.Unknown,
	}

	if hasResult {
		j.result = &result
	}

	out := sandboxed(judge(j), sandboxOf(rn.intent.Folder))

	if out.Result == nil {
		out.TokensIn, out.TokensOut, out.CacheRead = progress.TokensIn, progress.TokensOut, progress.CacheRead
	}

	out.Todos = progress.Todos

	if end.Killed && r.sweeper != nil {
		r.sweeper.Sweep(ctx, rn.dir)
	}

	if out.Status != StatusInterrupted {
		out.Commit, out.Changed = r.worktree(ctx, rn)
	}

	return scriptrun.Outcome{Status: scriptStatus(out.Status), Summary: out.Summary, Detail: out}
}

// worktree reads the worktree's commit as the run ended, and the files its commits changed
// since the head it started on; nothing that can't be read.
func (r *Runner) worktree(ctx context.Context, rn *run) (string, []string) {
	if r.git == nil {
		return "", []string{}
	}

	commit, err := r.git.Head(ctx, rn.dir)
	if err != nil {
		r.logger.WarnContext(ctx, "could not read an agent's run's commit", "error", err)

		return "", []string{}
	}

	before := rn.intent.HeadBefore
	if before == "" || commit == "" || commit == before {
		return commit, []string{}
	}

	changed, err := r.git.ChangedFiles(ctx, rn.dir, before, commit)
	if err != nil {
		r.logger.WarnContext(ctx, "could not read the files an agent's run changed", "error", err)

		return commit, []string{}
	}

	return commit, changed
}

// recorded tells the run's session how it ended, and Ended, once its record holds it. A
// run that couldn't start tells nothing: its start's error says so.
func (r *Runner) recorded(ctx context.Context, rn *run, id int64, out scriptrun.Outcome) {
	rn.mu.Lock()
	awake := rn.awake
	rn.awake = nil
	rn.mu.Unlock()

	if awake != nil {
		_ = awake.Signal(syscall.SIGTERM)
	}

	defer rn.finished()

	outcome, ok := out.Detail.(Outcome)
	if !ok {
		r.forget(id, rn)

		return
	}

	state := SessionEnded
	if outcome.Status == StatusFailed || outcome.Status == StatusOutOfBudget || outcome.Status == StatusOutOfUsage ||
		outcome.Status == StatusTimedOut {
		state = SessionFailed
	}

	r.setSession(ctx, rn, SessionUpdate{State: state, LastMessage: lastMessage(outcome)})

	if r.ended != nil {
		r.ended(ctx, End{ScriptRunID: id, Intent: rn.intent, Outcome: outcome, EndedAt: r.clock.Now()})
	}

	rn.mu.Lock()
	rn.delivered = true
	rn.mu.Unlock()

	r.forget(id, rn)
}

// forget stops following the run id.
func (r *Runner) forget(id int64, rn *run) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.byScript[id] == rn {
		delete(r.byScript, id)
	}

	if r.byLedger[rn.intent.RunID] == rn {
		delete(r.byLedger, rn.intent.RunID)
	}
}

// record finds the ledger's run in the history.
func (r *Runner) record(ctx context.Context, runID int64) (scriptrun.Run, error) {
	runs, err := r.runs.Runs(ctx, scriptrun.Filter{Kinds: []scriptrun.Kind{scriptrun.KindAgent}})
	if err != nil {
		return scriptrun.Run{}, err
	}

	for _, run := range runs {
		if intentIn(run.Result.Intent).RunID == runID {
			return run, nil
		}
	}

	return scriptrun.Run{}, fmt.Errorf("%w: run %d", ErrNoRun, runID)
}

// outlives reports a run that may go on once the app quits: its agent keeps to a budget in
// dollars of its own, so something bounds it while no copy of the app follows it.
func outlives(in Intent) bool {
	h, err := agent.HeadlessOf(in.Agent)

	return err == nil && h.Budget && in.Fence.BudgetUSD > 0
}

// writeFolder makes the run's own folder, if its fence's files didn't, and writes its
// prompt into it.
func writeFolder(plan Plan) error {
	if err := os.MkdirAll(plan.Folder, appdata.PrivateDirPerm); err != nil {
		return fmt.Errorf("make the run's folder: %w", err)
	}

	if err := os.WriteFile(plan.PromptPath(), []byte(plan.Prompt), 0o600); err != nil {
		return fmt.Errorf("write the run's prompt: %w", err)
	}

	return nil
}

// feed is a run's output reader: its parser, which tells Changed of each part it read, and
// Usage of the tokens so far every UsageEvery.
type feed struct {
	runner *Runner
	rn     *run
}

var _ io.Writer = feed{}

// Write feeds the run's parser.
func (f feed) Write(p []byte) (int, error) {
	n, err := f.rn.parser.Write(p)

	f.rn.mu.Lock()
	due := f.runner.usage != nil && f.runner.clock.Now().Sub(f.rn.lastUsage) >= UsageEvery

	if due {
		f.rn.lastUsage = f.runner.clock.Now()
	}
	f.rn.mu.Unlock()

	if f.runner.changed != nil {
		f.runner.changed(f.rn.intent.RunID)
	}

	if due {
		progress := f.rn.parser.Progress()
		f.runner.usage(context.Background(), Usage{
			RunID: f.rn.intent.RunID, TokensIn: progress.TokensIn, TokensOut: progress.TokensOut,
			CacheRead: progress.CacheRead,
		})
	}

	return n, err
}

// lastMessage is what a run's session's row shows of how it ended: its report's summary,
// its final message, or its own sentence.
func lastMessage(o Outcome) string {
	switch {
	case o.Report != nil && o.Report.Summary != "":
		return o.Report.Summary
	case o.Result != nil && o.Result.Message != "":
		return o.Result.Message
	default:
		return o.Summary
	}
}

// intentIn reads a run's intent from its record.
func intentIn(raw any) Intent {
	var in Intent

	body, err := json.Marshal(raw)
	if err == nil {
		_ = json.Unmarshal(body, &in) // a record without one reads as the zero intent
	}

	return in
}

// parserFor returns the stream parser of the agent with id, for a run in dir.
func parserFor(_ agent.ID, dir string) *stream {
	return newClaudeParser(dir)
}

// Liveness is what the runner knows of a run the ledger records as going.
type Liveness string

// The answers about a run.
const (
	LiveUnknown Liveness = "unknown"
	LiveAlive   Liveness = "alive"
	LiveGone    Liveness = "gone"
	LiveNever   Liveness = "never"
)

// Folder is a worktree an agent's run works in now.
type Folder struct {
	// RunID is the run in the history, or 0 while it starts, and LedgerRunID the ledger's;
	// Title is the run's, and Agent the agent it runs.
	RunID       int64
	LedgerRunID int64
	Title       string
	Agent       agent.ID

	// Dir is the worktree, and PID the agent's process, or 0 while it starts.
	Dir string
	PID int
}
