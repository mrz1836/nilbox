// Package scriptrun runs the commands of the Scripts tab, a release, a hooks action, a
// checks run, a script, or a clone's fast-forward pull, and a tool's own updater, in the
// app or in the user's terminal, and keeps their history.
//
// The Engine starts a run and follows it to its end. A run in the app starts in its own
// process group and writes straight into its log file, never through a pipe, so a crash of
// the app can't cut its output short; the engine tails the file every 100 ms and feeds what
// is new to the run's output reader. A run in the terminal is opened through a
// TerminalOpener, and followed through the files its script writes: the PID of the shell
// that runs it, its log, and its exit status.
//
// Every run holds its clone's lock (clonelock) from its start to its end, and a busy clone
// fails a run at once, naming what holds it: runs never queue on a clone. Up to four runs
// go in the app at once; a release never waits for a free slot, and any other run does.
//
// It owns the script_runs table: one row per run, written as it starts and as it ends. The
// newest 200 runs and 512 MB of logs are kept, the oldest finished runs going first, and a
// finished log over 16 MB keeps only its first 4 MB and its last 12 MB. When the app starts
// again, the runs it left behind are reconciled: one still going is followed again, and one
// that is gone is marked interrupted. When the app quits, every run in the app is stopped.
//
// The Service is what the window binds: the history, a run's output in pages, Stop, Run
// again, and the quit guard's answer. The Engine itself is not bound.
package scriptrun

import (
	"context"
	"errors"
	"io"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/repo"
)

// Kind is what a run does.
type Kind string

// The kinds of run.
const (
	// KindRelease is a release: magex tags the newest commit and pushes the tag.
	KindRelease Kind = "release"

	// KindHooks installs, updates, or removes a repository's go-pre-commit hooks.
	KindHooks Kind = "hooks"

	// KindChecks runs go-pre-commit's checks.
	KindChecks Kind = "checks"

	// KindScript is a script: the user's own, a repository's task, or a magex target.
	KindScript Kind = "script"

	// KindUpdate is a tool updating itself: its own updater, such as magex update:install,
	// replaces the copy it runs as with its latest release. It runs in the copy's folder,
	// never a clone, and doesn't count as using the tool (InUse): it is the update.
	KindUpdate Kind = "update"

	// KindPull fast-forwards a clone's branch to origin's, on the user's click on Repo
	// scripts: git's fast-forward-only pull, and nothing else.
	KindPull Kind = "gitPull"
)

// Kinds returns every kind.
func Kinds() []Kind {
	return []Kind{KindRelease, KindHooks, KindChecks, KindScript, KindUpdate, KindPull}
}

// Valid reports whether k is one of Kinds.
func (k Kind) Valid() bool {
	return slices.Contains(Kinds(), k)
}

// Timeout returns how long a run of the kind may take before it is stopped, or 0 for no
// limit: five minutes for a release, one for a hooks action, thirty for checks, ten for a
// tool updating itself (its download and swap, which the tools bound at six), two for a
// pull, and none for a script, which the user stops.
func (k Kind) Timeout() time.Duration {
	switch k {
	case KindRelease:
		return 5 * time.Minute
	case KindHooks:
		return time.Minute
	case KindChecks:
		return 30 * time.Minute
	case KindUpdate:
		return 10 * time.Minute
	case KindPull:
		return 2 * time.Minute
	case KindScript:
		return 0
	default:
		return 0
	}
}

// lockKind returns the kind of job a run of the kind holds its folder as.
func (k Kind) lockKind() clonelock.Kind {
	switch k {
	case KindRelease:
		return clonelock.KindRelease
	case KindHooks:
		return clonelock.KindHooks
	case KindChecks:
		return clonelock.KindChecks
	case KindUpdate:
		return clonelock.KindUpdate
	case KindPull:
		return clonelock.KindPull
	case KindScript:
		return clonelock.KindScript
	default:
		return clonelock.KindScript
	}
}

// Status is where a run is.
type Status string

