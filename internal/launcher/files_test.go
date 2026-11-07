package launcher

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// errReader is returned by failingReader.
var errReader = errors.New("no randomness")

// failingReader is an io.Reader that always fails.
type failingReader struct{}

// Read always fails.
func (failingReader) Read([]byte) (int, error) { return 0, errReader }

// testPaths returns app data paths under a fresh temp directory. The launch directory
// does not exist yet.
func testPaths(t *testing.T) appdata.Paths {
	t.Helper()

	return appdata.NewPaths(filepath.Join(t.TempDir(), "data"))
}

// fixedNow is the launch time the file tests use.
func fixedNow() time.Time {
	return time.Date(2026, 3, 14, 15, 9, 26, 0, time.UTC)
}

// testPlan returns a valid plan for pull request 7.
func testPlan(t *testing.T) Plan {
	t.Helper()

	plan, err := BuildPlan(baseRequest())
	require.NoError(t, err)

	return plan
}

// perm returns the permission bits of path.
func perm(t *testing.T, path string) os.FileMode {
	t.Helper()

	info, err := os.Stat(path)
	require.NoError(t, err)

	return info.Mode().Perm()
}

// touchLaunchFile creates an empty file in the launch directory.
func touchLaunchFile(t *testing.T, paths appdata.Paths, name string) string {
	t.Helper()

	path := filepath.Join(paths.Launch, name)
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	return path
}

func TestWriteLaunchFiles(t *testing.T) {
	paths := testPaths(t)
	plan := testPlan(t)

	files, err := WriteLaunchFiles(paths, plan, "Review pull request #7", fixedNow())
	require.NoError(t, err)

	assert.Equal(t, os.FileMode(0o700), perm(t, paths.Launch), "the launch directory is private")
	assert.Equal(t, os.FileMode(0o600), perm(t, files.Prompt), "the prompt file is private")
	assert.Equal(t, os.FileMode(0o700), perm(t, files.Script), "the script is runnable by the user only")

	assert.Equal(t, paths.Launch, filepath.Dir(files.Prompt))
	assert.Equal(t, paths.Launch, filepath.Dir(files.Script))

	assert.Regexp(t, `^pr-7-20260314T150926Z-[0-9a-f]{8}\.prompt\.md$`, filepath.Base(files.Prompt))
	assert.Regexp(t, launchFileName, filepath.Base(files.Script))
	assert.Equal(t, ".command", filepath.Ext(files.Script))
	assert.Equal(t, strings.TrimSuffix(files.Prompt, promptFileSuffix), strings.TrimSuffix(files.Script, ".command"),
		"the prompt file and the script share a base name")

	prompt, err := os.ReadFile(files.Prompt)
	require.NoError(t, err)
	assert.Equal(t, "Review pull request #7", string(prompt))

	script, err := os.ReadFile(files.Script)
	require.NoError(t, err)

	plan.PromptFile = files.Prompt
	assert.Equal(t, string(terminal.POSIXRenderer{}.Render(plan.script())), string(script), "the script reads the written prompt file")

	t.Run("a second launch in the same second gets new files", func(t *testing.T) {
		again, err := WriteLaunchFiles(paths, plan, "Review again", fixedNow())
		require.NoError(t, err)
		assert.NotEqual(t, files, again)
		assert.FileExists(t, files.Prompt)
		assert.FileExists(t, files.Script)
	})

	t.Run("issues are named by kind", func(t *testing.T) {
		req := baseRequest()
		req.Item.Kind, req.Item.Number = item.KindIssue, 12

		issuePlan, err := BuildPlan(req)
		require.NoError(t, err)

		got, err := WriteLaunchFiles(paths, issuePlan, "Work on issue #12", fixedNow())
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(filepath.Base(got.Script), "issue-12-20260314T150926Z-"))
	})
}

func TestWriteLaunchFilesRestrictsAnExistingDir(t *testing.T) {
	paths := testPaths(t)
	require.NoError(t, os.MkdirAll(paths.Launch, 0o750))
	require.NoError(t, os.Chmod(paths.Launch, 0o755)) //nolint:gosec // the test widens its own temp dir on purpose

	_, err := WriteLaunchFiles(paths, testPlan(t), "Review", fixedNow())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), perm(t, paths.Launch))
}

