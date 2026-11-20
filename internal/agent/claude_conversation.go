package agent

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// Claude Code's conversation half, as the conversation spike found it on 2.1.328, running
// `claude -p` turns in an empty folder against a stdio MCP server of its own:
//
//   - --tools "" --restricted --strict-mcp-config --mcp-config <file> leave a turn the
//     server's tools alone, the server connected, and none of the user's, the project's, or
//     the local settings, so none of their hooks, allow rules, MCP servers, or skills; the
//     user's and a folder's CLAUDE.md never reach it either.
//   - Under --permission-mode dontAsk the server's own tools are denied without an allow
//     rule; --allowedTools mcp__<server> allows every one of them, and nothing else.
//   - --max-budget-usd holds one process, so one turn, never the whole conversation.
//   - --resume takes the conversation up again in a new process, which takes every flag
//     again; with --system-prompt-snapshot off it reads its instructions fresh each turn,
//     so an app update changes what the next turn reads.
//   - --tools, --mcp-config, and --allowedTools take several values, so another flag
//     follows each one's value, and the prompt comes on stdin.
//   - --name names the session in the user's own lists, where a running turn shows; the
//     sessions watcher follows only the sessions the app recorded, so it ignores turns.
//
// The plugin brings the same server to the user's own sessions: a local marketplace folder
// with one plugin, its skill, and its .mcp.json. `claude plugin install <plugin>
// --marketplace <dir>` adds the marketplace and installs from it in one step, asking
// nothing, and the plugin loads in place from the folder, so the app keeps it current by
// rewriting the folder. Each plugin command prints one line of JSON with --json. In a
// session its tools are mcp__plugin_<plugin>_<server>__<tool>, and with no permission rule
// of its own each call asks the user first.

// Claude Code's flags and values for a conversation's turn, beside the headless run's
// (claude_headless.go) and the fence's (claude_fence.go).
const (
	claudeSessionIDFlag          = "--session-id"
	claudeResumeFlag             = "--resume"
	claudeNameFlag               = "--name"
	claudeMCPConfig              = "--mcp-config"
	claudeAllowedTools           = "--allowedTools"
	claudeAppendSystemPromptFile = "--append-system-prompt-file"

	// claudeNoTools is the --tools value that leaves a turn none of Claude Code's own tools.
	claudeNoTools = ""

	// claudeMCPTool starts an MCP tool's name, which joins its server's name and its own
	// with claudeToolJoin; a plugin's server goes by claudePluginServer, its plugin's
	// name, claudePluginJoin, and its own name there.
	claudeMCPTool      = "mcp__"
	claudeToolJoin     = "__"
	claudePluginServer = "plugin_"
	claudePluginJoin   = "_"

	// claudeStdio is the type of an MCP server Claude Code starts and talks to on its stdin
	// and stdout.
	claudeStdio = "stdio"
)

// Claude Code's plugin commands, each asked for its one line of JSON.
const (
	claudePlugin            = "plugin"
	claudePluginInstall     = "install"
	claudePluginUninstall   = "uninstall"
	claudePluginMarketplace = "marketplace"
	claudePluginRemove      = "remove"
	claudePluginList        = "list"
	claudeMarketplaceFlag   = "--marketplace"
	claudeJSON              = "--json"
)

// The files of a plugin's local marketplace, as Claude Code lays them out.
const (
	claudePluginDir       = ".claude-plugin"
	claudeMarketplaceFile = "marketplace.json"
	claudePluginManifest  = "plugin.json"
	claudeSkillsDir       = "skills"
	claudeSkillFile       = "SKILL.md"
	claudePluginMCP       = ".mcp.json"
)

// claudeConversationFloor is the oldest Claude Code that takes every flag a conversation's
// turn and the plugin commands pass, and reads every key the files below write
// (claudeConversationReviewed): the version the conversation spike ran each one on. A flag
// or key added here is reviewed against it, and the floor raised when the flag is newer.
const claudeConversationFloor = "2.1.328"

