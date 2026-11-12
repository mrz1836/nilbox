package testutil_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestPlantingKeysAreAsGitListsThem(t *testing.T) {
	t.Parallel()

	p := testutil.Planting{Name: "evil", Settings: [][2]string{
		{"Filter.Evil.Smudge", "%s"}, {"core.sshCommand", "%s"}, {"includeIf.gitdir:**/x/**.path", "%s"},
	}}

	assert.Equal(t, []string{"filter.Evil.smudge", "core.sshcommand", "includeif.gitdir:**/x/**.path"}, p.Keys())
	assert.Empty(t, testutil.Planting{}.Keys())
}

func TestPlanterPlantsAFakeInTheClonesOwnConfig(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	dir := filepath.Join(home, "example-repo")
	testutil.InitRepo(t, dir)

	p := testutil.NewPlanter(t, home)
	marker := p.Plant(dir, testutil.Planting{Name: "evil-editor", Settings: [][2]string{
		{"core.editor", "%s --wait"}, {"core.pager", "less"},
	}})

	assert.Equal(t, filepath.Join(home, "ran", "evil-editor"), marker)
	assert.DirExists(t, filepath.Dir(marker), "the folder the marker goes in is made")
	assert.NoFileExists(t, marker, "nothing ran yet")

	editor := strings.TrimSpace(testutil.RunGit(t, dir, "config", "--local", "--get", "core.editor"))
	program, ok := strings.CutSuffix(editor, " --wait")
	require.True(t, ok, editor)
	assert.Equal(t, "evil-editor", filepath.Base(program), "%s stands for the fake's path")
	assert.Equal(t, "less\n", testutil.RunGit(t, dir, "config", "--local", "--get", "core.pager"))

	assert.Len(t, p.Environ(), len(testutil.GitEnv(home))+1, "the fake's entry is added to git's environment")
	assert.NoFileExists(t, filepath.Join(dir, ".git", "objects", "info", "alternates"))
}

func TestPlanterGivesAnAlternateRefsCommandAnAlternateStore(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	dir := filepath.Join(home, "example-repo")
	testutil.InitRepo(t, dir)

	testutil.NewPlanter(t, home).Plant(dir, testutil.Planting{
		Name: testutil.AlternateRefs, Settings: [][2]string{{"core.alternateRefsCommand", "%s"}},
	})

	alternates, err := os.ReadFile(filepath.Join(dir, ".git", "objects", "info", "alternates")) //nolint:gosec // the test's own clone
	require.NoError(t, err)

	store, ok := strings.CutSuffix(string(alternates), "\n")
	require.True(t, ok)
	assert.DirExists(t, store, "an empty repository's object store")
	assert.Equal(t, "objects", filepath.Base(store))
}

func TestPlanterHooksRunAsFakes(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	dir := filepath.Join(home, "example-repo")
	testutil.InitRepo(t, dir)

	p := testutil.NewPlanter(t, home)
	marker := p.PlantHooks(dir, "post-commit")
	assert.Equal(t, filepath.Join(home, "ran", "post-commit"), marker)

	hooks := strings.TrimSpace(testutil.RunGit(t, dir, "config", "--local", "--get", "core.hooksPath"))
	assert.FileExists(t, filepath.Join(hooks, "post-commit"))

	p.AddEnv("EXAMPLE_ADDED=1")
	assert.Contains(t, p.Environ(), "EXAMPLE_ADDED=1")

	cmd := exec.CommandContext(t.Context(), "git", "commit", "-q", "--allow-empty", "-m", "first")
	cmd.Dir = dir
	cmd.Env = p.Environ()

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	body, err := os.ReadFile(marker) //nolint:gosec // the marker the test planted
	require.NoError(t, err)
	assert.Equal(t, "ran", string(body), "git ran the planted hook, which played as a fake in the Planter's environment")
}
