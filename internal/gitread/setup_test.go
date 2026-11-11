package gitread

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// on is git's word for a switch turned on.
const on = "true"

// setupClone is a clone of a local bare origin reached over file://, with git isolated
// from the user's configuration but for a global config of the user's own, at global, that
// names Git LFS's driver; env is git's environment.
func setupClone(t *testing.T) (dir, global string, env []string) {
	t.Helper()

	env = testutil.Git(t)
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	dir = filepath.Join(root, "clone")
	global = filepath.Join(root, "global.gitconfig")

	require.NoError(t, os.WriteFile(global, []byte("[filter \"lfs\"]\n\tprocess = git-lfs filter-process\n"), 0o600))
	testutil.RunGit(t, root, "-c", "init.defaultBranch=main", "init", "-q", "--bare", origin)
	testutil.InitRepo(t, dir)
	testutil.RunGit(t, dir, "remote", "add", "origin", "file://"+filepath.ToSlash(origin))

	return dir, global, append(env, "GIT_CONFIG_GLOBAL="+global)
}

// configOf returns the settings env passes git through GIT_CONFIG_COUNT, key=value each.
func configOf(t *testing.T, env []string) []string {
	t.Helper()

	count := lookupVar(env, configCountVar)
	require.NotNil(t, count, "the settings are passed through the environment")

	n, err := strconv.Atoi(*count)
	require.NoError(t, err)

	out := make([]string, 0, n)

	for i := range n {
		key, value := lookupVar(env, configKeyVar+strconv.Itoa(i)), lookupVar(env, configValueVar+strconv.Itoa(i))
		require.NotNil(t, key)
		require.NotNil(t, value)

		out = append(out, *key+"="+*value)
	}

	return out
}

// realDir is dir with every link resolved, as git names it.
func realDir(t *testing.T, dir string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	return resolved
}

func TestRemoteSetupHoldsWhatAnLsRemoteHolds(t *testing.T) {
	t.Parallel()

	dir, _, env := setupClone(t)

	setup, err := New(Config{Env: func() []string { return env }}).RemoteSetup(t.Context(), dir)
	require.NoError(t, err)

	assert.True(t, filepath.IsAbs(setup.Git), "git by its absolute path")

	hardening := unflagged(Hardening())
	assert.Equal(t, hardening, setup.Settings[:len(hardening)], "no hook, file system monitor, or submodule")
	assert.Equal(t, unflagged(fetchHardening()), setup.Settings[len(hardening):len(hardening)+len(unflagged(fetchHardening()))],
		"no alternate refs command, and no automatic maintenance")
	assert.Contains(t, setup.Settings, "http.savecookies=false")
	assert.NotContains(t, strings.Join(setup.Settings, " "), "filter.", "the user's own driver still runs")
	assert.Equal(t, setup.Settings, configOf(t, setup.Env), "every setting is passed through the environment")

	for _, want := range []string{
		"GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_ALLOW_PROTOCOL=file:http:https:ssh",
		"GIT_SSH_COMMAND=" + batchSSH, "GIT_DIR=" + filepath.Join(realDir(t, dir), ".git"), "GIT_WORK_TREE=" + realDir(t, dir),
	} {
		assert.Contains(t, gitVars(setup.Env), want)
	}

	assert.False(t, setup.Partial)
}

func TestTheSetupsSettingsAreTheOnesGitReads(t *testing.T) {
	t.Parallel()

	dir, _, env := setupClone(t)

	setup, err := New(Config{Env: func() []string { return env }}).RemoteSetup(t.Context(), dir)
	require.NoError(t, err)

	for key, want := range map[string]string{
		"core.hooksPath": os.DevNull, "core.alternateRefsCommand": on, "maintenance.auto": "false", "gc.auto": "0",
	} {
		cmd := exec.CommandContext(t.Context(), setup.Git, "config", "--show-scope", "--get", key) //nolint:gosec // test: git's absolute path and fixed words
		cmd.Dir, cmd.Env = t.TempDir(), setup.Env

		out, err := cmd.Output()
		require.NoError(t, err, key)
		assert.Equal(t, "command\t"+want+"\n", string(out), "%s: git reads it as the command line's, in the clone named", key)
	}
}

