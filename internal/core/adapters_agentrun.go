package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/agentrun"
	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/paths"
	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/sessions/observe/codex"
	"github.com/mrz1836/nilbox/internal/shellenv"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/worktrees"
)

// caffeinatePath is macOS's caffeinate, which keeps the Mac from idle sleep while a run's
// program runs (caffeinate -i -w <pid>).
const caffeinatePath = "/usr/bin/caffeinate"

// runEventsEvery is the least time between two autopilot:run events of one run.
const runEventsEvery = 250 * time.Millisecond

// backgroundAgents runs a routine's agent steps (autopilot.Runner): one in the background
// through the launcher's own background plan of the request a launch in the terminal
// makes (routineRequest), the item's worktree, workspace trust, and the background
// runner, exactly as a click's launch goes, but for the terminal and the fence; one in the
// terminal through the terminal's runner (agentRuns). It is also the app's own runs'
// folders (worktrees.RunFolders, launcher.RunFolders), the window's reads of the runs
// (autopilot.RunViews), and their logs' clearing (autopilot.RunLogs). Core builds it
// before the services that read it, and fills it in once the runs are built.
type backgroundAgents struct {
	launch   backgroundPlanner
	maker    launcher.WorktreeMaker
	touch    launcher.SessionRecorder
	ledger   *sessions.Ledger
	git      *gitread.Git
	shellEnv func() ([]string, bool)
	home     string
	logger   *slog.Logger

	// program is the app's own executable, which a run's fence hook runs in its hook mode,
	// and changes notes what a run's commits changed on its item (*inbox.Store), so a run
	// that wrote agent files holds the next launch there; either may be empty.
	program string
	changes agentChangeNoter

	// requests turns a routine's step into the launcher's request, as for a launch in the
	// terminal (routineRequest); filled in once the routines' agents are built.
	requests func(ctx context.Context, run autopilot.RunRequest) (launcher.LaunchRequest, error)

	// runner and runs are the background runner and the run engine, once built; engine is
	// Autopilot's, once built.
	runner *agentrun.Runner
	runs   *scriptrun.Engine
	engine func() runEngine

	// terminal runs an agent step in the user's terminal, nil until that runner is built.
	terminal autopilot.Runner
}

var _ autopilot.Runner = (*backgroundAgents)(nil)

// backgroundPlanner plans a background run and trusts its worktree: the launcher's
// Backgrounds.
type backgroundPlanner interface {
	Plan(ctx context.Context, req launcher.BackgroundRequest) (launcher.BackgroundPlan, *launcher.Hold, error)
	Preview(ctx context.Context, req launcher.BackgroundRequest) (launcher.Preview, error)
	Made(bp launcher.BackgroundPlan, wt launcher.Worktree, routine *launcher.RoutineLaunch) *launcher.Hold
	Fenced(bp launcher.BackgroundPlan, in launcher.FenceIn) (launcher.BackgroundPlan, agent.FencedRun, error)
	Trust(ctx context.Context, bp launcher.BackgroundPlan) error
}

// agentChangeNoter records the files an agent run's commits changed on an item, at its
// heads: *inbox.Store.
type agentChangeNoter interface {
	NoteAgentChanges(ctx context.Context, itemID int64, heads, files []string, runID int64) error
}

// runEngine is what the runner tells Autopilot's engine of its runs, and Take over of the
// runs the user takes over: *autopilot.Engine.
type runEngine interface {
	RunUsage(runID int64, u autopilot.RunUsage)
	RunEnded(end autopilot.RunEnd)
	TakeOver(ctx context.Context, runID int64) error
}

// errNoBackground is returned while the background runner isn't built.
var errNoBackground = errors.New("the background runner isn't ready")

// Dispatch starts job's agent step: in the background, it plans the run as a launch is
// planned, makes or resumes the item's worktree at the head the plan checked, trusts it,
// and starts the run; what holds the start back comes back as the Result a launch the
// launcher held back makes (Wait, Hold, Requeue), so the match waits for it and nothing
// counts. A step in the terminal goes to the terminal's runner.
func (b *backgroundAgents) Dispatch(ctx context.Context, job autopilot.Job) (autopilot.Result, error) {
	step := job.Step.Agent

	switch {
	case step == nil:
		return autopilot.Result{}, uierr.New("This step runs no agent.", autopilot.ErrNoAgentStep)
	case step.Place() != catalog.WhereBackground && b.terminal != nil:
		return b.terminal.Dispatch(ctx, job)
	case step.Place() != catalog.WhereBackground:
		return notReady("this copy can't run an agent in your terminal."), nil
	case b.runner == nil || b.launch == nil:
		return notReady("the background runner isn't ready."), nil
	}

	res, err := b.dispatch(ctx, job, *step)
	if w, ok := errors.AsType[*waitError](err); ok {
		return w.res, nil
	}

	return res, err
}

