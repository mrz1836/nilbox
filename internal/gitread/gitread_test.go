package gitread

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/execpath"
)

const repoDir = "/srv/projects/example-repo"

// originURL is the URL the fake git gives origin, unless a reply says otherwise.
const originURL = "https://github.com/acme-org/example-repo.git"

var errKilled = errors.New("fake: signal: killed")

// exitError is a fake process exit with a status, which execpath.ExitCode reads.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

func (e exitError) ExitCode() int { return e.code }

// reply is a fake git's answer to one command: what it prints, and its exit status.
type reply struct {
	out  string
	code int
}

// call is one command a recorder ran.
type call struct {
	// argv is the whole command line, git first; extra the -c settings gitread added after
	// the hardening; and args the words after those.
	argv  []string
	extra []string
	args  []string

	env []string

	// deadline is the command's context's deadline, when it has one.
	deadline    time.Time
	hasDeadline bool
}

// recorder is a fake git: it records every command it is asked to run, with the
// environment and deadline it was given, and answers from replies, keyed by the args
// joined with spaces, or else with answer. A command neither knows exits 128.
type recorder struct {
	replies map[string]reply
	answer  func(ctx context.Context, args []string) (string, error)

	mu    sync.Mutex
	calls []call
}

// Calls returns every command run so far, in order.
func (r *recorder) Calls() []call {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.calls)
}

// Args returns the args of every command run so far, in order.
func (r *recorder) Args() [][]string {
	calls := r.Calls()
	out := make([][]string, 0, len(calls))

	for _, c := range calls {
		out = append(out, c.args)
	}

	return out
}

// runner is the recorder as a Config.Runner.
func (r *recorder) runner(env []string) execpath.Runner {
	return recorded{r: r, env: slices.Clone(env)}
}

// recorded is one command's runner.
type recorded struct {
	r   *recorder
	env []string
}

// Run records the command and answers it.
func (c recorded) Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	deadline, ok := ctx.Deadline()

	argv := append([]string{name}, args...)
	rest := args[min(len(args), 2+len(hardening())):]

	var extra []string
	for len(rest) >= 2 && rest[0] == "-c" {
		extra, rest = append(extra, rest[:2]...), rest[2:]
	}

	c.r.mu.Lock()
	c.r.calls = append(c.r.calls, call{argv: argv, extra: extra, args: rest, env: c.env, deadline: deadline, hasDeadline: ok})
	c.r.mu.Unlock()

	if got, found := c.r.replies[strings.Join(rest, " ")]; found {
		if got.code != 0 {
			return []byte(got.out), []byte("fatal: fake refusal\n"), exitError{code: got.code}
		}

		return []byte(got.out), nil, nil
	}

	if slices.Equal(rest, getURLArgs()) {
		return []byte(originURL + "\n"), nil, nil // origin on GitHub, unless a reply says otherwise
	}

	if c.r.answer != nil {
		out, answerErr := c.r.answer(ctx, rest)

		return []byte(out), nil, answerErr
	}

	if slices.Equal(rest, filterDriversArgs()) || slices.Equal(rest, remoteKeysArgs()) {
		return nil, nil, exitError{code: 1} // none of the keys set, unless a reply says otherwise
	}

	return nil, []byte("fatal: fake git knows no such command\n"), exitError{code: 128}
}

// fakeGit returns a Git over env that runs rec.
func fakeGit(env []string, rec *recorder) *Git {
	return New(Config{Env: func() []string { return slices.Clone(env) }, Runner: rec.runner})
}

// plainEnv is an environment that sets no ssh of its own.
func plainEnv() []string {
	return []string{"PATH=/opt/tools/bin:/usr/bin", "HOME=/srv/home", "LANG=C"}
}

// words joins args with spaces, as recorder replies are keyed.
func words(args []string) string { return strings.Join(args, " ") }

func TestRunCommandLine(t *testing.T) {
	t.Parallel()

	rec := &recorder{replies: map[string]reply{words(worktreesArgs()): {}}}

	_, _, err := fakeGit(plainEnv(), rec).Run(t.Context(), repoDir, worktreesArgs()...)
	require.NoError(t, err)

	calls := rec.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, []string{
		"git", "-C", repoDir, "-c", "core.fsmonitor=false", "-c", "submodule.recurse=false", "worktree", "list", "--porcelain", "-z",
	}, calls[0].argv)

	for _, arg := range calls[0].argv {
		assert.NotContains(t, arg, "hooksPath", "gitread leaves core.hooksPath alone")
	}

	assert.False(t, calls[0].hasDeadline, "only ls-remote has a time limit")
}