func TestSettingsComeAfterTheOnesTheEnvironmentPasses(t *testing.T) {
	t.Parallel()

	got, err := withSettings([]string{"A=1", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=user.x", "GIT_CONFIG_VALUE_0=y"},
		[]string{"core.hooksPath=/dev/null", "credential.helper="})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"A=1", "GIT_CONFIG_KEY_0=user.x", "GIT_CONFIG_VALUE_0=y", "GIT_CONFIG_KEY_1=core.hooksPath",
		"GIT_CONFIG_VALUE_1=/dev/null", "GIT_CONFIG_KEY_2=credential.helper", "GIT_CONFIG_VALUE_2=", "GIT_CONFIG_COUNT=3",
	}, got)

	_, err = withSettings([]string{"GIT_CONFIG_COUNT=x"}, []string{"a.b=c"})
	require.ErrorIs(t, err, ErrGitCommandNotAllowed)
}

func TestCloneSettingAllowed(t *testing.T) {
	t.Parallel()

	for _, s := range [][2]string{
		{"core.repositoryformatversion", "0"},
		{"core.sparsecheckout", on},
		{"core.sparsecheckoutcone", on},
		{"remote.origin.url", "https://example.invalid/acme-org/example-repo.git"},
		{"remote.upstream.gh-resolved", "base"},
		{"remote.fork.github-pr-remote", on},
		{"remote.origin.partialclonefilter", "blob:none"},
		{"branch.feature/x.vscode-merge-base", "origin/main"},
		{"branch.main.rebase", on},
		{"extensions.worktreeconfig", on},
		{"user.signingkey", "~/.ssh/id.pub"},
		{"gpg.format", "ssh"},
		{"commit.gpgsign", on},
		{"tag.gpgsign", on},
		{"pull.ff", "only"},
		{"submodule.active", "."},
		{"submodule.lib.url", "../lib.git"},
		{"lfs.repositoryformatversion", "0"},
		{"lfs.https://example.invalid/acme-org/example-repo.git/info/lfs.access", "basic"},
		{"lfs.https://example.invalid/acme-org/example-repo.git/info/lfs.locksverify", on},
		{"filter.lfs.process", "git-lfs filter-process"},
		{"filter.lfs.smudge", "git-lfs smudge --skip -- %f"},
		{"filter.lfs.required", on},
		{"rerere.enabled", on},
		{"rerere.autoupdate", on},
		{"log.excludedecoration", "refs/prefetch/"},
		{"color.ui", "auto"},
		{"color.diff.meta", "yellow"},
		{"gui.wmstate", "normal"},
		{"mergetool.a.cmd", "x"},
		{"mergetool.keepbackup", "false"},
		{"difftool.a.cmd", "x"},
		{"merge.tool", "a"},
		{"diff.tool", "a"},
		{"push.default", "simple"},
		{"push.autosetupremote", on},
		{"rebase.autostash", on},
		{"status.showuntrackedfiles", "all"},
		{"gitflow.branch.master", "main"},
		{"gitflow.prefix.feature", "feature/"},
		{"gitflow.path.hooks", ".git/hooks"},
		{"init.defaultbranch", "main"},
		{"merge.ff", "only"},
		{"merge.conflictstyle", "zdiff3"},
	} {
		assert.True(t, cloneSettingAllowed(s[0], s[1]), "inert: %s=%s", s[0], s[1])
	}

	for _, s := range [][2]string{
		{"core.sshcommand", "x"},
		{"core.askpass", "x"},
		{"credential.helper", "x"},
		{"credential.https://example.invalid.helper", "x"},
		{"credential.usehttppath", on},
		{"url.https://example.invalid/.insteadof", "https://github.com/"},
		{"url.ext::sh.insteadof", "https://"},
		{"url.https://example.invalid/.pushinsteadof", "https://github.com/"},
		{"http.postbuffer", "524288000"},
		{"http.https://example.invalid/.extraheader", "x"},
		{"http.proxy", "x"},
		{"http.cookiefile", "x"},
		{"remote.origin.proxy", "x"},
		{"protocol.ext.allow", "always"},
		{"protocol.version", "2"},
		{"core.hookspath", ".husky/_"},
		{"core.fsmonitor", on},
		{"submodule.recurse", on},
		{"maintenance.auto", "false"},
		{"maintenance.strategy", "incremental"},
		{"maintenance.gc.enabled", on},
		{"maintenance.prefetch.schedule", "hourly"},
		{"gc.auto", "1"},
	} {
		assert.True(t, cloneSettingAllowed(s[0], s[1]), "neutralized: %s=%s", s[0], s[1])
	}

	for _, s := range [][2]string{
		{"core.worktree", "/elsewhere"},
		{"core.alternaterefscommand", "x"},
		{"core.pager", "x"},
		{"core.editor", "x"},
		{"sequence.editor", "x"},
		{"push.https://example.invalid.default", "x"},
		{"rebase.x.y", "x"},
		{"status.x.y", "x"},
		{"merge.renormalize", on},
		{"hook.a.command", "x"},
		{"hook.a.event", "post-checkout"},
		{"gc.recentobjectshook", "x"},
		{"gc.cruftpacks", on},
		{"lfs.customtransfer.a.path", "x"},
		{"lfs.standalonetransferagent", "a"},
		{"lfs.extension.a.clean", "x"},
		{"lfs.url", "https://example.invalid"},
		{"include.path", "x"},
		{"includeif.gitdir:**/.git/worktrees/**.path", "x"},
		{"includeif.onbranch:worktree-*.path", "x"},
		{"remote.origin.uploadpack", "x"},
		{"remote.origin.vcs", "x"},
		{"remote.origin.lfsurl", "x"},
		{"remote..url", "x"},
		{"branch.main.mergeoptions", "-s ours"},
		{"branch.mergeoptions", "x"},
		{"filter.lfs.smudge", "git-lfs smudge -- %f; touch /nonexistent/owned"},
		{"filter.lfs.required", "false"},
		{"filter.evil.smudge", "x"},
		{"submodule.lib.update", "!x"},
		{"merge.a.driver", "x"},
		{"diff.a.textconv", "x"},
		{"diff.external", "x"},
		{"uploadpack.packobjectshook", "x"},
		{"gpg.program", "x"},
		{"gpg.ssh.program", "x"},
		{"nodot", ""},
		{"user.useconfigonly", on},
		{"credential.protectprotocol", "false"},
		{"credential.https://example.invalid.protectprotocol", "false"},
		{"credential.sanitizeprompt", "false"},
		{"credential.interactive", "true"},
	} {
		assert.False(t, cloneSettingAllowed(s[0], s[1]), "refused: %s=%s", s[0], s[1])
	}
}

