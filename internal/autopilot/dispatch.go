package autopilot

import (
	"context"
	"errors"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/sensitive"
)

// The engine runs a match's steps through dispatchers, one per part of the app a step
// reaches, as the catalog names them (catalog.Dispatcher): the inbox's triage, the
// notices, the app's own GitHub writes, the agent runner, the follow of an agent's
// checks, merging, and posting an agent's review. Each is a narrow interface core
// registers (EngineConfig.Dispatchers, EngineConfig.Runner) over the service that does the
// work; the engine never reaches a service any other way. A step whose dispatcher isn't
// registered can't run, and the Service keeps a routine with such a step in Dry run.

// ErrClosed is returned by a dispatcher, or a runner, that closed for good, as the run
// engine does when the app quits: a start that races it fails with it, and its match stays
// queued for the next start.
var ErrClosed = errors.New("the runner closed")

// Job is one step of one match, as a dispatcher is asked to run it.
type Job struct {
	// MatchID and RunID name the match and, for a step recorded as a run (an agent's, or
	// a GitHub action's), the run, which the ledger recorded before the job was handed
	// over; RunID is 0 for a local step.
	MatchID int64
	RunID   int64

	// Routine is the routine's ID and name, and Mode how it acts.
	RoutineID   int64
	RoutineName string

	// Item is the item the match is about, and Head the head commit the match was decided
	// on, which a write is pinned to; "" for an issue. A GitHub action's is the head the
	// engine read right before it started, on which the cycle decided the match again.
	Item ItemRef
	Head string

	// Step is the step to run, and Index where it is among the match's steps.
	Step  spec.Step
	Index int

	// Action is which of a GitHub step's actions to send: each is its own run, recorded
	// before it is sent, so a quit never sends one twice. Accept is the warnings the user
	// accepted for it, as the sheet worded them (Result.Warning), "" for none.
	Action int
	Accept string

	// Attempt is how many runs the match started before this one.
	Attempt int

	// Manual reports a run the user started by hand.
	Manual bool

	// Budget is what the start reserved of the daily cap, in Unit; 0 when nothing.
	Budget float64
	Unit   string

	// Local is the item's local state as the cycle that ran the step read it, before it
	// decided (EngineItems.LocalStates): a triage step is a compare-and-set on it, so a
	// change of the user's in between wins. "" for a step that isn't local.
	Local string

	// MatchCreated is when the match was recorded, which tells it from a later match
	// given its ID once Clear history hands IDs out again.
	MatchCreated time.Time

	// Notices are the routine's own banner switches, over Settings → Notifications'; nil
	// follows the settings.
	Notices *spec.Notices

	// Reason says why the match matched, from its decision's trace, for a notice.
	Reason string

	// Before are the match's steps before this one and how each ended, for Tell me.
	Before []MatchStep

	// Burst reports a notice that comes with others at once (more than one in a cycle,
	// or a catch-up's), which the notices gather into one banner past three.
	Burst bool

	// Digest, for a Notify me a scheduled routine tells once with the list, is that list:
	// the step is one notice for every match it holds. nil for any other step.
	Digest *Digest

	// Trigger is the routine's trigger, which an agent's prompt names (.Trigger).
	Trigger catalog.Event

	// Confirmed are the Keys of the holds the user confirmed for this step (a file hold
	// on a commit, a dirty worktree), which an agent's launch goes ahead past; a hold with
	// any other key holds it again.
	Confirmed []string

	// Resting are the agents' profiles out of usage now, which an agent step's start
	// passes over for the next of its chain (fallback.go).
	Resting []Resting

	// AutoMerge has a Merge it turn auto-merge back on rather than merge now (merge.go),
	// and Review is the review a Post the agent's review posts (postreview.go).
	AutoMerge bool
	Review    *ReviewPost
}

