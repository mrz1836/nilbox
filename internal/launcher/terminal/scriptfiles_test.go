package terminal

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// writeNow is the time the file tests write at.
func writeNow() time.Time {
	return time.Date(2026, 3, 14, 15, 9, 26, 0, time.UTC)
}

// fixedRandom names a script <prefix>-20260314T150926Z-0a0b0c0d.
func fixedRandom() *bytes.Reader {
	return bytes.NewReader([]byte{0x0a, 0x0b, 0x0c, 0x0d})
}

// modeOf returns the permission bits of path.
func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()

	info, err := os.Stat(path)
	require.NoError(t, err)

	return info.Mode().Perm()
}

// touch creates an empty file in dir and returns its path.
func touch(t *testing.T, dir, name string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	return path
}

func TestWriteScript(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "launch")
	s := followedRun()

	path, err := WriteScript(dir, "run-42", s, writeNow(), fixedRandom(), nil)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, "run-42-20260314T150926Z-0a0b0c0d.command"), path)
	assert.Equal(t, os.FileMode(0o700), modeOf(t, dir), "the folder is private")
	assert.Equal(t, os.FileMode(0o700), modeOf(t, path), "the script is runnable by the user only")

	got, err := os.ReadFile(path) //nolint:gosec // the test's own script
	require.NoError(t, err)
	assert.Equal(t, string(POSIXRenderer{}.Render(s)), string(got))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "the script alone, with no prompt file")

	t.Run("a name already taken is never overwritten", func(t *testing.T) {
		t.Parallel()

		_, err := WriteScript(dir, "run-42", Script{Dir: "/work/other"}, writeNow(), fixedRandom(), nil)
		require.ErrorIs(t, err, os.ErrExist)

		again, err := os.ReadFile(path) //nolint:gosec // the test's own script
		require.NoError(t, err)
		assert.Equal(t, got, again)
	})

	t.Run("names from crypto/rand by default", func(t *testing.T) {
		t.Parallel()

		other, err := WriteScript(dir, "run-43", s, writeNow(), nil, nil)
		require.NoError(t, err)
		assert.Regexp(t, `^run-43-20260314T150926Z-[0-9a-f]{8}\.command$`, filepath.Base(other))
	})
}

func TestWriteScriptRejectsPrefixes(t *testing.T) {
	t.Parallel()

	for _, prefix := range []string{
		"", "run", "run-", "run-x", "run-1-2", "run-1 ", "RUN-1", "../run-1", "run-1/..", "a/run-1",
		"shell-1", "release-1", "prs-1", "pr-", "issue-#1",
	} {
		dir := filepath.Join(t.TempDir(), "launch")

		_, err := WriteScript(dir, prefix, followedRun(), writeNow(), fixedRandom(), nil)
		require.ErrorIs(t, err, ErrInvalidScriptPrefix, "%q", prefix)
		assert.NoDirExists(t, dir, "nothing is written for %q", prefix)
	}
}

