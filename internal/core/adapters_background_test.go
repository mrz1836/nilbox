package core

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/agentrun"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher"
	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/sessions/observe/codex"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/worktrees"
)

// The failures the tests' fakes answer with.
var (
	errArunNoPrompt  = errors.New("the routine's prompt is gone")
	errArunNoProfile = errors.New("no profile named work")
	errArunBroke     = errors.New("broke")
	errArunLocked    = errors.New("the database is locked")
	errArunUnread    = errors.New("exit status 128")
)

func TestBackgroundAgentsNotBuiltYetStartNothingAndReadNothing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	b := &backgroundAgents{}

	_, err := b.Dispatch(ctx, autopilot.Job{})
	require.ErrorIs(t, err, autopilot.ErrNoAgentStep, "a step that runs no agent is refused")

	res, err := b.Dispatch(ctx, agentJob(1))
	require.NoError(t, err)
	assert.Equal(t, notReady("the background runner isn't ready."), res,
		"a step in the background waits until the runner is built")

	_, err = b.PreviewRun(ctx, agentJob(1).RunRequest())
	require.ErrorIs(t, err, autopilot.ErrNoAgentStep, "with nothing to plan with, nothing previews")

	require.ErrorIs(t, b.Stop(ctx, 1), errNoBackground)
	require.NoError(t, b.StopAll(ctx), "stopping every run stops none")

	live, err := b.Alive(ctx, 1)
	require.ErrorIs(t, err, errNoBackground)
	assert.Equal(t, autopilot.LiveUnknown, live, "nothing can say whether a run goes")

	_, ok := b.Progress(1)
	assert.False(t, ok)

	_, err = b.Events(ctx, 1, 0)
	require.ErrorIs(t, err, errNoBackground)

	require.NoError(t, b.ClearAgentHistory(ctx), "with no run engine, there is no history to clear")
	assert.Equal(t, []worktrees.RunFolder{}, b.RunFolders(ctx), "no folder is in use")
	assert.Equal(t, []launcher.RunningSession{}, b.RunsIn(ctx, t.TempDir()), "and no session works anywhere")

	// An end told before Autopilot's engine is built goes nowhere, and fails nothing.
	b.Ended(ctx, agentrun.End{Intent: agentrun.Intent{RunID: 1}})
	assert.Nil(t, b.engineNow())
}

func TestABackgroundPreviewIsTheLaunchersOwnPlan(t *testing.T) {
	t.Parallel()

	r := newAgentRig(t)

	got, err := r.agents.PreviewRun(t.Context(), agentJob(1).RunRequest())
	require.NoError(t, err)
	assert.Equal(t, "claude -p --restricted < prompt.md", got.Command, "the launcher's background preview, as it would start")
	assert.Equal(t, []string{}, got.Env)

	wantErr := errArunNoPrompt
	r.agents.requests = func(context.Context, autopilot.RunRequest) (launcher.LaunchRequest, error) {
		return launcher.LaunchRequest{}, wantErr
	}

	_, err = r.agents.PreviewRun(t.Context(), agentJob(1).RunRequest())
	require.ErrorIs(t, err, wantErr, "a step whose request can't be made previews nothing")

	r.agents.requests = nil
	_, err = r.agents.PreviewRun(t.Context(), agentJob(1).RunRequest())
	require.ErrorIs(t, err, errNoBackground, "before the routines' agents are built, no request is made")
}

// arunPreviewFails is the fake planner, whose preview fails with err.
type arunPreviewFails struct {
	*fakePlanner

	err error
}

// Preview fails.
func (f arunPreviewFails) Preview(context.Context, launcher.BackgroundRequest) (launcher.Preview, error) {
	return launcher.Preview{}, f.err
}

func TestABackgroundPreviewThatCantBePlannedSaysWhy(t *testing.T) {
	t.Parallel()

	r := newAgentRig(t)
	wantErr := errArunNoProfile
	r.agents.launch = arunPreviewFails{fakePlanner: r.planner, err: wantErr}

	got, err := r.agents.PreviewRun(t.Context(), agentJob(1).RunRequest())
	require.ErrorIs(t, err, wantErr)
	assert.Empty(t, got.Command, "nothing is previewed")
}

// arunTerminal is the terminal's runner, which records each run it was asked to stop.
type arunTerminal struct {
	fakeTerminal

	mu      sync.Mutex
	stopped []int64
}