// RunRequest is the agent step of a job, as its runner launches it, and Preview plans it.
func (j Job) RunRequest() RunRequest {
	out := RunRequest{
		ItemID: j.Item.ID, RoutineID: j.RoutineID, Routine: j.RoutineName, Trigger: j.Trigger, Attempt: j.Attempt + 1,
		Confirmed: append([]string{}, j.Confirmed...), Resting: append([]Resting{}, j.Resting...),
	}

	if j.Step.Agent != nil {
		out.Step = *j.Step.Agent
	}

	return out
}

// ItemRef names a match's item, as the ledger keeps a copy of it.
type ItemRef struct {
	ID     int64     `json:"id"`
	Kind   item.Kind `json:"kind"`
	Repo   string    `json:"repo"`
	Number int       `json:"number"`
	Title  string    `json:"title"`
	URL    string    `json:"url"`
}

// Ref returns the item's ref.
func (r ItemRef) Ref() item.Ref {
	name, _ := repo.Parse(r.Repo)

	return item.Ref{Repo: name, Number: r.Number, Kind: r.Kind}
}

// Result is how a dispatcher's step went.
type Result struct {
	// Running reports a step that goes on after Dispatch returns, as an agent's run
	// does: its end comes to the engine through Engine.RunEnded. Every other step is done
	// when Dispatch returns.
	Running bool

	// Outcome is how the step ended (the catalog's Only if outcomes, such as
	// catalog.OutcomePushed), "" when it says nothing a later step reads.
	Outcome catalog.Outcome

	// Sent reports a write GitHub took; false for one the item's sheet no longer offered
	// (NotOffered), or one with warnings (Warning), which sent nothing.
	Sent bool

	// NotOffered says the item's own sheet no longer offers the action, which closes the
	// match; Warning holds the warnings it offers it with, which wait for the user.
	NotOffered string
	Warning    string

	// PushedSince reports a GitHub action that sent nothing because the pull request was
	// pushed to since the head it was pinned to, or its sheet couldn't read it right before:
	// no try, and it is read and decided again.
	PushedSince bool

	// Held says why the write gate refused a GitHub action before anything was sent (the
	// app's own pace, the budget's reserve, or GitHub asking it to wait), and RetryIn when
	// the next may go: no try, and GitHub actions wait until then.
	Held    string
	RetryIn time.Duration

	// Summary is a line of what happened, for the run's record.
	Summary string

	// Wait says the step didn't start and the match waits on it: a hold only the user
	// confirms (BlockHold, BlockDirty, with Hold), no clone (BlockNoClone), or something
	// working where it would start (BlockBusyWorktree). WaitWords says it in the window's
	// words. The run counts as never started.
	Wait      Blocker
	WaitWords string
	Hold      *Hold

	// Requeue says the step didn't start and the match goes back to the queue, to be
	// decided again on a fresh read, as when the pull request's head moved as its worktree
	// was made. The run counts as never started.
	Requeue bool

	// RetryAt is when a start that waits (Wait) may be tried again, as when every profile
	// it may run with rests until a moment; zero tries it again a tick later.
	RetryAt time.Time

	// Launched is what an agent's launch started, for the run's record.
	Launched *Launched
}

// unsent reports a GitHub action its sheet or the write gate kept from being sent: no
// longer offered, offered with warnings, pushed since, or held.
func (r *Result) unsent() bool {
	return r.NotOffered != "" || r.Warning != "" || r.PushedSince || r.Held != ""
}

