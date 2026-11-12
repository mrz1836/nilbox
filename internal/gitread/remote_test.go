package gitread

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// The scopes the cases set most.
const (
	localScope  = "local"
	globalScope = "global"
)

// scoped writes settings as `git config --show-scope -z --get-regexp` lists them: each a
// scope, a key, and a value.
func scoped(settings ...[3]string) string {
	var b strings.Builder

	for _, s := range settings {
		b.WriteString(s[0] + "\x00" + s[1] + "\n" + s[2] + "\x00")
	}

	return b.String()
}

// getURLArgs prints origin's URL as git would use it, insteadOf applied, asking nothing of
// origin.
func getURLArgs() []string { return getURLArgsFor(remoteName) }

// guardCase is a case of what an ls-remote runs with.
type guardCase struct {
	name string
	env  []string

	// settings are the config read's answer, or "" for none set.
	settings string

	// wantEnv and notEnv are what the ls-remote's environment must and must not hold;
	// wantExtra the -c settings it runs with before the cookie settings, and wantCookies
	// those after http.savecookies=false, in order.
	wantEnv     []string
	notEnv      []string
	wantExtra   []string
	wantCookies []string

	// allow is GIT_ALLOW_PROTOCOL's value, or "" for allProtocols and noneAllowed for "".
	allow string
}

// allProtocols is GIT_ALLOW_PROTOCOL's value when the user's config refuses none of them,
// and noneAllowed stands for an empty one.
const (
	allProtocols = "file:http:https:ssh"
	noneAllowed  = "-"
)

