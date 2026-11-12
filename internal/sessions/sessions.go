// Package sessions follows the coding-agent sessions the app launches, from the item each
// was launched on, while they run in the user's own terminal.
//
// Every launch is recorded in a ledger, the agent_sessions table this package owns, under
// an ID the app mints before the launch. A Watcher then reconciles what each agent
// reports through its own supported interface into the ledger, as a controller does: an
// Observer per agent (the observe/claude and observe/codex packages) turns the agent's
// own shapes into one State, and the Watcher stores each change, tells the window, and
// hands the moments that need the user to an AttentionSink. It observes only while some
// session has not ended, and it reconciles every session that has not ended when the app
// starts again, so it survives restarts.
//
// Following is read-only. The app never sends a session input, never answers its
// approvals, and never writes into an agent's config or conversation, apart from the
// session ID a Claude Code launch passes. Everything stays on this Mac: nothing here talks
// to GitHub or to any network.
package sessions

import (
	"slices"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
)

// MaxMessageRunes caps the last message a session keeps.
const MaxMessageRunes = 500

// State is what a session is doing, as its agent last reported it.
type State string

// The states of a session.
const (
	// StateStarting is a session launched whose agent has not reported it yet.
	StateStarting State = "starting"

	// StateWorking is an agent working on its turn.
	StateWorking State = "working"

	// StateNeedsApproval is an agent waiting for the user to allow an action.
	StateNeedsApproval State = "needs_approval"

	// StateNeedsInput is an agent waiting for the user to answer it.
	StateNeedsInput State = "needs_input"

	// StateIdle is an agent that finished its turn and waits for the next message.
	StateIdle State = "idle"

	// StateEnded is a session whose agent has quit. It is the only state that ends.
	StateEnded State = "ended"

	// StateFailed is an agent that reported an error it could not get past.
	StateFailed State = "failed"

	// StateUnknown is a session still running whose agent could not be asked how it is.
	StateUnknown State = "unknown"
)

// States returns every state.
func States() []State {
	return []State{
		StateStarting, StateWorking, StateNeedsApproval, StateNeedsInput, StateIdle, StateEnded, StateFailed,
		StateUnknown,
	}
}

// Valid reports whether s is one of States.
func (s State) Valid() bool {
	return slices.Contains(States(), s)
}

// Live reports a session that has not ended.
func (s State) Live() bool {
	return s != StateEnded
}

// NeedsYou reports a session waiting for the user.
func (s State) NeedsYou() bool {
	return s == StateNeedsApproval || s == StateNeedsInput
}

// Phrase says what a session in the state is doing, after the agent's name: "Claude
// needs approval", "Codex is working".
func (s State) Phrase() string {
	switch s {
	case StateStarting:
		return "is starting"
	case StateWorking:
		return "is working"
	case StateNeedsApproval:
		return "needs approval"
	case StateNeedsInput:
		return "needs your input"
	case StateIdle:
		return "is idle"
	case StateEnded:
		return "has ended"
	case StateFailed:
		return "hit an error"
	case StateUnknown:
		return "is running"
	default:
		return "is running"
	}
}

// WorktreeAction is what a session's launch did to its item's worktree.
type WorktreeAction string

// What a launch did to give its item a worktree, as the launcher names it.
const (
	// WorktreeNone is a session in the clone, or one recorded before the action was kept.
	WorktreeNone WorktreeAction = ""

	// WorktreeResume reused the item's worktree.
	WorktreeResume WorktreeAction = "resume"

	// WorktreeAdopt took over a worktree found on the item's branch somewhere else.
	WorktreeAdopt WorktreeAction = "adopt"

	// WorktreeAttach made a worktree again on the item's branch left from an earlier one.
	WorktreeAttach WorktreeAction = "attach"

	// WorktreeCreate made a new worktree.
	WorktreeCreate WorktreeAction = "create"
)

// Valid reports whether a is one of the actions, WorktreeNone included.
func (a WorktreeAction) Valid() bool {
	return slices.Contains([]WorktreeAction{WorktreeNone, WorktreeResume, WorktreeAdopt, WorktreeAttach, WorktreeCreate}, a)
}

// Session is one session as the window shows it. Every field is a plain value.
type Session struct {
	// ID is the app's own ID of the session.
	ID string `json:"id"`

	// AgentSessionID is the agent's own ID of it, or "" until the agent reports it.
	AgentSessionID string `json:"agentSessionId"`

	// ResumedFrom is the ID of the session this one resumed, or "".
	ResumedFrom string `json:"resumedFrom"`

	// Agent is the agent, with the profile it ran with, and the nickname of the prompt
	// it was started with, or "" for a session resumed.
	Agent   agent.ID `json:"agent"`
	Profile string   `json:"profile"`
	Prompt  string   `json:"prompt"`

	// Terminal is the terminal it runs in.
	Terminal string `json:"terminal"`

	// ItemID is the item's local ID, Repo is "owner/name", and Kind, Number, Title, and
	// URL are the item.
	ItemID int64     `json:"itemId"`
	Repo   string    `json:"repo"`
	Kind   item.Kind `json:"kind"`
	Number int       `json:"number"`
	Title  string    `json:"title"`
	URL    string    `json:"url"`

	// Dir is the folder it runs in, with ~ for home, and Worktree the item's worktree
	// there, or "" when it runs in the clone.
	Dir      string `json:"dir"`
	Worktree string `json:"worktree"`

	// WorktreeAction is what its launch did to that worktree, or "" in the clone or when
	// not known.
	WorktreeAction WorktreeAction `json:"worktreeAction"`

	// Permission summarizes the permissions it started with, such as "full access", and
	// Notice says what its launch worked around; each is "" when not known.
	Permission string `json:"permission"`
	Notice     string `json:"notice"`

	// State is what it is doing, Detail says more where the agent did, and StateAt is
	// when it came to that state, as RFC 3339.
	State   State  `json:"state"`
	Detail  string `json:"detail"`
	StateAt string `json:"stateAt"`

	// LastMessage is the start of what the agent last said, or "".
	LastMessage string `json:"lastMessage"`

	// LaunchedAt is when it was launched, and EndedAt when it ended, or "", as RFC 3339.
	LaunchedAt string `json:"launchedAt"`
	EndedAt    string `json:"endedAt"`

	// CanFocus reports a session whose terminal can be shown: it runs, and its process
	// is known. CanResume reports one that can be resumed: it ended, and the agent named
	// its conversation.
	CanFocus  bool `json:"canFocus"`
	CanResume bool `json:"canResume"`
}

// Snapshot is the sessions the window shows: every one still running, and the others
// launched in the last week, newest first.
type Snapshot struct {
	// Following reports whether the app follows sessions; the window shows none while
	// it does not.
	Following bool `json:"following"`

	Sessions []Session `json:"sessions"`
}
