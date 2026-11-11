package worktrees

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

const (
	gitPath  = "/opt/tools/bin/git"
	repoDir  = "/srv/projects/example-repo"
	treePath = "/srv/wt/feature"
	headSHA  = "fc1c3975011e66491785ace891c893e4fc716587"
	otherSHA = "0b1c3975011e66491785ace891c893e4fc716588"
)

var errFakeStart = errors.New("fake: cannot start git")

// exitError is a fake process exit with a status.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

func (e exitError) ExitCode() int { return e.code }

// argv joins words into an argument list: each part that holds spaces is split on
// them, and every other part (such as a path or an empty argument) is kept whole.
func argv(parts ...string) []string {
	var out []string

	for _, part := range parts {
		if strings.Contains(part, " ") {
			out = append(out, strings.Fields(part)...)
		} else {
			out = append(out, part)
		}
	}

	return out
}

// gitArgv is the argv a git run in dir gets: git, -C <dir>, the hardening settings, then
// words as argv splits them.
func gitArgv(dir string, words ...string) []string {
	out := append([]string{gitPath, "-C", dir}, hardening()...)

	return append(out, argv(words...)...)
}

func TestGitArgvUsesDashC(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	tests := []struct {
		name   string
		stdout string
		call   func(g git) error
		want   []string
	}{
		{
			name:   "list",
			stdout: string(porcelain(mainAttrs())),
			call:   func(g git) error { _, _, err := g.list(ctx, repoDir); return err },
			want:   gitArgv(repoDir, "worktree list --porcelain -z"),
		},
		{
			name: "remove",
			call: func(g git) error { _, err := g.remove(ctx, repoDir, treePath, false); return err },
			want: gitArgv(repoDir, "worktree remove", treePath),
		},
		{
			name: "force remove passes a single --force",
			call: func(g git) error { _, err := g.remove(ctx, repoDir, treePath, true); return err },
			want: gitArgv(repoDir, "worktree remove --force", treePath),
		},
		{
			name: "prune",
			call: func(g git) error { _, err := g.prune(ctx, repoDir); return err },
			want: gitArgv(repoDir, "worktree prune"),
		},
		{
			name: "status runs in the worktree",
			call: func(g git) error { _, _, err := g.dirtyCount(ctx, treePath); return err },
			want: gitArgv(treePath, "status --porcelain=v1 -z --untracked-files=all"),
		},
		{
			name: "last commit runs in the worktree",
			call: func(g git) error { _, err := g.lastCommit(ctx, treePath); return err },
			want: gitArgv(treePath, "log -1 --no-show-signature --format=%ct"),
		},
		{
			name:   "commit times run in the clone",
			stdout: headSHA + " 1700000000\n",
			call: func(g git) error {
				_, err := g.commitTimes(ctx, repoDir, []string{headSHA, otherSHA})

				return err
			},
			want: append(gitArgv(repoDir, "log --no-walk=unsorted --no-show-signature"), "--format=%H %ct", headSHA, otherSHA),
		},
		{
			name:   "unpushed count runs in the worktree",
			stdout: "0\n",
			call:   func(g git) error { _, _, err := g.unpushed(ctx, treePath); return err },
			want:   gitArgv(treePath, "rev-list --count HEAD --not --remotes"),
		},
		{
			name: "unlock",
			call: func(g git) error { _, err := g.unlock(ctx, repoDir, treePath); return err },
			want: gitArgv(repoDir, "worktree unlock", treePath),
		},
		{
			name: "delete branch compares and deletes",
			call: func(g git) error { _, err := g.deleteBranch(ctx, repoDir, "worktree-pr-7", headSHA); return err },
			want: gitArgv(repoDir, "update-ref -d refs/heads/worktree-pr-7", headSHA),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			run := &testutil.Runner{Stdout: tt.stdout}
			if run.Stdout == "" {
				run.Stdout = "1700000000\n"
			}

			require.NoError(t, tt.call(git{run: run, bin: gitPath}))
			assert.Equal(t, [][]string{tt.want}, run.Calls())
		})
	}
}

