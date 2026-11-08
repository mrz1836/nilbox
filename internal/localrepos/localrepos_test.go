package localrepos

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

const (
	exampleOwner = "acme-org"
	exampleName  = "example-repo"
	exampleHTTPS = "https://github.com/acme-org/example-repo.git"
)

// exampleRepo is the repository the example remotes point at.
func exampleRepo() repo.Name {
	return repo.Name{Owner: exampleOwner, Repo: exampleName}
}

var errFakeLaunch = errors.New("fake: cannot start git")

// isolatedGit returns a runner for real git that reads none of the user's or the
// system's configuration, which could list remotes of its own. It isolates git through
// the runner's environment, not the process's, so the test can run in parallel.
func isolatedGit(t *testing.T) execpath.Runner {
	t.Helper()

	return execpath.Exec{Env: testutil.Git(t)}
}

// initRepo creates a git repository at dir with the given remotes (name, url pairs).
func initRepo(t *testing.T, dir string, remotes ...string) {
	t.Helper()

	testutil.InitRepo(t, dir)

	for i := 0; i+1 < len(remotes); i += 2 {
		testutil.RunGit(t, dir, "remote", "add", remotes[i], remotes[i+1])
	}
}

// exitError is a fake process exit with a status.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

func (e exitError) ExitCode() int { return e.code }

func TestParseRemoteURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw  string
		want repo.Name
		ok   bool
	}{
		{raw: exampleHTTPS, want: exampleRepo(), ok: true},
		{raw: "https://github.com/acme-org/example-repo", want: exampleRepo(), ok: true},
		{raw: "https://github.com/acme-org/example-repo/", want: exampleRepo(), ok: true},
		{raw: "https://github.com/acme-org/example-repo.git/", want: exampleRepo(), ok: true},
		{raw: "  https://github.com/acme-org/example-repo.git\n", want: exampleRepo(), ok: true},
		{raw: "HTTPS://GitHub.COM/Acme-Org/Example-Repo.GIT", want: repo.Name{Owner: "Acme-Org", Repo: "Example-Repo"}, ok: true},
		{raw: "https://octo-user@github.com/acme-org/example-repo.git", want: exampleRepo(), ok: true},
		{raw: "http://github.com/acme-org/example-repo", want: exampleRepo(), ok: true},
		{raw: "https://www.github.com/acme-org/example-repo", want: exampleRepo(), ok: true},
		{raw: "ssh://git@github.com/acme-org/example-repo.git", want: exampleRepo(), ok: true},
		{raw: "ssh://git@ssh.github.com:443/acme-org/example-repo.git", want: exampleRepo(), ok: true},
		{raw: "git://github.com/acme-org/example-repo.git", want: exampleRepo(), ok: true},
		{raw: "git@github.com:acme-org/example-repo.git", want: exampleRepo(), ok: true},
		{raw: "git@github.com:acme-org/example-repo", want: exampleRepo(), ok: true},
		{raw: "git@github.com:/acme-org/example-repo.git/", want: exampleRepo(), ok: true},
		{raw: "github.com:Acme-Org/Example.Repo_2.git", want: repo.Name{Owner: "Acme-Org", Repo: "Example.Repo_2"}, ok: true},
		{raw: "https://github.com/octo_acme/example-repo", want: repo.Name{Owner: "octo_acme", Repo: "example-repo"}, ok: true},
		{raw: "https://github.com/acme.org/example-repo"},
		{raw: "https://github.com/-acme/example-repo"},
		{raw: "https://github.com/acme-org/-example-repo"},
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
			t.Parallel()

			got, ok := parseRemoteURL(tt.raw)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseRemotes(t *testing.T) {
	t.Parallel()

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

	assert.Equal(t, map[string]repo.Name{
		"origin":   exampleRepo(),
		"my.fork":  {Owner: "octo-user", Repo: exampleName},
		"Upstream": {Owner: "Acme-Org", Repo: "Example-Repo"},
	}, parseRemotes(out))
}

func TestScanFindsReposOneLevelDeep(t *testing.T) {
	t.Parallel()

	git := isolatedGit(t)

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

	repos, err := Scan(t.Context(), root, git)
	require.NoError(t, err)

	assert.Equal(t, []LocalRepo{
		{Path: filepath.Join(root, "alpha"), Remotes: map[string]repo.Name{
			"origin":   exampleRepo(),
			"upstream": {Owner: "octo-user", Repo: exampleName},
		}},
		{Path: filepath.Join(root, "beta"), Remotes: map[string]repo.Name{}},
		{Path: filepath.Join(root, "linked"), Remotes: map[string]repo.Name{"origin": {Owner: exampleOwner, Repo: "linked-repo"}}},
	}, repos)
}

func TestScanRunsReadOnlyGitArgv(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "example-repo")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o700))

	fake := &testutil.Runner{Stdout: "remote.origin.url " + exampleHTTPS + "\n"}

	repos, err := Scan(t.Context(), root, fake)
	require.NoError(t, err)
	require.Len(t, repos, 1)
	assert.Equal(t, map[string]repo.Name{"origin": exampleRepo()}, repos[0].Remotes)
	assert.Equal(t, [][]string{{"git", "-C", dir, "config", "--get-regexp", `^remote\..*\.url$`}}, fake.Calls())
}

func TestScanToleratesGitExitStatus(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "example-repo", ".git"), 0o700))

	for _, code := range []int{1, 128} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			t.Parallel()

			repos, err := Scan(t.Context(), root, &testutil.Runner{Err: exitError{code: code}})
			require.NoError(t, err)
			require.Len(t, repos, 1)
			assert.Empty(t, repos[0].Remotes)
		})
	}
}

