package describeit

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
)

// The plugin's eval suite (evals/), which magex autopilot:evals runs with Claude Code's
// plugin eval, in its format: each case a folder with a prompt.md and its graders, and
// the mocks of the plugin's own server's tools, named for this build's tools.

// frontKeys returns the keys of a file's front matter, and its body.
func frontKeys(t *testing.T, text string) ([]string, string) {
	t.Helper()

	rest, ok := strings.CutPrefix(text, "---\n")
	require.True(t, ok, "the file starts with its front matter")

	front, body, ok := strings.Cut(rest, "\n---\n")
	require.True(t, ok, "and ends it")

	var keys []string

	for _, line := range strings.Split(front, "\n") {
		if key, _, found := strings.Cut(line, ":"); found && !strings.HasPrefix(line, " ") {
			keys = append(keys, key)
		}
	}

	return keys, body
}

// evalFile reads a file of the suite with this build's names filled in.
func evalFile(t *testing.T, rel string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("evals", filepath.FromSlash(rel)))
	require.NoError(t, err)

	text := EvalText(string(data))
	assert.NotRegexp(t, `@[A-Z]+@`, text, "%s names nothing the build doesn't fill in", rel)

	return text
}

func TestTheEvalSuiteIsWellFormed(t *testing.T) {
	t.Parallel()

	cases, err := os.ReadDir("evals")
	require.NoError(t, err)

	seen := 0

	for _, c := range cases {
		if c.IsDir() && c.Name() != "mocks" {
			seen++

			checkCase(t, c.Name())
		}
	}

	assert.Equal(t, 2, seen, "a routine request that ends in a proposal, and one it must refuse")
}

// checkCase checks the suite's case name: its prompt.md's front matter holds only keys
// the format has, it has a prompt, and each of its graders is of a type there is, with
// only that type's keys.
func checkCase(t *testing.T, name string) {
	t.Helper()

	promptKeys := []string{
		"schema_version", "name", "description", "tags", "plugins", "runs", "expected_outcome", "model", "max_turns",
		"timeout_seconds", "allowed_tools", "append_system_prompt", "env",
	}
	graderKeys := []string{
		"type", "weight", "arm", "pattern", "flags", "match", "target", "tool", "input_match", "min", "max", "before",
		"after", "path", "exists", "criteria", "focus", "baseline_file",
	}
	graderTypes := []string{"regex", "tool_used", "tool_order", "file_exists", "llm", "baseline"}

	keys, body := frontKeys(t, evalFile(t, name+"/prompt.md"))
	assert.Subset(t, promptKeys, keys, "%s: prompt.md's front matter", name)
	assert.NotEmpty(t, strings.TrimSpace(body), "%s has a prompt", name)

	graders, err := os.ReadDir(filepath.Join("evals", name, "graders"))
	require.NoError(t, err)
	assert.NotEmpty(t, graders, "%s has a grader", name)

	for _, g := range graders {
		text := evalFile(t, name+"/graders/"+g.Name())

		keys, _ := frontKeys(t, text)
		assert.Subset(t, graderKeys, keys, "%s/%s", name, g.Name())

		kind := regexp.MustCompile(`(?m)^type: (\S+)$`).FindStringSubmatch(text)
		require.Len(t, kind, 2, "%s/%s names its type", name, g.Name())
		assert.Contains(t, graderTypes, kind[1])
	}
}

func TestTheEvalSuiteNamesThisBuildsTools(t *testing.T) {
	t.Parallel()

	prefix := agent.PluginToolPrefix(agent.Claude, PluginName, PluginServer)
	tools := kit.Tools(kit.Outside)
	named := regexp.MustCompile(regexp.QuoteMeta(prefix) + `(\w+)`)

	err := filepath.WalkDir("evals", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		rel, _ := filepath.Rel("evals", path)

		for _, m := range named.FindAllStringSubmatch(evalFile(t, filepath.ToSlash(rel)), -1) {
			assert.Contains(t, tools, m[1], "%s names a tool the plugin serves", rel)
		}

		return nil
	})
	require.NoError(t, err)

	mocks, err := os.ReadDir(filepath.Join("evals", "mocks", PluginServer))
	require.NoError(t, err, "the mocks are the plugin's server's, by its name")

	mocked := make([]string, 0, len(mocks))

	for _, m := range mocks {
		tool := strings.TrimSuffix(m.Name(), ".md")
		assert.Contains(t, tools, tool, "a mock stands in for one of the plugin's tools")

		mocked = append(mocked, tool)

		keys, body := frontKeys(t, evalFile(t, "mocks/"+PluginServer+"/"+m.Name()))
		for _, key := range keys {
			assert.Contains(t, []string{"type", "expect", "error", "abort_when"}, key, m.Name())
		}

		assert.NotEmpty(t, strings.TrimSpace(body))
	}

	slices.Sort(mocked)

	want := slices.Clone(tools)
	slices.Sort(want)
	assert.Equal(t, want, mocked, "every tool the plugin serves answers from a mock")
}
