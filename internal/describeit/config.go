package describeit

import (
	"context"
	"log/slog"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
)

// Config is what the service is built with.
type Config struct {
	// Dir is the data folder's describe folder (appdata.Paths.Describe).
	Dir string

	// Executable returns the app's own executable, which a turn's MCP config and the
	// plugin's link name in the tool mode.
	Executable func() string

	// Stable reports whether the app runs from a place that outlives it: a bundled app,
	// neither translocated nor a build from source. Only such an app points the plugin's
	// link at itself, so a build from source never leaves the link on a temporary binary.
	Stable func() bool

	Agents      Agents
	Setup       Setup
	Attachments Attachments

	Clock  clock.Clock
	Sink   event.Sink
	Logger *slog.Logger
}

// Agents finds Claude Code for a conversation and for the plugin's commands. Core adapts
// the agents' readiness, the launch defaults, the profiles, and the login shell's
// environment, as a background run's start reads them.
type Agents interface {
	// Conversation returns how to start Claude Code with profile, "" for the launch
	// defaults' own: its program and the environment a turn runs with, or why it can't run
	// one (Start.Ready).
	Conversation(ctx context.Context, profile string) (Start, error)

	// Profiles returns Claude Code's profiles, the launch defaults' first.
	Profiles(ctx context.Context) ([]Profile, error)
}

// Start is how to start Claude Code for a conversation, or why it can't start.
type Start struct {
	// Ready reports Claude Code found, at least agent.ConversationFloor, and signed in;
	// Problem says why not, and Fix what to do, each a sentence.
	Ready   bool
	Problem string
	Fix     string

	// Program is its absolute path, Version its version, and Env the environment a turn
	// runs with: the login shell's, with the profile's config folder (launcher.AgentEnv)
	// and without the variables a session sets for its children (agent.HeadlessEnv).
	Program string
	Version string
	Env     []string

	// Profile is the profile's name, and Model and Effort its own defaults, which a choice
	// of Inherit shows.
	Profile string
	Model   string
	Effort  string
}

// Profile is one of Claude Code's profiles.
type Profile struct {
	Name string

	// Default reports the launch defaults' profile.
	Default bool
}

// Setup reads what the agent may read of the user's setup and inbox, as one snapshot.
// Core adapts the routines, the prompt library (no prompt's text), the starters and the
// prompt variables, the template gallery, the scripts, the profiles, Autopilot's settings,
// the rules, and the inbox's names.
type Setup interface {
	Snapshot(ctx context.Context) (kit.Snapshot, error)
}

// Attachments words what the user attaches to a message. Core adapts the routines and the
// inbox's stored items.
type Attachments interface {
	// Routine returns one of the user's routines, by its ID: its name, its sentence, and its
	// definition, as the agent may read them.
	Routine(ctx context.Context, id int64) (Attachment, error)

	// Item returns a stored item, by its ID: its address and its short facts alone (kind,
	// repository, author, labels, and its failed checks' names), never its title, body,
	// comments, diff, or code.
	Item(ctx context.Context, id int64) (Attachment, error)
}
