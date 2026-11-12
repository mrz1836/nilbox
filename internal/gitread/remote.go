package gitread

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/execpath"
)

const (
	// askPassVar chooses the program git asks for a password with, over core.askPass,
	// and sshAskPassVar is the one git falls back to after core.askPass.
	askPassVar    = "GIT_ASKPASS"
	sshAskPassVar = "SSH_ASKPASS"

	// allowProtocolVar lists the only transports git may use, over any config: none for
	// every command but an ls-remote, and allowedProtocols, as the user allows them, for
	// one. git has read it since 2.6.1.
	allowProtocolVar = "GIT_ALLOW_PROTOCOL"

	// The config keys, as git lists them (lowercased), that the guard reads.
	sshCommandKey = "core.sshcommand"
	askPassKey    = "core.askpass"
	helperPrefix  = "credential."
	helperKey     = helperPrefix + "helper"
	helperSuffix  = ".helper"
	vcsKey        = "remote." + remoteName + ".vcs"
	proxyKey      = "remote." + remoteName + ".proxy"
	httpPrefix    = "http."

	// partialCloneKey names the remote a partial clone fetches missing objects from.
	partialCloneKey = "extensions.partialclone"

	// protocolKeyPrefix and protocolKeySuffix make a transport's own allowance,
	// protocol.<name>.allow, and protocolDefaultKey the allowance of every transport
	// without one; never is the allowance that refuses it.
	protocolKeyPrefix  = "protocol."
	protocolKeySuffix  = ".allow"
	protocolDefaultKey = "protocol.allow"
	protocolNever      = "never"

	// The settings of the clone's own an ls-remote set aside, as SetAsideError names them:
	// its ssh command, password program, and credential helpers.
	SetAsideSSHCommand = "core.sshCommand"
	SetAsideAskPass    = "core.askPass"
	SetAsideHelpers    = helperKey
)

// ErrRemoteHelper is returned for a clone whose origin is reached through a remote helper
// (a <helper>::<address> URL, after any insteadOf, or remote.origin.vcs): git looks for
// the helper as a program, then as an alias, which the clone's config can define, so an
// ls-remote there is refused before it runs.
var ErrRemoteHelper = errors.New("origin is reached through a remote helper, which isn't run")

// ErrCloneRoutes is returned for a clone whose origin is on http or https and whose own
// config changes how it is reached: whether its certificate is checked and against what,
// a proxy, or the address its host resolves to (routeKeys). Asked from there, origin could
// be someone else, handed the user's credentials, cookies, and headers for the real host,
// so an ls-remote there is refused before it runs.
var ErrCloneRoutes = errors.New("this clone's own git config changes how origin is reached, so it isn't asked from here")

// ErrRemoteTimeout is returned for an ls-remote that ran past RemoteTimeout, the only
// command here with a limit of its own.
var ErrRemoteTimeout = errors.New("origin didn't answer in time")

// RefusedError is a remote gitread's guards refused before anything ran: Remote is its
// name, and Err says why, ErrRemoteHelper or ErrCloneRoutes, with the setting or the URL
// that refused it.
type RefusedError struct {
	Remote string
	Err    error
}

// Error returns the refusal's text.
func (e *RefusedError) Error() string { return e.Err.Error() }

// Unwrap returns the refusal.
func (e *RefusedError) Unwrap() error { return e.Err }

// SetAsideError is an ls-remote, or a fetch run with RemoteSetupFor's settings, that failed
// after gitread set aside settings, which may be why it failed: the clone picks its own
// ssh key with core.sshCommand, say, which gitread doesn't run. Its text is the command's
// own.
type SetAsideError struct {
	// Err is the command's error, and Settings the settings set aside, of
	// SetAsideSSHCommand, SetAsideAskPass, and SetAsideHelpers.
	Err      error
	Settings []string

	// Remote is the remote the command reached, "" for origin.
	Remote string
}

// Error returns the ls-remote's error's text.
func (e *SetAsideError) Error() string { return e.Err.Error() }

