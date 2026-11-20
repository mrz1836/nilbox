package worktrees

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/execpath"
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
			want: gitArgv(treePath, "log -1 --no-show-signature --no-mailmap --format=%ct"),
		},
		{
			name:   "commit times run in the clone",
			stdout: headSHA + " 1700000000\n",
			call: func(g git) error {
				_, err := g.commitTimes(ctx, repoDir, []string{headSHA, otherSHA})

				return err
			},
			want: append(gitArgv(repoDir, "log --no-walk=unsorted --no-show-signature --no-mailmap"), "--format=%H %ct", headSHA, otherSHA),
		},
		{
			name:   "unpushed count runs in the worktree",
			stdout: "0\n",
			call:   func(g git) error { _, _, err := g.unpushed(ctx, treePath, ""); return err },
			want:   gitArgv(treePath, "rev-list --count HEAD --not --remotes"),
		},
		{
			name:   "unpushed count past the commit fetched from GitHub",
			stdout: "0\n",
			call:   func(g git) error { _, _, err := g.unpushed(ctx, treePath, otherSHA); return err },
			want:   append(gitArgv(treePath, "rev-list --count --ignore-missing HEAD --not --remotes"), otherSHA),
		},
		{
			name:   "unpushed count ignores a fetched head that is no commit",
			stdout: "0\n",
			call:   func(g git) error { _, _, err := g.unpushed(ctx, treePath, "main"); return err },
			want:   gitArgv(treePath, "rev-list --count HEAD --not --remotes"),
		},
		{
			name:   "a branch's unpushed count past the commit fetched from GitHub",
			stdout: "0\n",
			call:   func(g git) error { _, err := g.unpushedOf(ctx, repoDir, headSHA, otherSHA); return err },
			want:   append(gitArgv(repoDir, "rev-list --count --ignore-missing", headSHA, "--not --remotes"), otherSHA),
		},
		{
			name:   "the commits not on GitHub are listed in the worktree",
			stdout: headSHA + " 1700000000 Fix the cache\n",
			call:   func(g git) error { _, _, err := g.unpushedCommits(ctx, treePath, ""); return err },
			want: append(gitArgv(treePath, "log --no-show-signature --no-mailmap --max-count=200"), "--format=%H %ct %s",
				"HEAD", "--not", "--remotes"),
		},
		{
			name:   "the commits not on GitHub past the commit fetched from GitHub",
			stdout: headSHA + " 1700000000 Fix the cache\n",
			call:   func(g git) error { _, _, err := g.unpushedCommits(ctx, treePath, otherSHA); return err },
			want: append(gitArgv(treePath, "log --no-show-signature --no-mailmap --max-count=200"), "--format=%H %ct %s",
				"--ignore-missing", "HEAD", "--not", "--remotes", otherSHA),
		},
		{
			name:   "the commits not on GitHub ignore a fetched head that is no commit",
			stdout: headSHA + " 1700000000 Fix the cache\n",
			call:   func(g git) error { _, _, err := g.unpushedCommits(ctx, treePath, "main"); return err },
			want: append(gitArgv(treePath, "log --no-show-signature --no-mailmap --max-count=200"), "--format=%H %ct %s",
				"HEAD", "--not", "--remotes"),
		},
		{
			name:   "the refs are read in the clone",
			stdout: "refs/heads/main\x00" + headSHA + "\x00\x00\x00" + repoDir + "\n",
			call:   func(g git) error { _, err := g.readRefs(ctx, repoDir); return err },
			want: gitArgv(repoDir, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(upstream)%00%(symref)%00%(worktreepath)",
				"refs/heads/ refs/remotes/"),
		},
		{
			name: "unlock",
			call: func(g git) error { _, err := g.unlock(ctx, repoDir, treePath); return err },
			want: gitArgv(repoDir, "worktree unlock", treePath),
		},
		{
			name:   "the commits not on GitHub of several HEADs are listed in the clone",
			stdout: headSHA + "\x00" + otherSHA + "\x001700000000\x00octo-user@example.com\x00Fix the cache\n",
			call: func(g git) error {
				_, err := g.copyUnpushed(ctx, repoDir, []string{headSHA, otherSHA}, "")

				return err
			},
			want: append(gitArgv(repoDir, "log --no-show-signature --no-mailmap --max-count=10000"),
				"--format=%H%x00%P%x00%at%x00%ae%x00%s", headSHA, otherSHA, "--not", "--remotes"),
		},
		{
			name:   "and past the commit they were made at from GitHub",
			stdout: headSHA + "\x00\x001700000000\x00octo-user@example.com\x00Fix the cache\n",
			call: func(g git) error {
				_, err := g.copyUnpushed(ctx, repoDir, []string{headSHA}, otherSHA)

				return err
			},
			want: append(gitArgv(repoDir, "log --no-show-signature --no-mailmap --max-count=10000"),
				"--format=%H%x00%P%x00%at%x00%ae%x00%s", "--ignore-missing", headSHA, "--not", "--remotes", otherSHA),
		},
		{
			name:   "but not past a fetched head that is no commit",
			stdout: headSHA + "\x00\x001700000000\x00octo-user@example.com\x00Fix the cache\n",
			call: func(g git) error {
				_, err := g.copyUnpushed(ctx, repoDir, []string{headSHA}, "main")

				return err
			},
			want: append(gitArgv(repoDir, "log --no-show-signature --no-mailmap --max-count=10000"),
				"--format=%H%x00%P%x00%at%x00%ae%x00%s", headSHA, "--not", "--remotes"),
		},
		{
			name:   "the pushed commits that may be copies are listed in the clone",
			stdout: otherSHA + "\x00origin/feature-x\x001700000000\x00octo-user@example.com\x00Fix the cache\n",
			call: func(g git) error {
				_, err := g.copyCandidates(ctx, repoDir, 1699913600, "refs/remotes/origin/main")

				return err
			},
			want: append(gitArgv(repoDir, "log --no-show-signature --no-mailmap --no-merges --source --max-count=20000"),
				"--format=%H%x00%S%x00%at%x00%ae%x00%s", "--since=@1699913600", "--remotes", "--not", "refs/remotes/origin/main"),
		},
		{
			name:   "every one of them when the default branch isn't known",
			stdout: otherSHA + "\x00origin/feature-x\x001700000000\x00octo-user@example.com\x00Fix the cache\n",
			call:   func(g git) error { _, err := g.copyCandidates(ctx, repoDir, 0, ""); return err },
			want: append(gitArgv(repoDir, "log --no-show-signature --no-mailmap --no-merges --source --max-count=20000"),
				"--format=%H%x00%S%x00%at%x00%ae%x00%s", "--since=@0", "--remotes"),
		},
		{
			name:   "the changes are compared in the clone",
			stdout: "<" + otherSHA + "\n>" + headSHA + "\n",
			call:   func(g git) error { _, err := g.copySides(ctx, repoDir, otherSHA, headSHA); return err },
			want:   gitArgv(repoDir, "rev-list --left-right --no-merges", otherSHA+"..."+headSHA),
		},
		{
			name:   "the changes themselves are read in the clone",
			stdout: "\x00" + headSHA + "\n\ndiff --git a/f b/f\n",
			call:   func(g git) error { _, err := g.copyPatches(ctx, repoDir, []string{headSHA, otherSHA}); return err },
			want: append(gitArgv(repoDir, "log --no-show-signature --no-mailmap --no-walk=unsorted --root -p --no-ext-diff",
				"--no-textconv --no-color --no-renames --full-index --unified=3"), "--format=%x00%H", headSHA, otherSHA),
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
		argv("rev-list --count --ignore-missing main --not --remotes", headSHA),
		argv("rev-list --count --ignore-missing HEAD --not --remotes main"),
		argv("rev-list --count --ignore-missing HEAD --not --all", headSHA),
		argv("rev-list --count --ignore-missing HEAD --remotes --not", headSHA),
		argv("rev-list --count --ignore-missing HEAD --not --remotes", headSHA, otherSHA),
		argv("rev-list --count --all HEAD --not --remotes", headSHA),
		argv("merge-base --is-ancestor main", headSHA),
		argv("merge-base --is-ancestor", headSHA),
		argv("merge-base", headSHA, headSHA),
		argv("for-each-ref refs/heads/"),
		argv("for-each-ref --format=%(refname) refs/"),
		argv("for-each-ref --format=%(refname)%00%(objectname)%00%(upstream)%00%(symref)%00%(worktreepath) refs/"),
		argv("for-each-ref --format=%(refname)%00%(objectname)%00%(upstream)%00%(symref)%00%(worktreepath) refs/heads/"),
		append(refsArgs(), "refs/tags/"),
		append(refsArgs()[:2], "--count=1", "refs/heads/", "refs/remotes/"),
		{"log", "--no-walk=unsorted", "--no-show-signature", "--no-mailmap", "--format=%H %ct"},
		{"log", "--no-walk=unsorted", "--no-show-signature", "--no-mailmap", "--format=%H %ct", "HEAD"},
		{"log", "--no-walk=unsorted", "--no-show-signature", "--no-mailmap", "--format=%H %ct", headSHA, "--all"},
		{"log", "--no-walk=unsorted", "--no-show-signature", "--no-mailmap", "--format=%H %ct", strings.Repeat("0", 40)},
		{"log", "--no-walk=unsorted", "--format=%H %an", headSHA},
		{"log", "--no-walk", "--format=%H %ct", headSHA},
		append(unpushedLogCmd(), "main", "--not", "--remotes"),
		append(unpushedLogCmd(), headSHA, "--not", "--remotes"),
		append(unpushedLogCmd(), "HEAD", "--not", "--all"),
		append(unpushedLogCmd(), "HEAD", "--remotes", "--not"),
		append(unpushedLogCmd(), "HEAD", "--not", "--remotes", otherSHA),
		append(unpushedLogCmd(), "HEAD", "--not", "--remotes", "--not", otherSHA),
		append(unpushedLogCmd(), "HEAD", "--not", "--remotes", "-p"),
		append(unpushedLogCmd(), "--ignore-missing", "HEAD", "--not", "--remotes", "main"),
		append(unpushedLogCmd(), "--ignore-missing", "HEAD", "--not", "--remotes", strings.Repeat("0", 40)),
		append(unpushedLogCmd(), "--ignore-missing", "main", "--not", "--remotes", otherSHA),
		append(unpushedLogCmd(), "--ignore-missing", "HEAD", "--not", "--not", otherSHA),
		append(unpushedLogCmd(), "--ignore-missing", "HEAD", "--not", "--remotes", otherSHA, headSHA),
		append(argv("log --no-show-signature --no-mailmap --max-count=200"), "--format=%H %ct %s %b", "HEAD", "--not", "--remotes"),
		append(argv("log --no-show-signature --no-mailmap --max-count=2000"), "--format=%H %ct %s", "HEAD", "--not", "--remotes"),
		append(argv("log --no-mailmap --max-count=200"), "--format=%H %ct %s", "HEAD", "--not", "--remotes", "--all"),
		append(argv("log --max-count=200"), "--format=%H %ct %s", "HEAD", "--not", "--remotes"),
		append(copyUnpushedCmd(), "--not", "--remotes"),
		append(copyUnpushedCmd(), "HEAD", "--not", "--remotes"),
		append(copyUnpushedCmd(), "main", "--not", "--remotes"),
		append(copyUnpushedCmd(), headSHA, "--not", "--all"),
		append(copyUnpushedCmd(), headSHA, "--remotes", "--not"),
		append(copyUnpushedCmd(), headSHA, "--not", "--remotes", otherSHA),
		append(copyUnpushedCmd(), headSHA, "--not", "--remotes", "-p"),
		append(copyUnpushedCmd(), "--ignore-missing", headSHA, "--not", "--remotes"),
		append(copyUnpushedCmd(), "--ignore-missing", headSHA, "--not", "--remotes", "main"),
		append(copyUnpushedCmd(), "--ignore-missing", headSHA, "--not", "--remotes", otherSHA, headSHA),
		append(copyUnpushedCmd(), "--ignore-missing", "--not", "--remotes", otherSHA),
		append(copyUnpushedCmd(), "--all", headSHA, "--not", "--remotes"),
		append(argv("log --no-show-signature --no-mailmap --max-count=99999"), "--format=%H%x00%P%x00%at%x00%ae%x00%s",
			headSHA, "--not", "--remotes"),
		append(argv("log --no-mailmap --max-count=10000"), "--format=%H%x00%P%x00%at%x00%ae%x00%s",
			headSHA, "--not", "--remotes"),
		copyCandidatesCmd(),
		append(copyCandidatesCmd(), "--remotes"),
		append(copyCandidatesCmd(), "--since=@-1", "--remotes"),
		append(copyCandidatesCmd(), "--since=@01700000000", "--remotes"),
		append(copyCandidatesCmd(), "--since=@+1700000000", "--remotes"),
		append(copyCandidatesCmd(), "--since=1700000000", "--remotes"),
		append(copyCandidatesCmd(), "--since=yesterday", "--remotes"),
		append(copyCandidatesCmd(), "--since=@1700000000"),
		append(copyCandidatesCmd(), "--since=@1700000000", "--all"),
		append(copyCandidatesCmd(), "--since=@1700000000", "--remotes", "--not"),
		append(copyCandidatesCmd(), "--since=@1700000000", "--remotes", "--not", "refs/heads/main"),
		append(copyCandidatesCmd(), "--since=@1700000000", "--remotes", "--not", "HEAD"),
		append(copyCandidatesCmd(), "--since=@1700000000", "--remotes", "--not", "refs/remotes/origin/HEAD/x/.."),
		append(copyCandidatesCmd(), "--since=@1700000000", "--remotes", "--not", "refs/remotes/origin/main", "-p"),
		append(copyCandidatesCmd(), "--since=@1700000000", "--remotes", "--all", "refs/remotes/origin/main"),
		append(argv("log --no-show-signature --no-mailmap --source --max-count=20000"), "--format=%H%x00%S%x00%at%x00%ae%x00%s",
			"--since=@1700000000", "--remotes"),
		argv("rev-list --left-right --no-merges", "HEAD..."+headSHA),
		argv("rev-list --left-right --no-merges", otherSHA+".."+headSHA),
		argv("rev-list --left-right --no-merges", otherSHA+"..."+headSHA+"..."+headSHA),
		argv("rev-list --left-only --no-merges", otherSHA+"..."+headSHA),
		argv("rev-list --left-right", otherSHA+"..."+headSHA),
		argv("rev-list --left-right --no-merges", otherSHA+"..."+headSHA, "--all"),
		argv("rev-list --left-right --cherry-pick --no-merges", otherSHA+"..."+headSHA),
		copyPatchesCmd(),
		append(copyPatchesCmd(), "HEAD"),
		append(copyPatchesCmd(), headSHA, "--all"),
		append(copyPatchesCmd(), headSHA, "--ext-diff"),
		append(copyPatchesCmd(), "--textconv", headSHA),
		append(copyPatchesCmd()[1:], headSHA),
		append(argv("log --no-show-signature --no-mailmap --no-walk=unsorted --root -p --no-textconv --no-color"),
			"--no-renames", "--full-index", "--unified=3", "--format=%x00%H", headSHA),
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
		branchListArgs(), remoteHeadsArgs(), refsArgs(), branchUnpushedArgs(headSHA), ancestorArgs(headSHA, otherSHA),
		unpushedPastArgs(headRef, otherSHA), unpushedPastArgs(headSHA, otherSHA), unpushedLogArgs(),
		unpushedPastLogArgs(otherSHA), copyUnpushedArgs([]string{headSHA}, ""),
		copyUnpushedArgs([]string{headSHA, otherSHA}, otherSHA), copyCandidatesArgs(0, ""),
		copyCandidatesArgs(1699913600, "refs/remotes/origin/main"), copySidesArgs(otherSHA, headSHA),
		copyPatchesArgs([]string{headSHA, otherSHA}),
	} {
		_, _, err := git{run: &testutil.Runner{}, bin: gitBin}.do(t.Context(), repoDir, args...)
		require.NoError(t, err, args)
	}
}