// PreviewRun plans req's step in the background as it would start, holding nothing back
// and asking GitHub nothing: the launcher's own background Preview, in the words of a
// launch's (runPreviews).
func (b *backgroundAgents) PreviewRun(ctx context.Context, req autopilot.RunRequest) (autopilot.RunPreview, error) {
	if b.launch == nil {
		return autopilot.RunPreview{}, uierr.New("This copy can't run an agent in the background.", autopilot.ErrNoAgentStep)
	}

	launch, err := b.request(ctx, req)
	if err != nil {
		return autopilot.RunPreview{}, err
	}

	got, err := b.launch.Preview(ctx, launch)
	if err != nil {
		return autopilot.RunPreview{}, err
	}

	return previewOf(got), nil
}

// Stop stops the ledger's run.
func (b *backgroundAgents) Stop(ctx context.Context, runID int64) error {
	if b.runner == nil {
		return errNoBackground
	}

	err := b.runner.Stop(ctx, runID)
	if errors.Is(err, agentrun.ErrNoRun) && b.terminal != nil {
		return b.terminal.Stop(ctx, runID)
	}

	return err
}

// StopAll stops every agent's run in the background, and leaves the runner open.
func (b *backgroundAgents) StopAll(ctx context.Context) error {
	if b.runner == nil {
		return nil
	}

	return b.runner.StopAll(ctx)
}

// Alive says what the runner knows of the ledger's run: one it never started is never,
// whatever its place, and the engine takes a run in the terminal it never started as gone.
func (b *backgroundAgents) Alive(ctx context.Context, runID int64) (autopilot.Liveness, error) {
	if b.runner == nil {
		return autopilot.LiveUnknown, errNoBackground
	}

	live, err := b.runner.Alive(ctx, runID)

	return autopilot.Liveness(live), err
}

// Ended tells Autopilot's engine how a run ended, once its record holds it.
func (b *backgroundAgents) Ended(ctx context.Context, end agentrun.End) {
	b.noteChanges(ctx, end)

	e := b.engineNow()
	if e == nil {
		return
	}

	e.RunEnded(runEnd(end))
}

// Progress returns how far the ledger's run has got (autopilot.RunViews).
func (b *backgroundAgents) Progress(runID int64) (runevent.Progress, bool) {
	if b.runner == nil {
		return runevent.Progress{}, false
	}

	return b.runner.Progress(runID)
}

// Events returns the ledger's run's events after one (autopilot.RunViews).
func (b *backgroundAgents) Events(ctx context.Context, runID, after int64) (runevent.Page, error) {
	if b.runner == nil {
		return runevent.Page{}, errNoBackground
	}

	return b.runner.Events(ctx, runID, after)
}

// ClearAgentHistory forgets the agents' runs that ended, their logs and folders with them.
func (b *backgroundAgents) ClearAgentHistory(ctx context.Context) error {
	if b.runs == nil {
		return nil
	}

	return b.runs.ClearAgentHistory(ctx)
}

// RunFolders names the worktrees the agents' runs work in (worktrees.RunFolders).
func (b *backgroundAgents) RunFolders(context.Context) []worktrees.RunFolder {
	out := []worktrees.RunFolder{}
	if b.runner == nil {
		return out
	}

	for _, f := range b.runner.Folders() {
		out = append(out, worktrees.RunFolder{Dir: f.Dir, PID: f.PID, Agent: f.Agent, By: byOf(f)})
	}

	return out
}

// RunsIn names the agents' runs working in dir as the sessions they are
// (launcher.RunFolders): a click there is always held back.
func (b *backgroundAgents) RunsIn(ctx context.Context, dir string) []launcher.RunningSession {
	out := []launcher.RunningSession{}
	if b.runner == nil {
		return out
	}

	for _, f := range b.runner.Folders() {
		if !paths.Same(f.Dir, dir) {
			continue
		}

		s := launcher.RunningSession{
			Agent: f.Agent, Doing: "is working in the background", Dir: paths.Tilde(f.Dir, b.home), Background: true,
		}

		if b.ledger != nil && f.RunID != 0 {
			if r, err := b.ledger.ByRun(ctx, f.RunID); err == nil {
				s.SessionID, s.Repo, s.Number, s.Worktree = r.ID, r.Repo(), r.Number, r.Worktree
			}
		}

		out = append(out, s)
	}

	return out
}

// noteChanges records the files a run's commits changed on its item, at the head it
// started from and its own last commit, so a launch there holds for an agent file it
// wrote (inbox.Store.NoteAgentChanges). A run that changed nothing notes nothing.
func (b *backgroundAgents) noteChanges(ctx context.Context, end agentrun.End) {
	in, o := end.Intent, end.Outcome
	if b.changes == nil || in.ItemID == 0 || len(o.Changed) == 0 {
		return
	}

	err := b.changes.NoteAgentChanges(ctx, in.ItemID, []string{in.HeadBefore, o.Commit}, o.Changed, in.RunID)
	if err != nil {
		b.logger.WarnContext(ctx, "could not record what an agent's run changed", "run", in.RunID, "error", err)
	}
}

