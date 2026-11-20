//go:build mage

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/describeit"
	"github.com/mrz1836/nilbox/internal/mcpserver"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// evalsRig returns a rig whose checkout holds the repository's own eval suite, and a
// results folder an earlier run left in it, with a fake Claude Code that plays run.
func evalsRig(t *testing.T, run testutil.FakeRun) *rig {
	t.Helper()

	g := newRig(t)
	src := filepath.Join("..", filepath.FromSlash(evalsDir))

	require.NoError(t, filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		rel, err := filepath.Rel(src, path)
		require.NoError(t, err)

		data, err := os.ReadFile(path) //nolint:gosec // a file of the repository's suite
		require.NoError(t, err)

		testutil.WriteFile(t, g.path(evalsDir+"/"+filepath.ToSlash(rel)), string(data))

		return nil
	}))

	testutil.WriteFile(t, g.path(evalsDir+"/results/2026-01-01/report.html"), "an earlier run's report")

	g.fixed(agentProgram(), run)

	return g
}

func TestAutopilotEvalsRunsTheSuiteAgainstThisBuildsPlugin(t *testing.T) {
	t.Parallel()

	g := evalsRig(t, testutil.FakeRun{})

	require.NoError(t, g.runner().runEvals())

	lines := g.linesOf(agentProgram())
	require.Len(t, lines, 1)

	root := strings.Fields(lines[0])[2]
	assert.Equal(t, "plugin eval "+root+" --trust-plugin --no-publish --ablation none --max-cost-usd 5", lines[0],
		"the plugin is trusted as the checkout's own, its report kept local, and its spend capped")
	assert.Equal(t, describeit.PluginName, filepath.Base(root), "the plugin's own folder")
	assert.Contains(t, g.out.String(), "Running the plugin's evals in "+root)

	dir := filepath.Dir(filepath.Dir(root))

	files, err := agent.PluginFiles(agent.Claude, describeit.Plugin(filepath.Join(dir, "bin", brand.Slug+"-mcp"),
		[]string{mcpserver.Arg(dir)}))
	require.NoError(t, err)

	for _, f := range files {
		got, readErr := os.ReadFile(filepath.Join(filepath.Dir(root), filepath.FromSlash(f.Path)))
		require.NoError(t, readErr)
		assert.Equal(t, string(f.Data), string(got), "%s is the plugin the app writes", f.Path)
	}

	grader, err := os.ReadFile(filepath.Join(root, "evals", "routine-request", "graders", "proposed-once.md")) //nolint:gosec // the suite copied
	require.NoError(t, err)
	assert.Contains(t, string(grader), agent.PluginToolPrefix(agent.Claude, describeit.PluginName, describeit.PluginServer)+
		"autopilot_propose", "the suite names this build's tools")
	assert.NotContains(t, string(grader), "@TOOLS@")

	assert.FileExists(t, filepath.Join(root, "evals", "mocks", describeit.PluginServer, "autopilot_guide.md"))
	assert.NoDirExists(t, filepath.Join(root, "evals", "results"), "an earlier run's results stay behind")
}

func TestAutopilotEvalsTakesItsArguments(t *testing.T) {
	t.Parallel()

	g := evalsRig(t, testutil.FakeRun{})
	g.setenv(mageArgsJSON, `["runs=3","case=routine-*","model=opus","budget=1"]`)

	require.NoError(t, g.runner().runEvals())

	lines := g.linesOf(agentProgram())
	require.Len(t, lines, 1)

	root := strings.Fields(lines[0])[2]
	assert.Equal(t, "plugin eval "+root+" --trust-plugin --no-publish --ablation none --max-cost-usd 1 --runs 3 "+
		"--case routine-* --model opus", lines[0])
}

func TestAutopilotEvalsFails(t *testing.T) {
	t.Parallel()

	t.Run("when a case scores below the threshold", func(t *testing.T) {
		t.Parallel()

		g := evalsRig(t, testutil.FakeRun{Exit: 1})
		require.Error(t, g.runner().runEvals())
	})

	t.Run("without Claude Code", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)

		err := g.runner().runEvals()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "find Claude Code")
	})

	t.Run("without the suite", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.fixed(agentProgram(), testutil.FakeRun{})

		err := g.runner().runEvals()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "copy the eval suite")
		assert.Empty(t, g.lines(), "nothing runs")
	})
}