// Stop records runID.
func (a *arunTerminal) Stop(_ context.Context, runID int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.stopped = append(a.stopped, runID)

	return nil
}

// stops returns each run asked to stop, in order.
func (a *arunTerminal) stops() []int64 {
	a.mu.Lock()
	defer a.mu.Unlock()

	return slices.Clone(a.stopped)
}

func TestStoppingARunTheBackgroundNeverStartedGoesToTheTerminal(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	r := newAgentRig(t)

	require.ErrorIs(t, r.agents.Stop(ctx, 99), agentrun.ErrNoRun, "with no terminal runner, an unknown run is an error")

	term := &arunTerminal{}
	r.agents.terminal = term

	require.NoError(t, r.agents.Stop(ctx, 99))
	assert.Equal(t, []int64{99}, term.stops(), "a run the background doesn't know is the terminal's to stop")

	live, err := r.agents.Alive(ctx, 99)
	require.NoError(t, err)
	assert.Equal(t, autopilot.LiveNever, live, "the history holds no run of it")

	require.NoError(t, r.agents.StopAll(ctx), "stopping every run with none going stops none")
	require.NoError(t, r.agents.ClearAgentHistory(ctx), "an empty history clears")
}

// arunDistrust is the fake planner, whose workspace trust fails with err.
type arunDistrust struct {
	*fakePlanner

	err error
}

// Trust fails.
func (f arunDistrust) Trust(context.Context, launcher.BackgroundPlan) error { return f.err }

func TestABackgroundStartThatFailsOnTheWayStartsNothing(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		setup func(r *agentRig)
		want  error
		words string
	}{
		"its request can't be made": {
			setup: func(r *agentRig) {
				r.agents.requests = func(context.Context, autopilot.RunRequest) (launcher.LaunchRequest, error) {
					return launcher.LaunchRequest{}, errArunBroke
				}
			},
			want: errArunBroke,
		},
		"its plan fails": {setup: func(r *agentRig) { r.planner.err = errArunBroke }, want: errArunBroke},
		"its worktree can't be made": {
			setup: func(r *agentRig) { r.maker.err = errArunBroke }, want: errArunBroke,
		},
		"its fence can't be worked out": {
			setup: func(r *agentRig) { r.planner.fenceErr = errArunBroke }, want: errArunBroke,
			words: "Its fence couldn't be set up: Broke.",
		},
		"its fence can't be written": {
			setup: func(r *agentRig) {
				bp := r.planner.plan(t, testutil.FakeRun{})
				bp.Folder = "relative/run-folder"
				r.planner.set(bp)
			},
			words: "Write the run's fence",
		},
		"its worktree can't be trusted": {
			setup: func(r *agentRig) { r.agents.launch = arunDistrust{fakePlanner: r.planner, err: errArunBroke} },
			want:  errArunBroke,
		},
		"the app is quitting": {
			setup: func(r *agentRig) { r.runs.StopAll() }, want: autopilot.ErrClosed,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := newAgentRig(t)
			r.planner.set(r.planner.plan(t, testutil.FakeRun{}))
			tc.setup(r)

			res, err := r.agents.Dispatch(t.Context(), agentJob(5))
			require.Error(t, err, "a failure on the way fails the start")

			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			}

			if tc.words != "" {
				assert.Contains(t, uierr.Message(err), tc.words, "the window says why")
			}

			_, held := errors.AsType[*waitError](err)
			assert.False(t, held, "a failure is never a wait")
			assert.False(t, res.Running, "nothing started")
			assert.Nil(t, res.Launched)
			assert.Empty(t, r.agents.RunFolders(t.Context()), "no folder is in use")
		})
	}
}

// arunTouch is one agent recorded in a worktree.
type arunTouch struct {
	clone    string
	worktree string
	agent    agent.ID
}

// arunTouches records the agents each start records in its worktree, and when, failing
// with err.
type arunTouches struct {
	mu     sync.Mutex
	err    error
	seen   []arunTouch
	stamps []time.Time
}

// Touch records the agent in the worktree.
func (a *arunTouches) Touch(_ context.Context, clonePath, worktree string, id agent.ID, at time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.seen = append(a.seen, arunTouch{clone: clonePath, worktree: worktree, agent: id})
	a.stamps = append(a.stamps, at)

	return a.err
}