// dispatch plans and starts job's step in the background, on the first of its chain (its
// own profile as a click resolves it, then each fallback in order) whose agent's profile
// isn't resting out of usage (job.Resting) and that can run here; a waitError says what
// held it. A fallback that can't run in the background on this Mac (its agent can't keep
// the preset yet, or isn't there) is passed over; with none left, the start waits until the
// soonest rest ends, as not ready when a fallback was passed over for that.
func (b *backgroundAgents) dispatch(ctx context.Context, job autopilot.Job, step spec.AgentStep) (autopilot.Result, error) {
	chain := chainOf(job.Resting)

	for i := range 1 + len(step.Fallbacks) {
		cand := step
		if i > 0 {
			cand = fallbackStep(step, step.Fallbacks[i-1], chain.primary)
		}

		res, started, err := b.tryLaunch(ctx, job, cand, chain, i > 0)
		if started || err != nil {
			return res, err
		}
	}

	return chain.waits(), nil
}

// tryLaunch plans job's step as cand, one launch of its chain, and starts it unless its
// agent's profile rests out of usage, or, for a fallback, it can't run here: it reports
// whether it is done (started, or held back for something that holds every launch of the
// chain), which res says, and an error that fails the start.
func (b *backgroundAgents) tryLaunch(ctx context.Context, job autopilot.Job, cand spec.AgentStep, chain *fallbacks,
	fallback bool,
) (autopilot.Result, bool, error) {
	run := job.RunRequest()
	run.Step = cand

	req, err := b.request(ctx, run)
	if err != nil {
		return autopilot.Result{}, false, err
	}

	bp, hold, err := b.launch.Plan(ctx, req)

	switch {
	case err != nil && fallback && cantRunHere(err):
		chain.passOver(err)

		return autopilot.Result{}, false, nil
	case err != nil:
		return autopilot.Result{}, false, planError(err, job)
	case hold != nil:
		return heldResult(*hold), true, nil
	}

	if !fallback {
		chain.primary = bp.Plan.Agent
	}

	if chain.rests(bp.Plan.Agent, bp.Profile) {
		return autopilot.Result{}, false, nil
	}

	res, err := b.start(ctx, job, cand, req, bp)

	return res, true, err
}

// start makes the item's worktree, trusts it, and starts the run there, reporting what it
// started for the run's record.
func (b *backgroundAgents) start(ctx context.Context, job autopilot.Job, step spec.AgentStep,
	req launcher.BackgroundRequest, bp launcher.BackgroundPlan,
) (autopilot.Result, error) {
	if bp.Worktree.Head == "" && job.Item.Kind == item.KindPR {
		bp.Worktree.Head = strings.ToLower(job.Head)
	}

	wt, err := b.made(ctx, req, bp)
	if err != nil {
		return autopilot.Result{}, err
	}

	env := b.envFor(bp.Plan)

	if bp, err = b.fence(bp, wt, step, env); err != nil {
		return autopilot.Result{}, err
	}

	if err = b.launch.Trust(ctx, bp); err != nil {
		return autopilot.Result{}, err
	}

	id, err := b.runner.Start(ctx, b.plan(job, step, bp, wt, env))
	if err != nil {
		return autopilot.Result{}, startError(err)
	}

	if b.touch != nil {
		worktree := item.WorktreeName(job.Item.Kind, job.Item.Number)
		if err = b.touch.Touch(ctx, bp.Clone.Dir, worktree, bp.Plan.Agent, time.Now()); err != nil {
			b.logger.DebugContext(ctx, "could not record the agent in its worktree", "error", err)
		}
	}

	return autopilot.Result{
		Running: true,
		Launched: &autopilot.Launched{
			SessionID: bp.SessionID, Agent: string(bp.Plan.Agent), Profile: bp.Profile, Model: bp.Resolution.Model.Value,
			Effort: bp.Resolution.Effort.Value, ClonePath: bp.Clone.Dir, Worktree: wt.Path, Command: bp.Command,
			ScriptRunID: id,
		},
	}, nil
}

// made makes or resumes the item's worktree at the head the plan checked, and holds the
// run back from one with changes the user didn't confirm, or one off that head.
func (b *backgroundAgents) made(ctx context.Context, req launcher.BackgroundRequest, bp launcher.BackgroundPlan,
) (launcher.Worktree, error) {
	wt, err := b.maker.EnsureWorktree(ctx, bp.Worktree)
	if err != nil {
		return launcher.Worktree{}, worktreeError(err)
	}

	if hold := b.launch.Made(bp, wt, req.Routine); hold != nil {
		return launcher.Worktree{}, &waitError{res: heldResult(*hold)}
	}

	return wt, notAtHead(wt, bp.Worktree.Head)
}