func TestRunEnvironment(t *testing.T) {
	t.Parallel()

	given := []string{
		"PATH=/opt/tools/bin", "HOME=/srv/home", "GIT_TERMINAL_PROMPT=1", "GIT_OPTIONAL_LOCKS=1",
		"GIT_DIR=/srv/other/.git", "GIT_WORK_TREE=/srv/other", "GIT_INDEX_FILE=/srv/other/index",
		"GIT_COMMON_DIR=/srv/other/.git", "GIT_OBJECT_DIRECTORY=/srv/other/objects",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=/srv/more", "GIT_NAMESPACE=ns", "GIT_CONFIG_GLOBAL=/srv/home/gitconfig",
		"GIT_CONFIG=/srv/other/config", "GIT_NO_LAZY_FETCH=0", "GIT_ALLOW_PROTOCOL=ext",
		"SSH_AUTH_SOCK=/srv/agent.sock",
	}
	snapshot := slices.Clone(given)
	rec := &recorder{replies: map[string]reply{words(worktreesArgs()): {}}}
	g := New(Config{Env: func() []string { return given }, Runner: rec.runner})

	_, _, err := g.Run(t.Context(), repoDir, worktreesArgs()...)
	require.NoError(t, err)

	calls := rec.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, []string{
		"PATH=/opt/tools/bin", "HOME=/srv/home", "GIT_CONFIG_GLOBAL=/srv/home/gitconfig", "SSH_AUTH_SOCK=/srv/agent.sock",
		"GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_ALLOW_PROTOCOL=",
	}, calls[0].env, "the four settings replace the given ones, and nothing points git at another repository or config")
	assert.Equal(t, snapshot, given, "the given environment is not changed")
}

func TestRunReadsTheEnvironmentForEachCommand(t *testing.T) {
	t.Parallel()

	var reads atomic.Int32

	rec := &recorder{replies: map[string]reply{words(worktreesArgs()): {}}}
	g := New(Config{
		Env:    func() []string { return []string{fmt.Sprintf("READ=%d", reads.Add(1))} },
		Runner: rec.runner,
	})

	for range 2 {
		_, _, err := g.Run(t.Context(), repoDir, worktreesArgs()...)
		require.NoError(t, err)
	}

	calls := rec.Calls()
	require.Len(t, calls, 2)
	assert.Contains(t, calls[0].env, "READ=1")
	assert.Contains(t, calls[1].env, "READ=2")
}

func TestNewDefaultsToTheProcessEnvironment(t *testing.T) {
	t.Parallel()

	rec := &recorder{replies: map[string]reply{words(worktreesArgs()): {}}}

	_, _, err := New(Config{Runner: rec.runner}).Run(t.Context(), repoDir, worktreesArgs()...)
	require.NoError(t, err)

	calls := rec.Calls()
	require.Len(t, calls, 1)
	assert.Len(t, calls[0].env, len(New(Config{}).environment()))

	runner, ok := execRunner([]string{"PATH=/opt/tools/bin", "HOME=/srv/home"}).(execpath.Exec)
	require.True(t, ok)
	assert.Equal(t, execpath.Search{Path: "/opt/tools/bin", Home: "/srv/home"}, runner.Search,
		"git is found on the given environment's PATH")
	assert.Equal(t, []string{"PATH=/opt/tools/bin", "HOME=/srv/home"}, runner.Env)
}

func TestOnlyRemoteCommandsGetTheBatchSSH(t *testing.T) {
	t.Parallel()

	rec := &recorder{replies: map[string]reply{words(worktreesArgs()): {}}}

	_, _, err := fakeGit(plainEnv(), rec).Run(t.Context(), repoDir, worktreesArgs()...)
	require.NoError(t, err)

	assert.Equal(t, [][]string{worktreesArgs()}, rec.Args(), "a local command reads no ssh config")
	assert.NotContains(t, rec.Calls()[0].env, "GIT_SSH_COMMAND="+batchSSH)
}

func TestRemoteTimeout(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rec := &recorder{
			replies: map[string]reply{words(remoteKeysArgs()): {code: 1}},
			answer: func(ctx context.Context, _ []string) (string, error) {
				<-ctx.Done() // origin never answers

				return "", errKilled
			},
		}

		start := time.Now()

		_, err := fakeGit(plainEnv(), rec).RemoteRefs(t.Context(), repoDir)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, RemoteTimeout, time.Since(start), "ls-remote stops after the time limit, no sooner")

		for _, c := range rec.Calls() {
			assert.True(t, c.hasDeadline, "%q", c.args)
			assert.Equal(t, start.Add(RemoteTimeout), c.deadline, "%q", c.args)
		}
	})
}

func TestRemoteAnswersInTimeAreKept(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rec := &recorder{
			replies: map[string]reply{words(remoteKeysArgs()): {code: 1}},
			answer: func(_ context.Context, _ []string) (string, error) {
				time.Sleep(RemoteTimeout - time.Second)

				return headSHA + "\trefs/heads/main\n", nil
			},
		}

		refs, err := fakeGit(plainEnv(), rec).RemoteRefs(t.Context(), repoDir)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{mainBranch: headSHA}, refs.Heads)
	})
}