func TestGitRejectsCommandsOutsideAllowlist(t *testing.T) {
	t.Parallel()

	refused := [][]string{
		argv("branch -D worktree-pr-7"),
		argv("worktree remove --force --force", treePath),
		argv("worktree remove -f", treePath),
		argv("worktree remove --force"),
		argv("worktree remove"),
		argv("worktree remove", ""),
		argv("worktree remove", treePath, "extra"),
		argv("worktree add", treePath),
		argv("worktree unlock"),
		argv("worktree unlock", "--porcelain"),
		argv("worktree unlock", treePath, "extra"),
		argv("worktree lock", treePath),
		argv("update-ref -d refs/heads/-x", headSHA),
		argv("update-ref -d refs/heads/a..b", headSHA),
		argv("update-ref -d refs/heads/", headSHA),
		argv("update-ref -d refs/heads/feature/", headSHA),
		argv("update-ref -d refs/heads/worktree-..", headSHA),
		argv("update-ref -d refs/heads/worktree-pr-7.lock", headSHA),
		argv("update-ref -d refs/tags/worktree-pr-7", headSHA),
		argv("update-ref -d refs/heads/worktree-pr-7", "HEAD"),
		argv("update-ref -d refs/heads/worktree-pr-7", strings.Repeat("0", 40)),
		argv("update-ref -d refs/heads/worktree-pr-7", strings.Repeat("0", 64)),
		argv("update-ref -d refs/heads/worktree-pr-7"),
		argv("update-ref refs/heads/worktree-pr-7", headSHA),
		argv("rev-list --count HEAD"),
		argv("rev-list --count main --not --remotes"),
		argv("rev-list --count", headSHA, "--not --all"),
		argv("merge-base --is-ancestor main", headSHA),
		argv("merge-base --is-ancestor", headSHA),
		argv("merge-base", headSHA, headSHA),
		argv("for-each-ref refs/heads/"),
		argv("for-each-ref --format=%(refname) refs/"),
		{"log", "--no-walk=unsorted", "--no-show-signature", "--format=%H %ct"},
		{"log", "--no-walk=unsorted", "--no-show-signature", "--format=%H %ct", "HEAD"},
		{"log", "--no-walk=unsorted", "--no-show-signature", "--format=%H %ct", headSHA, "--all"},
		{"log", "--no-walk=unsorted", "--no-show-signature", "--format=%H %ct", strings.Repeat("0", 40)},
		{"log", "--no-walk=unsorted", "--format=%H %an", headSHA},
		{"log", "--no-walk", "--format=%H %ct", headSHA},
		argv("worktree list"),
		argv("checkout main"),
		argv("status"),
		{},
	}

	for _, args := range refused {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			t.Parallel()

			run := &testutil.Runner{}
			_, _, err := git{run: run, bin: gitBin}.do(t.Context(), repoDir, args...)
			require.ErrorIs(t, err, ErrGitCommandNotAllowed)
			assert.Empty(t, run.Calls(), "a refused command never runs")
		})
	}
}

func TestGitAllowsDeletingAnyWellFormedBranchAtACommit(t *testing.T) {
	t.Parallel()

	// Which branches may go is the callers' call (see deletable); the allowlist only
	// keeps the command to a real branch and a real commit, which git compares first.
	for _, branch := range []string{"main", "feature/worktree-pr-7", "worktree-", "worktree-pr/7", "fix-it"} {
		run := &testutil.Runner{}
		_, _, err := git{run: run, bin: gitBin}.do(t.Context(), repoDir, "update-ref", "-d", headsPrefix+branch, headSHA)
		require.NoError(t, err, branch)
		assert.Len(t, run.Calls(), 1)
	}

	for _, args := range [][]string{
		branchListArgs(), remoteHeadsArgs(), branchUnpushedArgs(headSHA), ancestorArgs(headSHA, otherSHA),
	} {
		_, _, err := git{run: &testutil.Runner{}, bin: gitBin}.do(t.Context(), repoDir, args...)
		require.NoError(t, err, args)
	}
}