// request is a routine's step as the launcher plans a background run of it: the request
// its launch in the terminal would make (routineRequest), with the fence, the report's
// schema, and the unattended note.
func (b *backgroundAgents) request(ctx context.Context, run autopilot.RunRequest) (launcher.BackgroundRequest, error) {
	if b.requests == nil {
		return launcher.BackgroundRequest{}, errNoBackground
	}

	req, err := b.requests(ctx, run)
	if err != nil {
		return launcher.BackgroundRequest{}, err
	}

	usd, _ := fenceBudget(run.Step)

	return launcher.BackgroundRequest{
		LaunchRequest: req, Preset: fence.Preset(run.Step.Tools()), BudgetUSD: usd,
		Schema: agentrun.ReportSchema, SystemNote: agentrun.UnattendedNote,
	}, nil
}

// fence works the run's fence out for the worktree made for it (launcher.Fenced) and
// writes its files into the run's folder (the policy its hook holds the run to, the
// agent's settings, and for a sandboxed preset, the sandbox check's folder), returning the
// plan with the fence's line in its unattended note. A push preset on a fork's pull
// request waits as a cross-repository item does.
func (b *backgroundAgents) fence(bp launcher.BackgroundPlan, wt launcher.Worktree, step spec.AgentStep, env []string,
) (launcher.BackgroundPlan, error) {
	var allow, deny []string

	if f := step.Fence; f != nil {
		allow, deny = f.Allow, f.Deny
	}

	fenced, run, err := b.launch.Fenced(bp, launcher.FenceIn{
		Worktree: wt.Path, Program: b.program, Env: envMap(env), Allow: allow, Deny: deny,
	})

	switch {
	case errors.Is(err, fence.ErrCrossRepository):
		return bp, waitOn(autopilot.BlockCrossRepo, "Its branch is in a fork, which a run can't push to", err)
	case err != nil:
		return bp, uierr.New("Its fence couldn't be set up: "+textutil.Sentence(uierr.Message(err)), err)
	}

	if err = run.Write(fenced.Folder); err != nil {
		return bp, fmt.Errorf("write the run's fence: %w", err)
	}

	return fenced, nil
}

// plan is the run the runner starts, from the launcher's background plan and the worktree
// made for it.
func (b *backgroundAgents) plan(job autopilot.Job, step spec.AgentStep, bp launcher.BackgroundPlan,
	wt launcher.Worktree, env []string,
) agentrun.Plan {
	usd, tokens := fenceBudget(step)
	name, _ := repo.Parse(job.Item.Repo)

	head := strings.ToLower(wt.Head)
	if head == "" {
		head = strings.ToLower(job.Head)
	}

	return agentrun.Plan{
		RunID: job.RunID, MatchID: job.MatchID, RoutineID: job.RoutineID,
		Title:  job.RoutineName + " · " + job.Item.Repo + " #" + strconv.Itoa(job.Item.Number),
		Source: "autopilot:" + strconv.FormatInt(job.RoutineID, 10), Agent: bp.Plan.Agent, SessionID: bp.SessionID,
		Repo: name, Dir: wt.Path, Argv: append([]string{bp.Plan.Binary}, bp.Plan.Args...), Env: env,
		Command: bp.Command, Folder: bp.Folder, Prompt: bp.Prompt, HeadBefore: head,
		Fence: agentrun.Fence{
			Preset: string(bp.Preset), Timeout: fenceTimeout(step), BudgetUSD: usd, BudgetTokens: tokens,
			ReadOnly: bp.ReadOnly,
		},
		Session: agentrun.Session{
			Agent: bp.Plan.Agent, Profile: bp.Profile, ConfigDir: bp.ConfigDir, Prompt: bp.Resolution.Prompt.Nickname,
			ItemID: job.Item.ID, Repo: name, Kind: job.Item.Kind, Number: job.Item.Number, Title: job.Item.Title,
			URL: job.Item.URL, Clone: bp.Clone.Dir, Worktree: item.WorktreeName(job.Item.Kind, job.Item.Number),
			WorktreeAction: string(wt.Action), Permission: bp.Plan.Permission,
		},
		Before: b.before(wt.Path, wt.Dirty),
	}
}

// before checks, under the worktree's lock, that the worktree holds no more changes than
// the dirty files the user confirmed (dirty, 0 for none), and that no session the app
// follows works in it.
func (b *backgroundAgents) before(dir string, dirty int) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := b.clean(ctx, dir, dirty); err != nil {
			return err
		}

		return b.unfollowed(ctx, dir)
	}
}

// clean refuses a worktree whose changes aren't exactly the dirty files confirmed: the
// match goes back to be decided again, and its next plan holds what is there now.
func (b *backgroundAgents) clean(ctx context.Context, dir string, dirty int) error {
	if b.git == nil {
		return nil
	}

	changes, err := b.git.Changes(ctx, dir)
	if err != nil {
		return err
	}

	if len(changes) != dirty {
		return requeue("Its worktree changed as the run started.", nil)
	}

	return nil
}

