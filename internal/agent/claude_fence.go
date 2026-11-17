package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// How Claude Code keeps the tools presets, as S4 measured them on Claude Code 2.1.325:
//
//   - A profile's own settings can allow Bash, Edit, Write, and WebFetch outright, which
//     undoes any "denied unless allowed" mode, so every fixed preset drops the user's,
//     the project's, and the local settings (--setting-sources "", or --restricted for
//     Read only), and loads none of a branch's hooks or MCP servers. Only the profile's own
//     preset keeps the user's settings, still without the branch's (--setting-sources user).
//   - Deny rules are no fence (sh -c 'git push' got through one), and neither is the auto
//     mode, which let a curl POST and another branch's push through. Each fixed preset runs
//     in dontAsk, which denies whatever no rule allows, and its allow rules name each
//     command; the exact push is an exact allow, and no push deny, which would beat it.
//   - The real boundaries are the fence hook, a PreToolUse hook that ran on every call and
//     held its denies over the user's allows, and the sandbox, which the edit presets run
//     every command in: writes kept to the worktree, its git folders, and the tools'
//     caches, the network to GitHub and the package registries. A sandbox setting it can't
//     read makes the run refuse to start; failIfUnavailable makes one that can't start a
//     failure too, and the hook holds every command until the sandbox check shows it held.
//   - Dropping the user's settings dropped their attribution-off setting, so a commit
//     carried an AI co-author trailer: the settings turn attribution off themselves.

// Claude Code's fence flags.
const (
	claudeSettingsFlag    = "--settings"
	claudeSettingSources  = "--setting-sources"
	claudeStrictMCPConfig = "--strict-mcp-config"
	claudeToolsFlag       = "--tools"
	claudeRestricted      = "--restricted"

	// claudeDontAsk is the --permission-mode value that denies whatever no rule allows.
	claudeDontAsk = "dontAsk"

	// claudeUserSettings keeps only the user's settings; "" keeps none.
	claudeUserSettings = "user"

	// claudeHookTimeout bounds one call of the fence hook, in seconds; a hook that fails
	// or runs out of time blocks its call (claudeHookBlocks).
	claudeHookTimeout = 30

	// claudeHookBlocks is the onFailure value that blocks a call whose hook fails.
	claudeHookBlocks = "block"
)

// claudeFenceFloor is the oldest Claude Code that takes every flag and settings key a
// fence uses (claudeFenceReviewed): each is in 2.1.319's help or settings, the version S1
// set the background runs' floor at. A flag or key added to a fence is reviewed against
// it, and the floor raised when the flag is newer.
const claudeFenceFloor = "2.1.319"

// FenceFloor returns the oldest version of the agent that keeps every tools preset, or ""
// for an agent that keeps none.
func FenceFloor(id ID) string {
	if ok, _ := KeepsPreset(id, fence.ReadOnly); !ok {
		return ""
	}

	return claudeFenceFloor
}

// claudeFenceReviewed lists every flag a fence's argv passes Claude Code and every key
// its settings write, as checked against claudeFenceFloor. A test fails when a preset
// uses one not listed here, or no longer uses one that is, so no fence changes without
// its floor's review.
func claudeFenceReviewed() []string {
	return []string{
		// Flags.
		"--allow-dangerously-skip-permissions", "--dangerously-skip-permissions", "--permission-mode",
		"--restricted", "--setting-sources", "--settings", "--strict-mcp-config", "--tools",
		// Settings keys.
		"attribution.commit", "attribution.pr", "disableAllHooks", "enableAllProjectMcpServers",
		"hooks.PreToolUse[].hooks[].command", "hooks.PreToolUse[].hooks[].onFailure",
		"hooks.PreToolUse[].hooks[].timeout", "hooks.PreToolUse[].hooks[].type", "hooks.PreToolUse[].matcher",
		"includeCoAuthoredBy", "permissions.allow", "permissions.deny", "permissions.disableBypassPermissionsMode",
		"sandbox.allowUnsandboxedCommands", "sandbox.autoAllowBashIfSandboxed", "sandbox.enabled",
		"sandbox.excludedCommands", "sandbox.failIfUnavailable", "sandbox.filesystem.allowWrite",
		"sandbox.filesystem.denyRead", "sandbox.filesystem.denyWrite", "sandbox.network.allowLocalBinding",
		"sandbox.network.allowedDomains",
	}
}

// claudeFenceArgs returns the flags of a fenced run's access: each fixed preset's own
// mode, tools, and settings sources, or the profile's own access with the user's settings
// alone; then no MCP server but those named (none), and the fence's settings.
func claudeFenceArgs(f *Fence) []string {
	if f == nil {
		return nil
	}

	return append(claudePresetFlags(f), claudeStrictMCPConfig, claudeSettingsFlag, f.Settings)
}

