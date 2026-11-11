// Package gitread runs the read-only git commands the release, hooks, and Repo scripts
// services ask of a clone, and reads their answers: the current branch, uncommitted
// changes, what git is in the middle of, branches and tags here and on origin, the
// commits between two, the signing settings, and where the hooks live. Message words
// their failures for the user.
//
// Every command is checked against an exact allowlist before it runs, and anything else
// is refused with ErrGitCommandNotAllowed: no command here changes a clone's files, index,
// refs, or config, or runs a hook, and a status runs with the filter drivers the clone's
// own config defines turned off, so reading a clone runs no program the clone names. Each runs as `git -C <dir> -c core.fsmonitor=false -c
// submodule.recurse=false <args>`, with GIT_OPTIONAL_LOCKS=0, so a status never takes the
// index lock a release might need, and GIT_TERMINAL_PROMPT=0, so git never waits for an
// answer no one can give. core.hooksPath is left alone: the hooks scan needs the folder
// it names, and none of these commands runs a hook.
//
// git is the one on the PATH of the environment Config.Env returns, the login shell's in
// the app, so the checks use the same git, and the same credential helpers, as the tools
// the user releases with. The ls-remote commands, the only ones that reach origin, stop
// after RemoteTimeout, and run ssh in batch mode unless the user chose their own ssh
// command, which can carry an agent such as a password manager's. They run no program the
// clone's own config names (see remoteGuard): a remote helper is refused, and so is an
// http or https origin the clone's own config reroutes; only the file, http, https, and
// ssh transports are allowed; the clone's ssh command, password program, and credential
// helpers give way to the user's own; no cookie file is written; and origin is asked for
// git's own upload-pack. No other command reaches a transport at all.
package gitread

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/shellenv"
)

const (
	// gitBin is the program every command runs.
	gitBin = "git"

	// RemoteTimeout bounds one ls-remote, the only command that reaches origin.
	RemoteTimeout = 15 * time.Second

	// batchSSH is the ssh command an ls-remote runs with when the user set none: it never
	// asks for a password or a host key's approval, and gives up on a host that doesn't
	// answer.
	batchSSH = "ssh -o BatchMode=yes -o ConnectTimeout=10"

	// sshCommandVar, and the older sshVar, choose the ssh git runs.
	sshCommandVar = "GIT_SSH_COMMAND"
	sshVar        = "GIT_SSH"
)

// ErrGitCommandNotAllowed is returned for a git command outside the allowlist, or a
// folder that is not absolute and clean; it is never run.
var ErrGitCommandNotAllowed = errors.New("git command not allowed")

// Config holds a Git's settings. Every field is optional.
type Config struct {
	// Env returns the environment each command starts from, read again for every
	// command: in the app, the login shell's (shellenv), so git, ssh, and the credential
	// helpers see what the user's terminal does. git is found on its PATH. It defaults to
	// os.Environ.
	Env func() []string

	// Runner returns what runs one command, given that command's whole environment. It
	// defaults to an execpath.Exec that runs with env and finds git on env's PATH, then
	// in the well-known tool folders (execpath.Search).
	Runner func(env []string) execpath.Runner
}

// Git runs allowlisted, read-only git commands. It is safe for concurrent use.
type Git struct {
	env    func() []string
	runner func(env []string) execpath.Runner
}

// New returns a Git over cfg.
func New(cfg Config) *Git {
	g := &Git{env: cfg.Env, runner: cfg.Runner}

	if g.env == nil {
		g.env = os.Environ
	}

	if g.runner == nil {
		g.runner = execRunner
	}

	return g
}

// Run runs `git -C dir -c core.fsmonitor=false -c submodule.recurse=false args...` and
// returns what git wrote, once args has passed the allowlist and dir is absolute and
// clean; otherwise it returns ErrGitCommandNotAllowed and runs nothing. A non-zero exit is
// an error that execpath.ExitCode reads. An ls-remote stops after RemoteTimeout, when the
// error wraps context.DeadlineExceeded. One that fails otherwise, after setting aside a
// setting the clone's own config makes, returns a *SetAsideError.
func (g *Git) Run(ctx context.Context, dir string, args ...string) (stdout, stderr []byte, err error) {
	switch {
	case !isDir(dir):
		return nil, nil, fmt.Errorf("%w: the folder %q", ErrGitCommandNotAllowed, dir)
	case !allowed(args):
		return nil, nil, fmt.Errorf("%w: git %q", ErrGitCommandNotAllowed, args)
	case args[0] == statusCmd:
		return g.status(ctx, dir, args)
	case args[0] != lsRemote:
		return g.exec(ctx, dir, g.environment(), nil, args)
	}

	return g.remote(ctx, dir, args)
}