// The statuses of a run.
const (
	// StatusRunning is a run going now.
	StatusRunning Status = "running"

	// StatusSucceeded is a run that ended well: its program exited 0, or its kind
	// verified it.
	StatusSucceeded Status = "succeeded"

	// StatusFailed is a run that ended badly, couldn't start, or ran past its limit.
	StatusFailed Status = "failed"

	// StatusStopped is a run the user stopped, or that the app stopped as it quit.
	StatusStopped Status = "stopped"

	// StatusInterrupted is a run that ended while the app wasn't watching: the app quit
	// or crashed with it inside, or its terminal closed.
	StatusInterrupted Status = "interrupted"

	// StatusOrphaned is a run the app started before it restarted, still going. The app
	// follows it again, and it can be stopped.
	StatusOrphaned Status = "orphaned"
)

// Statuses returns every status.
func Statuses() []Status {
	return []Status{
		StatusRunning, StatusSucceeded, StatusFailed, StatusStopped, StatusInterrupted, StatusOrphaned,
	}
}

// Valid reports whether s is one of Statuses.
func (s Status) Valid() bool {
	return slices.Contains(Statuses(), s)
}

// Live reports a run that has not ended: running or orphaned.
func (s Status) Live() bool {
	return s == StatusRunning || s == StatusOrphaned
}

// RunsIn is where a run runs: the setting's own type, so the user's choice goes straight
// into a Spec.
type RunsIn = integrationprefs.RunsIn

// The places a run runs.
const (
	// RunsInApp is a run the app starts and follows itself.
	RunsInApp = integrationprefs.RunsInApp

	// RunsInTerminal is a run in the user's terminal app, which the app follows.
	RunsInTerminal = integrationprefs.RunsInTerminal
)

// MaxUnrecognized caps the lines a run's result keeps that its reader didn't recognize;
// it still counts them all.
const MaxUnrecognized = 200

var (
	// ErrInvalidSpec is returned for a run missing what starting it needs.
	ErrInvalidSpec = errors.New("invalid run")

	// ErrNotFound is returned for a run the history no longer holds.
	ErrNotFound = errors.New("the run is no longer recorded")

	// ErrNoSlot is returned for a release when every slot for a run in the app is busy.
	ErrNoSlot = errors.New("every run slot is busy")

	// ErrNotRunning is returned when stopping a run that has ended.
	ErrNotRunning = errors.New("the run has ended")

	// ErrNoStop is returned when stopping a run in the terminal, which stops there.
	ErrNoStop = errors.New("a run in the terminal can't be stopped from the app")

	// ErrStopRefused is returned when stopping a run whose kind says it can't be stopped
	// now (Progress.StopRefused), as a release while magex pulls.
	ErrStopRefused = errors.New("the run can't be stopped now")

	// ErrNoRerun is returned for Run again on a release, or on a kind with no Rerunner.
	ErrNoRerun = errors.New("the run can't run again")

	// ErrNoTerminal is returned for a run in the terminal where none can be opened.
	ErrNoTerminal = errors.New("runs in the terminal aren't available")

	// ErrClosed is returned once the engine has stopped every run, as the app quits.
	ErrClosed = errors.New("the runner has stopped")

	// ErrNoFolder is returned for Open in terminal on a run whose folder is gone.
	ErrNoFolder = errors.New("the run's folder is gone")

	// ErrNoLog is returned for Show the log file on a run whose log is gone.
	ErrNoLog = errors.New("the run's log is gone")

	// ErrNoReveal is returned where no file manager can show a file.
	ErrNoReveal = errors.New("files can't be shown here")
)

