package describeit

import (
	"log/slog"
	"path/filepath"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/logging"
)

// Service is Describe it, as the window calls it.
type Service struct {
	cfg    Config
	clock  clock.Clock
	logger *slog.Logger
}

// New returns the service cfg describes. A nil Clock is the real clock, and a nil Logger
// discards.
func New(cfg Config) *Service {
	return &Service{cfg: cfg, clock: clock.Or(cfg.Clock), logger: logging.Or(cfg.Logger)}
}

// The describe folder's layout (internal/describeit's part of docs/architecture.md). Every
// folder is the user's alone, and every file is written whole (appdata.WriteFile).
const (
	// contextName is the snapshot the tool mode reads (kit.Snapshot).
	contextName = "context.json"

	// instructionsName is the in-app agent's instructions (kit.SkillText), which each turn
	// passes as its system prompt's addition.
	instructionsName = "instructions.md"

	// workName is the one empty folder every turn runs in, so the profile's own Claude
	// config keeps one project for every conversation (the spike's S6).
	workName = "work"

	// binName holds the link to the app's executable that the plugin runs.
	binName = "bin"

	// pluginName is the marketplace folder the user's own Claude Code installs the plugin
	// from, and loads it in place from.
	pluginName = "plugin"

	// agentName is the agent new conversations start with (AgentChoice).
	agentName = "agent.json"

	// conversationsName holds each conversation's folder, by its ID: its record
	// (conversationName), its MCP config, and each turn's prompt and stream.
	conversationsName = "conversations"
	conversationName  = "conversation.json"

	// proposalsName holds the proposals: each conversation's folder by its ID, and
	// Outside's, each proposal a kit.ProposalFile and the folder's kit.Outcomes beside them
	// (outcomesName).
	proposalsName = "proposals"
	outcomesName  = "outcomes.json"
)

// path returns a path under the describe folder.
func (s *Service) path(parts ...string) string {
	return filepath.Join(append([]string{s.cfg.Dir}, parts...)...)
}

// contextFile is the snapshot's file.
func (s *Service) contextFile() string { return s.path(contextName) }

// instructionsFile is the in-app agent's instructions' file.
func (s *Service) instructionsFile() string { return s.path(instructionsName) }

// workDir is the folder every turn runs in.
func (s *Service) workDir() string { return s.path(workName) }

// linkFile is the link the plugin runs: the app's executable, in its tool mode.
func (s *Service) linkFile() string { return s.path(binName, brand.Slug+"-mcp") }

// pluginDir is the plugin's marketplace folder.
func (s *Service) pluginDir() string { return s.path(pluginName) }

// agentFile is the agent new conversations start with.
func (s *Service) agentFile() string { return s.path(agentName) }

// conversationDir is the folder of the conversation id.
func (s *Service) conversationDir(id string) string { return s.path(conversationsName, id) }

// proposalsDir is the proposals folder of source: a conversation's ID, or Outside.
func (s *Service) proposalsDir(source string) string { return s.path(proposalsName, source) }
