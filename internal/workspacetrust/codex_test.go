package workspacetrust_test

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/workspacetrust"
)

// codexDirs returns a fresh home with a ~/.codex config directory, and two real folders
// to trust.
func codexDirs(t *testing.T) (home, worktree, clone string) {
	t.Helper()

	home = testutil.RealTempDir(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex"), 0o700))

	worktree = filepath.Join(home, "projects", "."+brand.Slug, "worktrees", "example-repo", "pr-7")
	clone = filepath.Join(home, "projects", "example-repo")

	require.NoError(t, os.MkdirAll(worktree, 0o700))
	require.NoError(t, os.MkdirAll(clone, 0o700))

	return home, worktree, clone
}

// trustLevels reads each project's trust_level from a Codex config.
func trustLevels(t *testing.T, path string) map[string]string {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // the test's own file
	require.NoError(t, err)

	var doc struct {
		Projects map[string]struct {
			TrustLevel string `toml:"trust_level"`
		} `toml:"projects"`
	}

	require.NoError(t, toml.Unmarshal(data, &doc))

	out := map[string]string{}
	for key, p := range doc.Projects {
		out[key] = p.TrustLevel
	}

	return out
}

func TestCodexTrustAppendsAndKeepsTheRest(t *testing.T) {
	t.Parallel()

	home, worktree, clone := codexDirs(t)
	config := filepath.Join(home, ".codex", "config.toml")
	original := "# my settings\nmodel = \"gpt-6-sol\"  # keep this\n\n[projects.\"/srv/other\"]\ntrust_level = \"trusted\""
	require.NoError(t, os.WriteFile(config, []byte(original), 0o600))
	require.NoError(t, os.Chmod(config, 0o640)) //nolint:gosec // a mode the user chose, which a grant keeps

	codex := workspacetrust.Codex{Home: home}
	require.NoError(t, codex.Trust(t.Context(), "", []string{worktree, clone, worktree}))

	data, err := os.ReadFile(config) //nolint:gosec // the test's own file
	require.NoError(t, err)
	assert.Equal(t, original+"\n\n[projects.\""+worktree+"\"]\ntrust_level = \"trusted\"\n\n[projects.\""+clone+
		"\"]\ntrust_level = \"trusted\"\n", string(data), "appended once each, the user's text kept byte for byte")

	info, err := os.Stat(config)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm(), "the file keeps its mode")

	require.NoError(t, codex.Trust(t.Context(), "", []string{worktree, clone}))

	again, err := os.ReadFile(config) //nolint:gosec // the test's own file
	require.NoError(t, err)
	assert.Equal(t, string(data), string(again), "a trusted folder is not added twice")
}