func TestScanFailsWhenGitCannotRun(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "example-repo", ".git"), 0o700))

	_, err := Scan(t.Context(), root, &testutil.Runner{Err: errFakeLaunch})
	require.ErrorIs(t, err, errFakeLaunch)

	_, err = Scan(t.Context(), root, &testutil.Runner{Err: exitError{code: -1}})
	require.Error(t, err)
}

func TestScanMissingRoot(t *testing.T) {
	t.Parallel()

	_, err := Scan(t.Context(), filepath.Join(t.TempDir(), "missing"), &testutil.Runner{})
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestScanEmptyRoot(t *testing.T) {
	t.Parallel()

	repos, err := Scan(t.Context(), t.TempDir(), &testutil.Runner{})
	require.NoError(t, err)
	assert.Empty(t, repos)
}

func TestResolvePrefersOrigin(t *testing.T) {
	t.Parallel()

	git := isolatedGit(t)

	home := t.TempDir()
	root := filepath.Join(home, "projects")
	initRepo(t, filepath.Join(root, "a-fork"), "origin", "git@github.com:octo-user/example-repo.git", "upstream", exampleHTTPS)
	initRepo(t, filepath.Join(root, "z-main"), "origin", exampleHTTPS)

	r := Resolver{Root: "~/projects", Home: home, Git: git}

	got, err := r.Resolve(t.Context(), repo.Name{Owner: "Acme-Org", Repo: "Example-Repo"})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "z-main"), got.Repo.Path)
	assert.Equal(t, "origin", got.MatchedRemote)
	assert.True(t, got.OriginMatched())

	fork, err := r.Resolve(t.Context(), repo.Name{Owner: "octo-user", Repo: exampleName})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "a-fork"), fork.Repo.Path)
	assert.True(t, fork.OriginMatched())
}

func TestResolveFallsBackToOtherRemote(t *testing.T) {
	t.Parallel()

	git := isolatedGit(t)

	home := t.TempDir()
	root := filepath.Join(home, "projects")
	initRepo(t, filepath.Join(root, "b-fork"), "origin", "git@github.com:octo-user/example-repo.git", "upstream", exampleHTTPS)
	initRepo(t, filepath.Join(root, "c-fork"), "origin", "git@github.com:octo-user/example-repo.git", "acme", exampleHTTPS)

	got, err := Resolver{Root: root, Home: home, Git: git}.Resolve(t.Context(), exampleRepo())
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "b-fork"), got.Repo.Path)
	assert.Equal(t, "upstream", got.MatchedRemote)
	assert.False(t, got.OriginMatched())
}

func TestResolveUnknownRepoError(t *testing.T) {
	t.Parallel()

	git := isolatedGit(t)

	home := t.TempDir()
	initRepo(t, filepath.Join(home, "projects", "example-repo"), "origin", exampleHTTPS)

	_, err := Resolver{Root: "~/projects", Home: home, Git: git}.Resolve(t.Context(), repo.Name{Owner: exampleOwner, Repo: "missing-repo"})
	require.ErrorIs(t, err, ErrCloneNotFound)
	assert.Equal(t, "no local clone of acme-org/missing-repo under ~/projects", err.Error())
}

func TestResolveRootErrors(t *testing.T) {
	t.Parallel()

	_, err := Resolver{Root: "", Git: &testutil.Runner{}}.Resolve(t.Context(), exampleRepo())
	require.ErrorIs(t, err, paths.ErrEmpty)

	_, err = Resolver{Root: filepath.Join(t.TempDir(), "missing"), Git: &testutil.Runner{}}.Resolve(t.Context(), exampleRepo())
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestRemoteFor(t *testing.T) {
	t.Parallel()

	clone := LocalRepo{Remotes: map[string]repo.Name{
		"zeta":   exampleRepo(),
		"beta":   exampleRepo(),
		"origin": {Owner: "octo-user", Repo: exampleName},
	}}

	remote, ok := clone.RemoteFor(repo.Name{Owner: "ACME-ORG", Repo: exampleName})
	assert.True(t, ok)
	assert.Equal(t, "beta", remote)

	remote, ok = clone.RemoteFor(repo.Name{Owner: "octo-user", Repo: exampleName})
	assert.True(t, ok)
	assert.Equal(t, "origin", remote)

	_, ok = clone.RemoteFor(repo.Name{Owner: exampleOwner, Repo: "other-repo"})
	assert.False(t, ok)
}

func TestInspect(t *testing.T) {
	t.Parallel()

	git := isolatedGit(t)

	dir := filepath.Join(t.TempDir(), "example-repo")
	initRepo(t, dir, "origin", exampleHTTPS)

	got, err := Inspect(t.Context(), dir, git)
	require.NoError(t, err)
	assert.Equal(t, LocalRepo{Path: dir, Remotes: map[string]repo.Name{"origin": exampleRepo()}}, got)

	_, err = Inspect(t.Context(), t.TempDir(), git)
	require.ErrorIs(t, err, ErrNotARepo)

	_, err = Inspect(t.Context(), dir, &testutil.Runner{Err: errFakeLaunch})
	require.ErrorIs(t, err, errFakeLaunch)
}

func TestClonesListsReposAndStopsWhenAsked(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, name := range []string{"alpha", "beta", ".hidden"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, name, ".git"), 0o700))
	}

	require.NoError(t, os.MkdirAll(filepath.Join(root, "plain-dir"), 0o700))

	clones, err := Clones(root)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(root, "alpha"), filepath.Join(root, "beta")}, slices.Collect(clones))

	for dir := range clones {
		assert.Equal(t, filepath.Join(root, "alpha"), dir)

		break
	}

	_, err = Clones(filepath.Join(root, "missing"))
	require.ErrorIs(t, err, fs.ErrNotExist)
}
