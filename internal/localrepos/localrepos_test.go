package localrepos

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/execpath"
)

const (
	exampleOwner    = "acme-org"
	exampleName     = "example-repo"
	exampleHTTPS    = "https://github.com/acme-org/example-repo.git"
	exampleRepoSlug = "acme-org/example-repo"
)

var errFakeLaunch = errors.New("fake: cannot start git")

// isolateGit keeps the user's and the system's git configuration out of the test.
func isolateGit(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Octo User")
	t.Setenv("GIT_AUTHOR_EMAIL", "octo-user@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Octo User")
	t.Setenv("GIT_COMMITTER_EMAIL", "octo-user@example.com")
}

// git runs a git command in the test and fails on error.
func git(t *testing.T, args ...string) {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "git", args...).CombinedOutput() //nolint:gosec // test fixture setup with fixed arguments
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
}

// initRepo creates a git repository at dir with the given remotes (name, url pairs).
func initRepo(t *testing.T, dir string, remotes ...string) {
	t.Helper()

	git(t, "-c", "init.defaultBranch=main", "init", "-q", dir)

	for i := 0; i+1 < len(remotes); i += 2 {
		git(t, "-C", dir, "remote", "add", remotes[i], remotes[i+1])
	}
}

// realGit runs the real git binary.
func realGit() Runner {
	return execpath.Runner{}
}

// exitError is a fake process exit with a status.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

func (e exitError) ExitCode() int { return e.code }

// recordingGit records every call and answers with fixed output or error.
type recordingGit struct {
	mu     sync.Mutex
	calls  [][]string
	stdout string
	err    error
}

func (g *recordingGit) Run(_ context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.calls = append(g.calls, append([]string{name}, args...))

	return []byte(g.stdout), nil, g.err
}

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		raw   string
		owner string
		name  string
		ok    bool
	}{
		{raw: exampleHTTPS, owner: exampleOwner, name: exampleName, ok: true},
		{raw: "https://github.com/acme-org/example-repo", owner: exampleOwner, name: exampleName, ok: true},
		{raw: "https://github.com/acme-org/example-repo/", owner: exampleOwner, name: exampleName, ok: true},
		{raw: "https://github.com/acme-org/example-repo.git/", owner: exampleOwner, name: exampleName, ok: true},
		{raw: "  https://github.com/acme-org/example-repo.git\n", owner: exampleOwner, name: exampleName, ok: true},
		{raw: "HTTPS://GitHub.COM/Acme-Org/Example-Repo.GIT", owner: exampleOwner, name: exampleName, ok: true},
		{raw: "https://octo-user@github.com/acme-org/example-repo.git", owner: exampleOwner, name: exampleName, ok: true},
		{raw: "http://github.com/acme-org/example-repo", owner: exampleOwner, name: exampleName, ok: true},
		{raw: "https://www.github.com/acme-org/example-repo", owner: exampleOwner, name: exampleName, ok: true},
		{raw: "ssh://git@github.com/acme-org/example-repo.git", owner: exampleOwner, name: exampleName, ok: true},
		{raw: "ssh://git@ssh.github.com:443/acme-org/example-repo.git", owner: exampleOwner, name: exampleName, ok: true},
		{raw: "git://github.com/acme-org/example-repo.git", owner: exampleOwner, name: exampleName, ok: true},
		{raw: "git@github.com:acme-org/example-repo.git", owner: exampleOwner, name: exampleName, ok: true},
		{raw: "git@github.com:acme-org/example-repo", owner: exampleOwner, name: exampleName, ok: true},
		{raw: "git@github.com:/acme-org/example-repo.git/", owner: exampleOwner, name: exampleName, ok: true},
		{raw: "github.com:Acme-Org/Example.Repo_2.git", owner: "acme-org", name: "example.repo_2", ok: true},
		{raw: "https://gitlab.com/acme-org/example-repo.git"},
		{raw: "git@gitlab.com:acme-org/example-repo.git"},
		{raw: "https://github.com/acme-org"},
		{raw: "https://github.com/acme-org/example-repo/tree/main"},
		{raw: "https://github.com//example-repo"},
		{raw: "https://github.com/acme-org/.git"},
		{raw: "https://github.com/acme-org/.."},
		{raw: "https://github.com/acme org/example-repo"},
		{raw: "file:///srv/git/example-repo.git"},
		{raw: "/srv/git/example-repo.git"},
		{raw: "../example-repo"},
		{raw: "https://github.com:bad-port/acme-org/example-repo"},
		{raw: ""},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			owner, name, ok := ParseRemoteURL(tt.raw)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.owner, owner)
			assert.Equal(t, tt.name, name)
		})
	}
}