// claudePresetFlags returns a preset's own mode, tools, and settings sources.
func claudePresetFlags(f *Fence) []string {
	switch f.Preset {
	case fence.ReadOnly:
		return []string{claudeRestricted, claudeToolsFlag, "Read,Grep,Glob", claudePermissionMode, claudeDontAsk}
	case fence.GitHubTasks:
		return []string{claudeSettingSources, "", claudeToolsFlag, "Bash,Read", claudePermissionMode, claudeDontAsk}
	case fence.EditTest, fence.EditTestPush:
		return []string{
			claudeSettingSources, "", claudeToolsFlag, "Bash,Read,Edit,Write,Grep,Glob", claudePermissionMode, claudeDontAsk,
		}
	case fence.Profile:
		return append([]string{claudeSettingSources, claudeUserSettings}, claudeAccess(f.Base)...)
	default:
		return nil
	}
}

// backgroundReserved lists the flags no extra argument of a background run sets: the
// headless run's own, the fence's, and every one that would widen it (extra directories,
// plugins and MCP servers, other agents, the browser, files fetched at the start).
func (claude) backgroundReserved() []reservedFlag {
	fenced := []string{
		"--tools", "--allowedTools", "--allowed-tools", "--disallowedTools", "--disallowed-tools", "--restricted",
		"--safe-mode", "--bare", "--settings", "--setting-sources", "--strict-mcp-config", "--permission-prompts",
		"--permission-prompt-tool", "--output-format", "--input-format", "--max-budget-usd", "--json-schema",
		"--system-prompt", "--system-prompt-file", "--append-system-prompt", "--append-system-prompt-file",
		"--system-prompt-snapshot", "--include-partial-messages", "--include-hook-events", "--replay-user-messages",
		"--verbose", "--no-session-persistence", "--forward-subagent-text",
	}
	loosens := []string{
		"--add-dir", "--mcp-config", "--plugin-dir", "--plugin-url", "--agents", "--agent", "--chrome", "--ide",
		"--remote-control", "--file", "--brief", "--tmux",
	}

	out := make([]reservedFlag, 0, len(fenced)+len(loosens))

	for _, long := range fenced {
		out = append(out, reservedFlag{long: long, reason: reasonFence, access: true})
	}

	for _, long := range loosens {
		out = append(out, reservedFlag{long: long, reason: reasonLoosens, access: true})
	}

	return out
}

// fenceFiles returns Claude Code's settings for a fenced run.
func (claude) fenceFiles(f *Fence, built fence.Fence, program string) ([]fence.File, error) {
	runDir := strings.TrimSuffix(f.Settings, "/"+FenceSettingsName)
	if runDir == f.Settings {
		return nil, fmt.Errorf("%w: the settings must be the run folder's %s", ErrInvalidAccess, FenceSettingsName)
	}

	data, err := json.MarshalIndent(claudeFenceSettings(f, built, program, runDir), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("write the fence's settings: %w", err)
	}

	return []fence.File{{Name: FenceSettingsName, Data: append(data, '\n')}}, nil
}

// claudeSettings is the part of Claude Code's settings a fence writes, in its own names.
type claudeSettings struct {
	Permissions                claudePermissions `json:"permissions"`
	Hooks                      claudeHooks       `json:"hooks"`
	DisableAllHooks            bool              `json:"disableAllHooks"`
	Attribution                claudeAttribution `json:"attribution"`
	IncludeCoAuthoredBy        bool              `json:"includeCoAuthoredBy"`
	EnableAllProjectMcpServers bool              `json:"enableAllProjectMcpServers"`
	Sandbox                    *claudeSandbox    `json:"sandbox,omitempty"`
}

// claudePermissions are the fence's permission rules.
type claudePermissions struct {
	Allow                        []string `json:"allow"`
	Deny                         []string `json:"deny"`
	DisableBypassPermissionsMode string   `json:"disableBypassPermissionsMode,omitempty"`
}

// claudeHooks holds the fence hook, on every tool call.
type claudeHooks struct {
	PreToolUse []claudeHookMatcher `json:"PreToolUse"`
}

// claudeHookMatcher is a hook and the calls it runs on.
type claudeHookMatcher struct {
	Matcher string       `json:"matcher"`
	Hooks   []claudeHook `json:"hooks"`
}

// claudeHook is one command hook: a command line its shell runs, which names the app's
// executable and the hook mode's flag, each quoted as one word. It is the hook's shell
// form, which S4 ran on every call; the exec form (args) would leave the flag off on a
// build that doesn't know it, and the app would start rather than decide the call.
type claudeHook struct {
	Type      string `json:"type"`
	Command   string `json:"command"`
	Timeout   int    `json:"timeout"`
	OnFailure string `json:"onFailure"`
}

// claudeHookCommand is the fence hook's command line: the app's own executable in its
// hook mode, for the run's folder.
func claudeHookCommand(program, runDir string) string {
	return textutil.ShellQuote(program) + " " + textutil.ShellQuote(fence.HookArg(runDir))
}