func TestGitBranchOutputParsing(t *testing.T) {
	t.Parallel()

	run := &testutil.Runner{Stdout: "main\x00" + headSHA + "\x00/srv/clone\n" +
		"feature/x\x00" + otherSHA + "\x00\n" +
		"broken\x00not-a-commit\x00\n" +
		"\x00" + headSHA + "\x00\n" +
		"too\x00few\n"}

	got, _, err := git{run: run, bin: gitBin}.branches(t.Context(), repoDir)
	require.NoError(t, err)
	assert.Equal(t, []localBranch{
		{name: "main", head: headSHA, worktree: "/srv/clone"},
		{name: "feature/x", head: otherSHA},
	}, got, "lines that are not a branch and a commit are left out")

	heads := git{run: &testutil.Runner{Stdout: "main\n\nmain\ndevelop\n"}, bin: gitBin}.remoteHeads(t.Context(), repoDir)
	assert.Equal(t, []string{"main", "develop"}, heads)
	assert.Empty(t, git{run: &testutil.Runner{Err: errFakeStart}, bin: gitBin}.remoteHeads(t.Context(), repoDir))

	n, err := git{run: &testutil.Runner{Stdout: "3\n"}, bin: gitBin}.unpushedOf(t.Context(), repoDir, headSHA)
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	_, err = git{run: &testutil.Runner{Stdout: "lots\n"}, bin: gitBin}.unpushedOf(t.Context(), repoDir, headSHA)
	require.ErrorIs(t, err, errParseCount)

	assert.True(t, git{run: &testutil.Runner{}, bin: gitBin}.isAncestor(t.Context(), repoDir, headSHA, otherSHA))
	assert.False(t, git{run: &testutil.Runner{Err: errFakeStart}, bin: gitBin}.isAncestor(t.Context(), repoDir, headSHA, otherSHA))
}

func TestGitOutputParsing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	t.Run("list parses porcelain", func(t *testing.T) {
		t.Parallel()

		run := &testutil.Runner{Stdout: string(porcelain(mainAttrs()))}
		entries, _, err := git{run: run, bin: gitBin}.list(ctx, repoDir)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.True(t, entries[0].Main)
	})

	t.Run("list returns git's stderr on failure", func(t *testing.T) {
		t.Parallel()

		run := &testutil.Runner{Stderr: "fatal: not a git repository", Err: exitError{code: 128}}
		_, stderr, err := git{run: run, bin: gitBin}.list(ctx, repoDir)
		require.Error(t, err)
		assert.Equal(t, "fatal: not a git repository", string(stderr))
	})

	t.Run("dirty count", func(t *testing.T) {
		t.Parallel()

		run := &testutil.Runner{Stdout: " M f\x00?? g\x00"}
		count, _, err := git{run: run, bin: gitBin}.dirtyCount(ctx, treePath)
		require.NoError(t, err)
		assert.Equal(t, 2, count)
	})

	t.Run("dirty count failure", func(t *testing.T) {
		t.Parallel()

		run := &testutil.Runner{Err: errFakeStart}
		_, _, err := git{run: run, bin: gitBin}.dirtyCount(ctx, treePath)
		require.ErrorIs(t, err, errFakeStart)
	})

	t.Run("last commit", func(t *testing.T) {
		t.Parallel()

		run := &testutil.Runner{Stdout: "1700000000\n"}
		unix, err := git{run: run, bin: gitBin}.lastCommit(ctx, treePath)
		require.NoError(t, err)
		assert.Equal(t, int64(1700000000), unix)
	})

	t.Run("last commit with unexpected output", func(t *testing.T) {
		t.Parallel()

		run := &testutil.Runner{Stdout: "yesterday\n"}
		_, err := git{run: run, bin: gitBin}.lastCommit(ctx, treePath)
		require.ErrorIs(t, err, errParseCommitTime)
	})

	t.Run("last commit failure", func(t *testing.T) {
		t.Parallel()

		run := &testutil.Runner{Err: exitError{code: 128}}
		_, err := git{run: run, bin: gitBin}.lastCommit(ctx, treePath)
		require.Error(t, err)
	})

	t.Run("commit times", func(t *testing.T) {
		t.Parallel()

		run := &testutil.Runner{Stdout: headSHA + " 1700000000\n" + otherSHA + " 1700000100\n"}
		times, err := git{run: run, bin: gitBin}.commitTimes(ctx, repoDir, []string{headSHA, otherSHA})
		require.NoError(t, err)
		assert.Equal(t, map[string]int64{headSHA: 1700000000, otherSHA: 1700000100}, times)
	})

	t.Run("commit times with unexpected output", func(t *testing.T) {
		t.Parallel()

		for _, out := range []string{headSHA + "\n", headSHA + " yesterday\n"} {
			run := &testutil.Runner{Stdout: out}
			_, err := git{run: run, bin: gitBin}.commitTimes(ctx, repoDir, []string{headSHA})
			require.ErrorIs(t, err, errParseCommitTime, out)
		}
	})

	t.Run("commit times failure", func(t *testing.T) {
		t.Parallel()

		run := &testutil.Runner{Err: exitError{code: 128}}
		_, err := git{run: run, bin: gitBin}.commitTimes(ctx, repoDir, []string{headSHA})
		require.Error(t, err)
	})

	t.Run("unpushed count", func(t *testing.T) {
		t.Parallel()

		run := &testutil.Runner{Stdout: "3\n"}
		count, _, err := git{run: run, bin: gitBin}.unpushed(ctx, treePath)
		require.NoError(t, err)
		assert.Equal(t, 3, count)
	})

	t.Run("unpushed count with unexpected output", func(t *testing.T) {
		t.Parallel()

		for _, out := range []string{"many\n", "-1\n"} {
			run := &testutil.Runner{Stdout: out}
			_, _, err := git{run: run, bin: gitBin}.unpushed(ctx, treePath)
			require.ErrorIs(t, err, errParseCount, out)
		}
	})

	t.Run("unpushed count failure returns stderr", func(t *testing.T) {
		t.Parallel()

		run := &testutil.Runner{Stderr: "fatal: bad revision 'HEAD'", Err: exitError{code: 128}}
		_, stderr, err := git{run: run, bin: gitBin}.unpushed(ctx, treePath)
		require.Error(t, err)
		assert.Equal(t, "fatal: bad revision 'HEAD'", string(stderr))
	})
}

