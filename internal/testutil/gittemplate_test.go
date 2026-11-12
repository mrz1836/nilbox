package testutil_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// originAndClone builds a bare origin, origin.git, and a clone of it, clone, made with git
// clone over file://, whose main holds one commit, which it returns.
func originAndClone(b *testutil.GitBuild) string {
	origin := filepath.Join(b.Root, "origin.git")
	url := "file://" + filepath.ToSlash(origin)

	b.Git(".", "-c", "init.defaultBranch=main", "init", "-q", "--bare", origin)
	b.Git(".", "-c", "init.defaultBranch=main", "init", "-q", "work")
	b.Commit("work", "one.txt", "one\n")
	b.Git("work", "push", "-q", url, "HEAD:main")
	b.Git(".", "clone", "-q", url, "clone")

	return b.Git("clone", "rev-parse", "HEAD")
}

func TestGitTemplateBuildsOnceAndCopiesIntoEachTest(t *testing.T) {
	t.Parallel()

	var builds atomic.Int32

	tpl := testutil.NewGitTemplate(func(b *testutil.GitBuild) string {
		builds.Add(1)

		return originAndClone(b)
	})

	mine, theirs := testutil.RealTempDir(t), testutil.RealTempDir(t)
	first := tpl.Copy(t, mine)
	require.Len(t, first, 40)
	assert.Equal(t, first, tpl.Copy(t, theirs), "every copy holds the same commits")
	assert.Equal(t, int32(1), builds.Load(), "built once, copied twice")

	clone := filepath.Join(mine, "clone")
	testutil.RunGit(t, clone, "diff-files", "--quiet") // the index was refreshed after the copy
	assert.Empty(t, testutil.RunGit(t, clone, "status", "--porcelain"))
	assert.Equal(t, "file://"+filepath.ToSlash(filepath.Join(mine, "origin.git"))+"\n",
		testutil.RunGit(t, clone, "config", "--get", "remote.origin.url"), "the clone's origin is its copy's")

	testutil.WriteFile(t, filepath.Join(clone, "two.txt"), "two\n")
	testutil.RunGit(t, clone, "add", "two.txt")
	testutil.RunGit(t, clone, "commit", "-q", "-m", "two")
	testutil.RunGit(t, clone, "push", "-q", "origin", "HEAD:main")

	pushed := strings.TrimSpace(testutil.RunGit(t, clone, "rev-parse", "HEAD"))
	assert.Equal(t, pushed+"\n", testutil.RunGit(t, filepath.Join(mine, "origin.git"), "rev-parse", "main"))
	assert.Equal(t, first+"\n", testutil.RunGit(t, filepath.Join(theirs, "origin.git"), "rev-parse", "main"),
		"a push to one copy's origin leaves the other's as it was")
}

func TestGitTemplateCommitsAreTheSameInEveryBuild(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	one := testutil.NewGitTemplate(originAndClone).Copy(t, dir)
	two := testutil.NewGitTemplate(originAndClone).Copy(t, t.TempDir())

	assert.Equal(t, one, two, "every build dates its commits alike")

	when := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC).Unix()
	assert.Equal(t, fmt.Sprintf("%s <%s> %d %d one.txt\n", testutil.GitName, testutil.GitEmail, when, when),
		testutil.RunGit(t, filepath.Join(dir, "clone"), "log", "-1", "--format=%an <%ae> %at %ct %s", one),
		"committed as the test identity, at the template's date, with the file's name as the message")
}

func TestGitTemplatesBuildEachKeyOnce(t *testing.T) {
	t.Parallel()

	var builds atomic.Int32

	set := testutil.NewGitTemplates(func(b *testutil.GitBuild, name string) string {
		builds.Add(1)

		b.Git(".", "-c", "init.defaultBranch=main", "init", "-q", name)
		b.WriteFile(filepath.Join(name, "README.md"), "# "+name+"\n")

		return name
	})

	root := t.TempDir()
	assert.Equal(t, "web", set.Copy(t, "web", root))
	assert.Equal(t, "api", set.Copy(t, "api", root))
	assert.Equal(t, "web", set.Copy(t, "web", t.TempDir()))
	assert.Equal(t, int32(2), builds.Load(), "one build for each key")

	for _, name := range []string{"web", "api"} {
		got, err := os.ReadFile(filepath.Join(root, name, "README.md")) //nolint:gosec // the test's own folder
		require.NoError(t, err)
		assert.Equal(t, "# "+name+"\n", string(got))
	}
}

// copyFails reports whether copying tpl into dir fails the test asking for it.
func copyFails[V any](t *testing.T, tpl *testutil.GitTemplate[V], dir string) bool {
	t.Helper()

	return fails(t, func(tb testing.TB) { tpl.Copy(tb, dir) })
}

func TestGitTemplateFailsEveryCopyOnceAStepFailed(t *testing.T) {
	t.Parallel()

	var builds, after atomic.Int32

	tpl := testutil.NewGitTemplate(func(b *testutil.GitBuild) struct{} {
		builds.Add(1)

		b.Git(".", "no-such-command")
		b.WriteFile("notes.txt", "never written\n")

		if b.Git(".", "--version") != "" {
			after.Add(1)
		}

		return struct{}{}
	})

	assert.True(t, copyFails(t, tpl, t.TempDir()), "a test never runs without its repositories")
	assert.True(t, copyFails(t, tpl, t.TempDir()), "nor does the next")
	assert.Equal(t, int32(1), builds.Load(), "a failed build isn't tried again")
	assert.Zero(t, after.Load(), "no step runs after one failed")
}

func TestGitTemplateNeverCopiesOverAFile(t *testing.T) {
	t.Parallel()

	tpl := testutil.NewGitTemplate(func(b *testutil.GitBuild) struct{} {
		b.WriteFile("notes.txt", "the template's\n")

		return struct{}{}
	})

	dir := t.TempDir()
	testutil.WriteFile(t, filepath.Join(dir, "notes.txt"), "the test's own\n")

	assert.True(t, copyFails(t, tpl, dir))

	got, err := os.ReadFile(filepath.Join(dir, "notes.txt")) //nolint:gosec // the test's own folder
	require.NoError(t, err)
	assert.Equal(t, "the test's own\n", string(got), "the file already there is kept")
}