// unfollowed refuses a worktree a session the app follows works in.
func (b *backgroundAgents) unfollowed(ctx context.Context, dir string) error {
	if b.ledger == nil {
		return nil
	}

	if running, err := b.ledger.Running(ctx, dir); err == nil && len(running) > 0 {
		return waitOn(autopilot.BlockBusyWorktree,
			"Queued: "+paths.Tilde(dir, b.home)+" is in use by a session; starts when it ends.", nil)
	}

	return nil
}

// envFor is a run's environment: the login shell's, as a terminal gives (or the app's with
// the finder's folders while the shell can't be read), with the plan's on top, the config
// folder never pointed at the agent's default one, no API key unless the user lets the
// agent use one, and none of the variables that steer the agent itself.
func (b *backgroundAgents) envFor(plan launcher.Plan) []string {
	base := []string{}
	if b.shellEnv != nil {
		base, _ = b.shellEnv()
	}

	return agentEnv(base, plan, b.home)
}

// engineNow returns Autopilot's engine, once built.
func (b *backgroundAgents) engineNow() runEngine {
	if b.engine == nil {
		return nil
	}

	return b.engine()
}

// ownExecutable is the app's own executable, its links resolved, which a run's fence hook
// runs in its hook mode; "" when it can't be told, which no fence takes.
func ownExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}

	if resolved, resolveErr := filepath.EvalSymlinks(exe); resolveErr == nil {
		exe = resolved
	}

	return exe
}

// fenceBudget is the step's budget per run: in dollars, which the agent keeps to itself, or
// in tokens, which the runner stops it at; with none, the catalog's default in dollars.
func fenceBudget(step spec.AgentStep) (float64, int64) {
	if f := step.Fence; f != nil && f.Budget != nil {
		switch {
		case f.Budget.Tokens != nil:
			return 0, *f.Budget.Tokens
		case f.Budget.USD != nil:
			return *f.Budget.USD, 0
		}
	}

	return float64(catalog.DefaultRunCents) / 100, 0
}

// fenceTimeout is the step's time limit.
func fenceTimeout(step spec.AgentStep) time.Duration {
	minutes := catalog.DefaultTimeoutMinutes
	if f := step.Fence; f != nil && f.TimeoutMinutes != nil {
		minutes = *f.TimeoutMinutes
	}

	return time.Duration(minutes) * time.Minute
}

// agentEnv builds a background run's environment from base (see envFor): the plan's own
// changes (launcher.AgentEnv's: the profile's config folder, or none for the default
// profile, and no API key unless the user lets the agent use one), the config folder
// never named as the agent's default one, which would sign it out, and none of the
// variables that steer the agent itself (agent.HeadlessEnv).
func agentEnv(base []string, plan launcher.Plan, home string) []string {
	env := shellenv.Without(base, plan.Unset...)

	for _, key := range slices.Sorted(maps.Keys(plan.Env)) {
		env = shellenv.With(env, key, plan.Env[key])
	}

	if a, err := agent.Lookup(plan.Agent); err == nil {
		if dir := shellenv.Lookup(env, a.ConfigEnv); dir != "" && home != "" &&
			paths.Same(dir, filepath.Join(home, a.ConfigDir)) {
			env = shellenv.Without(env, a.ConfigEnv)
		}
	}

	return agent.HeadlessEnv(plan.Agent, env)
}

// waitError is a start held back on the way, carrying the Result the engine takes it as:
// a wait, or a requeue.
type waitError struct {
	res autopilot.Result
	err error
}

// Error says why.
func (e *waitError) Error() string {
	if e.err != nil {
		return e.res.WaitWords + " " + e.err.Error()
	}

	return e.res.WaitWords
}

// Unwrap returns why.
func (e *waitError) Unwrap() error { return e.err }

// waitOn holds a start back, the match waiting on b, saying words.
func waitOn(b autopilot.Blocker, words string, err error) error {
	return &waitError{res: autopilot.Result{Wait: b, WaitWords: textutil.Sentence(words)}, err: err}
}

// requeue sends a start back to be decided again, on a fresh read.
func requeue(words string, err error) error {
	return &waitError{res: autopilot.Result{Requeue: true, WaitWords: textutil.Sentence(words)}, err: err}
}

// notReady is a start this Mac isn't ready for, saying why.
func notReady(why string) autopilot.Result {
	return autopilot.Result{Wait: autopilot.BlockNotReady, WaitWords: "Not ready on this Mac: " + why}
}