// touched returns each agent recorded, in order.
func (a *arunTouches) touched() []arunTouch {
	a.mu.Lock()
	defer a.mu.Unlock()

	return slices.Clone(a.seen)
}

// stamped returns when each agent was recorded, in order.
func (a *arunTouches) stamped() []time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()

	return slices.Clone(a.stamps)
}

func TestCoreBuildsTheBackgroundAgentsOnTheAppsClock(t *testing.T) {
	t.Parallel()

	clk := testutil.NewClock(time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC))
	touches := &arunTouches{}

	b := newBackgroundAgents(Options{Home: "/srv/home", Clock: clk}, backgroundDeps{touch: touches}, nil)

	assert.Same(t, clk, b.clock, "the runs' worktree records are stamped by the app's clock")
	assert.Equal(t, "/srv/home", b.home)
	assert.Same(t, touches, b.touch)
}

func TestABackgroundStartStampsItsAgentFromTheClock(t *testing.T) {
	t.Parallel()

	t.Run("the clock it is given", func(t *testing.T) {
		t.Parallel()

		at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

		r := newAgentRig(t)
		r.planner.set(r.planner.plan(t, testutil.FakeRun{Stdout: agentFixture(t, "success-structured.jsonl")}))

		touches := &arunTouches{}
		r.agents.touch, r.agents.clock = touches, testutil.NewClock(at)

		_, err := r.agents.Dispatch(t.Context(), agentJob(6))
		require.NoError(t, err)

		assert.Equal(t, []time.Time{at}, touches.stamped(), "the worktree's record is stamped by the app's clock")
		assert.Equal(t, autopilot.RunDone, r.engine.end(t, 6).Outcome)
	})

	t.Run("no clock", func(t *testing.T) {
		t.Parallel()

		r := newAgentRig(t)
		r.planner.set(r.planner.plan(t, testutil.FakeRun{Stdout: agentFixture(t, "success-structured.jsonl")}))

		touches := &arunTouches{}
		r.agents.touch = touches

		before := time.Now()
		_, err := r.agents.Dispatch(t.Context(), agentJob(6))
		after := time.Now()

		require.NoError(t, err)

		stamps := touches.stamped()
		require.Len(t, stamps, 1)
		assert.False(t, stamps[0].Before(before) || stamps[0].After(after), "without one, the real clock stamps it")
		assert.Equal(t, autopilot.RunDone, r.engine.end(t, 6).Outcome)
	})
}

func TestABackgroundStartRecordsItsAgentInTheWorktree(t *testing.T) {
	t.Parallel()

	for name, fail := range map[string]error{"recorded": nil, "not recorded": errArunLocked} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer

			r := newAgentRig(t)
			bp := r.planner.plan(t, testutil.FakeRun{Stdout: agentFixture(t, "success-structured.jsonl")})
			bp.Clone.Dir = "/srv/projects/example-repo"
			r.planner.set(bp)

			touches := &arunTouches{err: fail}
			r.agents.touch = touches
			r.agents.logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

			res, err := r.agents.Dispatch(t.Context(), agentJob(6))
			require.NoError(t, err)
			assert.True(t, res.Running, "the run starts whether or not its agent could be recorded")

			assert.Equal(t, []arunTouch{{clone: "/srv/projects/example-repo", worktree: "pr-12", agent: agent.Claude}},
				touches.touched(), "the worktree's record names the agent working in it")
			assert.Equal(t, fail != nil, strings.Contains(logs.String(), "could not record the agent in its worktree"),
				"a record that failed is logged")

			assert.Equal(t, autopilot.RunDone, r.engine.end(t, 6).Outcome)
		})
	}
}

// arunChangesFail records nothing of what agent runs changed.
type arunChangesFail struct{}

// NoteAgentChanges fails.
func (arunChangesFail) NoteAgentChanges(context.Context, int64, []string, []string, int64) error {
	return errArunLocked
}

