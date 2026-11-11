package gitread

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// fixture is a clone of a local bare origin, isolated from the user's git config:
//
//   - origin has main at second, the annotated tag v1.0.0 on first, the lightweight tag
//     v1.1.0 on second, and the tag release-notes;
//   - the clone's main follows origin's and has third on top, unpushed, tagged v1.2.0
//     only here; one file is changed and one is untracked;
//   - the worktree feature has the branch feature, and the worktree detached is detached
//     at second, with a merge in progress.
type fixture struct {
	git    *Git
	origin string

	// clone, feature, and detached are the worktrees' folders.
	clone    string
	feature  string
	detached string

	// first, second, and third are the commits on main, oldest first.
	first  string
	second string
	third  string
}

// newFixture builds a fixture in a fresh folder, or skips the test without git.
func newFixture(t *testing.T) fixture {
	t.Helper()

	env := testutil.Git(t)
	root := t.TempDir()

	f := fixture{
		git:      New(Config{Env: func() []string { return env }}),
		origin:   filepath.Join(root, "origin.git"),
		clone:    filepath.Join(root, "clone"),
		feature:  filepath.Join(root, "feature"),
		detached: filepath.Join(root, "detached"),
	}

	testutil.RunGit(t, root, "-c", "init.defaultBranch=main", "init", "-q", "--bare", f.origin)
	testutil.InitRepo(t, f.clone)

	f.first = commitFile(t, f.clone, "one.txt", "one")
	testutil.RunGit(t, f.clone, "tag", "-a", "v1.0.0", "-m", "v1.0.0")
	f.second = commitFile(t, f.clone, "two.txt", "two")
	testutil.RunGit(t, f.clone, "tag", "v1.1.0")
	testutil.RunGit(t, f.clone, "tag", "release-notes")
	testutil.RunGit(t, f.clone, "remote", "add", "origin", f.origin)
	testutil.RunGit(t, f.clone, "push", "-q", "-u", "origin", mainBranch, "--tags")

	f.third = commitFile(t, f.clone, "three.txt", "three")
	testutil.RunGit(t, f.clone, "tag", "v1.2.0")

	testutil.RunGit(t, f.clone, "worktree", "add", "-q", "-b", "feature", f.feature)
	testutil.RunGit(t, f.clone, "worktree", "add", "-q", "--detach", f.detached, f.second)

	gitDir := strings.TrimSpace(testutil.RunGit(t, f.detached, "rev-parse", "--absolute-git-dir"))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "MERGE_HEAD"), []byte(f.third+"\n"), 0o600))

	writeFile(t, f.clone, "one.txt", "changed")
	writeFile(t, f.clone, "notes.txt", "untracked")

	return f
}

// commitFile writes name in dir and commits it, returning the commit.
func commitFile(t *testing.T, dir, name, text string) string {
	t.Helper()

	writeFile(t, dir, name, text)
	testutil.RunGit(t, dir, "add", name)
	testutil.RunGit(t, dir, "commit", "-q", "-m", name)

	return strings.TrimSpace(testutil.RunGit(t, dir, "rev-parse", "HEAD"))
}

// writeFile writes text to name in dir.
func writeFile(t *testing.T, dir, name, text string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(text+"\n"), 0o600))
}

// resolved returns path with every symbolic link resolved, as git prints a worktree's folder.
func resolved(t *testing.T, path string) string {
	t.Helper()

	out, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)

	return out
}

