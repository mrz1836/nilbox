package agent

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
)

// The goldens' conversation: its ID and its folder in the app's data, its server, and the
// plugin for the user's own sessions.
const (
	testSession      = "00000000-0000-4000-8000-000000000001"
	testConversation = testDataDir + "/describe/conversations/" + testSession
	testServerName   = "app"
)

// testToolMode stands in for the arguments of the app's own executable in its tool mode,
// which the caller spells.
func testToolMode() []string {
	return []string{"--app-tools=" + testDataDir + "/describe", "--app-conversation=" + testSession}
}

// turnOptions are the golden first turn's.
func turnOptions() ConversationOptions {
	return ConversationOptions{
		SessionID: testSession, Name: "App: Describe it", Model: "haiku", Effort: "low",
		MCPConfig: testConversation + "/" + MCPConfigName, SystemPromptFile: testConversation + "/instructions.md",
		Server: testServerName, BudgetUSD: 0.5,
	}
}

// testServer is the golden turn's server: the app's own executable in its tool mode.
func testServer() ConversationSpec {
	return ConversationSpec{Server: testServerName, Command: testProgram, Args: testToolMode()}
}

// testPlugin is the golden plugin.
func testPlugin() PluginSpec {
	return PluginSpec{
		Name: "app", Marketplace: "app-local", Version: "1.0.0",
		Description: "Propose Autopilot routines, prompts, and settings for the app to review.", Author: "App",
		Skill: PluginSkill{
			Name:        "describe",
			Description: "Use when the user wants an Autopilot routine: describe it, check it, and propose it.",
			Body:        "# Describe it\n\nPropose every change with autopilot_propose; the user reviews each one.\n",
		},
		Server: ConversationSpec{Server: "autopilot", Command: testDataDir + "/describe/bin/app-mcp", Args: testToolMode()[:1]},
	}
}

// testRef names the golden plugin and its marketplace's folder.
func testRef() PluginRef {
	return PluginRef{Name: "app", Marketplace: "app-local", Dir: testDataDir + "/describe/plugin"}
}

// assertGolden checks got against the golden file at path, which -update rewrites.
func assertGolden(t *testing.T, path, got string) {
	t.Helper()

	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
	}

	want, err := os.ReadFile(path) //nolint:gosec // the test's own golden file
	require.NoError(t, err, "run with -update to write it")
	assert.Equal(t, string(want), got)
}

// conversationFixture returns one of the conversation spike's recordings.
func conversationFixture(t *testing.T, name string) []byte {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", "conversation", name)) //nolint:gosec // the package's own fixture
	require.NoError(t, err)

	return body
}

// valueOf returns the value the flag in args takes, which it passes once.
func valueOf(t *testing.T, args []string, flag string) string {
	t.Helper()

	i := slices.Index(args, flag)
	require.GreaterOrEqual(t, i, 0, "%s is passed", flag)
	require.Less(t, i+1, len(args), "%s takes a value", flag)
	assert.NotContains(t, args[i+1:], flag, "%s is passed once", flag)

	return args[i+1]
}

func TestTheConversationsArgv(t *testing.T) {
	t.Parallel()

	first, err := ConversationArgs(Claude, turnOptions())
	require.NoError(t, err)

	o := turnOptions()
	o.Resume = true

	resumed, err := ConversationArgs(Claude, o)
	require.NoError(t, err)

	var b strings.Builder

	b.WriteString("# The argv each turn of a conversation passes Claude Code, its prompt on stdin: the first turn,\n")
	b.WriteString("# then one that resumes it. Rewrite with\n")
	b.WriteString("# go test ./internal/agent -run TestTheConversationsArgv -update\n")

	for _, turn := range []struct {
		name string
		args []string
	}{{"first turn", first}, {"resumed turn", resumed}} {
		fmt.Fprintf(&b, "\n%s:\n", turn.name)

		for _, a := range turn.args {
			fmt.Fprintf(&b, "  %q\n", a)
		}
	}

	assertGolden(t, filepath.Join("testdata", "conversation", "argv.golden"), b.String())

	assert.Equal(t, testSession, valueOf(t, first, "--session-id"), "the first turn starts the conversation")
	assert.NotContains(t, first, "--resume")
	assert.Equal(t, testSession, valueOf(t, resumed, "--resume"), "a later turn takes it up again")
	assert.NotContains(t, resumed, "--session-id")
	assert.Equal(t, slices.Delete(slices.Clone(first), 6, 8), slices.Delete(slices.Clone(resumed), 6, 8),
		"every other flag is passed again")
}

