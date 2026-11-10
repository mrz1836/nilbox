package testutil_test

import (
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

// runFake runs a fake tool with its environment entry and returns what it wrote and how
// it exited.
func runFake(t *testing.T, path, env string) (stdout, stderr string, code int) {
	t.Helper()

	var out, errOut bytes.Buffer

	cmd := exec.CommandContext(t.Context(), path, "ignored", "--args")

	cmd.Env = append(os.Environ(), env)
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

func TestFakeToolReportsStdin(t *testing.T) {
	t.Parallel()

	path, env := testutil.FakeTool(t, "magex", testutil.FakeRun{ReportStdin: true})

	stdout, _, _ := runFake(t, path, env)
	assert.Equal(t, "stdin: 0 bytes, devnull: true\n", stdout, "exec.Cmd with no Stdin reads /dev/null")
}

func TestLoadFakeRunReadsARecordedFixture(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "patch.json")
	testutil.WriteFile(t, path, `{"name": "patch", "stdout": "✅ ✅ Created tag: v1.4.3\n", "stderr": "", "exit": 0,
		"lines": [{"stream": "out", "ms": 194, "text": "✅ ✅ Created tag: v1.4.3"}]}`)

	run := testutil.LoadFakeRun(t, path)
	assert.Equal(t, testutil.FakeRun{Stdout: "✅ ✅ Created tag: v1.4.3\n"}, run)
}
