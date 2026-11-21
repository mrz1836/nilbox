package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/mcpserver"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// TestMain runs this test binary, which holds the build's own main, as an agent runs the
// app's executable for its fence hook or as its tool server: given the hook mode's flag or
// the tool mode's, it is main in that mode, whichever build the tests were built for.
func TestMain(m *testing.M) {
	if fence.IsHookCall(os.Args[1:]) || mcpserver.IsCall(os.Args[1:]) {
		main()
	}

	os.Exit(m.Run())
}

// clientEnv is the environment the tests start the executable in, as an agent's client
// starts it: home its only home, and nothing of the test's own but the GOCOVERDIR go test
// -cover sets. Under -cover the executable is this test binary, built with coverage, which
// writes what it ran into GOCOVERDIR as it exits, where go test adds it to the profile,
// and without one warns of it on the standard error the tests read.
func clientEnv(home string) []string {
	env := []string{"HOME=" + home, "TMPDIR=" + home, "PATH=/usr/bin:/bin"}

	if dir := os.Getenv("GOCOVERDIR"); dir != "" {
		env = append(env, "GOCOVERDIR="+dir)
	}

	return env
}

// hookCall runs the executable in hook mode on one tool call, with an empty home and
// nothing else in its environment (clientEnv), and returns its exit status, stdout, and
// stderr.
func hookCall(t *testing.T, exe, home, runDir, tool string, input map[string]any) (int, string, string) {
	t.Helper()

	payload, err := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input})
	require.NoError(t, err)

	cmd := exec.CommandContext(t.Context(), exe, fence.HookArg(runDir)) //nolint:gosec // the test's own binary
	cmd.Env = clientEnv(home)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.WaitDelay = 10 * time.Second

	var stdout, stderr bytes.Buffer

	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err = cmd.Run()

	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return exit.ExitCode(), stdout.String(), stderr.String()
	}

	require.NoError(t, err)

	return 0, stdout.String(), stderr.String()
}

func TestTheHookModeDecidesAndOpensNothing(t *testing.T) {
	t.Parallel()

	exe, err := os.Executable()
	require.NoError(t, err)

	home := t.TempDir()
	root := t.TempDir()
	wt := filepath.Join(root, "worktree")
	runDir := filepath.Join(root, "run")

	require.NoError(t, os.MkdirAll(wt, 0o700))

	f, err := fence.Build(fence.Spec{
		Preset: fence.EditTestPush, Worktree: wt, Repo: "acme-org/api", Number: 12, Branch: "fix-it", Remote: "origin",
		RunDir: runDir, Home: home, DataDir: filepath.Join(home, "data"),
	})
	require.NoError(t, err)
	require.NoError(t, fence.WriteRun(runDir, f))
	require.NoError(t, os.WriteFile(f.Policy.Canary.Proof, nil, 0o600))

	before := testutil.Tree(t, root)

	tests := []struct {
		name  string
		tool  string
		input map[string]any
		code  int
	}{
		{name: "the exact push", tool: "Bash", input: map[string]any{"command": "git push origin HEAD:refs/heads/fix-it"}},
		{name: "a forced push", tool: "Bash", input: map[string]any{"command": "git push -f origin HEAD:fix-it"}, code: 2},
		{name: "an agent file", tool: "Write", input: map[string]any{"file_path": filepath.Join(wt, "CLAUDE.md")}, code: 2},
		{name: "a source file", tool: "Edit", input: map[string]any{"file_path": filepath.Join(wt, "main.go")}},
	}

	for _, tt := range tests {
		code, stdout, stderr := hookCall(t, exe, home, runDir, tt.tool, tt.input)
		assert.Equal(t, tt.code, code, "%s: %s", tt.name, stderr)
		assert.Empty(t, stdout, "%s: the hook mode writes nothing to stdout, the app's identity included", tt.name)

		if tt.code != 0 {
			assert.NotEmpty(t, strings.TrimSpace(stderr), "%s says why", tt.name)
		}
	}

	code, _, stderr := hookCall(t, exe, home, filepath.Join(root, "missing"), "Read", map[string]any{"file_path": "/etc/hosts"})
	assert.Equal(t, 2, code, "a run with no policy refuses every call")
	assert.Contains(t, stderr, "fence refused")

	assert.Equal(t, []string{home}, testutil.Tree(t, home), "the hook mode creates nothing in home: no data, no logs")

	denials := filepath.Join(runDir, fence.DenialsFile)
	assert.Equal(t, sortedFiles(append(slices.Clone(before), denials)), sortedFiles(testutil.Tree(t, root)),
		"nor in the run's folder or the worktree, but its own log of the calls it refused")

	count, _ := fence.ReadDenials(runDir)
	assert.Equal(t, 2, count, "the forced push and the agent file")
	assert.False(t, slices.ContainsFunc(testutil.Tree(t, root), func(p string) bool { return strings.HasSuffix(p, ".log") }))
}

// sortedFiles returns paths sorted, as a walk orders a folder's names.
func sortedFiles(paths []string) []string {
	out := slices.Clone(paths)
	slices.Sort(out)

	return out
}