func TestRemoteGuard(t *testing.T) {
	t.Parallel()

	batch := sshCommandVar + "=" + batchSSH
	agentSSH := "ssh -o IdentityAgent=/srv/agent.sock"
	cloneSSH := [3]string{localScope, sshCommandKey, "touch /srv/owned; ssh"}

	for _, tt := range []guardCase{
		{name: "nothing chosen: batch mode", env: plainEnv(), wantEnv: []string{batch}},
		{
			name:    "GIT_SSH_COMMAND in the environment beats every config",
			env:     append(plainEnv(), sshCommandVar+"="+agentSSH),
			wantEnv: []string{sshCommandVar + "=" + agentSSH}, notEnv: []string{batch},
			settings: scoped(cloneSSH),
		},
		{
			name:    "an empty GIT_SSH_COMMAND is set, as git reads it",
			env:     append(plainEnv(), sshCommandVar+"="),
			wantEnv: []string{sshCommandVar + "="}, notEnv: []string{batch},
		},
		{
			name:    "GIT_SSH in the environment",
			env:     append(plainEnv(), "GIT_SSH=/opt/tools/bin/plink"),
			wantEnv: []string{"GIT_SSH=/opt/tools/bin/plink"}, notEnv: []string{batch},
		},
		{
			name:     "the user's own core.sshCommand",
			env:      plainEnv(),
			settings: scoped([3]string{globalScope, sshCommandKey, agentSSH}),
			notEnv:   []string{batch},
		},
		{
			name:     "the clone's core.sshCommand gives way to batch mode",
			env:      plainEnv(),
			settings: scoped(cloneSSH),
			wantEnv:  []string{batch},
		},
		{
			name: "the clone's core.sshCommand gives way to the user's",
			env:  plainEnv(),
			settings: scoped(
				[3]string{"system", sshCommandKey, "ssh -o Old=yes"},
				[3]string{globalScope, sshCommandKey, agentSSH},
				cloneSSH,
				[3]string{"worktree", sshCommandKey, "touch /srv/owned-too"},
			),
			wantEnv: []string{sshCommandVar + "=" + agentSSH},
		},
		{
			name:     "the clone's core.sshCommand gives way to GIT_SSH, which git would have put after it",
			env:      append(plainEnv(), "GIT_SSH=/opt/tools/it's/plink"),
			settings: scoped(cloneSSH),
			wantEnv:  []string{sshCommandVar + `='/opt/tools/it'\''s/plink'`, "GIT_SSH=/opt/tools/it's/plink"},
		},
		{
			name:      "the clone's core.askPass gives way to none",
			env:       plainEnv(),
			settings:  scoped([3]string{localScope, askPassKey, "/srv/owned"}),
			wantEnv:   []string{batch},
			wantExtra: []string{"-c", "core.askpass="},
		},
		{
			name: "the clone's core.askPass gives way to the user's",
			env:  plainEnv(),
			settings: scoped(
				[3]string{globalScope, askPassKey, "/opt/tools/bin/askpass"},
				[3]string{localScope, askPassKey, "/srv/owned"},
			),
			wantExtra: []string{"-c", "core.askpass=/opt/tools/bin/askpass"},
		},
		{
			name:     "GIT_ASKPASS in the environment beats core.askPass",
			env:      append(plainEnv(), "GIT_ASKPASS=/opt/tools/bin/askpass"),
			settings: scoped([3]string{localScope, askPassKey, "/srv/owned"}),
		},
		{
			name:     "the user's own credential helpers alone are left as they are",
			env:      plainEnv(),
			settings: scoped([3]string{"system", helperKey, "osxkeychain"}),
		},
		{
			name: "the clone's credential helpers are dropped, and the user's added back in order",
			env:  plainEnv(),
			settings: scoped(
				[3]string{"system", helperKey, "osxkeychain"},
				[3]string{globalScope, "credential.https://github.com.helper", ""},
				[3]string{globalScope, "credential.https://github.com.helper", "!gh auth git-credential"},
				[3]string{localScope, helperKey, "!touch /srv/owned"},
				[3]string{localScope, "credential.https://github.com.helper", "!touch /srv/owned-too"},
				[3]string{"command", "credential.http://127.0.0.1:8080.helper", ""},
			),
			wantExtra: []string{
				"-c", "credential.helper=",
				"-c", "credential.helper=osxkeychain",
				"-c", "credential.https://github.com.helper=",
				"-c", "credential.https://github.com.helper=!gh auth git-credential",
				"-c", "credential.http://127.0.0.1:8080.helper=",
			},
		},
		{
			name: "the transports, as the user's own config allows them, whatever the clone's says",
			env:  plainEnv(),
			settings: scoped(
				[3]string{globalScope, "protocol.https.allow", "never"},
				[3]string{globalScope, "protocol.persistent-https.allow", "always"},
				[3]string{localScope, "protocol.ext.allow", "always"},
				[3]string{localScope, "protocol.allow", "always"},
				[3]string{"worktree", "protocol.https.allow", "always"},
			),
			wantEnv: []string{batch},
			allow:   "file:http:ssh",
		},
		{
			name: "the user's own protocol.allow is the allowed transports' default",
			env:  plainEnv(),
			settings: scoped(
				[3]string{globalScope, "protocol.allow", "never"},
				[3]string{globalScope, "protocol.https.allow", "user"},
				[3]string{localScope, "protocol.ssh.allow", "always"},
			),
			allow: "https",
		},
		{
			name:  "the user's own GIT_ALLOW_PROTOCOL limits them",
			env:   append(plainEnv(), allowProtocolVar+"=https:ext"),
			allow: "https",
		},
		{
			name:  "an empty GIT_ALLOW_PROTOCOL allows none",
			env:   append(plainEnv(), allowProtocolVar+"="),
			allow: noneAllowed,
		},
		{
			name: "the user's own proxy and certificates are theirs",
			env:  plainEnv(),
			settings: scoped(
				[3]string{globalScope, "http.proxy", "http://proxy.example:3128"},
				[3]string{globalScope, "http.sslcainfo", "/opt/certs/corp.pem"},
				[3]string{"command", proxyKey, "http://proxy.example:3128"},
			),
		},
		{
			name: "no cookie is saved, and the clone names no cookie file",
			env:  plainEnv(),
			settings: scoped(
				[3]string{localScope, "http.savecookies", "true"},
				[3]string{localScope, "http.https://x.example.savecookies", "true"},
				[3]string{localScope, "http.https://x.example.savecookies", "yes"},
				[3]string{localScope, "http.cookiefile", "/srv/home/.zshenv"},
				[3]string{globalScope, "http.cookiefile", "/srv/home/.cookies"},
				[3]string{globalScope, "http.https://y.example.savecookies", "true"},
			),
			wantCookies: []string{
				"-c", "http.https://x.example.savecookies=false",
				"-c", "http.cookiefile=",
				"-c", "http.https://y.example.savecookies=false",
			},
		},
		{
			name:      "the clone's core.askPass gives way to SSH_ASKPASS, which git would have fallen back to",
			env:       append(plainEnv(), "SSH_ASKPASS=/opt/tools/bin/ssh-askpass"),
			settings:  scoped([3]string{localScope, askPassKey, "/srv/owned"}),
			wantEnv:   []string{batch},
			wantExtra: []string{"-c", "core.askpass=/opt/tools/bin/ssh-askpass"},
		},
		{
			name: "a value with a newline is passed whole",
			env:  plainEnv(),
			settings: scoped(
				[3]string{globalScope, askPassKey, "/opt/tools/bin/ask\npass"},
				[3]string{localScope, askPassKey, "/srv/owned"},
			),
			wantExtra: []string{"-c", "core.askpass=/opt/tools/bin/ask\npass"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, args := range [][]string{remoteRefsArgs(), remoteTagsArgs(), remoteTagArgs("v1.4.2")} {
				checkGuard(t, tt, args)
			}
		})
	}
}