func TestRunRefusesCommandsOutsideTheAllowlist(t *testing.T) {
	t.Parallel()

	refused := [][]string{
		{},
		{"push", remoteName, mainBranch},
		{"push", remoteName, "v1.4.2"},
		{"fetch", remoteName},
		{"fetch", "--tags", "--force", remoteName},
		{"pull", "--rebase", remoteName},
		{"checkout", mainBranch},
		{"switch", mainBranch},
		{"merge", mainBranch},
		{"rebase", mainBranch},
		{"commit", "-m", "x"},
		{"reset", "--hard"},
		{"stash"},
		{"restore", "."},
		{"cherry-pick", headSHA},
		{"clean", "-fdx"},
		{"branch", "-D", mainBranch},
		{"update-ref", "-d", "refs/heads/main", headSHA},
		{"tag", "v9.9.9"},
		{"tag", "-a", "v9.9.9", "-m", "v9.9.9"},
		{"tag", "-d", "v1.4.2"},
		{"tag", "--list"},
		{"tag", "--list", "v*", "--contains", headSHA},
		{"config", "core.sshCommand", "ssh -i /srv/key"},
		{"config", "--add", "user.signingkey", "x"},
		{"config", "--unset", "core.sshCommand"},
		{"config", "--get", "core.hooksPath"},
		{"config", "--get", "remote.origin.url"},
		{"config", "--type=bool", "--get", "core.sshCommand"},
		{"config", "--get-regexp", "branch\\..*"},
		{"worktree", "add", "/srv/wt"},
		{"worktree", "remove", "/srv/wt"},
		{"worktree", "list"},
		{"symbolic-ref", "HEAD", "refs/heads/other"},
		{"symbolic-ref", "--quiet", "--short", "HEAD", "refs/heads/other"},
		{"status"},
		{"status", "--porcelain", "-z", "--ignored"},
		{"status", "--porcelain", "-z", "--", "secret"},
		{"rev-parse", "--git-path", "../../etc/passwd"},
		{"rev-parse", "--git-path", "config"},
		{"rev-parse", "--verify", "--quiet", "refs/heads/-x"},
		{"rev-parse", "--verify", "--quiet", "refs/heads/a..b"},
		{"rev-parse", "--verify", "--quiet", "refs/heads/x\x00y"},
		{"rev-parse", "--verify", "--quiet", "refs/heads/x\ny"},
		{"rev-parse", "--verify", "--quiet", "refs/heads/" + injection},
		{"rev-parse", "--verify", "refs/heads/main"},
		{"rev-parse", "--show-toplevel"},
		{"rev-list", "--count", "refs/remotes/origin/main..refs/heads/main"},
		{"rev-list", "--count", "refs/remotes/origin/a..refs/heads/b", "--"},
		{"rev-list", "--count", "HEAD", "--not", "--remotes"},
		{"merge-base", "--is-ancestor", "HEAD", headSHA},
		{"merge-base", headSHA, otherSHA},
		{"merge-base", "--is-ancestor", headSHA},
		{lsRemote, injection, remoteName},
		{lsRemote, "--heads", "--tags", remoteName, injection},
		{lsRemote, "--heads", "--tags", "upstream"},
		{lsRemote, "--heads", "--tags", "/srv/elsewhere.git"},
		{lsRemote, "--heads", "--tags", "ext::sh -c touch% /srv/pwned"},
		{lsRemote, "--tags", "--refs", remoteName, "*"},
		{lsRemote, "--tags", "--refs", remoteName, injection},
		{lsRemote, remoteName, "refs/tags/v1.4.2", "refs/tags/v1.4.3^{}"},
		{lsRemote, remoteName, "refs/tags/v1.4.2"},
		{lsRemote, "--symref", remoteName, "HEAD"},
		{lsRemote, "--heads", tagsFlag, remoteName},
		{lsRemote, "--upload-pack=sh -c touch", "--heads", tagsFlag, remoteName},
		{lsRemote, uploadPackFlag, "--upload-pack=x", "--heads", tagsFlag, remoteName},
		{configCmd, getFlag, "core.sshCommand"},
		{configCmd, "--show-scope", "-z", "--get-regexp", "^core\\..*$"},
		{configCmd, "--show-scope", "--get-regexp", remoteKeysPattern},
		{"-c", "core.hooksPath=/srv/hooks", "status", "--porcelain", "-z"},
		{"--exec-path=/srv/bin", "status", "--porcelain", "-z"},
		{"--git-dir=/srv/other/.git", "status", "--porcelain", "-z"},
	}

	for _, args := range refused {
		rec := &recorder{}

		_, _, err := fakeGit(plainEnv(), rec).Run(t.Context(), repoDir, args...)
		require.ErrorIs(t, err, ErrGitCommandNotAllowed, "%q", args)
		assert.Empty(t, rec.Calls(), "%q ran", args)
	}
}