func TestARunsChangesThatCantBeNotedAreLogged(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	engine := &fakeRunEngine{ends: map[int64]autopilot.RunEnd{}}
	b := &backgroundAgents{
		changes: arunChangesFail{}, logger: slog.New(slog.NewTextHandler(&logs, nil)),
		engine: func() runEngine { return engine },
	}

	b.Ended(t.Context(), agentrun.End{
		Intent:  agentrun.Intent{RunID: 7, ItemID: 12, HeadBefore: agentHead},
		Outcome: agentrun.Outcome{Status: agentrun.StatusDone, Commit: strings.Repeat("b", 40), Changed: []string{"main.go"}},
	})

	assert.Contains(t, logs.String(), "could not record what an agent's run changed")
	assert.Equal(t, autopilot.RunDone, engine.end(t, 7).Outcome, "the engine still hears how the run ended")
}

func TestARunsPlanCarriesItsStepsFenceAndTheMatchsHead(t *testing.T) {
	t.Parallel()

	tokens, minutes := int64(250_000), 45
	step := spec.AgentStep{Fence: &spec.Fence{
		Tools: catalog.PresetReadOnly, Budget: &spec.Budget{Tokens: &tokens}, TimeoutMinutes: &minutes,
	}}
	bp := launcher.BackgroundPlan{
		Plan:      launcher.Plan{Agent: agent.Claude, Binary: "/opt/bin/claude", Args: []string{"-p"}},
		SessionID: "s1", Profile: workProfile, Command: "claude -p < prompt.md",
	}

	b := &backgroundAgents{}
	job := agentJob(3)
	job.Head = strings.ToUpper(agentHead)

	got := b.plan(job, step, bp, launcher.Worktree{Path: "/srv/wt/pr-12"}, nil)
	assert.Equal(t, agentHead, got.HeadBefore, "a worktree with no head read starts from the match's, in lower case")
	assert.Equal(t, int64(250_000), got.Fence.BudgetTokens, "a budget in tokens is the runner's to stop it at")
	assert.Zero(t, got.Fence.BudgetUSD)
	assert.Equal(t, 45*time.Minute, got.Fence.Timeout)
	assert.Equal(t, "Review · acme-org/example-repo #12", got.Title)
	assert.Equal(t, "autopilot:1", got.Source)
	assert.Equal(t, []string{"/opt/bin/claude", "-p"}, got.Argv)
	assert.Equal(t, repo.Name{Owner: boundOwner, Repo: boundRepo}, got.Session.Repo)
	assert.Equal(t, "pr-12", got.Session.Worktree)

	usd := 7.5
	step.Fence.Budget = &spec.Budget{USD: &usd}
	got = b.plan(job, step, bp, launcher.Worktree{Path: "/srv/wt/pr-12", Head: strings.Repeat("A", 40)}, nil)
	assert.InDelta(t, 7.5, got.Fence.BudgetUSD, 0, "a budget in dollars is the agent's own")
	assert.Zero(t, got.Fence.BudgetTokens)
	assert.Equal(t, strings.Repeat("a", 40), got.HeadBefore, "the worktree's own head wins")

	step.Fence = nil
	got = b.plan(job, step, bp, launcher.Worktree{Path: "/srv/wt/pr-12"}, nil)
	assert.InDelta(t, float64(catalog.DefaultRunCents)/100, got.Fence.BudgetUSD, 0, "with none, the catalog's default")
	assert.Equal(t, time.Duration(catalog.DefaultTimeoutMinutes)*time.Minute, got.Fence.Timeout)
}

// arunGit answers git as a worktree holding changes would: its filter drivers unset, and
// status listing each of changes, or failing with err. It runs no program.
func arunGit(err error, changes ...string) *gitread.Git {
	fake := &testutil.Runner{Script: func(_ context.Context, argv []string) ([]byte, []byte, error) {
		switch {
		case slices.Contains(argv, "--get-regexp"):
			return nil, nil, exitError{}
		case err != nil:
			return nil, []byte("fatal: not a git repository"), err
		case slices.Contains(argv, "status"):
			var out strings.Builder
			for _, c := range changes {
				out.WriteString(" M " + c + "\x00")
			}

			return []byte(out.String()), nil, nil
		}

		return nil, []byte("not run"), exitError{}
	}}

	return gitread.New(gitread.Config{
		Env: func() []string { return nil }, Runner: func([]string) execpath.Runner { return fake },
	})
}