// checkGuard runs the ls-remote args as tt sets it up and checks what it ran with.
func checkGuard(t *testing.T, tt guardCase, args []string) {
	t.Helper()

	read := reply{code: 1}
	if tt.settings != "" {
		read = reply{out: tt.settings}
	}

	rec := &recorder{replies: map[string]reply{words(remoteKeysArgs()): read, words(args): {}}}

	_, _, err := fakeGit(tt.env, rec).Run(t.Context(), repoDir, args...)
	require.NoError(t, err)

	calls := rec.Calls()
	require.Len(t, calls, 3, "the settings are read, then origin's URL, then origin asked")

	for _, read := range calls[:2] {
		assert.Empty(t, read.extra, "the reads run with no settings of gitread's")
		assert.NotContains(t, read.env, sshCommandVar+"="+batchSSH, "the reads set no ssh")
		assert.Contains(t, read.env, allowProtocolVar+"=", "the reads reach no transport")
	}

	assert.Equal(t, remoteKeysArgs(), calls[0].args)
	assert.Equal(t, getURLArgs(), calls[1].args)

	last := calls[2]
	assert.Equal(t, args, last.args)
	assert.Equal(t, uploadPackFlag, last.args[1], "git's own upload-pack, whatever the clone says")

	want := append(slices.Clone(tt.wantExtra), "-c", "http.savecookies=false")
	assert.Equal(t, append(want, tt.wantCookies...), last.extra)

	allow := tt.allow

	switch allow {
	case "":
		allow = allProtocols
	case noneAllowed:
		allow = ""
	}

	assert.Equal(t, []string{allow}, values(last.env, allowProtocolVar), "GIT_ALLOW_PROTOCOL, once")

	for _, kv := range tt.wantEnv {
		assert.Contains(t, last.env, kv)
	}

	for _, kv := range tt.notEnv {
		assert.NotContains(t, last.env, kv)
	}

	assert.LessOrEqual(t, len(values(last.env, sshCommandVar)), 1, "GIT_SSH_COMMAND is set once at most")
}

// values returns every value env gives key.
func values(env []string, key string) []string {
	var out []string

	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			out = append(out, v)
		}
	}

	return out
}

func TestRemoteGuardStopsOnAConfigItCantRead(t *testing.T) {
	t.Parallel()

	for name, read := range map[string]reply{
		"git fails":       {code: 128},
		"a broken answer": {out: "local\x00core.askpass\n/srv/x\x00global"},
		"a key it can't -c": {out: scoped(
			[3]string{globalScope, "credential.https://x.example/?a=b.helper", "y"}, [3]string{localScope, helperKey, "z"},
		)},
		"a cookie key it can't -c": {out: scoped([3]string{localScope, "http.https://x.example/?a=b.savecookies", "true"})},
		"a control key": {out: scoped(
			[3]string{globalScope, "credential.https://x.example/\x01.helper", "y"}, [3]string{localScope, helperKey, "z"},
		)},
	} {
		rec := &recorder{replies: map[string]reply{words(remoteKeysArgs()): read, words(remoteRefsArgs()): {}}}

		_, err := fakeGit(plainEnv(), rec).RemoteRefs(t.Context(), repoDir)
		require.Error(t, err, name)
		assert.NotContains(t, rec.Args(), remoteRefsArgs(), "%s: origin isn't asked", name)
	}

	_, err := configSetting("a=b", "c")
	require.ErrorIs(t, err, ErrGitCommandNotAllowed)
}

