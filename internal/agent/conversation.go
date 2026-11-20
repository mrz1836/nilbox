package agent

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Each agent's conversation half: how the app talks with it one headless turn at a time,
// with no tools of its own and the app's own MCP server alone (Describe it), and the
// plugin that brings the same server to the user's own sessions. An agent claims it by
// implementing conversationSpec; one that doesn't (Codex) holds no conversation.

// MCPConfigName is the file of a conversation's folder that holds a turn's MCP config, as
// ConversationFiles writes it and ConversationOptions.MCPConfig names it.
const MCPConfigName = "mcp.json"

const (
	// maxSessionName caps the name a conversation's session goes by in the user's own lists.
	maxSessionName = 100

	// maxPlainName caps a server's, a plugin's, a marketplace's, and a skill's name.
	maxPlainName = 64
)

var (
	// ErrNoConversation is returned for an agent the app holds no conversation with.
	ErrNoConversation = errors.New("the agent holds no conversation with the app")

	// ErrInvalidConversation is returned for a turn's options, or its files, a turn can't
	// start with.
	ErrInvalidConversation = errors.New("invalid conversation turn")

	// ErrInvalidPlugin is returned for a plugin, or a plugin command, that can't be written.
	ErrInvalidPlugin = errors.New("invalid plugin")

	// ErrPluginOutput is returned for what a plugin command printed when it isn't the JSON
	// it was asked for.
	ErrPluginOutput = errors.New("unreadable plugin command output")
)

// plainNamePattern is a name the agent's files and its tools' names take as it is:
// lowercase letters and digits, with single hyphens between them, starting with a letter,
// as the brand's slug is. It holds no underscore, which a tool's name joins its parts with.
var plainNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