// planError is a background plan's failure as the engine takes it: no clone, an agent
// that isn't installed, and a preset this build can't fence make the match wait.
func planError(err error, job autopilot.Job) error {
	switch {
	case errors.Is(err, localrepos.ErrCloneNotFound):
		return waitOn(autopilot.BlockNoClone, "No local clone of "+job.Item.Repo, err)
	case errors.Is(err, launcher.ErrFenceNotKept), errors.Is(err, agent.ErrPresetNotKept),
		errors.Is(err, execpath.ErrNotFound):
		return &waitError{res: notReady(textutil.Sentence(uierr.Message(err))), err: err}
	default:
		return err
	}
}

// worktreeError is a worktree's failure as the engine takes it: a head that moved since
// the plan checked it sends the match back to be decided on the new head, and a clone
// another job holds makes it queue.
func worktreeError(err error) error {
	switch {
	case errors.Is(err, worktrees.ErrHeadMoved):
		return requeue("Its head moved as its worktree was made.", err)
	case errors.Is(err, clonelock.ErrBusy):
		return waitOn(autopilot.BlockBusyWorktree, "Queued: "+uierr.Message(err), err)
	default:
		return err
	}
}

// notAtHead holds a run back from a worktree that isn't at the head the plan checked: it
// may hold the user's own commits, and a review of it would be of other code.
func notAtHead(wt launcher.Worktree, head string) error {
	if head == "" || wt.Head == "" || strings.EqualFold(wt.Head, head) {
		return nil
	}

	return waitOn(autopilot.BlockDirty, filepath.Base(wt.Path)+
		" isn't at the pull request's head, and may hold your commits: update or remove it to let the run start", nil)
}

// startError is the runner's failure as the engine takes it: once the run engine closed,
// the start stays queued for the next start; a worktree another job holds makes it queue.
func startError(err error) error {
	switch {
	case errors.Is(err, scriptrun.ErrClosed):
		return fmt.Errorf("%w: %w", autopilot.ErrClosed, err)
	case errors.Is(err, clonelock.ErrBusy):
		return waitOn(autopilot.BlockBusyWorktree, "Queued: "+uierr.Message(err), err)
	default:
		return err
	}
}

// runEnd is a run's end in the engine's words.
func runEnd(end agentrun.End) autopilot.RunEnd {
	o := end.Outcome
	out := autopilot.RunEnd{
		RunID: end.Intent.RunID, Head: o.Commit, TokensIn: o.TokensIn, TokensOut: o.TokensOut, CacheRead: o.CacheRead,
		Summary: o.Summary, EndedAt: end.EndedAt,
	}

	if r := o.Result; r != nil {
		out.Cost, out.CostEstimated, out.Turns = r.Cost, r.Estimated, r.Turns
	}

	if rep := o.Report; rep != nil && rep.Pushed && rep.PushedCommit != "" && strings.EqualFold(rep.PushedCommit, o.Commit) {
		out.Commit = strings.ToLower(rep.PushedCommit)
	}

	if rep := o.Report; rep != nil {
		out.Review = agentReview(rep.Review)
	}

	out.Outcome = runOutcome(o.Status)

	switch out.Outcome {
	case autopilot.RunOutOfUsage:
		out.Error, out.Limit = o.Summary, usageLimit(o.Limit)
	case autopilot.RunFailed:
		out.Error = o.Summary
	case autopilot.RunDone, autopilot.RunStopped, autopilot.RunInterrupted, autopilot.RunNotSent, autopilot.RunNotStarted:
	}

	return out
}

// runOutcome is how a run came out, in the engine's words: one refused for its profile's
// usage limit is out of usage, which the engine falls back from, and every failure, its
// budget or time limit included, failed.
func runOutcome(s agentrun.Status) autopilot.RunOutcome {
	switch s {
	case agentrun.StatusDone:
		return autopilot.RunDone
	case agentrun.StatusStopped:
		return autopilot.RunStopped
	case agentrun.StatusInterrupted:
		return autopilot.RunInterrupted
	case agentrun.StatusOutOfUsage:
		return autopilot.RunOutOfUsage
	case agentrun.StatusFailed, agentrun.StatusOutOfBudget, agentrun.StatusTimedOut:
		return autopilot.RunFailed
	}

	return autopilot.RunFailed
}

// byOf names a run in the Worktrees view: "⚡ <its title>".
func byOf(f agentrun.Folder) string {
	if f.Title == "" {
		return "⚡ Autopilot"
	}

	return "⚡ " + f.Title
}

// runSessions records the agents' runs as sessions in the ledger (agentrun.Sessions).
type runSessions struct {
	ledger *sessions.Ledger
}

// Started records a run's session, working.
func (s runSessions) Started(ctx context.Context, ss agentrun.StartedSession) error {
	return s.ledger.Start(ctx, sessions.Record{
		ID: ss.ID, AgentSessionID: ss.AgentSessionID, Agent: ss.Agent, Profile: ss.Profile, ConfigDir: ss.ConfigDir,
		Prompt: ss.Prompt, PromptHead: ss.PromptHead, ItemID: ss.ItemID, Owner: ss.Repo.Owner, Name: ss.Repo.Repo,
		Kind: ss.Kind, Number: ss.Number, Title: ss.Title, URL: ss.URL, Dir: ss.Dir, ClonePath: ss.Clone,
		Worktree: ss.Worktree, WorktreeAction: sessions.WorktreeAction(ss.WorktreeAction), Permission: ss.Permission,
		State: sessions.StateWorking, LaunchedAt: ss.LaunchedAt, RunID: ss.RunID,
	})
}