func TestRemoteGuardRefusesARemoteHelper(t *testing.T) {
	t.Parallel()

	for name, replies := range map[string]map[string]reply{
		"a helper URL":                 {words(getURLArgs()): {out: "ssh::anything\n"}},
		"a helper URL after insteadOf": {words(getURLArgs()): {out: "file::/srv/elsewhere\n"}},
		"remote.origin.vcs, the clone's": {
			words(remoteKeysArgs()): {out: scoped([3]string{localScope, vcsKey, "file"})},
		},
		"remote.origin.vcs, anyone's": {
			words(remoteKeysArgs()): {out: scoped([3]string{globalScope, vcsKey, "hg"})},
		},
		"origin's URL can't be read": {words(getURLArgs()): {code: 128}},
	} {
		replies[words(remoteRefsArgs())] = reply{}
		rec := &recorder{replies: replies}

		_, err := fakeGit(plainEnv(), rec).RemoteRefs(t.Context(), repoDir)
		require.Error(t, err, name)
		assert.NotContains(t, rec.Args(), remoteRefsArgs(), "%s: origin isn't asked", name)

		if name != "origin's URL can't be read" {
			require.ErrorIs(t, err, ErrRemoteHelper, name)
		}
	}
}

func TestRemoteGuardRefusesAnOriginTheCloneRoutes(t *testing.T) {
	t.Parallel()

	for _, route := range [][3]string{
		{localScope, "http.https://github.com.sslverify", "false"},
		{localScope, "http.sslcainfo", "/srv/evil-ca.pem"},
		{localScope, "http.sslcapath", "/srv/evil-cas"},
		{"worktree", "http.proxy", "http://203.0.113.9:8080"},
		{localScope, "http.https://github.com.curloptresolve", "github.com:443:203.0.113.9"},
		{localScope, proxyKey, "http://203.0.113.9:8080"},
	} {
		for _, url := range []string{originURL, "http://git.example/acme-org/example-repo.git", "HTTPS://github.com/x"} {
			rec := &recorder{replies: map[string]reply{
				words(remoteKeysArgs()): {out: scoped(route)},
				words(getURLArgs()):     {out: url + "\n"},
				words(remoteRefsArgs()): {},
			}}

			_, err := fakeGit(plainEnv(), rec).RemoteRefs(t.Context(), repoDir)
			require.ErrorIs(t, err, ErrCloneRoutes, "%s on %s", route[1], url)
			assert.Contains(t, err.Error(), route[1], "the error names the setting")
			assert.NotContains(t, rec.Args(), remoteRefsArgs(), "%s on %s: origin isn't asked", route[1], url)
		}
	}
}

func TestRemoteGuardLetsAnSSHOriginGoWhateverTheHTTPSettings(t *testing.T) {
	t.Parallel()

	for _, url := range []string{"git@github.com:acme-org/example-repo.git", "ssh://git@github.com/acme-org/x.git", "/srv/origin.git"} {
		rec := &recorder{replies: map[string]reply{
			words(remoteKeysArgs()): {out: scoped([3]string{localScope, "http.proxy", "http://203.0.113.9:8080"})},
			words(getURLArgs()):     {out: url + "\n"},
			words(remoteRefsArgs()): {},
		}}

		_, err := fakeGit(plainEnv(), rec).RemoteRefs(t.Context(), repoDir)
		require.NoError(t, err, url)
		assert.Contains(t, rec.Args(), remoteRefsArgs(), "%s: http settings don't change how ssh or a folder is reached", url)
	}
}