// refusedSettings are settings of a clone's own config that refuse a Setup, each with the
// key git lists it by: one of each kind a security review found running a program or
// writing outside the clone.
func refusedSettings(root string) [][3]string {
	return [][3]string{
		{"hook.evil.command", "touch " + filepath.Join(root, "hook"), "hook.evil.command"},
		{"hook.evil.event", "post-checkout", "hook.evil.event"},
		{"core.alternateRefsCommand", "touch " + filepath.Join(root, "alternates"), "core.alternaterefscommand"},
		{"gc.recentObjectsHook", "touch " + filepath.Join(root, "gc"), "gc.recentobjectshook"},
		{"lfs.customtransfer.evil.path", filepath.Join(root, "agent"), "lfs.customtransfer.evil.path"},
		{"lfs.standalonetransferagent", "evil", "lfs.standalonetransferagent"},
		{"core.worktree", filepath.Join(root, "elsewhere"), "core.worktree"},
		{"includeIf.gitdir:**/.git/worktrees/**.path", filepath.Join(root, "evil.gitconfig"), "includeif.gitdir:**/.git/worktrees/**.path"},
		{"includeIf.onbranch:worktree-*.path", filepath.Join(root, "evil.gitconfig"), "includeif.onbranch:worktree-*.path"},
		{"filter.evil.smudge", "evil-smudge", "filter.evil.smudge"},
		{"branch.main.mergeOptions", "-s evil", "branch.main.mergeoptions"},
	}
}

