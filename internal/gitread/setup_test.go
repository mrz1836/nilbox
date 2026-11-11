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

// setupClone is a clone of a local bare origin reached over file://, whose own config names
// an ssh command, a password program, a credential helper, a cookie file, and a filter
// driver, with git isolated from the user's configuration but for a global config of the
// user's own that names Git LFS's driver; env is git's environment.
func setupClone(t *testing.T) (dir string, env []string) {
	t.Helper()

	env = testutil.Git(t)
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	dir = filepath.Join(root, "clone")
	global := filepath.Join(root, "global.gitconfig")

	require.NoError(t, os.WriteFile(global, []byte("[filter \"lfs\"]\n\tprocess = git-lfs filter-process\n"), 0o600))
	testutil.RunGit(t, root, "-c", "init.defaultBranch=main", "init", "-q", "--bare", origin)
	testutil.InitRepo(t, dir)
	testutil.RunGit(t, dir, "remote", "add", "origin", "file://"+filepath.ToSlash(origin))

	for _, kv := range [][2]string{
		{"core.sshCommand", "evil-ssh"},
		{"core.askPass", "evil-askpass"},
		{"credential.helper", "!evil-helper"},
		{"http.cookieFile", filepath.Join(root, "cookies")},
		{"filter.evil.smudge", "evil-smudge"},
		{"filter.evil.process", "evil-process"},
	} {
		testutil.RunGit(t, dir, "config", kv[0], kv[1])
	}

	return dir, append(env, "GIT_CONFIG_GLOBAL="+global)
}

func TestRemoteSetupHoldsWhatAnLsRemoteHolds(t *testing.T) {
	t.Parallel()

	dir, env := setupClone(t)

	setup, err := New(Config{Env: func() []string { return env }}).RemoteSetup(t.Context(), dir)
	require.NoError(t, err)

	assert.True(t, filepath.IsAbs(setup.Git), "git by its absolute path")
	assert.Equal(t, Hardening(), setup.Settings[:len(Hardening())], "no hook, file system monitor, or submodule")

	settings := strings.Join(setup.Settings, " ")
	for _, want := range []string{
		"-c core.askpass=", "-c credential.helper=", "-c http.savecookies=false", "-c http.cookiefile=",
		"-c filter.evil.clean= -c filter.evil.smudge= -c filter.evil.process= -c filter.evil.required=false",
	} {
		assert.Contains(t, settings, want)
	}

	assert.NotContains(t, settings, "filter.lfs", "the user's own driver still runs")
	assert.NotContains(t, settings, "evil-", "nothing the clone names is handed on")

	for _, want := range []string{
		"GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_ALLOW_PROTOCOL=file:http:https:ssh",
		"GIT_SSH_COMMAND=" + batchSSH,
	} {
		assert.Contains(t, setup.Env, want)
	}

	assert.Equal(t, []string{SetAsideSSHCommand, SetAsideAskPass, SetAsideHelpers}, setup.SetAside)
	assert.Equal(t, "This clone's own core.sshCommand, core.askPass, and credential.helper aren't used to reach "+
		"origin, which may be why: set them for this folder in your global git config, with an includeIf.",
		SetAsideNote(setup.SetAside))
	assert.Empty(t, SetAsideNote(nil))
}

func TestRemoteSetupRefusesWhatAnLsRemoteRefuses(t *testing.T) {
	t.Parallel()

	dir, env := setupClone(t)
	g := New(Config{Env: func() []string { return env }})

	testutil.RunGit(t, dir, "remote", "set-url", "origin", "ext::sh -c touch% /nonexistent/pwned")

	_, err := g.RemoteSetup(t.Context(), dir)
	require.ErrorIs(t, err, ErrRemoteHelper)

	testutil.RunGit(t, dir, "remote", "set-url", "origin", "https://example.invalid/acme-org/example-repo.git")
	testutil.RunGit(t, dir, "config", "http.proxy", "http://203.0.113.9:8080")

	_, err = g.RemoteSetup(t.Context(), dir)
	require.ErrorIs(t, err, ErrCloneRoutes)

	_, err = g.RemoteSetup(t.Context(), "relative")
	require.ErrorIs(t, err, ErrGitCommandNotAllowed)
}

func TestHardening(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		"-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-c", "submodule.recurse=false",
	}, Hardening())
}

func TestValidBranch(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"main", "feature/x", "release-1.4"} {
		assert.True(t, ValidBranch(name), name)
	}

	for _, name := range []string{"", "-x", "a..b", "a:b", "a*", "a^", "a b", "x.lock", "a~1", "a?", "a[b", `a\b`} {
		assert.False(t, ValidBranch(name), name)
	}
}

func TestRemoteSetupForWeighsTheRemoteNamed(t *testing.T) {
	t.Parallel()

	dir, env := setupClone(t)
	g := New(Config{Env: func() []string { return env }})

	testutil.RunGit(t, dir, "remote", "add", "upstream", "ext::sh -c touch% /nonexistent/pwned")

	_, err := g.RemoteSetup(t.Context(), dir)
	require.NoError(t, err, "origin is fine")

	_, err = g.RemoteSetupFor(t.Context(), dir, "upstream")
	require.ErrorIs(t, err, ErrRemoteHelper, "upstream's own URL is weighed")

	testutil.RunGit(t, dir, "remote", "set-url", "upstream", "https://example.invalid/acme-org/example-repo.git")
	testutil.RunGit(t, dir, "config", "remote.upstream.proxy", "http://203.0.113.9:8080")

	_, err = g.RemoteSetupFor(t.Context(), dir, "upstream")
	require.ErrorIs(t, err, ErrCloneRoutes, "and its own proxy")

	_, err = g.RemoteSetupFor(t.Context(), dir, "-x")
	require.ErrorIs(t, err, ErrGitCommandNotAllowed)
}

func TestCheckoutSetupReachesNoRemote(t *testing.T) {
	t.Parallel()

	dir, env := setupClone(t)

	setup, err := New(Config{Env: func() []string { return env }}).CheckoutSetup(t.Context(), dir)
	require.NoError(t, err)

	assert.True(t, filepath.IsAbs(setup.Git))
	assert.Equal(t, append(Hardening(), "-c", "filter.evil.clean=", "-c", "filter.evil.smudge=", "-c",
		"filter.evil.process=", "-c", "filter.evil.required=false"), setup.Settings)
	assert.Contains(t, setup.Env, "GIT_ALLOW_PROTOCOL=", "no transport at all")
	assert.Contains(t, setup.Env, "GIT_NO_LAZY_FETCH=1")
	assert.Empty(t, setup.SetAside)
}