func TestRunRefusesFoldersThatAreNotAbsoluteAndClean(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{
		"", "relative/dir", "-C", injection, "/srv/projects/../etc", "/srv/projects/", "/srv//projects",
		"/srv/projects\nb", "/srv/projects\x00b",
	} {
		rec := &recorder{}

		_, _, err := fakeGit(plainEnv(), rec).Run(t.Context(), dir, worktreesArgs()...)
		require.ErrorIs(t, err, ErrGitCommandNotAllowed, "%q", dir)
		assert.Empty(t, rec.Calls(), "%q ran", dir)
	}
}

func TestHelpersRefuseBadNamesBeforeRunning(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	g := fakeGit(plainEnv(), rec)
	ctx := t.Context()

	for name, run := range map[string]func() error{
		"LocalBranch":     func() error { _, err := g.LocalBranch(ctx, repoDir, injection); return err },
		"TrackingBranch":  func() error { _, err := g.TrackingBranch(ctx, repoDir, "a..b"); return err },
		"HasCommit":       func() error { _, err := g.HasCommit(ctx, repoDir, "HEAD"); return err },
		"Upstream":        func() error { _, err := g.Upstream(ctx, repoDir, "-x"); return err },
		"Unpushed":        func() error { _, err := g.Unpushed(ctx, repoDir, "main\nx"); return err },
		"IsAncestor":      func() error { _, err := g.IsAncestor(ctx, repoDir, mainBranch, headSHA); return err },
		"CommitsBetween":  func() error { _, err := g.CommitsBetween(ctx, repoDir, headSHA, "HEAD"); return err },
		"RemoteTagCommit": func() error { _, err := g.RemoteTagCommit(ctx, repoDir, "1.4.2"); return err },
		"RemoteTagCommit option": func() error {
			_, err := g.RemoteTagCommit(ctx, repoDir, "v1 "+injection)

			return err
		},
		"CurrentBranch in a relative folder": func() error { _, err := g.CurrentBranch(ctx, "clone"); return err },
	} {
		require.ErrorIs(t, run(), ErrGitCommandNotAllowed, name)
	}

	assert.Empty(t, rec.Calls())
}