// Unwrap returns the ls-remote's error.
func (e *SetAsideError) Unwrap() error { return e.Err }

// trustedScopes are the config scopes whose settings are the user's own: their system and
// global config files (an includeIf there included), and the settings their environment
// passes (GIT_CONFIG_PARAMETERS, GIT_CONFIG_COUNT). A clone's local or worktree config, or
// any scope git adds later, is the clone's, which anyone who made the clone could have
// written.
func trustedScopes() []string { return []string{"system", "global", "command"} }

// trustedScope reports a scope whose settings are the user's own.
func trustedScope(scope string) bool { return slices.Contains(trustedScopes(), scope) }

// allowedProtocols are the transports an ls-remote may use: GitHub's (https and ssh),
// plain http, and file, for a local origin. Every other one is refused, so no ext:: URL,
// whose address is a command, runs.
func allowedProtocols() []string { return []string{"file", "http", "https", "ssh"} }

// routeKeys are the http settings, set for every URL or one, that change how http and
// https reach origin: whether its certificate is checked and against what, a proxy, and
// the address a host name resolves to. remote.origin.proxy is one too (isRouteKey).
func routeKeys() []string {
	return []string{"sslverify", "sslcainfo", "sslcapath", "proxy", "curloptresolve"}
}

// scopedSetting is one config setting as `git config --show-scope -z` lists it: where it
// is set (its scope and, when listed with --show-origin, its origin), its key (lowercased,
// apart from a subsection), and its value.
type scopedSetting struct {
	scope  string
	origin string
	key    string
	value  string
}

// trusted reports a setting from the user's own config rather than the clone's.
func (s scopedSetting) trusted() bool { return trustedScope(s.scope) }

// remoteSetup is what one ls-remote runs with: its environment, the -c settings it adds,
// and what it set aside.
type remoteSetup struct {
	env      []string
	extra    []string
	setAside []string
}

// remoteGuard works out what an ls-remote in dir runs with, so that it runs no program the
// clone's own config names, writes no file the clone names, and hands the user's
// credentials to nothing the clone points it at, while the user's own choices still run.
// env is the command's environment (environment's), and userAllow the user's own
// GIT_ALLOW_PROTOCOL, when their environment sets it. It reads where each setting that
// matters is set, with one config command, and origin's URL as git would use it, with
// `ls-remote --get-url`, which asks nothing of origin; then:
//
//   - a remote helper: refused, with ErrRemoteHelper;
//   - an http or https origin whose way there the clone's own config changes (routeKeys):
//     refused, with ErrCloneRoutes;
//   - the transports: only allowedProtocols, as the user's own config and GIT_ALLOW_PROTOCOL
//     allow them, through GIT_ALLOW_PROTOCOL, which beats every config, the clone's own
//     protocol.<name>.allow included;
//   - the ssh command: GIT_SSH_COMMAND in the environment beats every config, and is kept.
//     When the clone sets core.sshCommand, GIT_SSH_COMMAND is set to the one the user's
//     own config chose, else to GIT_SSH as a command, since git prefers core.sshCommand to
//     GIT_SSH, else to the batch-mode ssh. When nothing chose one, it is the batch-mode ssh;
//   - the password program and the credential helpers: when the clone sets core.askPass
//     and the environment doesn't choose one with GIT_ASKPASS,
//     core.askPass is set to the user's own, else to SSH_ASKPASS, which git would have
//     fallen back to, else to none (git runs it even with GIT_TERMINAL_PROMPT=0); and when
//     the clone sets a credential helper, an empty credential.helper clears every helper,
//     the URL-scoped ones too, and the user's own are added back in order;
//   - cookies: none are saved (http.saveCookies=false, the same for each URL a setting
//     names), and a cookie file the clone names is set to none.
//
// The upload-pack origin is asked for is git's own whatever the clone says, by the
// ls-remote's own --upload-pack (the allowlist holds it). A config git can't read stops
// the ls-remote, as it would have stopped git.
func (g *Git) remoteGuard(
	ctx context.Context, dir string, env []string, userAllow *string,
) (setup remoteSetup, stderr []byte, err error) {
	settings, stderr, err := g.readScoped(ctx, dir, env)
	if err != nil {
		return remoteSetup{}, stderr, err
	}

	return g.guardRemotes(ctx, dir, []string{remoteName}, settings, env, userAllow)
}