func TestParseRemotes(t *testing.T) {
	out := strings.Join([]string{
		"remote.origin.url " + exampleHTTPS,
		"remote.origin.url https://github.com/acme-org/second-url.git",
		"remote.my.fork.url git@github.com:octo-user/example-repo.git",
		"remote.Upstream.url https://github.com/Acme-Org/Example-Repo\r",
		"remote.mirror.url https://gitlab.com/acme-org/example-repo.git",
		"remote.origin.pushurl https://github.com/acme-org/push-only.git",
		"remote..url https://github.com/acme-org/nameless.git",
		"no-value-line",
		"",
	}, "\n")

	assert.Equal(t, map[string]string{
		"origin":   exampleRepoSlug,
		"my.fork":  "octo-user/example-repo",
		"Upstream": exampleRepoSlug,
	}, parseRemotes(out))
}

func TestScanFindsReposOneLevelDeep(t *testing.T) {
	isolateGit(t)

	root := t.TempDir()
	initRepo(t, filepath.Join(root, "alpha"), "origin", exampleHTTPS, "upstream", "git@github.com:octo-user/example-repo.git")
	initRepo(t, filepath.Join(root, "beta"))
	initRepo(t, filepath.Join(root, ".hidden"), "origin", exampleHTTPS)
	initRepo(t, filepath.Join(root, "group", "nested"), "origin", exampleHTTPS)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "plain-dir"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o600))

	outside := filepath.Join(t.TempDir(), "linked-target")
	initRepo(t, outside, "origin", "https://github.com/acme-org/linked-repo")
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "linked")))
	require.NoError(t, os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "dangling")))

	repos, err := Scan(t.Context(), root, realGit())
	require.NoError(t, err)

	assert.Equal(t, []LocalRepo{
		{Path: filepath.Join(root, "alpha"), Remotes: map[string]string{
			"origin":   exampleRepoSlug,
			"upstream": "octo-user/example-repo",
		}},
		{Path: filepath.Join(root, "beta"), Remotes: map[string]string{}},
		{Path: filepath.Join(root, "linked"), Remotes: map[string]string{"origin": "acme-org/linked-repo"}},
	}, repos)
}

func TestScanRunsReadOnlyGitArgv(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "example-repo")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o700))

	fake := &recordingGit{stdout: "remote.origin.url " + exampleHTTPS + "\n"}

	repos, err := Scan(t.Context(), root, fake)
	require.NoError(t, err)
	require.Len(t, repos, 1)
	assert.Equal(t, map[string]string{"origin": exampleRepoSlug}, repos[0].Remotes)
	assert.Equal(t, [][]string{{"git", "-C", repo, "config", "--get-regexp", `^remote\..*\.url$`}}, fake.calls)
}

func TestScanToleratesGitExitStatus(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "example-repo", ".git"), 0o700))

	for _, code := range []int{1, 128} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			repos, err := Scan(t.Context(), root, &recordingGit{err: exitError{code: code}})
			require.NoError(t, err)
			require.Len(t, repos, 1)
			assert.Empty(t, repos[0].Remotes)
		})
	}
}

func TestScanFailsWhenGitCannotRun(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "example-repo", ".git"), 0o700))

	_, err := Scan(t.Context(), root, &recordingGit{err: errFakeLaunch})
	require.ErrorIs(t, err, errFakeLaunch)

	_, err = Scan(t.Context(), root, &recordingGit{err: exitError{code: -1}})
	require.Error(t, err)
}

func TestScanMissingRoot(t *testing.T) {
	_, err := Scan(t.Context(), filepath.Join(t.TempDir(), "missing"), &recordingGit{})
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestScanEmptyRoot(t *testing.T) {
	repos, err := Scan(t.Context(), t.TempDir(), &recordingGit{})
	require.NoError(t, err)
	assert.Empty(t, repos)
}

func TestResolvePrefersOrigin(t *testing.T) {
	isolateGit(t)

	home := t.TempDir()
	root := filepath.Join(home, "projects")
	initRepo(t, filepath.Join(root, "a-fork"), "origin", "git@github.com:octo-user/example-repo.git", "upstream", exampleHTTPS)
	initRepo(t, filepath.Join(root, "z-main"), "origin", exampleHTTPS)

	r := Resolver{Root: "~/projects", Home: home, Git: realGit()}

	got, err := r.Resolve(t.Context(), "Acme-Org", "Example-Repo")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "z-main"), got.Repo.Path)
	assert.Equal(t, "origin", got.MatchedRemote)
	assert.True(t, got.OriginMatched())

	fork, err := r.Resolve(t.Context(), "octo-user", "example-repo")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "a-fork"), fork.Repo.Path)
	assert.True(t, fork.OriginMatched())
}

