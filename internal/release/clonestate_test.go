package release

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// cloneRig is a clone of a local bare origin, isolated from the user's git config, and a
// GitChecker over it. origin's master has first and second, tagged v1.0.0 on first; the
// clone's master is at second.
type cloneRig struct {
	checker GitChecker
	origin  string
	clone   string
	other   string
	first   string
	second  string
}

// newCloneRig builds a cloneRig in a fresh folder, a copy of sdkFixtures' fixture with
// the clone at clone, or skips the test without git.
func newCloneRig(t *testing.T) cloneRig {
	t.Helper()

	env := testutil.Git(t)
	root := t.TempDir()

	r := cloneRig{
		checker: GitChecker{Git: gitread.New(gitread.Config{Env: func() []string { return env }})},
		origin:  filepath.Join(root, "origin.git"),
		clone:   filepath.Join(root, "clone"),
		other:   filepath.Join(root, "other"),
	}

	c := sdkFixtures.Copy(t, sdkFixture{clone: "clone", base: "v1.0.0"}, root)
	r.first, r.second = c.first, c.second

	return r
}

func TestCloneStateOfACleanClone(t *testing.T) {
	t.Parallel()

	r := newCloneRig(t)

	got := r.checker.CloneState(t.Context(), r.clone, "master", r.second, []Tag{{Name: "v1.0.0", Commit: r.first}})
	assert.Equal(t, CloneState{Branch: "master", LocalOnly: []string{}}, got, "on master, clean, in step with GitHub")
}

func TestCloneStateFindsWhatMatters(t *testing.T) {
	t.Parallel()

	r := newCloneRig(t)

	testutil.Commit(t, r.clone, "three.txt", "three.txt"+"\n")
	testutil.RunGit(t, r.clone, "tag", "v1.1.0")
	testutil.RunGit(t, r.clone, "tag", "v0.9.0") // older than GitHub's newest: not judged
	testutil.RunGit(t, r.clone, "checkout", "-q", "-b", "feature-x")
	require.NoError(t, os.WriteFile(filepath.Join(r.clone, "one.txt"), []byte("changed\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(r.clone, "notes.txt"), []byte("new\n"), 0o600))

	got := r.checker.CloneState(t.Context(), r.clone, "master", r.second, []Tag{{Name: "v1.0.0", Commit: r.first}})
	assert.Equal(t, "feature-x", got.Branch)
	assert.False(t, got.Detached)
	assert.Equal(t, 2, got.Dirty, "a changed file and an untracked one")
	assert.Equal(t, 1, got.NotPushed, "third isn't on origin")
	assert.False(t, got.Behind, "GitHub's newest commit is an ancestor of the clone's")
	assert.Equal(t, []string{"v1.1.0"}, got.LocalOnly, "only a version above GitHub's newest counts")
	assert.False(t, got.Unreadable)
}

func TestCloneStateSeesItIsBehind(t *testing.T) {
	t.Parallel()

	r := newCloneRig(t)

	testutil.RunGit(t, filepath.Dir(r.clone), "clone", "-q", r.origin, r.other)
	ahead := testutil.Commit(t, r.other, "elsewhere.txt", "elsewhere.txt"+"\n")
	testutil.RunGit(t, r.other, "push", "-q", "origin", "master")

	got := r.checker.CloneState(t.Context(), r.clone, "master", ahead, nil)
	assert.True(t, got.Behind, "GitHub has a commit the clone hasn't fetched")

	testutil.RunGit(t, r.clone, "fetch", "-q", "origin")

	got = r.checker.CloneState(t.Context(), r.clone, "master", ahead, nil)
	assert.True(t, got.Behind, "fetched but not pulled: the clone's head is an ancestor of GitHub's")

	diverged := testutil.Commit(t, r.clone, "mine.txt", "mine.txt"+"\n")
	got = r.checker.CloneState(t.Context(), r.clone, "master", ahead, nil)
	assert.False(t, got.Behind, "diverged is not behind")
	assert.Equal(t, 1, got.NotPushed)
	assert.NotEmpty(t, diverged)
}

func TestCloneStateOfADetachedCloneWithoutTheBranch(t *testing.T) {
	t.Parallel()

	r := newCloneRig(t)

	testutil.RunGit(t, r.clone, "checkout", "-q", "--detach", r.first)

	got := r.checker.CloneState(t.Context(), r.clone, "main", r.second, nil)
	assert.True(t, got.Detached)
	assert.Empty(t, got.Branch)
	assert.Zero(t, got.NotPushed, "the clone has no main: nothing to compare")
	assert.False(t, got.Behind)

	got = r.checker.CloneState(t.Context(), r.clone, "", r.second, nil)
	assert.False(t, got.Unreadable, "no branch to compare is fine")
}

func TestCloneStateOfAFolderThatIsntAClone(t *testing.T) {
	t.Parallel()

	r := newCloneRig(t)

	got := r.checker.CloneState(t.Context(), t.TempDir(), "master", r.second, nil)
	assert.True(t, got.Unreadable)
	assert.Equal(t, []string{}, got.LocalOnly)
}

func TestLocalOnly(t *testing.T) {
	t.Parallel()

	gh := []Tag{{Name: "v1.4.2"}, {Name: "v1.4.1"}, {Name: "release-7"}}

	assert.Equal(t, []string{"v1.4.3", "v2.0.0-rc.1"},
		localOnly([]string{"v1.4.1", "v1.4.2", "v1.4.3", "v1.3.9", "v2.0.0-rc.1", "notes"}, gh))
	assert.Equal(t, []string{"v0.0.1"}, localOnly([]string{"v0.0.1"}, nil), "with nothing on GitHub, every version is only here")
	assert.Equal(t, []string{}, localOnly(nil, gh))
}

// errGitFailed is a git command that failed.
var errGitFailed = errors.New("git failed")

// failingRunner runs git for real, except a command holding failOn, which fails.
type failingRunner struct {
	real   execpath.Runner
	failOn string
}

func (f failingRunner) Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	if slices.ContainsFunc(args, func(a string) bool { return strings.Contains(a, f.failOn) }) {
		return nil, []byte("fatal: " + f.failOn), errGitFailed
	}

	return f.real.Run(ctx, name, args...) //nolint:wrapcheck // the real runner's own error
}

func TestCloneStateMarksWhatGitCantRead(t *testing.T) {
	t.Parallel()

	r := newCloneRig(t)
	env := testutil.Git(t)

	testutil.RunGit(t, filepath.Dir(r.clone), "clone", "-q", r.origin, r.other)
	ahead := testutil.Commit(t, r.other, "elsewhere.txt", "elsewhere.txt"+"\n")
	testutil.RunGit(t, r.other, "push", "-q", "origin", "master")
	testutil.RunGit(t, r.clone, "fetch", "-q", "origin")

	for _, step := range []string{"symbolic-ref", "status", "refs/heads/", "refs/remotes/", "rev-list", "^{commit}", "merge-base", "tag"} {
		t.Run(step, func(t *testing.T) {
			t.Parallel()

			checker := GitChecker{Git: gitread.New(gitread.Config{
				Env: func() []string { return env },
				Runner: func(e []string) execpath.Runner {
					return failingRunner{real: execpath.Exec{Env: e}, failOn: step}
				},
			})}

			got := checker.CloneState(t.Context(), r.clone, "master", ahead, nil)
			assert.True(t, got.Unreadable, "%s failed", step)
			assert.NotNil(t, got.LocalOnly)
		})
	}
}
