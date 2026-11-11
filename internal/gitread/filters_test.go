package gitread

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestStatusTurnsOffTheClonesFilterDrivers(t *testing.T) {
	t.Parallel()

	rec := &recorder{replies: map[string]reply{
		words(filterDriversArgs()): {out: "local\tfilter.x.clean sh -c 'touch /tmp/owned; cat'\n" +
			"global\tfilter.lfs.process git-lfs filter-process\n" +
			"worktree\tfilter.odd.name.process y\n" +
			"local\tfilter.x.process x\n"},
	}, answer: func(_ context.Context, _ []string) (string, error) { return "", nil }}

	_, err := fakeGit(plainEnv(), rec).Changes(t.Context(), repoDir)
	require.NoError(t, err)

	calls := rec.Calls()
	require.Len(t, calls, 2, "the drivers are read first, then the status runs")

	status := strings.Join(calls[1].argv, " ")
	assert.Contains(t, status, "-c filter.x.clean= -c filter.x.process= -c filter.odd.name.clean= -c filter.odd.name.process=")
	assert.NotContains(t, status, "filter.lfs", "the user's own drivers still run")
	assert.True(t, strings.HasSuffix(status, "status --porcelain -z --ignore-submodules=dirty"))

	rec = &recorder{replies: map[string]reply{
		words(filterDriversArgs()): {out: "local\tfilter.x.clean sh -c 'touch /tmp/owned; cat'\n"},
	}, answer: func(_ context.Context, _ []string) (string, error) { return "", nil }}

	_, err = fakeGit(plainEnv(), rec).Entries(t.Context(), repoDir)
	require.NoError(t, err)

	calls = rec.Calls()
	require.Len(t, calls, 2, "the entries' status turns them off too")

	status = strings.Join(calls[1].argv, " ")
	assert.Contains(t, status, "-c filter.x.clean= -c filter.x.process=")
	assert.True(t, strings.HasSuffix(status, "status --porcelain=v2 -z --untracked-files=all --ignore-submodules=dirty"))
}

func TestStatusRefusesADriverItCantName(t *testing.T) {
	t.Parallel()

	for _, line := range []string{
		"local\tfilter.a=b.clean evil\n",
		"local\tfilter.a\x01b.clean evil\n",
		"local\tfilter..clean evil\n",
		"local\tother.x.clean evil\n",
	} {
		rec := &recorder{replies: map[string]reply{words(filterDriversArgs()): {out: line}}}

		_, err := fakeGit(plainEnv(), rec).Changes(t.Context(), repoDir)
		require.ErrorIs(t, err, ErrGitCommandNotAllowed, "%q", line)
		assert.Len(t, rec.Calls(), 1, "%q: no status ran", line)
	}
}

func TestStatusStopsWhenTheDriversCantBeRead(t *testing.T) {
	t.Parallel()

	rec := &recorder{replies: map[string]reply{words(filterDriversArgs()): {code: 128}}}

	_, err := fakeGit(plainEnv(), rec).Changes(t.Context(), repoDir)
	require.Error(t, err)
	assert.Len(t, rec.Calls(), 1, "no status ran")
}

// TestStatusRunsNoFilterTheCloneNames reproduces a clone whose own config makes a filter
// driver run a program, with every file's stat data changed, as in a copied clone: a
// plain status runs the program, and gitread's doesn't.
func TestStatusRunsNoFilterTheCloneNames(t *testing.T) {
	t.Parallel()

	env := testutil.Git(t)
	dir := filepath.Join(t.TempDir(), "clone")
	marker := filepath.Join(t.TempDir(), "ran")

	testutil.InitRepo(t, dir)
	writeFile(t, dir, ".gitattributes", "* filter=x")
	commitFile(t, dir, "a.txt", "a")
	testutil.RunGit(t, dir, "config", "filter.x.clean", "touch '"+marker+"'; cat")

	later := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "a.txt"), later, later))

	g := New(Config{Env: func() []string { return env }})

	_, err := g.Changes(t.Context(), dir)
	require.NoError(t, err)
	assert.NoFileExists(t, marker, "gitread's status ran the clone's filter")

	testutil.RunGit(t, dir, "status", "--porcelain")
	assert.FileExists(t, marker, "the clone is set up so that a plain status runs it")
}