// Spec is a run to start.
type Spec struct {
	// Kind is what the run does.
	Kind Kind

	// Title is what the window calls it: "Release v1.4.3", "Checks · all files".
	Title string

	// Source says where its command came from, as its kind names it: a script's ID, a
	// task file, a magex target. It may be "".
	Source string

	// Repo is the clone's repository on GitHub, or the zero Name for a folder that isn't
	// one.
	Repo repo.Name

	// Dir is the clone's absolute folder: the run runs there, and holds its lock. A tool
	// updating itself runs in, and holds, its copy's folder.
	Dir string

	// Argv is the command, its program's absolute path first. A shell command runs as
	// [shell, -c, command], exactly as written: the engine never adds a shell. It may be
	// empty when Before works it out.
	Argv []string

	// Env is the environment. For a run in the app it is the whole environment, as in
	// exec.Cmd.Env, nil being the app's own. For a run in the terminal it is only what the
	// script exports, such as a tool's quiet settings, never the login shell's environment,
	// which the terminal's own shell sets up.
	Env []string

	// Command is the display form of Argv; "" writes Argv out, quoted as a shell would, with
	// its program's folder under home from ~. Argv itself stays absolute.
	Command string

	// RunsIn is where it runs; "" is the app.
	RunsIn RunsIn

	// Timeout is how long it may run before it is stopped: 0 is the kind's Timeout, and a
	// negative one is no limit. A run in the terminal has none.
	Timeout time.Duration

	// Tool is the integration it uses, such as "mage-x", which InUse reports, or "". A
	// tool updating itself names the tool it updates, which InUse leaves out.
	Tool string

	// Intent is what it sets out to do, written into its result as it starts, so a run
	// cut short can still be checked. It must encode as JSON; nil is none.
	Intent any

	// Output is the run's output reader: it is fed every byte of the log in order, as it
	// arrives, from one goroutine at a time. nil reads nothing.
	Output io.Writer

	// Progress says how far the run has got, and whether Stop is let through now, as its
	// kind works it out from what Output has read. The engine asks it as the window reads
	// the run, at each Stop, and after each read of the log, telling Config.Progressed when
	// the answer changed. It is called from any goroutine, must return promptly, and must
	// never call back into the engine. nil has no steps, and lets Stop through.
	Progress func() Progress

	// Before runs under the clone's lock and a slot, before anything is recorded or
	// started; an error ends the start there. It may return the command to run, worked out
	// under the lock, which replaces Argv; nil keeps Argv. A release gathers its facts again
	// in it and decides there what magex runs.
	Before func(ctx context.Context) ([]string, error)

	// Recorded is called once the run's end is recorded, with the outcome recorded, before
	// the run lets go of its clone: what is told of an outcome is told only once it is
	// kept, so a record that failed, which the next start settles again, tells nothing
	// twice. It is never called for an outcome left Unsettled, nor when the record failed.
	// It runs on the goroutine that recorded the run, and must return promptly. nil tells
	// nothing.
	Recorded func(ctx context.Context, id int64, out Outcome)

	// Released is called once the run has let go of its clone and its slot, after its end
	// was recorded, or its record failed: what it tells sees the clone free and the run
	// ended, as a page that reads the clone again after a run must. It isn't called for a run
	// left going as the app quit. It runs on the goroutine that recorded the run, must return
	// promptly, and must never call back into the engine. nil tells nothing.
	Released func(id int64)

	// Finish works out the run's outcome once it has ended and Output has read all of
	// it. It is given how the run ended and the outcome the engine worked out from that,
	// and returns the one to record. nil records the engine's. Its context ends when the
	// app quits and can wait no longer, and Finish must then return at once: with an
	// Unsettled outcome, when what it needed to know couldn't be learned in time.
	Finish func(ctx context.Context, end Ending, out Outcome) Outcome
}

// Progress is how far a run going now has got, as its kind reads its output, and whether
// the user may stop it now.
type Progress struct {
	// Step is the last step the run reached, and Working the one under way, in its kind's
	// own names, as a release's "onBranch" or "tagCreated"; "" for none yet, or for a kind
	// without steps.
	Step    string `json:"step"`
	Working string `json:"working"`

	// StopRefused, when set, says why Stop is off now, as "Stopping now could leave a
	// rebase in progress.": Stop refuses with it.
	StopRefused string `json:"stopRefused"`

	// StopConfirm, when set, is what the window asks before it stops the run, since
	// stopping now leaves something behind: "Stopping now can leave v1.4.3 on this Mac but
	// not on GitHub, and your clone on master."
	StopConfirm string `json:"stopConfirm"`
}

// Ending is how a run ended, as the engine saw it.
type Ending struct {
	// ExitCode is the program's exit status, or nil when it is unknown.
	ExitCode *int

	// Stopped reports that the user stopped it, TimedOut that it ran past its limit and
	// was stopped, and Quit that the app stopped it as it quit.
	Stopped  bool
	TimedOut bool
	Quit     bool

	// Interrupted, when set, says why its end couldn't be seen: "The terminal closed while
	// this was running."
	Interrupted string
}

