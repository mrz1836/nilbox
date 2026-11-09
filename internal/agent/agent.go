package agent

import (
	"errors"
	"fmt"
	"slices"
)

// ID names a coding agent, as profiles, settings, and launch defaults store it.
type ID string

// The coding agents, and the empty choice.
const (
	// Inherit is no choice of agent: a launch default or a launch that leaves the choice
	// to the next layer.
	Inherit ID = ""

	// Claude is Claude Code, the default agent.
	Claude ID = "claude"

	// Codex is OpenAI Codex.
	Codex ID = "codex"
)

// ErrUnknownAgent is returned for an agent ID this build does not know.
var ErrUnknownAgent = errors.New("unknown coding agent")

// Choice is one value an agent setting can take, with what the UI calls it.
type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Marker is an entry an agent writes into a config directory it runs with. A directory
// that holds one of an agent's markers is that agent's config directory.
type Marker struct {
	// Name is the entry's name, such as "settings.json".
	Name string `json:"name"`

	// Dir reports a directory rather than a file.
	Dir bool `json:"dir"`
}

// Agent is a coding agent the launcher can start. Every field is a plain value.
type Agent struct {
	// ID is what profiles and launch defaults store, such as "claude".
	ID ID `json:"id"`

	// Name is what the UI shows, as in "Open in Claude".
	Name string `json:"name"`

	// Command is the program a launch script runs.
	Command string `json:"command"`

	// ConfigEnv is the environment variable that points the agent at a profile's config
	// directory, such as CLAUDE_CONFIG_DIR.
	ConfigEnv string `json:"configEnv"`

	// ConfigDir is the agent's default config directory under the home directory, such
	// as ".claude". A sibling named ConfigDir_<name> or ConfigDir-<name> is profile
	// <name>.
	ConfigDir string `json:"configDir"`

	// Markers are the entries that mark the agent's config directory.
	Markers []Marker `json:"markers"`

	// EffortLevels are the effort levels the agent accepts, lowest first.
	EffortLevels []Choice `json:"effortLevels"`

	// Models suggests model names; any model name the agent accepts works too.
	Models []Choice `json:"models"`

	// PermissionModes are the permission modes a profile may set, besides full access.
	PermissionModes []Choice `json:"permissionModes"`

	// PlanLabel names the agent's planning mode, which launches use for outside
	// contributors' items: "Plan mode" or "Read-only".
	PlanLabel string `json:"planLabel"`

	// SessionNames reports that a launch can name the session.
	SessionNames bool `json:"sessionNames"`

	// SessionIDs reports that a launch passes the agent the session's ID, so the app
	// follows the session from its start. Without it, the app finds the agent's own ID
	// once the session starts.
	SessionIDs bool `json:"sessionIds"`

	// FastMode reports that a launch can start the session in the agent's Fast mode.
	FastMode bool `json:"fastMode"`

	// ConfigOverrides reports an agent that takes the effort level and Fast mode only as
	// config overrides, and runs a session started with any outside its shared app
	// server, where the app can't see that it waits for the user. A launch passes it the
	// effort only when the user's settings say to.
	ConfigOverrides bool `json:"configOverrides"`

	// AuthFile is the file in a config directory that holds the agent's sign-in, or ""
	// when the app does not check the agent's sign-in. Only its presence is checked; it is
	// never read.
	AuthFile string `json:"authFile"`

	// AuthEnv lists environment variables that sign the agent in with a key instead.
	AuthEnv []string `json:"authEnv"`

	// ModelsCache is the file in a config directory where the agent keeps the models it
	// offers, or "" when it keeps none the app reads.
	ModelsCache string `json:"modelsCache"`
}

// spec is one backend: what it is, and how a launch's options become its flags.
type spec interface {
	// info describes the agent.
	info() Agent

	// reserved lists the flags the launcher owns.
	reserved() []reservedFlag

	// reservedConfig says why a config override of key may not be added, when it may
	// not: "" allows it. Only an agent with configFlags overrides config this way.
	reservedConfig(key string) string

	// configFlags names the option that overrides one config value, such as -c, or nil.
	configFlags() []string

	// commands lists the words that make the agent run another command instead of a
	// session, such as Codex's exec, when one comes before the prompt. An agent whose
	// prompt comes first lists none.
	commands() []string

	// args returns a launch's flags, in order: model, effort, access, extra
	// directories, extra arguments, session name, and the session to start or resume.
	args(o Options) []string

	// promptLast reports that the prompt follows the flags, after "--", rather than
	// coming first.
	promptLast() bool

	// modelHint is the sentence the editors show for a model name the agent would
	// misread.
	modelHint() string
}

// specs returns every backend, the default first.
func specs() []spec {
	return []spec{claude{}, codex{}}
}

// specFor returns the backend with id, or ErrUnknownAgent.
func specFor(id ID) (spec, error) {
	for _, s := range specs() {
		if s.info().ID == id {
			return s, nil
		}
	}

	return nil, fmt.Errorf("%w %q", ErrUnknownAgent, id)
}

// All returns every agent, the default first. Each call returns fresh copies.
func All() []Agent {
	all := make([]Agent, 0, len(specs()))
	for _, s := range specs() {
		all = append(all, s.info())
	}

	return all
}

// IDs returns every agent's ID, the default first.
func IDs() []ID {
	ids := make([]ID, 0, len(specs()))
	for _, s := range specs() {
		ids = append(ids, s.info().ID)
	}

	return ids
}

// Default returns the agent used when nothing names one.
func Default() Agent {
	return All()[0]
}

// Lookup returns the agent with id, or the default agent for Inherit.
func Lookup(id ID) (Agent, error) {
	if id == Inherit {
		return Default(), nil
	}

	s, err := specFor(id)
	if err != nil {
		return Agent{}, err
	}

	return s.info(), nil
}

// Known reports whether id names an agent; Inherit names none.
func Known(id ID) bool {
	return slices.Contains(IDs(), id)
}

// PromptLast reports whether the agent's prompt follows its flags, after "--", rather than
// coming first. An unknown agent reports false.
func PromptLast(id ID) bool {
	s, err := specFor(id)

	return err == nil && s.promptLast()
}

// effortLabels names every effort level any agent accepts.
func effortLabels() map[string]string {
	return map[string]string{
		"minimal": "Minimal", "low": "Low", "medium": "Medium", "high": "High", "xhigh": "Extra high",
		"max": "Max", "ultra": "Ultra",
	}
}

// effortChoices returns the levels as choices, in order.
func effortChoices(levels ...string) []Choice {
	labels := effortLabels()

	out := make([]Choice, 0, len(levels))
	for _, level := range levels {
		out = append(out, Choice{Value: level, Label: labels[level]})
	}

	return out
}

// values returns each choice's value, in order.
func values(choices []Choice) []string {
	out := make([]string, 0, len(choices))
	for _, c := range choices {
		out = append(out, c.Value)
	}

	return out
}

// sortedIDs returns the keys of a per-agent map: the known agents in their order, then
// any others by name.
func sortedIDs[V any](m map[ID]V) []ID {
	out := make([]ID, 0, len(m))

	for _, id := range IDs() {
		if _, ok := m[id]; ok {
			out = append(out, id)
		}
	}

	var unknown []ID

	for id := range m {
		if !Known(id) {
			unknown = append(unknown, id)
		}
	}

	slices.Sort(unknown)

	return append(out, unknown...)
}
