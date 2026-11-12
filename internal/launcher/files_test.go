package launcher

import (
	"bytes"
	"errors"
	"io"
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
	"github.com/mrz1836/nilbox/internal/textutil"
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

// randomBytes is how many random bytes end a launch file's base name.
const randomBytes = 4

// posixDeps returns the writer writeLaunchFiles uses for a POSIX script named from
// random, logging nowhere.
func posixDeps(random io.Reader) terminal.ScriptWriter {
	return terminal.ScriptWriter{Renderer: terminal.POSIXRenderer{}, Random: random}
}

// touchLaunchFile creates an empty file in the launch directory.
func touchLaunchFile(t *testing.T, paths appdata.Paths, name string) string {
	t.Helper()

	path := filepath.Join(paths.Launch, name)
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	return path
}

func TestWriteLaunchFiles(t *testing.T) {
	t.Parallel()

	paths := testPaths(t)
	plan := testPlan(t)

	files, err := WriteLaunchFiles(paths, plan, "Review pull request #7", fixedNow(), nil)
	require.NoError(t, err)

	assert.Equal(t, os.FileMode(0o700), perm(t, paths.Launch), "the launch directory is private")
	assert.Equal(t, os.FileMode(0o600), perm(t, files.Prompt), "the prompt file is private")
	assert.Equal(t, os.FileMode(0o700), perm(t, files.Script), "the script is runnable by the user only")

	assert.Equal(t, paths.Launch, filepath.Dir(files.Prompt))
	assert.Equal(t, paths.Launch, filepath.Dir(files.Script))

	assert.Regexp(t, `^pr-7-20260314T150926Z-[0-9a-f]{8}\.prompt\.md$`, filepath.Base(files.Prompt))
	assert.Regexp(t, `^pr-7-20260314T150926Z-[0-9a-f]{8}\.command$`, filepath.Base(files.Script))
	assert.Equal(t, ".command", filepath.Ext(files.Script))
	assert.Equal(t, strings.TrimSuffix(files.Prompt, terminal.PromptSuffix), strings.TrimSuffix(files.Script, ".command"),
		"the prompt file and the script share a base name")

	prompt, err := os.ReadFile(files.Prompt)
	require.NoError(t, err)
	assert.Equal(t, "Review pull request #7", string(prompt))

	script, err := os.ReadFile(files.Script)
	require.NoError(t, err)

	plan.PromptFile = files.Prompt
	assert.Equal(t, string(terminal.POSIXRenderer{}.Render(plan.script())), string(script), "the script reads the written prompt file")

	t.Run("a second launch in the same second gets new files", func(t *testing.T) {
		t.Parallel()

		again, err := WriteLaunchFiles(paths, plan, "Review again", fixedNow(), nil)
		require.NoError(t, err)
		assert.NotEqual(t, files, again)
		assert.FileExists(t, files.Prompt)
		assert.FileExists(t, files.Script)
	})

	t.Run("issues are named by kind", func(t *testing.T) {
		t.Parallel()

		req := baseRequest()
		req.Item.Kind, req.Item.Number = item.KindIssue, 12

		issuePlan, err := BuildPlan(req)
		require.NoError(t, err)

		got, err := WriteLaunchFiles(paths, issuePlan, "Work on issue #12", fixedNow(), nil)
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(filepath.Base(got.Script), "issue-12-20260314T150926Z-"))
	})
}

func TestWriteLaunchFilesWithoutAPrompt(t *testing.T) {
	t.Parallel()

	paths := testPaths(t)
	req := baseRequest()
	req.ResumeID = "0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70"

	plan, err := BuildPlan(req)
	require.NoError(t, err)
	require.True(t, plan.NoPrompt)

	_, err = WriteLaunchFiles(paths, plan, "a prompt", fixedNow(), nil)
	require.ErrorIs(t, err, ErrResumePrompt)

	files, err := WriteLaunchFiles(paths, plan, "", fixedNow(), nil)
	require.NoError(t, err)
	assert.Empty(t, files.Prompt, "no prompt file")

	entries, err := os.ReadDir(paths.Launch)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "the script alone")

	script, err := os.ReadFile(files.Script)
	require.NoError(t, err)
	assert.NotContains(t, string(script), "prompt")
	assert.Contains(t, string(script), "'--resume="+req.ResumeID+"'\n")
}