// pluginVersionPattern is a plugin's version: major.minor.patch.
var pluginVersionPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$`)

// File is a file the agent reads, which the app writes for it: its path, relative to the
// folder it goes in and slash-separated, and its bytes.
type File struct {
	Path string
	Data []byte
}

// ConversationOptions are what one turn of a conversation passes its agent, besides the
// prompt, which it reads on stdin. There is no field for extra arguments: a profile's own
// never reach a conversation.
type ConversationOptions struct {
	// SessionID is the conversation's ID, a UUID the app minted, and Resume says the turn
	// takes it up again rather than starting it.
	SessionID string
	Resume    bool

	// Name is what the session is called in the user's own lists of sessions, or "".
	Name string

	// Model and Effort are "" for the agent's own default.
	Model  string
	Effort string

	// MCPConfig is the absolute path of the turn's MCP config (ConversationFiles), and
	// SystemPromptFile that of the file holding the instructions the turn reads beside the
	// agent's own system prompt.
	MCPConfig        string
	SystemPromptFile string

	// Server is the MCP server the config names, whose tools are the only ones the turn has
	// and every one of which it may call.
	Server string

	// BudgetUSD is the turn's budget in dollars, which the agent keeps to itself: one turn's,
	// never the conversation's.
	BudgetUSD float64
}

// ConversationSpec is the MCP server a turn, or a plugin, starts: its name, and the program
// it runs with its arguments, as the app's own executable in its tool mode. The agent never
// spells the tool mode's flags: they come in Args.
type ConversationSpec struct {
	Server  string
	Command string
	Args    []string
}

// PluginSpec is the plugin that brings the app's MCP server and its skill to the user's
// own sessions of the agent, in a local marketplace of its own.
type PluginSpec struct {
	// Name is the plugin's, and Marketplace that of the local marketplace that lists it,
	// the folder PluginFiles fills: plain lowercase names.
	Name        string
	Marketplace string

	// Version is the plugin's (major.minor.patch), Description what the plugin and its
	// marketplace say they are, and Author who makes it and owns the marketplace.
	Version     string
	Description string
	Author      string

	// Skill is the one skill it carries, and Server the MCP server its tools come from.
	Skill  PluginSkill
	Server ConversationSpec
}

// PluginSkill is a plugin's skill: its plain lowercase name, the description the agent
// reads to pick it, and its instructions.
type PluginSkill struct {
	Name        string
	Description string
	Body        string
}

// PluginOp is what one of the agent's plugin commands does.
type PluginOp string

// The plugin commands.
const (
	// PluginInstall installs the plugin from its local marketplace's folder, adding the
	// marketplace as it does.
	PluginInstall PluginOp = "install"

	// PluginUninstall uninstalls it.
	PluginUninstall PluginOp = "uninstall"

	// PluginRemoveMarketplace removes its local marketplace.
	PluginRemoveMarketplace PluginOp = "removeMarketplace"

	// PluginList lists every plugin installed.
	PluginList PluginOp = "list"
)

// PluginRef names what a plugin command acts on: the plugin, its marketplace, and the
// marketplace's folder (PluginFiles'), each as the command needs it.
type PluginRef struct {
	Name        string
	Marketplace string
	Dir         string
}

// PluginOutcome is how a plugin command says it went.
type PluginOutcome string

// The outcomes.
const (
	// PluginOK is a command that did what it was asked, or found it already done, as an
	// install of a plugin already installed.
	PluginOK PluginOutcome = "ok"

	// PluginFailed is one that didn't, its FailureCode saying why.
	PluginFailed PluginOutcome = "failed"
)

// PluginResult is what one of the agent's plugin commands says it did (ParsePluginResult).
type PluginResult struct {
	// Command is the command's own word for itself ("install", "uninstall",
	// "marketplace-remove"), and Outcome how it went.
	Command string        `json:"command"`
	Outcome PluginOutcome `json:"outcome"`

	// PluginID names the plugin it acted on, <plugin>@<marketplace>, or "".
	PluginID string `json:"pluginId"`

	// Message says what happened, in the agent's words, and FailureCode why a command
	// failed ("not_installed"), or "".
	Message     string `json:"message"`
	FailureCode string `json:"failureCode"`
}

// OK reports a command that did what it was asked.
func (r PluginResult) OK() bool {
	return r.Outcome == PluginOK
}

// PluginListing is a plugin as the agent's plugin list says it is installed
// (ParsePluginList).
type PluginListing struct {
	// ID is <plugin>@<marketplace>, Version the version installed, and Scope where it is
	// installed ("user").
	ID      string `json:"id"`
	Version string `json:"version"`
	Scope   string `json:"scope"`

	// Enabled reports it on, and ReadFromFolder is the folder it loads in place from, or ""
	// for one loaded from its copy.
	Enabled        bool   `json:"enabled"`
	ReadFromFolder string `json:"readFromFolder"`
}

// conversationSpec is the conversation half of an agent the app talks with one headless
// turn at a time, through its own MCP server alone.
type conversationSpec interface {
	spec

	// conversationFloor is the oldest version that takes every flag a turn and the plugin
	// commands pass, and reads every file below.
	conversationFloor() string

	// conversationArgs returns a turn's flags, in order, its options checked.
	conversationArgs(o ConversationOptions) []string

	// conversationFiles returns the files a turn's flags name, its server checked.
	conversationFiles(s ConversationSpec) ([]File, error)

	// toolPrefix starts the name of every tool of the server named server in a turn, and
	// pluginToolPrefix that of every tool of a plugin's server, both names checked.
	toolPrefix(server string) string
	pluginToolPrefix(plugin, server string) string

	// pluginFiles returns the local marketplace's files, holding p, which is checked.
	pluginFiles(p PluginSpec) ([]File, error)

	// pluginArgs returns the plugin command op, its arguments after the agent's program,
	// r checked for it.
	pluginArgs(op PluginOp, r PluginRef) []string
}

// conversationFor returns the conversation half of the agent with id, or ErrNoConversation.
func conversationFor(id ID) (conversationSpec, error) {
	s, err := specFor(id)
	if err != nil {
		return nil, err
	}

	c, ok := s.(conversationSpec)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoConversation, s.info().Name)
	}

	return c, nil
}

// ConversationFloor returns the oldest version of the agent with id that takes every flag a
// conversation's turn and the plugin commands pass, or "" for an agent that holds no
// conversation.
func ConversationFloor(id ID) string {
	c, err := conversationFor(id)
	if err != nil {
		return ""
	}

	return c.conversationFloor()
}

// ConversationArgs returns the flags a conversation's turn passes the agent with id, in
// order, once each option checks out: the stream, nobody to answer, the session started or
// resumed and its name, the model and effort, none of the agent's own tools and none of the
// user's or a folder's settings, the turn's MCP server alone and its every tool allowed, the
// budget, and the instructions. The prompt comes on stdin, never on the command line.
func ConversationArgs(id ID, o ConversationOptions) ([]string, error) {
	c, err := conversationFor(id)
	if err != nil {
		return nil, err
	}

	if err = checkConversation(id, o); err != nil {
		return nil, err
	}

	return c.conversationArgs(o), nil
}

// ConversationFiles returns the files a conversation's turn of the agent with id reads
// from its folder: for Claude Code, its MCP config (MCPConfigName), which names the server
// s alone.
func ConversationFiles(id ID, s ConversationSpec) ([]File, error) {
	c, err := conversationFor(id)
	if err != nil {
		return nil, err
	}

	if err = checkServer(s, ErrInvalidConversation); err != nil {
		return nil, err
	}

	return c.conversationFiles(s)
}

// ConversationToolPrefix returns what the name of every tool of the MCP server named server
// starts with, in a turn of the agent with id: every tool an in-app turn may use starts with
// it. It is "" for an agent that holds no conversation, or a name that isn't a server's,
// which no tool can be checked against.
func ConversationToolPrefix(id ID, server string) string {
	c, err := conversationFor(id)
	if err != nil || !plainName(server) {
		return ""
	}

	return c.toolPrefix(server)
}

// PluginToolPrefix returns what the name of every tool of the plugin's MCP server starts
// with, in the user's own sessions of the agent with id; "" as ConversationToolPrefix
// returns it.
func PluginToolPrefix(id ID, plugin, server string) string {
	c, err := conversationFor(id)
	if err != nil || !plainName(plugin) || !plainName(server) {
		return ""
	}

	return c.pluginToolPrefix(plugin, server)
}

// PluginFiles returns the files of the plugin's local marketplace for the agent with id,
// relative to the marketplace's folder, which the app keeps current and the agent loads
// in place: the marketplace that lists the plugin, the plugin's manifest, its skill, and
// its MCP server. The plugin carries no settings, hooks, or permission rules, so a session
// asks the user before every call of its tools.
func PluginFiles(id ID, p PluginSpec) ([]File, error) {
	c, err := conversationFor(id)
	if err != nil {
		return nil, err
	}

	if err = checkPlugin(p); err != nil {
		return nil, err
	}

	return c.pluginFiles(p)
}

// PluginArgs returns the arguments, after the agent's program, of the agent with id's
// plugin command op, each asking for its one line of JSON (ParsePluginResult, or
// ParsePluginList for PluginList): install needs r's plugin and its marketplace's folder,
// uninstall its plugin and marketplace, and the marketplace's removal its marketplace.
func PluginArgs(id ID, op PluginOp, r PluginRef) ([]string, error) {
	c, err := conversationFor(id)
	if err != nil {
		return nil, err
	}

	if err = checkPluginRef(op, r); err != nil {
		return nil, err
	}

	return c.pluginArgs(op, r), nil
}

// checkConversation checks a turn's options for the agent with id.
func checkConversation(id ID, o ConversationOptions) error {
	if !ValidSessionID(o.SessionID) {
		return fmt.Errorf("%w %q", ErrInvalidSessionID, o.SessionID)
	}

	if err := ValidateModel(o.Model); err != nil {
		return err
	}

	if err := ValidateEffort(id, o.Effort); err != nil {
		return err
	}

	if o.BudgetUSD <= 0 || math.IsNaN(o.BudgetUSD) || math.IsInf(o.BudgetUSD, 0) {
		return fmt.Errorf("%w: %v dollars, where a turn needs a budget of its own", ErrInvalidBudget, o.BudgetUSD)
	}

	return checkConversationValues(o)
}

// checkConversationValues checks a turn's files, its server, and its session's name.
func checkConversationValues(o ConversationOptions) error {
	switch {
	case !absolutePath(o.MCPConfig):
		return fmt.Errorf("%w: its MCP config must be an absolute path", ErrInvalidConversation)
	case !absolutePath(o.SystemPromptFile):
		return fmt.Errorf("%w: its instructions' file must be an absolute path", ErrInvalidConversation)
	case !plainName(o.Server):
		return fmt.Errorf("%w: %q isn't a plain lowercase server name", ErrInvalidConversation, o.Server)
	case o.Name != "" && !sessionName(o.Name):
		return fmt.Errorf("%w: a session's name is one line, at most %d characters, that doesn't start with -",
			ErrInvalidConversation, maxSessionName)
	default:
		return nil
	}
}

// checkServer checks an MCP server, failing with sentinel: a plain lowercase name, an
// absolute program, and arguments no process can refuse.
func checkServer(s ConversationSpec, sentinel error) error {
	switch {
	case !plainName(s.Server):
		return fmt.Errorf("%w: %q isn't a plain lowercase server name", sentinel, s.Server)
	case !absolutePath(s.Command):
		return fmt.Errorf("%w: the server's program must be an absolute path", sentinel)
	case slices.ContainsFunc(s.Args, func(a string) bool { return strings.ContainsRune(a, 0) }):
		return fmt.Errorf("%w: the server's arguments hold a NUL byte", sentinel)
	default:
		return nil
	}
}

// checkPlugin checks a plugin: its names, its version, its words, and its server.
func checkPlugin(p PluginSpec) error {
	switch {
	case !plainName(p.Name) || !plainName(p.Marketplace) || !plainName(p.Skill.Name):
		return fmt.Errorf("%w: the plugin, its marketplace, and its skill each need a plain lowercase name", ErrInvalidPlugin)
	case !pluginVersionPattern.MatchString(p.Version):
		return fmt.Errorf("%w: its version %q isn't major.minor.patch", ErrInvalidPlugin, p.Version)
	case !oneLine(p.Description) || !oneLine(p.Author) || !oneLine(p.Skill.Description):
		return fmt.Errorf("%w: its description, its author, and its skill's description are a line each", ErrInvalidPlugin)
	case strings.TrimSpace(p.Skill.Body) == "" || strings.ContainsRune(p.Skill.Body, 0):
		return fmt.Errorf("%w: its skill needs instructions", ErrInvalidPlugin)
	default:
		return checkServer(p.Server, ErrInvalidPlugin)
	}
}

// checkPluginRef checks that r names what the plugin command op needs.
func checkPluginRef(op PluginOp, r PluginRef) error {
	ok := true

	switch op {
	case PluginInstall:
		ok = plainName(r.Name) && absolutePath(r.Dir)
	case PluginUninstall:
		ok = plainName(r.Name) && plainName(r.Marketplace)
	case PluginRemoveMarketplace:
		ok = plainName(r.Marketplace)
	case PluginList:
	default:
		return fmt.Errorf("%w: no plugin command %q", ErrInvalidPlugin, op)
	}

	if !ok {
		return fmt.Errorf("%w: %s needs plain lowercase names and an absolute folder", ErrInvalidPlugin, op)
	}

	return nil
}

// plainName reports a name the agent's files and tools take as it is (plainNamePattern), at
// most maxPlainName characters.
func plainName(name string) bool {
	return len(name) <= maxPlainName && plainNamePattern.MatchString(name)
}

// absolutePath reports a clean absolute path, which no process can refuse.
func absolutePath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsRune(path, 0)
}

// sessionName reports a session's name the agent can't misread: one line of text, at most
// maxSessionName characters, that doesn't start with "-".
func sessionName(name string) bool {
	return oneLine(name) && !strings.HasPrefix(name, "-") && utf8.RuneCountInString(name) <= maxSessionName
}

// oneLine reports text with something to say, on one line and without control characters.
func oneLine(text string) bool {
	return strings.TrimSpace(text) != "" && !strings.ContainsFunc(text, unicode.IsControl)
}
