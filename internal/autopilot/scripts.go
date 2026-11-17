package autopilot

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Run one of my scripts (catalog.StepScript) runs one of the user's own Repo scripts in the
// item's clone, in the app, as Repo scripts runs it:
//
//   - It follows the routine's mode, as a GitHub action does, and is recorded as a run
//     (runner "script") before it is handed to the scripts part's runner
//     (EngineConfig.Scripts), which starts it and tells its end through Engine.RunEnded,
//     as an agent's runner does; Stop stops it there.
//   - Its limits are a GitHub action's: the routine's tries and runs a day, Pause, the
//     active hours, an outside contributor's item waiting for the user's OK, and one run
//     per item. It takes no agent's slot (Repo scripts has its own), and counts toward
//     neither the agents' caps nor the GitHub actions'.
//   - A script set to ask before each run (Ask before running) waits for the user's OK
//     each time it would run, whatever the routine's mode, a manual run's included
//     (scriptAsks). The OK is for exactly the script the user saw (ScriptKey: its look,
//     which names its name, command, place, and setting), so one changed since asks
//     again, and the runner checks it once more as it starts.
//   - A step names its script by its ID on this Mac and the name it was picked under. A
//     script whose name is no longer that one waits for the user to pick it again, and
//     Save and Describe say so: an ID a routine brought from another Mac, or a backup
//     restored, may name another script here, which never runs in its place.

// runnerScript is the runner of a script step: Repo scripts, in the app.
const runnerScript = "script"

// MyScripts reads the user's own Repo scripts, for the script step: the builder's choices
// (Service.MyScripts), the checks of a draft (Describe and Save), and the engine's Ask
// before running. Core adapts Repo scripts' own.
type MyScripts interface {
	MyScripts(ctx context.Context) ([]MyScript, error)
}

