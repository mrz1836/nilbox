package gitread

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// errGitExit is git exiting with a status, and errUnread a failure that isn't git's.
var (
	errGitExit = errors.New("exit status 1")
	errUnread  = errors.New("config unreadable")
)

func TestMessage(t *testing.T) {
	t.Parallel()

	inner := fmt.Errorf("run git: %w", errGitExit)
	unread := errUnread

	tests := []struct {
		err  error
		want string
	}{
		{err: fmt.Errorf("git ls-remote: Permission denied (publickey).: %w", inner), want: "Permission denied (publickey)."},
		{err: fmt.Errorf("git status: %w", inner), want: "git status"},
		{
			err:  fmt.Errorf("git ls-remote: %w", fmt.Errorf("%w: %w: %w", ErrRemoteTimeout, context.DeadlineExceeded, inner)),
			want: "origin didn't answer within " + RemoteTimeout.String(),
		},
		{
			err:  fmt.Errorf("fetch: %w", fmt.Errorf("%w: %w", context.DeadlineExceeded, inner)),
			want: "git didn't finish in time",
		},
		{err: unread, want: unread.Error()},
		{
			err: &RefusedError{Remote: "upstream", Err: fmt.Errorf("%w (upstream's URL is \"ext::x\")", ErrRemoteHelper)},
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
			err:  fmt.Errorf("%w: /x is in \"/\"", ErrWorkTree),
			want: "Git doesn't take this folder as its clone's top folder, so nothing that changes it runs from here.",
		},
		{
			err: &SetAsideError{Err: inner, Settings: []string{SetAsideHelpers}, Remote: "upstream"},
			want: inner.Error() + ". This clone's own credential.helper isn't used to reach upstream, which may be " +
				"why: set it for this folder in your global git config, with an includeIf.",
		},
		{
			err: fmt.Errorf("git ls-remote: Permission denied (publickey): %w",
				&SetAsideError{Err: inner, Settings: []string{SetAsideSSHCommand}}),
			want: "Permission denied (publickey). This clone's own core.sshCommand isn't used to reach origin, " +
				"which may be why: set it for this folder in your global git config, with an includeIf.",
		},
		{
			err: fmt.Errorf("git ls-remote: Repository not found.: %w", &SetAsideError{
				Err: inner, Settings: []string{SetAsideSSHCommand, SetAsideHelpers},
			}),
			want: "Repository not found. This clone's own core.sshCommand and credential.helper aren't used to reach " +
				"origin, which may be why: set them for this folder in your global git config, with an includeIf.",
		},
		{
			err: fmt.Errorf("git ls-remote: %w", &SetAsideError{
				Err: inner, Settings: []string{SetAsideSSHCommand, SetAsideAskPass, SetAsideHelpers},
			}),
			want: "git ls-remote. This clone's own core.sshCommand, core.askPass, and credential.helper aren't used to " +
				"reach origin, which may be why: set them for this folder in your global git config, with an includeIf.",
		},
		{err: &SetAsideError{Err: inner}, want: inner.Error()},
		{
			err: fmt.Errorf("git ls-remote: %w", fmt.Errorf("%w (http.proxy)", ErrCloneRoutes)),
			want: "This clone's own git config changes how origin is reached (its proxy, the certificates it trusts, " +
				"or the address it resolves), so it isn't asked from here, where it could be handed your credentials: " +
				"set that for this folder in your global git config, with an includeIf.",
		},
		{
			err: fmt.Errorf("git ls-remote: %w", fmt.Errorf("%w (origin's URL is \"ssh::x\")", ErrRemoteHelper)),
			want: "This clone reaches origin through a remote helper (a URL that starts with a name and ::, or " +
				"remote.origin.vcs), which isn't run here, as the clone's own config could make it run anything.",
		},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, Message(tc.err), "%v", tc.err)
	}
}