// conversationFloor is claudeConversationFloor.
func (claude) conversationFloor() string { return claudeConversationFloor }

// claudeConversationReviewed lists every flag a turn passes Claude Code, every plugin
// command, and every key the turn's and the plugin's files write, as checked against
// claudeConversationFloor; in a file's path and a key, <plugin>, <skill>, and <server> stand
// for their names. A test fails when the conversation uses one not listed here, or no longer
// uses one that is, so nothing changes without its floor's review.
func claudeConversationReviewed() []string {
	return []string{
		// A turn's flags.
		"-p", "--output-format", "--verbose", "--permission-prompts", "--session-id", "--resume", "--name", "--model",
		"--effort", "--tools", "--restricted", "--strict-mcp-config", "--mcp-config", "--permission-mode",
		"--allowedTools", "--max-budget-usd", "--append-system-prompt-file", "--system-prompt-snapshot",
		// The plugin commands.
		"plugin install <plugin> --marketplace <dir> --json", "plugin uninstall <plugin>@<marketplace> --json",
		"plugin marketplace remove <marketplace> --json", "plugin list --json",
		// A turn's MCP config.
		"mcp.json: mcpServers.<server>.type", "mcp.json: mcpServers.<server>.command",
		"mcp.json: mcpServers.<server>.args",
		// The plugin's files.
		".claude-plugin/marketplace.json: name", ".claude-plugin/marketplace.json: owner.name",
		".claude-plugin/marketplace.json: description", ".claude-plugin/marketplace.json: plugins[].name",
		".claude-plugin/marketplace.json: plugins[].source", ".claude-plugin/marketplace.json: plugins[].description",
		".claude-plugin/marketplace.json: plugins[].version",
		"<plugin>/.claude-plugin/plugin.json: name", "<plugin>/.claude-plugin/plugin.json: version",
		"<plugin>/.claude-plugin/plugin.json: description", "<plugin>/.claude-plugin/plugin.json: author.name",
		"<plugin>/skills/<skill>/SKILL.md: name", "<plugin>/skills/<skill>/SKILL.md: description",
		"<plugin>/.mcp.json: mcpServers.<server>.command", "<plugin>/.mcp.json: mcpServers.<server>.args",
	}
}

// conversationArgs returns a turn's flags: the stream, nobody to answer, the session
// started (--session-id) or resumed (--resume) and its name, the model and effort, no tool
// of Claude Code's own, no settings but the managed ones, the turn's MCP server alone and
// every tool of it allowed while dontAsk denies anything else, the turn's budget, and the
// instructions, read fresh each turn (--system-prompt-snapshot off).
func (claude) conversationArgs(o ConversationOptions) []string {
	args := []string{
		claudePrint, claudeOutputFormat, claudeStreamJSON, claudeVerbose, claudePermissionPrompts, claudeNoPrompts,
	}

	if o.Resume {
		args = append(args, claudeResumeFlag, o.SessionID)
	} else {
		args = append(args, claudeSessionIDFlag, o.SessionID)
	}

	if o.Name != "" {
		args = append(args, claudeNameFlag, o.Name)
	}

	if o.Model != "" {
		args = append(args, claudeModel, o.Model)
	}

	if o.Effort != "" {
		args = append(args, claudeEffort, o.Effort)
	}

	return append(args,
		claudeToolsFlag, claudeNoTools, claudeRestricted, claudeStrictMCPConfig, claudeMCPConfig, o.MCPConfig,
		claudePermissionMode, claudeDontAsk, claudeAllowedTools, claudeMCPTool+o.Server,
		claudeMaxBudget, strconv.FormatFloat(o.BudgetUSD, 'f', -1, 64),
		claudeAppendSystemPromptFile, o.SystemPromptFile,
		claudeSystemPromptSnapshot, claudeOff,
	)
}

// toolPrefix: mcp__<server>__.
func (claude) toolPrefix(server string) string {
	return claudeMCPTool + server + claudeToolJoin
}