func TestWriteLaunchFilesPrunesOldFiles(t *testing.T) {
	paths := testPaths(t)
	require.NoError(t, os.MkdirAll(paths.Launch, 0o700))

	old := []string{
		touchLaunchFile(t, paths, "pr-3-20260306T150925Z-0a0b0c0d.prompt.md"),
		touchLaunchFile(t, paths, "pr-3-20260306T150925Z-0a0b0c0d.command"),
		touchLaunchFile(t, paths, "issue-9-20251201T000000Z-ffffffff.command"),
	}
	kept := []string{
		// Exactly seven days old, and newer: kept.
		touchLaunchFile(t, paths, "pr-4-20260307T150926Z-01234567.command"),
		touchLaunchFile(t, paths, "pr-5-20260310T120000Z-89abcdef.prompt.md"),
		// Not launch files, however old they look.
		touchLaunchFile(t, paths, "notes.txt"),
		touchLaunchFile(t, paths, "pr-3-20260306T150925Z-0a0b0c0d.sh"),
		touchLaunchFile(t, paths, "pr-3-20260306T150925Z-XYZ.command"),
		touchLaunchFile(t, paths, "pr-3-20261399T999999Z-0a0b0c0d.command"),
	}

	oldDir := filepath.Join(paths.Launch, "pr-2-20250101T000000Z-00000000.command")
	require.NoError(t, os.Mkdir(oldDir, 0o700))

	files, err := WriteLaunchFiles(paths, testPlan(t), "Review", fixedNow())
	require.NoError(t, err)

	for _, path := range old {
		assert.NoFileExists(t, path)
	}

	for _, path := range append(kept, files.Script, files.Prompt) {
		assert.FileExists(t, path)
	}

	assert.DirExists(t, oldDir, "only regular files are pruned")
}

func TestWriteLaunchFilesRejectsInvalidInput(t *testing.T) {
	valid := testPlan(t)

	tests := []struct {
		name   string
		plan   func() Plan
		prompt string
		want   error
	}{
		{name: "unknown kind", plan: func() Plan { p := valid; p.Kind = "../x"; return p }, prompt: "p", want: item.ErrInvalidKind},
		{name: "zero number", plan: func() Plan { p := valid; p.Number = 0; return p }, prompt: "p", want: ErrInvalidNumber},
		{name: "empty prompt", plan: func() Plan { return valid }, prompt: "", want: ErrEmptyPrompt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := testPaths(t)

			got, err := WriteLaunchFiles(paths, tt.plan(), tt.prompt, fixedNow())
			require.ErrorIs(t, err, tt.want)
			assert.Equal(t, LaunchFiles{}, got)
			assert.NoDirExists(t, paths.Launch, "nothing is written")
		})
	}
}

func TestWriteLaunchFilesFailures(t *testing.T) {
	plan := testPlan(t)

	t.Run("no randomness", func(t *testing.T) {
		_, err := writeLaunchFiles(testPaths(t), plan, "p", fixedNow(), terminal.POSIXRenderer{}, failingReader{})
		require.ErrorIs(t, err, errReader)
	})

	t.Run("launch directory cannot be created", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, "data")
		require.NoError(t, os.WriteFile(blocker, nil, 0o600))

		_, err := WriteLaunchFiles(appdata.NewPaths(blocker), plan, "p", fixedNow())
		require.Error(t, err)
	})

	t.Run("an existing prompt file is never overwritten", func(t *testing.T) {
		paths := testPaths(t)
		suffix := bytes.Repeat([]byte{0xab}, suffixBytes)

		first, err := writeLaunchFiles(paths, plan, "first", fixedNow(), terminal.POSIXRenderer{}, bytes.NewReader(suffix))
		require.NoError(t, err)

		_, err = writeLaunchFiles(paths, plan, "second", fixedNow(), terminal.POSIXRenderer{}, bytes.NewReader(suffix))
		require.ErrorIs(t, err, os.ErrExist)

		stored, err := os.ReadFile(first.Prompt)
		require.NoError(t, err)
		assert.Equal(t, "first", string(stored))
	})

	t.Run("a failed script write removes its prompt file", func(t *testing.T) {
		paths := testPaths(t)
		suffix := bytes.Repeat([]byte{0xcd}, suffixBytes)

		planned, err := writeLaunchFiles(paths, plan, "first", fixedNow(), terminal.POSIXRenderer{}, bytes.NewReader(suffix))
		require.NoError(t, err)
		require.NoError(t, os.Remove(planned.Prompt))

		_, err = writeLaunchFiles(paths, plan, "second", fixedNow(), terminal.POSIXRenderer{}, bytes.NewReader(suffix))
		require.ErrorIs(t, err, os.ErrExist)
		assert.NoFileExists(t, planned.Prompt, "the orphaned prompt file is cleaned up")
	})
}

