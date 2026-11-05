package worktrees

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	gitPath  = "/opt/tools/bin/git"
	repoDir  = "/srv/projects/example-repo"
	treePath = "/srv/wt/feature"
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

// recordingRunner records every call and answers with fixed output or error.
type recordingRunner struct {
	mu     sync.Mutex
	calls  [][]string
	stdout string
	stderr string
	err    error
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, append([]string{name}, args...))

	return []byte(r.stdout), []byte(r.stderr), r.err
}

func TestGitArgvUsesDashC(t *testing.T) {
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
			want:   argv(gitPath, "-C", repoDir, "worktree list --porcelain -z"),
		},
		{
			name: "remove",
			call: func(g git) error { _, err := g.remove(ctx, repoDir, treePath, false); return err },
			want: argv(gitPath, "-C", repoDir, "worktree remove", treePath),
		},
		{
			name: "force remove passes a single --force",
			call: func(g git) error { _, err := g.remove(ctx, repoDir, treePath, true); return err },
			want: argv(gitPath, "-C", repoDir, "worktree remove --force", treePath),
		},
		{
			name: "prune",
			call: func(g git) error { _, err := g.prune(ctx, repoDir); return err },
			want: argv(gitPath, "-C", repoDir, "worktree prune"),
		},
		{
			name: "status runs in the worktree",
			call: func(g git) error { _, _, err := g.dirtyCount(ctx, treePath); return err },
			want: argv(gitPath, "-C", treePath, "status --porcelain=v1 -z --untracked-files=all"),
		},
		{
			name: "last commit runs in the worktree",
			call: func(g git) error { _, err := g.lastCommit(ctx, treePath); return err },
			want: argv(gitPath, "-C", treePath, "log -1 --format=%ct"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := &recordingRunner{stdout: tt.stdout}
			if run.stdout == "" {
				run.stdout = "1700000000\n"
			}

			require.NoError(t, tt.call(git{run: run, bin: gitPath}))
			assert.Equal(t, [][]string{tt.want}, run.calls)
		})
	}
}

func TestGitRejectsCommandsOutsideAllowlist(t *testing.T) {
	refused := [][]string{
		argv("branch -D worktree-pr-7"),
		argv("worktree remove --force --force", treePath),
		argv("worktree remove -f", treePath),
		argv("worktree remove --force"),
		argv("worktree remove"),
		argv("worktree remove", ""),
		argv("worktree remove", treePath, "extra"),
		argv("worktree add", treePath),
		argv("worktree unlock", treePath),
		argv("worktree list"),
		argv("checkout main"),
		argv("status"),
		{},
	}

	for _, args := range refused {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			run := &recordingRunner{}
			_, _, err := git{run: run, bin: gitBin}.do(t.Context(), repoDir, args...)
			require.ErrorIs(t, err, ErrGitCommandNotAllowed)
			assert.Empty(t, run.calls, "a refused command never runs")
		})
	}
}

func TestGitOutputParsing(t *testing.T) {
	ctx := t.Context()

	t.Run("list parses porcelain", func(t *testing.T) {
		run := &recordingRunner{stdout: string(porcelain(mainAttrs()))}
		entries, _, err := git{run: run, bin: gitBin}.list(ctx, repoDir)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.True(t, entries[0].Main)
	})

	t.Run("list returns git's stderr on failure", func(t *testing.T) {
		run := &recordingRunner{stderr: "fatal: not a git repository", err: exitError{code: 128}}
		_, stderr, err := git{run: run, bin: gitBin}.list(ctx, repoDir)
		require.Error(t, err)
		assert.Equal(t, "fatal: not a git repository", string(stderr))
	})

	t.Run("dirty count", func(t *testing.T) {
		run := &recordingRunner{stdout: " M f\x00?? g\x00"}
		count, _, err := git{run: run, bin: gitBin}.dirtyCount(ctx, treePath)
		require.NoError(t, err)
		assert.Equal(t, 2, count)
	})

	t.Run("dirty count failure", func(t *testing.T) {
		run := &recordingRunner{err: errFakeStart}
		_, _, err := git{run: run, bin: gitBin}.dirtyCount(ctx, treePath)
		require.ErrorIs(t, err, errFakeStart)
	})

	t.Run("last commit", func(t *testing.T) {
		run := &recordingRunner{stdout: "1700000000\n"}
		unix, err := git{run: run, bin: gitBin}.lastCommit(ctx, treePath)
		require.NoError(t, err)
		assert.Equal(t, int64(1700000000), unix)
	})

	t.Run("last commit with unexpected output", func(t *testing.T) {
		run := &recordingRunner{stdout: "yesterday\n"}
		_, err := git{run: run, bin: gitBin}.lastCommit(ctx, treePath)
		require.ErrorIs(t, err, errParseCommitTime)
	})

	t.Run("last commit failure", func(t *testing.T) {
		run := &recordingRunner{err: exitError{code: 128}}
		_, err := git{run: run, bin: gitBin}.lastCommit(ctx, treePath)
		require.Error(t, err)
	})
}

func TestCountStatusEntries(t *testing.T) {
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
			assert.Equal(t, tt.want, countStatusEntries([]byte(tt.out)))
		})
	}
}

func TestGitMessages(t *testing.T) {
	assert.Equal(t, "not a git repository", firstLine([]byte("\n  fatal: not a git repository\nmore")))
	assert.Equal(t, "bad thing", firstLine([]byte("error: bad thing")))
	assert.Empty(t, firstLine([]byte("\n \n")))

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
	assert.True(t, exitCode(ctx, fmt.Errorf("run git: %w", cause)))
	assert.False(t, exitCode(ctx, errFakeStart))
	assert.False(t, exitCode(ctx, exitError{code: 0}))

	cancel()
	assert.False(t, exitCode(ctx, cause), "a canceled run is not git refusing")
}
