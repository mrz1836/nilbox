package workspacetrust_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/workspacetrust"
)

// makeDir creates dir and its parents.
func makeDir(t *testing.T, dir string) string {
	t.Helper()

	require.NoError(t, os.MkdirAll(dir, 0o700))

	return dir
}

// readJSON decodes a config file into raw values.
func readJSON(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // a test file under a temp dir
	require.NoError(t, err)

	var out map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &out))

	return out
}

// projects decodes a config file's project entries.
func projects(t *testing.T, path string) map[string]map[string]any {
	t.Helper()

	var out map[string]map[string]any
	require.NoError(t, json.Unmarshal(readJSON(t, path)["projects"], &out))

	return out
}

// dirNames lists the names in dir.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	return names
}

func TestConfigFile(t *testing.T) {
	t.Parallel()

	home := testutil.RealTempDir(t)
	work := makeDir(t, filepath.Join(home, ".claude_work"))
	makeDir(t, filepath.Join(home, ".claude"))

	assert.Equal(t, filepath.Join(home, ".claude.json"), workspacetrust.ConfigFile("", home),
		"the default profile's config sits in home")
	assert.Equal(t, filepath.Join(work, ".claude.json"), workspacetrust.ConfigFile(work, home),
		"a profile's config sits in its config directory")
	assert.Empty(t, workspacetrust.ConfigFile("", ""), "the default profile needs a home")

	// An empty legacy file is ignored; one that holds anything wins.
	testutil.WriteFile(t, filepath.Join(work, ".config.json"), "")
	assert.Equal(t, filepath.Join(work, ".claude.json"), workspacetrust.ConfigFile(work, home))

	testutil.WriteFile(t, filepath.Join(work, ".config.json"), "{}")
	assert.Equal(t, filepath.Join(work, ".config.json"), workspacetrust.ConfigFile(work, home))

	testutil.WriteFile(t, filepath.Join(home, ".claude", ".config.json"), "{}")
	assert.Equal(t, filepath.Join(home, ".claude", ".config.json"), workspacetrust.ConfigFile("", home))
}

func TestGrantTrustsTheCloneAndKeepsEverythingElse(t *testing.T) {
	t.Parallel()

	home := testutil.RealTempDir(t)
	clone := makeDir(t, filepath.Join(home, "projects", "example-repo"))
	file := filepath.Join(home, ".claude.json")

	testutil.WriteFile(t, file, `{
  "numStartups": 12345678901234567890,
  "oauthAccount": {"emailAddress": "octo-user@example.com"},
  "note": "keeps <, >, & and é as written",
  "projects": {
    "`+filepath.Join(home, "projects", "other-repo")+`": {"hasTrustDialogAccepted": true, "allowedTools": []},
    "`+clone+`": {"hasTrustDialogAccepted": false, "lastCost": 1.25, "mcpServers": {}}
  }
}`)
	require.NoError(t, os.Chmod(file, 0o400))

	changed, err := workspacetrust.Grant(t.Context(), file, clone)
	require.NoError(t, err)
	assert.True(t, changed)

	config := readJSON(t, file)
	assert.Equal(t, "12345678901234567890", string(config["numStartups"]), "large numbers keep every digit")
	assert.JSONEq(t, `{"emailAddress": "octo-user@example.com"}`, string(config["oauthAccount"]))
	assert.Equal(t, `"keeps <, >, & and é as written"`, string(config["note"]), "values keep their bytes")

	entries := projects(t, file)
	assert.Equal(t, map[string]any{"hasTrustDialogAccepted": true, "lastCost": 1.25, "mcpServers": map[string]any{}},
		entries[clone], "only the trust answer changes")
	assert.Equal(t, map[string]any{"hasTrustDialogAccepted": true, "allowedTools": []any{}},
		entries[filepath.Join(home, "projects", "other-repo")])

	data, err := os.ReadFile(file) //nolint:gosec // a test file under a temp dir
	require.NoError(t, err)
	assert.Contains(t, string(data), "\n  \"numStartups\"", "indented by two spaces, as the agent writes it")
	assert.NotEqual(t, byte('\n'), data[len(data)-1], "no trailing newline, as the agent writes it")

	info, err := os.Stat(file)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o400), info.Mode().Perm(), "the file keeps its mode")

	assert.ElementsMatch(t, []string{".claude.json", "projects"}, dirNames(t, home),
		"the lock and the temporary file are gone")
}

func TestGrantLeavesATrustedCloneAlone(t *testing.T) {
	t.Parallel()

	home := testutil.RealTempDir(t)
	clone := makeDir(t, filepath.Join(home, "example-repo"))
	file := filepath.Join(home, ".claude.json")
	content := `{"projects": {"` + clone + `": {"hasTrustDialogAccepted": true}}, "z": 1, "a": 2}`

	testutil.WriteFile(t, file, content)

	changed, err := workspacetrust.Grant(t.Context(), file, clone)
	require.NoError(t, err)
	assert.False(t, changed)

	data, err := os.ReadFile(file) //nolint:gosec // a test file under a temp dir
	require.NoError(t, err)
	assert.Equal(t, content, string(data), "the file is not rewritten")
}

func TestGrantCreatesAMissingConfigFile(t *testing.T) {
	t.Parallel()

	home := testutil.RealTempDir(t)
	clone := makeDir(t, filepath.Join(home, "example-repo"))
	file := filepath.Join(home, ".claude.json")

	changed, err := workspacetrust.Grant(t.Context(), file, clone)
	require.NoError(t, err)
	assert.True(t, changed)

	assert.Equal(t, map[string]map[string]any{clone: {"hasTrustDialogAccepted": true}}, projects(t, file))

	info, err := os.Stat(file)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	// An empty file is an empty config too.
	testutil.WriteFile(t, file, "  \n")

	changed, err = workspacetrust.Grant(t.Context(), file, clone)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Contains(t, projects(t, file), clone)
}