func TestARunStartsOnlyInAWorktreeAsItWasPlanned(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	home := t.TempDir()
	dir := filepath.Join(home, "wt", "pr-12")
	require.NoError(t, os.MkdirAll(dir, 0o700))

	confirmed := &backgroundAgents{git: arunGit(nil, "notes.md"), home: home}
	require.NoError(t, confirmed.before(dir, 1)(ctx), "the one dirty file the user confirmed lets it start")

	changed := confirmed.before(dir, 0)(ctx)
	w, ok := errors.AsType[*waitError](changed)
	require.True(t, ok, "a worktree that changed is no failure: %v", changed)
	assert.True(t, w.res.Requeue, "the match is decided again, on what is there now")
	assert.Equal(t, "Its worktree changed as the run started.", w.res.WaitWords)

	unread := &backgroundAgents{git: arunGit(errArunUnread), home: home}
	require.ErrorIs(t, unread.before(dir, 0)(ctx), errArunUnread, "a worktree that can't be read doesn't start")

	ledger := sessions.NewLedger(sessions.Config{Store: sessions.NewStore(testutil.DB(t))})
	busy := &backgroundAgents{git: arunGit(nil), ledger: ledger, home: home}
	require.NoError(t, busy.before(dir, 0)(ctx), "a clean worktree no session works in lets it start")

	require.NoError(t, ledger.Start(ctx, sessions.Record{
		ID: "s1", Agent: agent.Claude, Profile: defaultProfile, Owner: boundOwner, Name: boundRepo, Kind: item.KindPR,
		Number: 12, Dir: dir, State: sessions.StateWorking, LaunchedAt: time.Now(),
	}))

	err := busy.before(dir, 0)(ctx)
	w, ok = errors.AsType[*waitError](err)
	require.True(t, ok, "a session working there holds the run: %v", err)
	assert.Equal(t, autopilot.BlockBusyWorktree, w.res.Wait)
	assert.Equal(t, "Queued: ~/wt/pr-12 is in use by a session; starts when it ends.", w.res.WaitWords)
}

func TestFailuresOnTheWayBecomeWhatTheEngineTakesThemAs(t *testing.T) {
	t.Parallel()

	other := errArunBroke

	require.ErrorIs(t, startError(scriptrun.ErrClosed), autopilot.ErrClosed,
		"once the run engine closed, the start stays queued for the next start")
	require.ErrorIs(t, startError(scriptrun.ErrClosed), scriptrun.ErrClosed)

	busy := startError(clonelock.ErrBusy)
	w, ok := errors.AsType[*waitError](busy)
	require.True(t, ok)
	assert.Equal(t, autopilot.BlockBusyWorktree, w.res.Wait, "a clone another job holds makes it queue")
	require.ErrorIs(t, busy, clonelock.ErrBusy, "and says why")
	assert.True(t, strings.HasPrefix(busy.Error(), "Queued: "), busy.Error())

	assert.Same(t, other, startError(other), "any other failure fails the start as it is")
	assert.Same(t, other, planError(other, agentJob(1)))
	assert.Same(t, other, worktreeError(other))

	plain := waitOn(autopilot.BlockHold, "held for the user", nil)
	assert.Equal(t, "Held for the user.", plain.Error(), "a wait with no cause says its words alone")
	require.NoError(t, errors.Unwrap(plain))
}

func TestARunsEndIsToldInTheEnginesWords(t *testing.T) {
	t.Parallel()

	head := strings.Repeat("C", 40)
	pushed := runEnd(agentrun.End{
		Intent: agentrun.Intent{RunID: 4},
		Outcome: agentrun.Outcome{
			Status: agentrun.StatusDone, Commit: head, Report: &agentrun.Report{Pushed: true, PushedCommit: head},
		},
	})
	assert.Equal(t, int64(4), pushed.RunID)
	assert.Equal(t, autopilot.RunDone, pushed.Outcome)
	assert.Equal(t, strings.ToLower(head), pushed.Commit, "a push of the run's own last commit is the commit it pushed")
	assert.Nil(t, pushed.Review, "a report with no review holds none")

	elsewhere := runEnd(agentrun.End{Outcome: agentrun.Outcome{
		Status: agentrun.StatusDone, Commit: head, Report: &agentrun.Report{Pushed: true, PushedCommit: strings.Repeat("d", 40)},
	}})
	assert.Empty(t, elsewhere.Commit, "a push of another commit than the run's own counts as none")

	for status, want := range map[agentrun.Status]autopilot.RunOutcome{
		agentrun.StatusInterrupted: autopilot.RunInterrupted,
		agentrun.StatusStopped:     autopilot.RunStopped,
		agentrun.StatusTimedOut:    autopilot.RunFailed,
		agentrun.StatusOutOfBudget: autopilot.RunFailed,
		"somethingNew":             autopilot.RunFailed,
	} {
		assert.Equal(t, want, runOutcome(status), "%s", status)
	}

	failed := runEnd(agentrun.End{Outcome: agentrun.Outcome{Status: agentrun.StatusTimedOut, Summary: "Timed out."}})
	assert.Equal(t, "Timed out.", failed.Error, "a failure says why")

	refused := runEnd(agentrun.End{Outcome: agentrun.Outcome{Status: agentrun.StatusOutOfUsage, Summary: "Out of usage."}})
	assert.Equal(t, autopilot.RunOutOfUsage, refused.Outcome)
	assert.Equal(t, "Out of usage.", refused.Error)
	assert.Nil(t, refused.Limit, "a refusal whose limit wasn't read names none")
}

