package gitread

import (
	"context"
	"fmt"
	"slices"
)

// Setup is what a git command that reaches origin and writes the clone's files runs with,
// Repo scripts' pull: every guard gitread's own ls-remote runs under, and those a status
// runs under, so the one command the app runs that changes a clone's checkout is held at
// least as tight as its reads.
type Setup struct {
	// Git is git's absolute path: the git every gitread command runs.
	Git string

	// Env is the command's whole environment: the ls-remote's (remoteGuard), with
	// GIT_TERMINAL_PROMPT=0, GIT_NO_LAZY_FETCH=1, the transports GIT_ALLOW_PROTOCOL allows,
	// and the ssh command the user's own config or environment chose, else a batch-mode
	// ssh.
	Env []string

	// Settings are the -c settings that go ahead of its subcommand: Hardening; the
	// ls-remote's, which set aside the clone's own password program, credential helpers,
	// and cookie file; and every filter driver the clone's own config defines turned off,
	// as a status turns them off (the user's own, such as Git LFS's, still run).
	Settings []string

	// SetAside names the clone's own settings set aside, of SetAsideSSHCommand,
	// SetAsideAskPass, and SetAsideHelpers, which may be why the command fails.
	SetAside []string
}

// RemoteSetup works out what a git command in the clone at dir that reaches origin and
// writes the clone's files runs with (Setup), reading the clone's config and origin's URL
// (`ls-remote --get-url`, which asks origin nothing) as an ls-remote's guard does. It
// refuses what that guard refuses, before anything runs: an origin reached through a
// remote helper (ErrRemoteHelper), an http or https origin the clone's own config reroutes
// (ErrCloneRoutes), and a filter driver or setting it can't name safely
// (ErrGitCommandNotAllowed).
func (g *Git) RemoteSetup(ctx context.Context, dir string) (Setup, error) {
	return g.RemoteSetupFor(ctx, dir, remoteName)
}

// RemoteSetupFor is RemoteSetup for the clone's remote called remote, as worktree creation
// fetches from a fork's upstream: that remote's URL, remote helper, and proxy are weighed
// in origin's place. A name that isn't one is refused (ErrGitCommandNotAllowed).
func (g *Git) RemoteSetupFor(ctx context.Context, dir, remote string) (Setup, error) {
	if !isRemoteName(remote) {
		return Setup{}, fmt.Errorf("%w: a remote named %q", ErrGitCommandNotAllowed, remote)
	}

	base := g.env()
	env := environmentOf(base)

	setup, err := g.checkoutSetup(ctx, dir, env)
	if err != nil {
		return Setup{}, err
	}

	guarded, stderr, err := g.remoteGuardFor(ctx, dir, remote, env, lookupVar(base, allowProtocolVar))
	if err != nil {
		return Setup{}, failure([]string{lsRemote}, stderr, err)
	}

	setup.Env = guarded.env
	setup.Settings = slices.Concat(Hardening(), guarded.extra, setup.Settings[len(Hardening()):])
	setup.SetAside = slices.Clone(guarded.setAside)

	return setup, nil
}

// CheckoutSetup works out what a git command in the clone at dir that writes its files but
// reaches no remote runs with, as `git worktree add` does: gitread's own git, the
// environment every gitread read runs with (no transport allowed, no lazy fetch, no
// prompt), Hardening, and every filter driver the clone's own config defines turned off.
func (g *Git) CheckoutSetup(ctx context.Context, dir string) (Setup, error) {
	return g.checkoutSetup(ctx, dir, g.environment())
}

// checkoutSetup is CheckoutSetup with env, the environment the command runs with.
func (g *Git) checkoutSetup(ctx context.Context, dir string, env []string) (Setup, error) {
	if !isDir(dir) {
		return Setup{}, fmt.Errorf("%w: the folder %q", ErrGitCommandNotAllowed, dir)
	}

	git, err := gitSearch(env).Find(gitBin)
	if err != nil {
		return Setup{}, fmt.Errorf("find git: %w", err)
	}

	off, stderr, err := g.filtersOff(ctx, dir, env)
	if err != nil {
		return Setup{}, failure(filterDriversArgs(), stderr, err)
	}

	return Setup{Git: git, Env: env, Settings: slices.Concat(Hardening(), off), SetAside: []string{}}, nil
}

// SetAsideNote says which of the clone's own settings, of SetAsideSSHCommand,
// SetAsideAskPass, and SetAsideHelpers, reaching origin went without, which may be why it
// failed, and where to set them so that they are used: the user's global config, for this
// folder alone with an includeIf, which gitread counts as the user's own. It is "" for
// none.
func SetAsideNote(settings []string) string {
	if len(settings) == 0 {
		return ""
	}

	return setAsideNote(settings)
}
