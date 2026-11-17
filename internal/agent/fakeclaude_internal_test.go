package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/fence"
)

// fakeClaude evaluates tool calls the way Claude Code decided them in S4, from a run's
// whole argv and the settings it names: which tools there are, then the fence hook (run
// for real, in its hook mode), then the deny rules, then the allow rules (the settings',
// and the profile's own when its user settings load), then the permission mode, under
// --permission-prompts none, where whatever would prompt is denied. Its sandbox writes the
// sandbox check's proof, and refuses its probe, as the real one did.
type fakeClaude struct {
	argv       []string
	tools      []string
	restricted bool
	mode       string
	sources    []string
	allSources bool
	strictMCP  bool
	addDirs    []string
	settings   claudeSettings
	cwd        string

	// profileAllows are the rules the owner's profiles allow in their own settings (S4).
	profileAllows []string
}

// newFakeClaude reads a run's argv, and the settings file it names, from disk.
func newFakeClaude(t *testing.T, argv []string, cwd string) *fakeClaude {
	t.Helper()

	f := &fakeClaude{
		argv: argv, allSources: true, mode: "default", cwd: cwd,
		profileAllows: []string{"Bash", "Edit", "Write", "WebFetch"},
	}

	for i := 0; i < len(argv); i++ {
		a := argv[i]
		value := func() string {
			require.Less(t, i+1, len(argv), "%s takes a value", a)

			i++

			return argv[i]
		}

		if !f.readValueFlag(t, a, value) {
			f.readSwitch(a)
		}
	}

	return f
}

// readValueFlag reads a flag that takes a value, and reports one it read.
func (f *fakeClaude) readValueFlag(t *testing.T, a string, value func() string) bool {
	t.Helper()

	switch a {
	case "--tools":
		f.tools = strings.Split(value(), ",")
	case "--permission-mode":
		f.mode = value()
	case "--setting-sources":
		f.allSources = false
		if v := value(); v != "" {
			f.sources = strings.Split(v, ",")
		}
	case "--settings":
		data, err := os.ReadFile(value())
		require.NoError(t, err)

		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		require.NoError(t, dec.Decode(&f.settings))
	default:
		return false
	}

	return true
}

// readSwitch reads a flag that takes no value.
func (f *fakeClaude) readSwitch(a string) {
	switch {
	case a == "--restricted":
		f.restricted = true
	case a == "--dangerously-skip-permissions":
		f.mode = "bypassPermissions"
	case a == "--strict-mcp-config":
		f.strictMCP = true
	case strings.HasPrefix(a, "--add-dir"):
		f.addDirs = append(f.addDirs, a)
	}
}

// userSettings reports the profile's own settings loaded.
func (f *fakeClaude) userSettings() bool {
	return !f.restricted && (f.allSources || slices.Contains(f.sources, "user"))
}

// projectSettings reports a branch's own settings, hooks, and MCP servers loaded.
func (f *fakeClaude) projectSettings() bool {
	return !f.restricted && (f.allSources || slices.Contains(f.sources, "project"))
}

// available reports a tool the run has.
func (f *fakeClaude) available(tool string) bool {
	if f.tools != nil {
		return slices.Contains(f.tools, tool)
	}

	if f.restricted {
		return tool != "Bash" && tool != "WebFetch"
	}

	return true
}

// decide returns whether the call runs, and what decided it.
func (f *fakeClaude) decide(t *testing.T, c fence.Call) (bool, string) {
	t.Helper()

	if !f.available(c.Tool) {
		return false, "no such tool"
	}

	if denied := f.runHooks(t, c); denied {
		return false, "hook"
	}

	if f.matchesAny(f.settings.Permissions.Deny, c, false) {
		return false, "deny rule"
	}

	allows := slices.Clone(f.settings.Permissions.Allow)
	if f.userSettings() {
		allows = append(allows, f.profileAllows...)
	}

	if f.matchesAny(allows, c, true) {
		f.sandboxRun(c)

		return true, "allow rule"
	}

	if f.modeAllows(c) {
		f.sandboxRun(c)

		return true, "mode"
	}

	return false, "nothing allows it"
}

// modeAllows reports a call the permission mode lets through when no rule decides it:
// bypassPermissions and auto, every call, as S4 found; acceptEdits, an edit in the folder.
func (f *fakeClaude) modeAllows(c fence.Call) bool {
	switch f.mode {
	case "bypassPermissions", "auto":
		return true
	case "acceptEdits":
		return (c.Tool == "Edit" || c.Tool == "Write") && strings.HasPrefix(c.Path, f.cwd+"/")
	default:
		return false
	}
}

// runHooks runs the settings' PreToolUse hooks through the hook mode, as the agent would,
// and reports a call one denied.
func (f *fakeClaude) runHooks(t *testing.T, c fence.Call) bool {
	t.Helper()

	for _, m := range f.settings.Hooks.PreToolUse {
		for _, h := range m.Hooks {
			input := map[string]any{"command": c.Command, "file_path": c.Path}
			if c.Tool == "Glob" {
				input = map[string]any{"pattern": c.Pattern, "path": c.Path}
			}

			payload, err := json.Marshal(map[string]any{
				"hook_event_name": "PreToolUse", "tool_name": c.Tool, "tool_input": input, "cwd": f.cwd,
			})
			require.NoError(t, err)

			words := shellWords(t, h.Command)
			require.Len(t, words, 2, "the hook is the app's executable and its flag")
			require.Equal(t, testProgram, words[0])

			code, ok := fence.RunHook(words[1:], bytes.NewReader(payload), &bytes.Buffer{})
			require.True(t, ok, "the hook runs in its hook mode")
			require.Equal(t, "block", h.OnFailure, "a hook that fails blocks its call")

			if code == 2 {
				return true
			}
		}
	}

	return false
}

