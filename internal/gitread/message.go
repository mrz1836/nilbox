package gitread

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// Message is the part of a failure of one of gitread's commands, or of a command run with
// a Setup, worth showing the user: what git said, without the command and the exit status,
// or that origin didn't answer an ls-remote in time. A clone whose own config sets
// settings a Setup refuses names them, and where to set them instead (CloneConfigError); a
// remote refused before anything ran says why, naming it (RefusedError, origin when none
// is named); and a command that failed after setting aside the clone's own ssh command,
// password program, or credential helpers says so, and where to set them instead.
func Message(err error) string {
	remote := remoteName
	if refused, ok := errors.AsType[*RefusedError](err); ok && refused.Remote != "" {
		remote = refused.Remote
	}

	if refused, ok := errors.AsType[*CloneConfigError](err); ok {
		return cloneConfigText(refused.Keys)
	}

	switch {
	case errors.Is(err, ErrRemoteHelper):
		return "This clone reaches " + remote + " through a remote helper (a URL that starts with a name and ::, or " +
			"remote." + remote + ".vcs), which isn't run here, as the clone's own config could make it run anything."
	case errors.Is(err, ErrCloneRoutes):
		return "This clone's own git config changes how " + remote + " is reached (its proxy, the certificates it " +
			"trusts, or the address it resolves), so it isn't asked from here, where it could be handed your " +
			"credentials: set that for this folder in your global git config, with an includeIf."
	case errors.Is(err, ErrWorkTree):
		return "Git doesn't take this folder as its clone's top folder, so nothing that changes it runs from here."
	case errors.Is(err, ErrNoGit):
		return "git can't be found on your login shell's PATH or in the usual tool folders."
	}

	msg := gitSaid(err)

	if aside, ok := errors.AsType[*SetAsideError](err); ok && len(aside.Settings) > 0 {
		return textutil.EndSentence(msg) + " " + setAsideNote(cmp.Or(aside.Remote, remoteName), aside.Settings)
	}

	return msg
}

// setAsideNote says which of the clone's own settings reaching remote went without, which
// may be why it failed, and where to set them so that it uses them: the user's global
// config, for this folder alone with an includeIf, which gitread counts as the user's own.
func setAsideNote(remote string, settings []string) string {
	names, verb, them := listed(settings)

	return "This clone's own " + names + " " + verb + " used to reach " + remote + ", which may be why: set " + them +
		" for this folder in your global git config, with an includeIf."
}

// maxNamed is how many settings cloneConfigText names before it counts the rest.
const maxNamed = 4

// cloneConfigText says which settings of the clone's own config refused a command that
// changes it (keys, at most maxNamed of them named), and where to set them instead: the
// user's global config, for this folder alone with an includeIf, which gitread counts as
// the user's own.
func cloneConfigText(keys []string) string {
	named := keys
	if len(keys) > maxNamed {
		named = append(slices.Clone(keys[:maxNamed-1]), fmt.Sprintf("%d more", len(keys)-maxNamed+1))
	}

	names, verb, them := listed(named)
	if len(keys) > 1 {
		verb, them = "aren't", "them"
	}

	return "This clone's own git config sets " + names + ", which " + verb + " run with here: a clone's own " +
		"config may hold only what git and Git LFS write there. Set " + them + " for this folder in your global " +
		"git config, with an includeIf, instead."
}

// listed words names as a list (textutil.Series: "a", "a and b", "a, b, and c"), with the
// verb and the pronoun that go with that many.
func listed(names []string) (list, verb, them string) {
	if len(names) == 1 {
		return names[0], "isn't", "it"
	}

	return textutil.Series(names), "aren't", "them"
}

// gitSaid is what git said in a failure, without the command and the exit status, or the
// command when git said nothing worth showing, read from the *commandError in err's
// chain, however a caller wraps it; or that origin didn't answer an ls-remote within
// RemoteTimeout, or that git ran out of time otherwise; or else textSaid.
func gitSaid(err error) string {
	switch {
	case errors.Is(err, ErrRemoteTimeout):
		return "origin didn't answer within " + RemoteTimeout.String()
	case errors.Is(err, context.DeadlineExceeded):
		return "git didn't finish in time"
	}

	if failed, ok := errors.AsType[*commandError](err); ok {
		if failed.said == "" {
			return "git " + failed.cmd
		}

		return failed.said
	}

	return textSaid(err)
}

// textSaid is what an error that holds no *commandError says, read from its text: its own
// words, without those of the error it wraps, as a file gitread couldn't look for is
// "look for index.lock"; and, in a failure's shape, "git <command>: <said>", what follows
// the command, as a refused command (ErrGitCommandNotAllowed) gives what was refused, and
// as a git failure built elsewhere in that shape gives what git said.
func textSaid(err error) string {
	text := err.Error()
	if inner := errors.Unwrap(err); inner != nil {
		text = strings.TrimSuffix(text, ": "+inner.Error())
	}

	if rest, ok := strings.CutPrefix(text, "git "); ok {
		if _, said, found := strings.Cut(rest, ": "); found && said != "" {
			return said
		}
	}

	return text
}
