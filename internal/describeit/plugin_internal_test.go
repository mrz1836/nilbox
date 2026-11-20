package describeit

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/mcpserver"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// pluginCall is one plugin command the service ran: its argv, after the program, and the
// environment it ran with.
type pluginCall struct {
	args []string
	env  []string
}

// pluginRunner stands in for Claude Code's plugin commands: it answers each by its first
// arguments, from the answers the test gives, and records every call.
type pluginRunner struct {
	mu      sync.Mutex
	answers map[string]string
	exitErr error
	calls   []pluginCall
}

// runner returns the runner of the commands run with env.
func (p *pluginRunner) runner(env []string) execpath.Runner {
	return &testutil.Runner{Script: func(_ context.Context, argv []string) ([]byte, []byte, error) {
		p.mu.Lock()
		defer p.mu.Unlock()

		p.calls = append(p.calls, pluginCall{args: slices.Clone(argv[1:]), env: env})

		for prefix, out := range p.answers {
			if strings.HasPrefix(strings.Join(argv[1:], " "), prefix) {
				return []byte(out), []byte("a line for people\n"), p.exitErr
			}
		}

		return nil, []byte("unknown command\n"), errFake
	}}
}

// lines returns every call's arguments, a line each.
func (p *pluginRunner) lines() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]string, 0, len(p.calls))
	for _, c := range p.calls {
		out = append(out, strings.Join(c.args, " "))
	}

	return out
}

// pluginRig is a rig whose Claude Code's plugin commands answer as the test says, with
// two profiles.
func pluginRig(t *testing.T, answers map[string]string) (*rig, *pluginRunner) {
	t.Helper()

	r := newRig(t)
	r.agents.profiles = []Profile{{Name: defaultProfile, Default: true}, {Name: otherProfile}}
	r.agents.set(Start{Program: "/usr/local/bin/" + claudeProgram(t), Version: readyVersion(), Env: []string{"HOME=/home/user"}})

	p := &pluginRunner{answers: answers}
	r.svc.runner = p.runner

	return r, p
}

func TestThePluginCarriesThisBuildsWords(t *testing.T) {
	t.Parallel()

	p := Plugin("/data/describe/bin/"+brand.Slug+"-mcp", []string{"--a"})
	assert.Equal(t, brand.Slug, p.Name)
	assert.Equal(t, brand.Slug+"-app", p.Marketplace)
	assert.Equal(t, "1.0.0", p.Version)
	assert.Equal(t, "autopilot", p.Skill.Name)
	assert.Equal(t, kit.SkillText(kit.Outside), p.Skill.Body, "the skill is the words for the user's own sessions")
	assert.Contains(t, p.Skill.Description, "Autopilot", "it triggers on Autopilot's requests")
	assert.Equal(t, agent.ConversationSpec{Server: "autopilot", Command: "/data/describe/bin/" + brand.Slug + "-mcp", Args: []string{"--a"}},
		p.Server)

	_, err := agent.PluginFiles(agent.Claude, p)
	require.NoError(t, err, "the agent writes every file of it")
}

func TestThePluginsFolderIsRewrittenOnlyWhenItDiffers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := Plugin(filepath.Join(dir, "bin", brand.Slug+"-mcp"), []string{mcpserver.Arg(dir)})

	changed, err := WritePlugin(filepath.Join(dir, "plugin"), p)
	require.NoError(t, err)
	assert.True(t, changed)

	files, err := agent.PluginFiles(agent.Claude, p)
	require.NoError(t, err)

	for _, f := range files {
		got, readErr := os.ReadFile(filepath.Join(dir, "plugin", filepath.FromSlash(f.Path))) //nolint:gosec // the plugin's file
		require.NoError(t, readErr)
		assert.Equal(t, string(f.Data), string(got), f.Path)
	}

	skill := filepath.Join(dir, "plugin", brand.Slug, "skills", "autopilot", "SKILL.md")
	info, err := os.Stat(skill)
	require.NoError(t, err)

	changed, err = WritePlugin(filepath.Join(dir, "plugin"), p)
	require.NoError(t, err)
	assert.False(t, changed, "the same plugin writes nothing")

	again, err := os.Stat(skill)
	require.NoError(t, err)
	assert.Equal(t, info.ModTime(), again.ModTime())

	require.NoError(t, os.WriteFile(skill, []byte("old words"), 0o600))

	changed, err = WritePlugin(filepath.Join(dir, "plugin"), p)
	require.NoError(t, err)
	assert.True(t, changed, "a plugin with other words is written again, which loads in place")

	_, err = WritePlugin(filepath.Join(dir, "plugin"), Plugin("relative", nil))
	require.Error(t, err, "a server that isn't an absolute program is refused")
}