// shellWords splits a command line of single-quoted words, as textutil.ShellQuote writes
// them, the way a POSIX shell does.
func shellWords(t *testing.T, line string) []string {
	t.Helper()

	words := regexp.MustCompile(`(?:'[^']*'|\\.)+`).FindAllString(line, -1)
	require.Equal(t, line, strings.Join(words, " "), "the hook's command line is quoted words alone")

	part := regexp.MustCompile(`'([^']*)'|\\(.)`)

	out := make([]string, 0, len(words))
	for _, w := range words {
		out = append(out, part.ReplaceAllString(w, "$1$2"))
	}

	return out
}

func TestTheHookCommandIsTheAppInItsHookMode(t *testing.T) {
	t.Parallel()

	line := claudeHookCommand("/Applications/It's.app/Contents/MacOS/App", "/Users/example/a b/run")
	assert.Equal(t, []string{"/Applications/It's.app/Contents/MacOS/App", "--fence-hook-v1=/Users/example/a b/run"},
		shellWords(t, line), "each word quoted whole, a quote in a path included")
}

// sandboxRun runs a command the sandbox holds: the sandbox check writes its proof and is
// refused its probe.
func (f *fakeClaude) sandboxRun(c fence.Call) {
	sb := f.settings.Sandbox
	if c.Tool != "Bash" || sb == nil || !sb.Enabled || !strings.HasPrefix(c.Command, "touch ") {
		return
	}

	for _, dir := range sb.Filesystem.AllowWrite {
		if strings.HasSuffix(dir, "/"+fence.ProofDir) && strings.Contains(c.Command, dir) {
			_ = os.WriteFile(filepath.Join(dir, "tried"), nil, 0o600)
		}
	}
}

// matchesAny reports a call one of the rules matches. A Bash command is matched part by
// part: an allow must match every part of a line, a deny any one.
func (f *fakeClaude) matchesAny(rules []string, c fence.Call, allow bool) bool {
	if c.Tool != "Bash" {
		return slices.ContainsFunc(rules, func(r string) bool { return f.matchFile(r, c) })
	}

	parts := splitCommand(c.Command)
	matched := func(part string) bool {
		return slices.ContainsFunc(rules, func(r string) bool { return matchBash(r, part) })
	}

	if allow {
		return !slices.ContainsFunc(parts, func(p string) bool { return !matched(p) })
	}

	return slices.ContainsFunc(parts, matched)
}

// splitCommand splits a command line at its separators and pipes, as Claude Code checks
// each part.
func splitCommand(line string) []string {
	parts := regexp.MustCompile(`&&|\|\||;|\||\n`).Split(line, -1)

	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}

	return out
}

// matchBash matches a Bash rule against one command.
func matchBash(rule, cmd string) bool {
	if rule == "Bash" {
		return true
	}

	inner, ok := strings.CutPrefix(rule, "Bash(")
	if !ok {
		return false
	}

	inner = strings.TrimSuffix(inner, ")")

	if prefix, isPrefix := strings.CutSuffix(inner, ":*"); isPrefix {
		return cmd == prefix || strings.HasPrefix(cmd, prefix+" ")
	}

	return cmd == inner
}

// matchFile matches a file tool's rule.
func (f *fakeClaude) matchFile(rule string, c fence.Call) bool {
	tool, pattern, hasPattern := strings.Cut(rule, "(")

	// An Edit rule holds every tool that edits a file, and a Read rule every one that
	// reads, as Claude Code's own rules do.
	covers := tool == c.Tool || tool == "Edit" && slices.Contains([]string{"Write", "MultiEdit", "NotebookEdit"}, c.Tool) ||
		tool == "Read" && slices.Contains([]string{"Grep", "Glob"}, c.Tool)
	if !covers {
		return false
	}

	if !hasPattern {
		return true
	}

	pattern = strings.TrimSuffix(pattern, ")")

	abs, isAbs := strings.CutPrefix(pattern, "//")
	if !isAbs {
		return false
	}

	return globRegexp("/" + abs).MatchString(c.Path)
}

// globRegexp turns a rule's gitignore-style glob into a regular expression, ignoring case
// as the real one may not: a fence must hold either way.
func globRegexp(glob string) *regexp.Regexp {
	var b strings.Builder

	b.WriteString("(?i)^")

	for i := 0; i < len(glob); i++ {
		switch {
		case strings.HasPrefix(glob[i:], "**/"):
			b.WriteString("(.*/)?")

			i += 2
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(".*")

			i++
		case glob[i] == '*':
			b.WriteString("[^/]*")
		case glob[i] == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(glob[i : i+1]))
		}
	}

	b.WriteString("$")

	return regexp.MustCompile(b.String())
}