func TestHelpersReadGitsAnswers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	tests := []struct {
		name    string
		replies map[string]reply
		run     func(g *Git) (any, error)
		want    any
	}{
		{
			name:    "the current branch",
			replies: map[string]reply{words(currentBranchArgs()): {out: "feature/x\n"}},
			run:     func(g *Git) (any, error) { return g.CurrentBranch(ctx, repoDir) },
			want:    "feature/x",
		},
		{
			name:    "a detached HEAD",
			replies: map[string]reply{words(currentBranchArgs()): {code: 1}},
			run:     func(g *Git) (any, error) { return g.CurrentBranch(ctx, repoDir) },
			want:    "",
		},
		{
			name:    "origin's default branch",
			replies: map[string]reply{words(originHeadArgs()): {out: "origin/release/v2\n"}},
			run:     func(g *Git) (any, error) { return g.OriginHead(ctx, repoDir) },
			want:    "release/v2",
		},
		{
			name:    "no origin HEAD",
			replies: map[string]reply{words(originHeadArgs()): {code: 1}},
			run:     func(g *Git) (any, error) { return g.OriginHead(ctx, repoDir) },
			want:    "",
		},
		{
			name:    "an origin HEAD naming another remote's branch",
			replies: map[string]reply{words(originHeadArgs()): {out: "upstream/main\n"}},
			run:     func(g *Git) (any, error) { return g.OriginHead(ctx, repoDir) },
			want:    "",
		},
		{
			name:    "an origin HEAD naming no branch",
			replies: map[string]reply{words(originHeadArgs()): {out: "origin/a..b\n"}},
			run:     func(g *Git) (any, error) { return g.OriginHead(ctx, repoDir) },
			want:    "",
		},
		{
			name: "changes, renames read once",
			replies: map[string]reply{
				words(statusArgs()): {out: " M a.go\x00?? notes/\x00R  new.go\x00old.go\x00A  b c.go\x00 D gone\x00"},
			},
			run: func(g *Git) (any, error) { return g.Changes(ctx, repoDir) },
			want: []Change{
				{Code: " M", Path: "a.go"},
				{Code: "??", Path: "notes/"},
				{Code: "R ", Path: "new.go"},
				{Code: "A ", Path: "b c.go"},
				{Code: " D", Path: "gone"},
			},
		},
		{
			name:    "no changes",
			replies: map[string]reply{words(statusArgs()): {}},
			run:     func(g *Git) (any, error) { return g.Changes(ctx, repoDir) },
			want:    []Change{},
		},
		{
			name: "entries of every kind, odd lines skipped",
			replies: map[string]reply{
				words(entriesArgs()): {out: "1 .M N... 100644 100644 100644 " + headSHA + " " +
					headSHA + " a.go\x00" +
					"1 M. N... 100644 100644 100644 " + headSHA + " " + otherSHA + " docs/my setup.md\x00" +
					"2 R. N... 100644 100644 100644 " + headSHA + " " + otherSHA + " R100 new.go\x00old.go\x00" +
					"u UU N... 100644 100644 100644 100644 " + headSHA + " " + otherSHA + " " + sha256ID + " both.go\x00" +
					"? notes/todo.txt\x00" +
					"# branch.head main\x00" +
					"1 .M short\x00" +
					"u UU short\x00" +
					"?\x00" +
					"? \x00" +
					"2 R. N... 100644 100644 100644 " + headSHA + " " + otherSHA + " R100 last.go"},
			},
			run: func(g *Git) (any, error) { return g.Entries(ctx, repoDir) },
			want: []Entry{
				{Code: ".M", Index: headSHA, Path: "a.go"},
				{Code: "M.", Index: otherSHA, Path: "docs/my setup.md"},
				{Code: "R.", Index: otherSHA, Path: "new.go", From: "old.go"},
				{Code: "UU", Index: headSHA + ":" + otherSHA + ":" + sha256ID, Path: "both.go"},
				{Code: "??", Path: "notes/todo.txt"},
			},
		},
		{
			name:    "no entries",
			replies: map[string]reply{words(entriesArgs()): {}},
			run:     func(g *Git) (any, error) { return g.Entries(ctx, repoDir) },
			want:    []Entry{},
		},
		{
			name:    "a local branch",
			replies: map[string]reply{words(verifyArgs("refs/heads/main")): {out: headSHA + "\n"}},
			run:     func(g *Git) (any, error) { return g.LocalBranch(ctx, repoDir, mainBranch) },
			want:    headSHA,
		},
		{
			name:    "no such local branch",
			replies: map[string]reply{words(verifyArgs("refs/heads/main")): {code: 1}},
			run:     func(g *Git) (any, error) { return g.LocalBranch(ctx, repoDir, mainBranch) },
			want:    "",
		},
		{
			name:    "a branch on origin",
			replies: map[string]reply{words(verifyArgs("refs/remotes/origin/main")): {out: otherSHA + "\n"}},
			run:     func(g *Git) (any, error) { return g.TrackingBranch(ctx, repoDir, mainBranch) },
			want:    otherSHA,
		},
		{
			name:    "a commit the clone has",
			replies: map[string]reply{words(verifyArgs(headSHA + "^{commit}")): {out: headSHA + "\n"}},
			run:     func(g *Git) (any, error) { return g.HasCommit(ctx, repoDir, headSHA) },
			want:    true,
		},
		{
			name:    "a commit the clone lacks",
			replies: map[string]reply{words(verifyArgs(headSHA + "^{commit}")): {code: 1}},
			run:     func(g *Git) (any, error) { return g.HasCommit(ctx, repoDir, headSHA) },
			want:    false,
		},
		{
			name: "an upstream",
			replies: map[string]reply{
				words(configArgs("branch.main.remote")): {out: "origin\n"},
				words(configArgs("branch.main.merge")):  {out: "refs/heads/main\n"},
			},
			run:  func(g *Git) (any, error) { return g.Upstream(ctx, repoDir, mainBranch) },
			want: Upstream{Remote: "origin", Ref: "refs/heads/main"},
		},
		{
			name: "no upstream",
			replies: map[string]reply{
				words(configArgs("branch.main.remote")): {code: 1},
				words(configArgs("branch.main.merge")):  {code: 1},
			},
			run:  func(g *Git) (any, error) { return g.Upstream(ctx, repoDir, mainBranch) },
			want: Upstream{},
		},
		{
			name:    "unpushed commits",
			replies: map[string]reply{words(unpushedArgs(mainBranch)): {out: "3\n"}},
			run:     func(g *Git) (any, error) { return g.Unpushed(ctx, repoDir, mainBranch) },
			want:    3,
		},
		{
			name:    "the commits between two",
			replies: map[string]reply{words(commitsBetweenArgs(headSHA, otherSHA)): {out: "2\n"}},
			run:     func(g *Git) (any, error) { return g.CommitsBetween(ctx, repoDir, headSHA, otherSHA) },
			want:    2,
		},
		{
			name:    "an ancestor",
			replies: map[string]reply{words(ancestorArgs(headSHA, otherSHA)): {}},
			run:     func(g *Git) (any, error) { return g.IsAncestor(ctx, repoDir, headSHA, otherSHA) },
			want:    true,
		},
		{
			name:    "not an ancestor",
			replies: map[string]reply{words(ancestorArgs(headSHA, otherSHA)): {code: 1}},
			run:     func(g *Git) (any, error) { return g.IsAncestor(ctx, repoDir, headSHA, otherSHA) },
			want:    false,
		},
		{
			name:    "the local version tags",
			replies: map[string]reply{words(localTagsArgs()): {out: "v1.0.0\nv1.10.0\nv1.2.0\n"}},
			run:     func(g *Git) (any, error) { return g.LocalVersionTags(ctx, repoDir) },
			want:    []string{"v1.0.0", "v1.10.0", "v1.2.0"},
		},
		{
			name:    "no local version tags",
			replies: map[string]reply{words(localTagsArgs()): {}},
			run:     func(g *Git) (any, error) { return g.LocalVersionTags(ctx, repoDir) },
			want:    []string{},
		},
		{
			name: "origin's version tags",
			replies: map[string]reply{
				words(remoteTagsArgs()): {out: headSHA + "\trefs/tags/v1.1.0\n" + otherSHA + "\trefs/tags/release/v2\n" +
					otherSHA + "\trefs/tags/v1.0.0\n"},
			},
			run:  func(g *Git) (any, error) { return g.RemoteVersionTags(ctx, repoDir) },
			want: []string{"v1.0.0", "v1.1.0"},
		},
		{
			name: "an annotated tag's commit on origin",
			replies: map[string]reply{
				words(remoteTagArgs("v1.4.2")): {
					out: otherSHA + "\trefs/tags/v1.4.2\n" + headSHA + "\trefs/tags/v1.4.2^{}\n",
				},
			},
			run:  func(g *Git) (any, error) { return g.RemoteTagCommit(ctx, repoDir, "v1.4.2") },
			want: headSHA,
		},
		{
			name: "a lightweight tag's commit on origin",
			replies: map[string]reply{
				words(remoteTagArgs("v1.4.2")): {out: headSHA + "\trefs/tags/v1.4.2\n"},
			},
			run:  func(g *Git) (any, error) { return g.RemoteTagCommit(ctx, repoDir, "v1.4.2") },
			want: headSHA,
		},
		{
			name: "a tag origin doesn't have",
			replies: map[string]reply{
				words(remoteTagArgs("v1.4.2")): {},
			},
			run:  func(g *Git) (any, error) { return g.RemoteTagCommit(ctx, repoDir, "v1.4.2") },
			want: "",
		},
		{
			name: "signing on",
			replies: map[string]reply{
				words(boolConfigArgs(keyTagSign)):   {out: "false\n"},
				words(boolConfigArgs(keyForceSign)): {out: "true\n"},
				words(configArgs(keyGPGFormat)):     {out: "ssh\n"},
				words(configArgs(keySigningKey)):    {out: "~/keys/signing.pub\n"},
			},
			run:  func(g *Git) (any, error) { return g.Signing(ctx, repoDir) },
			want: Signing{SignAnnotated: true, Format: "ssh", Key: "~/keys/signing.pub"},
		},
		{
			name: "signing unset",
			replies: map[string]reply{
				words(boolConfigArgs(keyTagSign)):   {code: 1},
				words(boolConfigArgs(keyForceSign)): {code: 1},
				words(configArgs(keyGPGFormat)):     {code: 1},
				words(configArgs(keySigningKey)):    {code: 1},
			},
			run:  func(g *Git) (any, error) { return g.Signing(ctx, repoDir) },
			want: Signing{},
		},
		{
			name:    "the hooks folder",
			replies: map[string]reply{words(gitPathArgs(hooksName)): {out: ".git/hooks\n"}},
			run:     func(g *Git) (any, error) { return g.HooksDir(ctx, repoDir) },
			want:    filepath.Join(repoDir, ".git", "hooks"),
		},
		{
			name: "the hook paths, relative",
			replies: map[string]reply{
				words(hookPathsArgs()): {out: repoDir + "\n.git/hooks\n"},
				words(commonDirArgs()): {out: ".git\n"},
			},
			run: func(g *Git) (any, error) { return g.HookPaths(ctx, repoDir) },
			want: HookPaths{
				Toplevel: repoDir, Hooks: filepath.Join(repoDir, ".git", "hooks"), CommonDir: filepath.Join(repoDir, ".git"),
			},
		},
		{
			name: "the hook paths, shared",
			replies: map[string]reply{
				words(hookPathsArgs()): {out: repoDir + "\n/srv/shared hooks/\n"},
				words(commonDirArgs()): {out: "/srv/projects/example-repo/.git\n"},
			},
			run:  func(g *Git) (any, error) { return g.HookPaths(ctx, repoDir) },
			want: HookPaths{Toplevel: repoDir, Hooks: "/srv/shared hooks", CommonDir: filepath.Join(repoDir, ".git")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.run(fakeGit(plainEnv(), &recorder{replies: tt.replies}))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRemoteRefsReadsHeadsAndPeeledTags(t *testing.T) {
	t.Parallel()

	const tagObject = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	out := headSHA + "\tHEAD\n" +
		headSHA + "\trefs/heads/main\n" +
		otherSHA + "\trefs/heads/feature/x\n" +
		tagObject + "\trefs/tags/v1.0.0\n" +
		otherSHA + "\trefs/tags/v1.0.0^{}\n" +
		headSHA + "\trefs/tags/v1.1.0\n" +
		headSHA + "\trefs/tags/release-notes\n" +
		headSHA + "\trefs/tags/orphan^{}\n" +
		"not-an-oid\trefs/heads/broken\n" +
		"no tab here\n" +
		headSHA + "\trefs/pull/7/head\n"

	rec := &recorder{replies: map[string]reply{
		words(remoteRefsArgs()): {out: out},
	}}

	refs, err := fakeGit(plainEnv(), rec).RemoteRefs(t.Context(), repoDir)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{mainBranch: headSHA, "feature/x": otherSHA}, refs.Heads)
	assert.Equal(t, map[string]RemoteTag{
		"v1.0.0":        {Object: tagObject, Commit: otherSHA},
		"v1.1.0":        {Object: headSHA, Commit: headSHA},
		"release-notes": {Object: headSHA, Commit: headSHA},
	}, refs.Tags)

	empty := parseRemoteRefs(nil)
	assert.NotNil(t, empty.Heads)
	assert.NotNil(t, empty.Tags)
}

func TestHelpersReportGitsFailures(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	g := fakeGit(plainEnv(), &recorder{replies: map[string]reply{
		words(currentBranchArgs()):              {code: 128},
		words(ancestorArgs(headSHA, otherSHA)):  {code: 128},
		words(unpushedArgs(mainBranch)):         {out: "many\n"},
		words(boolConfigArgs(keyTagSign)):       {code: 128},
		words(hookPathsArgs()):                  {out: repoDir + "\n"},
		words(gitPathArgs(hooksName)):           {out: "\n"},
		words(verifyArgs("refs/heads/main")):    {out: "not-an-oid\n"},
		words(configArgs("branch.main.remote")): {code: 128},
	}})

	_, err := g.CurrentBranch(ctx, repoDir)
	require.ErrorContains(t, err, "git symbolic-ref: fake refusal")
	assert.Equal(t, 128, execpath.ExitCode(err))

	_, err = g.IsAncestor(ctx, repoDir, headSHA, otherSHA)
	require.Error(t, err, "a commit the clone lacks is an error, not a no")

	_, err = g.Unpushed(ctx, repoDir, mainBranch)
	require.ErrorIs(t, err, errOutput)

	_, err = g.Signing(ctx, repoDir)
	require.Error(t, err)

	_, err = g.HookPaths(ctx, repoDir)
	require.ErrorIs(t, err, errOutput)

	_, err = g.HooksDir(ctx, repoDir)
	require.ErrorIs(t, err, errOutput)

	_, err = g.LocalBranch(ctx, repoDir, mainBranch)
	require.ErrorIs(t, err, errOutput)

	_, err = g.Upstream(ctx, repoDir, mainBranch)
	require.Error(t, err)

	_, err = g.Changes(ctx, repoDir)
	require.ErrorContains(t, err, "git status: fake git knows no such command")
}

func TestInProgress(t *testing.T) {
	t.Parallel()

	for _, f := range operationFiles() {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()

			gitDir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(gitDir, f.name), nil, 0o600))

			got, err := fakeGit(plainEnv(), gitPaths(gitDir)).InProgress(t.Context(), repoDir)
			require.NoError(t, err)
			assert.Equal(t, f.op, got)
		})
	}

	t.Run("nothing", func(t *testing.T) {
		t.Parallel()

		rec := gitPaths(t.TempDir())

		got, err := fakeGit(plainEnv(), rec).InProgress(t.Context(), repoDir)
		require.NoError(t, err)
		assert.Equal(t, OperationNone, got)
		assert.Len(t, rec.Calls(), len(operationFiles()), "each file is looked for")
	})

	t.Run("a rebase stopped on a pick", func(t *testing.T) {
		t.Parallel()

		gitDir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(gitDir, "rebase-merge"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(gitDir, "CHERRY_PICK_HEAD"), nil, 0o600))

		got, err := fakeGit(plainEnv(), gitPaths(gitDir)).InProgress(t.Context(), repoDir)
		require.NoError(t, err)
		assert.Equal(t, OperationRebasing, got)
	})

	t.Run("a relative answer is read against the folder", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "REVERT_HEAD"), nil, 0o600))

		replies := map[string]reply{}
		for _, name := range gitPathNames() {
			replies[words(gitPathArgs(name))] = reply{out: ".git/" + name + "\n"}
		}

		got, err := fakeGit(plainEnv(), &recorder{replies: replies}).InProgress(t.Context(), dir)
		require.NoError(t, err)
		assert.Equal(t, OperationReverting, got)
	})

	t.Run("git fails", func(t *testing.T) {
		t.Parallel()

		_, err := fakeGit(plainEnv(), &recorder{}).InProgress(t.Context(), repoDir)
		require.Error(t, err)
	})
}

