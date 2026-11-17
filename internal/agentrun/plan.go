// Package agentrun runs a coding agent in the background, for Autopilot: one
// scriptrun.Spec of KindAgent from a finished plan, each agent's stream parser into the one
// event model (runevent), how a run came out, the Verifier that picks a run up after a
// restart, and the run view's reads.
//
// It takes its own plain input, Plan, which core builds from the launcher's background
// plan: the argv, the environment, the worktree, the prompt, and the fence. It never
// plans a launch itself, so a background run has no more access than the launcher gave
// it. It never imports Autopilot, the launcher, the inbox, the settings, the desktop
// shell, or core.
package agentrun

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// promptFile is the file of a run's own folder its prompt is read from, beside its fence's
// files (fence.WriteRun's: the policy, the agent's settings, and the sandbox check's
// folder), which core writes before the run starts.
const promptFile = "prompt.md"

// ErrInvalidPlan is returned for a plan a run can't start from.
var ErrInvalidPlan = errors.New("invalid background run")

// Fence is what holds a background run in, as its routine's step set it.
type Fence struct {
	// Preset is the tools preset, in the catalog's words ("readOnly").
	Preset string `json:"preset"`

	// Timeout is its time limit, on Go's timers, which don't count a sleeping Mac.
	Timeout time.Duration `json:"timeout"`

	// BudgetUSD is its budget in dollars, which the agent keeps to itself, or 0; and
	// BudgetTokens its budget in tokens, which the runner stops it at, or 0.
	BudgetUSD    float64 `json:"budgetUsd"`
	BudgetTokens int64   `json:"budgetTokens"`

	// ReadOnly reports the preset that changes nothing, whose final message is its notes.
	ReadOnly bool `json:"readOnly"`
}

// Session is the session a background run is recorded as, so the item's row and its
// panel follow it as they follow any session.
type Session struct {
	Agent     agent.ID
	Profile   string
	ConfigDir string

	// Prompt is the prompt's nickname.
	Prompt string

	ItemID int64
	Repo   repo.Name
	Kind   item.Kind
	Number int
	Title  string
	URL    string

	// Clone and Worktree are the clone and the item's worktree the run works in.
	Clone    string
	Worktree string

	// WorktreeAction is what making the worktree did, and Permission summarizes the
	// fence's access.
	WorktreeAction string
	Permission     string
}

// Plan is a background run, worked out and checked: core builds it from the launcher's
// background plan, and the runner only starts it.
type Plan struct {
	// RunID is the ledger's run, which the run's end is told for; MatchID and RoutineID
	// name its match and routine.
	RunID     int64
	MatchID   int64
	RoutineID int64

	// Title and Source are the run's: "Fix checks · acme-org/api #212",
	// "autopilot:<routine id>".
	Title  string
	Source string

	// Agent is the agent it runs, and SessionID the session's ID minted for it, which
	// names its own folder.
	Agent     agent.ID
	SessionID string

	// Repo is the item's repository, and Dir the item's worktree, where it runs and whose
	// lock it holds.
	Repo repo.Name
	Dir  string

	// Argv is the agent's command, its binary's absolute path first; the prompt is never on
	// it. Env is the run's whole environment.
	Argv []string
	Env  []string

	// Command is Argv as the window shows it, "" to write it out.
	Command string

	// Folder is the run's own folder, <data>/agent-runs/<session id>, which holds its
	// fence's files and its prompt, and which goes with its log.
	Folder string

	// Prompt is the filled-in prompt, read on stdin.
	Prompt string

	// HeadBefore is the item's head the run starts on, which its commits are read from.
	HeadBefore string

	// Fence is what holds it in.
	Fence Fence

	// Session is how the run is recorded as a session.
	Session Session

	// Before checks, under the worktree's lock and a slot, that the run may still start:
	// the worktree is still clean with nothing unpushed, and no session runs there. Its
	// error ends the start, as it is. nil checks nothing.
	Before func(ctx context.Context) error
}

// PromptPath returns the file the run reads its prompt from.
func (p Plan) PromptPath() string {
	return filepath.Join(p.Folder, promptFile)
}

// FolderFor returns a run's own folder in root, <data>/agent-runs, named by its session.
func FolderFor(root, sessionID string) string {
	return filepath.Join(root, sessionID)
}

// check returns why the plan can't start, if it can't.
func (p Plan) check() error {
	switch {
	case p.RunID <= 0:
		return fmt.Errorf("%w: no run in the ledger", ErrInvalidPlan)
	case !agent.ValidSessionID(p.SessionID):
		return fmt.Errorf("%w: the session ID %q", ErrInvalidPlan, p.SessionID)
	case !filepath.IsAbs(p.Dir) || len(p.Argv) == 0 || !filepath.IsAbs(p.Argv[0]):
		return fmt.Errorf("%w: the worktree and the agent's binary must be absolute paths", ErrInvalidPlan)
	case filepath.Base(p.Folder) != p.SessionID:
		return fmt.Errorf("%w: the run's folder isn't named by its session", ErrInvalidPlan)
	case p.Prompt == "":
		return fmt.Errorf("%w: no prompt", ErrInvalidPlan)
	case p.Fence.Timeout <= 0:
		return fmt.Errorf("%w: no time limit", ErrInvalidPlan)
	}

	return nil
}

// Intent is what a run sets out to do, written into its record as it starts, so a run
// picked up after a restart is read and told of as one the runner started.
type Intent struct {
	RunID      int64    `json:"runId"`
	MatchID    int64    `json:"matchId"`
	RoutineID  int64    `json:"routineId"`
	Agent      agent.ID `json:"agent"`
	SessionID  string   `json:"sessionId"`
	HeadBefore string   `json:"headBefore"`
	Fence      Fence    `json:"fence"`

	// ItemID is the item the run's session is about, and Folder the run's own folder,
	// where its fence's policy is read from as it ends.
	ItemID int64  `json:"itemId"`
	Folder string `json:"folder,omitempty"`

	// Dir is the worktree it works in, whose changes the run view's Diff reads.
	Dir string `json:"dir,omitempty"`

	// ConfigDir is the agent profile's config folder, "" for the default profile's, where
	// a LimitReader finds the agent's own record of the run.
	ConfigDir string `json:"configDir,omitempty"`
}

// intentOf returns the plan's intent.
func intentOf(p Plan) Intent {
	return Intent{
		RunID: p.RunID, MatchID: p.MatchID, RoutineID: p.RoutineID, Agent: p.Agent, SessionID: p.SessionID,
		HeadBefore: p.HeadBefore, Fence: p.Fence, ItemID: p.Session.ItemID, Folder: p.Folder, Dir: p.Dir,
		ConfigDir: p.Session.ConfigDir,
	}
}