// State records what a run says of its session.
func (s runSessions) State(ctx context.Context, id string, u agentrun.SessionUpdate) error {
	return s.ledger.RunState(ctx, id, sessions.RunUpdate{
		State: sessions.State(u.State), LastMessage: u.LastMessage, AgentSessionID: u.AgentSessionID,
	})
}

// runEvents tells the window a run's events changed, at most every runEventsEvery for a
// run, and once more after the last change.
type runEvents struct {
	sink event.Sink

	mu      sync.Mutex
	last    map[int64]time.Time
	pending map[int64]bool
}

// newRunEvents returns the throttle over sink.
func newRunEvents(sink event.Sink) *runEvents {
	return &runEvents{sink: event.Or(sink), last: map[int64]time.Time{}, pending: map[int64]bool{}}
}

// Changed tells the window of the run runID, now or once runEventsEvery has passed.
func (r *runEvents) Changed(runID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.pending[runID] {
		return
	}

	if wait := runEventsEvery - time.Since(r.last[runID]); wait > 0 {
		r.pending[runID] = true

		time.AfterFunc(wait, func() {
			r.mu.Lock()
			delete(r.pending, runID)
			r.last[runID] = time.Now()
			r.mu.Unlock()

			r.sink.Emit(event.AutopilotRun{RunID: runID})
		})

		return
	}

	r.last[runID] = time.Now()
	r.sink.Emit(event.AutopilotRun{RunID: runID})
}

// caffeinate returns macOS's caffeinate where it is, else "".
func caffeinate() string {
	if runtime.GOOS != "darwin" {
		return ""
	}

	if info, err := os.Stat(caffeinatePath); err != nil || info.IsDir() {
		return ""
	}

	return caffeinatePath
}

// runLimits reads which usage limit refused a background run, and when it resets, from the
// agent's own record of it (agentrun.LimitReader): Codex's rollout of the thread, whose
// token counts carry the plan's windows; Claude Code's stream says so itself.
type runLimits struct {
	codex codex.Limits
}

// Limit reads the limit a Codex thread's rollout last recorded.
func (l runLimits) Limit(_ context.Context, id agent.ID, configDir, agentSessionID string) (agentrun.Limit, bool) {
	if id != agent.Codex || agentSessionID == "" {
		return agentrun.Limit{}, false
	}

	got, ok := l.codex.Limit(configDir, agentSessionID)
	if !ok {
		return agentrun.Limit{}, false
	}

	status := "allowed"
	if got.Reached {
		status = "rejected"
	}

	return agentrun.Limit{Status: status, Type: got.Window, ResetsAt: got.ResetsAt}, true
}

// agentRunnerDeps are what the background runner is built over.
type agentRunnerDeps struct {
	runs    *scriptrun.Engine
	history *scriptrun.Service
	git     *gitread.Git
	ledger  *sessions.Ledger
	agents  *backgroundAgents
	engine  *engineWaker
	runner  execpath.Runner
}

// newAgentRunner builds the background runner over the run engine: each run recorded as a
// session, its end told to Autopilot's engine, its tokens so far written to its record,
// its events told to the window, the Mac kept awake while it goes when the user asks, and
// what it left in its worktree after SIGKILL ended.
func newAgentRunner(pool *sql.DB, opts Options, deps agentRunnerDeps, logger *slog.Logger) *agentrun.Runner {
	settings := autopilotPrefs{prefs: prefsstore.New(pool, opts.Clock)}

	return agentrun.New(agentrun.Config{
		Engine: deps.runs, Runs: deps.history, Git: deps.git, Sessions: runSessions{ledger: deps.ledger},
		Ended: deps.agents.Ended, Changed: newRunEvents(opts.Events).Changed,
		Usage: func(_ context.Context, u agentrun.Usage) {
			if e := deps.engine.engine; e != nil {
				e.RunUsage(u.RunID, autopilot.RunUsage{TokensIn: u.TokensIn, TokensOut: u.TokensOut, CacheRead: u.CacheRead})
			}
		},
		KeepAwake: func(ctx context.Context) bool {
			c, err := settings.Autopilot(ctx)

			return err == nil && c.KeepAwake
		},
		Caffeinate: caffeinate(),
		Sweeper:    agentrun.ProcSweeper{Lister: procs.System{Run: deps.runner}, Logger: logger},
		Limits:     runLimits{codex: codex.Limits{Home: opts.Home}},
		Clock:      opts.Clock, Logger: logger,
	})
}

