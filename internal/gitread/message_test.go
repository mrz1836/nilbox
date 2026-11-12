package gitread

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errGitExit is git exiting with a status, and errUnread a failure that isn't git's.
var (
	errGitExit = errors.New("exit status 1")
	errUnread  = errors.New("config unreadable")
)

// TestMessage pins Message's words for every shape a failure reaches it in: a git
// command's failure as failure builds it, with or without a line git said, alone or
// wrapped by a caller; one that ran out of time; a refused command or remote; the clone's
// own settings; settings set aside; and errors gitread makes itself.
func TestMessage(t *testing.T) {
	t.Parallel()

	inner := fmt.Errorf("run git: %w", errGitExit)
	unread := errUnread

	tests := []struct {
		err  error
		want string
	}{
		{
			err:  failure([]string{lsRemote}, []byte("fatal: Permission denied (publickey).\n"), inner),
			want: "Permission denied (publickey).",
		},
		{err: failure([]string{statusCmd}, nil, inner), want: "git status"},
		{
			err:  failure([]string{revParse}, []byte("error: bad revision: HEAD~\nmore\n"), inner),
			want: "bad revision: HEAD~",
		},
		{
			err:  failure([]string{lsRemote}, nil, fmt.Errorf("%w: %w: %w", ErrRemoteTimeout, context.DeadlineExceeded, inner)),
			want: "origin didn't answer within " + RemoteTimeout.String(),
		},
		{
			err:  fmt.Errorf("fetch: %w", fmt.Errorf("%w: %w", context.DeadlineExceeded, inner)),
			want: "git didn't finish in time",
		},
		{
			err:  failure([]string{lsRemote}, []byte("fatal: the remote end hung up\n"), fmt.Errorf("%w: %w", context.Canceled, inner)),
			want: "the remote end hung up",
		},
		{err: failure([]string{statusCmd}, nil, fmt.Errorf("%w: %w", context.Canceled, inner)), want: "git status"},
		{err: unread, want: unread.Error()},
		{
			err: &RefusedError{Remote: "upstream", Err: fmt.Errorf("%w (upstream's URL is \"ext::x\")", ErrRemoteHelper)},
			want: "This clone reaches upstream through a remote helper (a URL that starts with a name and ::, or " +
				"remote.upstream.vcs), which isn't run here, as the clone's own config could make it run anything.",
		},
		{
			err: failure([]string{lsRemote}, nil,
				&RefusedError{Remote: "upstream", Err: fmt.Errorf("%w (upstream's URL is \"ext::x\")", ErrRemoteHelper)}),
			want: "This clone reaches upstream through a remote helper (a URL that starts with a name and ::, or " +
				"remote.upstream.vcs), which isn't run here, as the clone's own config could make it run anything.",
		},
		{
			err: fmt.Errorf("fetch: %w", &RefusedError{Remote: "upstream", Err: ErrCloneRoutes}),
			want: "This clone's own git config changes how upstream is reached (its proxy, the certificates it trusts, " +
				"or the address it resolves), so it isn't asked from here, where it could be handed your credentials: " +
				"set that for this folder in your global git config, with an includeIf.",
		},
		{
			err: fmt.Errorf("pull: %w", &CloneConfigError{Keys: []string{"core.worktree"}, Origins: []string{"file:.git/config"}}),
			want: "This clone's own git config sets core.worktree, which isn't run with here: a clone's own config may " +
				"hold only what git and Git LFS write there. Set it for this folder in your global git config, with an " +
				"includeIf, instead.",
		},
		{
			err: &CloneConfigError{Keys: []string{"hook.a.command", "hook.a.event"}, Origins: []string{"file:x", "file:x"}},
			want: "This clone's own git config sets hook.a.command and hook.a.event, which aren't run with here: a " +
				"clone's own config may hold only what git and Git LFS write there. Set them for this folder in your " +
				"global git config, with an includeIf, instead.",
		},
		{
			err: &CloneConfigError{
				Keys: []string{"a.b", "c.d", "e.f", "g.h", "i.j"}, Origins: []string{"file:x", "file:x", "file:x", "file:x", "file:x"},
			},
			want: "This clone's own git config sets a.b, c.d, e.f, and 2 more, which aren't run with here: a clone's own " +
				"config may hold only what git and Git LFS write there. Set them for this folder in your global git " +
				"config, with an includeIf, instead.",
		},
		{
			err:  fmt.Errorf("%w: %w", ErrNoGit, errUnread),
			want: "git can't be found on your login shell's PATH or in the usual tool folders.",
		},
		{
			err:  fmt.Errorf("%w: /x is in \"/\"", ErrWorkTree),
			want: "Git doesn't take this folder as its clone's top folder, so nothing that changes it runs from here.",
		},
		{
			err: &SetAsideError{Err: inner, Settings: []string{SetAsideHelpers}, Remote: "upstream"},
			want: inner.Error() + ". This clone's own credential.helper isn't used to reach upstream, which may be " +
				"why: set it for this folder in your global git config, with an includeIf.",
		},
		{
			err: failure([]string{lsRemote}, []byte("fatal: Permission denied (publickey)\n"),
				&SetAsideError{Err: inner, Settings: []string{SetAsideSSHCommand}}),
			want: "Permission denied (publickey). This clone's own core.sshCommand isn't used to reach origin, " +
				"which may be why: set it for this folder in your global git config, with an includeIf.",
		},
		{
			err: failure([]string{lsRemote}, []byte("fatal: Repository not found.\n"), &SetAsideError{
				Err: inner, Settings: []string{SetAsideSSHCommand, SetAsideHelpers},
			}),
			want: "Repository not found. This clone's own core.sshCommand and credential.helper aren't used to reach " +
				"origin, which may be why: set them for this folder in your global git config, with an includeIf.",
		},
		{
			err: failure([]string{lsRemote}, nil, &SetAsideError{
				Err: inner, Settings: []string{SetAsideSSHCommand, SetAsideAskPass, SetAsideHelpers},
			}),
			want: "git ls-remote. This clone's own core.sshCommand, core.askPass, and credential.helper aren't used to " +
				"reach origin, which may be why: set them for this folder in your global git config, with an includeIf.",
		},
		{err: &SetAsideError{Err: inner}, want: inner.Error()},
		{
			err: failure([]string{lsRemote}, nil, fmt.Errorf("%w (http.proxy)", ErrCloneRoutes)),
			want: "This clone's own git config changes how origin is reached (its proxy, the certificates it trusts, " +
				"or the address it resolves), so it isn't asked from here, where it could be handed your credentials: " +
				"set that for this folder in your global git config, with an includeIf.",
		},
		{
			err: failure([]string{lsRemote}, nil, fmt.Errorf("%w (origin's URL is \"ssh::x\")", ErrRemoteHelper)),
			want: "This clone reaches origin through a remote helper (a URL that starts with a name and ::, or " +
				"remote.origin.vcs), which isn't run here, as the clone's own config could make it run anything.",
		},
		{
			err:  failure([]string{statusCmd}, nil, fmt.Errorf("%w: a filter driver named %q", ErrGitCommandNotAllowed, "filter.a b.clean")),
			want: `a filter driver named "filter.a b.clean"`,
		},
		{err: fmt.Errorf("%w: a remote named %q", ErrGitCommandNotAllowed, "-x"), want: `a remote named "-x"`},
		{err: fmt.Errorf("%w: a count of %q", errOutput, "many"), want: `unexpected git output: a count of "many"`},
		{
			err:  fmt.Errorf("look for %s: %w", indexLock, &fs.PathError{Op: "lstat", Path: "/x/.git/index.lock", Err: fs.ErrPermission}),
			want: "look for index.lock",
		},
		{err: fmt.Errorf("fetch: %w", errUnread), want: "fetch"},
		{err: fmt.Errorf("git config: bad config line 3 in file .git/config: %w", inner), want: "bad config line 3 in file .git/config"},

		// A caller's wrap of a git command's failure changes none of its words.
		{
			err:  fmt.Errorf("read the branch: %w", failure([]string{revParse}, []byte("fatal: not a git repository\n"), inner)),
			want: "not a git repository",
		},
		{err: fmt.Errorf("read the branch: %w", failure([]string{statusCmd}, nil, inner)), want: "git status"},
		{
			err: fmt.Errorf("ask origin: %w", failure([]string{lsRemote}, []byte("fatal: Repository not found.\n"),
				&SetAsideError{Err: inner, Settings: []string{SetAsideHelpers}})),
			want: "Repository not found. This clone's own credential.helper isn't used to reach origin, which may be " +
				"why: set it for this folder in your global git config, with an includeIf.",
		},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, Message(tc.err), "%v", tc.err)
	}
}