func TestATurnHasNoToolsButItsServers(t *testing.T) {
	t.Parallel()

	for _, resume := range []bool{false, true} {
		o := turnOptions()
		o.Resume = resume

		args, err := ConversationArgs(Claude, o)
		require.NoError(t, err)

		assert.Empty(t, valueOf(t, args, "--tools"), "none of Claude Code's own tools")
		assert.Contains(t, args, "--restricted", "none of the user's, the project's, or the local settings")
		assert.Contains(t, args, "--strict-mcp-config", "no MCP server but its config's")
		assert.Equal(t, o.MCPConfig, valueOf(t, args, "--mcp-config"))
		assert.Equal(t, "dontAsk", valueOf(t, args, "--permission-mode"), "whatever no rule allows is denied")
		assert.Equal(t, "none", valueOf(t, args, "--permission-prompts"), "with nobody to answer")
		assert.Equal(t, "mcp__"+testServerName, valueOf(t, args, "--allowedTools"), "every tool of its server, and nothing else")
		assert.Equal(t, "0.5", valueOf(t, args, "--max-budget-usd"), "the turn's own budget")
		assert.Equal(t, o.SystemPromptFile, valueOf(t, args, "--append-system-prompt-file"))
		assert.Equal(t, "off", valueOf(t, args, "--system-prompt-snapshot"), "its instructions read fresh each turn")

		for _, loose := range []string{
			"--dangerously-skip-permissions", "--allow-dangerously-skip-permissions", "--add-dir", "--settings",
			"--setting-sources", "--plugin-dir", "--agents", "--continue", "--fork-session",
		} {
			assert.NotContains(t, args, loose)
		}

		for _, variadic := range []string{"--tools", "--mcp-config", "--allowedTools"} {
			i := slices.Index(args, variadic)
			require.Less(t, i+2, len(args), "%s's value is never last", variadic)
			assert.True(t, strings.HasPrefix(args[i+2], "--"), "another flag follows %s's one value, so it takes no other",
				variadic)
		}
	}

	bare := turnOptions()
	bare.Name, bare.Model, bare.Effort = "", "", ""

	args, err := ConversationArgs(Claude, bare)
	require.NoError(t, err)

	for _, unset := range []string{"--name", "--model", "--effort"} {
		assert.NotContains(t, args, unset, "what isn't set isn't passed")
	}
}

func TestATurnRefusesWhatItCantStartWith(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		edit func(o *ConversationOptions)
		err  error
	}{
		"no session":               {func(o *ConversationOptions) { o.SessionID = "" }, ErrInvalidSessionID},
		"a session that isn't one": {func(o *ConversationOptions) { o.SessionID = "last" }, ErrInvalidSessionID},
		"a model read as a flag": {
			func(o *ConversationOptions) { o.Model = "--dangerously-skip-permissions" }, ErrInvalidModel,
		},
		"an effort it doesn't take": {func(o *ConversationOptions) { o.Effort = "ultra" }, ErrInvalidEffort},
		"no budget":                 {func(o *ConversationOptions) { o.BudgetUSD = 0 }, ErrInvalidBudget},
		"a negative budget":         {func(o *ConversationOptions) { o.BudgetUSD = -1 }, ErrInvalidBudget},
		"a budget that isn't one":   {func(o *ConversationOptions) { o.BudgetUSD = math.NaN() }, ErrInvalidBudget},
		"an endless budget":         {func(o *ConversationOptions) { o.BudgetUSD = math.Inf(1) }, ErrInvalidBudget},
		"a relative MCP config":     {func(o *ConversationOptions) { o.MCPConfig = MCPConfigName }, ErrInvalidConversation},
		"an unclean MCP config": {
			func(o *ConversationOptions) { o.MCPConfig = testConversation + "/../x/" + MCPConfigName }, ErrInvalidConversation,
		},
		"no instructions":       {func(o *ConversationOptions) { o.SystemPromptFile = "" }, ErrInvalidConversation},
		"no server":             {func(o *ConversationOptions) { o.Server = "" }, ErrInvalidConversation},
		"a server in capitals":  {func(o *ConversationOptions) { o.Server = "App" }, ErrInvalidConversation},
		"a server's underscore": {func(o *ConversationOptions) { o.Server = "my_app" }, ErrInvalidConversation},
		"a name read as a flag": {func(o *ConversationOptions) { o.Name = "--continue" }, ErrInvalidConversation},
		"a name of two lines":   {func(o *ConversationOptions) { o.Name = "App\nDescribe it" }, ErrInvalidConversation},
		"a name past its cap": {
			func(o *ConversationOptions) { o.Name = strings.Repeat("x", maxSessionName+1) }, ErrInvalidConversation,
		},
	} {
		o := turnOptions()
		tc.edit(&o)

		_, err := ConversationArgs(Claude, o)
		require.ErrorIs(t, err, tc.err, name)
	}

	_, err := ConversationArgs(Codex, turnOptions())
	require.ErrorIs(t, err, ErrNoConversation, "Codex holds no conversation with the app")

	_, err = ConversationArgs("aider", turnOptions())
	require.ErrorIs(t, err, ErrUnknownAgent)
}

