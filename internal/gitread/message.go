package gitread

import (
	"context"
	"errors"
	"strings"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// Message is the part of a failure of one of gitread's commands worth showing the user:
// what git said, without the command and the exit status, or that origin didn't answer in
// time. An ls-remote refused before it ran says why, and one that failed after setting
// aside the clone's own ssh command, password program, or credential helpers says so,
// and where to set them instead.
func Message(err error) string {
	if errors.Is(err, ErrRemoteHelper) {
		return "This clone reaches origin through a remote helper (a URL that starts with a name and ::, or " +
			"remote.origin.vcs), which isn't run here, as the clone's own config could make it run anything."
	}

	if errors.Is(err, ErrCloneRoutes) {
		return "This clone's own git config changes how origin is reached (its proxy, the certificates it " +
			"trusts, or the address it resolves), so it isn't asked from here, where it could be handed your " +
			"credentials: set that for this folder in your global git config, with an includeIf."
	}

	msg := gitSaid(err)

	if aside, ok := errors.AsType[*SetAsideError](err); ok && len(aside.Settings) > 0 {
		return textutil.EndSentence(msg) + " " + setAsideNote(aside.Settings)
	}

	return msg
}

// setAsideNote says which of the clone's own settings asking origin went without, which may
// be why it failed, and where to set them so that it uses them: the user's global config,
// for this folder alone with an includeIf, which gitread counts as the user's own.
func setAsideNote(settings []string) string {
	var names, verb, them string

	switch n := len(settings); n {
	case 1:
		names, verb, them = settings[0], "isn't", "it"
	case 2:
		names, verb, them = settings[0]+" and "+settings[1], "aren't", "them"
	default:
		names, verb, them = strings.Join(settings[:n-1], ", ")+", and "+settings[n-1], "aren't", "them"
	}

	return "This clone's own " + names + " " + verb + " used to reach origin, which may be why: set " + them +
		" for this folder in your global git config, with an includeIf."
}

// gitSaid is what git said in a failure, without the command and the exit status, or that
// origin didn't answer in time.
func gitSaid(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "origin didn't answer within " + RemoteTimeout.String()
	}

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