func TestSetupRefusesTheClonesOwnSettingsBeforeAnythingRuns(t *testing.T) {
	t.Parallel()

	for _, s := range refusedSettings(t.TempDir()) {
		t.Run(s[2], func(t *testing.T) {
			t.Parallel()

			dir, _, env := setupClone(t)
			g := New(Config{Env: func() []string { return env }})

			testutil.RunGit(t, dir, "config", s[0], s[1])

			for name, setup := range map[string]func() (Setup, error){
				"fetch":    func() (Setup, error) { return g.RemoteSetup(t.Context(), dir) },
				"checkout": func() (Setup, error) { return g.CheckoutSetup(t.Context(), dir) },
			} {
				_, err := setup()
				require.ErrorIs(t, err, ErrCloneConfig, name)

				refused, ok := asCloneConfig(err)
				require.True(t, ok, name)
				assert.Equal(t, []string{s[2]}, refused.Keys, name)
				assert.Equal(t, []string{"file:.git/config"}, refused.Origins, name)
			}
		})
	}
}

// asCloneConfig returns err's CloneConfigError.
func asCloneConfig(err error) (*CloneConfigError, bool) {
	return errors.AsType[*CloneConfigError](err)
}

func TestSetupReadsTheWorktreeConfigAndIncludesToo(t *testing.T) {
	t.Parallel()

	dir, _, env := setupClone(t)
	g := New(Config{Env: func() []string { return env }})
	included := filepath.Join(t.TempDir(), "included.gitconfig")

	require.NoError(t, os.WriteFile(included, []byte("[hook \"evil\"]\n\tcommand = evil-hook\n"), 0o600))
	testutil.RunGit(t, dir, "config", "extensions.worktreeConfig", on)
	testutil.RunGit(t, dir, "config", "--worktree", "gc.recentObjectsHook", "evil-gc")
	testutil.RunGit(t, dir, "config", "include.path", included)

	_, err := g.CheckoutSetup(t.Context(), dir)

	refused, ok := asCloneConfig(err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, []string{"include.path", "hook.evil.command", "gc.recentobjectshook"}, refused.Keys,
		"an include, what it includes, and the worktree's own config")
	assert.Equal(t, []string{"file:.git/config", "file:" + included, "file:.git/config.worktree"}, refused.Origins)
}

func TestSetupAllowsWhatGitAndItsToolsWrite(t *testing.T) {
	t.Parallel()

	dir, _, env := setupClone(t)
	g := New(Config{Env: func() []string { return env }})

	for _, kv := range [][2]string{
		{"remote.upstream.url", "https://example.invalid/acme-org/example-repo.git"},
		{"remote.upstream.fetch", "+refs/heads/*:refs/remotes/upstream/*"},
		{"remote.upstream.gh-resolved", "base"},
		{"branch.main.remote", "origin"},
		{"branch.main.merge", "refs/heads/main"},
		{"branch.main.vscode-merge-base", "origin/main"},
		{"user.signingkey", "~/.ssh/id_ed25519.pub"},
		{"gpg.format", "ssh"},
		{"commit.gpgsign", on},
		{"tag.gpgsign", on},
		{"filter.lfs.clean", "git-lfs clean -- %f"},
		{"filter.lfs.smudge", "git-lfs smudge -- %f"},
		{"filter.lfs.process", "git-lfs filter-process"},
		{"filter.lfs.required", on},
		{"lfs.repositoryformatversion", "0"},
		{"submodule.active", "."},
		{"core.hooksPath", ".husky/_"},
		{"log.excludeDecoration", "refs/prefetch/"},
		{"pull.rebase", on},
	} {
		testutil.RunGit(t, dir, "config", kv[0], kv[1])
	}

	setup, err := g.RemoteSetup(t.Context(), dir)
	require.NoError(t, err)
	assert.NotContains(t, strings.Join(setup.Settings, " "), "filter.lfs", "Git LFS's own driver stays on")
	assert.Contains(t, setup.Settings, "core.hooksPath="+os.DevNull, "husky's folder is set again")

	_, err = g.RemoteSetupFor(t.Context(), dir, "upstream")
	require.NoError(t, err, "a fork's upstream")

	_, err = g.CheckoutSetup(t.Context(), dir)
	require.NoError(t, err)
}

func TestTheUsersOwnIncludeIfStillCounts(t *testing.T) {
	t.Parallel()

	dir, global, env := setupClone(t)
	mine := filepath.Join(t.TempDir(), "mine.gitconfig")

	require.NoError(t, os.WriteFile(mine, []byte("[core]\n\tsshCommand = my-ssh\n[credential]\n\thelper = my-helper\n"), 0o600))
	require.NoError(t, os.WriteFile(global, []byte("[includeIf \"gitdir:"+realDir(t, dir)+"/\"]\n\tpath = "+mine+"\n"), 0o600))

	setup, err := New(Config{Env: func() []string { return env }}).RemoteSetup(t.Context(), dir)
	require.NoError(t, err, "settings the user's own config includes for the folder are theirs")
	assert.NotContains(t, gitVars(setup.Env), "GIT_SSH_COMMAND="+batchSSH, "git runs the user's own ssh command")
	assert.NotContains(t, setup.Settings, "credential.helper=", "and the user's own helper")
}