func TestARunsFolderIsNamedForItsRoutine(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "⚡ Review · acme-org/example-repo #12", byOf(agentrun.Folder{Title: "Review · acme-org/example-repo #12"}))
	assert.Equal(t, "⚡ Autopilot", byOf(agentrun.Folder{}), "a run still starting has no title yet")
}

func TestARunsEventsReachTheWindowAtMostEveryQuarterSecond(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		events := &testutil.Events{}
		r := newRunEvents(events)

		r.Changed(1)
		r.Changed(1)
		r.Changed(1)
		r.Changed(2)

		assert.Equal(t, []event.Event{event.AutopilotRun{RunID: 1}, event.AutopilotRun{RunID: 2}}, events.All(),
			"each run's first change is told at once, and the next ones wait")

		time.Sleep(runEventsEvery)
		synctest.Wait()

		assert.Equal(t, []event.Event{
			event.AutopilotRun{RunID: 1}, event.AutopilotRun{RunID: 2}, event.AutopilotRun{RunID: 1},
		}, events.All(), "the changes that waited are told once, after the wait")

		r.Changed(1)
		assert.Equal(t, 3, events.Count(), "a change right after that waits again")

		time.Sleep(runEventsEvery)
		synctest.Wait()
		assert.Equal(t, 4, events.Count())
	})
}

func TestRunLimitsReadNothingOfACodexThreadWithNoRollout(t *testing.T) {
	t.Parallel()

	l := runLimits{codex: codex.Limits{Home: t.TempDir()}}

	_, ok := l.Limit(t.Context(), agent.Codex, "", "019a8c2e-0000-7000-8000-000000000001")
	assert.False(t, ok, "a thread whose rollout isn't there says no limit")
}

func TestTheAgentsRunLogsAreKeptAsLongAsAutopilotsSettingSays(t *testing.T) {
	t.Parallel()

	keep := agentLogsFor(prefsstore.New(testutil.DB(t), nil))
	assert.Equal(t, time.Duration(autopilotprefs.Defaults().LogDays)*24*time.Hour, keep(t.Context()),
		"the setting's default, before the user changes it")

	unread := agentLogsFor(prefsstore.New(testutil.ClosedDB(t), nil))
	assert.Zero(t, unread(t.Context()), "settings that can't be read keep nothing on their account")
}