func TestTheLinkPointsAtAnAppInAStablePlace(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.stable = false
	r.svc.preparePlugin(t.Context())

	assert.FileExists(t, r.path("plugin", ".claude-plugin", "marketplace.json"), "the plugin's folder is written regardless")

	_, err := os.Lstat(r.path("bin", brand.Slug+"-mcp"))
	require.ErrorIs(t, err, os.ErrNotExist, "an app in a place that won't last leaves the link alone")

	r.stable = true
	r.svc.preparePlugin(t.Context())

	target, err := os.Readlink(r.path("bin", brand.Slug+"-mcp"))
	require.NoError(t, err)
	assert.Equal(t, r.exe, target)

	mcp, err := os.ReadFile(r.path("plugin", brand.Slug, ".mcp.json"))
	require.NoError(t, err)
	assert.Contains(t, string(mcp), r.path("bin", brand.Slug+"-mcp"), "the plugin runs the link")
	assert.Contains(t, string(mcp), mcpserver.Arg(r.dir), "in the tool mode, for this folder")

	moved := filepath.Join(t.TempDir(), "moved")
	testutil.WriteExecutable(t, moved, "")
	r.exe = moved
	r.svc.preparePlugin(t.Context())

	target, err = os.Readlink(r.path("bin", brand.Slug+"-mcp"))
	require.NoError(t, err)
	assert.Equal(t, moved, target, "an app that moved points the link at its new place")

	r.stable = false
	r.exe = filepath.Join(t.TempDir(), "dev-build")
	r.svc.preparePlugin(t.Context())

	target, err = os.Readlink(r.path("bin", brand.Slug+"-mcp"))
	require.NoError(t, err)
	assert.Equal(t, moved, target, "a build from source never takes the link over")
}

func TestInstallPluginRunsClaudeCodesInstallInTheProfile(t *testing.T) {
	t.Parallel()

	r, p := pluginRig(t, map[string]string{
		"plugin install": pluginFixture(t, "install-ok.json"),
		"plugin list":    pluginFixture(t, "list-installed.json"),
	})

	states, err := r.svc.InstallPlugin(t.Context(), otherProfile)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"plugin install " + brand.Slug + " --marketplace " + r.path("plugin") + " --json",
		"plugin list --json", "plugin list --json",
	}, p.lines(), "the install, then each profile's list")
	assert.Equal(t, []string{"HOME=/home/user"}, p.calls[0].env, "with the profile's environment")
	assert.Contains(t, r.agents.asked, otherProfile)

	assert.Equal(t, []PluginState{
		{Profile: defaultProfile, Default: true, Installed: true, Version: "0.0.1"},
		{Profile: otherProfile, Installed: true, Version: "0.0.1"},
	}, states)

	target, err := os.Readlink(r.path("bin", brand.Slug+"-mcp"))
	require.NoError(t, err)
	assert.Equal(t, r.exe, target, "the link points at the app before Claude Code reads the plugin")
}

func TestInstallPluginNeedsALinkThatLasts(t *testing.T) {
	t.Parallel()

	r, p := pluginRig(t, map[string]string{
		"plugin install": pluginFixture(t, "install-ok.json"), "plugin list": pluginFixture(t, "list-empty.json"),
	})
	r.stable = false

	_, err := r.svc.InstallPlugin(t.Context(), "")
	require.ErrorIs(t, err, errUnstable)
	assert.Contains(t, uierr.Message(err), "won't last")
	assert.Empty(t, p.lines(), "nothing is installed")

	require.NoError(t, os.MkdirAll(r.path("bin"), 0o700))
	require.NoError(t, os.Symlink(r.exe, r.path("bin", brand.Slug+"-mcp")))

	_, err = r.svc.InstallPlugin(t.Context(), "")
	require.NoError(t, err, "a link a stable app left still leads to it")
}

func TestInstallPluginSaysWhyClaudeCodeRefused(t *testing.T) {
	t.Parallel()

	r, _ := pluginRig(t, map[string]string{
		"plugin install": `{"command":"install","outcome":"failed","message":"Marketplace not reachable","failureCode":"x"}`,
	})

	_, err := r.svc.InstallPlugin(t.Context(), "")
	require.ErrorIs(t, err, errPlugin)
	assert.Equal(t, agentWords+" couldn't install the plugin: Marketplace not reachable.", uierr.Message(err))

	r, _ = pluginRig(t, map[string]string{"plugin install": "nothing of JSON"})

	_, err = r.svc.InstallPlugin(t.Context(), "")
	require.ErrorIs(t, err, agent.ErrPluginOutput)
	assert.Equal(t, agentWords+" couldn't install the plugin.", uierr.Message(err))
}

