package describeit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/mcpserver"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The plugin that brings Describe it's tools to the user's own Claude Code: a local
// marketplace in the describe folder (pluginName), which Claude Code installs from and
// loads in place (the spike's S9), so the app keeps it current by rewriting the folder
// whenever this build's words differ. Its MCP server runs the link in the describe folder
// (binName), which only an app in a stable place points at itself, in the tool mode.
const (
	// PluginName is the plugin's name, and PluginMarketplace that of the local marketplace
	// that lists it: the plugin is <PluginName>@<PluginMarketplace> in Claude Code's lists.
	PluginName        = brand.Slug
	PluginMarketplace = brand.Slug + "-app"

	// PluginServer is the plugin's MCP server, and PluginSkill its one skill.
	PluginServer = "autopilot"
	PluginSkill  = "autopilot"

	// PluginVersion is the plugin's version: it changes only when the tool mode's protocol
	// does, as the words load in place whatever it says.
	PluginVersion = "1.0.0"

	// pluginTimeout bounds one of Claude Code's plugin commands.
	pluginTimeout = time.Minute
)

// pluginDescription is what the plugin and its marketplace say they are.
const pluginDescription = brand.Name + "'s Autopilot tools: describe what Autopilot should do for your GitHub pull " +
	"requests and issues, and the proposal waits for your review in " + brand.Name + "'s Autopilot tab."

// skillDescription is what the agent reads to pick the plugin's skill: any request of
// Autopilot's, and nothing else.
const skillDescription = "Use when the user wants " + brand.Name + "'s Autopilot to do something for them: a routine " +
	"that watches their GitHub pull requests or issues and acts on them (tells them, pins, snoozes, labels, comments, " +
	"reviews, merges, or runs a coding agent), a change to one of their routines, a prompt for one, Autopilot's " +
	"settings, or the rules routines lean on (trusted bots, pinned or muted repositories, ignored workflows). It " +
	"proposes the change for the user to review and apply in " + brand.Name + "'s Autopilot tab."

// Plugin returns the plugin that brings Describe it's tools to the user's own Claude Code,
// whose MCP server runs command with args: this build's skill (kit.SkillText for
// kit.Outside), its name, and its description.
func Plugin(command string, args []string) agent.PluginSpec {
	return agent.PluginSpec{
		Name: PluginName, Marketplace: PluginMarketplace, Version: PluginVersion, Description: pluginDescription,
		Author: brand.Name,
		Skill:  agent.PluginSkill{Name: PluginSkill, Description: skillDescription, Body: kit.SkillText(kit.Outside)},
		Server: agent.ConversationSpec{Server: PluginServer, Command: command, Args: textutil.NonNil(args)},
	}
}

// WritePlugin writes the plugin p's local marketplace into the folder dir (agent.PluginFiles),
// each file only when it holds something else, and reports whether any did. The plugin's
// own folder, where its eval suite goes, is <dir>/<PluginName>.
func WritePlugin(dir string, p agent.PluginSpec) (bool, error) {
	files, err := agent.PluginFiles(agent.Claude, p)
	if err != nil {
		return false, err
	}

	changed := false

	for _, f := range files {
		wrote, err := writeIfChanged(filepath.Join(dir, filepath.FromSlash(f.Path)), f.Data)
		if err != nil {
			return changed, err
		}

		changed = changed || wrote
	}

	return changed, nil
}

// EvalText returns a file of the plugin's eval suite with the names it leaves for the
// build to fill in: @TOOLS@, what the name of each of the plugin's tools starts with in a
// session (mcp__plugin_<plugin>_<server>__), and @APP@, the app's name.
func EvalText(text string) string {
	return strings.NewReplacer(
		"@TOOLS@", agent.PluginToolPrefix(agent.Claude, PluginName, PluginServer), "@APP@", brand.Name,
	).Replace(text)
}

// thisPlugin is the plugin as the describe folder keeps it: its server the link to the app,
// in the tool mode for this folder.
func (s *Service) thisPlugin() agent.PluginSpec {
	return Plugin(s.linkFile(), []string{mcpserver.Arg(s.cfg.Dir)})
}