func TestWriteLaunchFilesRestrictsAnExistingDir(t *testing.T) {
	t.Parallel()

	paths := testPaths(t)
	require.NoError(t, os.MkdirAll(paths.Launch, 0o750))
	require.NoError(t, os.Chmod(paths.Launch, 0o755)) //nolint:gosec // the test widens its own temp dir on purpose

	_, err := WriteLaunchFiles(paths, testPlan(t), "Review", fixedNow(), nil)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), perm(t, paths.Launch))
}

func TestWriteLaunchFilesPrunesOldFiles(t *testing.T) {
	t.Parallel()

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

	files, err := WriteLaunchFiles(paths, testPlan(t), "Review", fixedNow(), nil)
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
	t.Parallel()

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
			t.Parallel()

			paths := testPaths(t)

			got, err := WriteLaunchFiles(paths, tt.plan(), tt.prompt, fixedNow(), nil)
			require.ErrorIs(t, err, tt.want)
			assert.Equal(t, LaunchFiles{}, got)
			assert.NoDirExists(t, paths.Launch, "nothing is written")
		})
	}
}

func TestWriteLaunchFilesFailures(t *testing.T) {
	t.Parallel()

	plan := testPlan(t)

	t.Run("no randomness", func(t *testing.T) {
		t.Parallel()

		_, err := writeLaunchFiles(testPaths(t), plan, "p", fixedNow(), posixDeps(failingReader{}))
		require.ErrorIs(t, err, errReader)
	})

	t.Run("launch directory cannot be created", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		blocker := filepath.Join(root, "data")
		require.NoError(t, os.WriteFile(blocker, nil, 0o600))

		_, err := WriteLaunchFiles(appdata.NewPaths(blocker), plan, "p", fixedNow(), nil)
		require.Error(t, err)
	})

	t.Run("an existing prompt file is never overwritten", func(t *testing.T) {
		t.Parallel()

		paths := testPaths(t)
		suffix := bytes.Repeat([]byte{0xab}, randomBytes)

		first, err := writeLaunchFiles(paths, plan, "first", fixedNow(), posixDeps(bytes.NewReader(suffix)))
		require.NoError(t, err)

		_, err = writeLaunchFiles(paths, plan, "second", fixedNow(), posixDeps(bytes.NewReader(suffix)))
		require.ErrorIs(t, err, os.ErrExist)

		stored, err := os.ReadFile(first.Prompt)
		require.NoError(t, err)
		assert.Equal(t, "first", string(stored))
	})

	t.Run("a failed script write removes its prompt file", func(t *testing.T) {
		t.Parallel()

		paths := testPaths(t)
		suffix := bytes.Repeat([]byte{0xcd}, randomBytes)

		planned, err := writeLaunchFiles(paths, plan, "first", fixedNow(), posixDeps(bytes.NewReader(suffix)))
		require.NoError(t, err)
		require.NoError(t, os.Remove(planned.Prompt))

		_, err = writeLaunchFiles(paths, plan, "second", fixedNow(), posixDeps(bytes.NewReader(suffix)))
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

func TestPromptNeverOnCommandLine(t *testing.T) {
	t.Parallel()

	prompt := "Review pull request #7: it's `rm -rf ~` $(curl evil) \"quoted\"\nsecond line"

	for _, req := range []Request{baseRequest(), codexRequest()} {
		plan, err := BuildPlan(req)
		require.NoError(t, err)

		files, err := WriteLaunchFiles(testPaths(t), plan, prompt, fixedNow(), nil)
		require.NoError(t, err)

		script, err := os.ReadFile(files.Script)
		require.NoError(t, err)

		for _, piece := range []string{prompt, "Review pull request", "rm -rf", "curl evil", "second line"} {
			assert.NotContains(t, string(script), piece)
		}

		assert.Contains(t, string(script), "prompt=$(cat -- "+textutil.ShellQuote(files.Prompt)+")\n")
		assert.Contains(t, string(script), `"$prompt"`)

		stored, err := os.ReadFile(files.Prompt)
		require.NoError(t, err)
		assert.Equal(t, prompt, string(stored))
	}
}

func TestPromptNeverOnCommandLineEvenOnce(t *testing.T) {
	t.Parallel()

	prompt := "Review"
	req := baseRequest()

	plan, err := BuildPlan(req)
	require.NoError(t, err)

	files, err := WriteLaunchFiles(testPaths(t), plan, prompt, fixedNow(), nil)
	require.NoError(t, err)

	script, err := os.ReadFile(files.Script)
	require.NoError(t, err)

	assert.Equal(t, 1, strings.Count(string(script), `"$prompt"`), "the prompt is passed once")
}

func TestRenderedScriptRunsWithExactArgv(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	clone := filepath.Join(root, "clone dir 'quoted' $x")
	require.NoError(t, os.MkdirAll(clone, 0o700))

	agent := filepath.Join(root, "bin with space", "claude")
	require.NoError(t, os.MkdirAll(filepath.Dir(agent), 0o700))
	testutil.WriteExecutable(t, agent, fakeAgent)

	req := baseRequest()
	req.Dir = clone
	req.BinaryPath = agent
	req.Profile.AddDirs = []string{filepath.Join(root, "docs; rm -rf x")}
	req.Profile.ExtraArgs = []string{"--append-system-prompt", "keep `this` & $that", "--verbose"}
	req.Profile.Env = map[string]string{"EXTRA_VAR": "it's $HOME\nnext"}

	plan, err := BuildPlan(req)
	require.NoError(t, err)

	prompt := "Review #7: it's `id` $(id) \"q\"\n\nwith café 🚀\n"

	files, err := WriteLaunchFiles(testPaths(t), plan, prompt, fixedNow(), nil)
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
	assert.Equal(t, strings.TrimRight(prompt, "\n"), fields[3])

	// Codex gets its prompt last, after --, as one argument too.
	codex := codexRequest()
	codex.Dir, codex.BinaryPath, codex.Profile.AddDirs = clone, agent, req.Profile.AddDirs
	codex.Profile.ExtraArgs = []string{"-c", "model_verbosity=\"low $x\""}

	plan, err = BuildPlan(codex)
	require.NoError(t, err)

	files, err = WriteLaunchFiles(testPaths(t), plan, prompt, fixedNow(), nil)
	require.NoError(t, err)

	out, err = runSh(t, append(os.Environ(), "CODEX_HOME="+filepath.Join(root, "inherited")), files.Script)
	require.NoError(t, err)

	fields = strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	require.GreaterOrEqual(t, len(fields), 5)
	assert.Equal(t, plan.Argv(strings.TrimRight(prompt, "\n"))[1:], fields[3:])
	assert.Equal(t, []string{"--", strings.TrimRight(prompt, "\n")}, fields[len(fields)-2:])
}

func TestRenderedScriptStopsWhenTheCloneIsMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	marker := filepath.Join(root, "agent-ran")
	agent := filepath.Join(root, "claude")
	testutil.WriteExecutable(t, agent, "#!/bin/sh\ntouch "+textutil.ShellQuote(marker)+"\n")

	req := baseRequest()
	req.Dir = filepath.Join(root, "gone")
	req.BinaryPath = agent

	plan, err := BuildPlan(req)
	require.NoError(t, err)

	files, err := WriteLaunchFiles(testPaths(t), plan, "Review", fixedNow(), nil)
	require.NoError(t, err)

	_, err = runSh(t, os.Environ(), files.Script)
	require.Error(t, err)
	assert.NoFileExists(t, marker)
}
