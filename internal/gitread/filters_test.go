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
		words(filterDriversArgs()): {out: zLines("local\tfilter.x.clean sh -c 'touch /tmp/owned; cat'\n" +
			"global\tfilter.lfs.process git-lfs filter-process\n" +
			"worktree\tfilter.odd.name.process y\n" +
			"local\tfilter.x.process x\n")},
	}, answer: func(_ context.Context, _ []string) (string, error) { return "", nil }}

	_, err := fakeGit(plainEnv(), rec).Changes(t.Context(), repoDir)
	require.NoError(t, err)

	calls := rec.Calls()
	require.Len(t, calls, 2, "the drivers are read first, then the status runs")

	status := strings.Join(calls[1].argv, " ")
	assert.Contains(t, status, "-c filter.x.clean= -c filter.x.smudge= -c filter.x.process= -c filter.x.required=false "+
		"-c filter.odd.name.clean= -c filter.odd.name.smudge= -c filter.odd.name.process= -c filter.odd.name.required=false")
	assert.NotContains(t, status, "filter.lfs", "the user's own drivers still run")
	assert.True(t, strings.HasSuffix(status, "status --porcelain -z --ignore-submodules=dirty"))

	rec = &recorder{replies: map[string]reply{
		words(filterDriversArgs()): {out: zLines("local\tfilter.x.clean sh -c 'touch /tmp/owned; cat'\n")},
	}, answer: func(_ context.Context, _ []string) (string, error) { return "", nil }}

	_, err = fakeGit(plainEnv(), rec).Entries(t.Context(), repoDir)
	require.NoError(t, err)

	calls = rec.Calls()
	require.Len(t, calls, 2, "the entries' status turns them off too")

	status = strings.Join(calls[1].argv, " ")
	assert.Contains(t, status, "-c filter.x.clean= -c filter.x.smudge= -c filter.x.process= -c filter.x.required=false")
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
		rec := &recorder{replies: map[string]reply{words(filterDriversArgs()): {out: zLines(line)}}}

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

func TestStatusTrustsOnlyTheUsersOwnScopes(t *testing.T) {
	t.Parallel()

	rec := &recorder{replies: map[string]reply{
		words(filterDriversArgs()): {out: zLines("system\tfilter.sys.process a\n" +
			"command\tfilter.mine.clean b\n" +
			"unknown\tfilter.odd.clean c\n")},
	}, answer: func(_ context.Context, _ []string) (string, error) { return "", nil }}

	_, err := fakeGit(plainEnv(), rec).Changes(t.Context(), repoDir)
	require.NoError(t, err)

	calls := rec.Calls()
	require.Len(t, calls, 2)

	status := strings.Join(calls[1].argv, " ")
	assert.Contains(t, status, "-c filter.odd.clean= -c filter.odd.smudge= -c filter.odd.process= "+
		"-c filter.odd.required=false", "a scope that isn't the user's is the clone's")
	assert.NotContains(t, status, "filter.sys", "the system config is the user's")
	assert.NotContains(t, status, "filter.mine", "the environment's settings are the user's")
}

func TestTheClonesOwnGitLFSStaysOn(t *testing.T) {
	t.Parallel()

	lfs := "local\tfilter.lfs.clean git-lfs clean -- %f\n" +
		"local\tfilter.lfs.smudge git-lfs smudge -- %f\n" +
		"local\tfilter.lfs.process git-lfs filter-process\n"

	off, err := cloneFiltersOff(settingsOf(t, lfs))
	require.NoError(t, err)
	assert.Empty(t, off, "git lfs install --local runs the user's own git-lfs")

	for _, line := range []string{
		"local\tfilter.lfs.smudge git-lfs smudge -- %f; touch /nonexistent/owned\n",
		"local\tfilter.lfs.process /srv/elsewhere/git-lfs filter-process\n",
		"local\tfilter.lfs.clean git-lfs smudge -- %f\n",
	} {
		off, err = cloneFiltersOff(settingsOf(t, lfs+line))
		require.NoError(t, err)
		assert.Equal(t, []string{
			"-c", "filter.lfs.clean=", "-c", "filter.lfs.smudge=", "-c", "filter.lfs.process=",
			"-c", "filter.lfs.required=false",
		}, off, "%q: any other command turns the whole driver off", line)
	}
}

// zLines writes "scope\tkey value\n" lines as `git config --show-scope -z --get-regexp`
// writes them: the scope, a NUL, the key, a newline and the value, and a NUL.
func zLines(lines string) string {
	var b strings.Builder

	for line := range strings.Lines(lines) {
		scope, rest, _ := strings.Cut(strings.TrimSuffix(line, "\n"), "\t")
		key, value, _ := strings.Cut(rest, " ")
		b.WriteString(scope + "\x00" + key + "\n" + value + "\x00")
	}

	return b.String()
}

// settingsOf reads "scope\tkey value\n" lines as settings.
func settingsOf(t *testing.T, lines string) []scopedSetting {
	t.Helper()

	settings, err := parseScoped([]byte(zLines(lines)))
	require.NoError(t, err)

	return settings
}