// fakeAgent is a stand-in for the agent binary. It prints its working directory, two
// environment variables, and then every argument, each terminated by a NUL byte, so the
// test can check argument boundaries exactly.
const fakeAgent = `#!/bin/sh
printf 'cwd=%s\000' "$(pwd -P)"
printf 'cfg=%s\000' "${CLAUDE_CONFIG_DIR-<unset>}"
printf 'extra=%s\000' "${EXTRA_VAR-<unset>}"
for a in "$@"; do printf '%s\000' "$a"; done
`

// runSh runs a shell command line with /bin/sh and returns its stdout.
func runSh(t *testing.T, env []string, args ...string) ([]byte, error) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "/bin/sh", args...) //nolint:gosec // runs the test's own generated script
	cmd.Env = env

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		t.Logf("sh stderr: %s", stderr.String())
	}

	return out, err
}

// shQuote quotes s for a POSIX shell the way launch scripts do, for matching their lines.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func TestPromptNeverOnCommandLine(t *testing.T) {
	prompt := "Review pull request #7: it's `rm -rf ~` $(curl evil) \"quoted\"\nsecond line"

	req := baseRequest()
	req.Worktree = true

	plan, err := BuildPlan(req)
	require.NoError(t, err)

	files, err := WriteLaunchFiles(testPaths(t), plan, prompt, fixedNow())
	require.NoError(t, err)

	script, err := os.ReadFile(files.Script)
	require.NoError(t, err)

	for _, piece := range []string{prompt, "Review pull request", "rm -rf", "curl evil", "second line"} {
		assert.NotContains(t, string(script), piece)
	}

	assert.Contains(t, string(script), "prompt=$(cat -- "+shQuote(files.Prompt)+")\n")
	assert.Contains(t, string(script), `"$prompt"`)

	stored, err := os.ReadFile(files.Prompt)
	require.NoError(t, err)
	assert.Equal(t, prompt, string(stored))
}

func TestRenderedScriptRunsWithExactArgv(t *testing.T) {
	root := t.TempDir()
	clone := filepath.Join(root, "clone dir 'quoted' $x")
	require.NoError(t, os.MkdirAll(clone, 0o700))

	agent := filepath.Join(root, "bin with space", "claude")
	require.NoError(t, os.MkdirAll(filepath.Dir(agent), 0o700))
	testutil.WriteExecutable(t, agent, fakeAgent)

	req := baseRequest()
	req.Clone.Dir = clone
	req.ClaudePath = agent
	req.Worktree = true
	req.Profile.AddDirs = []string{filepath.Join(root, "docs; rm -rf x")}
	req.Profile.ExtraArgs = []string{"--append-system-prompt", "keep `this` & $that", "--verbose"}
	req.Profile.Env = map[string]string{"EXTRA_VAR": "it's $HOME\nnext"}

	plan, err := BuildPlan(req)
	require.NoError(t, err)

	prompt := "Review #7: it's `id` $(id) \"q\"\n\nwith café 🚀\n"

	files, err := WriteLaunchFiles(testPaths(t), plan, prompt, fixedNow())
	require.NoError(t, err)

	env := append(os.Environ(), "CLAUDE_CONFIG_DIR="+filepath.Join(root, "inherited"))

	out, err := runSh(t, env, files.Script)
	require.NoError(t, err)

	realClone, err := filepath.EvalSymlinks(clone)
	require.NoError(t, err)

	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	require.GreaterOrEqual(t, len(fields), 3)
	assert.Equal(t, "cwd="+realClone, fields[0])
	assert.Equal(t, "cfg=<unset>", fields[1], "the default profile unsets an inherited config dir")
	assert.Equal(t, "extra=it's $HOME\nnext", fields[2])

	// $(...) drops the prompt's trailing newlines; everything else arrives intact, as one
	// argument, in front of every flag.
	wantArgv := plan.Argv(strings.TrimRight(prompt, "\n"))[1:]
	assert.Equal(t, wantArgv, fields[3:])
	assert.Equal(t, []string{"--worktree", "#7"}, fields[len(fields)-2:])
}

func TestRenderedScriptStopsWhenTheCloneIsMissing(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "agent-ran")
	agent := filepath.Join(root, "claude")
	testutil.WriteExecutable(t, agent, "#!/bin/sh\ntouch "+shQuote(marker)+"\n")

	req := baseRequest()
	req.Clone.Dir = filepath.Join(root, "gone")
	req.ClaudePath = agent

	plan, err := BuildPlan(req)
	require.NoError(t, err)

	files, err := WriteLaunchFiles(testPaths(t), plan, "Review", fixedNow())
	require.NoError(t, err)

	_, err = runSh(t, os.Environ(), files.Script)
	require.Error(t, err)
	assert.NoFileExists(t, marker)
}