func TestSetupRunsOnlyInTheClonesTopFolder(t *testing.T) {
	t.Parallel()

	dir, _, env := setupClone(t)
	g := New(Config{Env: func() []string { return env }})
	sub := filepath.Join(dir, "sub")

	require.NoError(t, os.Mkdir(sub, 0o700))

	_, err := g.CheckoutSetup(t.Context(), sub)
	require.ErrorIs(t, err, ErrWorkTree)

	_, err = g.RemoteSetup(t.Context(), "relative")
	require.ErrorIs(t, err, ErrGitCommandNotAllowed)
}

func TestRemoteSetupRefusesWhatAnLsRemoteRefuses(t *testing.T) {
	t.Parallel()

	dir, _, env := setupClone(t)
	g := New(Config{Env: func() []string { return env }})

	testutil.RunGit(t, dir, "remote", "set-url", "origin", "ext::sh -c touch% /nonexistent/pwned")

	_, err := g.RemoteSetup(t.Context(), dir)
	require.ErrorIs(t, err, ErrRemoteHelper)
}

func TestHardening(t *testing.T) {
	t.Parallel()

	got := Hardening()
	assert.Equal(t, []string{
		"-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-c", "submodule.recurse=false",
	}, got[:6])

	for _, event := range []string{
		"post-checkout", "post-merge", "post-rewrite", "reference-transaction", "post-index-change", "pre-auto-gc",
		"push-to-checkout",
	} {
		assert.Contains(t, got, "hook."+event+".enabled=false", "every hook a config names for %s is off", event)
	}
}

// TestHardeningTurnsOffTheHooksAConfigNames plants hooks a clone's own config names, which
// core.hooksPath doesn't reach, for the events a status that writes the index and a
// branch's deletion fire, and runs both with Hardening: neither hook runs, and both run
// without it.
func TestHardeningTurnsOffTheHooksAConfigNames(t *testing.T) {
	t.Parallel()

	env := testutil.Git(t)
	dir := filepath.Join(t.TempDir(), "clone")
	ran := t.TempDir()

	testutil.InitRepo(t, dir)
	commitFile(t, dir, "a.txt", "a")
	testutil.RunGit(t, dir, "branch", "doomed")

	for _, event := range []string{"post-index-change", "reference-transaction"} {
		testutil.RunGit(t, dir, "config", "hook.evil-"+event+".command", "touch "+filepath.Join(ran, event))
		testutil.RunGit(t, dir, "config", "hook.evil-"+event+".event", event)
	}

	run := func(args ...string) {
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // test: fixed words
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}

	touch := func(at time.Time) { require.NoError(t, os.Chtimes(filepath.Join(dir, "a.txt"), at, at)) }

	touch(time.Now().Add(time.Hour))
	run(append(Hardening(), "status", "--porcelain")...)
	run(append(Hardening(), "update-ref", "-d", "refs/heads/doomed")...)
	assert.NoFileExists(t, filepath.Join(ran, "post-index-change"))
	assert.NoFileExists(t, filepath.Join(ran, "reference-transaction"))

	testutil.RunGit(t, dir, "branch", "doomed")
	touch(time.Now().Add(2 * time.Hour))
	run("status", "--porcelain")
	run("update-ref", "-d", "refs/heads/doomed")
	assert.FileExists(t, filepath.Join(ran, "post-index-change"), "a plain status runs it")
	assert.FileExists(t, filepath.Join(ran, "reference-transaction"), "a plain update-ref runs it")
}