// guardRemotes is remoteGuard for each of remotes, each of which ValidRemoteName passed,
// over settings, the ones remoteKeysPattern matches, read once: each remote's own
// remote.<remote>.vcs and remote.<remote>.proxy, and its URL, are weighed in turn.
func (g *Git) guardRemotes(
	ctx context.Context, dir string, remotes []string, settings []scopedSetting, env []string, userAllow *string,
) (remoteSetup, []byte, error) {
	for _, remote := range remotes {
		if stderr, err := g.weigh(ctx, dir, remote, settings, env); err != nil {
			return remoteSetup{}, stderr, err
		}
	}

	setup, err := guard(env, settings, userAllow)

	return setup, nil, err
}

// weigh refuses the clone's remote called remote when it is reached through a remote
// helper, or is an http or https one the clone's own config reroutes, reading its URL as
// git would use it, which asks it nothing.
func (g *Git) weigh(ctx context.Context, dir, remote string, settings []scopedSetting, env []string) ([]byte, error) {
	vcs := "remote." + remote + ".vcs"

	if s, set := lastAny(settings, vcs); set {
		return nil, &RefusedError{Remote: remote, Err: fmt.Errorf("%w (%s is %q)", ErrRemoteHelper, vcs, s)}
	}

	url, stderr, err := g.exec(ctx, dir, env, nil, getURLArgsFor(remote))
	if err != nil {
		return stderr, fmt.Errorf("read %s's URL: %w", remote, err)
	}

	u := firstLine(url)

	if helperURL(u) {
		return nil, &RefusedError{Remote: remote, Err: fmt.Errorf("%w (%s's URL is %q)", ErrRemoteHelper, remote, u)}
	}

	if key, routes := cloneRoute(settings, remote); routes && httpURL(u) {
		return nil, &RefusedError{Remote: remote, Err: fmt.Errorf("%w (%s)", ErrCloneRoutes, key)}
	}

	return nil, nil
}

// promisors returns the remotes a partial clone fetches a missing object from: the one
// extensions.partialClone names, then every remote whose remote.<name>.promisor is true
// (promisorFlags), as the clone's config or the user's says, each once, in that order. A
// name ValidRemoteName refuses is refused.
func promisors(settings []scopedSetting) ([]string, error) {
	var out []string

	if name, ok := lastAny(settings, partialCloneKey); ok && name != "" {
		out = append(out, name)
	}

	for _, name := range promisorFlags(settings) {
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}

	for _, name := range out {
		if !ValidRemoteName(name) {
			return nil, fmt.Errorf("%w: a promisor remote named %q", ErrGitCommandNotAllowed, name)
		}
	}

	return out, nil
}

// promisorFlags returns the remotes whose last remote.<name>.promisor is true, in the
// order the first one of each is set.
func promisorFlags(settings []scopedSetting) []string {
	var order []string

	last := map[string]bool{}

	for _, s := range settings {
		rest, ok := strings.CutPrefix(s.key, "remote.")
		name, isPromisor := strings.CutSuffix(rest, ".promisor")

		if !ok || !isPromisor {
			continue
		}

		if _, seen := last[name]; !seen {
			order = append(order, name)
		}

		last[name] = gitTrue(s.value)
	}

	return slices.DeleteFunc(order, func(name string) bool { return !last[name] })
}

// gitTrue reports a value git reads as true: a key with no value, or true, yes, on, or 1,
// in any case.
func gitTrue(value string) bool {
	return slices.Contains([]string{"", "true", "yes", "on", "1"}, strings.ToLower(value))
}

