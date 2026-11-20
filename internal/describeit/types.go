package describeit

import (
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
)

// Status says whether Describe it can run a turn now, and with which agent.
type Status struct {
	// Ready reports Claude Code found, new enough, and signed in; Problem says why not,
	// and Fix what to do, each a sentence, "" while it is ready.
	Ready   bool   `json:"ready"`
	Problem string `json:"problem"`
	Fix     string `json:"fix"`

	// Version is Claude Code's version, "" when it isn't found.
	Version string `json:"version"`

	// Agent is the agent new conversations start with, and Profile, Model, and Effort
	// what it comes to, Inherit resolved.
	Agent   AgentChoice `json:"agent"`
	Profile string      `json:"profile"`
	Model   string      `json:"model"`
	Effort  string      `json:"effort"`

	// Running reports a turn running now.
	Running bool `json:"running"`

	// Pending counts the cards from the user's own Claude Code with a part no one has dealt
	// with yet: the left nav's count.
	Pending int `json:"pending"`
}

// AgentChoice is the agent a conversation runs with: Claude Code's profile, model, and
// effort, each "" to inherit the launch defaults' and the profile's own.
type AgentChoice struct {
	Profile string `json:"profile"`
	Model   string `json:"model"`
	Effort  string `json:"effort"`
}

// AttachKind is what a message attaches.
type AttachKind string

// The kinds of attachment.
const (
	// AttachRoutine is one of the user's routines, which Change with Claude… attaches.
	AttachRoutine AttachKind = "routine"

	// AttachItem is a stored item, which Describe a routine for this… attaches: its address
	// and short facts alone.
	AttachItem AttachKind = "item"
)

// AttachRef names what a message attaches, by its kind and ID.
type AttachRef struct {
	Kind AttachKind `json:"kind"`
	ID   int64      `json:"id"`
}

// Attachment is what a message attached, as the turn's prompt carries it.
type Attachment struct {
	Kind AttachKind `json:"kind"`
	ID   int64      `json:"id"`

	// Label is its chip's text ("Fix failing checks", "acme-org/example-repo#42"), and Words
	// what the turn tells the agent of it.
	Label string `json:"label"`
	Words string `json:"words"`
}

// Message is what the user sends: their words, and what they attach.
type Message struct {
	Text   string      `json:"text"`
	Attach []AttachRef `json:"attach"`
}

// TurnState is where a turn stands.
type TurnState string

// The states of a turn.
const (
	// TurnRunning is a turn whose agent runs now.
	TurnRunning TurnState = "running"

	// TurnDone is a turn the agent finished.
	TurnDone TurnState = "done"

	// TurnStopped is a turn the user stopped, or the app's quit ended; the conversation
	// goes on from it.
	TurnStopped TurnState = "stopped"

	// TurnFailed is a turn that ended in error: its agent couldn't start, its setup was
	// wrong (the app's tools didn't start, or a tool not the app's appeared), it ran out of
	// its budget, or it ended in error. Error says which.
	TurnFailed TurnState = "failed"
)

// Turn is one of a conversation's turns: what the user sent, and how the agent's run of it
// went.
type Turn struct {
	// N numbers it from 1.
	N int `json:"n"`

	// Text is what the user wrote, and Attach what the message attached.
	Text   string       `json:"text"`
	Attach []Attachment `json:"attach"`

	// Feedback is what the turn told the agent the user did since the turn before ("Added
	// “X”, switched off; discarded the settings change."), "" for nothing.
	Feedback string `json:"feedback"`

	State TurnState `json:"state"`

	// Error says why it failed, worded for the window, "" unless it did.
	Error string `json:"error"`

	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`

	// Reply is the agent's final message, "" until it is in.
	Reply string `json:"reply"`

	// Cost is what the turn alone cost, nil until its result says, and Estimated says it is
	// an estimate, as for a run signed in with the user's plan.
	Cost      *float64 `json:"cost"`
	Estimated bool     `json:"estimated"`

	// Cards are the proposals the agent made in the turn, in the order it made them.
	Cards []Card `json:"cards"`
}

// Conversation is the current conversation.
type Conversation struct {
	// ID is its UUID, the agent's session's ID too; "" when there is no conversation.
	ID string `json:"id"`

	// Agent is the agent it runs with, as chosen, and Profile, Model, and Effort what that
	// comes to, Inherit resolved, for the header.
	Agent   AgentChoice `json:"agent"`
	Profile string      `json:"profile"`
	Model   string      `json:"model"`
	Effort  string      `json:"effort"`

	CreatedAt time.Time `json:"createdAt"`
	Turns     []Turn    `json:"turns"`

	// Running reports a turn running now.
	Running bool `json:"running"`

	// Cost is what the conversation cost so far, nil before a turn reported one, and
	// TokensIn and TokensOut the tokens it used; Estimated as a Turn's.
	Cost      *float64 `json:"cost"`
	Estimated bool     `json:"estimated"`
	TokensIn  int64    `json:"tokensIn"`
	TokensOut int64    `json:"tokensOut"`
}

// Outside is a CardRef's Source for a card from the user's own Claude Code.
const Outside = "outside"

// CardRef names a card: the conversation it came from (its ID, or Outside) and its
// proposal's ID.
type CardRef struct {
	Source   string `json:"source"`
	Proposal string `json:"proposal"`
}

// Card is one proposal, read from its file and checked against the live snapshot as it is
// shown: its parts in the order a card applies them, each with what applying it would do,
// its problems, and its risks, and what the user did with each so far.
type Card struct {
	Ref CardRef `json:"ref"`

	// At is when the agent proposed it; Where is the folder an outside session ran in, by
	// its base name, "" in-app; Turn is the in-app turn it came in, 0 for one from outside.
	At    time.Time `json:"at"`
	Where string    `json:"where"`
	Turn  int       `json:"turn"`

	// Summary is what the agent says it does: the card's "Claude says".
	Summary string `json:"summary"`

	// Proposal is the proposal as the agent wrote it, which a part applies from where
	// Checked holds nothing of its own (a routine's description, the mode the agent suggests
	// as a hint, the template it starts from, the change's routine and base), and Checked
	// each part checked against the live snapshot, which a part applies from otherwise (a
	// routine's name and spec as a save stores them, the settings and rules once applied).
	Proposal kit.Proposal  `json:"proposal"`
	Checked  kit.Checked   `json:"checked"`
	Outcomes []kit.Outcome `json:"outcomes"`

	// Settled reports a card the user dealt with every part of.
	Settled bool `json:"settled"`
}

// SettleRequest records what the user did with one part of a card.
type SettleRequest struct {
	Ref   CardRef  `json:"ref"`
	Part  kit.Part `json:"part"`
	Index int      `json:"index"`

	Kind kit.OutcomeKind `json:"kind"`

	// Words say it as the next turn tells the agent: `Added "Fix checks", switched off`.
	Words string `json:"words"`

	// RoutineID is the routine a routine's part added or saved, 0 for none.
	RoutineID int64 `json:"routineId"`
}

// PluginState is whether the plugin for the user's own Claude Code is installed in one of
// Claude Code's profiles.
type PluginState struct {
	Profile string `json:"profile"`
	Default bool   `json:"default"`

	Installed bool   `json:"installed"`
	Version   string `json:"version"`

	// Problem says why its state couldn't be read, or why it can't be installed here, ""
	// for none.
	Problem string `json:"problem"`
}