func TestLocalSetup(t *testing.T) {
	t.Parallel()

	dir, _, env := setupClone(t)
	g := New(Config{Env: func() []string { return env }})

	testutil.RunGit(t, dir, "config", "filter.lfs.process", "git-lfs filter-process")

	for _, kv := range [][2]string{
		{"push.default", "simple"},
		{"rebase.autoStash", on},
		{"status.short", on},
		{"gitflow.branch.master", "main"},
		{"init.defaultBranch", "main"},
	} {
		testutil.RunGit(t, dir, "config", kv[0], kv[1]) // what users and their tools write, which refuses nothing
	}

	local, err := g.LocalSetup(t.Context(), dir)
	require.NoError(t, err)
	require.NoError(t, local.Refused)

	for _, want := range []string{
		"GIT_OPTIONAL_LOCKS=0", "GIT_NO_LAZY_FETCH=1", "GIT_ALLOW_PROTOCOL=", "GIT_NO_REPLACE_OBJECTS=1",
	} {
		assert.Contains(t, gitVars(local.Env), want)
	}

	assert.Contains(t, local.Settings, "filter.lfs.process=", "a read turns the clone's own Git LFS off")
	assert.Contains(t, local.Settings, "hook.post-index-change.enabled=false")
	assert.Equal(t, local.Settings, configOf(t, local.Env))

	testutil.RunGit(t, dir, "config", "filter.evil.clean", "evil-clean")

	local, err = g.LocalSetup(t.Context(), dir)
	require.NoError(t, err, "a read runs none of the clone's programs")
	require.ErrorIs(t, local.Refused, ErrCloneConfig, "a command that could is refused")
	assert.Contains(t, local.Settings, "filter.evil.clean=")

	_, err = g.LocalSetup(t.Context(), "relative")
	require.ErrorIs(t, err, ErrGitCommandNotAllowed)
}

func TestSetupHandsCredentialsOverOnlyAsGitWould(t *testing.T) {
	t.Parallel()

	dir, _, env := setupClone(t)
	g := New(Config{Env: func() []string { return env }})

	setup, err := g.RemoteSetup(t.Context(), dir)
	require.NoError(t, err)
	assert.Contains(t, setup.Settings, "credential.protectProtocol=true")
	assert.Contains(t, setup.Settings, "credential.sanitizePrompt=true")
	assert.Contains(t, gitVars(setup.Env), "GIT_NO_REPLACE_OBJECTS=1", "a replace ref changes no commit's files")

	for _, key := range []string{"credential.protectProtocol", "credential.https://example.invalid.protectProtocol"} {
		testutil.RunGit(t, dir, "config", key, "false")

		_, err = g.RemoteSetup(t.Context(), dir)
		require.ErrorIs(t, err, ErrCloneConfig, "%s: a URL's own couldn't be beaten from the command line", key)

		testutil.RunGit(t, dir, "config", "--unset", key)
	}
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

	dir, _, env := setupClone(t)
	g := New(Config{Env: func() []string { return env }})

	testutil.RunGit(t, dir, "remote", "add", "upstream", "ext::sh -c touch% /nonexistent/pwned")

	_, err := g.RemoteSetup(t.Context(), dir)
	require.NoError(t, err, "origin is fine")

	_, err = g.RemoteSetupFor(t.Context(), dir, "upstream")
	require.ErrorIs(t, err, ErrRemoteHelper, "upstream's own URL is weighed")

	refused, ok := errors.AsType[*RefusedError](err)
	require.True(t, ok)
	assert.Equal(t, "upstream", refused.Remote)

	_, err = g.RemoteSetupFor(t.Context(), dir, "-x")
	require.ErrorIs(t, err, ErrGitCommandNotAllowed)
}

func TestCheckoutSetupReachesNoRemote(t *testing.T) {
	t.Parallel()

	dir, _, env := setupClone(t)

	setup, err := New(Config{Env: func() []string { return env }}).CheckoutSetup(t.Context(), dir)
	require.NoError(t, err)

	assert.True(t, filepath.IsAbs(setup.Git))
	assert.Equal(t, append(unflagged(slices.Concat(Hardening(), fetchHardening(), credentialHardening())),
		"http.savecookies=false"), setup.Settings)
	assert.Contains(t, gitVars(setup.Env), "GIT_ALLOW_PROTOCOL=", "no transport at all")
	assert.Contains(t, gitVars(setup.Env), "GIT_NO_LAZY_FETCH=1")
	assert.Contains(t, gitVars(setup.Env), "GIT_WORK_TREE="+realDir(t, dir))
	assert.False(t, setup.Partial)

	remote, err := New(Config{Env: func() []string { return env }}).RemoteSetup(t.Context(), dir)
	require.NoError(t, err)

	checkout := remote.Checkout()
	assert.Equal(t, gitVars(setup.Env), gitVars(checkout.Env), "a remote setup's checkout is the checkout's setup")
	assert.Equal(t, setup.Settings, checkout.Settings)
	assert.True(t, slices.Equal(setup.Env, checkout.Env), "the rest of the environment too")
}