func TestOwnBranchRefs(t *testing.T) {
	t.Parallel()

	assert.True(t, isOwnBranchRef("refs/heads/worktree-pr-184"))
	assert.True(t, isOwnBranchRef("refs/heads/worktree-issue-7"))
	assert.True(t, isOwnBranchRef("refs/heads/worktree-bright-running-fox"))

	for _, ref := range []string{
		"refs/heads/main", "refs/heads/worktree-", "refs/heads/worktree-a/b", "refs/heads/worktree-a b",
		"refs/heads/worktree-.hidden", "refs/heads/worktree-a..b", "refs/heads/worktree-x.lock", "worktree-pr-7",
		`refs/heads/worktree-a\b`, "refs/heads/worktree-a:b",
	} {
		assert.False(t, isOwnBranchRef(ref), ref)
	}
}

func TestCountStatusEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
		want int
	}{
		{name: "clean", out: "", want: 0},
		{name: "modified and untracked", out: " M f\x00?? g\x00", want: 2},
		{name: "rename counts its source once", out: "R  new\x00a\x00 M f\x00", want: 2},
		{name: "copy in the worktree column", out: " C copy\x00orig\x00", want: 1},
		{name: "staged, deleted, and conflicted", out: "A  a\x00 D b\x00UU c\x00", want: 3},
		{name: "untracked file with a space", out: "?? with space.txt\x00", want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, countStatusEntries([]byte(tt.out)))
		})
	}
}

func TestGitMessages(t *testing.T) {
	t.Parallel()

	assert.Equal(t, reasonDirty,
		refusalReason([]byte("fatal: '/srv/wt/a' contains modified or untracked files, use --force to delete it")))
	assert.Equal(t, reasonLocked,
		refusalReason([]byte("fatal: cannot remove a locked working tree, lock reason: x\nuse 'remove -f -f' to override")))
	assert.Equal(t, "something else", refusalReason([]byte("fatal: something else")))
	assert.Equal(t, "git exited with an error", refusalReason(nil))

	cause := exitError{code: 128}
	require.ErrorIs(t, gitError(cause, nil), cause)
	require.ErrorIs(t, gitError(cause, []byte("fatal: broken")), errGit)
	assert.Equal(t, "git: broken", gitError(cause, []byte("fatal: broken")).Error())

	ctx, cancel := context.WithCancel(t.Context())
	assert.True(t, gitRefused(ctx, fmt.Errorf("run git: %w", cause)))
	assert.False(t, gitRefused(ctx, errFakeStart))
	assert.False(t, gitRefused(ctx, exitError{code: 0}))

	cancel()
	assert.False(t, gitRefused(ctx, cause), "a canceled run is not git refusing")
}