// readScoped reads, with where each is set, the settings remoteGuard weighs, and returns
// what git wrote to standard error when it fails.
func (g *Git) readScoped(ctx context.Context, dir string, env []string) ([]scopedSetting, []byte, error) {
	out, errOut, err := g.exec(ctx, dir, env, nil, remoteKeysArgs())

	switch {
	case err == nil:
	case ctx.Err() == nil && execpath.ExitCode(err) == 1:
		return nil, nil, nil // none of the keys is set
	default:
		return nil, errOut, fmt.Errorf("read the clone's git config: %w", err)
	}

	settings, err := parseScoped(out)

	return settings, nil, err
}

// guard works out what an ls-remote runs with from env, the settings remoteGuard read, and
// the user's own GIT_ALLOW_PROTOCOL, as remoteGuard describes.
func guard(env []string, settings []scopedSetting, userAllow *string) (remoteSetup, error) {
	setup := remoteSetup{
		env: withVar(sshEnvironment(env, settings), allowProtocolVar, allowList(settings, userAllow)),
	}

	cookies, err := cookieSettings(settings)
	if err != nil {
		return remoteSetup{}, err
	}

	if !hasVar(env, sshCommandVar) && cloneSets(settings, isKey(sshCommandKey)) {
		setup.setAside = append(setup.setAside, SetAsideSSHCommand)
	}

	askPass, err := askPassSettings(env, settings)
	if err != nil {
		return remoteSetup{}, err
	}

	helpers, err := helperSettings(settings)
	if err != nil {
		return remoteSetup{}, err
	}

	setup.extra = append(append(append(setup.extra, askPass...), helpers...), cookies...)

	if len(askPass) > 0 {
		setup.setAside = append(setup.setAside, SetAsideAskPass)
	}

	if len(helpers) > 0 {
		setup.setAside = append(setup.setAside, SetAsideHelpers)
	}

	return setup, nil
}

// remoteKeysArgs lists the settings remoteGuard weighs, with where each is set,
// NUL-terminated so a value may hold a newline.
func remoteKeysArgs() []string {
	return []string{configCmd, "--show-scope", "-z", "--get-regexp", remoteKeysPattern}
}

// getURLArgs prints origin's URL as git would use it, insteadOf applied, asking nothing of
// origin.
func getURLArgs() []string { return getURLArgsFor(remoteName) }

// getURLArgsFor prints remote's URL as git would use it, insteadOf applied, asking nothing of
// it.
func getURLArgsFor(remote string) []string { return []string{lsRemote, "--get-url", remote} }

// parseScoped reads `git config --show-scope -z --get-regexp` output (parseSettings).
func parseScoped(out []byte) ([]scopedSetting, error) { return parseSettings(out, false) }

// parseSettings reads `git config --show-scope -z` output, --show-origin's too when
// withOrigin is set: for each setting, its scope and a NUL, then its origin and a NUL, then
// its key, a newline and its value (or nothing, for a key without one), and a NUL.
func parseSettings(out []byte, withOrigin bool) ([]scopedSetting, error) {
	text := strings.TrimSuffix(string(out), "\x00")
	if text == "" {
		return nil, nil
	}

	each := 2
	if withOrigin {
		each = 3
	}

	fields := strings.Split(text, "\x00")
	if len(fields)%each != 0 {
		return nil, fmt.Errorf("%w: config settings as %q", errOutput, out)
	}

	settings := make([]scopedSetting, 0, len(fields)/each)

	for i := 0; i < len(fields); i += each {
		s := scopedSetting{scope: fields[i]}
		if withOrigin {
			s.origin = fields[i+1]
		}

		s.key, s.value, _ = strings.Cut(fields[i+each-1], "\n")
		settings = append(settings, s)
	}

	return settings, nil
}

// helperURL reports a URL in git's <helper>::<address> form: a scheme's characters, a
// letter or a digit first (git's is_urlschemechar), then "::".
func helperURL(url string) bool {
	name, _, ok := strings.Cut(url, "::")
	if !ok || name == "" || !isLetter(rune(name[0])) && !isDigit(rune(name[0])) {
		return false
	}

	return !strings.ContainsFunc(name, func(r rune) bool { return !isSchemeChar(r) })
}