// TestEntriesRunNoFilterTheCloneNames is TestStatusRunsNoFilterTheCloneNames for the
// porcelain v2 status a checks run takes before and after: it, too, runs none of the
// clone's own filter drivers, and looks inside no submodule.
func TestEntriesRunNoFilterTheCloneNames(t *testing.T) {
	t.Parallel()

	env := testutil.Git(t)
	dir := filepath.Join(t.TempDir(), "clone")
	marker := filepath.Join(t.TempDir(), "ran")

	testutil.InitRepo(t, dir)
	writeFile(t, dir, ".gitattributes", "* filter=x")
	commitFile(t, dir, "a.txt", "a")
	testutil.RunGit(t, dir, "config", "filter.x.clean", "touch '"+marker+"'; cat")

	later := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "a.txt"), later, later))

	g := New(Config{Env: func() []string { return env }})

	_, err := g.Entries(t.Context(), dir)
	require.NoError(t, err)
	assert.NoFileExists(t, marker, "gitread's status ran the clone's filter")

	testutil.RunGit(t, dir, "status", "--porcelain=v2")
	assert.FileExists(t, marker, "the clone is set up so that a plain status runs it")
}

func TestFilterName(t *testing.T) {
	t.Parallel()

	for key, want := range map[string]string{
		"filter.lfs.clean":        "lfs",
		"filter.a.b.process":      "a.b",
		"filter.with space.clean": "with space",
	} {
		got, ok := filterName(key)
		assert.True(t, ok, key)
		assert.Equal(t, want, got, key)
	}

	for _, key := range []string{"filter.clean", "filter..clean", "filter.a=b.clean", "core.x.clean", "filter.a\tb.clean"} {
		_, ok := filterName(key)
		assert.False(t, ok, key)
	}
}

// TestStatusRunsNoFilterASubmoduleNames reproduces a submodule whose own config and
// attributes name a filter driver: a status in the superproject would check the submodule
// with a status of its own, which runs the driver. gitread's status doesn't look inside.
func TestStatusRunsNoFilterASubmoduleNames(t *testing.T) {
	t.Parallel()

	env := testutil.Git(t)
	root := t.TempDir()
	sub := filepath.Join(root, "sub-origin")
	super := filepath.Join(root, "super")
	marker := filepath.Join(t.TempDir(), "ran")

	testutil.InitRepo(t, sub)
	commitFile(t, sub, "a.txt", "a")
	testutil.InitRepo(t, super)
	commitFile(t, super, "readme.txt", "readme")
	testutil.RunGit(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", sub, "sub")
	testutil.RunGit(t, super, "commit", "-q", "-m", "add sub")

	modules := strings.TrimSpace(testutil.RunGit(t, filepath.Join(super, "sub"), "rev-parse", "--absolute-git-dir"))
	testutil.RunGit(t, filepath.Join(super, "sub"), "config", "filter.evil.clean", "touch '"+marker+"'; cat")
	require.NoError(t, os.MkdirAll(filepath.Join(modules, "info"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(modules, "info", "attributes"), []byte("* filter=evil\n"), 0o600))

	later := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(super, "sub", "a.txt"), later, later))

	g := New(Config{Env: func() []string { return env }})

	_, err := g.Changes(t.Context(), super)
	require.NoError(t, err)
	assert.NoFileExists(t, marker, "gitread's status ran the submodule's filter")

	testutil.RunGit(t, super, "status", "--porcelain")
	assert.FileExists(t, marker, "the superproject is set up so that a plain status runs it")
}