// pluginToolPrefix: mcp__plugin_<plugin>_<server>__.
func (claude) pluginToolPrefix(plugin, server string) string {
	return claudeMCPTool + claudePluginServer + plugin + claudePluginJoin + server + claudeToolJoin
}

// claudeMCPConfigFile is an MCP config, as --mcp-config and a plugin's .mcp.json read it.
type claudeMCPConfigFile struct {
	MCPServers map[string]claudeMCPServer `json:"mcpServers"`
}

// claudeMCPServer is one stdio server: its type, which an --mcp-config file names and a
// plugin's .mcp.json leaves out, and the program it runs with its arguments.
type claudeMCPServer struct {
	Type    string   `json:"type,omitempty"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// claudeMCPConfigOf returns the MCP config that names the server s alone, of the type
// given ("" for none).
func claudeMCPConfigOf(s ConversationSpec, kind string) claudeMCPConfigFile {
	return claudeMCPConfigFile{MCPServers: map[string]claudeMCPServer{
		s.Server: {Type: kind, Command: s.Command, Args: textutil.NonNil(s.Args)},
	}}
}

// conversationFiles returns a turn's MCP config, which names its stdio server alone.
func (claude) conversationFiles(s ConversationSpec) ([]File, error) {
	data, err := claudeJSONFile(claudeMCPConfigOf(s, claudeStdio))
	if err != nil {
		return nil, fmt.Errorf("write the turn's MCP config: %w", err)
	}

	return []File{{Path: MCPConfigName, Data: data}}, nil
}

// claudeMarketplace is a local marketplace's manifest, which lists its one plugin.
type claudeMarketplace struct {
	Name        string                   `json:"name"`
	Owner       claudeAuthor             `json:"owner"`
	Description string                   `json:"description"`
	Plugins     []claudeMarketplaceEntry `json:"plugins"`
}

// claudeMarketplaceEntry is a plugin a marketplace lists: its folder (source), relative to
// the marketplace's.
type claudeMarketplaceEntry struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

// claudePluginFile is a plugin's manifest.
type claudePluginFile struct {
	Name        string       `json:"name"`
	Version     string       `json:"version"`
	Description string       `json:"description"`
	Author      claudeAuthor `json:"author"`
}

// claudeAuthor names who makes a plugin, or owns a marketplace.
type claudeAuthor struct {
	Name string `json:"name"`
}

// pluginFiles returns the local marketplace's files: its manifest, the plugin's manifest,
// its skill, and its MCP server. Nothing else is written: no settings, hooks, permission
// rules, commands, or agents.
func (claude) pluginFiles(p PluginSpec) ([]File, error) {
	market, err := claudeJSONFile(claudeMarketplace{
		Name: p.Marketplace, Owner: claudeAuthor{Name: p.Author}, Description: p.Description,
		Plugins: []claudeMarketplaceEntry{{Name: p.Name, Source: "./" + p.Name, Description: p.Description, Version: p.Version}},
	})
	if err != nil {
		return nil, fmt.Errorf("write the plugin's marketplace: %w", err)
	}

	manifest, err := claudeJSONFile(claudePluginFile{
		Name: p.Name, Version: p.Version, Description: p.Description, Author: claudeAuthor{Name: p.Author},
	})
	if err != nil {
		return nil, fmt.Errorf("write the plugin's manifest: %w", err)
	}

	skill, err := claudeSkill(p.Skill)
	if err != nil {
		return nil, fmt.Errorf("write the plugin's skill: %w", err)
	}

	servers, err := claudeJSONFile(claudeMCPConfigOf(p.Server, ""))
	if err != nil {
		return nil, fmt.Errorf("write the plugin's MCP server: %w", err)
	}

	return []File{
		{Path: path.Join(claudePluginDir, claudeMarketplaceFile), Data: market},
		{Path: path.Join(p.Name, claudePluginDir, claudePluginManifest), Data: manifest},
		{Path: path.Join(p.Name, claudeSkillsDir, p.Skill.Name, claudeSkillFile), Data: skill},
		{Path: path.Join(p.Name, claudePluginMCP), Data: servers},
	}, nil
}

// claudeSkill returns a skill's SKILL.md: its name and description as YAML front matter,
// each quoted so no colon, quote, or other mark in it can change what the front matter says,
// then its instructions.
func claudeSkill(s PluginSkill) ([]byte, error) {
	name, err := yamlQuoted(s.Name)
	if err != nil {
		return nil, err
	}

	description, err := yamlQuoted(s.Description)
	if err != nil {
		return nil, err
	}

	return []byte("---\nname: " + name + "\ndescription: " + description + "\n---\n\n" +
		strings.TrimSpace(s.Body) + "\n"), nil
}

// yamlQuoted writes text as a double-quoted YAML scalar: a JSON string, which YAML reads as
// the same text, every quote, backslash, and control character escaped.
func yamlQuoted(text string) (string, error) {
	var b bytes.Buffer

	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(text); err != nil {
		return "", err
	}

	return strings.TrimSuffix(b.String(), "\n"), nil
}

// claudeJSONFile writes v as an indented JSON file Claude Code reads, ending in a newline,
// with no character escaped that JSON doesn't need escaped.
func claudeJSONFile(v any) ([]byte, error) {
	var b bytes.Buffer

	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	if err := enc.Encode(v); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

// pluginArgs returns a plugin command's arguments, each with --json.
func (claude) pluginArgs(op PluginOp, r PluginRef) []string {
	switch op {
	case PluginInstall:
		return []string{claudePlugin, claudePluginInstall, r.Name, claudeMarketplaceFlag, r.Dir, claudeJSON}
	case PluginUninstall:
		return []string{claudePlugin, claudePluginUninstall, r.Name + "@" + r.Marketplace, claudeJSON}
	case PluginRemoveMarketplace:
		return []string{claudePlugin, claudePluginMarketplace, claudePluginRemove, r.Marketplace, claudeJSON}
	case PluginList:
		return []string{claudePlugin, claudePluginList, claudeJSON}
	default:
		return nil
	}
}

// claudePluginLine is what a plugin command's line of JSON is read for; a command that
// failed names its plugin as plugin alone.
type claudePluginLine struct {
	Command     string `json:"command"`
	Outcome     string `json:"outcome"`
	PluginID    string `json:"pluginId"`
	Plugin      string `json:"plugin"`
	Message     string `json:"message"`
	FailureCode string `json:"failureCode"`
}

// ParsePluginResult reads what one of Claude Code's plugin commands printed on stdout with
// --json (PluginArgs): its one line of JSON, among blank lines and any line that isn't one.
// Its exit code says nothing more: a failed command exits 1, and its line says why.
func ParsePluginResult(stdout []byte) (PluginResult, error) {
	for line := range bytes.Lines(stdout) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}

		var l claudePluginLine
		if json.Unmarshal(line, &l) != nil || l.Outcome == "" {
			continue
		}

		return PluginResult{
			Command: l.Command, Outcome: PluginOutcome(l.Outcome), PluginID: cmp.Or(l.PluginID, l.Plugin),
			Message: l.Message, FailureCode: l.FailureCode,
		}, nil
	}

	return PluginResult{}, fmt.Errorf("%w: no line says how the command went", ErrPluginOutput)
}

// ParsePluginList reads what Claude Code's plugin list printed on stdout with --json: one
// JSON array, over as many lines as it takes, of every plugin installed; never nil.
func ParsePluginList(stdout []byte) ([]PluginListing, error) {
	for rest := stdout; len(rest) > 0; {
		line, next, _ := bytes.Cut(rest, []byte("\n"))

		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 && trimmed[0] == '[' {
			var list []PluginListing
			if json.NewDecoder(bytes.NewReader(rest)).Decode(&list) == nil {
				return textutil.NonNil(list), nil
			}
		}

		rest = next
	}

	return nil, fmt.Errorf("%w: no list of plugins", ErrPluginOutput)
}
