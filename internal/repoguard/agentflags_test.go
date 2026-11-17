package repoguard_test

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agentFlagHomes are where a coding agent's flags and its program's name may be spelled:
// the agents' specs, the background runner's stream parsers, and the session observers,
// which run `claude agents --json` and talk to Codex's app-server. A path ending in "/"
// covers the folder.
func agentFlagHomes() []string {
	return []string{"internal/agent/", "internal/agentrun/", "internal/sessions/observe/"}
}

// agentNameExceptions are the production files that spell an agent's program name for
// something else, each with why.
func agentNameExceptions() map[string]string {
	return map[string]string{
		"internal/statusprefs/statusprefs.go": "names the status page of Claude's provider, not the agent's program",
	}
}

// sharedFlags are long flags the agents' specs spell that other programs take too, so
// their spelling elsewhere is no agent's: a tool's own --version, --json, --config, git
// commit's --file, and the like.
func sharedFlags() []string {
	return []string{
		"--version", "--verbose", "--help", "--json", "--config", "--name", "--remote", "--cd", "--flag", "--print",
		"--background", "--environment", "--continue", "--worktree", "--settings", "--tools", "--resume", "--model",
		"--sandbox", "--restricted", "--bare", "--bg", "--desktop", "--cloud", "--file",
	}
}

// longFlag matches a long flag as a Go string literal: the flag, alone or with "=".
var longFlag = regexp.MustCompile(`"(--[a-zA-Z][a-zA-Z-]*)=?"`)

// agentFlags returns the long flags the agents' specs spell that are theirs alone.
func agentFlags(t *testing.T) []string {
	t.Helper()

	seen := map[string]bool{}

	for _, rel := range goFiles(t) {
		if !strings.HasPrefix(rel, "internal/agent/") || strings.HasSuffix(rel, "_test.go") {
			continue
		}

		if text, ok := readText(t, rel); ok {
			flagsIn(text, seen)
		}
	}

	out := make([]string, 0, len(seen))
	for flag := range seen {
		out = append(out, flag)
	}

	sort.Strings(out)

	return out
}

// flagsIn adds to seen the long flags text spells that are the agents' alone.
func flagsIn(text string, seen map[string]bool) {
	for _, m := range longFlag.FindAllStringSubmatch(text, -1) {
		if !slices.Contains(sharedFlags(), m[1]) {
			seen[m[1]] = true
		}
	}
}

// spellsAgent returns a matcher for a line that spells one of flags, or an agent's program
// name, as a Go string literal outside a comment.
func spellsAgent(flags []string) func(string) bool {
	quoted := make([]string, 0, len(flags))
	for _, f := range flags {
		quoted = append(quoted, regexp.QuoteMeta(f))
	}

	// Built from parts so this file does not match itself.
	pattern := regexp.MustCompile(`"(?:(?:` + strings.Join(quoted, "|") + `)(?:=[^"]*)?|cla` + `ude|cod` + `ex)"`)

	return func(line string) bool {
		return !commentLine(line) && pattern.MatchString(line)
	}
}

func TestOnlyTheAgentsHomesSpellTheirFlags(t *testing.T) {
	t.Parallel()

	flags := agentFlags(t)
	require.Contains(t, flags, "--plugin-url", "the agents' flags are read from internal/agent; update this guard")
	require.Contains(t, flags, "--dangerously-skip-permissions")

	// The test support (testutil, and the routines' simulator, which only tests import)
	// and the build targets are no launch.
	exempt := append(agentFlagHomes(), "internal/testutil/", "internal/autopilot/sim/", "magefiles/")
	for rel := range agentNameExceptions() {
		exempt = append(exempt, rel)
	}

	assert.Empty(t, productionMatches(t, spellsAgent(flags), exempt...),
		"nothing spells a coding agent's flags or its program's name outside internal/agent, the background "+
			"runner's parsers, and the session observers: build a launch through the agent's spec")
}

func TestSpellsAgentPattern(t *testing.T) {
	t.Parallel()

	match := spellsAgent([]string{"--plugin-url", "--max-budget-usd"})

	for _, line := range []string{
		`args := []string{"--plugin-url", url}`, `"--max-budget-usd=1"`, `argv := []string{"cla` + `ude", "-p"}`,
		`name := "cod` + `ex"`,
	} {
		assert.True(t, match(line), line)
	}

	for _, line := range []string{
		`// such as "cod` + `ex", as the OS reports it`, `"--plugin-urls"`, `"--version"`, `".cla` + `ude.json"`,
		`"cla` + `ude-code"`,
	} {
		assert.False(t, match(line), line)
	}
}