// gitVars returns env's git variables alone, so a failure never prints the rest of the
// test's environment.
func gitVars(env []string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(kv string) bool { return !strings.HasPrefix(kv, "GIT_") })
}

// transportClone is a clone whose own config holds the settings that change how a remote
// is reached, which every Setup sets aside or weighs: an ssh command, a password program,
// a credential helper, an insteadOf that rewrites origin's URL to the file:// origin it
// was cloned from, a cookie file, and http settings that send nothing elsewhere.
func transportClone(t *testing.T) (dir string, env []string) {
	t.Helper()

	dir, _, env = setupClone(t)
	origin := strings.TrimSpace(testutil.RunGit(t, dir, "remote", "get-url", "origin"))

	for _, kv := range [][2]string{
		{"core.sshCommand", "evil-ssh"},
		{"core.askPass", "evil-askpass"},
		{"credential.helper", "!evil-helper"},
		{"url." + origin + ".insteadOf", "https://example.invalid/acme-org/example-repo.git"},
		{"remote.origin.url", "https://example.invalid/acme-org/example-repo.git"},
		{"http.cookieFile", filepath.Join(t.TempDir(), "cookies")},
		{"http.postBuffer", "524288000"},
		{"protocol.version", "2"},
	} {
		testutil.RunGit(t, dir, "config", kv[0], kv[1])
	}

	return dir, env
}

func TestSetupSetsTheClonesTransportSettingsAside(t *testing.T) {
	t.Parallel()

	dir, env := transportClone(t)
	g := New(Config{Env: func() []string { return env }})

	for name, setup := range map[string]func() (Setup, error){
		"fetch":    func() (Setup, error) { return g.RemoteSetup(t.Context(), dir) },
		"checkout": func() (Setup, error) { return g.CheckoutSetup(t.Context(), dir) },
	} {
		got, err := setup()
		require.NoError(t, err, name)

		assert.Contains(t, gitVars(got.Env), "GIT_SSH_COMMAND="+batchSSH, "%s: never the clone's ssh command", name)

		for _, want := range []string{"core.askpass=", "credential.helper=", "http.cookiefile="} {
			assert.Contains(t, got.Settings, want, "%s: the clone's own set aside", name)
		}

		assert.NotContains(t, strings.Join(got.Settings, " "), "evil-", name)
		assert.Equal(t, []string{SetAsideSSHCommand, SetAsideAskPass, SetAsideHelpers}, got.SetAside, name)
	}

	assert.Equal(t, "This clone's own core.sshCommand, core.askPass, and credential.helper aren't used to reach "+
		"origin, which may be why: set them for this folder in your global git config, with an includeIf.",
		SetAsideNote(remoteName, []string{SetAsideSSHCommand, SetAsideAskPass, SetAsideHelpers}))
	assert.Empty(t, SetAsideNote(remoteName, nil))
}

func TestSetupRefusesRoutesWhereGitLFSMayRun(t *testing.T) {
	t.Parallel()

	dir, global, env := setupClone(t)
	g := New(Config{Env: func() []string { return env }})

	testutil.RunGit(t, dir, "config", "http.proxy", "http://203.0.113.9:8080")

	_, err := g.CheckoutSetup(t.Context(), dir)
	require.ErrorIs(t, err, ErrCloneRoutes, "the user's Git LFS would reach its server through the clone's proxy")

	require.NoError(t, os.WriteFile(global, nil, 0o600))

	_, err = g.CheckoutSetup(t.Context(), dir)
	require.NoError(t, err, "without Git LFS, a checkout reaches no host")

	_, err = g.RemoteSetup(t.Context(), dir)
	require.NoError(t, err, "nor a fetch from a file:// origin")

	testutil.RunGit(t, dir, "remote", "set-url", "origin", "https://example.invalid/acme-org/example-repo.git")

	_, err = g.RemoteSetup(t.Context(), dir)
	require.ErrorIs(t, err, ErrCloneRoutes, "but an https origin is refused")
}

