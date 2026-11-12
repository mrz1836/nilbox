// Package notifyprefs reads and writes which kinds of notification show a banner, the
// pref the settings page changes and the notify package obeys. It imports nothing of the
// app's but the prefval leaf, so the settings service does not depend on the notify
// package, nor through it on the inbox and the GitHub client.
package notifyprefs

import (
	"errors"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
)

// KeyBannersOff is the prefs key listing, as a JSON array, the kinds of notification whose
// banners start on and were turned off. Such a kind is on unless listed, so a kind added
// later that starts on is on. The settings package writes it.
const KeyBannersOff = "notify.bannersOff"

// KeyBannersOn is the prefs key listing, as a JSON array, the kinds of notification whose
// banners start off and were turned on (see Kind.DefaultOn). The settings package writes
// it.
const KeyBannersOn = "notify.bannersOn"

// Kind is a kind of notification: one of the inbox's kinds of arrival (see
// inbox.Arrival), a moment an agent session needs the user, or an event about no item,
// such as a release that reached GitHub or a run that failed.
type Kind string

// Kinds of notification.
const (
	// KindReviewRequested is a pull request that newly asks for the user's review.
	KindReviewRequested Kind = "review_requested"

	// KindAssigned is an issue or pull request newly assigned to the user.
	KindAssigned Kind = "assigned"

	// KindWoke is a snoozed item that new activity brought back early.
	KindWoke Kind = "woke"

	// KindSessionNeedsYou is an agent session waiting for the user's approval or input.
	KindSessionNeedsYou Kind = "session_needs_you"

	// KindSessionFinished is an agent session that finished its turn.
	KindSessionFinished Kind = "session_finished"

	// KindReleaseDone is a release whose tag reached GitHub.
	KindReleaseDone Kind = "release_done"

	// KindReleasePublished is a release its workflow on GitHub published.
	KindReleasePublished Kind = "release_published"

	// KindReleaseFailed is a release whose workflow on GitHub failed.
	KindReleaseFailed Kind = "release_failed"

	// KindRunFailed is a run from the Scripts tab that failed or ran past its time limit.
	KindRunFailed Kind = "run_failed"
)

// ErrUnknownKind is returned for a kind of notification that is not one of Kinds.
var ErrUnknownKind = errors.New("unknown kind of notification")

// Kinds returns the kinds of notification in the order the settings list them.
func Kinds() []Kind {
	return []Kind{
		KindReviewRequested, KindAssigned, KindWoke, KindSessionNeedsYou, KindSessionFinished, KindReleaseDone,
		KindReleasePublished, KindReleaseFailed, KindRunFailed,
	}
}

// Valid reports whether k is one of Kinds.
func (k Kind) Valid() bool {
	return slices.Contains(Kinds(), k)
}

// DefaultOn reports whether a kind's banners are on until the user turns them off. The
// sessions' kinds start off: the coding agents and the terminal can already notify the
// user themselves.
func (k Kind) DefaultOn() bool {
	return k != KindSessionNeedsYou && k != KindSessionFinished
}

// DecodeBannersOff reads the kinds whose banners are off, in the order of Kinds, from the
// stored values of KeyBannersOff and KeyBannersOn: a kind that starts on is off when the
// first lists it, and one that starts off is off unless the second lists it. A value that
// is unset ("") or cannot be read lists none, and kinds that are not known are dropped.
func DecodeBannersOff(storedOff, storedOn string) []Kind {
	listedOff, _ := prefval.List[Kind](storedOff)
	listedOn, _ := prefval.List[Kind](storedOn)
	off := []Kind{}

	for _, kind := range Kinds() {
		if kind.DefaultOn() && slices.Contains(listedOff, kind) || !kind.DefaultOn() && !slices.Contains(listedOn, kind) {
			off = append(off, kind)
		}
	}

	return off
}

// SetBanner turns a kind's banners on or off in off, the kinds whose banners are off, and
// returns the new list in the order of Kinds.
func SetBanner(off []Kind, kind Kind, on bool) ([]Kind, error) {
	if !slices.Contains(Kinds(), kind) {
		return nil, fmt.Errorf("%w %q", ErrUnknownKind, kind)
	}

	out := []Kind{}

	for _, k := range Kinds() {
		isOff := slices.Contains(off, k)
		if k == kind {
			isOff = !on
		}

		if isOff {
			out = append(out, k)
		}
	}

	return out, nil
}

// EncodeBanners writes the kinds whose banners are off as the values of KeyBannersOff,
// which lists the kinds that start on and are off, and KeyBannersOn, which lists the
// kinds that start off and are on.
func EncodeBanners(off []Kind) (storedOff, storedOn string) {
	offList, onList := []Kind{}, []Kind{}

	for _, kind := range Kinds() {
		isOff := slices.Contains(off, kind)

		switch {
		case kind.DefaultOn() && isOff:
			offList = append(offList, kind)
		case !kind.DefaultOn() && !isOff:
			onList = append(onList, kind)
		}
	}

	return prefval.EncodeList(offList), prefval.EncodeList(onList)
}