func TestTheConversationsFiles(t *testing.T) {
	t.Parallel()

	turn, err := ConversationFiles(Claude, testServer())
	require.NoError(t, err)

	plugin, err := PluginFiles(Claude, testPlugin())
	require.NoError(t, err)

	var b strings.Builder

	b.WriteString("# The files a conversation's turn reads from its folder, then those of the plugin for the user's\n")
	b.WriteString("# own Claude Code, from its marketplace's folder. Rewrite with\n")
	b.WriteString("# go test ./internal/agent -run TestTheConversationsFiles -update\n")

	for _, f := range slices.Concat(turn, plugin) {
		fmt.Fprintf(&b, "\n%s:\n%s", f.Path, f.Data)
	}

	assertGolden(t, filepath.Join("testdata", "conversation", "files.golden"), b.String())

	require.Len(t, turn, 1)
	assert.Equal(t, MCPConfigName, turn[0].Path)

	var config struct {
		MCPServers map[string]struct {
			Type    string   `json:"type"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}

	require.NoError(t, json.Unmarshal(turn[0].Data, &config))
	require.Len(t, config.MCPServers, 1, "the turn's server alone")
	assert.Equal(t, "stdio", config.MCPServers[testServerName].Type)
	assert.Equal(t, testProgram, config.MCPServers[testServerName].Command)
	assert.Equal(t, testToolMode(), config.MCPServers[testServerName].Args, "the caller's arguments, as they came")

	paths := make([]string, 0, len(plugin))
	for _, f := range plugin {
		paths = append(paths, f.Path)
	}

	assert.Equal(t, []string{
		".claude-plugin/marketplace.json", "app/.claude-plugin/plugin.json", "app/skills/describe/SKILL.md", "app/.mcp.json",
	}, paths, "the marketplace, and its one plugin with its skill and its server: no settings, hooks, or rules")

	noArgs := testServer()
	noArgs.Args = nil

	files, err := ConversationFiles(Claude, noArgs)
	require.NoError(t, err)
	assert.Contains(t, string(files[0].Data), `"args": []`, "a server with no arguments lists none, never null")
}

func TestAServerTheAgentCantStartIsRefused(t *testing.T) {
	t.Parallel()

	for name, edit := range map[string]func(s *ConversationSpec){
		"no name":                func(s *ConversationSpec) { s.Server = "" },
		"a name with a dot":      func(s *ConversationSpec) { s.Server = "app.dev" },
		"a relative program":     func(s *ConversationSpec) { s.Command = "app" },
		"an argument with a NUL": func(s *ConversationSpec) { s.Args = []string{"a\x00b"} },
	} {
		s := testServer()
		edit(&s)

		_, err := ConversationFiles(Claude, s)
		require.ErrorIs(t, err, ErrInvalidConversation, name)

		p := testPlugin()
		p.Server = s

		_, err = PluginFiles(Claude, p)
		require.ErrorIs(t, err, ErrInvalidPlugin, name)
	}

	_, err := ConversationFiles(Codex, testServer())
	require.ErrorIs(t, err, ErrNoConversation)
}

// frontMatter returns a SKILL.md's front matter as its keys' raw values, and its body.
func frontMatter(t *testing.T, data []byte) (map[string]string, string) {
	t.Helper()

	rest, ok := strings.CutPrefix(string(data), "---\n")
	require.True(t, ok, "the file opens with its front matter")

	head, body, ok := strings.Cut(rest, "\n---\n")
	require.True(t, ok, "the front matter is closed")

	keys := map[string]string{}

	for line := range strings.Lines(head) {
		key, value, found := strings.Cut(strings.TrimSuffix(line, "\n"), ": ")
		require.True(t, found, "each line is a key and its value: %q", line)

		keys[key] = value
	}

	return keys, body
}

func TestTheSkillsFrontMatterSaysWhatItWasGiven(t *testing.T) {
	t.Parallel()

	for _, description := range []string{
		"Use when the user wants a routine: describe it.", `Says "propose", then 'check'.`, "# not a comment",
		`a \ backslash`, "---", "key: value # and a comment", "no, yes, true", "émoji 🚀 and <b>&</b>", "[a, list]",
		"{a: map}", "- an item", "*anchor", "!tag", "| block", "> folded", "@at", "%percent", "`backtick`",
	} {
		p := testPlugin()
		p.Skill.Description = description

		files, err := PluginFiles(Claude, p)
		require.NoError(t, err, description)

		keys, body := frontMatter(t, files[2].Data)
		assert.Len(t, keys, 2, "%s: its name and description alone", description)

		var name, got string

		require.NoError(t, json.Unmarshal([]byte(keys["name"]), &name), "the name is quoted")
		require.NoError(t, json.Unmarshal([]byte(keys["description"]), &got), "the description is quoted: %s", description)
		assert.Equal(t, p.Skill.Name, name)
		assert.Equal(t, description, got, "a double-quoted YAML scalar reads back as the text it was given")
		assert.Equal(t, "\n# Describe it\n\nPropose every change with autopilot_propose; the user reviews each one.\n", body)
	}
}

func TestAPluginTheAgentCantLoadIsRefused(t *testing.T) {
	t.Parallel()

	for name, edit := range map[string]func(p *PluginSpec){
		"no name":                      func(p *PluginSpec) { p.Name = "" },
		"a name with a slash":          func(p *PluginSpec) { p.Name = "../app" },
		"a marketplace in capitals":    func(p *PluginSpec) { p.Marketplace = "App-Local" },
		"a skill with a space":         func(p *PluginSpec) { p.Skill.Name = "describe it" },
		"no version":                   func(p *PluginSpec) { p.Version = "" },
		"a version that isn't semver":  func(p *PluginSpec) { p.Version = "v1" },
		"no description":               func(p *PluginSpec) { p.Description = " " },
		"a description of two lines":   func(p *PluginSpec) { p.Description = "a\nb" },
		"no author":                    func(p *PluginSpec) { p.Author = "" },
		"a skill with no description":  func(p *PluginSpec) { p.Skill.Description = "" },
		"a skill with no instructions": func(p *PluginSpec) { p.Skill.Body = "\n" },
		"a server with no program":     func(p *PluginSpec) { p.Server.Command = "" },
	} {
		p := testPlugin()
		edit(&p)

		_, err := PluginFiles(Claude, p)
		require.ErrorIs(t, err, ErrInvalidPlugin, name)
	}

	_, err := PluginFiles(Codex, testPlugin())
	require.ErrorIs(t, err, ErrNoConversation)
}

func TestThePluginCommands(t *testing.T) {
	t.Parallel()

	r := testRef()

	for op, want := range map[PluginOp][]string{
		PluginInstall:           {"plugin", "install", "app", "--marketplace", r.Dir, "--json"},
		PluginUninstall:         {"plugin", "uninstall", "app@app-local", "--json"},
		PluginRemoveMarketplace: {"plugin", "marketplace", "remove", "app-local", "--json"},
		PluginList:              {"plugin", "list", "--json"},
	} {
		got, err := PluginArgs(Claude, op, r)
		require.NoError(t, err, op)
		assert.Equal(t, want, got, op)
	}

	list, err := PluginArgs(Claude, PluginList, PluginRef{})
	require.NoError(t, err, "listing needs no plugin")
	assert.Equal(t, []string{"plugin", "list", "--json"}, list)

	for name, tc := range map[string]struct {
		op  PluginOp
		ref PluginRef
	}{
		"an install with no folder":      {PluginInstall, PluginRef{Name: "app"}},
		"an install from a relative one": {PluginInstall, PluginRef{Name: "app", Dir: "plugin"}},
		"an install of a flag":           {PluginInstall, PluginRef{Name: "--all", Dir: r.Dir}},
		"an uninstall with no market":    {PluginUninstall, PluginRef{Name: "app"}},
		"a removal of a flag":            {PluginRemoveMarketplace, PluginRef{Marketplace: "-y"}},
		"a command it doesn't know":      {PluginOp("update"), r},
	} {
		_, err = PluginArgs(Claude, tc.op, tc.ref)
		require.ErrorIs(t, err, ErrInvalidPlugin, name)
	}

	_, err = PluginArgs(Codex, PluginList, r)
	require.ErrorIs(t, err, ErrNoConversation)
}

func TestAPluginCommandsLineSaysHowItWent(t *testing.T) {
	t.Parallel()

	market := brand.Slug + "-local"
	id := brand.Slug + "@" + market

	for name, want := range map[string]PluginResult{
		"install-ok.json": {
			Command: "install", Outcome: PluginOK, PluginID: id,
			Message: "Successfully installed plugin: " + id + " (scope: user)",
		},
		"install-already.json": {
			Command: "install", Outcome: PluginOK, PluginID: id,
			Message: `Plugin "` + id + `" is already installed (scope: user) — it loads in place from ` +
				"/data/describe/plugin, so edits there take effect at the next session start or /reload-plugins",
		},
		"uninstall-ok.json": {
			Command: "uninstall", Outcome: PluginOK, PluginID: id,
			Message: "Successfully uninstalled plugin: " + brand.Slug + " (scope: user)",
		},
		"uninstall-not-installed.json": {
			Command: "uninstall", Outcome: PluginFailed, PluginID: id, FailureCode: "not_installed",
			Message: `Plugin "` + id + `" not found in installed plugins`,
		},
		"marketplace-add-ok.json": {
			Command: "marketplace-add", Outcome: PluginOK,
			Message: "Successfully added marketplace: " + market + " (declared in user settings)",
		},
		"marketplace-remove-ok.json": {
			Command: "marketplace-remove", Outcome: PluginOK, Message: "Successfully removed marketplace: " + market,
		},
		"update-ok.json": {
			Command: "update", Outcome: PluginOK, PluginID: id,
			Message: `Plugin "` + brand.Slug + `" updated from 0.0.1 to 0.0.2 for scope user. Restart to apply changes.`,
		},
	} {
		line := conversationFixture(t, filepath.Join("plugin", name))

		for _, out := range [][]byte{line, append([]byte("\n\nSome words a person reads\n"), line...)} {
			got, err := ParsePluginResult(out)
			require.NoError(t, err, name)
			assert.Equal(t, want, got, name)
			assert.Equal(t, want.Outcome == PluginOK, got.OK(), name)
		}
	}

	for _, out := range []string{"", "\n\n", "not json\n", "{}\n", `{"command":"install"}` + "\n", "[]\n"} {
		_, err := ParsePluginResult([]byte(out))
		require.ErrorIs(t, err, ErrPluginOutput, "%q says nothing of how a command went", out)
	}
}

func TestThePluginListIsRead(t *testing.T) {
	t.Parallel()

	installed := conversationFixture(t, filepath.Join("plugin", "list-installed.json"))
	want := []PluginListing{{
		ID: brand.Slug + "@" + brand.Slug + "-local", Version: "0.0.1", Scope: "user", Enabled: true,
		ReadFromFolder: "/data/describe/plugin",
	}}

	for _, out := range [][]byte{
		installed, append([]byte("\n[warn] some words a person reads\n"), installed...),
	} {
		got, err := ParsePluginList(out)
		require.NoError(t, err)
		assert.Equal(t, want, got, "each plugin's ID, version, scope, switch, and the folder it loads in place from")
	}

	empty, err := ParsePluginList(conversationFixture(t, filepath.Join("plugin", "list-empty.json")))
	require.NoError(t, err)
	assert.NotNil(t, empty, "no plugin is an empty list, never nil")
	assert.Empty(t, empty)

	for _, out := range []string{"", "\n", "not json\n", `{"id":"x"}` + "\n", "[ not json\n"} {
		_, err = ParsePluginList([]byte(out))
		require.ErrorIs(t, err, ErrPluginOutput, "%q is no list", out)
	}
}

func TestTheToolPrefixes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "mcp__app__", ConversationToolPrefix(Claude, "app"))
	assert.Equal(t, "mcp__plugin_app_autopilot__", PluginToolPrefix(Claude, "app", "autopilot"),
		"a plugin's tools are named for the plugin and its server")

	args, err := ConversationArgs(Claude, turnOptions())
	require.NoError(t, err)
	assert.Equal(t, ConversationToolPrefix(Claude, testServerName), valueOf(t, args, "--allowedTools")+"__",
		"the tools a turn's allow rule names are the ones its prefix starts")

	for _, prefix := range []string{
		ConversationToolPrefix(Claude, ""), ConversationToolPrefix(Claude, "my_app"), ConversationToolPrefix(Codex, "app"),
		ConversationToolPrefix("aider", "app"), PluginToolPrefix(Claude, "app", ""), PluginToolPrefix(Claude, "a_b", "c"),
		PluginToolPrefix(Codex, "app", "autopilot"),
	} {
		assert.Empty(t, prefix, "no prefix for a name that isn't one, or an agent that holds no conversation")
	}
}

func TestTheConversationsFloorIsTheVersionItRanOn(t *testing.T) {
	t.Parallel()

	assert.Equal(t, claudeConversationFloor, ConversationFloor(Claude))
	assert.Equal(t, ParseVersion(conversationFixture(t, "version.txt")), ConversationFloor(Claude),
		"the floor is the version the spike ran every flag and file on")
	assert.Empty(t, ConversationFloor(Codex))
	assert.Empty(t, ConversationFloor("aider"))
}

// usedByConversations records every flag a turn passes, every plugin command, and every
// key the files write, in claudeConversationReviewed's words.
func usedByConversations(t *testing.T) map[string]bool {
	t.Helper()

	used := map[string]bool{}

	for _, resume := range []bool{false, true} {
		o := turnOptions()
		o.Resume = resume

		args, err := ConversationArgs(Claude, o)
		require.NoError(t, err)

		for _, a := range args {
			if strings.HasPrefix(a, "-") {
				used[a] = true
			}
		}
	}

	for _, op := range []PluginOp{PluginInstall, PluginUninstall, PluginRemoveMarketplace, PluginList} {
		used[pluginCommand(t, op)] = true
	}

	turn, err := ConversationFiles(Claude, testServer())
	require.NoError(t, err)

	plugin, err := PluginFiles(Claude, testPlugin())
	require.NoError(t, err)

	p := testPlugin()
	names := strings.NewReplacer(
		p.Name+"/", "<plugin>/", "/"+p.Skill.Name+"/", "/<skill>/", "."+testServerName+".", ".<server>.",
		"."+p.Server.Server+".", ".<server>.",
	)

	for _, f := range slices.Concat(turn, plugin) {
		for _, key := range fileKeys(t, f) {
			used[names.Replace(f.Path+": "+key)] = true
		}
	}

	return used
}

// pluginCommand returns the plugin command op of the golden plugin, its names and its
// folder in claudeConversationReviewed's words.
func pluginCommand(t *testing.T, op PluginOp) string {
	t.Helper()

	r := testRef()
	words := map[string]string{
		r.Name: "<plugin>", r.Marketplace: "<marketplace>", r.Dir: "<dir>",
		r.Name + "@" + r.Marketplace: "<plugin>@<marketplace>",
	}

	args, err := PluginArgs(Claude, op, r)
	require.NoError(t, err)

	for i, a := range args {
		if word, ok := words[a]; ok {
			args[i] = word
		}
	}

	return strings.Join(args, " ")
}

// fileKeys returns the keys a file writes: a JSON file's key paths, or a SKILL.md's front
// matter's keys.
func fileKeys(t *testing.T, f File) []string {
	t.Helper()

	keys := map[string]bool{}

	if strings.HasSuffix(f.Path, ".md") {
		front, _ := frontMatter(t, f.Data)
		for key := range front {
			keys[key] = true
		}
	} else {
		var v any
		require.NoError(t, json.Unmarshal(f.Data, &v), f.Path)
		settingsKeys(v, "", keys)
	}

	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}

	sort.Strings(out)

	return out
}

func TestAConversationsFlagsCantChangeWithoutTheFloorsReview(t *testing.T) {
	t.Parallel()

	used := usedByConversations(t)

	got := make([]string, 0, len(used))
	for k := range used {
		got = append(got, k)
	}

	sort.Strings(got)

	reviewed := slices.Sorted(slices.Values(claudeConversationReviewed()))
	assert.Equal(t, reviewed, got, "a conversation's flags, plugin commands, or files changed: review each against "+
		"Claude Code %s (its help, and its plugin and MCP config formats), raise claudeConversationFloor if one is "+
		"newer, and list it in claudeConversationReviewed", claudeConversationFloor)
}