func TestCodexTrustCreatesTheConfigInAProfilesDirectory(t *testing.T) {
	t.Parallel()

	home, worktree, _ := codexDirs(t)
	profile := filepath.Join(home, ".codex_work")
	require.NoError(t, os.MkdirAll(profile, 0o700))

	require.NoError(t, workspacetrust.Codex{Home: home}.Trust(t.Context(), profile, []string{worktree}))

	assert.Equal(t, map[string]string{worktree: "trusted"}, trustLevels(t, filepath.Join(profile, "config.toml")))
	assert.NoFileExists(t, filepath.Join(home, ".codex", "config.toml"), "the default profile's config is left alone")

	info, err := os.Stat(filepath.Join(profile, "config.toml"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestCodexTrustLeavesWhatItShouldNotTouch(t *testing.T) {
	t.Parallel()

	home, worktree, clone := codexDirs(t)
	config := filepath.Join(home, ".codex", "config.toml")

	// The user said no to one folder; that stays, and only the other is trusted.
	untrusted := "[projects.\"" + worktree + "\"]\ntrust_level = \"untrusted\"\n"
	require.NoError(t, os.WriteFile(config, []byte(untrusted), 0o600))

	var logs testutil.Logs

	require.NoError(t, workspacetrust.Codex{Home: home, Logger: logs.Logger()}.Trust(t.Context(), "", []string{worktree, clone}))
	assert.Equal(t, map[string]string{worktree: "untrusted", clone: "trusted"}, trustLevels(t, config))
	assert.Contains(t, logs.String(), "left a Codex project's own trust setting as it is")

	// A config that is not TOML is never written.
	broken := "model = \n[[[\n"
	require.NoError(t, os.WriteFile(config, []byte(broken), 0o600))
	require.NoError(t, workspacetrust.Codex{Home: home, Logger: logs.Logger()}.Trust(t.Context(), "", []string{clone}))

	data, err := os.ReadFile(config) //nolint:gosec // the test's own file
	require.NoError(t, err)
	assert.Equal(t, broken, string(data))
	assert.Contains(t, logs.String(), "Codex's config could not be read")

	// Projects written inline cannot take another table; nothing is written.
	inline := "projects = { \"/srv/x\" = { trust_level = \"trusted\" } }\n"
	require.NoError(t, os.WriteFile(config, []byte(inline), 0o600))
	require.NoError(t, workspacetrust.Codex{Home: home, Logger: logs.Logger()}.Trust(t.Context(), "", []string{clone}))

	data, err = os.ReadFile(config) //nolint:gosec // the test's own file
	require.NoError(t, err)
	assert.Equal(t, inline, string(data))

	// A profile Codex has never run with is left for Codex to set up.
	require.NoError(t, workspacetrust.Codex{Home: home}.Trust(t.Context(), filepath.Join(home, ".codex_new"), []string{clone}))
	assert.NoDirExists(t, filepath.Join(home, ".codex_new"))
}

func TestCodexTrustFollowsALinkedConfigAndResolvesFolders(t *testing.T) {
	t.Parallel()

	home, worktree, _ := codexDirs(t)
	linked := filepath.Join(home, "dotfiles", "codex.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(linked), 0o700))
	require.NoError(t, os.WriteFile(linked, []byte("model = \"x\"\n"), 0o600))
	require.NoError(t, os.Symlink(linked, filepath.Join(home, ".codex", "config.toml")))

	link := filepath.Join(home, "link-to-worktree")
	require.NoError(t, os.Symlink(worktree, link))

	require.NoError(t, workspacetrust.Codex{Home: home}.Trust(t.Context(), "", []string{link}))

	target, err := os.Readlink(filepath.Join(home, ".codex", "config.toml"))
	require.NoError(t, err)
	assert.Equal(t, linked, target, "the link stays a link")
	assert.Equal(t, map[string]string{worktree: "trusted"}, trustLevels(t, linked), "the folder is trusted by its real path")
}

func TestCodexTrustKeepsEveryLaunchsEntryWhenLaunchesRunAtOnce(t *testing.T) {
	t.Parallel()

	home, _, _ := codexDirs(t)
	config := filepath.Join(home, ".codex", "config.toml")
	require.NoError(t, os.WriteFile(config, []byte("model = \"gpt-6-sol\"\n"), 0o600))

	dirs := make([]string, 12)
	for i := range dirs {
		dirs[i] = filepath.Join(home, "projects", "repo-"+strconv.Itoa(i))
		require.NoError(t, os.MkdirAll(dirs[i], 0o700))
	}

	var wg sync.WaitGroup

	for _, dir := range dirs {
		wg.Go(func() {
			assert.NoError(t, workspacetrust.Codex{Home: home}.Trust(t.Context(), "", []string{dir}))
		})
	}

	wg.Wait()

	levels := trustLevels(t, config)
	for _, dir := range dirs {
		assert.Equal(t, "trusted", levels[dir], "each launch's folder stays trusted")
	}
}

func TestCodexTrustQuotesAnyPath(t *testing.T) {
	t.Parallel()

	home := testutil.RealTempDir(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex"), 0o700))

	odd := filepath.Join(home, `a "quoted" \ dir`)
	require.NoError(t, os.MkdirAll(odd, 0o700))

	require.NoError(t, workspacetrust.Codex{Home: home}.Trust(t.Context(), "", []string{odd}))
	assert.Equal(t, map[string]string{odd: "trusted"}, trustLevels(t, filepath.Join(home, ".codex", "config.toml")))
}

func TestCodexTrustRefusesRelativePaths(t *testing.T) {
	t.Parallel()

	home, worktree, _ := codexDirs(t)

	require.ErrorIs(t, workspacetrust.Codex{Home: home}.Trust(t.Context(), "relative", []string{worktree}),
		workspacetrust.ErrRelativePath)
	require.ErrorIs(t, workspacetrust.Codex{Home: home}.Trust(t.Context(), "", []string{"relative"}),
		workspacetrust.ErrRelativePath)
	require.NoError(t, workspacetrust.Codex{}.Trust(t.Context(), "", []string{worktree}), "no home, no default profile")
}

func TestClaudeTrustGrantsEachFolder(t *testing.T) {
	t.Parallel()

	home, worktree, clone := codexDirs(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o700))

	require.NoError(t, workspacetrust.Claude{Home: home}.Trust(t.Context(), "", []string{worktree, clone}))

	data, err := os.ReadFile(filepath.Join(home, ".claude.json")) //nolint:gosec // the test's own file
	require.NoError(t, err)
	assert.Contains(t, string(data), worktree)
	assert.Contains(t, string(data), clone)

	require.NoError(t, workspacetrust.Claude{}.Trust(t.Context(), "", []string{worktree}), "no home, no default config")
}