// remote runs an ls-remote, which has passed the allowlist, within RemoteTimeout and as
// remoteGuard sets it up, and words its failure as Run says.
func (g *Git) remote(ctx context.Context, dir string, args []string) (stdout, stderr []byte, err error) {
	ctx, cancel := context.WithTimeout(ctx, RemoteTimeout)
	defer cancel()

	base := g.env()

	setup, stderr, err := g.remoteGuard(ctx, dir, environmentOf(base), lookupVar(base, allowProtocolVar))
	if err == nil {
		stdout, stderr, err = g.exec(ctx, dir, setup.env, setup.extra, args)
	}

	switch {
	case err == nil:
	case ctx.Err() != nil:
		err = fmt.Errorf("%w: %w", ctx.Err(), err)
	case len(setup.setAside) > 0:
		err = &SetAsideError{Err: err, Settings: setup.setAside}
	}

	return stdout, stderr, err
}

// exec runs git in dir with env and args, which have passed the allowlist, and the extra
// settings, which gitread builds itself.
func (g *Git) exec(ctx context.Context, dir string, env, extra, args []string) (stdout, stderr []byte, err error) {
	safe := hardening()
	argv := make([]string, 0, len(safe)+len(extra)+len(args)+2)
	argv = append(argv, "-C", dir)
	argv = append(argv, safe...)
	argv = append(argv, extra...)
	argv = append(argv, args...)

	return g.runner(env).Run(ctx, gitBin, argv...)
}

// status runs a status with every filter driver the clone's own config defines turned
// off. A status reads a file whose stat data changed, as in a copied clone, and reading
// it runs the clean filter or the process its .gitattributes names: a clone's config could
// make that any program, run with no click when the Release page reads its clones. The
// user's own drivers, in their global or system config, such as Git LFS's, still run. A
// driver whose name can't be passed safely refuses the status.
func (g *Git) status(ctx context.Context, dir string, args []string) (stdout, stderr []byte, err error) {
	env := g.environment()

	off, errOut, err := g.filtersOff(ctx, dir, env)
	if err != nil {
		return nil, errOut, err
	}

	return g.exec(ctx, dir, env, off, args)
}

// filtersOff reads the filter drivers' program settings in dir, with where each is set,
// and returns the settings that turn off every driver the clone's own config defines
// (cloneFiltersOff), or git's error output when they can't be read.
func (g *Git) filtersOff(ctx context.Context, dir string, env []string) (off []string, stderr []byte, err error) {
	out, errOut, err := g.exec(ctx, dir, env, nil, filterDriversArgs())

	switch {
	case err == nil:
	case ctx.Err() == nil && execpath.ExitCode(err) == 1:
		out = nil // no filter drivers set
	default:
		return nil, errOut, err
	}

	off, err = cloneFiltersOff(out)

	return off, nil, err
}

// cloneFiltersOff reads `git config --show-scope --get-regexp` lines for the filter
// drivers' keys and returns the settings that turn off each driver from the clone's own
// config (any scope but trustedScopes): its clean, smudge, and process commands emptied,
// which git reads as no filter, and required off, so the file is read or written as it is
// rather than failing. A driver name passable refuses can't be named in a -c setting, and
// is refused.
func cloneFiltersOff(out []byte) ([]string, error) {
	var (
		off  []string
		seen []string
	)

	for line := range strings.Lines(string(out)) {
		scope, rest, ok := strings.Cut(strings.TrimRight(line, "\n"), "\t")
		if !ok || trustedScope(scope) {
			continue
		}

		key, _, _ := strings.Cut(rest, " ")

		name, ok := filterName(key)
		if !ok {
			return nil, fmt.Errorf("%w: a filter driver named %q", ErrGitCommandNotAllowed, key)
		}

		if slices.Contains(seen, name) {
			continue
		}

		seen = append(seen, name)
		off = append(off, "-c", "filter."+name+".clean=", "-c", "filter."+name+".smudge=",
			"-c", "filter."+name+".process=", "-c", "filter."+name+".required=false")
	}

	return off, nil
}