func TestHTTPURL(t *testing.T) {
	t.Parallel()

	for _, url := range []string{"https://github.com/x", "http://host/x", "HTTPS://github.com/x", "Http://h/x"} {
		assert.True(t, httpURL(url), url)
	}

	for _, url := range []string{"git@github.com:x/y.git", "ssh://h/x", "/srv/x", "file:///srv/x", "https:/x", "xhttps://h"} {
		assert.False(t, httpURL(url), url)
	}
}

func TestHelperURL(t *testing.T) {
	t.Parallel()

	for _, url := range []string{"ssh::x", "file::/srv/x", "ext::sh -c x", "persistent-https::host/x", "a+b.c-d::x", "X1::y", "0ssh::q", "1a::x"} {
		assert.True(t, helperURL(url), url)
	}

	for _, url := range []string{
		"https://github.com/acme-org/example-repo.git", "git@github.com:acme-org/example-repo.git",
		"/srv/origin.git", "::x", "+a::x", "a b::x", "ssh://host/a::b", "file:///srv/a::b",
	} {
		assert.False(t, helperURL(url), url)
	}
}

func TestParseScoped(t *testing.T) {
	t.Parallel()

	got, err := parseScoped([]byte("global\x00core.askpass\n/a\x00local\x00credential.helper\x00"))
	require.NoError(t, err)
	assert.Equal(t, []scopedSetting{
		{scope: "global", key: "core.askpass", value: "/a"},
		{scope: "local", key: helperKey},
	}, got, "a key without a value has an empty one")

	got, err = parseScoped(nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestIsHelperKey(t *testing.T) {
	t.Parallel()

	for _, key := range []string{helperKey, "credential.https://github.com.helper", "credential.https://a.b/c.helper"} {
		assert.True(t, isHelperKey(key), key)
	}

	for _, key := range []string{"credential.username", "core.askpass", "credentialhelper", "credential.https://a.helperx"} {
		assert.False(t, isHelperKey(key), key)
	}
}

// remoteRig is a clone of a local bare origin with a version tag, isolated from the user's
// git config, and its environment.
type remoteRig struct {
	env    []string
	clone  string
	marker string
}

// newRemoteRig builds a remoteRig, or skips without git.
func newRemoteRig(t *testing.T) remoteRig {
	t.Helper()

	r := remoteRig{env: testutil.Git(t)}
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	r.clone = filepath.Join(root, "clone")
	r.marker = filepath.Join(t.TempDir(), "ran")

	testutil.RunGit(t, root, "-c", "init.defaultBranch=main", "init", "-q", "--bare", origin)
	testutil.InitRepo(t, r.clone)
	commitFile(t, r.clone, "a.txt", "a")
	testutil.RunGit(t, r.clone, "tag", "v1.0.0")
	testutil.RunGit(t, r.clone, "remote", "add", "origin", origin)
	testutil.RunGit(t, r.clone, "push", "-q", "origin", "HEAD:refs/heads/main", "--tags")

	return r
}

// git returns a Git over the rig's environment.
func (r remoteRig) git() *Git {
	return New(Config{Env: func() []string { return r.env }})
}

// plainLsRemote runs a plain `git ls-remote origin` in the clone, as the user's terminal
// would, and ignores how it ends: the case only shows that it runs the clone's program.
func (r remoteRig) plainLsRemote(t *testing.T) {
	t.Helper()

	if _, _, err := (execpath.Exec{Env: r.env}).Run(t.Context(), gitBin, "-C", r.clone, lsRemote, remoteName); err != nil {
		t.Logf("a plain ls-remote: %v", err)
	}
}

// TestRemoteRunsNoUploadPackTheCloneNames reproduces a clone whose config names the
// upload-pack origin runs: for a local origin, it runs on this Mac.
func TestRemoteRunsNoUploadPackTheCloneNames(t *testing.T) {
	t.Parallel()

	r := newRemoteRig(t)
	testutil.RunGit(t, r.clone, configCmd, "remote.origin.uploadpack", "touch '"+r.marker+"'; git-upload-pack")

	got, err := r.git().RemoteVersionTags(t.Context(), r.clone)
	require.NoError(t, err)
	assert.Equal(t, []string{"v1.0.0"}, got)
	assert.NoFileExists(t, r.marker, "gitread's ls-remote ran the clone's upload-pack")

	r.plainLsRemote(t)
	assert.FileExists(t, r.marker, "the clone is set up so that a plain ls-remote runs it")
}

// TestRemoteRunsNoExtURLTheCloneAllows reproduces a clone whose config allows the ext
// transport and points origin at an ext:: URL, which is a command.
func TestRemoteRunsNoExtURLTheCloneAllows(t *testing.T) {
	t.Parallel()

	r := newRemoteRig(t)
	testutil.RunGit(t, r.clone, configCmd, "protocol.ext.allow", "always")
	testutil.RunGit(t, r.clone, configCmd, "remote.origin.url", "ext::sh -c touch% "+r.marker)

	_, err := r.git().RemoteVersionTags(t.Context(), r.clone)
	require.Error(t, err)
	assert.NoFileExists(t, r.marker, "gitread's ls-remote ran the clone's ext:: command")

	r.plainLsRemote(t)
	assert.FileExists(t, r.marker, "the clone is set up so that a plain ls-remote runs it")
}

// TestRemoteRunsNoSSHCommandTheCloneNames reproduces a clone whose config names the ssh
// command, for a user whose environment sets GIT_SSH: git prefers the clone's
// core.sshCommand to GIT_SSH. GIT_SSH names a program that doesn't exist, so nothing
// reaches the network either way.
func TestRemoteRunsNoSSHCommandTheCloneNames(t *testing.T) {
	t.Parallel()

	r := newRemoteRig(t)
	r.env = append(r.env, "GIT_SSH="+filepath.Join(t.TempDir(), "no-ssh"))

	testutil.RunGit(t, r.clone, configCmd, "remote.origin.url", "ssh://git@example.invalid/acme-org/example-repo.git")
	testutil.RunGit(t, r.clone, configCmd, "core.sshCommand", "touch '"+r.marker+"'; false")

	_, err := r.git().RemoteVersionTags(t.Context(), r.clone)
	require.Error(t, err)
	assert.NoFileExists(t, r.marker, "gitread's ls-remote ran the clone's ssh command")

	r.plainLsRemote(t)
	assert.FileExists(t, r.marker, "the clone is set up so that a plain ls-remote runs it")
}

func TestRemoteFailureNamesWhatItSetAside(t *testing.T) {
	t.Parallel()

	everything := reply{out: scoped(
		[3]string{localScope, sshCommandKey, "ssh -i /srv/work-key"},
		[3]string{localScope, askPassKey, "/srv/ask"},
		[3]string{localScope, helperKey, "!/srv/helper"},
	)}

	for _, tt := range []struct {
		name string
		env  []string
		read reply
		want []string // the settings named, or nil for a plain error
	}{
		{name: "everything the clone names", env: plainEnv(), read: everything, want: []string{
			SetAsideSSHCommand, SetAsideAskPass, SetAsideHelpers,
		}},
		{name: "nothing the clone names", env: plainEnv(), read: reply{code: 1}},
		{
			name: "only what the environment doesn't override",
			env:  append(plainEnv(), sshCommandVar+"=ssh", askPassVar+"=/opt/tools/bin/askpass"),
			read: everything,
			want: []string{SetAsideHelpers},
		},
	} {
		rec := &recorder{replies: map[string]reply{words(remoteKeysArgs()): tt.read, words(remoteRefsArgs()): {code: 128}}}

		_, err := fakeGit(tt.env, rec).RemoteRefs(t.Context(), repoDir)
		require.Error(t, err, tt.name)
		assert.Equal(t, 128, execpath.ExitCode(err), "%s: the ls-remote's own error is kept", tt.name)

		var aside *SetAsideError
		if tt.want == nil {
			assert.NotErrorAs(t, err, &aside, tt.name)

			continue
		}

		require.ErrorAs(t, err, &aside, tt.name)
		assert.Equal(t, tt.want, aside.Settings, tt.name)
		assert.Equal(t, aside.Err.Error(), aside.Error(), "%s: its text is the ls-remote's", tt.name)
	}
}

func TestRemoteTimeoutIsNotSetAside(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rec := &recorder{
			replies: map[string]reply{words(remoteKeysArgs()): {out: scoped([3]string{localScope, sshCommandKey, "x"})}},
			answer: func(ctx context.Context, _ []string) (string, error) {
				<-ctx.Done()

				return "", errKilled
			},
		}

		_, err := fakeGit(plainEnv(), rec).RemoteRefs(t.Context(), repoDir)
		require.ErrorIs(t, err, context.DeadlineExceeded)

		var aside *SetAsideError
		assert.NotErrorAs(t, err, &aside, "a timeout is a timeout, whatever was set aside")
	})
}