func TestRemovePluginUninstallsItAndItsMarketplace(t *testing.T) {
	t.Parallel()

	r, p := pluginRig(t, map[string]string{
		"plugin uninstall":          pluginFixture(t, "uninstall-ok.json"),
		"plugin marketplace remove": pluginFixture(t, "marketplace-remove-ok.json"),
		"plugin list":               pluginFixture(t, "list-empty.json"),
	})

	states, err := r.svc.RemovePlugin(t.Context(), defaultProfile)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"plugin uninstall " + brand.Slug + "@" + brand.Slug + "-app --json",
		"plugin marketplace remove " + brand.Slug + "-app --json", "plugin list --json", "plugin list --json",
	}, p.lines())
	assert.Equal(t, []PluginState{{Profile: defaultProfile, Default: true}, {Profile: otherProfile}}, states)

	r, _ = pluginRig(t, map[string]string{
		"plugin uninstall": pluginFixture(t, "uninstall-not-installed.json"), "plugin list": pluginFixture(t, "list-empty.json"),
	})

	_, err = r.svc.RemovePlugin(t.Context(), defaultProfile)
	require.NoError(t, err, "a plugin that isn't installed is removed already, its marketplace's failure only logged")

	r, _ = pluginRig(t, map[string]string{
		"plugin uninstall": `{"command":"uninstall","outcome":"failed","message":"Settings are locked","failureCode":"locked"}`,
	})

	_, err = r.svc.RemovePlugin(t.Context(), defaultProfile)
	require.ErrorIs(t, err, errPlugin)
	assert.Equal(t, agentWords+" couldn't remove the plugin: Settings are locked.", uierr.Message(err))

	r, _ = pluginRig(t, map[string]string{})

	_, err = r.svc.RemovePlugin(t.Context(), defaultProfile)
	require.Error(t, err)
	assert.Equal(t, agentWords+" couldn't remove the plugin.", uierr.Message(err))
}

func TestPluginsReadsEachProfilesOwnList(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.agents.profiles = []Profile{{Name: defaultProfile, Default: true}}
	r.claude(testutil.FakeRun{WantArgs: []string{"plugin", "list", "--json"}, Stdout: pluginFixture(t, "list-installed.json")})

	states, err := r.svc.Plugins(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []PluginState{{Profile: defaultProfile, Default: true, Installed: true, Version: "0.0.1"}}, states,
		"read from Claude Code's own list, run as a program")

	r.claude(testutil.FakeRun{Stdout: pluginFixture(t, "list-empty.json")})

	states, err = r.svc.Plugins(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []PluginState{{Profile: defaultProfile, Default: true}}, states)

	r.claude(testutil.FakeRun{Stdout: "not a list", Exit: 1})

	states, err = r.svc.Plugins(t.Context())
	require.NoError(t, err)
	assert.Equal(t, agentWords+" couldn't list its plugins.", states[0].Problem)
}

func TestPluginsWarnOnlyOfAListTheWindowStillWaitsFor(t *testing.T) {
	t.Parallel()

	r, _ := pluginRig(t, map[string]string{})

	var logs bytes.Buffer

	r.svc.logger = slog.New(slog.NewTextHandler(&logs, nil))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	states, err := r.svc.Plugins(ctx)
	require.NoError(t, err)
	require.Len(t, states, 2)
	assert.Equal(t, agentWords+" couldn't list its plugins.", states[0].Problem)
	assert.Empty(t, logs.String(), "the window moved on: no warning")

	_, err = r.svc.Plugins(t.Context())
	require.NoError(t, err)
	assert.Contains(t, logs.String(), "describe it couldn't list a profile's plugins", "a list that failed on its own is")
}

func TestPluginsSaysWhyAProfileCantBeRead(t *testing.T) {
	t.Parallel()

	r, _ := pluginRig(t, map[string]string{})

	r.agents.set(Start{Problem: agentWords + " isn't installed.", Fix: "Install it."})

	states, err := r.svc.Plugins(t.Context())
	require.NoError(t, err)
	require.Len(t, states, 2)
	assert.Equal(t, agentWords+" isn't installed. Install it.", states[0].Problem)

	r.agents.set(Start{Program: "/usr/local/bin/" + claudeProgram(t), Version: "2.0.0"})

	states, err = r.svc.Plugins(t.Context())
	require.NoError(t, err)
	assert.Contains(t, states[1].Problem, "is older than Describe it needs")

	r.agents.err = errFake

	states, err = r.svc.Plugins(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "Describe it couldn't find "+agentWords+".", states[0].Problem)

	r.agents.profErr = errFake

	states, err = r.svc.Plugins(t.Context())
	require.Error(t, err)
	assert.NotNil(t, states)

	bare := New(Config{Dir: t.TempDir()})

	states, err = bare.Plugins(t.Context())
	require.ErrorIs(t, err, errNoAgents)
	assert.NotNil(t, states)

	_, err = bare.InstallPlugin(t.Context(), "")
	require.ErrorIs(t, err, errUnstable)

	_, err = bare.RemovePlugin(t.Context(), "")
	require.ErrorIs(t, err, errNoAgents)
}

func TestEvalTextFillsInThisBuildsNames(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "tool: mcp__plugin_"+brand.Slug+"_autopilot__autopilot_propose in "+brand.Name+"'s tab",
		EvalText("tool: @TOOLS@autopilot_propose in @APP@'s tab"))
	assert.Equal(t, agent.PluginToolPrefix(agent.Claude, PluginName, PluginServer), EvalText("@TOOLS@"))
}