// agentLogsFor reads how long the agents' runs that ended are kept: Autopilot's run logs
// setting, in days.
func agentLogsFor(prefs *prefsstore.Store) func(context.Context) time.Duration {
	settings := autopilotPrefs{prefs: prefs}

	return func(ctx context.Context) time.Duration {
		c, err := settings.Autopilot(ctx)
		if err != nil {
			return 0
		}

		return time.Duration(c.LogDays) * 24 * time.Hour
	}
}

// agentReview is the review a run's report held, in the engine's words; nil for none.
func agentReview(rv *agentrun.Review) *autopilot.AgentReview {
	if rv == nil {
		return nil
	}

	out := &autopilot.AgentReview{
		Verdict: autopilot.ReviewVerdict(rv.Verdict), Body: rv.Body, Comments: make([]autopilot.ReviewLine, 0, len(rv.Comments)),
	}

	for _, c := range rv.Comments {
		out.Comments = append(out.Comments, autopilot.ReviewLine{
			Path: c.Path, Line: c.Line, StartLine: c.StartLine, Side: c.Side, Body: c.Body,
		})
	}

	return out
}

// usageLimit is the usage limit that refused a run, in the engine's words; nil for none.
func usageLimit(l *agentrun.Limit) *autopilot.UsageLimit {
	if l == nil {
		return nil
	}

	out := &autopilot.UsageLimit{Type: l.Type}
	if l.ResetsAt > 0 {
		out.ResetsAt = time.Unix(l.ResetsAt, 0).UTC()
	}

	return out
}

// fallbackStep is step run on the fallback fb: that agent's profile, for every repository
// (the step's rows per org or repository are its own profile's), with the step's model and
// effort only while fb is the agent the step's own launch resolved to (primary), since
// another agent's catalog names others.
func fallbackStep(step spec.AgentStep, fb spec.ProfileRef, primary agent.ID) spec.AgentStep {
	out := step
	out.Agent, out.Profile, out.LaunchFor, out.Fallbacks = fb.Agent, fb.Profile, nil, nil

	if agent.ID(fb.Agent) != primary {
		out.Model, out.Effort = "", ""
	}

	return out
}

// cantRunHere reports a plan's failure that passes a fallback over: its agent can't keep
// the preset in a background run yet, can't run in the background at all, or isn't on
// this Mac.
func cantRunHere(err error) bool {
	return errors.Is(err, launcher.ErrFenceNotKept) || errors.Is(err, agent.ErrPresetNotKept) ||
		errors.Is(err, agent.ErrNoBackground) || errors.Is(err, execpath.ErrNotFound)
}

// fallbacks is a start's walk through its chain: the profiles resting, the agent its own
// launch resolved to, and what it passed over.
type fallbacks struct {
	resting map[string]autopilot.Resting
	primary agent.ID

	// rested are the resting profiles it passed over, and notHere why it passed over the
	// fallbacks that can't run here.
	rested  []autopilot.Resting
	notHere []string
}

// chainOf starts a walk through a chain with the profiles resting now.
func chainOf(resting []autopilot.Resting) *fallbacks {
	out := &fallbacks{resting: map[string]autopilot.Resting{}}

	for _, r := range resting {
		out.resting[restingKey(r.Agent, r.Profile)] = r
	}

	return out
}

// restingKey names an agent's profile among those resting, as the engine does.
func restingKey(agentID, profile string) string {
	return strings.ToLower(agentID) + "/" + strings.ToLower(profile)
}

// rests reports a launch on the agent's profile that rests out of usage, and notes it by
// the names the launch resolved to.
func (f *fallbacks) rests(id agent.ID, profile string) bool {
	r, ok := f.resting[restingKey(string(id), profile)]
	if ok {
		r.Agent, r.Profile = string(id), profile
		f.rested = append(f.rested, r)
	}

	return ok
}

// passOver notes a fallback that can't run here, and why.
func (f *fallbacks) passOver(err error) {
	f.notHere = append(f.notHere, textutil.Sentence(uierr.Message(err)))
}

// waits is the start's answer once nothing in its chain could start: it waits until the
// soonest rest ends, queued while only rests held it, else as not ready, naming why.
func (f *fallbacks) waits() autopilot.Result {
	var soonest time.Time

	for _, r := range f.rested {
		if soonest.IsZero() || r.Until.Before(soonest) {
			soonest = r.Until
		}
	}

	rested := autopilot.RestingWords(f.rested)

	if len(f.notHere) > 0 {
		words := "Not ready on this Mac: " + strings.Join(f.notHere, " ")
		if rested != "" {
			words += " And " + rested + "."
		}

		return autopilot.Result{Wait: autopilot.BlockNotReady, WaitWords: words, RetryAt: soonest}
	}

	return autopilot.Result{
		Wait: autopilot.BlockOutOfUsage, RetryAt: soonest, WaitWords: textutil.Sentence("Queued: " + rested),
	}
}
