package kit

import (
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// SnapshotVersion is the snapshot's format version. The tool mode refuses a snapshot of
// another version rather than guess at it, and the app writes it again as it starts.
// Version 2 holds routines in version 2 of the spec's format, without its UUIDs, which a
// tool mode of an older build still running would misread.
const SnapshotVersion = 2

// Snapshot is what the agent may read of the user's setup and inbox, beside the parts of
// the app's vocabulary that packages above the leaves hold: the app writes it to its
// context.json, and its tool mode reads it there.
type Snapshot struct {
	// Version is SnapshotVersion, and WrittenAt when the app wrote it.
	Version   int       `json:"version"`
	WrittenAt time.Time `json:"writtenAt"`

	Setup      Setup      `json:"setup"`
	Inbox      InboxNames `json:"inbox"`
	Vocabulary Vocabulary `json:"vocabulary"`
}

// Setup is the user's own setup: their routines, prompts, scripts, and agent profiles,
// Autopilot's settings, and the rules routines lean on.
type Setup struct {
	Routines []Routine             `json:"routines"`
	Prompts  []Prompt              `json:"prompts"`
	Scripts  []Script              `json:"scripts"`
	Profiles []Profile             `json:"profiles"`
	Settings autopilotprefs.Config `json:"settings"`
	Rules    Rules                 `json:"rules"`
}

// Routine is one of the user's routines, as the agent reads it.
type Routine struct {
	ID          int64               `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Enabled     bool                `json:"enabled"`
	Mode        autopilotprefs.Mode `json:"mode"`

	// Sentence says it in one sentence, "" for a broken one, and Broken says why it is
	// broken, "" for one that loads.
	Sentence string `json:"sentence"`
	Broken   string `json:"broken"`

	// Spec is its definition, as far as it was read for a broken one.
	Spec spec.Spec `json:"spec"`

	// UpdatedAt is when it last changed. An edit names it as its base, so a card can say
	// the routine changed since the agent read it.
	UpdatedAt time.Time `json:"updatedAt"`
}

// Purpose is what a library prompt is for, as the prompt library names it.
type Purpose string

// The purposes.
const (
	// PurposeAgent is a prompt a coding agent runs.
	PurposeAgent Purpose = "agent"

	// PurposeReply is a review reply, which a GitHub step comments.
	PurposeReply Purpose = "reply"
)

// ReplyAction is the review a reply is written for, as the prompt library names it.
type ReplyAction string

// The reviews a reply is written for. A routine's GitHub step comments a comment's reply
// alone.
const (
	ReplyApprove        ReplyAction = "approve"
	ReplyRequestChanges ReplyAction = "request_changes"
	ReplyComment        ReplyAction = "comment"
)

// AppliesTo is the kind of item a prompt is written for, as the prompt library names it.
type AppliesTo string

// The kinds a prompt is written for.
const (
	AppliesToPR    AppliesTo = "pr"
	AppliesToIssue AppliesTo = "issue"
	AppliesToAny   AppliesTo = "any"
)

// Prompt is one of the library's prompts, as the agent reads it: its names and what it is
// for, never its text.
type Prompt struct {
	// Starter is the starter it is the user's copy of, "" for one of the user's own.
	Starter  string `json:"starter"`
	Nickname string `json:"nickname"`
	Name     string `json:"name"`

	Purpose     Purpose     `json:"purpose"`
	ReplyAction ReplyAction `json:"replyAction"`
	AppliesTo   AppliesTo   `json:"appliesTo"`
}

// Script is one of the user's own Repo scripts, which a routine's script step names by its
// ID and the name it was picked under.
type Script struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`

	// Shows says which clones it shows in, and so runs in: "every clone", "Go
	// repositories", or the repositories it names.
	Shows string `json:"shows"`
}

// Profile is one of the user's agent profiles, which an agent step may name by its agent
// and its name.
type Profile struct {
	Agent string `json:"agent"`
	Name  string `json:"name"`
}

// Rules are the rules routines lean on: the bots the user trusts, the repositories and
// orgs pinned or muted, and the workflows whose held runs count for nothing.
type Rules struct {
	TrustedBots      []string `json:"trustedBots"`
	Pinned           []string `json:"pinned"`
	Muted            []string `json:"muted"`
	IgnoredWorkflows []string `json:"ignoredWorkflows"`
}

// InboxNames are the names the user's inbox holds, each list most common first: what a
// routine's watch and conditions can name. They are names alone, never an item's text.
type InboxNames struct {
	Repositories []string `json:"repositories"`
	Owners       []string `json:"owners"`
	People       []string `json:"people"`
	Bots         []string `json:"bots"`
	Labels       []string `json:"labels"`
	Checks       []string `json:"checks"`
}

// Vocabulary is the part of the app's vocabulary that packages above the leaves hold: the
// template gallery, the starter prompts, and the variables a prompt's text can use.
type Vocabulary struct {
	Templates []Template `json:"templates"`
	Starters  []Starter  `json:"starters"`
	Variables []Variable `json:"variables"`
}

// Template is one of the gallery's templates: a routine the app ships ready to use, with
// the blanks quick setup asks for.
type Template struct {
	Key      string              `json:"key"`
	Name     string              `json:"name"`
	Icon     string              `json:"icon"`
	Summary  string              `json:"summary"`
	Suggests autopilotprefs.Mode `json:"suggests"`
	Blanks   []Blank             `json:"blanks"`
	Spec     spec.Spec           `json:"spec"`
}

// Blank is one thing a template leaves for the user to fill in: a search's Where (its
// source), or a condition's values (its condition).
type Blank struct {
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Help      string `json:"help"`
	Source    int    `json:"source"`
	Condition int    `json:"condition"`
	Required  bool   `json:"required"`
}

// Starter is one of the starter prompts the app ships, by its key, which a step names to
// run the user's copy of it: its names and what it is for, never its text.
type Starter struct {
	Key         string      `json:"key"`
	Name        string      `json:"name"`
	Nickname    string      `json:"nickname"`
	AppliesTo   AppliesTo   `json:"appliesTo"`
	Purpose     Purpose     `json:"purpose"`
	ReplyAction ReplyAction `json:"replyAction"`
}

// Variable is one of the values a prompt's text can name, written {{.Name}}.
type Variable struct {
	Name        string `json:"name"`
	Group       string `json:"group"`
	Type        string `json:"type"`
	Description string `json:"description"`

	// PROnly reports a value that is empty for an issue.
	PROnly bool `json:"prOnly"`
}