// gitPaths is a fake git that resolves every git path into gitDir.
func gitPaths(gitDir string) *recorder {
	replies := map[string]reply{}
	for _, name := range gitPathNames() {
		replies[words(gitPathArgs(name))] = reply{out: filepath.Join(gitDir, name) + "\n"}
	}

	return &recorder{replies: replies}
}

func TestCheckedOutElsewhere(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mainDir, feature, detached := filepath.Join(root, mainBranch), filepath.Join(root, "feature"), filepath.Join(root, "detached")

	for _, dir := range []string{mainDir, feature, detached} {
		require.NoError(t, os.Mkdir(dir, 0o700))
	}

	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(mainDir, link))

	list := "worktree " + mainDir + "\x00HEAD " + headSHA + "\x00branch refs/heads/main\x00\x00" +
		"worktree " + feature + "\x00HEAD " + otherSHA + "\x00branch refs/heads/feature/x\x00locked\x00\x00" +
		"worktree " + detached + "\x00HEAD " + headSHA + "\x00detached\x00\x00" +
		"worktree " + filepath.Join(root, "gone") + "\x00HEAD " + headSHA + "\x00branch refs/heads/main\x00prunable gitdir file points to non-existent location\x00\x00"

	g := fakeGit(plainEnv(), &recorder{replies: map[string]reply{words(worktreesArgs()): {out: list}}})

	for _, tt := range []struct {
		dir, branch string
		want        []string
	}{
		{dir: mainDir, branch: mainBranch, want: []string{filepath.Join(root, "gone")}},
		{dir: link, branch: mainBranch, want: []string{filepath.Join(root, "gone")}},
		{dir: mainDir, branch: "feature/x", want: []string{feature}},
		{dir: feature, branch: mainBranch, want: []string{mainDir, filepath.Join(root, "gone")}},
		{dir: mainDir, branch: "other", want: []string{}},
	} {
		got, err := g.CheckedOutElsewhere(t.Context(), tt.dir, tt.branch)
		require.NoError(t, err)
		assert.Equal(t, tt.want, got, "%s from %s", tt.branch, tt.dir)
	}
}