// Outcome is what a run's record says about how it ended.
type Outcome struct {
	// Status is how it ended, and Summary says so in a sentence.
	Status  Status
	Summary string

	// Warnings are the tool's warning lines.
	Warnings []string

	// Unrecognized are the lines the reader didn't recognize, of which the first
	// MaxUnrecognized are kept, and UnrecognizedCount counts them all.
	Unrecognized      []string
	UnrecognizedCount int

	// Detail is the kind's own result, encoded as JSON.
	Detail any

	// Unsettled, from a Finish whose context ended first, records nothing: the run stays
	// recorded as going, and the next start of the app settles it again, through its
	// kind's Verifier. Every other field is then ignored.
	Unsettled bool
}

// Result is a run's result, as recorded.
type Result struct {
	// Tool is the integration it used, or "".
	Tool string `json:"tool"`

	// Intent is what it set out to do, as its kind wrote it at the start, or nil.
	Intent any `json:"intent"`

	// Warnings, Unrecognized, and UnrecognizedCount are the Outcome's.
	Warnings          []string `json:"warnings"`
	Unrecognized      []string `json:"unrecognized"`
	UnrecognizedCount int      `json:"unrecognizedCount"`

	// Outcome is the kind's own result, or nil.
	Outcome any `json:"outcome"`
}

// Record is one run as the history holds it.
type Record struct {
	ID      int64
	Kind    Kind
	Title   string
	Source  string
	Repo    repo.Name
	Dir     string
	Argv    []string
	Command string
	RunsIn  RunsIn
	Status  Status

	// ExitCode is the program's exit status, or nil when unknown or still running.
	ExitCode *int

	// PID and PGID are its process and process group once known, or 0, and
	// ProcStartedAt is when that process started, or zero.
	PID           int
	PGID          int
	ProcStartedAt time.Time

	Summary  string
	Result   Result
	LogBytes int64

	// SeenAt is when the user saw it, StartedAt when it started, and FinishedAt when it
	// ended; SeenAt and FinishedAt are zero until then.
	SeenAt     time.Time
	StartedAt  time.Time
	FinishedAt time.Time
}

// TerminalRun is a run for the terminal to open: a script that changes into Dir, records
// its shell's PID in PIDFile, runs Argv with its output recorded in Log, and writes its
// exit status to ExitFile, under a temporary name first and then renamed, so it is never
// read half written.
type TerminalRun struct {
	// Name names the script, "run-<id>".
	Name string

	// Dir is the folder it runs in, Argv the command, and Env what the script exports.
	Dir  string
	Argv []string
	Env  []string

	// Log, PIDFile, and ExitFile are the files the engine follows it through.
	Log      string
	PIDFile  string
	ExitFile string
}

// TerminalOpener opens a run in the user's terminal app, or the terminal in a run's folder
// with nothing run there, for Open in terminal. Core adapts the launcher's terminal to it;
// where no terminal can be opened, it returns an error.
type TerminalOpener interface {
	OpenRun(ctx context.Context, run TerminalRun) error
	OpenFolder(ctx context.Context, name, dir string) error
}

// Revealer shows a file in the user's file manager: Finder on macOS. Core adapts it.
type Revealer interface {
	Reveal(ctx context.Context, path string) error
}

// Rerunner works a run's command out again from its source, for Run again: the script's
// current text, the task file as it is now, the same hooks action, or the same magex
// target. It never replays the recorded argv.
type Rerunner interface {
	Rerun(ctx context.Context, run Record) (Spec, error)
}

// Verifier stands in, for the runs of its kind from before the app restarted, for what
// their Spec gave them: an output reader and a Finish, closures a restart loses. As the
// engine picks such a run up, orphaned or interrupted, it asks Verify for them again,
// built from the run's record alone, its intent above all. It then feeds the reader the
// run's whole log, from its start, and calls Finish when the run ends, as for a run it
// started: a release is verified that way, wherever its tag and its clone were left.
type Verifier interface {
	Verify(run Record) Verification
}

// Verification is what a Verifier gives a run from before the app restarted: its output
// reader, its progress, its Finish, and its Recorded, as a Spec's. Any may be nil, as in a
// Spec.
type Verification struct {
	Output   io.Writer
	Progress func() Progress
	Finish   func(ctx context.Context, end Ending, out Outcome) Outcome
	Recorded func(ctx context.Context, id int64, out Outcome)
}
