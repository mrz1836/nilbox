package testutil_test

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// runFake runs a fake tool with its environment entry, and any others after it, and
// returns what it wrote and how it exited.
func runFake(t *testing.T, path string, env ...string) (stdout, stderr string, code int) {
	t.Helper()

	var out, errOut bytes.Buffer

	cmd := exec.CommandContext(t.Context(), path, "ignored", "--args")

	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout, cmd.Stderr = &out, &errOut

	err := cmd.Run()
	if err != nil {
		var exit *exec.ExitError
		require.ErrorAs(t, err, &exit)
	}

	return out.String(), errOut.String(), cmd.ProcessState.ExitCode()
}

func TestFakeToolReplaysARun(t *testing.T) {
	t.Parallel()

	path, env := testutil.FakeTool(t, "go-pre-commit", testutil.FakeRun{Stdout: "✓ eof check passed\n", Stderr: "warn\n", Exit: 3})

	assert.Equal(t, "go-pre-commit", filepath.Base(path))

	stdout, stderr, code := runFake(t, path, env)
	assert.Equal(t, "✓ eof check passed\n", stdout)
	assert.Equal(t, "warn\n", stderr)
	assert.Equal(t, 3, code)
}

func TestFakeToolSleepsBeforeExiting(t *testing.T) {
	t.Parallel()

	path, env := testutil.FakeTool(t, "magex", testutil.FakeRun{Stdout: "done\n", Sleep: 200 * time.Millisecond})

	start := time.Now()
	stdout, _, code := runFake(t, path, env)

	assert.Equal(t, "done\n", stdout)
	assert.Zero(t, code)
	assert.GreaterOrEqual(t, time.Since(start), 200*time.Millisecond)
}

func TestFakeToolExitsOnceTheFileItWaitsForAppears(t *testing.T) {
	t.Parallel()

	proceed := filepath.Join(t.TempDir(), "proceed")
	path, env := testutil.FakeTool(t, "magex", testutil.FakeRun{Stdout: "waiting\n", WaitFor: proceed, Exit: 4})

	cmd := exec.CommandContext(t.Context(), path) //nolint:gosec // the fake tool this test made

	cmd.Env = append(os.Environ(), env)

	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())

	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "waiting\n", line)

	testutil.WriteFile(t, proceed, "")

	start := time.Now()

	var exit *exec.ExitError
	require.ErrorAs(t, cmd.Wait(), &exit)
	assert.Equal(t, 4, exit.ExitCode())
	assert.Less(t, time.Since(start), time.Minute, "it exits once the file appears, not at its bound")
}

func TestFakeToolReportsStdin(t *testing.T) {
	t.Parallel()

	path, env := testutil.FakeTool(t, "magex", testutil.FakeRun{ReportStdin: true})

	stdout, _, _ := runFake(t, path, env)
	assert.Equal(t, "stdin: 0 bytes, devnull: true\n", stdout, "exec.Cmd with no Stdin reads /dev/null")
}

func TestFakeToolReportsItsEnvironment(t *testing.T) {
	t.Parallel()

	path, env := testutil.FakeTool(t, "codex", testutil.FakeRun{
		Stdout: "done\n", ReportEnv: []string{"FAKE_SECOND", "FAKE_FIRST", "FAKE_UNSET"},
	})

	stdout, _, code := runFake(t, path, env, "FAKE_FIRST=one", "FAKE_SECOND=two=2")
	require.Zero(t, code)
	assert.Equal(t, "FAKE_SECOND=two=2\nFAKE_FIRST=one\nFAKE_UNSET=\ndone\n", stdout,
		"each named variable in order, an unset one empty, before what it writes")
}

func TestFakeToolChangesFilesBeforeWriting(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hook, backup, gone := filepath.Join(dir, "pre-commit"), filepath.Join(dir, "pre-commit.backup.1"), filepath.Join(dir, "gone")
	testutil.WriteFile(t, hook, "#!/usr/bin/env sh\n")
	testutil.WriteFile(t, gone, "x")
	testutil.WriteFile(t, filepath.Join(dir, "kept"), "old")
	require.NoError(t, os.Chmod(filepath.Join(dir, "kept"), 0o600))

	path, env := testutil.FakeTool(t, "go-pre-commit", testutil.FakeRun{Stdout: "done\n", Files: []testutil.FakeFile{
		{Path: backup, From: hook},
		{Path: hook, Content: "# Go Pre-commit Hook\n", Mode: 0o755},
		{Path: filepath.Join(dir, "kept"), Content: "new"},
		{Path: gone, Remove: true},
	}})

	stdout, _, code := runFake(t, path, env)
	require.Zero(t, code)
	assert.Equal(t, "done\n", stdout)

	got, err := os.ReadFile(backup) //nolint:gosec // the test's own file
	require.NoError(t, err)
	assert.Equal(t, "#!/usr/bin/env sh\n", string(got))

	info, err := os.Stat(hook)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())

	info, err = os.Stat(filepath.Join(dir, "kept"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "a file already there keeps its mode")
	assert.NoFileExists(t, gone)

	path, env = testutil.FakeTool(t, "go-pre-commit", testutil.FakeRun{Files: []testutil.FakeFile{{Path: gone, Remove: true}}})
	_, stderr, code := runFake(t, path, env)
	assert.Equal(t, 1, code, "a change that fails fails the run")
	assert.Contains(t, stderr, "fake tool:")
}

func TestLoadFakeRunReadsARecordedFixture(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "patch.json")
	testutil.WriteFile(t, path, `{"name": "patch", "stdout": "✅ ✅ Created tag: v1.4.3\n", "stderr": "", "exit": 0,
		"lines": [{"stream": "out", "ms": 194, "text": "✅ ✅ Created tag: v1.4.3"}]}`)

	run := testutil.LoadFakeRun(t, path)
	assert.Equal(t, testutil.FakeRun{Stdout: "✅ ✅ Created tag: v1.4.3\n"}, run)
}