func TestTheCommitsNotOnGitHubAreListedAsARead(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		unpushedLogArgs(), unpushedPastLogArgs(otherSHA), refsArgs(), copyUnpushedArgs([]string{headSHA}, otherSHA),
		copyCandidatesArgs(0, "refs/remotes/origin/main"), copySidesArgs(otherSHA, headSHA), copyPatchesArgs([]string{headSHA}),
	} {
		assert.True(t, readOnly(args), args)
	}

	readEnv := []string{"GIT_NO_LAZY_FETCH=1"}
	guard := failingGuard{env: func() []string { return readEnv }, err: errFakeStart, calls: &atomic.Int32{}}

	var envs [][]string

	g := git{bin: gitPath, guard: guard, with: func(env []string) execpath.Runner {
		envs = append(envs, env)

		return &testutil.Runner{}
	}}

	_, _, err := g.unpushedCommits(t.Context(), treePath, otherSHA)
	require.NoError(t, err)
	assert.Equal(t, [][]string{readEnv}, envs, "it runs with gitread's read environment")
	assert.Zero(t, guard.calls.Load(), "and works out no setup of the clone's own config, as a status would")

	// So do the reads that compare commits with the pushed branches', a partial clone's
	// missing object failing them rather than being fetched.
	envs = nil

	_, err = g.copyUnpushed(t.Context(), repoDir, []string{headSHA}, otherSHA)
	require.NoError(t, err)
	_, err = g.copyCandidates(t.Context(), repoDir, 0, "")
	require.NoError(t, err)
	_, err = g.copySides(t.Context(), repoDir, otherSHA, headSHA)
	require.NoError(t, err)
	_, err = g.readRefs(t.Context(), repoDir)
	require.NoError(t, err)
	_, err = g.copyPatches(t.Context(), repoDir, []string{headSHA})
	require.NoError(t, err)
	assert.Equal(t, [][]string{readEnv, readEnv, readEnv, readEnv, readEnv}, envs)
	assert.Zero(t, guard.calls.Load())
}