// MyScript is one of the user's own Repo scripts, as a routine runs it.
type MyScript struct {
	// ID names it on this Mac; Name and Command are as the user saved them.
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`

	// Asks reports one set to ask before each run: a routine runs it only once the user
	// says so, each time.
	Asks bool `json:"asks"`

	// Look names it as it is now, so an OK is for exactly the script the user saw.
	Look string `json:"look"`

	// Shows says which clones it shows in, and so runs in: "every clone", "Go
	// repositories", or the repositories it names.
	Shows string `json:"shows"`
}

// ScriptKey is the key of the user's OK to run a script that asks before each run, as it
// is now: its ID and its look. The engine records it in the step's Confirmed, and the
// runner runs the script only when the job carries it.
func ScriptKey(id int64, look string) string {
	return "script:" + strconv.FormatInt(id, 10) + ":" + look
}

// MyScripts returns the user's own Repo scripts, which a routine's Run one of my scripts
// picks from, in the user's order. It reads the database alone, and the list is never nil.
func (s *Service) MyScripts(ctx context.Context) ([]MyScript, error) {
	if s.myScripts == nil {
		return []MyScript{}, nil
	}

	got, err := s.myScripts.MyScripts(ctx)
	if err != nil {
		return []MyScript{}, uierr.New("Your scripts couldn't be read.", err)
	}

	return nonNil(got), nil
}

// scriptProblems returns the problems of the scripts sp's steps name: one of the user's
// scripts no longer saved, or saved under another name than the step's. Without the
// scripts to read, or with scripts that can't be read (logged), it finds none here; the
// runner refuses a script that is gone as it starts, and the engine holds one renamed.
func (s *Service) scriptProblems(ctx context.Context, sp spec.Spec) []spec.Problem {
	if s.myScripts == nil || !hasStep(sp, catalog.StepScript) {
		return nil
	}

	mine, err := s.myScripts.MyScripts(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "could not read the user's scripts to check a routine's", "error", err)

		return nil
	}

	var out []spec.Problem

	for i, st := range sp.Steps {
		if st.Script == nil {
			continue
		}

		at := slices.IndexFunc(mine, func(m MyScript) bool { return m.ID == st.Script.ID })

		var message string

		switch {
		case at < 0:
			message = fmt.Sprintf("%q is no longer one of your scripts: pick another", st.Script.Name)
		case mine[at].Name != st.Script.Name:
			message = renamedWords(st.Script.Name, mine[at].Name)
		default:
			continue
		}

		out = append(out, spec.Problem{Part: spec.PartSteps, Field: fmt.Sprintf("[%d].script.id", i), Message: message})
	}

	return out
}

// renamedWords says that the script a step picked as picked is now saved as name, so it
// waits to be picked again.
func renamedWords(picked, name string) string {
	return fmt.Sprintf("The script picked as %q is now %q: pick it again", picked, name)
}

// hasStep reports a spec with a step of kind.
func hasStep(sp spec.Spec, kind catalog.StepKind) bool {
	return slices.ContainsFunc(sp.Steps, func(st spec.Step) bool { return st.Kind() == kind })
}

// scriptsOf reads the user's scripts once a cycle, by ID, when some routine runs one:
// what the Ask before running rule reads. A read that fails is kept as failed, so every
// script step waits rather than run one that may ask.
func (p *pass) scriptsOf(ctx context.Context) {
	if p.e.svc.myScripts == nil || !slices.ContainsFunc(p.routines, func(r *routineRun) bool {
		return hasStep(r.spec, catalog.StepScript)
	}) {
		return
	}

	mine, err := p.e.svc.myScripts.MyScripts(ctx)
	if err != nil {
		p.e.logger.WarnContext(ctx, "Autopilot could not read the user's scripts", "error", err)

		p.scriptsFailed = true

		return
	}

	p.scripts = make(map[int64]MyScript, len(mine))
	for _, m := range mine {
		p.scripts[m.ID] = m
	}
}

// scriptAsks holds m's script step while its script asks before each run, until the user's
// OK for exactly that script on this step, whatever the routine's mode; while the user's
// scripts can't be read; and while its script is saved under another name than the one it
// was picked under, until the user picks it again. A script that is gone is let through:
// its runner refuses it, and the step fails saying so.
func (p *pass) scriptAsks(sr *stepRun) verdict {
	if sr.Step.Script == nil {
		return verdict{}
	}

	if p.scriptsFailed {
		return wait(BlockNotReady, "Your scripts couldn't be read; it tries again soon.")
	}

	sc, ok := p.scripts[sr.Step.Script.ID]

	switch {
	case !ok:
		return verdict{}
	case sc.Name != sr.Step.Script.Name:
		return wait(BlockNotReady, renamedWords(sr.Step.Script.Name, sc.Name)+" in the routine.")
	case !sc.Asks:
		return verdict{}
	}

	key := ScriptKey(sc.ID, sc.Look)
	if slices.Contains(sr.Confirmed, key) {
		return verdict{}
	}

	sr.Hold = &Hold{Key: key, Reason: HoldScriptAsks}

	return wait(BlockScriptAsks, scriptAsksWords(sc.Name))
}

// HoldScriptAsks is a Hold's reason for a script that asks before each run.
const HoldScriptAsks = "script_asks"

// scriptAsksWords says what a script that asks before each run waits for.
func scriptAsksWords(name string) string {
	return fmt.Sprintf("%q asks before each run: run it?", name)
}

// ScriptAsks is the Result a scripts runner returns for a script that asks before each run
// that the job's Confirmed doesn't carry the OK of, as it is now: it waits for the user,
// as the engine's own check does.
func ScriptAsks(sc MyScript) Result {
	return Result{
		Wait: BlockScriptAsks, WaitWords: scriptAsksWords(sc.Name),
		Hold: &Hold{Key: ScriptKey(sc.ID, sc.Look), Reason: HoldScriptAsks},
	}
}

// ScriptRun is a script step's run as the ledger records it, for the scripts' runner: the
// run engine's run it started (0 while none did), and whether it ended.
type ScriptRun struct {
	ScriptRunID int64
	Ended       bool
}

// ScriptRunOf returns the script step's run runID as the ledger records it, for the
// scripts' runner, which stops and follows the run engine's run it started.
func (e *Engine) ScriptRunOf(ctx context.Context, runID int64) (ScriptRun, error) {
	row, err := e.ledger.q.GetRunScript(ctx, runID)
	if err != nil {
		return ScriptRun{}, fmt.Errorf("read run %d: %w", runID, err)
	}

	return ScriptRun{ScriptRunID: row.ScriptRunID, Ended: row.Status == runEnded}, nil
}

// ScriptRunsGoing returns the script steps' runs going, by the ledger's ID, with the run
// engine's run each started (0 while none did): what Stop all stops.
func (e *Engine) ScriptRunsGoing(ctx context.Context) (map[int64]int64, error) {
	rows, err := e.ledger.q.ListActiveScriptRuns(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the script runs going: %w", err)
	}

	out := make(map[int64]int64, len(rows))
	for _, r := range rows {
		out[r.ID] = r.ScriptRunID
	}

	return out, nil
}

// isAgentRunner reports a run of an agent: in the background or the terminal, never a
// GitHub action or a script.
func isAgentRunner(runner string) bool {
	return runner != runnerGitHub && runner != runnerScript
}

// runnerFor returns the runner of runs recorded under name: the scripts', or the agents'.
func (e *Engine) runnerFor(name string) Runner {
	if name == runnerScript {
		return e.scripts
	}

	return e.runner
}