// Hold is a start held back for the user to confirm, as the launcher holds a click's
// launch back: for the pull request's files (agent files changed, or files not all
// checked) at a head commit, or a worktree with uncommitted changes. Only the user's
// Run confirms it, and only for exactly what they were shown (Key). It is the launcher's
// own hold, as the window's hold dialog shows it.
type Hold struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`

	// Head is the head commit the files were checked at, or the worktree is at.
	Head string `json:"head"`

	// Files are the agent files the pull request changes; Checked of Total files were
	// checked, and Error says why the check failed.
	Files   []sensitive.Finding `json:"files"`
	Checked int                 `json:"checked"`
	Total   int                 `json:"total"`
	Error   string              `json:"error"`

	// AtHead reports a session that would start in the pull request's worktree, which
	// loads its files at once.
	AtHead bool `json:"atHead"`

	// Dir is where the session would start, with ~ for home, Worktree its worktree's name,
	// and Dirty how many of its files have uncommitted changes.
	Dir      string `json:"dir"`
	Worktree string `json:"worktree"`
	Dirty    int    `json:"dirty"`
}

// Launched is what an agent's launch started: its session, as the sessions ledger records
// it, and what it runs with; or, for a script step, the run engine's run of the script.
type Launched struct {
	SessionID string
	Agent     string
	Profile   string
	Model     string
	Effort    string
	ClonePath string
	Worktree  string
	Command   string

	// ScriptRunID is the run in the run engine's history: for an agent's run in the
	// background, or a script step's run of one of the user's scripts, which its runner stops
	// and reads the end of; 0 for one in the terminal, which the run engine never runs.
	ScriptRunID int64
}

// Dispatcher runs one part's steps. Dispatch is called on a worker, never on the
// engine's own goroutine, and may reach the network; it must return when ctx ends. An
// error fails the step, and the match with it, but ErrClosed, which keeps the match
// queued.
type Dispatcher interface {
	Dispatch(ctx context.Context, job Job) (Result, error)
}

// Runner is the agent part's dispatcher: Dispatch starts a run and returns with Running,
// and the run's end comes later through Engine.RunEnded, however it ends. Stop and
// StopAll stop runs at once, never queued behind the engine, so a Stop works before the
// first refresh, offline, or signed out; Alive says what the runner knows of a run the
// ledger records as going, so the engine records one a crash ended as interrupted, and
// puts back in the queue one the runner never started.
type Runner interface {
	Dispatcher

	// Stop stops the run with the ledger's ID, as its Stop does; the end comes through
	// RunEnded.
	Stop(ctx context.Context, runID int64) error

	// StopAll stops every agent run, and leaves the runner open for later starts.
	StopAll(ctx context.Context) error

	// Alive says what the runner knows of the run with the ledger's ID. A run stays
	// LiveAlive until its RunEnded has been delivered (the call returned), so the engine
	// never takes a run that is ending for one that ended unseen. LiveNever is a positive
	// answer, and puts the start back in the queue to run again: the runner has no record
	// of the run ever starting, as for a start the quit cut off before it reached the
	// runner. It must hold across restarts, and never come from a history that forgets
	// runs: a runner that prunes its records answers LiveGone, or LiveUnknown, for a run
	// it no longer knows, so a run that started and crashed is never run again on its own.
	// An error is LiveUnknown.
	Alive(ctx context.Context, runID int64) (Liveness, error)
}

// Liveness is what a runner knows of a run the ledger records as going.
type Liveness string

// The answers about a run.
const (
	// LiveUnknown is a runner that can't say now.
	LiveUnknown Liveness = "unknown"

	// LiveAlive is a run that goes, or whose end hasn't been delivered yet.
	LiveAlive Liveness = "alive"

	// LiveGone is a run that started and ended, with no RunEnded delivered, as one that
	// ended while the app was closed.
	LiveGone Liveness = "gone"

	// LiveNever is a run the runner has no record of ever starting.
	LiveNever Liveness = "never"
)

// RunEnd is how a run ended, as its runner tells the engine.
type RunEnd struct {
	// RunID is the ledger's run.
	RunID int64

	// Outcome is how it ended.
	Outcome RunOutcome

	// Commit is the commit the run pushed, "" for none, and Head the head commit after it.
	Commit string
	Head   string

	// Cost is what it reported spending, in dollars, nil when it reported none, and
	// CostEstimated says it is the agent's estimate of what the API would have charged, as
	// for a run on the user's plan; the tokens are counted apart.
	Cost          *float64
	CostEstimated bool
	TokensIn      int64
	TokensOut     int64
	CacheRead     int64
	Turns         int64
	Summary       string
	Error         string
	EndedAt       time.Time
	StoppedForMe  bool

	// Limit is the usage limit that refused a run out of usage (RunOutOfUsage), nil for
	// any other end.
	Limit *UsageLimit

	// Review is the review a read-only run's report held, nil for none.
	Review *AgentReview
}

// RunOutcome is how a run ended.
type RunOutcome string

// The ways a run ends.
const (
	// RunDone is a run that finished cleanly.
	RunDone RunOutcome = "done"

	// RunFailed is one that failed, or ran out of its budget or time.
	RunFailed RunOutcome = "failed"

	// RunStopped is one the user stopped, or the engine stopped because its item closed.
	RunStopped RunOutcome = "stopped"

	// RunInterrupted is one the app's quit, or a crash, ended: it counts as no try, and
	// its match waits for the user.
	RunInterrupted RunOutcome = "interrupted"

	// RunNotSent is a GitHub action that sent nothing.
	RunNotSent RunOutcome = "not_sent"

	// RunOutOfUsage is one the provider refused for its profile's usage limit: it counts
	// as no try, its profile rests until the limit resets, and its step starts again on
	// the next profile of its chain (fallback.go).
	RunOutOfUsage RunOutcome = "out_of_usage"

	// RunNotStarted is one that never reached its runner, as a start the quit cut off, or
	// one the runner refused as it closed: it counts as no run and no try, against no
	// limit, and its match goes back to the queue. Only the engine records it; a runner
	// that tells it of a run it was handed is taken as telling RunInterrupted.
	RunNotStarted RunOutcome = "not_started"
)

// Holds is what a later part of the app says holds a start back, beyond the engine's own
// limits: this Mac's readiness for the routine, the agent's provider's status, a worktree
// something works in or with changes of the user's, the file check, and the write gate.
// Hold is asked on a worker, right before a start, after the item was read fresh; it
// returns what the start waits on, or "" to let it through.
type Holds interface {
	Hold(ctx context.Context, job Job) (Blocker, string, error)
}

// Problem is something wrong with a routine that the user is told once, when it starts:
// a search GitHub fails, or a routine that won't load.
type Problem struct {
	// Key names it: the routine, the kind, and for a search, the search.
	Key string `json:"key"`

	RoutineID int64       `json:"routineId"`
	Kind      ProblemKind `json:"kind"`

	// Detail says it in the window's words.
	Detail string `json:"detail"`

	// Since is when it was first seen, and Told whether the user was told.
	Since time.Time `json:"since"`
	Told  bool      `json:"told"`
}

// ProblemKind is a kind of problem.
type ProblemKind string

// The kinds of problem.
const (
	ProblemSearchFailed ProblemKind = "search_failed"
	ProblemWontLoad     ProblemKind = "wont_load"

	// ProblemNotReady is something a routine needs that this Mac doesn't have (ready.go).
	ProblemNotReady ProblemKind = "not_ready"
)

// ProblemSink tells the user of a problem, once, as the notifications do: an error leaves
// it untold, and it is told on a later cycle. NewProblemNotices is the one core registers.
type ProblemSink interface {
	Problem(ctx context.Context, p ProblemNotice) error
}

// Outcome is what the inbox saw happen to an item: the user finished it, or it closed or
// left the lists. The engine reads the item's state again on its next cycle.
type Outcome struct {
	Kind string
	Ref  item.Ref
	At   time.Time

	// Reversed names what a routine did to the item that the user's own triage took back
	// (a restore of what it snoozed or dismissed, a snooze over it, a dismissal of what it
	// brought back, an unpin of what it pinned), which leaves that routine hands off the
	// item; the zero Reversal otherwise.
	Reversed Reversal
}