func TestWriteScriptFailures(t *testing.T) {
	t.Parallel()

	t.Run("no randomness", func(t *testing.T) {
		t.Parallel()

		_, err := WriteScript(t.TempDir(), "run-1", followedRun(), writeNow(), bytes.NewReader(nil), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name script files")
	})

	t.Run("the folder cannot be created", func(t *testing.T) {
		t.Parallel()

		blocker := touch(t, t.TempDir(), "file")

		_, err := WriteScript(filepath.Join(blocker, "launch"), "run-1", followedRun(), writeNow(), fixedRandom(), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "create script directory")
	})
}

// TestPruningTouchesOnlyItsOwnFiles fills a folder with old files of every kind, and
// checks that a write prunes exactly the old scripts and prompt files named by its own
// prefixes, pr, issue, and run, and leaves everything else.
func TestPruningTouchesOnlyItsOwnFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outside := touch(t, t.TempDir(), "target.command")

	old := []string{
		touch(t, dir, "pr-3-20260306T150925Z-0a0b0c0d.prompt.md"),
		touch(t, dir, "pr-3-20260306T150925Z-0a0b0c0d.command"),
		touch(t, dir, "issue-9-20251201T000000Z-ffffffff.command"),
		touch(t, dir, "issue-9-20251201T000000Z-ffffffff.prompt.md"),
		touch(t, dir, "run-42-20250101T000000Z-01234567.command"),
		touch(t, dir, "run-0-20250101T000000Z-89abcdef.command"),
	}

	kept := []string{
		// Exactly seven days old, and newer.
		touch(t, dir, "pr-4-20260307T150926Z-01234567.command"),
		touch(t, dir, "run-5-20260310T120000Z-89abcdef.command"),
		// Other prefixes, however old they look.
		touch(t, dir, "release-1-20250101T000000Z-00000000.command"),
		touch(t, dir, "runs-1-20250101T000000Z-00000000.command"),
		touch(t, dir, "xrun-1-20250101T000000Z-00000000.command"),
		touch(t, dir, "shell-1-20250101T000000Z-00000000.command"),
		touch(t, dir, "PR-1-20250101T000000Z-00000000.command"),
		touch(t, dir, ".run-1-20250101T000000Z-00000000.command"),
		touch(t, dir, "run-20250101T000000Z-00000000.command"),
		// Its own prefixes with another suffix, or a malformed name.
		touch(t, dir, "run-1-20250101T000000Z-00000000.log"),
		touch(t, dir, "run-1-20250101T000000Z-00000000.pid"),
		touch(t, dir, "run-1-20250101T000000Z-00000000.exit"),
		touch(t, dir, "run-1-20250101T000000Z-00000000.sh"),
		touch(t, dir, "run-1-20250101T000000Z-00000000.command.bak"),
		touch(t, dir, "run-1-20250101T000000Z-00000000"),
		touch(t, dir, "run-1-20250101T000000Z-ABCDEF01.command"),
		touch(t, dir, "run-1-20250101T000000Z-0000000.command"),
		touch(t, dir, "run-1-20251399T999999Z-00000000.command"),
		touch(t, dir, "run-1-2025-01-01-00000000.command"),
		// Anything else.
		touch(t, dir, "42.log"),
		touch(t, dir, "42.pid"),
		touch(t, dir, "42.exit"),
		touch(t, dir, "notes.txt"),
		touch(t, dir, "prompts.json"),
	}

	// Only regular files are pruned: a folder or a link named like an old script stays,
	// and so does what the link points at.
	oldDir := filepath.Join(dir, "run-2-20250101T000000Z-00000000.command")
	require.NoError(t, os.Mkdir(oldDir, 0o700))

	oldLink := filepath.Join(dir, "run-3-20250101T000000Z-00000000.command")
	require.NoError(t, os.Symlink(outside, oldLink))

	var logs testutil.Logs

	path, err := ScriptWriter{Random: fixedRandom(), Log: logs.Logger()}.Write(dir, "run-7", followedRun(), writeNow())
	require.NoError(t, err)

	for _, p := range old {
		assert.NoFileExists(t, p)
	}

	for _, p := range append(kept, path, outside) {
		assert.FileExists(t, p)
	}

	assert.DirExists(t, oldDir)

	_, err = os.Lstat(oldLink)
	require.NoError(t, err, "the link stays")
	assert.Empty(t, logs.String(), "nothing failed")
}

func TestPruneScriptsLogsWhatItCannotList(t *testing.T) {
	t.Parallel()

	var logs testutil.Logs

	pruneScripts(filepath.Join(t.TempDir(), "missing"), writeNow(), ".command", logs.Logger())

	assert.Contains(t, logs.String(), `level=WARN msg="could not list old script files"`)
}

func TestCheckScriptFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	other := t.TempDir()

	script, err := WriteScript(dir, "run-42", followedRun(), writeNow(), fixedRandom(), nil)
	require.NoError(t, err)
	require.NoError(t, CheckScriptFile(dir, script))

	launch, err := ScriptWriter{Random: fixedRandom()}.WriteWithPrompt(dir, "pr-7", Script{Dir: "/work"}, "p", writeNow())
	require.NoError(t, err)
	require.NoError(t, CheckScriptFile(dir, launch.Script))

	elsewhere := touch(t, other, filepath.Base(script))
	link := filepath.Join(dir, "run-3-20260314T150926Z-00000000.command")
	require.NoError(t, os.Symlink(elsewhere, link))

	folder := filepath.Join(dir, "run-4-20260314T150926Z-00000000.command")
	require.NoError(t, os.Mkdir(folder, 0o700))

	for name, path := range map[string]string{
		"a prompt file":           launch.Prompt,
		"the same name elsewhere": elsewhere,
		"a link":                  link,
		"a folder":                folder,
		"missing":                 filepath.Join(dir, "run-5-20260314T150926Z-00000000.command"),
		"another name":            touch(t, dir, "notes.command"),
		"another extension":       touch(t, dir, "run-6-20260314T150926Z-00000000.sh"),
		"relative":                filepath.Base(script),
		"not clean":               filepath.Join(dir, "x") + "/../" + filepath.Base(script),
		"a control character":     filepath.Join(dir, "run-7-20260314T150926Z-00000000.command\n"),
		"empty":                   "",
	} {
		require.ErrorIs(t, CheckScriptFile(dir, path), ErrInvalidScriptPath, name)
	}
}