// TestRemoteRunsNoAliasForAHelperURL reproduces a clone whose origin is reached through a
// remote helper named after an allowed transport: git finds no git-remote-ssh program and
// runs the alias of that name the clone's config defines.
func TestRemoteRunsNoAliasForAHelperURL(t *testing.T) {
	t.Parallel()

	for name, setUp := range map[string][][]string{
		"a helper URL":      {{configCmd, "remote.origin.url", "ssh::anything"}},
		"remote.origin.vcs": {{configCmd, "remote.origin.vcs", "ssh"}},
	} {
		r := newRemoteRig(t)
		testutil.RunGit(t, r.clone, configCmd, "alias.remote-ssh", "!touch '"+r.marker+"'; false")

		for _, args := range setUp {
			testutil.RunGit(t, r.clone, args...)
		}

		_, err := r.git().RemoteVersionTags(t.Context(), r.clone)
		require.ErrorIs(t, err, ErrRemoteHelper, name)
		assert.NoFileExists(t, r.marker, "%s: gitread's ls-remote ran the clone's alias", name)

		r.plainLsRemote(t)
		assert.FileExists(t, r.marker, "%s: the clone is set up so that a plain ls-remote runs it", name)
	}
}

// TestReadsFetchNothingInAPartialClone reproduces a partial clone whose origin is an ext::
// URL the clone allows, with a tag at an object it doesn't have: reading the tag's commit
// fetches the object, which runs origin's command. gitread's reads reach no transport.
func TestReadsFetchNothingInAPartialClone(t *testing.T) {
	t.Parallel()

	r := newRemoteRig(t)

	for _, kv := range [][2]string{
		{"core.repositoryformatversion", "1"},
		{"extensions.partialClone", "origin"},
		{"remote.origin.promisor", "true"},
		{"protocol.ext.allow", "always"},
		{"remote.origin.url", "ext::sh -c touch% " + r.marker},
	} {
		testutil.RunGit(t, r.clone, configCmd, kv[0], kv[1])
	}

	// Written by hand: git's own update-ref would fetch the missing object first.
	missing := strings.Repeat("1", 40)
	require.NoError(t, os.WriteFile(filepath.Join(r.clone, ".git", "refs", "tags", "v9.9.9"), []byte(missing+"\n"), 0o600))

	g := r.git()

	got, err := g.LocalTagCommit(t.Context(), r.clone, "v9.9.9")
	require.NoError(t, err)
	assert.Empty(t, got, "a commit the clone doesn't have")

	_, err = g.HasCommit(t.Context(), r.clone, missing)
	require.NoError(t, err)
	assert.NoFileExists(t, r.marker, "gitread's read fetched the missing object")

	if _, _, err = (execpath.Exec{Env: r.env}).Run(t.Context(), gitBin, "-C", r.clone, "rev-parse", "--verify", "--quiet",
		"refs/tags/v9.9.9^{commit}"); err != nil {
		t.Logf("a plain rev-parse: %v", err)
	}

	assert.FileExists(t, r.marker, "the clone is set up so that a plain read fetches it")
}

func TestRemoteGuardPassesOnWhatGitSaid(t *testing.T) {
	t.Parallel()

	rec := &recorder{replies: map[string]reply{words(remoteKeysArgs()): {code: 128}}}

	_, err := fakeGit(plainEnv(), rec).RemoteRefs(t.Context(), repoDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git ls-remote: fake refusal", "git's own words about the config it couldn't read")
}