// claudeAttribution is the text a commit and a pull request credit the agent with: none.
type claudeAttribution struct {
	Commit string `json:"commit"`
	PR     string `json:"pr"`
}

// claudeSandbox is Claude Code's sandbox for a run's commands.
type claudeSandbox struct {
	Enabled                  bool             `json:"enabled"`
	FailIfUnavailable        bool             `json:"failIfUnavailable"`
	AllowUnsandboxedCommands bool             `json:"allowUnsandboxedCommands"`
	AutoAllowBashIfSandboxed bool             `json:"autoAllowBashIfSandboxed"`
	ExcludedCommands         []string         `json:"excludedCommands"`
	Filesystem               claudeFilesystem `json:"filesystem"`
	Network                  claudeNetwork    `json:"network"`
}

// claudeFilesystem is what the sandbox's commands may write and read.
type claudeFilesystem struct {
	AllowWrite []string `json:"allowWrite"`
	DenyWrite  []string `json:"denyWrite"`
	DenyRead   []string `json:"denyRead"`
}

// claudeNetwork is what the sandbox's commands may reach. Local binding lets a test
// start a server on the loopback address, as Go's httptest does.
type claudeNetwork struct {
	AllowedDomains    []string `json:"allowedDomains"`
	AllowLocalBinding bool     `json:"allowLocalBinding"`
}

// claudeFenceSettings returns a fenced run's settings.
func claudeFenceSettings(f *Fence, built fence.Fence, program, runDir string) claudeSettings {
	p := built.Policy
	allow, deny := claudeRules(p)

	s := claudeSettings{
		Permissions: claudePermissions{Allow: allow, Deny: deny},
		Hooks: claudeHooks{PreToolUse: []claudeHookMatcher{{
			Matcher: "*",
			Hooks: []claudeHook{{
				Type: "command", Command: claudeHookCommand(program, runDir), Timeout: claudeHookTimeout,
				OnFailure: claudeHookBlocks,
			}},
		}}},
		Attribution: claudeAttribution{Commit: "", PR: ""},
	}

	if f.Preset.Fixed() {
		s.Permissions.DisableBypassPermissionsMode = "disable"
	}

	if sb := built.Sandbox; sb != nil {
		s.Sandbox = &claudeSandbox{
			Enabled: true, FailIfUnavailable: true, ExcludedCommands: claudeCommands(sb.Excluded, " *"),
			Filesystem: claudeFilesystem{AllowWrite: nonNil(sb.Write), DenyWrite: nonNil(sb.DenyWrite), DenyRead: nonNil(sb.DenyRead)},
			Network:    claudeNetwork{AllowedDomains: nonNil(sb.Domains), AllowLocalBinding: true},
		}
	}

	return s
}

// claudeRules returns a fenced run's allow and deny rules.
func claudeRules(p fence.Policy) (allow, deny []string) {
	allow = []string{}

	switch p.Preset {
	case fence.ReadOnly:
		allow = append(allow, "Read", "Grep", "Glob")
	case fence.GitHubTasks:
		allow = append(allow, "Read")
	case fence.EditTest, fence.EditTestPush:
		allow = append(allow, "Read", "Grep", "Glob")

		for _, wt := range p.Worktrees() {
			allow = append(allow, "Edit("+claudeAbs(wt)+"/**)", "Write("+claudeAbs(wt)+"/**)")
		}
	case fence.Profile:
	}

	for _, c := range fence.AllowedCommands(p) {
		allow = append(allow, claudeBash(c))
	}

	deny = []string{"WebFetch", "WebSearch"}

	for _, c := range fence.DeniedCommands(p) {
		deny = append(deny, claudeBash(c))
	}

	if !p.Preset.Edits() {
		return allow, append(deny, "Edit", "Write", "NotebookEdit")
	}

	for _, path := range fence.DeniedPaths(p) {
		deny = append(deny, "Edit("+claudeAbs(path)+")", "Write("+claudeAbs(path)+")")
	}

	return allow, deny
}

// claudeBash is a command's Bash rule: the exact words, or a prefix of words.
func claudeBash(c fence.Command) string {
	if c.Prefix {
		return "Bash(" + c.Words + ":*)"
	}

	return "Bash(" + c.Words + ")"
}

// claudeCommands writes commands as sandbox patterns: a prefix followed by any arguments
// (suffix), or the exact words.
func claudeCommands(cmds []fence.Command, suffix string) []string {
	out := make([]string, 0, len(cmds))

	for _, c := range cmds {
		if c.Prefix {
			out = append(out, c.Words+suffix)
		} else {
			out = append(out, c.Words)
		}
	}

	return out
}

// claudeAbs writes an absolute path as a rule names one: //path, since /path is from the
// settings file.
func claudeAbs(path string) string {
	return "/" + path
}

// nonNil returns list, or an empty list for nil, so the settings write [] rather than
// null.
func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}

	return list
}