// TestMessageOfAHelpersFailure words what gitread's own helpers return: a git command
// that failed, saying what git said, a refused folder or command, naming what was
// refused, and an answer gitread can't read.
func TestMessageOfAHelpersFailure(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	g := fakeGit(plainEnv(), &recorder{replies: map[string]reply{
		words(currentBranchArgs()):           {code: 128},
		words(unpushedArgs(mainBranch)):      {out: "many\n"},
		words(verifyArgs("refs/heads/main")): {code: 128},
	}})

	_, err := g.CurrentBranch(ctx, repoDir)
	require.Error(t, err)
	assert.Equal(t, "fake refusal", Message(err))

	_, err = g.LocalBranch(ctx, repoDir, mainBranch)
	require.Error(t, err)
	assert.Equal(t, "fake refusal", Message(err))

	_, err = g.Changes(ctx, repoDir)
	require.Error(t, err)
	assert.Equal(t, "fake git knows no such command", Message(err))

	_, err = g.Unpushed(ctx, repoDir, mainBranch)
	require.Error(t, err)
	assert.Equal(t, `unexpected git output: a count of "many\n"`, Message(err))

	_, err = g.CurrentBranch(ctx, "example-repo")
	require.ErrorIs(t, err, ErrGitCommandNotAllowed)
	assert.Equal(t, `the folder "example-repo"`, Message(err))

	_, _, err = g.Run(ctx, repoDir, "push")
	require.ErrorIs(t, err, ErrGitCommandNotAllowed)
	assert.Equal(t, `git ["push"]`, Message(err))
}