// preparePlugin keeps the plugin current as the app starts: its folder rewritten when this
// build's differs, so an installed plugin loads this build's words at its next session
// (the spike's S9), and its link pointed at this app when it runs from a stable place.
func (s *Service) preparePlugin(ctx context.Context) {
	if _, err := WritePlugin(s.pluginDir(), s.thisPlugin()); err != nil {
		s.logger.WarnContext(ctx, "describe it couldn't write its plugin", "error", err)
	}

	if err := s.pointLink(); err != nil {
		s.logger.WarnContext(ctx, "describe it couldn't point its plugin at the app", "error", err)
	}
}

// stable reports an app that runs from a place that outlives it.
func (s *Service) stable() bool {
	return s.cfg.Stable != nil && s.cfg.Stable() && s.executable() != ""
}

// executable returns the app's own executable, "" when it can't be told.
func (s *Service) executable() string {
	if s.cfg.Executable == nil {
		return ""
	}

	exe := s.cfg.Executable()
	if !filepath.IsAbs(exe) {
		return ""
	}

	return exe
}

// pointLink points the plugin's link at this app, when it runs from a stable place and the
// link points elsewhere: through a link made beside it and renamed over it, so a session
// starting meanwhile finds one link or the other.
func (s *Service) pointLink() error {
	if !s.stable() {
		return nil
	}

	exe, link := s.executable(), s.linkFile()

	if current, err := os.Readlink(link); err == nil && current == exe {
		return nil
	}

	if err := ensureDir(filepath.Dir(link)); err != nil {
		return err
	}

	tmp := fmt.Sprintf("%s.%d.tmp", link, os.Getpid())
	_ = os.Remove(tmp)

	if err := os.Symlink(exe, tmp); err != nil {
		return err
	}

	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)

		return err
	}

	return nil
}

// linkUsable reports a stable link there to run: one this app can point at itself, or one
// a stable app left, which still leads to a program.
func (s *Service) linkUsable() bool {
	if s.stable() {
		return true
	}

	info, err := os.Stat(s.linkFile())

	return err == nil && info.Mode().IsRegular() && execpath.Executable(info.Mode())
}

// Plugins returns, for each of Claude Code's profiles, whether the plugin is installed
// there, read from Claude Code's own list of its plugins.
func (s *Service) Plugins(ctx context.Context) ([]PluginState, error) {
	profiles, err := s.profiles(ctx)
	if err != nil {
		return []PluginState{}, err
	}

	states := make([]PluginState, 0, len(profiles))

	for _, p := range profiles {
		states = append(states, s.pluginState(ctx, p))
	}

	return states, nil
}

// pluginState reads whether the plugin is installed in the profile p's Claude Code.
func (s *Service) pluginState(ctx context.Context, p Profile) PluginState {
	st := PluginState{Profile: p.Name, Default: p.Default}

	start, err := s.pluginStart(ctx, p.Name)
	if err != nil {
		st.Problem = uierr.Message(err)

		return st
	}

	stdout, stderr, err := s.pluginCommand(ctx, start, agent.PluginList, agent.PluginRef{})

	listing, perr := agent.ParsePluginList(stdout)
	if perr != nil {
		// A list the window stopped waiting for, as it moves on from Settings, is no news.
		if ctx.Err() == nil {
			s.logger.WarnContext(ctx, "describe it couldn't list a profile's plugins", "profile", p.Name, "error",
				errors.Join(err, perr), "stderr", textutil.Truncate(textutil.OneLine(string(stderr)), 300))
		}

		st.Problem = agentWords + " couldn't list its plugins."

		return st
	}

	id := PluginName + "@" + PluginMarketplace

	for _, l := range listing {
		if l.ID == id {
			st.Installed, st.Version = true, l.Version
		}
	}

	return st
}