func TestEveryAllowlistEntryIsUsed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	rec := &recorder{
		replies: map[string]reply{words(hookPathsArgs()): {out: repoDir + "\n.git/hooks\n"}},
		answer:  func(context.Context, []string) (string, error) { return "", exitError{code: 1} },
	}
	g := fakeGit(plainEnv(), rec)

	// Each helper, answered "no", runs each command it needs at least once; the errors
	// that a no gives some of them don't matter here.
	_, _ = g.CurrentBranch(ctx, repoDir)
	_, _ = g.OriginHead(ctx, repoDir)
	_, _ = g.InProgress(ctx, repoDir)
	_, _ = g.Changes(ctx, repoDir)
	_, _ = g.Entries(ctx, repoDir)
	_, _ = g.CheckedOutElsewhere(ctx, repoDir, mainBranch)
	_, _ = g.LocalBranch(ctx, repoDir, mainBranch)
	_, _ = g.TrackingBranch(ctx, repoDir, mainBranch)
	_, _ = g.HasCommit(ctx, repoDir, headSHA)
	_, _ = g.Upstream(ctx, repoDir, mainBranch)
	_, _ = g.Unpushed(ctx, repoDir, mainBranch)
	_, _ = g.CommitsBetween(ctx, repoDir, headSHA, otherSHA)
	_, _ = g.IsAncestor(ctx, repoDir, headSHA, otherSHA)
	_, _ = g.LocalVersionTags(ctx, repoDir)
	_, _ = g.RemoteRefs(ctx, repoDir)
	_, _ = g.RemoteVersionTags(ctx, repoDir)
	_, _ = g.RemoteTagCommit(ctx, repoDir, "v1.4.2")
	_, _ = g.LocalTagCommit(ctx, repoDir, "v1.4.2")
	_, _ = g.Signing(ctx, repoDir)
	_, _ = g.HooksDir(ctx, repoDir)
	_, _ = g.HookPaths(ctx, repoDir)

	used := map[string]bool{}

	for _, args := range rec.Args() {
		matched := false

		for _, c := range allowlist() {
			if c.matches(args) {
				used[c.name], matched = true, true
			}
		}

		assert.True(t, matched, "%q ran outside the allowlist", args)
	}

	for _, c := range allowlist() {
		assert.True(t, used[c.name], "no helper runs %s", c.name)
	}
}