// filterName returns the driver's name in a filter.<name>.clean, .smudge, or .process
// key, and whether it can be named in a -c setting.
func filterName(key string) (string, bool) {
	rest, ok := strings.CutPrefix(key, "filter.")
	if !ok {
		return "", false
	}

	cut := strings.LastIndexByte(rest, '.')
	if cut <= 0 {
		return "", false
	}

	name := rest[:cut]

	return name, passable(name)
}

// filterDriversArgs lists the filter drivers' program settings with where each is set.
func filterDriversArgs() []string {
	return []string{configCmd, "--show-scope", "--get-regexp", filterKeysPattern}
}

// environment returns Env's environment with GIT_OPTIONAL_LOCKS=0, GIT_TERMINAL_PROMPT=0,
// GIT_NO_LAZY_FETCH=1, and an empty GIT_ALLOW_PROTOCOL in place of any value it had, and
// without the variables that would point git at another repository than -C's or another
// config file. No command reaches a transport: a partial clone's missing object isn't
// fetched (GIT_NO_LAZY_FETCH from git 2.44, and no transport allowed before it), so a
// clone can't make a read reach origin. An ls-remote allows its own transports again.
func (g *Git) environment() []string { return environmentOf(g.env()) }

// environmentOf is environment over base.
func environmentOf(base []string) []string {
	dropped := droppedVars()
	env := make([]string, 0, len(base)+4)

	for _, kv := range base {
		if key, _, _ := strings.Cut(kv, "="); !slices.Contains(dropped, key) {
			env = append(env, kv)
		}
	}

	return append(env, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", allowProtocolVar+"=")
}

// hardening are the settings every command runs with: the file system monitor and
// submodule recursion off. Unlike a command that changes a clone (Hardening), hooks stay
// where core.hooksPath puts them, since `rev-parse --git-path hooks` must name the real
// folder, and none of these commands runs one.
func hardening() []string {
	return []string{"-c", "core.fsmonitor=false", "-c", "submodule.recurse=false"}
}

// Hardening returns the settings, ahead of its subcommand, that every git command the app
// runs to change a clone runs with: no hook, the file system monitor off, and no submodule
// recursion. git runs hooks and the file system monitor with the clone as their working
// folder, so either could run a program the clone's own files hold, and a submodule's URL
// is the clone's to name. internal/worktrees runs each of its commands with them, and Repo
// scripts' pull too (RemoteSetup).
func Hardening() []string {
	return append([]string{"-c", "core.hooksPath=" + os.DevNull}, hardening()...)
}

// ValidBranch reports a branch name the app may hand git as an argument: one that can't be
// read as an option, a range, a refspec's src:dst, or a pattern, and that git itself
// would take (validBranch).
func ValidBranch(name string) bool {
	return validBranch(name)
}

// droppedVars are the variables environment drops: the four it sets, those that would
// point git at another repository, index, object store, or namespace than the one at -C,
// and GIT_CONFIG, which only `git config` reads, so it would hide the clone's own settings
// from the guards' reads while the command they guard still reads them.
func droppedVars() []string {
	return []string{
		"GIT_OPTIONAL_LOCKS", "GIT_TERMINAL_PROMPT", "GIT_NO_LAZY_FETCH", allowProtocolVar, "GIT_DIR", "GIT_WORK_TREE",
		"GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE",
		"GIT_CONFIG",
	}
}

// hasVar reports whether env sets key, to any value: git reads an empty GIT_SSH_COMMAND
// as set, too.
func hasVar(env []string, key string) bool {
	return slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, key+"=") })
}

// execRunner is the default Runner: an execpath.Exec that runs with env and finds git as
// gitSearch does.
func execRunner(env []string) execpath.Runner {
	return execpath.Exec{Search: gitSearch(env), Env: env}
}

// gitSearch finds git on env's PATH, then in the well-known tool folders under env's HOME:
// the git every command runs, and the one RemoteSetup names.
func gitSearch(env []string) execpath.Search {
	return execpath.Search{Path: shellenv.Lookup(env, "PATH"), Home: shellenv.Lookup(env, "HOME")}
}