// TestReadsARealClone runs every helper, and so every allowlisted command, against real
// git. The subtests only read, so they share one fixture.
func TestReadsARealClone(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	g := f.git

	t.Run("the current branch", func(t *testing.T) {
		t.Parallel()

		got, err := g.CurrentBranch(t.Context(), f.clone)
		require.NoError(t, err)
		assert.Equal(t, mainBranch, got)

		got, err = g.CurrentBranch(t.Context(), f.feature)
		require.NoError(t, err)
		assert.Equal(t, "feature", got)

		got, err = g.CurrentBranch(t.Context(), f.detached)
		require.NoError(t, err)
		assert.Empty(t, got, "a detached HEAD has no branch")
	})

	t.Run("an operation in progress", func(t *testing.T) {
		t.Parallel()

		got, err := g.InProgress(t.Context(), f.clone)
		require.NoError(t, err)
		assert.Equal(t, OperationNone, got)

		got, err = g.InProgress(t.Context(), f.detached)
		require.NoError(t, err)
		assert.Equal(t, OperationMerging, got, "a linked worktree's state is its own")

		got, err = g.InProgress(t.Context(), f.feature)
		require.NoError(t, err)
		assert.Equal(t, OperationNone, got)
	})

	t.Run("uncommitted changes", func(t *testing.T) {
		t.Parallel()

		got, err := g.Changes(t.Context(), f.clone)
		require.NoError(t, err)
		assert.ElementsMatch(t, []Change{{Code: " M", Path: "one.txt"}, {Code: "??", Path: "notes.txt"}}, got)

		got, err = g.Changes(t.Context(), f.feature)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("entries, with what the index holds", func(t *testing.T) {
		t.Parallel()

		staged := strings.TrimSpace(testutil.RunGit(t, f.clone, "rev-parse", ":one.txt"))

		got, err := g.Entries(t.Context(), f.clone)
		require.NoError(t, err)
		assert.ElementsMatch(t, []Entry{{Code: ".M", Index: staged, Path: "one.txt"}, {Code: "??", Path: "notes.txt"}}, got)

		got, err = g.Entries(t.Context(), f.feature)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("a branch checked out elsewhere", func(t *testing.T) {
		t.Parallel()

		got, err := g.CheckedOutElsewhere(t.Context(), f.clone, "feature")
		require.NoError(t, err)
		assert.Equal(t, []string{resolved(t, f.feature)}, got)

		got, err = g.CheckedOutElsewhere(t.Context(), f.clone, mainBranch)
		require.NoError(t, err)
		assert.Empty(t, got, "the folder asked about doesn't count")

		got, err = g.CheckedOutElsewhere(t.Context(), f.feature, mainBranch)
		require.NoError(t, err)
		assert.Equal(t, []string{resolved(t, f.clone)}, got)
	})

	t.Run("branches here and on origin", func(t *testing.T) {
		t.Parallel()

		local, err := g.LocalBranch(t.Context(), f.clone, mainBranch)
		require.NoError(t, err)
		assert.Equal(t, f.third, local)

		tracking, err := g.TrackingBranch(t.Context(), f.clone, mainBranch)
		require.NoError(t, err)
		assert.Equal(t, f.second, tracking)

		missing, err := g.LocalBranch(t.Context(), f.clone, "nope")
		require.NoError(t, err)
		assert.Empty(t, missing)

		missing, err = g.TrackingBranch(t.Context(), f.clone, "feature")
		require.NoError(t, err)
		assert.Empty(t, missing, "feature was never on origin")
	})

	t.Run("commits the clone has", func(t *testing.T) {
		t.Parallel()

		has, err := g.HasCommit(t.Context(), f.clone, f.first)
		require.NoError(t, err)
		assert.True(t, has)

		has, err = g.HasCommit(t.Context(), f.clone, headSHA)
		require.NoError(t, err)
		assert.False(t, has)
	})

	t.Run("the upstream", func(t *testing.T) {
		t.Parallel()

		got, err := g.Upstream(t.Context(), f.clone, mainBranch)
		require.NoError(t, err)
		assert.Equal(t, Upstream{Remote: "origin", Ref: "refs/heads/main"}, got)

		got, err = g.Upstream(t.Context(), f.clone, "feature")
		require.NoError(t, err)
		assert.Equal(t, Upstream{}, got)
	})

	t.Run("unpushed commits", func(t *testing.T) {
		t.Parallel()

		got, err := g.Unpushed(t.Context(), f.clone, mainBranch)
		require.NoError(t, err)
		assert.Equal(t, 1, got)

		_, err = g.Unpushed(t.Context(), f.clone, "feature")
		require.Error(t, err, "without origin's branch there is nothing to count against")
	})

	t.Run("the commits between two", func(t *testing.T) {
		t.Parallel()

		got, err := g.CommitsBetween(t.Context(), f.clone, f.first, f.third)
		require.NoError(t, err)
		assert.Equal(t, 2, got)

		got, err = g.CommitsBetween(t.Context(), f.clone, f.third, f.first)
		require.NoError(t, err)
		assert.Zero(t, got, "the newer commit has every commit the older one has")

		_, err = g.CommitsBetween(t.Context(), f.clone, f.first, otherSHA)
		require.Error(t, err, "a commit the clone lacks can't be counted to")
	})

	t.Run("ancestors", func(t *testing.T) {
		t.Parallel()

		yes, err := g.IsAncestor(t.Context(), f.clone, f.first, f.third)
		require.NoError(t, err)
		assert.True(t, yes)

		yes, err = g.IsAncestor(t.Context(), f.clone, f.third, f.second)
		require.NoError(t, err)
		assert.False(t, yes)

		_, err = g.IsAncestor(t.Context(), f.clone, headSHA, f.third)
		require.Error(t, err, "a commit the clone lacks is not a no")
	})

	t.Run("version tags here and on origin", func(t *testing.T) {
		t.Parallel()

		local, err := g.LocalVersionTags(t.Context(), f.clone)
		require.NoError(t, err)
		assert.Equal(t, []string{"v1.0.0", "v1.1.0", "v1.2.0"}, local)

		remote, err := g.RemoteVersionTags(t.Context(), f.clone)
		require.NoError(t, err)
		assert.Equal(t, []string{"v1.0.0", "v1.1.0"}, remote)
	})

	t.Run("a version tag's commit here", func(t *testing.T) {
		t.Parallel()

		got, err := g.LocalTagCommit(t.Context(), f.clone, "v1.0.0")
		require.NoError(t, err)
		assert.Equal(t, f.first, got, "an annotated tag is peeled")

		got, err = g.LocalTagCommit(t.Context(), f.clone, "v1.1.0")
		require.NoError(t, err)
		assert.Equal(t, f.second, got)

		got, err = g.LocalTagCommit(t.Context(), f.clone, "v9.9.9")
		require.NoError(t, err)
		assert.Empty(t, got, "a tag the clone lacks is no commit")

		_, err = g.LocalTagCommit(t.Context(), f.clone, "release-notes")
		require.ErrorIs(t, err, ErrGitCommandNotAllowed, "only version tags")
	})

	t.Run("origin's branches and tags", func(t *testing.T) {
		t.Parallel()

		refs, err := g.RemoteRefs(t.Context(), f.clone)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{mainBranch: f.second}, refs.Heads)
		require.Contains(t, refs.Tags, "v1.0.0")
		assert.Equal(t, f.first, refs.Tags["v1.0.0"].Commit, "an annotated tag is peeled")
		assert.NotEqual(t, f.first, refs.Tags["v1.0.0"].Object)
		assert.Equal(t, RemoteTag{Object: f.second, Commit: f.second}, refs.Tags["v1.1.0"])
		assert.Contains(t, refs.Tags, "release-notes")
		assert.NotContains(t, refs.Tags, "v1.2.0")
	})

	t.Run("a tag's commit on origin", func(t *testing.T) {
		t.Parallel()

		got, err := g.RemoteTagCommit(t.Context(), f.clone, "v1.0.0")
		require.NoError(t, err)
		assert.Equal(t, f.first, got, "the annotated tag's commit, not the tag")

		got, err = g.RemoteTagCommit(t.Context(), f.clone, "v1.1.0")
		require.NoError(t, err)
		assert.Equal(t, f.second, got)

		got, err = g.RemoteTagCommit(t.Context(), f.clone, "v1.2.0")
		require.NoError(t, err)
		assert.Empty(t, got, "v1.2.0 is only in the clone")
	})

	t.Run("hooks", func(t *testing.T) {
		t.Parallel()

		hooks, err := g.HooksDir(t.Context(), f.clone)
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(f.clone, ".git", "hooks"), hooks)

		paths, err := g.HookPaths(t.Context(), f.clone)
		require.NoError(t, err)
		assert.Equal(t, HookPaths{
			Toplevel: resolved(t, f.clone), Hooks: filepath.Join(f.clone, ".git", "hooks"),
			CommonDir: filepath.Join(f.clone, ".git"),
		}, paths)

		linked, err := g.HookPaths(t.Context(), f.feature)
		require.NoError(t, err)
		assert.Equal(t, resolved(t, f.feature), linked.Toplevel)
		assert.Equal(t, resolved(t, paths.CommonDir), resolved(t, linked.CommonDir), "worktrees share the common git folder")
		assert.Equal(t, resolved(t, filepath.Dir(paths.Hooks)), resolved(t, filepath.Dir(linked.Hooks)))
	})

	t.Run("an unreadable folder", func(t *testing.T) {
		t.Parallel()

		_, err := g.CurrentBranch(t.Context(), filepath.Join(f.clone, "missing"))
		require.Error(t, err)
		assert.NotErrorIs(t, err, ErrGitCommandNotAllowed)
	})
}

func TestReadsSigningConfig(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	got, err := f.git.Signing(t.Context(), f.clone)
	require.NoError(t, err)
	assert.Equal(t, Signing{}, got)
	assert.False(t, got.Signed())

	testutil.RunGit(t, f.clone, "config", "tag.gpgSign", "yes")
	testutil.RunGit(t, f.clone, "config", "gpg.format", "ssh")
	testutil.RunGit(t, f.clone, "config", "user.signingkey", "~/keys/signing.pub")

	got, err = f.git.Signing(t.Context(), f.clone)
	require.NoError(t, err)
	assert.Equal(t, Signing{SignTags: true, Format: "ssh", Key: "~/keys/signing.pub"}, got, "git reads yes as true")
	assert.True(t, got.Signed())

	testutil.RunGit(t, f.clone, "config", "tag.gpgSign", "false")
	testutil.RunGit(t, f.clone, "config", "tag.forceSignAnnotated", "1")

	got, err = f.git.Signing(t.Context(), f.clone)
	require.NoError(t, err)
	assert.Equal(t, Signing{SignAnnotated: true, Format: "ssh", Key: "~/keys/signing.pub"}, got)
	assert.True(t, got.Signed())

	testutil.RunGit(t, f.clone, "config", "tag.forceSignAnnotated", "maybe")

	_, err = f.git.Signing(t.Context(), f.clone)
	require.Error(t, err, "a value git can't read as a boolean")
}

func TestReadsAnSSHCommandBeforeAskingOrigin(t *testing.T) {
	t.Parallel()

	f := newFixture(t)

	// origin is a local folder, so git runs no ssh; the clone's choice only has to be
	// read and the answer still come back.
	testutil.RunGit(t, f.clone, "config", "core.sshCommand", "ssh -o IdentityAgent=none")

	got, err := f.git.RemoteVersionTags(t.Context(), f.clone)
	require.NoError(t, err)
	assert.Equal(t, []string{"v1.0.0", "v1.1.0"}, got)
}

func TestReadsHooksMovedByHooksPath(t *testing.T) {
	t.Parallel()

	env := testutil.Git(t)
	g := New(Config{Env: func() []string { return env }})
	repo := filepath.Join(t.TempDir(), "repo")
	sub := filepath.Join(repo, "sub")

	testutil.InitRepo(t, repo)
	require.NoError(t, os.Mkdir(sub, 0o700))

	shared := t.TempDir()
	testutil.RunGit(t, repo, "config", "core.hooksPath", shared)

	got, err := g.HooksDir(t.Context(), repo)
	require.NoError(t, err)
	assert.Equal(t, shared, got, "core.hooksPath is left alone, so git names the real folder")

	testutil.RunGit(t, repo, "config", "core.hooksPath", "githooks")

	paths, err := g.HookPaths(t.Context(), sub)
	require.NoError(t, err)
	assert.Equal(t, resolved(t, repo), paths.Toplevel)
	assert.Equal(t, filepath.Join(repo, "githooks"), paths.Hooks, "a relative hooksPath is the top folder's")
	assert.Equal(t, filepath.Join(repo, ".git"), paths.CommonDir)
}

func TestReadsANewRepository(t *testing.T) {
	t.Parallel()

	env := testutil.Git(t)
	g := New(Config{Env: func() []string { return env }})
	repo := filepath.Join(t.TempDir(), "repo")

	testutil.InitRepo(t, repo)

	branch, err := g.CurrentBranch(t.Context(), repo)
	require.NoError(t, err)
	assert.Equal(t, mainBranch, branch, "a branch with no commits yet is still the current one")

	local, err := g.LocalBranch(t.Context(), repo, mainBranch)
	require.NoError(t, err)
	assert.Empty(t, local)

	tags, err := g.LocalVersionTags(t.Context(), repo)
	require.NoError(t, err)
	assert.Empty(t, tags)

	_, err = g.RemoteRefs(t.Context(), repo)
	require.Error(t, err, "there is no origin to ask")

	head, err := g.OriginHead(t.Context(), repo)
	require.NoError(t, err)
	assert.Empty(t, head, "no origin, so no origin HEAD")
}

func TestReadsOriginHead(t *testing.T) {
	t.Parallel()

	env := testutil.Git(t)
	g := New(Config{Env: func() []string { return env }})
	root := t.TempDir()
	origin, repo := filepath.Join(root, "origin.git"), filepath.Join(root, "repo")

	testutil.RunGit(t, root, "-c", "init.defaultBranch=trunk", "init", "-q", "--bare", origin)
	testutil.InitRepo(t, repo)
	commitFile(t, repo, "one.txt", "one")
	testutil.RunGit(t, repo, "switch", "-q", "-c", "trunk")
	testutil.RunGit(t, repo, "remote", "add", "origin", origin)
	testutil.RunGit(t, repo, "push", "-q", "-u", "origin", "trunk")
	testutil.RunGit(t, repo, "switch", "-q", "-c", "feature")

	head, err := g.OriginHead(t.Context(), repo)
	require.NoError(t, err)
	assert.Empty(t, head, "a pushed branch sets no origin HEAD")

	testutil.RunGit(t, repo, "remote", "set-head", "origin", "trunk")

	head, err = g.OriginHead(t.Context(), repo)
	require.NoError(t, err)
	assert.Equal(t, "trunk", head, "read from the clone's own ref, on any branch")

	cloned := filepath.Join(root, "cloned")
	testutil.RunGit(t, root, "clone", "-q", origin, cloned)

	head, err = g.OriginHead(t.Context(), cloned)
	require.NoError(t, err)
	assert.Equal(t, "trunk", head, "git clone writes it")
}
