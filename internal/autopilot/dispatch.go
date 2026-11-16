package autopilot

import (
	"context"
	"errors"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
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
	// on, which a write is pinned to; "" for an issue.
	Item ItemRef
	Head string

	// Step is the step to run, and Index where it is among the match's steps.
	Step  spec.Step
	Index int

	// Attempt is how many runs the match started before this one.
	Attempt int

	// Manual reports a run the user started by hand.
	Manual bool

	// Budget is what the start reserved of the daily cap, in Unit; 0 when nothing.
	Budget float64
	Unit   string
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

	// Summary is a line of what happened, for the run's record.
	Summary string
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
// first refresh, offline, or signed out; Alive says whether a run the ledger records as
// going still goes, so the engine records one a crash ended as interrupted.
type Runner interface {
	Dispatcher

	// Stop stops the run with the ledger's ID, as its Stop does; the end comes through
	// RunEnded.
	Stop(ctx context.Context, runID int64) error

	// StopAll stops every agent run, and leaves the runner open for later starts.
	StopAll(ctx context.Context) error

	// Alive reports whether the run with the ledger's ID still goes.
	Alive(ctx context.Context, runID int64) (bool, error)
}

// RunEnd is how a run ended, as its runner tells the engine.
type RunEnd struct {
	// RunID is the ledger's run.
	RunID int64

	// Outcome is how it ended.
	Outcome RunOutcome

	// Commit is the commit the run pushed, "" for none, and Head the head commit after it.
	Commit string
	Head   string

	// Cost is what it reported spending, in dollars, nil when it reported none; the
	// tokens are counted apart.
	Cost         *float64
	TokensIn     int64
	TokensOut    int64
	CacheRead    int64
	Turns        int64
	Summary      string
	Error        string
	EndedAt      time.Time
	StoppedForMe bool
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
)

// ProblemSink tells the user of a problem, once, as the notifications do.
type ProblemSink interface {
	Problem(ctx context.Context, routine string, p Problem) error
}

// Outcome is what the inbox saw happen to an item: the user finished it, or it closed or
// left the lists. The engine reads the item's state again on its next cycle.
type Outcome struct {
	Kind string
	Ref  item.Ref
	At   time.Time
}
