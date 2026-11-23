package describeit

import (
	"context"
	"log/slog"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/logging"
)

// Service is Describe it, as the window calls it.
type Service struct {
	cfg    Config
	clock  clock.Clock
	logger *slog.Logger
	sink   event.Sink

	// runner runs one of the plugin's commands with env (execpath.Exec): tests stand in for
	// it.
	runner func(env []string) execpath.Runner

	// mu guards the current conversation, the turn running, and closing.
	mu sync.Mutex

	// rec is the current conversation, nil for none, once looked says it was looked for.
	rec    *record
	looked bool

	// live is the turn running, or starting, nil while none does.
	live *liveTurn

	// closing reports the app quitting: no turn starts any more.
	closing bool

	// outcomesMu orders the writes of every outcomes file, each a read and a write.
	outcomesMu sync.Mutex

	// snap keeps the live snapshot a while.
	snap snapshotCache
}

// New returns the service cfg describes. A nil Clock is the real clock, a nil Sink
// discards, and a nil Logger discards.
func New(cfg Config) *Service {
	return &Service{
		cfg: cfg, clock: clock.Or(cfg.Clock), logger: logging.Or(cfg.Logger), sink: event.Or(cfg.Sink),
		runner: execRunner,
	}
}

// startProcess starts a turn's agent in a process group of its own.
func startProcess(ctx context.Context, cmd execpath.Cmd) (process, error) {
	proc, err := execpath.Start(ctx, cmd)
	if err != nil {
		return nil, err
	}

	return proc, nil
}

// execRunner runs one of the plugin's commands with env as its whole environment.
func execRunner(env []string) execpath.Runner {
	return execpath.Exec{Env: env}
}

// The describe folder's layout (docs/architecture/describeit.md). Every folder is the user's
// alone, and every file is written whole (appdata.WriteFile).
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

// recordFile is the record of the conversation id.
func (s *Service) recordFile(id string) string {
	return filepath.Join(s.conversationDir(id), conversationName)
}

// turnFile is the file of the conversation id's turn n with the suffix given: its prompt
// (".prompt.md") or its stream (".jsonl").
func (s *Service) turnFile(id string, n int, suffix string) string {
	name := strconv.Itoa(n)
	for len(name) < 4 {
		name = "0" + name
	}

	return filepath.Join(s.conversationDir(id), "turn-"+name+suffix)
}

// turnLog is the stream of the conversation id's turn n.
func (s *Service) turnLog(id string, n int) string { return s.turnFile(id, n, ".jsonl") }

// turnPrompt is the prompt of the conversation id's turn n.
func (s *Service) turnPrompt(id string, n int) string { return s.turnFile(id, n, ".prompt.md") }

// proposalsDir is the proposals folder of source: a conversation's ID, or Outside.
func (s *Service) proposalsDir(source string) string { return s.path(proposalsName, source) }

// outcomesFile is the outcomes file of the proposals folder of source.
func (s *Service) outcomesFile(source string) string {
	return filepath.Join(s.proposalsDir(source), outcomesName)
}