// InstallPlugin installs the plugin in the profile's Claude Code: the plugin's folder
// written from this build (agent.PluginFiles), the link it runs pointed at this app, and
// Claude Code's own install run with the profile's environment. It returns every
// profile's state. An app that doesn't run from a stable place (a build from source, or
// one macOS translocated) can't point the link at itself, and says so.
func (s *Service) InstallPlugin(ctx context.Context, profile string) ([]PluginState, error) {
	if !s.linkUsable() {
		return []PluginState{}, uierr.New("This copy of "+brand.Name+" runs from a place that won't last (a build "+
			"from source, or a copy macOS moved aside), so the plugin can't run it. Install from "+brand.Name+" in "+
			"your Applications folder.", errUnstable)
	}

	if _, err := WritePlugin(s.pluginDir(), s.thisPlugin()); err != nil {
		return []PluginState{}, uierr.New("Describe it couldn't write its plugin.", err)
	}

	if err := s.pointLink(); err != nil {
		return []PluginState{}, uierr.New("Describe it couldn't point its plugin at "+brand.Name+".", err)
	}

	start, err := s.pluginStart(ctx, profile)
	if err != nil {
		return []PluginState{}, err
	}

	res, err := s.pluginResult(ctx, start, agent.PluginInstall, agent.PluginRef{Name: PluginName, Dir: s.pluginDir()})
	if err != nil {
		return []PluginState{}, uierr.New(agentWords+" couldn't install the plugin.", err)
	}

	if !res.OK() {
		return []PluginState{}, uierr.New(textutil.Sentence(agentWords+" couldn't install the plugin: "+
			textutil.Truncate(textutil.OneLine(res.Message), 300)), fmt.Errorf("%w: %s", errPlugin, res.FailureCode))
	}

	return s.Plugins(ctx)
}

// RemovePlugin uninstalls the plugin from the profile's Claude Code, and removes its
// marketplace there, and returns every profile's state.
func (s *Service) RemovePlugin(ctx context.Context, profile string) ([]PluginState, error) {
	start, err := s.pluginStart(ctx, profile)
	if err != nil {
		return []PluginState{}, err
	}

	res, err := s.pluginResult(ctx, start, agent.PluginUninstall,
		agent.PluginRef{Name: PluginName, Marketplace: PluginMarketplace})
	if err != nil {
		return []PluginState{}, uierr.New(agentWords+" couldn't remove the plugin.", err)
	}

	if !res.OK() && res.FailureCode != notInstalled {
		return []PluginState{}, uierr.New(textutil.Sentence(agentWords+" couldn't remove the plugin: "+
			textutil.Truncate(textutil.OneLine(res.Message), 300)), fmt.Errorf("%w: %s", errPlugin, res.FailureCode))
	}

	market, err := s.pluginResult(ctx, start, agent.PluginRemoveMarketplace,
		agent.PluginRef{Marketplace: PluginMarketplace})
	if err != nil || !market.OK() {
		s.logger.WarnContext(ctx, "describe it couldn't remove its plugin's marketplace", "profile", profile,
			"error", err, "message", textutil.Truncate(textutil.OneLine(market.Message), 300))
	}

	return s.Plugins(ctx)
}

// notInstalled is how Claude Code's uninstall names a plugin that isn't installed.
const notInstalled = "not_installed"

// pluginStart returns how to run Claude Code's plugin commands with profile: a Claude Code
// that is found and new enough, signed in or not.
func (s *Service) pluginStart(ctx context.Context, profile string) (Start, error) {
	start, err := s.conversationStart(ctx, profile)

	switch {
	case err != nil:
		return Start{}, err
	case start.Program == "" || !filepath.IsAbs(start.Program):
		problem := start.Problem
		if problem == "" {
			problem = agentWords + " isn't installed."
		}

		return Start{}, uierr.New(strings.TrimSpace(problem+" "+start.Fix), errNotReady)
	case belowFloor(start.Version):
		problem, fix := floorWords(start.Version)

		return Start{}, uierr.New(problem+" "+fix, errNotReady)
	default:
		return start, nil
	}
}

// pluginResult runs one of Claude Code's plugin commands and reads its line of JSON.
func (s *Service) pluginResult(ctx context.Context, start Start, op agent.PluginOp, r agent.PluginRef) (
	agent.PluginResult, error,
) {
	stdout, stderr, err := s.pluginCommand(ctx, start, op, r)

	res, perr := agent.ParsePluginResult(stdout)
	if perr != nil {
		why := textutil.Truncate(textutil.OneLine(string(stderr)), 300)

		return agent.PluginResult{}, fmt.Errorf("%w: %s", errors.Join(perr, err), why)
	}

	return res, nil
}

// pluginCommand runs one of Claude Code's plugin commands with the profile's environment,
// and returns what it printed: a failed command exits 1, and its line says why.
func (s *Service) pluginCommand(ctx context.Context, start Start, op agent.PluginOp, r agent.PluginRef) (
	[]byte, []byte, error,
) {
	args, err := agent.PluginArgs(agent.Claude, op, r)
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, pluginTimeout)
	defer cancel()

	return s.runner(start.Env).Run(ctx, start.Program, args...)
}
