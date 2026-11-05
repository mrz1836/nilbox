package launcher

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
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
	assert.True(t, launchFilePattern(".command").MatchString(filepath.Base(files.Script)))
	assert.Equal(t, ".command", filepath.Ext(files.Script))
	assert.Equal(t, strings.TrimSuffix(files.Prompt, promptFileSuffix), strings.TrimSuffix(files.Script, ".command"),
		"the prompt file and the script share a base name")

	prompt, err := os.ReadFile(files.Prompt)
	require.NoError(t, err)
	assert.Equal(t, "Review pull request #7", string(prompt))

	script, err := os.ReadFile(files.Script)
	require.NoError(t, err)

	plan.PromptFile = files.Prompt
	assert.Equal(t, string(POSIXRenderer{}.Render(plan)), string(script), "the script reads the written prompt file")

	t.Run("a second launch in the same second gets new files", func(t *testing.T) {
		again, err := WriteLaunchFiles(paths, plan, "Review again", fixedNow())
		require.NoError(t, err)
		assert.NotEqual(t, files, again)
		assert.FileExists(t, files.Prompt)
		assert.FileExists(t, files.Script)
	})

	t.Run("issues are named by kind", func(t *testing.T) {
		req := baseRequest()
		req.Item.Kind, req.Item.Number = KindIssue, 12

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
		{name: "unknown kind", plan: func() Plan { p := valid; p.Kind = "../x"; return p }, prompt: "p", want: ErrInvalidKind},
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
		_, err := writeLaunchFiles(testPaths(t), plan, "p", fixedNow(), POSIXRenderer{}, failingReader{})
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

		first, err := writeLaunchFiles(paths, plan, "first", fixedNow(), POSIXRenderer{}, bytes.NewReader(suffix))
		require.NoError(t, err)

		_, err = writeLaunchFiles(paths, plan, "second", fixedNow(), POSIXRenderer{}, bytes.NewReader(suffix))
		require.ErrorIs(t, err, os.ErrExist)

		stored, err := os.ReadFile(first.Prompt)
		require.NoError(t, err)
		assert.Equal(t, "first", string(stored))
	})

	t.Run("a failed script write removes its prompt file", func(t *testing.T) {
		paths := testPaths(t)
		suffix := bytes.Repeat([]byte{0xcd}, suffixBytes)

		planned, err := writeLaunchFiles(paths, plan, "first", fixedNow(), POSIXRenderer{}, bytes.NewReader(suffix))
		require.NoError(t, err)
		require.NoError(t, os.Remove(planned.Prompt))

		_, err = writeLaunchFiles(paths, plan, "second", fixedNow(), POSIXRenderer{}, bytes.NewReader(suffix))
		require.ErrorIs(t, err, os.ErrExist)
		assert.NoFileExists(t, planned.Prompt, "the orphaned prompt file is cleaned up")
	})
}