func TestSetupTakesTheFolderByAnyCase(t *testing.T) {
	t.Parallel()

	dir, _, env := setupClone(t)
	other := filepath.Join(filepath.Dir(dir), strings.ToUpper(filepath.Base(dir)))

	if _, err := os.Stat(other); err != nil {
		t.Skip("the file system tells folders apart by case")
	}

	setup, err := New(Config{Env: func() []string { return env }}).RemoteSetup(t.Context(), other)
	require.NoError(t, err, "the clone root's case may differ from the disk's")
	assert.Contains(t, gitVars(setup.Env), "GIT_WORK_TREE="+realDir(t, dir), "as git names it")
}

func TestTheSetupsGitIsTheOneGitFinderFinds(t *testing.T) {
	t.Parallel()

	dir, _, env := setupClone(t)
	g := New(Config{Env: func() []string { return env }})

	setup, err := g.RemoteSetup(t.Context(), dir)
	require.NoError(t, err)

	found, err := g.GitFinder().Find("git")
	require.NoError(t, err)
	assert.Equal(t, found, setup.Git)
}

// blobless returns a blobless partial clone of a local bare origin over file:// that lets
// clones filter, and git's environment.
func blobless(t *testing.T) (dir string, env []string) {
	t.Helper()

	env = testutil.Git(t)
	root := t.TempDir()
	src, origin := filepath.Join(root, "src"), filepath.Join(root, "origin.git")
	dir = filepath.Join(root, "clone")

	testutil.InitRepo(t, src)
	commitFile(t, src, "a.txt", "a")
	testutil.RunGit(t, root, "clone", "-q", "--bare", src, origin)
	testutil.RunGit(t, origin, "config", "uploadpack.allowFilter", on)
	testutil.RunGit(t, root, "clone", "-q", "--filter=blob:none", "--no-checkout", "file://"+filepath.ToSlash(origin), dir)

	return dir, env
}

func TestAPartialClonesSetupMayFetchFromItsPromisor(t *testing.T) {
	t.Parallel()

	dir, env := blobless(t)
	g := New(Config{Env: func() []string { return env }})

	for name, setup := range map[string]func() (Setup, error){
		"checkout": func() (Setup, error) { return g.CheckoutSetup(t.Context(), dir) },
		"fetch":    func() (Setup, error) { return g.RemoteSetup(t.Context(), dir) },
	} {
		got, err := setup()
		require.NoError(t, err, name)

		assert.True(t, got.Partial, name)
		assert.NotContains(t, gitVars(got.Env), "GIT_NO_LAZY_FETCH=1", "%s: a missing file's contents may be fetched", name)
		assert.Contains(t, gitVars(got.Env), "GIT_ALLOW_PROTOCOL=file:http:https:ssh", "%s: over the user's transports", name)
		assert.Contains(t, got.Settings, "http.savecookies=false", "%s: as a fetch", name)
	}

	testutil.RunGit(t, dir, "config", "remote.origin.uploadpack", "touch /nonexistent/owned; git-upload-pack")

	_, err := g.CheckoutSetup(t.Context(), dir)
	require.ErrorIs(t, err, ErrCloneConfig, "a lazy fetch would run the clone's own upload-pack")

	testutil.RunGit(t, dir, "config", "--unset", "remote.origin.uploadpack")
	testutil.RunGit(t, dir, "remote", "set-url", "origin", "ext::sh -c touch% /nonexistent/owned")

	_, err = g.CheckoutSetup(t.Context(), dir)
	require.ErrorIs(t, err, ErrRemoteHelper, "the promisor remote is weighed as any remote is")
}

func TestPromisors(t *testing.T) {
	t.Parallel()

	got, err := promisors([]scopedSetting{
		{scope: "local", key: "remote.fork.promisor", value: on},
		{scope: "local", key: partialCloneKey, value: "origin"},
		{scope: "local", key: "remote.old.promisor", value: on},
		{scope: "local", key: "remote.old.promisor", value: "false"},
		{scope: "local", key: "remote.origin.promisor", value: ""},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"origin", "fork"}, got)

	_, err = promisors([]scopedSetting{{scope: "local", key: partialCloneKey, value: "-x"}})
	require.ErrorIs(t, err, ErrGitCommandNotAllowed)

	got, err = promisors(nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}
