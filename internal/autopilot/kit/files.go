package kit

import "time"

// The files the tool mode writes and the app reads, and the app writes and the tool mode
// reads, beside the snapshot: a proposal, one file each, and what the user did with each
// part of the proposals in a folder.

// ProposalFileVersion is a proposal file's format version.
const ProposalFileVersion = 1

// MaxOutsidePending caps the proposals waiting from the user's own Claude Code sessions:
// past it, the tool mode refuses another until the user deals with some.
const MaxOutsidePending = 50

// ProposalFile is one proposal as the tool mode writes it: in-app, into its conversation's
// folder; from the user's own Claude Code, into the outside folder.
type ProposalFile struct {
	Version int `json:"version"`

	// ID is its file name's stem: the moment it was written, in UTC, and random hex
	// (NewProposalID), so two sessions writing at once never pick the same name.
	ID string    `json:"id"`
	At time.Time `json:"at"`

	// Conversation is the in-app conversation it came from, "" for one from outside.
	Conversation string `json:"conversation,omitempty"`

	// Where is the folder an outside session ran in, by its base name, "" in-app.
	Where string `json:"where,omitempty"`

	// SnapshotAt is when the snapshot it was checked against was written.
	SnapshotAt time.Time `json:"snapshotAt"`

	Proposal Proposal `json:"proposal"`
}

// Part is a part of a proposal a card shows as a row of its own.
type Part string

// The parts, in the order a card applies them.
const (
	PartPrompt   Part = "prompt"
	PartRoutine  Part = "routine"
	PartSettings Part = "settings"
	PartRules    Part = "rules"
)

// OutcomeKind is what the user did with a part of a proposal.
type OutcomeKind string

// What the user can do with a part.
const (
	// OutcomeAdded is a new routine or prompt added, OutcomeSaved a change to a routine
	// saved, and OutcomeApplied a settings or rules change applied.
	OutcomeAdded   OutcomeKind = "added"
	OutcomeSaved   OutcomeKind = "saved"
	OutcomeApplied OutcomeKind = "applied"

	// OutcomeBuilder is a routine opened in the builder, to finish there.
	OutcomeBuilder OutcomeKind = "builder"

	// OutcomeDiscarded is a part the user discarded.
	OutcomeDiscarded OutcomeKind = "discarded"
)

// OutcomesVersion is the outcomes file's format version.
const OutcomesVersion = 1

// Outcomes are what the user did with the parts of the proposals in one folder, as the app
// records them: the next turn tells the agent, and the tool mode's proposal status reads
// them.
type Outcomes struct {
	Version int       `json:"version"`
	Items   []Outcome `json:"items"`
}

// Outcome is what the user did with one part of a proposal.
type Outcome struct {
	// Proposal is the proposal's ID, Part the part, and Index which of its kind, in the
	// proposal's order (0 for the settings and the rules, which a proposal holds once).
	Proposal string `json:"proposal"`
	Part     Part   `json:"part"`
	Index    int    `json:"index"`

	Kind OutcomeKind `json:"kind"`

	// Words say it as the next turn tells the agent: `Added "Fix checks", switched off`.
	Words string `json:"words"`

	// RoutineID is the routine a routine's part added or saved, 0 for none.
	RoutineID int64 `json:"routineId,omitempty"`

	At time.Time `json:"at"`
}
