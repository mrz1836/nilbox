package kit

import (
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// Where is where the agent reading kit's words runs: in the app, or in the user's own
// Claude Code.
type Where string

// The places.
const (
	// InApp is the app's own conversation, whose agent has no tools but the tool mode's.
	InApp Where = "inApp"

	// Outside is a session in the user's own Claude Code, which may run anywhere, with
	// whatever tools the user gave it: it reads less of the setup, and none of the inbox's
	// names.
	Outside Where = "outside"
)

// RiskLevel is how much a risk asks of the user's attention.
type RiskLevel string

// The levels.
const (
	// RiskWarning is a change that acts on GitHub, runs an agent with more reach, or
	// loosens a limit or a rule: read it before applying.
	RiskWarning RiskLevel = "warning"

	// RiskCaution is a change worth knowing about that loosens nothing: a stale base, or a
	// name already taken.
	RiskCaution RiskLevel = "caution"
)

// Risk is one thing a part of a proposal changes that the user should see before applying
// it, worked out from the part's before and after, never from the agent's words.
type Risk struct {
	Level RiskLevel `json:"level"`

	// Text says it as one sentence.
	Text string `json:"text"`
}

// Checked is a proposal checked against a snapshot: each part with what it would be once
// applied, its problems and warnings, and its risks.
type Checked struct {
	Prompts  []CheckedPrompt  `json:"prompts"`
	Routines []CheckedRoutine `json:"routines"`
	Settings *CheckedSettings `json:"settings"`
	Rules    *CheckedRules    `json:"rules"`
}

// CheckedPrompt is a new prompt, checked.
type CheckedPrompt struct {
	Index int `json:"index"`

	// Problems are what keep it from being added, each a sentence.
	Problems []string  `json:"problems"`
	Risks    []Risk    `json:"risks"`
	Prompt   NewPrompt `json:"prompt"`
}

// CheckedRoutine is a new routine, or a change to one, checked as the builder checks a
// draft.
type CheckedRoutine struct {
	Index int `json:"index"`

	// Name is the name it would be saved under: the one proposed, or, when another routine
	// has it, the free one a save would give it, and NameTaken says so.
	Name      string `json:"name"`
	NameTaken bool   `json:"nameTaken"`

	// Spec is the routine's definition as a save would store it, normalized.
	Spec spec.Spec `json:"spec"`

	// Sentence says it in one sentence, "" while it has problems; Problems keep it from
	// being saved, and Warnings don't; DryRunOnly are the kinds of step this build runs only
	// in Dry run.
	Sentence   string             `json:"sentence"`
	Problems   []spec.Problem     `json:"problems"`
	Warnings   []spec.Problem     `json:"warnings"`
	DryRunOnly []catalog.StepKind `json:"dryRunOnly"`

	// Before is the routine a change changes, as the snapshot holds it, nil for a new one.
	// Gone reports a change to a routine the snapshot doesn't hold, and Stale one whose
	// base isn't the routine's updatedAt any more.
	Before *Routine `json:"before"`
	Gone   bool     `json:"gone"`
	Stale  bool     `json:"stale"`

	Risks []Risk `json:"risks"`
}

// SettingChange is one of Autopilot's settings a proposal changes, in the window's words.
type SettingChange struct {
	// Key is the setting, as the proposal names it ("agentsAtOnce").
	Key    string `json:"key"`
	Label  string `json:"label"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// CheckedSettings is a change to Autopilot's settings, checked.
type CheckedSettings struct {
	Changes  []SettingChange `json:"changes"`
	Problems []string        `json:"problems"`
	Risks    []Risk          `json:"risks"`

	// After is the whole of the settings once the change is applied.
	After autopilotprefs.Config `json:"after"`
}

// RuleChange is one change to the rules routines lean on, in the window's words.
type RuleChange struct {
	// Kind is the proposal's list it came from ("trust", "mute", ...), Target the bot,
	// repository, org, or workflow, and Words the change as one line ("Trust
	// renovate[bot]").
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Words  string `json:"words"`
}

// CheckedRules is a change to the rules routines lean on, checked.
type CheckedRules struct {
	Changes  []RuleChange `json:"changes"`
	Problems []string     `json:"problems"`
	Risks    []Risk       `json:"risks"`

	// After is the whole of the rules once the change is applied.
	After Rules `json:"after"`
}