func TestGrantSkipsAConfigDirectoryThatDoesNotExist(t *testing.T) {
	t.Parallel()

	home := testutil.RealTempDir(t)
	clone := makeDir(t, filepath.Join(home, "example-repo"))
	configDir := filepath.Join(home, ".claude_new")

	changed, err := workspacetrust.Grant(t.Context(), filepath.Join(configDir, ".claude.json"), clone)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.NoDirExists(t, configDir, "the agent sets up a profile directory on its own")
}

func TestGrantKeysTheCloneByItsRealPath(t *testing.T) {
	t.Parallel()

	home := testutil.RealTempDir(t)
	clone := makeDir(t, filepath.Join(home, "elsewhere", "example-repo"))
	link := filepath.Join(home, "linked-repo")
	file := filepath.Join(home, ".claude.json")

	require.NoError(t, os.Symlink(clone, link))

	_, err := workspacetrust.Grant(t.Context(), file, link)
	require.NoError(t, err)

	entries := projects(t, file)
	assert.Contains(t, entries, clone, "the agent keys trust by the real path it starts in")
	assert.NotContains(t, entries, link)
}

func TestGrantKeysTheCloneInNFC(t *testing.T) {
	t.Parallel()

	home := testutil.RealTempDir(t)
	// The same name with "é" as one code point (NFC) and as "e" plus a combining accent.
	composed, decomposed := "caf\u00e9-repo", "cafe\u0301-repo"
	clone := makeDir(t, filepath.Join(home, decomposed))
	file := filepath.Join(home, ".claude.json")

	_, err := workspacetrust.Grant(t.Context(), file, clone)
	require.NoError(t, err)

	entries := projects(t, file)
	assert.Contains(t, entries, filepath.Join(home, composed), "the agent keys trust in NFC form")
	assert.Len(t, entries, 1)
}

func TestGrantWritesThroughASymlinkedConfigFile(t *testing.T) {
	t.Parallel()

	home := testutil.RealTempDir(t)
	clone := makeDir(t, filepath.Join(home, "example-repo"))
	dotfiles := makeDir(t, filepath.Join(home, "dotfiles"))
	target := filepath.Join(dotfiles, "claude.json")
	file := filepath.Join(home, ".claude.json")

	testutil.WriteFile(t, target, `{"numStartups": 3}`)
	require.NoError(t, os.Symlink(target, file))

	changed, err := workspacetrust.Grant(t.Context(), file, clone)
	require.NoError(t, err)
	assert.True(t, changed)

	info, err := os.Lstat(file)
	require.NoError(t, err)
	assert.Equal(t, os.ModeSymlink, info.Mode().Type(), "the link stays a link")

	assert.Contains(t, projects(t, target), clone, "the file it points at is updated")
	assert.JSONEq(t, `3`, string(readJSON(t, target)["numStartups"]))
	assert.Equal(t, []string{"claude.json"}, dirNames(t, dotfiles), "no temporary file is left beside it")
}

func TestGrantRefusesAConfigItCannotRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content func(clone string) string
		isErr   error
	}{
		{name: "not JSON", content: func(string) string { return `{"projects": ` }},
		{name: "not an object", content: func(string) string { return `[1, 2]` }, isErr: workspacetrust.ErrNotObject},
		{
			name: "projects not an object", content: func(string) string { return `{"projects": [1]}` },
			isErr: workspacetrust.ErrNotObject,
		},
		{
			name: "project entry not an object", content: func(clone string) string { return `{"projects": {"` + clone + `": true}}` },
			isErr: workspacetrust.ErrNotObject,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := testutil.RealTempDir(t)
			clone := makeDir(t, filepath.Join(home, "example-repo"))
			file := filepath.Join(home, ".claude.json")
			content := []byte(tt.content(clone))

			require.NoError(t, os.WriteFile(file, content, 0o600))

			changed, err := workspacetrust.Grant(t.Context(), file, clone)
			require.Error(t, err)
			assert.False(t, changed)
			assert.Contains(t, err.Error(), file)

			if tt.isErr != nil {
				require.ErrorIs(t, err, tt.isErr)
			}

			data, err := os.ReadFile(file) //nolint:gosec // a test file under a temp dir
			require.NoError(t, err)
			assert.Equal(t, content, data, "a config that cannot be read is left alone")
			assert.ElementsMatch(t, []string{".claude.json", "example-repo"}, dirNames(t, home))
		})
	}
}

func TestGrantNeedsAbsolutePathsAndAClone(t *testing.T) {
	t.Parallel()

	home := testutil.RealTempDir(t)
	clone := makeDir(t, filepath.Join(home, "example-repo"))

	_, err := workspacetrust.Grant(t.Context(), ".claude.json", clone)
	require.ErrorIs(t, err, workspacetrust.ErrRelativePath)

	_, err = workspacetrust.Grant(t.Context(), filepath.Join(home, ".claude.json"), "example-repo")
	require.ErrorIs(t, err, workspacetrust.ErrRelativePath)

	_, err = workspacetrust.Grant(t.Context(), filepath.Join(home, ".claude.json"), filepath.Join(home, "gone"))
	require.ErrorIs(t, err, os.ErrNotExist)
	assert.NoFileExists(t, filepath.Join(home, ".claude.json"))
}
