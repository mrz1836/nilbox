package kit

import (
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// What one proposal may hold. A proposal past them is refused whole, so the agent splits
// a larger change into proposals of its own, each reviewed apart.
const (
	MaxRoutines = 10
	MaxPrompts  = 10

	// MaxPromptBody caps a new prompt's text, in bytes.
	MaxPromptBody = 32 << 10
)

// Proposal is what the agent proposes, for the user to review in the Autopilot tab: new
// prompts, new routines and changes to routines, a change to Autopilot's settings, and a
// change to the rules routines lean on. A card applies its parts in that order, so a
// routine may run a prompt the same proposal adds.
type Proposal struct {
	// Summary says what it does in a sentence or two, as the card shows it under "Claude
	// says".
	Summary string `json:"summary"`

	Prompts  []NewPrompt     `json:"prompts,omitempty"`
	Routines []RoutineChange `json:"routines,omitempty"`
	Settings *SettingsChange `json:"settings,omitempty"`
	Rules    *RulesChange    `json:"rules,omitempty"`
}

// NewPrompt is a prompt to add to the library: its names, what it is for, and its text. It
// carries no models, effort, plan mode, worktree, or extra flags, which reach a click's
// launch without a fence, so a new prompt takes the library's defaults for each.
type NewPrompt struct {
	Name        string      `json:"name"`
	Nickname    string      `json:"nickname"`
	Purpose     Purpose     `json:"purpose"`
	ReplyAction ReplyAction `json:"replyAction,omitempty"`
	AppliesTo   AppliesTo   `json:"appliesTo"`
	Body        string      `json:"body"`
}

// RoutineChange is a new routine, or a change to one of the user's routines: a change
// never touches a routine's mode or switch, and a new routine arrives switched off, in the
// mode Settings says new routines start in.
type RoutineChange struct {
	// ID is the routine it changes, 0 for a new one, and Base that routine's updatedAt as
	// the agent read it, so a card can say it changed since.
	ID   int64  `json:"id,omitempty"`
	Base string `json:"base,omitempty"`

	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// Mode is the mode the agent suggests a new routine starts in, which the card shows
	// beside the one it starts in as a hint, as the gallery's suggestion does; never
	// applied.
	Mode autopilotprefs.Mode `json:"mode,omitempty"`

	// Template is the gallery template it starts from, "" for none.
	Template string `json:"template,omitempty"`

	Spec spec.Spec `json:"spec"`
}

// SettingsChange is a change to Autopilot's settings: only what changes is set. It has no
// field for Autopilot's switch or for Pause, which are the user's alone, so a proposal
// that names either is refused as it is read.
type SettingsChange struct {
	DefaultMode   *autopilotprefs.Mode     `json:"defaultMode,omitempty"`
	AgentsAtOnce  *int                     `json:"agentsAtOnce,omitempty"`
	RunsPerDay    *int                     `json:"runsPerDay,omitempty"`
	ActionsPerDay *int                     `json:"actionsPerDay,omitempty"`
	DailyCap      *autopilotprefs.DailyCap `json:"dailyCap,omitempty"`
	KeepAwake     *bool                    `json:"keepAwake,omitempty"`
	HoldOnOutage  *bool                    `json:"holdOnOutage,omitempty"`
	CatchUpHours  *int                     `json:"catchUpHours,omitempty"`
	HistoryDays   *int                     `json:"historyDays,omitempty"`
	LogDays       *int                     `json:"logDays,omitempty"`
}

// RulesChange is a change to the rules routines lean on: bots to trust or no longer trust,
// repositories and orgs to pin, mute, or clear of either, and workflows whose held runs to
// ignore or count again.
type RulesChange struct {
	Trust   []string `json:"trust,omitempty"`
	Untrust []string `json:"untrust,omitempty"`

	Pin   []string `json:"pin,omitempty"`
	Mute  []string `json:"mute,omitempty"`
	Clear []string `json:"clear,omitempty"`

	IgnoreWorkflows   []string `json:"ignoreWorkflows,omitempty"`
	UnignoreWorkflows []string `json:"unignoreWorkflows,omitempty"`
}