func TestCopyReadsParseWhatGitPrints(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	run := func(stdout, stderr string, err error) git {
		return git{run: &testutil.Runner{Stdout: stdout, Stderr: stderr, Err: err}, bin: gitBin}
	}

	set, err := run(headSHA+"\x00"+otherSHA+" "+strings.Repeat("1", 40)+"\x001700000000\x00a@example.com\x00Fix: a\x00b\n\n", "", nil).
		copyUnpushed(ctx, repoDir, []string{headSHA}, "")
	require.NoError(t, err)
	assert.Equal(t, map[string]copyCommit{headSHA: {
		oid: headSHA, parents: []string{otherSHA, strings.Repeat("1", 40)}, at: 1700000000, email: "a@example.com",
		subject: "Fix: a\x00b",
	}}, set, "a subject is kept whole, whatever it holds")

	for _, bad := range []string{"not-a-commit\x00\x001\x00a\x00s\n", headSHA + "\x00\x00soon\x00a\x00s\n", headSHA + "\x00x\n"} {
		_, err = run(bad, "", nil).copyUnpushed(ctx, repoDir, []string{headSHA}, "")
		require.ErrorIs(t, err, errParseCommits, bad)
	}

	many := strings.Repeat(headSHA+"\x00\x001\x00a\x00s\n", copyBatchMax)
	_, err = run(many, "", nil).copyUnpushed(ctx, repoDir, []string{headSHA}, "")
	require.ErrorIs(t, err, errTooManyCommits)

	candidates, err := run(headSHA+"\x00origin/feature-x\x001\x00a\x00s\n"+
		otherSHA+"\x00refs/remotes/upstream/feature-y\x002\x00b\x00t\n", "", nil).copyCandidates(ctx, repoDir, 0, "")
	require.NoError(t, err)
	assert.Equal(t, []copyCommit{
		{oid: headSHA, ref: "refs/remotes/origin/feature-x", at: 1, email: "a", subject: "s"},
		{oid: otherSHA, ref: "refs/remotes/upstream/feature-y", at: 2, email: "b", subject: "t"},
	}, candidates, "the branch each was found on, by its full name")

	_, err = run(strings.Repeat(headSHA+"\x00origin/x\x001\x00a\x00s\n", copyCandidatesMax), "", nil).
		copyCandidates(ctx, repoDir, 0, "")
	require.ErrorIs(t, err, errTooManyCandidates)

	sides, err := run(">"+headSHA+"\n<"+otherSHA+"\n", "", nil).copySides(ctx, repoDir, otherSHA, headSHA)
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{headSHA: false, otherSHA: true}, sides, "true for the tip's side")

	for _, bad := range []string{"", "=" + headSHA, "-" + headSHA, "+" + headSHA, "<main", headSHA} {
		_, err = run(bad+"\n", "", nil).copySides(ctx, repoDir, otherSHA, headSHA)
		require.ErrorIs(t, err, errParseCommits, bad)
	}

	_, err = run(">"+headSHA+"\n", "warning: something odd", nil).copySides(ctx, repoDir, otherSHA, headSHA)
	require.ErrorIs(t, err, errGitWarned, "an answer git warned about is no answer")

	_, err = run("", "fatal: bad object", exitError{code: 128}).copySides(ctx, repoDir, otherSHA, headSHA)

	gitErr, ok := errors.AsType[*copyGitError](err)
	require.True(t, ok)
	assert.Equal(t, "fatal: bad object", string(gitErr.stderr))
	assert.Equal(t, exitError{code: 128}, errors.Unwrap(err))
	assert.Equal(t, "exit status 128", err.Error())
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

	refs := parseRefs([]byte(strings.Join([]string{
		"refs/heads/main\x00" + headSHA + "\x00refs/remotes/origin/main\x00\x00/srv/clone",
		"refs/heads/feat/x\x00" + otherSHA + "\x00\x00\x00",
		"refs/heads/\x00" + headSHA + "\x00\x00\x00",
		"refs/heads/broken\x00not-a-commit\x00\x00\x00",
		"refs/remotes/origin/HEAD\x00" + headSHA + "\x00\x00refs/remotes/origin/main\x00",
		"refs/remotes/upstream/HEAD\x00" + headSHA + "\x00\x00refs/remotes/origin/develop\x00",
		"refs/remotes/origin/main\x00" + headSHA + "\x00\x00\x00",
		"refs/remotes/origin/feat/x\x00" + otherSHA + "\x00\x00\x00",
		"refs/remotes/origin/-x\x00" + otherSHA + "\x00\x00\x00",
		"refs/remotes/origin/linked\x00" + otherSHA + "\x00\x00refs/remotes/origin/feat/x\x00",
		"refs/remotes/bad..remote/x\x00" + otherSHA + "\x00\x00\x00",
		"too\x00few",
	}, "\n") + "\n"))

	assert.Equal(t, []localBranch{
		{name: mainBranch, head: headSHA, worktree: "/srv/clone", upstream: "refs/remotes/origin/main"},
		{name: "feat/x", head: otherSHA},
	}, refs.locals, "lines that are not a branch and a commit are left out")
	assert.Equal(t, map[string]string{
		"refs/remotes/origin/main": headSHA, "refs/remotes/origin/feat/x": otherSHA,
	}, refs.tracking, "remote-tracking branches with a plain remote and branch, no symbolic ref among them")
	assert.Equal(t, map[string]string{originRemote: mainBranch}, refs.defaults,
		"a remote's HEAD pointing at its own branch; one pointing at another remote's is no answer")
	assert.Equal(t, []string{mainBranch}, refs.defaultNames())
	assert.NotEqual(t, refs.fingerprint(), parseRefs(nil).fingerprint())
	assert.Equal(t, refs.fingerprint(), parseRefs([]byte("refs/remotes/origin/feat/x\x00"+otherSHA+"\x00\x00\x00\n"+
		"refs/remotes/origin/HEAD\x00"+headSHA+"\x00\x00refs/remotes/origin/main\x00\n"+
		"refs/remotes/upstream/HEAD\x00"+otherSHA+"\x00\x00refs/remotes/origin/develop\x00\n"+
		"refs/remotes/origin/main\x00"+headSHA+"\x00\x00\x00\n")).fingerprint(),
		"the same remote state in another order, without the local branches")

	heads := git{run: &testutil.Runner{Stdout: "main\n\nmain\ndevelop\n"}, bin: gitBin}.remoteHeads(t.Context(), repoDir)
	assert.Equal(t, []string{"main", "develop"}, heads)
	assert.Empty(t, git{run: &testutil.Runner{Err: errFakeStart}, bin: gitBin}.remoteHeads(t.Context(), repoDir))

	n, err := git{run: &testutil.Runner{Stdout: "3\n"}, bin: gitBin}.unpushedOf(t.Context(), repoDir, headSHA, "")
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	_, err = git{run: &testutil.Runner{Stdout: "lots\n"}, bin: gitBin}.unpushedOf(t.Context(), repoDir, headSHA, "")
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

	t.Run("the first changed files", func(t *testing.T) {
		t.Parallel()

		run := &testutil.Runner{Stdout: "?? z\x00 M f\x00R  new\x00old\x00"}
		count, files, _, err := git{run: run, bin: gitPath}.firstChanged(ctx, treePath, 2)
		require.NoError(t, err)
		assert.Equal(t, 3, count)
		assert.Equal(t, []FileEntry{{Code: " M", Path: "f"}, {Code: "R ", Path: "new"}}, files, "by path")
		assert.Equal(t, [][]string{gitArgv(treePath, "status --porcelain=v1 -z --untracked-files=all")}, run.Calls(),
			"the one status the count reads")
	})

	t.Run("the first changed files failure returns stderr", func(t *testing.T) {
		t.Parallel()

		run := &testutil.Runner{Stderr: "fatal: unreadable index", Err: exitError{code: 128}}
		count, files, stderr, err := git{run: run, bin: gitPath}.firstChanged(ctx, treePath, 12)
		require.Error(t, err)
		assert.Zero(t, count)
		assert.Nil(t, files)
		assert.Equal(t, "fatal: unreadable index", string(stderr))
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
		count, _, err := git{run: run, bin: gitBin}.unpushed(ctx, treePath, "")
		require.NoError(t, err)
		assert.Equal(t, 3, count)
	})

	t.Run("unpushed count with unexpected output", func(t *testing.T) {
		t.Parallel()

		for _, out := range []string{"many\n", "-1\n"} {
			run := &testutil.Runner{Stdout: out}
			_, _, err := git{run: run, bin: gitBin}.unpushed(ctx, treePath, "")
			require.ErrorIs(t, err, errParseCount, out)
		}
	})

	t.Run("the commits not on GitHub", func(t *testing.T) {
		t.Parallel()

		run := &testutil.Runner{Stdout: headSHA + " 1700000100 Fix the cache: keep  two spaces\n\n" + otherSHA + " 1700000000 \n"}
		commits, _, err := git{run: run, bin: gitBin}.unpushedCommits(ctx, treePath, "")
		require.NoError(t, err)
		assert.Equal(t, []UnpushedCommit{
			{OID: headSHA, Unix: 1700000100, Subject: "Fix the cache: keep  two spaces"},
			{OID: otherSHA, Unix: 1700000000},
		}, commits, "in git's order, newest first, a subject's words as they are, an empty one, and no blank line")

		commits, _, err = git{run: &testutil.Runner{}, bin: gitBin}.unpushedCommits(ctx, treePath, "")
		require.NoError(t, err)
		assert.NotNil(t, commits, "none is [], never null")
		assert.Empty(t, commits)
	})

	t.Run("the commits not on GitHub with unexpected output", func(t *testing.T) {
		t.Parallel()

		for _, out := range []string{
			"commits\n", headSHA + "\n", headSHA + " yesterday Fix\n", "not-a-commit 1700000000 Fix\n",
			headSHA + " 1700000000 Fix\nsomething else\n",
		} {
			run := &testutil.Runner{Stdout: out}
			_, _, err := git{run: run, bin: gitBin}.unpushedCommits(ctx, treePath, "")
			require.ErrorIs(t, err, errParseCommits, out)
		}
	})

	t.Run("the commits not on GitHub failure returns stderr", func(t *testing.T) {
		t.Parallel()

		run := &testutil.Runner{Stderr: "fatal: bad revision 'HEAD'", Err: exitError{code: 128}}
		_, stderr, err := git{run: run, bin: gitBin}.unpushedCommits(ctx, treePath, otherSHA)
		require.Error(t, err)
		assert.Equal(t, "fatal: bad revision 'HEAD'", string(stderr))
	})

	t.Run("unpushed count failure returns stderr", func(t *testing.T) {
		t.Parallel()

		run := &testutil.Runner{Stderr: "fatal: bad revision 'HEAD'", Err: exitError{code: 128}}
		_, stderr, err := git{run: run, bin: gitBin}.unpushed(ctx, treePath, "")
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

func TestReadStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		out   string
		n     int
		count int
		want  []FileEntry
	}{
		{name: "clean", out: "", n: 12, want: []FileEntry{}},
		{name: "modified in the worktree", out: " M f\x00", n: 12, count: 1, want: []FileEntry{{Code: " M", Path: "f"}}},
		{name: "staged, then modified again", out: "MM f\x00", n: 12, count: 1, want: []FileEntry{{Code: "MM", Path: "f"}}},
		{name: "untracked", out: "?? g\x00", n: 12, count: 1, want: []FileEntry{{Code: "??", Path: "g"}}},
		{
			name: "a rename is its new path, its source no file of its own", out: "R  new\x00old\x00 M f\x00", n: 12,
			count: 2, want: []FileEntry{{Code: " M", Path: "f"}, {Code: "R ", Path: "new"}},
		},
		{
			name: "a copy in the worktree column", out: " C copy\x00orig\x00?? g\x00", n: 12, count: 2,
			want: []FileEntry{{Code: " C", Path: "copy"}, {Code: "??", Path: "g"}},
		},
		{
			name: "a rename whose source looks like an entry", out: "RM new\x00?? old\x00", n: 12, count: 1,
			want: []FileEntry{{Code: "RM", Path: "new"}},
		},
		{
			name: "staged, deleted, and conflicted", out: "UU c\x00 D b\x00A  a\x00", n: 12, count: 3,
			want: []FileEntry{{Code: "A ", Path: "a"}, {Code: " D", Path: "b"}, {Code: "UU", Path: "c"}},
		},
		{
			name: "paths with spaces and beyond ASCII, unquoted, by their bytes", n: 12, count: 3,
			out: "?? with space.txt\x00 M docs/café ☕.md\x00?? README.md\x00",
			want: []FileEntry{
				{Code: "??", Path: "README.md"}, {Code: " M", Path: "docs/café ☕.md"}, {Code: "??", Path: "with space.txt"},
			},
		},
		{name: "a field too short to be an entry", out: "M\x00\x00 M f", n: 12, count: 1, want: []FileEntry{{Code: " M", Path: "f"}}},
		{
			name: "only the first n are kept, every one counted", out: "?? d\x00?? b\x00?? e\x00?? a\x00?? c\x00", n: 2,
			count: 5, want: []FileEntry{{Code: "??", Path: "a"}, {Code: "??", Path: "b"}},
		},
		{name: "a count alone keeps none", out: " M f\x00?? g\x00", count: 2, want: []FileEntry{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			count, files := readStatus([]byte(tt.out), tt.n)
			assert.Equal(t, tt.count, count)
			assert.Equal(t, tt.want, files, "never nil, so the window gets []")
		})
	}
}

func TestReadStatusKeepsTheFirstOfManyUnsortedFiles(t *testing.T) {
	t.Parallel()

	// 5,000 files in an order of their own, a rename among every ten, as git lists a
	// worktree with many untracked files.
	const total = 5000

	var out []byte

	want := make([]FileEntry, 0, total)

	for i := range total {
		path := fmt.Sprintf("dir-%02d/file-%04d.txt", (i*7919)%13, (i*7919)%total)

		code := "??"
		if i%10 == 0 {
			code = "R "
		}

		out = append(out, code+" "+path+"\x00"...)
		if code == "R " {
			out = append(out, "old/"+path+"\x00"...)
		}

		want = append(want, FileEntry{Code: code, Path: path})
	}

	slices.SortFunc(want, func(a, b FileEntry) int { return strings.Compare(a.Path, b.Path) })

	for _, n := range []int{1, 12, 200} {
		count, files := readStatus(out, n)
		assert.Equal(t, total, count, "every file counted, a rename once")
		assert.Equal(t, want[:n], files, "the first %d by path", n)
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