func TestARunsSessionIsTheLedgersRecordOfIt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := sessions.NewStore(testutil.DB(t))
	s := runSessions{ledger: sessions.NewLedger(sessions.Config{Store: store})}
	at := time.Date(2026, 11, 9, 5, 11, 0, 0, time.UTC)

	require.NoError(t, s.Started(ctx, agentrun.StartedSession{
		Agent: agent.Claude, Profile: workProfile, ConfigDir: "/srv/home/.claude_work", Prompt: "Review", ItemID: 12,
		Repo: repo.Name{Owner: boundOwner, Repo: boundRepo}, Kind: item.KindPR, Number: 12, Title: "Bump zod",
		URL: "https://github.com/acme-org/example-repo/pull/12", Clone: "/srv/projects/example-repo",
		Worktree: "pr-12", WorktreeAction: string(launcher.WorktreeCreate), Permission: "read only",
		ID: "s1", RunID: 41, AgentSessionID: "s1", Dir: "/srv/wt/pr-12", PromptHead: "Review it", LaunchedAt: at,
	}))

	got, err := store.Get(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, sessions.StateWorking, got.State, "a run's session starts working")
	assert.Equal(t, int64(41), got.RunID, "and names its run, whose stream says its state from here")
	assert.Equal(t, agentJob(1).Item.Repo, got.Repo())
	assert.Equal(t, 12, got.Number)
	assert.Equal(t, int64(12), got.ItemID)
	assert.Equal(t, "/srv/wt/pr-12", got.Dir)
	assert.Equal(t, "/srv/projects/example-repo", got.ClonePath)
	assert.Equal(t, sessions.WorktreeCreate, got.WorktreeAction)
	assert.Equal(t, "read only", got.Permission)
	assert.Equal(t, "Review it", got.PromptHead)

	require.NoError(t, s.State(ctx, "s1", agentrun.SessionUpdate{State: agentrun.SessionEnded, LastMessage: "Looks good."}))

	got, err = store.Get(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, sessions.StateEnded, got.State, "what the run says of its session is recorded")
	assert.Equal(t, "Looks good.", got.LastMessage)
}

func TestABackgroundRunWorkingInAFolderHoldsAClickThereAsItsSession(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	r := newAgentRig(t)

	// The runner as core builds it, recording each run's session in the ledger.
	ledger := sessions.NewLedger(sessions.Config{Store: sessions.NewStore(testutil.DB(t))})
	r.agents.ledger = ledger
	r.agents.runner = newAgentRunner(testutil.DB(t), Options{Home: r.agents.home}, agentRunnerDeps{
		runs: r.runs, history: r.history, git: arunGit(nil), ledger: ledger, agents: r.agents, engine: &engineWaker{},
		runner: noPrograms(),
	}, slog.New(slog.DiscardHandler))

	plan := r.planner.plan(t, testutil.FakeRun{
		Stdout: agentLines(t, "success-structured.jsonl", 3), Sleep: time.Minute,
		OnInterrupt: &testutil.FakeInterrupt{Stdout: agentLastLine(t, "success-structured.jsonl")},
	})
	r.planner.set(plan)

	res, err := r.agents.Dispatch(ctx, agentJob(13))
	require.NoError(t, err)
	require.True(t, res.Running)

	var pid int

	require.Eventually(t, func() bool {
		folders := r.agents.RunFolders(ctx)
		if len(folders) == 1 {
			pid = folders[0].PID
		}

		return pid > 0
	}, waitAgent, 10*time.Millisecond)

	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })

	assert.Equal(t, []worktrees.RunFolder{{
		Dir: r.maker.dir, PID: pid, Agent: agent.Claude, By: "⚡ Review · acme-org/example-repo #12",
	}}, r.agents.RunFolders(ctx), "the Worktrees view names the routine's run working there")

	var held []launcher.RunningSession

	require.Eventually(t, func() bool {
		held = r.agents.RunsIn(ctx, r.maker.dir)

		return len(held) == 1 && held[0].SessionID != ""
	}, waitAgent, 10*time.Millisecond, "the run's session is recorded")

	assert.Equal(t, launcher.RunningSession{
		SessionID: plan.SessionID, Agent: agent.Claude, Doing: "is working in the background",
		Dir: r.maker.dir, Background: true, Repo: agentJob(13).Item.Repo, Number: 12, Worktree: "pr-12",
	}, held[0], "a click in its folder is held back by the run, named as its session")
	assert.Empty(t, r.agents.RunsIn(ctx, t.TempDir()), "a folder no run works in holds nothing")

	_, ok := r.agents.Progress(13)
	assert.True(t, ok, "how far it has got is read while it goes")

	// The fake answers SIGINT only once it is ready, and it writes its first lines after
	// that.
	require.Eventually(t, func() bool {
		page, readErr := r.agents.Events(ctx, 13, 0)

		return readErr == nil && len(page.Events) > 0
	}, waitAgent, 10*time.Millisecond, "the agent wrote its first lines")

	require.NoError(t, r.agents.Stop(ctx, 13))
	assert.Equal(t, autopilot.RunStopped, r.engine.end(t, 13).Outcome, "the user's Stop stops it")
}
