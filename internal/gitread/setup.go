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
	if !isDir(dir) {
		return Setup{}, fmt.Errorf("%w: the folder %q", ErrGitCommandNotAllowed, dir)
	}

	base := g.env()
	env := environmentOf(base)

	git, err := gitSearch(env).Find(gitBin)
	if err != nil {
		return Setup{}, fmt.Errorf("find git: %w", err)
	}

	setup, stderr, err := g.remoteGuard(ctx, dir, env, lookupVar(base, allowProtocolVar))
	if err != nil {
		return Setup{}, failure([]string{lsRemote}, stderr, err)
	}

	off, stderr, err := g.filtersOff(ctx, dir, env)
	if err != nil {
		return Setup{}, failure(filterDriversArgs(), stderr, err)
	}

	return Setup{
		Git: git, Env: setup.env, Settings: slices.Concat(Hardening(), setup.extra, off),
		SetAside: slices.Clone(setup.setAside),
	}, nil
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