// isSchemeChar reports a character a URL scheme, or a remote helper's name, may hold.
func isSchemeChar(r rune) bool {
	return isLetter(r) || isDigit(r) || r == '+' || r == '-' || r == '.'
}

// isDigit reports an ASCII digit.
func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// httpURL reports a URL git reaches over http or https.
func httpURL(url string) bool {
	scheme, _, ok := strings.Cut(url, "://")

	return ok && (strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https"))
}

// cloneRoute returns the first of remote's route keys (isRouteKey) the clone's own config sets,
// and whether it sets one.
func cloneRoute(settings []scopedSetting, remote string) (string, bool) {
	for _, s := range settings {
		if !s.trusted() && isRouteKey(s.key, remote) {
			return s.key, true
		}
	}

	return "", false
}

// isLetter reports an ASCII letter.
func isLetter(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }

// allowList returns GIT_ALLOW_PROTOCOL's value for an ls-remote: each of allowedProtocols
// that the user's own GIT_ALLOW_PROTOCOL lists, when they set one, or else that their own
// config doesn't set to never (its protocol.<name>.allow, else protocol.allow).
func allowList(settings []scopedSetting, userAllow *string) string {
	allowed := func(p string) bool { return userAllows(settings, p) }

	if userAllow != nil {
		listed := strings.Split(*userAllow, ":")
		allowed = func(p string) bool { return slices.Contains(listed, p) }
	}

	return strings.Join(slices.DeleteFunc(allowedProtocols(), func(p string) bool { return !allowed(p) }), ":")
}

// userAllows reports whether the user's own config allows the transport p: its own
// protocol.<name>.allow, else protocol.allow, isn't never.
func userAllows(settings []scopedSetting, p string) bool {
	allow, set := lastTrusted(settings, protocolKeyPrefix+p+protocolKeySuffix)
	if !set {
		allow, _ = lastTrusted(settings, protocolDefaultKey)
	}

	return allow != protocolNever
}

// sshEnvironment returns env with the ssh command the ls-remote runs, as remoteGuard
// describes.
func sshEnvironment(env []string, settings []scopedSetting) []string {
	if hasVar(env, sshCommandVar) {
		return env
	}

	chosen, userChose := lastTrusted(settings, sshCommandKey)
	cloneChose := cloneSets(settings, isKey(sshCommandKey))

	switch {
	case !cloneChose && (userChose || hasVar(env, sshVar)):
		return env
	case !cloneChose:
		return append(env, sshCommandVar+"="+batchSSH)
	case userChose:
		return append(env, sshCommandVar+"="+chosen)
	case hasVar(env, sshVar):
		return append(env, sshCommandVar+"="+shellQuote(varValue(env, sshVar)))
	default:
		return append(env, sshCommandVar+"="+batchSSH)
	}
}

// askPassSettings returns the -c setting that replaces a core.askPass the clone sets, as
// remoteGuard describes, or none.
func askPassSettings(env []string, settings []scopedSetting) ([]string, error) {
	if hasVar(env, askPassVar) || !cloneSets(settings, isKey(askPassKey)) {
		return nil, nil
	}

	chosen, ok := lastTrusted(settings, askPassKey)
	if !ok {
		chosen = varValue(env, sshAskPassVar)
	}

	return configSetting(askPassKey, chosen)
}

// helperSettings returns the -c settings that drop the credential helpers the clone sets
// and keep the user's own, as remoteGuard describes, or none.
func helperSettings(settings []scopedSetting) ([]string, error) {
	if !cloneSets(settings, isHelperKey) {
		return nil, nil
	}

	out := []string{"-c", helperKey + "="}

	for _, s := range settings {
		if !s.trusted() || !isHelperKey(s.key) {
			continue
		}

		setting, err := configSetting(s.key, s.value)
		if err != nil {
			return nil, err
		}

		out = append(out, setting...)
	}

	return out, nil
}

// cookieSettings returns the -c settings that keep an ls-remote from writing a cookie
// file: http.saveCookies=false, then the same for each URL a saveCookies setting names,
// whose own setting beats the general one, and an empty cookie file for each one the
// clone names.
func cookieSettings(settings []scopedSetting) ([]string, error) {
	out := []string{"-c", "http.savecookies=false"}

	var done []string

	for _, s := range settings {
		var value string

		switch name, ok := httpName(s.key); {
		case !ok || slices.Contains(done, s.key) || s.key == "http.savecookies":
			continue
		case name == "savecookies":
			value = "false"
		case name == "cookiefile" && !s.trusted():
			value = ""
		default:
			continue
		}

		done = append(done, s.key)

		setting, err := configSetting(s.key, value)
		if err != nil {
			return nil, err
		}

		out = append(out, setting...)
	}

	return out, nil
}

// httpName returns the setting's name in an http.<name> or http.<url>.<name> key, and
// whether key is one.
func httpName(key string) (string, bool) {
	if !strings.HasPrefix(key, httpPrefix) {
		return "", false
	}

	return key[strings.LastIndexByte(key, '.')+1:], true
}

// isRouteKey reports one of routeKeys, for every URL or one, or remote.<remote>.proxy.
func isRouteKey(key, remote string) bool {
	name, ok := httpName(key)

	return key == "remote."+remote+".proxy" || ok && slices.Contains(routeKeys(), name)
}

// configSetting returns key=value as a -c setting, refusing a key passable doesn't take.
func configSetting(key, value string) ([]string, error) {
	if !passable(key) {
		return nil, fmt.Errorf("%w: a config key %q", ErrGitCommandNotAllowed, key)
	}

	return []string{"-c", key + "=" + value}, nil
}

// passable reports whether name can be part of a -c setting's key: git splits a -c
// setting at its first '=', so a name holding one can't, nor one holding a control
// character.
func passable(name string) bool {
	return !strings.ContainsRune(name, '=') && !strings.ContainsFunc(name, func(r rune) bool { return r < ' ' || r == 0x7f })
}

// lastTrusted returns the value of the last setting of key from the user's own config,
// the one git would use, and whether there is one.
func lastTrusted(settings []scopedSetting, key string) (string, bool) {
	for _, s := range slices.Backward(settings) {
		if s.key == key && s.trusted() {
			return s.value, true
		}
	}

	return "", false
}

// lastAny returns the value of the last setting of key, from any config, and whether there
// is one.
func lastAny(settings []scopedSetting, key string) (string, bool) {
	for _, s := range slices.Backward(settings) {
		if s.key == key {
			return s.value, true
		}
	}

	return "", false
}

// cloneSets reports whether the clone's own config sets a key that matches.
func cloneSets(settings []scopedSetting, matches func(key string) bool) bool {
	return slices.ContainsFunc(settings, func(s scopedSetting) bool { return !s.trusted() && matches(s.key) })
}

// isKey returns a matcher for one key.
func isKey(want string) func(key string) bool {
	return func(key string) bool { return key == want }
}

// isHelperKey reports credential.helper, or a URL's own credential.<url>.helper.
func isHelperKey(key string) bool {
	return strings.HasPrefix(key, helperPrefix) && strings.HasSuffix(key, helperSuffix)
}

// varValue returns the value env gives key, or "".
func varValue(env []string, key string) string {
	for _, kv := range slices.Backward(env) {
		if value, ok := strings.CutPrefix(kv, key+"="); ok {
			return value
		}
	}

	return ""
}

// lookupVar returns the value env gives key, or nil when it sets none.
func lookupVar(env []string, key string) *string {
	for _, kv := range slices.Backward(env) {
		if value, ok := strings.CutPrefix(kv, key+"="); ok {
			return &value
		}
	}

	return nil
}

// withVar returns env with key set to value, in place of any value it had.
func withVar(env []string, key, value string) []string {
	out := slices.DeleteFunc(slices.Clone(env), func(kv string) bool { return strings.HasPrefix(kv, key+"=") })

	return append(out, key+"="+value)
}

// shellQuote quotes s as one word for the shell git runs GIT_SSH_COMMAND with.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