func TestResolveFallsBackToOtherRemote(t *testing.T) {
	isolateGit(t)

	home := t.TempDir()
	root := filepath.Join(home, "projects")
	initRepo(t, filepath.Join(root, "b-fork"), "origin", "git@github.com:octo-user/example-repo.git", "upstream", exampleHTTPS)
	initRepo(t, filepath.Join(root, "c-fork"), "origin", "git@github.com:octo-user/example-repo.git", "acme", exampleHTTPS)

	got, err := Resolver{Root: root, Home: home, Git: realGit()}.Resolve(t.Context(), exampleOwner, exampleName)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "b-fork"), got.Repo.Path)
	assert.Equal(t, "upstream", got.MatchedRemote)
	assert.False(t, got.OriginMatched())
}

func TestResolveUnknownRepoError(t *testing.T) {
	isolateGit(t)

	home := t.TempDir()
	initRepo(t, filepath.Join(home, "projects", "example-repo"), "origin", exampleHTTPS)

	_, err := Resolver{Root: "~/projects", Home: home, Git: realGit()}.Resolve(t.Context(), "acme-org", "missing-repo")
	require.ErrorIs(t, err, ErrCloneNotFound)
	assert.Equal(t, "no local clone of acme-org/missing-repo under ~/projects", err.Error())
}

func TestResolveRootErrors(t *testing.T) {
	_, err := Resolver{Root: "", Git: &recordingGit{}}.Resolve(t.Context(), exampleOwner, exampleName)
	require.ErrorIs(t, err, ErrEmptyRoot)

	_, err = Resolver{Root: filepath.Join(t.TempDir(), "missing"), Git: &recordingGit{}}.Resolve(t.Context(), exampleOwner, exampleName)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestRemoteFor(t *testing.T) {
	repo := LocalRepo{Remotes: map[string]string{
		"zeta":   exampleRepoSlug,
		"beta":   exampleRepoSlug,
		"origin": "octo-user/example-repo",
	}}

	remote, ok := repo.RemoteFor("ACME-ORG", "example-repo")
	assert.True(t, ok)
	assert.Equal(t, "beta", remote)

	remote, ok = repo.RemoteFor("octo-user", "example-repo")
	assert.True(t, ok)
	assert.Equal(t, "origin", remote)

	_, ok = repo.RemoteFor("acme-org", "other-repo")
	assert.False(t, ok)
}

func TestInspect(t *testing.T) {
	isolateGit(t)

	dir := filepath.Join(t.TempDir(), "example-repo")
	initRepo(t, dir, "origin", exampleHTTPS)

	got, err := Inspect(t.Context(), dir, realGit())
	require.NoError(t, err)
	assert.Equal(t, LocalRepo{Path: dir, Remotes: map[string]string{"origin": exampleRepoSlug}}, got)

	_, err = Inspect(t.Context(), t.TempDir(), realGit())
	require.ErrorIs(t, err, ErrNotARepo)

	_, err = Inspect(t.Context(), dir, &recordingGit{err: errFakeLaunch})
	require.ErrorIs(t, err, errFakeLaunch)
}

func TestExpandRoot(t *testing.T) {
	home := filepath.Join(string(filepath.Separator)+"home", "octo-user")
	abs := filepath.Join(string(filepath.Separator)+"srv", "clones")

	tests := []struct {
		name    string
		root    string
		home    string
		want    string
		wantErr error
	}{
		{name: "tilde path", root: "~/projects", home: home, want: filepath.Join(home, "projects")},
		{name: "tilde path with trailing slash", root: " ~/projects/ ", home: home, want: filepath.Join(home, "projects")},
		{name: "tilde alone", root: "~", home: home, want: home},
		{name: "absolute", root: abs + string(filepath.Separator), home: home, want: abs},
		{name: "absolute without home", root: abs, want: abs},
		{name: "empty", root: "  ", home: home, wantErr: ErrEmptyRoot},
		{name: "tilde without home", root: "~/projects", wantErr: ErrNoHome},
		{name: "relative", root: "projects", home: home, wantErr: ErrRelativeRoot},
		{name: "other user's tilde", root: "~octo-user/projects", home: home, wantErr: ErrRelativeRoot},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpandRoot(tt.root, tt.home)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTildePath(t *testing.T) {
	home := filepath.Join(string(filepath.Separator)+"home", "octo-user")
	other := filepath.Join(string(filepath.Separator)+"srv", "clones")

	assert.Equal(t, "~/projects/example-repo", TildePath(filepath.Join(home, "projects", "example-repo"), home))
	assert.Equal(t, "~", TildePath(home, home))
	assert.Equal(t, other, TildePath(other, home))
	assert.Equal(t, home+"-other", TildePath(home+"-other", home))
	assert.Equal(t, other, TildePath(other, ""))
}
